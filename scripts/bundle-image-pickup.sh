#!/bin/bash

###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

####################
## COLORS
####################
CYAN="\033[0;36m"
GREEN="\033[0;32m"
PURPLE="\033[0;35m"
RED="\033[0;31m"
YELLOW="\033[0;33m"
NC="\033[0m"

log_color () {
  case $1 in
    cyan)
      echo -e "${CYAN}$2 ${NC}"$3
    ;;
    green)
      echo -e "${GREEN}$2 ${NC}"$3
    ;;
    purple)
      echo -e "${PURPLE}$2 ${NC}"$3
    ;;
    red)
      echo -e "${RED}$2 ${NC}"$3
    ;;
    yellow)
      echo -e "${YELLOW}$2 ${NC}"$3
    ;;
  esac
}

####################
## ENV VARIABLES
####################
ORG=${ORG:-"stolostron"}
PIPELINE_REPO=${PIPELINE_REPO:-"pipeline"}
RELEASE_BRANCH=${RELEASE_BRANCH:-"2.7-integration"}

####################
## PATHS (I.E DIR, FILES, ETC)
####################
PIPELINE_MANIFEST_FILEPATH=${PIPELINE_MANIFEST_FILEPATH:-"manifest.json"}
OPERATOR_CSV_FILEPATH=${OPERATOR_CSV_FILEPATH:-"bundle/manifests/search-v2-operator.clusterserviceversion.yaml"}

OPERATOR_CONTAINER_PATH=${OPERATOR_CONTAINER_PATH:-".spec.install.spec.deployments[0].spec.template.spec.containers[1]"}
OPERATOR_ENV_PATH=${OPERATOR_ENV_PATH:-"$OPERATOR_CONTAINER_PATH.env[].value"}
OPERATOR_IMAGE_PATH=${OPERATOR_IMAGE_PATH:-"$OPERATOR_CONTAINER_PATH.image"}

####################
## IMAGE VARIABLES
####################
IMG_REGISTRY=${IMG_REGISTRY:-"quay.io/$ORG"}

# DEFAULT IMAGES
DEFAULT_TAG=${DEFAULT_TAG:-"2.6.0-SNAPSHOT-2022-08-08-20-23-21"}
DEFAULT_OPERATOR_TAG=${DEFAULT_OPERATOR_TAG:-"2.6"}

# SEARCH IMAGES
DEFAULT_API_IMAGE=$IMG_REGISTRY/search-v2-api:$DEFAULT_TAG
DEFAULT_COLLECTOR_IMAGE=$IMG_REGISTRY/search-collector:$DEFAULT_TAG
DEFAULT_INDEXER_IMAGE=$IMG_REGISTRY/search-indexer:$DEFAULT_TAG
DEFAULT_OPERATOR_IMAGE=$IMG_REGISTRY/search-v2-operator:${DEFAULT_OPERATOR_TAG:-DEFAULT_TAG}

# POSTGRES IMAGES
DEFAULT_POSTGRES_IMAGE=registry.redhat.io/rhel8/postgresql-13:1-56

####################
## IGNORE VARIABLES
####################
IGNORE_API_IMAGE_UPDATE=${IGNORE_API_IMAGE_UPDATE:-"false"}
IGNORE_COLLECTOR_IMAGE_UPDATE=${IGNORE_COLLECTOR_IMAGE_UPDATE:-"false"}
IGNORE_INDEXER_IMAGE_UPDATE=${IGNORE_INDEXER_IMAGE_UPDATE:-"false"}
IGNORE_OPERATOR_IMAGE_UPDATE=${IGNORE_OPERATOR_IMAGE_UPDATE:-"false"}
IGNORE_POSTGRES_IMAGE_UPDATE=${IGNORE_POSTGRES_IMAGE_UPDATE:-"true"}

####################
## FUNCTIONS/METHODS
####################

cleanup () {
    echo -e "\nRemoving $ORG/$PIPELINE_REPO repository..."
    rm -rf $PIPELINE_REPO
}

display_component_images () {
    echo -e "Component Images"
    echo -e "==============================================================================" \
    "\nPOSTGRES:\t\t${POSTGRES_IMAGE:-$DEFAULT_POSTGRES_IMAGE}" \
    "\nSEARCH_API:\t\t${API_IMAGE:-$DEFAULT_API_IMAGE}" \
    "\nSEARCH_COLLECTOR:\t${COLLECTER_IMAGE:-$DEFAULT_COLLECTOR_IMAGE}" \
    "\nSEARCH_INDEXER:\t\t${INDEXER_IMAGE:-$DEFAULT_INDEXER_IMAGE}" \
    "\nSEARCH_OPERATOR:\t${OPERATOR_IMAGE:-$DEFAULT_OPERATOR_IMAGE}" \
    "\n==============================================================================\n"
}

get_default_images_from_csv () {
  log_color purple "Fetching default component images from ${OPERATOR_CSV_FILEPATH}\n"

  for IMG in $(yq e $OPERATOR_IMAGE_PATH $OPERATOR_CSV_FILEPATH); do
    if [[ $IMG =~ .*"search-v2-operator".* ]]; then
      DEFAULT_OPERATOR_IMAGE=$IMG
    fi
  done

  for IMG in $(yq e $OPERATOR_ENV_PATH $OPERATOR_CSV_FILEPATH); do
    if [[ $IMG =~ .*"postgres".* ]]; then
      DEFAULT_POSTGRES_IMAGE=$IMG

    elif [[ $IMG =~ .*"search-collector".* ]]; then
      DEFAULT_COLLECTOR_IMAGE=$IMG

    elif [[ $IMG =~ .*"search-indexer".* ]]; then
      DEFAULT_INDEXER_IMAGE=$IMG

    elif [[ $IMG =~ .*"search-v2-api".* ]]; then
      DEFAULT_API_IMAGE=$IMG
    fi
  done

  display_component_images
}

update_images_csv () {
  COMPONENT=$1
  NEW_IMAGE=$2
  log_color purple "Preparing to update component: $COMPONENT => $NEW_IMAGE\n"

  # TODO: Replace yq path with $OPERATOR_ENV_PATH. (Note: Adding the env variable seems to cause yq to return no results)
  if [[ $COMPONENT =~ .*"search-indexer".* ]]; then
    yq -i e '.spec.install.spec.deployments[0].spec.template.spec.containers[1].env[1].value = "'$NEW_IMAGE'"' $OPERATOR_CSV_FILEPATH

  elif [[ $COMPONENT =~ .*"search-collector".* ]]; then
    yq -i e '.spec.install.spec.deployments[0].spec.template.spec.containers[1].env[2].value = "'$NEW_IMAGE'"' $OPERATOR_CSV_FILEPATH

  elif [[ $COMPONENT =~ .*"search-v2-api".* ]]; then
    yq -i e '.spec.install.spec.deployments[0].spec.template.spec.containers[1].env[3].value = "'$NEW_IMAGE'"' $OPERATOR_CSV_FILEPATH
    
  # TODO: Replace yq path with $OPERATOR_IMAGE_PATH. (Note: Adding the env variable seems to cause yq to return no results)
  elif [[ $COMPONENT =~ .*"search-v2-operator".* ]]; then
    yq -i e '.spec.install.spec.deployments[0].spec.template.spec.containers[1].image = "'$NEW_IMAGE'"' $OPERATOR_CSV_FILEPATH
  fi 
}

log_color "cyan" "Initializing search bundle image pickup..."
echo -e "Current dir: $(pwd)\n"

# Create an array containing the Search components that we will focus on for image versioning
SEARCH_COMPONENTS=(postgresql-13 search-collector search-indexer search-v2-api search-v2-operator)
get_default_images_from_csv

# Clone the pipeline repository (We need to fetch the latest manifest file to capture the latest builds)
curl -o $PIPELINE_MANIFEST_FILEPATH https://raw.githubusercontent.com/$ORG/$PIPELINE_REPO/$RELEASE_BRANCH/manifest.json -H "Authorization: token $GITHUB_TOKEN"

log_color "purple" "Fetching image-tags from $PIPELINE_MANIFEST_FILEPATH\n"

for COMPONENT in ${SEARCH_COMPONENTS[@]}; do
    # Fetch search component within the manifest file.
    MANIFEST_JSON=$(jq '.[] | select(."image-name" | match("'$COMPONENT'";"i"))' $PIPELINE_MANIFEST_FILEPATH)
    
    # Generate the base image.
    IMAGE=$IMG_REGISTRY/$COMPONENT

    # Extract the image tag.
    TAG=$(echo $MANIFEST_JSON | jq -r '."image-tag"')

    # Build the latest image tag that will be used within the bundle
    LATEST_TAG=$IMAGE:$TAG
    
    log_color "yellow" "Component: $COMPONENT"
    echo -e "Manifest Tag: $MANIFEST_JSON\n"
    log_color "cyan" "Latest Image: $LATEST_TAG"

    update_images_csv $COMPONENT $LATEST_TAG
done

display_component_images
cleanup && exit 0
