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

func (r *SearchReconciler) createIndexerDeployment(request reconcile.Request,
	deploy *appsv1.Deployment,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	return r.createOrUpdateDeployment(context.TODO(), deploy)
	/*	found := &appsv1.Deployment{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Name:      deploy.Name,
			Namespace: request.Namespace,
		}, found)
		if err != nil && errors.IsNotFound(err) {

			err = r.Create(context.TODO(), deploy)
			if err != nil {
				log.Error(err, "Could not create %s deployment", deploy.Name)
				return &reconcile.Result{}, err
			} else {
				return nil, nil
			}
		} else if err != nil {
			return &reconcile.Result{}, err
		}

		return nil, nil
	*/
}

func (r *SearchReconciler) IndexerDeployment(instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentName := indexerDeploymentName
	image_sha := getImageSha(deploymentName, instance)
	log.V(2).Info("Using indexer image ", image_sha)

	deployment := getDeployment(deploymentName, instance)
	indexerContainer := corev1.Container{
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
				Name:      "search-indexer-certs",
				MountPath: "/sslcert",
			},
		},
		ReadinessProbe: &corev1.Probe{
			InitialDelaySeconds: 15,
			TimeoutSeconds:      30,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(3010),
					Path:   "/readiness",
					Scheme: "HTTPS",
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 20,
			TimeoutSeconds:      30,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Port:   intstr.FromInt(3010),
					Path:   "/liveness",
					Scheme: "HTTPS",
				},
			},
		},
	}
	indexerContainer.Resources = getResourceRequirements(deploymentName, instance)
	volumes := []corev1.Volume{
		{
			Name: "search-indexer-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "search-indexer-certs",
				},
			},
		},
	}
	indexerContainer.ImagePullPolicy = getImagePullPolicy(deploymentName, instance)
	deployment.Spec.Replicas = getReplicaCount(deploymentName, instance)

	deployment.Spec.Template.Spec.Containers = []corev1.Container{indexerContainer}
	deployment.Spec.Template.Spec.Volumes = volumes
	deployment.Spec.Template.Spec.ServiceAccountName = getServiceAccountName()
	deployment.Spec.Template.Spec.ImagePullSecrets = getImagePullSecret(deploymentName, instance)
	if getNodeSelector(deploymentName, instance) != nil {
		deployment.Spec.Template.Spec.NodeSelector = getNodeSelector(deploymentName, instance)
	}
	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-indexer deployment")
	}
	return deployment
}
