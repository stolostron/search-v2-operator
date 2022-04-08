# Role Based Access Control (RBAC)

### Status: DRAFT (April 7, 2022)
**This feature is not fully implemented. This document describes the desired imlpementation for Odessey (search v2).**

The search service collects data using a service account with wide cluster access and stores all resources in the database. The API must enforce that results for each user (or service account) only contain resources that they are authorized to access.

## Access to the Search API
<!-- This feature is new for V2 -->
The API itself is protected by RBAC. Users must be given a role that allows access to search.

The default ACM admin and viewer roles should include access to the search API by default. [TODO: Describe the roles and what needs to be added.]

> **DISCUSSION:** Implementation options.
> 1. Kube API server extension? This can add additional load to the kube API server.
> 2. Validation at the service? 

## Enforcing RBAC on results

The API authenticates the user (or service account) and impersonates the user to obtain their access rules.

> Use the [TokenReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#tokenreview-v1-authentication-k8s-io) to validate the user token and obtain the UserInfo (username and groups).
> 
> **DISCUSSION:** 
> Use [User Impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation) when sending API requests on behalf of the end user.

After authenticating the user, we'll obtain their authorization rules. We have to cover 2 different scenarios:
    1. [Resources in the hub cluster](#hub-cluster)
    2. [Resources in managed clusters](#managed-clusters)

### 1. Hub cluster

Users must see **exactly** the same resources they are able to list using kubectl, oc cli, or the kubernetes API on the OpenShift cluster hosting the ACM Hub.

Collect all the authorization rules for the user and [cache](#cache) the results.
> 1. Get all resources available in the cluster. [Can this be shared across all users?]
>       - CLI: `oc api-resources`
>       - API: See with `oc api-resources -v=6`
> 2. For each cluster-scoped (namespace == false) resource, check if user has permission to list.
>       - CLI: `oc auth can-i list <resource> --as=<user>`
>       - API: [SelfSubjectAccessReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectaccessreview-v1-authorization-k8s-io) 
> 3. Get all namespaces (projects) for the user.
>       - CLI: `oc get namespaces --as=<user>`
>       - API: [NamespaceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#namespacelist-v1-core)
> 4. For each namespace, obtain the user's authorization rules.
>       - CLI: There isn't an equicalent command, the closest is `oc auth can-i list <resource> -n <ns> --as=<user>`
>       - API: [SelfSubjectRulesReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io)

Resources are matched to the authorization rules using these attributes:
- action - we'll only match the `list` action.
- apigroup
- kind
- namespace - only applies for namespaced scoped resources.
- name - only applies when a `resourceNames` list exists in a particular rule.
<!-- NOTE: Name was missed in the V1 implementation. -->

Finally, we use these rules to [query the database](#query-the-database)

### 2. Managed clusters
<!-- This implementation is different from V1. -->
We match ACM capabilities for access to resources in managed clusters.
As of ACM 2.5, view access is granted per managed cluster, which gives the user access to all resources in the cluster (except secrets).

Find the managed clusters the user is authorized to view and [cache](#cache) the results.
> 1. Get all the namespaces associated with a managed cluster. We do this once for all users.
>       - CLI: `oc get ManagedCluster`
> 2. Get all namespaces (projects) for the user. (We already have this data.)
>       - CLI: `oc get namespaces --as=<user>`
> 3. Build a list of all the clusters that the user has access.
> 4. For each managed cluster, check if the user has permission to view resources.
>       - CLI: `oc auth can-i create ManagedClusterView --as=<user>`
>       - API: [SelfSubjectRulesReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) (We already have this data.)

Use the list of managed clusters to [query the database](#query-the-database).

## Cache

Building the user's authorization rules requires a lot of API requests because these APIs are scoped to a single resource or namespace. We must cache the results to minimize the impact on the Kubernetes API server.

Data is cached within the API pod (golang). The Kubernetes Service load balancer is configured with `SessionAffinity: ClusterIP` to route requests from a given user to the same API pod instance. This configuration eliminates the need for a shared cache across pods.

The default time-to-live (TTL) is 10 minutes. Each incoming request from the user resets the cache expiration.

We watch the Kubernetes resources with the RBAC definitions and invalidate the cache when any of these resources change.
[Optionally, we will proactively rebuild the cache for active users, but we must be careful to not create a spike to the kube API.]

- **Namespace** 
    - deleted - all caches can be updated without additional API requests.
    - created - requires 1 API call per active user. 
- **Role** 
    - rebuild if `list` verb is added or removed.
    - **[Is Role always scoped to a single namespace?]** If yes, only rebuild rules for the affected namespace.
- **ClusterRole**
    - rebuild if `list` verb is added or removed.
    - **[Is ClusterRole always scoped to cluster-scoped resources?]** If yes, only rebuild the cluster-scoped RBAC.
- **RoleBinding**
    - **[Is RoleBinding always scoped to a single namespace?]** If yes, only rebuild rules for the namespace.
- **ClusterRoleBinding**
    - **[Is ClusterRoleBinding always scoped to cluster resources].**
- **Groups**
    - If an active user is added or removed from a group, invalidate the cache for those users only.


## Query the database

Once we have the access rules for the user, we use the data to query the database.

> **DISCUSSION:** Implementation options
> 1. Append a WHERE clause to every query.
>    - This makes all queries long and complex. 
>    - We are likely to hit limits for the query.
> 2. Build a VIEW or MATERIALIZED VIEW with all the resources visible to the user.
>    - Initial cost to build the VIEW.
>    - Additional cost to keep the VIEW updated.
>    - Additional storage or memory to store data.
> 3. Save the user's rules in a table and use a JOIN.
>    - [TODO: Sherin needs to explain this option.]

