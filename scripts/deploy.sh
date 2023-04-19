#!/bin/bash
# Usage: ./deploy.sh [-c CR_FILE_PATH] [-u]
# to be run from ./scripts
# expects a secret called $EXPECTED_SECRET_FILE_NAME in the ./scripts folder
# To obtain it:
# 1) go to https://quay.io/user/<your_id>?tab=settings replacing <your_id> with your username
# 2) click on Generate Encrypted Password
# 3) enter your quay.io password
# 4) select Kubernetes Secret from left-hand menu
# 5) Download yaml file and rename secret as search-pull-secret

set -euo pipefail

exec_to_check="operator-sdk yq"

# default values
INSTALL_NAMESPACE="open-cluster-management"
EXPECTED_SECRET_FILE_NAME="quay_secret.yaml"
EXPECTED_SECRET_NAME="search-pull-secret"
CR_PATH="../config/samples/search_v1alpha1_search.yaml"
EXPECTED_CR_NAME="search-v2-operator"
DEFAULT_SNAPSHOT="2.7.0-SNAPSHOT-2022-10-20-11-31-03"
UNINSTALL_COMMAND="operator-sdk cleanup search-v2-operator -n $INSTALL_NAMESPACE"

usage() { echo "Usage: $0 [-c CR_FILE_PATH] [-u]" 1>&2; exit 1; }

uninstaller() {
    echo "* Deleting operator"
    eval "$UNINSTALL_COMMAND"
    echo "* Deleting quay.io secret"
    $CLI_EXEC delete secret $EXPECTED_SECRET_NAME -n "$INSTALL_NAMESPACE" || true
    echo "* Reenable search v1"
    if [ "$cluster_type" == "OpenShift" ]; then
      oc patch mch "${mch_name}" -n "$INSTALL_NAMESPACE" --type=merge -p '{"spec":{"overrides":{"components":[{"name":"search","enabled": true}]}}}'
    fi
    echo "All done!"
}

uninstall=0
while getopts ":c:u" o; do
    case "${o}" in
        c)
            CR_PATH=${OPTARG}
            ;;
        u)
            uninstall=1
            ;;
        *)
            usage
            ;;
    esac
done
shift $((OPTIND-1))

echo "* Checking local prerequisites"
# Check if the oc command exists
if command -v oc >/dev/null 2>&1; then
  CLI_EXEC="oc"
else
  # If oc doesn't exist, check if kubectl exists
  if command -v kubectl >/dev/null 2>&1; then
    CLI_EXEC="kubectl"
  else
    printf "Neither oc nor kubectl commands are found. Please install one of them."
    exit 1
  fi
fi

echo "* Using $CLI_EXEC for cluster interaction"

for exec in $exec_to_check; do
    if ! command -v "$exec" &> /dev/null
    then
        printf "**WARNING** $exec not found in PATH, please install it\n"
        exit 1
    fi
done

echo "* Testing the connection"
if ! $CLI_EXEC cluster-info >/dev/null 2>&1; then
    echo "**ERROR**: Make sure you are logged into an OpenShift Container Platform or a Kubernetes cluster before running this script"
    exit 2
fi

if $CLI_EXEC whoami >/dev/null 2>&1; then
    cluster_type="OpenShift"
else
    cluster_type="Kubernetes"
fi

echo "Connected to a $cluster_type cluster."

if [ "$cluster_type" == "OpenShift" ]; then
    # echo "* Checking cluster setup"
    mch_name="multiclusterhub" # TODO: search for name
    $CLI_EXEC get mch -n "$INSTALL_NAMESPACE" $mch_name
    if [ $? -ne 0 ]; then
    echo "**ERROR**: multiclusterhub not installed, please install it before!"
    exit 3
    fi
    # check RHACM version
    major_version=$($CLI_EXEC get MulticlusterHub $mch_name -n "$INSTALL_NAMESPACE" -o jsonpath="{.status.currentVersion}" | cut -d'.' -f 1-2)
    if [ "$major_version" != "2.6" ]; then
        echo "**ERROR**: This script applies currently only to ACM 2.6!"
        exit 3
    fi
fi


# launch uninstall if requested
if [ $uninstall -eq 1 ]; then
    uninstaller
    exit 0
fi

# check that secret file exists
if [ ! -f $EXPECTED_SECRET_FILE_NAME ]; then
  echo "**ERROR**: Secret file $EXPECTED_SECRET_FILE_NAME not found in the current directory!"
  exit 4
fi
# and that it is named correctly
secret_name=$(yq eval '.metadata.name' $EXPECTED_SECRET_FILE_NAME)
if [ "$secret_name" != $EXPECTED_SECRET_NAME ]; then
  echo "**ERROR**: Secret in $EXPECTED_SECRET_FILE_NAME must be named $EXPECTED_SECRET_NAME!"
  exit 5
fi

# check that CR is there
if [ ! -f "$CR_PATH" ]; then
    echo "**ERROR**: Custom Resource $CR_PATH not found!"
    exit 6
fi
# and that it is named correctly
cr_name=$(yq eval '.metadata.name' "$CR_PATH")
if [ "$cr_name" != $EXPECTED_CR_NAME ]; then
  echo "**ERROR**: Custom Resource in $CR_PATH must be named $EXPECTED_CR_NAME!"
  exit 7
fi

# check the operator is not already installed
EXIT_CODE=0
$CLI_EXEC get catalogsources.operators.coreos.com -n "$INSTALL_NAMESPACE" search-v2-operator-catalog > /dev/null 2>&1  || EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
    # we could try to upgrade if we have a different version, keep it simple for now
    echo "**ERROR**: operator already installed, please uninstall it before with \"$0 -u\"!"
    exit 8
fi
echo "All good, proceeding with install"

if [ "$cluster_type" == "OpenShift" ]; then
    # disable search v1
    echo "* Disable search v1"
    $CLI_EXEC patch mch ${mch_name} -n "$INSTALL_NAMESPACE" --type=merge -p '{"spec":{"overrides":{"components":[{"name":"search","enabled": false}]}}}'
fi

echo "* Apply quay.io secret"
$CLI_EXEC apply -f quay_secret.yaml -n "$INSTALL_NAMESPACE"

# deploy operator
echo "* Deploy search v2"
# compute version
printf "Find snapshot tags @ https://quay.io/repository/stolostron/search-operator-bundle?tab=tags\nEnter SNAPSHOT TAG: (Press ENTER for default: ${DEFAULT_SNAPSHOT})\n"
read -r SNAPSHOT_CHOICE
if [ "${SNAPSHOT_CHOICE}" == "" ]; then
    SNAPSHOT_CHOICE=${DEFAULT_SNAPSHOT}
fi
operator-sdk run bundle quay.io/stolostron/search-operator-bundle:${SNAPSHOT_CHOICE} --pull-secret-name $EXPECTED_SECRET_NAME -n "$INSTALL_NAMESPACE" --timeout 5m0s

# apply CR
$CLI_EXEC apply -f "$CR_PATH" -n "$INSTALL_NAMESPACE"

echo "* Done! Search v2 pods can be found in $INSTALL_NAMESPACE"
echo "* To uninstall, please run \"$0 -u\"."
