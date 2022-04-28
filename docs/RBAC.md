# Role Based Access Control (RBAC)

### Status: DRAFT (April 22, 2022)
**This feature is not fully implemented. This document describes the desired imlpementation for Odyssey (search v2).**

The search service collects data using a service account with wide cluster access and stores all resources in the database. The Search API must enforce that results for each user (or service account) only contain resources that they are authorized to access.

## [Swimlane diagram (link)](https://swimlanes.io/#tVZNb9tGEL3zVwyQiy2YlJ0URSE4BvwBpDokaGUnORgBvCaH0tbULrO7lOC0/e99u0uKpOwYOaSAQYvc+Xwzb2addBXP6JqFyVd0/secFhfnl0ny0bKh9GxwsBNa8NeGrUuStzOaTE4yOm/cipWTuXBMDRQnkyRR2sHugp2RvGGCBDn9wIpKo9fh1UQ7tGJRwJk2lGv9IDmjJBnEgyAuRQ75g0rnojqc0SdRycL72hnNOn+fmfIg3PvbRGmp4Ro+BNmVNo5qNlIXdPDrMVnOtSrsIRQQVNHkCOHG61oShlv5Sm64IKtpy7RuELbhTRfHWkNMlw6B0M1KWnJyzanTqVfqPOGzVAXXjIdyEA8xhmgLMk3F9oi2K4msIVlptQQmwtHJMa2lahxbryKVyJ3cSPeIlGU5o3nZpon8u1RhT6pgvRYItM8wI0A7ApNSwPvQ3HMqajl7WskO1x5y7+yIluzCORyV+oiEKghW8wcSsKCN/BYBB6CQgntfTUQ8odsA7II3kre+vF8OVs7Vdjad+iiMYiSaST0tdG6nhks2rHKeLlmx8ZkNxHzI081J9vrN9FUIywSr6eYkFX0eCCN9+M2mUh96976v54j5J/r2OHgYXnS8AzCiZndAoVgtSr5gdtf3WYJGeVKtMSGH5Sr6eozpAx2f9Iw+qoHPg1+OTw5bCr/O6J2AJbNXvdCUgTQdqV+k5WVogEi/Tqm1gcxF37B7B6F1m9rzJSCE3g1Gsr3kT0e92k6hgbHYqwOAjgadfTtfg4dWq64GP1h8kedsbairCs9v03GNY/1TuTOPb2GSLCOkeEgThmqMMrCghdvjaNjqxsAJ0Z3OCcGm/acU40YWTP/Q0nBNW+GQWftSSevuelu10X9x7qAT7PSvqbBvT32MZ1769pqr8rq594fnIbfIxZ/EBQvrNlqPyI05ueutjhmhUfIK8xQY2lzXfhZ26WMRjANeeAD/r3hDdX4kXCXWbGuRj0JNIl0HBDlN9wmyGPFpn9u+vZ9w6u9SWPRDzdgEpUir5b/9JAkkeannn1G+6VaOH0HYCQ6rRTe2ehzvDoE/+6jyldEKx76bfDGzJKW52oxGmYuLMgKHsYYtaIYr5xmVSP0o+f31ltLn0O49G9wKieS6qQoolyhZpJVfPrJ3EY+QR8wzjh6wYqEr7v5fYBNLtfSvl7H5utPB60DondFN7X986GofZBdX7QR9k9GfDZvHMMO9x3th2Q/MVzTqiKdDc4HLT5sB3NYGe7Udu/6a9J0SX7UeZsl8p+U9D9GFya9PQspAqPPL9/R7c9/jOsPkuQuzBy0PlyqVYbjQaSdyNpwidJsqOlX27Au0YO+9UGLp4Y7IvWw3N+xr1Oq0YH/ydwFvtDXhQR65hBnqbiKTyc3F1WwyoQ8cV6coingBK9gJWSH1e93gVqm3/qq2lVX1DBCAG858ecPn0EelrOA82yPxWY/2MzXeX4lx0y7QI2h+OHehf5vKDXmf/Ac=)

![](https://static.swimlanes.io/2ee006a19f5dee69ece614e95f72d480.png)


## Protect the Search API
<!-- [NEW IN V2] This feature is new for V2. -->
The API itself is protected by RBAC. Users must be given a role that allows access to search.

The default ACM admin and viewer roles should include access to the Search API by default.

> **OPEN DISCUSSION:**
>
> We are still investigating the implementation of this feature. We'll update as we learn more.
>
> Options:
> 1. Kube API server extension - This can add additional load to the kube API server.
> 2. Management Ingress - This is being deprecated.
> 3. Validate access at the service - Provides the most flexibility, but how will it leverage kubernetes implementation?

## Authenticate the user 

The Search API uses the Service Account bind to the pod (search-serviceaccount) to authenticate the user (or service account) that makes the request, then impersonates the user to obtain their access rules.
We cache the token validation for a short period (default: 60 seconds) to reduce requests to the kube api. Tokens have a short expiration, so we must revalidate often. **Note that** this time-to-live period is independent of the cached rules, which is longer at 10 minutes of inactivity.

> Use the [TokenReview API](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#tokenreview-v1-authentication-k8s-io) to validate the user token and obtain the UserInfo (username and groups).

## Obtain the user's RBAC rules
After authenticating the user, we obtain their authorization rules. There's 2 different scenarios:
1. [Rules for resources in the hub cluster](#hub-cluster)
2. [Rules for resources in managed clusters](#managed-clusters)

> Use [User Impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation) when sending API requests on behalf of the end user.
```
CHANGE IN V2. [Does not affect API results.]
V1 passes the user's token directly to the API, but impersonation is desired.
This change doesn't change the semantic of the API.
```

### 1. Hub cluster

The search results must include **exactly** the same resources users are authorized to list using kubectl, oc, or the kubernetes API on the OpenShift cluster hosting the ACM Hub.

We request all the authorization rules for the user and [cache](#cache) the results.
> 1. Get all resources in the cluster that support `list` and `watch`. This is shared across all users.
>       - CLI: `oc api-resources -o wide | grep watch | grep list`
>       - API: See with `oc api-resources -o wide -v=6`
> 2. For each cluster-scoped resource (namespaced == false), check if user has permission to list.
>       - CLI: `oc auth can-i list <resource> --as=<user>`
>       - API: [SelfSubjectAccessReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectaccessreview-v1-authorization-k8s-io) 
> 3. Get all projects (namespaces) for the user. **Note:** Project list gets filtered by user's access but namespace list won't, so we use projects.
>       - CLI: `oc projects --as=<user>`
>       - API: [ProjectList](https://docs.okd.io/3.9/rest_api/apis-project.openshift.io/v1.Project.html#Get-apis-project.openshift.io-v1-projects)
> 4. For each namespace, obtain the user's authorization rules.
>       - CLI: `oc auth can-i --list -n <ns> --as=<user> | grep '\[\*\] \| list'`
>       - API: [SelfSubjectRulesReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io)

Resources are matched to the authorization rules using these attributes:
- action - we'll only match the `list` action.
- apigroup
- kind
- namespace - only applies for namespace-scoped resources.
- name - when a `resourceNames` list exists in a rule, otherwise defaults to all names.

```
CHANGES IN V2. [Will affect API results.]
1. V1 uses the `get` action, however `list` aligns much better with the search functionality.
2. V1 did't account for resourceNames. V2 addresses this gap.
```

Finally, we use these rules to [query the database.](#query-the-database)

### 2. Managed clusters

We match ACM capabilities for access to resources in managed clusters.
As of ACM 2.5, view access is granted per managed cluster, which gives the user access to all resources in the cluster (except secrets).

```
CHANGE IN V2. [Will affect API results.]
V1 attempts to map the RBAC rules from the hub's namespace to the managed cluster. ACM's
implementation has drifted as we learned the limitations, this change realigns search with ACM.
```

Find the managed clusters that the user is authorized to view and [cache](#cache) the results.
> 1. Get all the namespaces associated with a managed cluster. We do this once for all users.
>       - CLI: `oc get ManagedClusters`
> 2. Get all projects (namespaces) for the user. (We already have this data.)
>       - CLI: `oc projects --as=<user>`
>       - API: [ProjectList](https://docs.okd.io/3.9/rest_api/apis-project.openshift.io/v1.Project.html#Get-apis-project.openshift.io-v1-projects)
> 3. Build a list of all the managed clusters visible to the user.
>       - UNION of results from steps 1 and 2.
> 4. For each managed cluster, check if the user has permission to view resources.
>       - CLI: `oc auth can-i create ManagedClusterView -n <managedClusterName> --as=<user>`
>       - API: [SelfSubjectRulesReview](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#selfsubjectrulesreview-v1-authorization-k8s-io) (We already have this data.)

Use the list of managed clusters to [query the database](#query-the-database).

## Cache

Building the user's authorization rules requires a lot of API requests because these APIs are scoped to a single resource or namespace. We must cache the results to minimize the impact on the Kubernetes API server.

Data is cached within the API pod (golang). The Kubernetes Service load balancer is configured with `SessionAffinity: ClusterIP` to route requests from a given user to the same API pod instance. This configuration eliminates the need for a shared cache across pods.

The default time-to-live (TTL) is 10 minutes. Each incoming request from the user resets the cache expiration.

We watch the Kubernetes resources with the RBAC definitions and invalidate the cache when any of these resources change. Invalidated caches will get rebuilt on the next incoming request for each user. This adds a delay to the request, but reduces the pressure on the Kube API server. 

- **Role** 
    - Invalidate cached rules for the affected namespace. **[TODO: Confirm that Role is always scoped to a single namespace.]** 
    - Optimization: Check if the affected rules contain the verb `*` or `list`. We can ignore other changes.
- **ClusterRole**
    - Invalidate all cached rules for all users.
    - Optimization: Check if the affected rules contain the verb `*` or `list`. We can ignore other changes.
- **RoleBinding**
    - Invalidate cached rules for the affected namespace. **[TODO: Confirm that RoleBinding is always scoped to a single namespace.]** 
- **ClusterRoleBinding**
    - Invalidate all cached rules for all users.
- **Groups**
    - Invalidate cached rules only for users added or removed from a group.
- **Namespace** 
    - deleted - No impact to results, but should remove from user caches for efficiency.
    - created - requires 1 API request per active user. 
- **CRD**
    - Cluster-scoped CRD
        - crd deleted - No impact to results, but should remove from user caches for efficiency.
        - crd created - requires 1 API call per active user.
    - Namespace-scoped CRD
        - crd deleted - No impact to results, but should remove from user caches for efficiency.
        - crd created - requires several API calls (active users * namespaces).


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
