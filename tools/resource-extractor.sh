#!/bin/bash

# HOW TO RUN THIS SCRIPT:
# Log into the Managed Cluster with "oc login --server=''  --token='' "
# ./resource-extractor.sh

# DESCRIPTION:
# This script collects the number of different kubernetes resources running on a cluster.

kinds=( $(oc api-resources --no-headers --verbs='watch','list' | awk '{print $1;}') )
declare  kinds >> /dev/null
for i in "${kinds[@]}"
do
    echo "$(oc get ${i} --all-namespaces --ignore-not-found | wc -l) - ${i}"
done