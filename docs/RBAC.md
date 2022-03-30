# Role Based Access Control (RBAC)

**This document describes the desired imlpementation for Odessey (search v2) and it must be reviewed after completing the implementation.**

The API must enforce that results for each user only contain resources that they are authorized to access. The search service collects and index data using a service account with wide cluster access. The database contains all resources, when querying from the API we must filter the results to match the access of the user (or service account) making the request.

## Access to the search API
<!-- NOTE this feature is new for V2 -->
The API itself is protected by Kubernetes. Users must be given a role that allows access to search.
[What are the default ACM roles? acm-viewer or acm-admin]

## Enforcing RBAC on results

The API will authenticate the user (or service account) and will impersonate the user to obtain their access rules.

RBAC is enforced differently for resources in the hub vs. managed clusters. Let's discuss both senarios separately.

### A. RBAC for resources in the Hub cluster

Users must see **exactly** the same resources they would see when using kubectl, oc cli, or the kubernetes API on the OpenShift cluster hosting the ACM Hub. 

**Implementation details:**
>Use [SelfSubjectAccessReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectaccessreview-v1-authorization-k8s-io) to obtain the user's authorization rules for cluster-scoped resources.
> 
> Use [SelfSubjectRulesReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) to obtain the user's authorization rules for resources in a given namespace.
>
> We must cache the results from these APIs to minimize the impact to the kubernetes api server and improve performance.
>
> - The authorization list is expensive to build from the APIs. It requires a large number of requests because the APIs are scoped to a single resource or namespace. The cache helps to minimize the impact of these requests.
> - Cached data must live for a short period of time. The default time-to-live (TTL) is 10 minutes after the last user activity from. We reset the TTL with each incoming request from the user.
> - Watch the kubernetes base resources for changes that could change authorization and invalidate the cache. If these resources change, we will update the cached rules for the active users. We use a service account with the right authorization to get these resources.
>   - Namespace - add or delete rules for the affected namespace.
>   - Role - only rebuild if `list` verb is added or removed. **Is Role always scoped to a single namespace?** If yes, only rebuild rules for the namespace.
>   - ClusterRole - only rebuild if `list` verb is added or removed. **Is thic always scoped to cluster-scoped resources?** If yes, only rebuild the cluster-scoped RBAC.
>   - RoleBinding - **Are these always scoped to a single namespace?** If yes, only rebuild rules for the namespace.
>   - ClusterRoleBinding - **Check scope of these.**
>   

### B. RBAC for resources in the managed clusters

We will match ACM capabilities for access to resources in managed clusters.
As of ACM 2.5, access is granted per managed cluster which gives the user access to all resources in the cluster (except secrets).

**Implementation details:**
> Use `ManagedClusterInfo` api to get all the clusters visible to the user.
>
> Then we will use the [SelfSubjectRulesReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) to find if the user is authorized to create the `ManagedClusterView` in the cluster's namespace.
>
> Cache the results from the API as described in the previous section.
>



