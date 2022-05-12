// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *SearchReconciler) CollectorDeployment(instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentName := collectorDeploymentName
	image_sha := getImageSha(deploymentName, instance)
	log.V(2).Info("Using collector image ", image_sha)

	deployment := getDeployment(deploymentName, instance)
	collectorContainer := corev1.Container{
		Name:  deploymentName,
		Image: image_sha,
		Env: []corev1.EnvVar{
			newEnvVar("DEPLOYED_IN_HUB", "true"),
			newEnvVar("CLUSTER_NAME", "local-cluster"),
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
				Exec: &corev1.ExecAction{
					Command: []string{"ls"},
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 20,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"ls"},
				},
			},
		},
	}
	args := getContainerArgs(deploymentName, instance)
	if args != nil {
		collectorContainer.Args = args
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
	deployment.Spec.Replicas = getReplicaCount(deploymentName, instance)

	deployment.Spec.Template.Spec.Containers = []corev1.Container{collectorContainer}
	deployment.Spec.Template.Spec.Volumes = volumes
	deployment.Spec.Template.Spec.ServiceAccountName = getServiceAccountName()
	deployment.Spec.Template.Spec.ImagePullSecrets = getImagePullSecret(deploymentName, instance)
	if getNodeSelector(deploymentName, instance) != nil {
		deployment.Spec.Template.Spec.NodeSelector = getNodeSelector(deploymentName, instance)
	}
	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-collector deployment", err)
	}
	return deployment
}

func newEnvVar(name, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}
