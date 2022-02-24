// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const apiName = "search-api"

func (r *OCMSearchReconciler) createAPIDeployment(request reconcile.Request,
	deploy *appsv1.Deployment,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &appsv1.Deployment{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      deploy.Name,
		Namespace: request.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), deploy)
		if err != nil {
			return &reconcile.Result{}, err
		} else {
			return nil, nil
		}
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	return nil, nil
}

func (r *OCMSearchReconciler) APIDeployment(instance *cachev1.OCMSearch) *appsv1.Deployment {

	image_sha := os.Getenv("API_IMAGE")
	log.V(2).Info("Using api image ", image_sha)
	deploymentLabels := generateLabels("name", apiName)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiName,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
				},
			},
		},
	}
	indexerContainer := corev1.Container{
		Name:  apiName,
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
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
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
	var replicas int32 = 1
	deployment.Spec.Replicas = &replicas

	deployment.Spec.Template.Spec.Containers = []corev1.Container{indexerContainer}
	deployment.Spec.Template.Spec.Volumes = volumes
	deployment.Spec.Template.Spec.ServiceAccountName = "search-v2-operator"

	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-api deployment")
	}
	return deployment
}
