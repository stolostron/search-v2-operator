# search-v2-operator

## Installing search-v2-operator in openshift cluster

Install search-v2-operator in open-cluster-management namespace, after disabling search-operator (v1) by updating MulticlusterHub CR.

**Prerequisite** :  You will need operator sdk to install search operator bundle image. Download and install operator-sdk client version  >= v1.15

<https://sdk.operatorframework.io/docs/installation/>

#### 1. Create Image pull secret to pull images from quay (Hub cluster)

    go to https://quay.io/user/<your_id>?tab=settings replacing <your_id>  with your username
    click on Generate Encrypted Password
    enter your quay.io password
    select Kubernetes Secret from left-hand menu
    Download yaml file and rename secret as  `search-pull-secret`
    oc apply -f <your_secret.yaml>
    Verify secrets presence by running ` oc get secret | grep search-pull-secret`

#### 2. Run bundle

    operator-sdk run bundle quay.io/stolostron/search-operator-bundle:latest --pull-secret-name search-pull-secret

Wait for `OLM has successfully installed "search-v2-operator.v0.0.1"` message.
You can replace the latest tag with specific image tag from quay to test other images.

#### 3. Apply the empty CR to create the search components

    oc apply -f config/samples/search_v1alpha1_search.yaml

Check if all the search pods are running, use ACM console to search.

## Building search-v2-operator in local machine

This step is only required if you made code changes to search-v2-operator runtime. You DO NOT need to run this steps if you only update the search components tag in search-v2-operator.clusterserviceversion.yaml. You can jump to step (4)

#### 1. Download and install operator-sdk client version >= v1.15

    https://sdk.operatorframework.io/docs/installation/

#### 2. Build the operator and push to quay

    export IMG=quay.io/<your_id>/search-v2-operator:v0.0.1
    make docker-build docker-push

#### 3. Update the operator version in bundle clusterserviceversion to use the image built in step (2)

    Update the file search-v2-operator/bundle/manifests/search-v2-operator.clusterserviceversion.yaml in the container named manager to use the image above.

### **Building search-v2-operator-bundle in your cluster:**

If you want to replace any PR images for any of the search components, you can update in search-v2-operator.clusterserviceversion.yaml file by replacing the tag.

#### 4. Build the bundle image and push to quay

    export BUNDLE_IMG=quay.io/<your_id>/search-v2-operator-bundle:v0.0.1
    make bundle-build bundle-push 

#### 5. Create the Image pull secret to pull image from quay

    go to https://quay.io/user/<your_id>?tab=settings replacing <your_id> with your username
    click on Generate Encrypted Password
    enter your quay.io password
    select Kubernetes Secret from left-hand menu
    Download yaml file and rename secret as  `search-pull-secret`
    oc apply -f <your_secret.yaml>
    Verify secrets presence by running ` oc get secret | grep search-pull-secret`

#### 6. Login to you Openshift cluster, and run the bundle

    operator-sdk run bundle $BUNDLE_IMG --pull-secret-name search-pull-secret

#### 7. Verify the pods

    Review the output bundle installation is completed sucessfully
    oc get pods | grep search-v2-operator

#### 8. Apply the empty CR to create the search components

    oc apply -f config/samples/search_v1alpha1_search.yaml

#### 9. Verify for following search pods are running

    search-api
    search-collector
    search-indexer
    search-postgres
