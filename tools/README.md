# Tools

This directory has scripts and other utilites to help in your Search journey

1. This [script](postgres-debug.sh) collects data from the Postgres instance to help debug issues with the RHACM search service.
1. This [script](/resource-extractor.sh) collects the number of different kubernetes resources running on a cluster. If there are `x` number of manager clusters of this size going to the connect to a ACM Hub, this data can be used to simulate loading on the search service in ACM Hub. 