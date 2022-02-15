# search-v2-operator

## Building search-v2-operator in local machine:

This step is only required if you made code changes to search-v2-operator runtime. You DO NOT need to run this steps if you only update the search components tag in search-v2-operator.clusterserviceversion.yaml . You can jump to step (3)

   ### Step 0) Download and install operator-sdk client version  >= v1.15
           https://sdk.operatorframework.io/docs/installation/

   ### Step 1) Build the operator and push to quay 
           export IMG=quay.io/<your_id>/search-v2-operator:v0.0.1
           make docker-build docker-push   

   ### Step 2) Update the operator version in bundle clusterserviceversion to use the image built in step (1)
           Update the file search-v2-operator/bundle/manifests/search-v2-operator.clusterserviceversion.yaml in the container named manager to use use the image above.

           

## Running search-v2-operator in your cluster:     

If you want to replace any PR images for any of the search components , you can update in search-v2-operator.clusterserviceversion.yaml file by replacing the tag.

   ### Step 3) Build the bundle image and push to quay
           export BUNDLE_IMG=quay.io/<your_id>/search-v2-operator-bundle:v0.0.1
           make bundle-build bundle-push 

   ### Step 4) Create the Image pull secret  to pull image from quay
           go to https://quay.io/user/<your_id>?tab=settings replacing <your_id>  with your username
           click on Generate Encrypted Password
           enter your quay.io password
           select Kubernetes Secret from left-hand menu  
           Download yaml file and rename secret as  `search-pull-secret`
           oc apply -f <your_secret.yaml>
           Verify secrets presence by running ` oc get secret | grep search-pull-secret`
           

   ### Step 5) Login to you Openshift cluster , and run the bundle
           operator-sdk run bundle $BUNDLE_IMG --pull-secret-name search-pull-secret

   ### Step 6) Verify the pods
           Review the output bundle installation is completed sucessfully
           oc get pods | grep search-v2-operator

   ### Step 7) Once the operator is installed , edit the search-v2-operator service account to include your 
           oc patch serviceaccount search-v2-operator -p '{"imagePullSecrets": [{"name": "search-pull-secret"}]}'

   ### Step 8) Apply the empty CR to create the search components
           oc apply -f config/samples/cache_v1_ocmsearch.yaml

  ### Step 9) Verify for following search pods running
              search-api
              search-collector
              search-indexer
              search-postgres
                       
                            
                  