// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createAPIDeployment(request reconcile.Request,
	deploy *appsv1.Deployment,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	return r.createOrUpdateDeployment(context.TODO(), deploy)
	/*found := &appsv1.Deployment{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      deploy.Name,
		Namespace: request.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), deploy)
		if err != nil {
			log.Error(err, "Could not create %s deployment", deploy.Name)
			return &reconcile.Result{}, err
		}
	}
	if err := r.Update(context.TODO(), deploy); err != nil {
		log.Error(err, "Could not update %s deployment", deploy.Name)
		return &reconcile.Result{}, err
	}
	log.V(2).Info("Created %s deployment", deploy.Name)
	return nil, nil*/
}

func (r *SearchReconciler) APIDeployment(instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentName := apiDeploymentName
	image_sha := getImageSha(deploymentName, instance)
	log.V(2).Info("Using api image ", image_sha)

	deployment := getDeployment(deploymentName, instance)
	apiContainer := corev1.Container{
		Name:  deploymentName,
		Image: image_sha,
		Env: []corev1.EnvVar{
			newSecretEnvVar("DB_USER", "database-user", "search-postgres"),
			newSecretEnvVar("DB_PASS", "database-password", "search-postgres"),
			newSecretEnvVar("DB_NAME", "database-name", "search-postgres"),
			newEnvVar("DB_HOST", "search-postgres."+instance.Namespace+".svc"),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "search-api-certs",
				MountPath: "/sslcert",
			},
		},
		ReadinessProbe: &corev1.Probe{
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(4010),
					Path:   "/readiness",
					Scheme: "HTTPS",
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 30,
			TimeoutSeconds:      1,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(4010),
					Path:   "/liveness",
					Scheme: "HTTPS",
				},
			},
		},
	}
	apiContainer.Resources = getResourceRequirements(apiDeploymentName, instance)
	volumes := []corev1.Volume{
		{
			Name: "search-api-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "search-api-certs",
				},
			},
		},
	}
	apiContainer.ImagePullPolicy = getImagePullPolicy(deploymentName, instance)
	deployment.Spec.Replicas = getReplicaCount(deploymentName, instance)

	deployment.Spec.Template.Spec.Containers = []corev1.Container{apiContainer}
	deployment.Spec.Template.Spec.Volumes = volumes
	deployment.Spec.Template.Spec.ServiceAccountName = getServiceAccountName()
	deployment.Spec.Template.Spec.ImagePullSecrets = getImagePullSecret(deploymentName, instance)
	if getNodeSelector(deploymentName, instance) != nil {
		deployment.Spec.Template.Spec.NodeSelector = getNodeSelector(deploymentName, instance)
	}

	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-api deployment")
	}
	return deployment
}
