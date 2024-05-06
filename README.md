# Search Operator

Deploys the Open Cluster Management Search v2 components.

## Installing the Search Operator in a Red Hat OpenShift cluster

### 0. Prerequisites

- You will need [Operator SDK](https://sdk.operatorframework.io/) to install search operator bundle image. [Download and install](<https://sdk.operatorframework.io/docs/installation/>) client version >= v1.15
- You'll need **Red Hat Advanced Cluster Management** v2.5 or later.

### 1. Log into the cluster using CLI

If not done already, use `oc login` to log in to the cluster by replacing `yoururl` and `yourpassword` below

```bash
oc login https://yoururl.com:6443 -u kubeadmin -p yourpassword
```

### 2. Create Image pull secret to pull images from quay (Hub cluster)

1. go to https://quay.io/user/<your_id>?tab=settings replacing <your_id>  with your username
1. click on Generate Encrypted Password
1. enter your quay.io password
1. select Kubernetes Secret from left-hand menu
1. Download yaml file and rename secret as `search-pull-secret`

### 3a. Automatic install
A script is provided which covers step 4 and 5 is provided in this repo for RHACM 2.6 version. You can launch the script and it will offer you the possibility to choose a version or use the default one. You will need to have the secret created in step 2. in the `deploy` folder.

```bash
cd ./scripts
./deploy.sh
```

### 3b. Manual install
#### Appply quay.io secret

1. Ensure to be on your `open-cluster-management` namespace and run `oc apply -f` <your_secret.yaml>
1. Verify secrets presence by running `oc get secret | grep search-pull-secret`

> **IMPORTANT**: The secret MUST be created with name as `search-pull-secret`

#### Disable search v1
- Update the MulticlusterHub CR to disable search v1.
    In the MulticlusterHub CR set `enabled: false` where `spec.overrides.components.name = search`

>    `oc patch mch multiclusterhub -n open-cluster-management --type=merge -p '{"spec":{"overrides":{"components":[{"name":"search","enabled": false}]}}}'`

After disabling search (v1), install the search operator (v2) in the open-cluster-management namespace.

#### Run bundle

```bash
operator-sdk run bundle quay.io/stolostron/search-operator-bundle@sha256:1a20394565bdc61870db1e4443d4d24d0d8eb2d65f3efdffd068cfd389b370ac --pull-secret-name search-pull-secret
```

Wait for `OLM has successfully installed "search-v2-operator.v0.0.1"` message.

> **NOTE**: If you receive an error try adding this flag. `--index-image=quay.io/operator-framework/opm:v1.23.0`  
> **TIP**: You can replace the image tag with other images in our [Quay repo](https://quay.io/repository/stolostron/search-operator-bundle?tab=tags)

#### Apply the empty CR to create the search components

```bash
oc apply -f config/samples/search_v1alpha1_search.yaml -n open-cluster-management
```

> **IMPORTANT**: The custom resource must be named  `search-v2-operator`.

### 4. Verifying search-v2 installation

> On your hub cluster, list the pods `oc get pods -n open-cluster-management | grep search`
> You should see the following pods running.

```
search-api-5884985f56-tx4kl                                       1/1     Running     
search-collector-85db8d84cc-ndtm5                                 1/1     Running 
search-indexer-65f975b8b4-bn4lf                                   1/1     Running  
search-postgres-59b96c5486-xjs8p                                  1/1     Running
search-v2-operator-controller-manager-549ff4b78b-l9qs2            2/2     Running
```

> On the managed cluster (if you have managed clusters in hub), list the pods `oc get pods -n open-cluster-management-agent-addon | grep search`
> You should see the following pod running.

```
klusterlet-addon-search-7b6645bd4-h7pxj        1/1     Running
```

### Uninstallation
Uninstalling search-v2-operator: You can uninstall the operator using the following command.

```bash
operator-sdk cleanup search-v2-operator
```

You can also use the deployer script which will also restore the search v1 and cleanup the secret
```bash
cd ./scripts
./deploy.sh -u
```

### Building search-v2-operator in local machine

This step is only required if you made code changes to search-v2-operator runtime. You DO NOT need to run this steps if you only update the search components tag in search-v2-operator.clusterserviceversion.yaml. You can jump to step (4)

#### 1. Download and install operator-sdk client version >= v1.15

```bash
https://sdk.operatorframework.io/docs/installation/
```

#### 2. Build the operator and push to quay

```bash
export IMG=quay.io/<your_id>/search-v2-operator:v0.0.1
make docker-build docker-push
```

#### 3. Update the operator version in bundle clusterserviceversion to use the image built in step (2)

```bash
Update the file search-v2-operator/bundle/manifests/search-v2-operator.clusterserviceversion.yaml in the container named manager to use the image above.
```

### **Building search-v2-operator-bundle in your cluster:**

If you want to replace any PR images for any of the search components, you can update in search-v2-operator.clusterserviceversion.yaml file by replacing the tag.

#### 4. Build the bundle image and push to quay

```bash
export BUNDLE_IMG=quay.io/<your_id>/search-v2-operator-bundle:v0.0.1
make bundle-build bundle-push
```

#### 5. Create the Image pull secret to pull image from quay

```bash
go to https://quay.io/user/<your_id>?tab=settings replacing <your_id> with your username
click on Generate Encrypted Password
enter your quay.io password
select Kubernetes Secret from left-hand menu
Download yaml file and rename secret as  `search-pull-secret`
oc apply -f <your_secret.yaml>

# Verify secrets presence by running
oc get secret | grep search-pull-secret
```

#### 6. Login to you Openshift cluster, and run the bundle

```bash
operator-sdk run bundle $BUNDLE_IMG --pull-secret-name search-pull-secret
```

#### 7. Verify the pods

```bash
# Review the output bundle installation is completed sucessfully
oc get pods | grep search-v2-operator
```

#### 8. Apply the empty CR to create the search components

```bash
oc apply -f config/samples/search_v1alpha1_search.yaml
```

#### 9. Verify for following search pods are running

```bash
search-api
search-collector
search-indexer
search-postgres
```

## Global Search Feature

**NOTE: The global search feature is tech preview as of ACM 2.11.**

Use global search in environments with the Multicluster Global Hub operator to federate the search
queries to the managed hubs.

To enable global search, add the annotation `global-search-preview=true` to the search operator instance.

```bash
oc annotate search search-v2-operator -n open-cluster-management 'global-search-preview=true'
```

Version: 0.0.2 06/17/2022
