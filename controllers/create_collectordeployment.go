// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *SearchReconciler) CollectorDeployment(ctx context.Context, instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentName := collectorDeploymentName
	image_sha := getImageSha(deploymentName, instance)
	log.V(2).Info("Using collector image ", "name", image_sha)

	clusterName := r.getClusterNameFromMCH(ctx)

	deployment := getDeployment(deploymentName, instance)
	collectorContainer := corev1.Container{
		Name:  deploymentName,
		Image: image_sha,
		Env: []corev1.EnvVar{
			newEnvVar("DEPLOYED_IN_HUB", "true"),
			newEnvVar("CLUSTER_NAME", clusterName),
			newEnvVar("AGGREGATOR_URL", "https://search-indexer."+instance.Namespace+".svc:3010"),
			newEnvVar("POD_NAMESPACE", instance.Namespace),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "search-indexer-certs",
				MountPath: "/sslcert",
			},
		},
		ReadinessProbe: &corev1.Probe{
			InitialDelaySeconds: 15,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(5010),
					Path:   "/readiness",
					Scheme: "HTTP",
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 20,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(5010),
					Path:   "/liveness",
					Scheme: "HTTP",
				},
			},
		},
	}
	args := getContainerArgs(deploymentName, instance)
	if args != nil {
		collectorContainer.Args = args
	}
	env := getContainerEnvVar(deploymentName, instance)
	if env != nil {
		collectorContainer.Env = append(collectorContainer.Env, env...)
	}
	collectorContainer.Resources = getResourceRequirements(deploymentName, instance)
	volumes := []corev1.Volume{
		{
			Name: "search-indexer-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: indexerSecretName,
				},
			},
		},
	}
	collectorContainer.ImagePullPolicy = getImagePullPolicy(deploymentName, instance)
	collectorContainer.SecurityContext = getContainerSecurityContext()

	deployment.Spec.Replicas = getReplicaCount(deploymentName, instance)

	deployment.Spec.Template.Spec.SecurityContext = getPodSecurityContext()
	deployment.Spec.Template.Spec.Containers = []corev1.Container{collectorContainer}
	deployment.Spec.Template.Spec.Volumes = volumes
	deployment.Spec.Template.Spec.ServiceAccountName = getServiceAccountName()
	if getNodeSelector(deploymentName, instance) != nil {
		deployment.Spec.Template.Spec.NodeSelector = getNodeSelector(deploymentName, instance)
	}
	if getTolerations(deploymentName, instance) != nil {
		deployment.Spec.Template.Spec.Tolerations = getTolerations(deploymentName, instance)
	}
	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-collector deployment")
	}
	return deployment
}

func newEnvVar(name, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}

func (r *SearchReconciler) getClusterNameFromMCH(ctx context.Context) string {
	// default name in case resource can't be found or err parsing
	clusterName := "local-cluster"
	mch := schema.GroupVersionResource{
		Group:    "operator.open-cluster-management.io",
		Version:  "v1",
		Resource: "multiclusterhubs",
	}

	// verify that MulticlusterHub operator is installed and configured.
	mchs, err := r.DynamicClient.Resource(mch).List(ctx, metav1.ListOptions{})
	if err != nil || len(mchs.Items) == 0 {
		log.Error(err, "Failed to validate dependency MulticlusterHub operator.")
		return clusterName
	} else {
		log.V(5).Info("Found MulticlusterHub instance.")
	}

	// check for local-cluster name
	if spec, ok := mchs.Items[0].Object["spec"].(map[string]interface{}); ok {
		if name, ok := spec["localClusterName"].(string); ok && name != "" {
			clusterName = name
			log.V(5).Info("Found localClusterName: " + clusterName)
		}
	}

	return clusterName
}
