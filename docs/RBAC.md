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

> - Use [SelfSubjectAccessReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectaccessreview-v1-authorization-k8s-io) to obtain the user's authorization rules for cluster-scoped resources.
> - Use [SelfSubjectRulesReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) to obtain the user's authorization rules for resources in a given namespace.
> - [Cache](#cache) the results and use to [Querying the database](#querying-the-database) as described in the correcponding sections of this document.

Authorized resources will be matched using these attributes:
- action - we'll only match resoces the user is authorized to `list`.
- apigroup
- kind
- namespace - only applies for namespaced scoped resources.
- name - only applies when a `resourceNames` list exists in a particular rule.
<!-- NOTE: Name was missed in the V1 implementation. -->

### 2. Managed clusters

We match ACM capabilities for access to resources in managed clusters.
As of ACM 2.5, view access is granted per managed cluster, which gives the user access to all resources in the cluster (except secrets).

> **Implementation details**
> - Use `ManagedClusterInfo` api to get all the clusters visible to the user.
> - Then we use the [SelfSubjectRulesReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) to find if the user is authorized to create the `ManagedClusterView` in each cluster namespace.
> - [Cache](#cache) the results and use to [Querying the database](#querying-the-database) as described in the correcponding sections of this document.

## Cache

It is expensive to build the user's authorization rules from the API. It requires a large number of requests because the APIs are scoped to a single resource or namespace. We must cache these results to minimize the impact on the Kubernetes API server.

Data is cached within the API pod (golang). The Kubernetes Service load balancer is configured with `SessionAffinity: ClusterIP` so requests from a given user are always sent to the same API pod instance.  This configuration eliminates the need for a shared cache.

The default time-to-live (TTL) is 10 minutes. Each incoming request from the user resets the cache expiration.

We watch the Kubernetes resources used for RBAC and invalidate the cache when any of these resources change.  Optionally we will proactively rebuild the cache for active users, but we must be careful to not create a spike to the kube API.

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


## Querying the database

Once we have the access rules for the user, we use the data to query the database.

> **DISCUSSION:** Implementation options
> 1. Use a WHERE clause.
>    - This makes all queries long and complex. 
>    - We are likely to hit limits for the query.
> 2. Build a VIEW or MATERIALIZED VIEW with all the resources visible to the user.
>    - Initial cost to build the VIEW.
>    - Additional cost to keep the VIEW updated.
>    - Additional storage or memory to store data.
> 3. Save the user's rules in a table and use a JOIN.
>    - [TODO: Need help from Sherin to understand this option.]


