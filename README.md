# search-v2-operator

Building search-v2-operator in local machine:

   ### Step 0) Download and install operator-sdk client version  >= v1.15
           https://sdk.operatorframework.io/docs/installation/

   ### Step 1) Build the operator and push to quay 
           export IMG=quay.io/<your_id>/search-v2-operator:v0.0.1
           make docker-build docker-push   

   ### Step 2) Update the operator version in bundle clusterserviceversion to use the image built in step (1)
           Update the file search-v2-operator/bundle/manifests/search-v2-operator.clusterserviceversion.yaml in the container named manager to use use the image above.

   ### Step 3) Build the bundle image and push to quay
           export BUNDLE_IMG=quay.io/<your_id>/search-v2-operator-bundle:v0.0.1
           make bundle-build bundle-push 

   ### Step 4) Create the Image pull secret  to pull image from quay
           go to https://quay.io/user/<your_id>?tab=settings replacing tpouyer with your username
           click on Generate Encrypted Password
           enter your quay.io password
           select Kubernetes Secret from left-hand menu  
           Name the secret as `search-pull-secret`

   ### Step 5) Login to you Openshift cluster , and run the bundle
           operator-sdk run bundle $BUNDLE_IMG 

   ### Step 6) Once the operator is installed , edit the search-v2-operator service account to include your ImagePullSecret

   ### Step 7) Apply the empty CR to create the search components
           oc apply -f search-v2-operator/config/samples/cache_v1_ocmsearch.yaml
                            
                  