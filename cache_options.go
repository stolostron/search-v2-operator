// Copyright Contributors to the Open Cluster Management project
package main

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newCacheOptions(watchNs string) cache.Options {
	var cacheOpts cache.Options

	if watchNs != "" {
		cacheOpts.DefaultNamespaces = map[string]cache.Config{watchNs: {}}
	}

	cacheOpts.ByObject = cacheOptionsByResourceKind()

	return cacheOpts
}

// cacheOptionsByResourceKind returns resource kinds stripped of fields not needed
// for Search reconciliation in effort to reduce memory utilization by cached resources
func cacheOptionsByResourceKind() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		&appsv1.Deployment{}: {
			Transform: func(obj any) (any, error) {
				dep, ok := obj.(*appsv1.Deployment)
				if !ok {
					return obj, nil
				}
				// controllers/common.go createOrUpdateDeployment needs:
				//   - deployment.metadata.name + deployment.metadata.namespace to get deployment
				//   - deployment.metadata.ownerReferences for to watch operator only managed deployments
				//   - deployment.metadata.resourceVersion for Kube API watch stream
				//   - deployment.spec to deep equals compare deployment diffs during reconciliation
				dep.Annotations = nil
				dep.Labels = nil
				dep.Status = appsv1.DeploymentStatus{}
				return dep, nil
			},
		},
		&clusterv1.ManagedCluster{}: {
			Transform: func(obj any) (any, error) {
				mc, ok := obj.(*clusterv1.ManagedCluster)
				if !ok {
					return obj, nil
				}
				// controllers/search_controller.go watch and isManagedHub needs:
				//   - managedcluster.metadata.name + managedcluster.metadata.namespace to reconcile managed hubs
				//   - managedcluster.metadata.resourceVersion for Kube API watch stream
				//   - managedcluster.status.clusterclaims to check if hub and is installed
				mc.Annotations = nil
				mc.Labels = nil
				mc.Spec = clusterv1.ManagedClusterSpec{}
				return mc, nil
			},
		},
		&corev1.Pod{}: {
			Transform: func(obj any) (any, error) {
				p, ok := obj.(*corev1.Pod)
				if !ok {
					return obj, nil
				}
				// controllers/search_controller.go updateStatus() and controllers/common.go searchLabels() needs:
				//   - pod.metadata.labels to check if pod is search-v2-operator component and needs reconciling
				//   - pod.metadata.name + pod.metadata.namespace to update status conditions
				//   - pod.metadata.resourceVersion for Kube API watch stream
				//   - pod.Status
				p.Annotations = nil
				p.Spec = corev1.PodSpec{}
				conds := p.Status.Conditions
				p.Status = corev1.PodStatus{Conditions: conds}
				return p, nil
			},
		},
		&corev1.Secret{}: {
			Transform: func(obj any) (any, error) {
				sec, ok := obj.(*corev1.Secret)
				if !ok {
					return obj, nil
				}
				// controllers/common.go createSecret needs:
				//   - secret.metadata.name + secret.metadata.namespace to get secret
				//   - secret.metadata.resourceVersion for Kube API watch stream
				sec.Annotations = nil
				sec.Data = nil
				sec.Labels = nil
				sec.StringData = nil
				return sec, nil
			},
		},
	}
}
