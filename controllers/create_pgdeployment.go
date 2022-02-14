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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *OCMSearchReconciler) createPGDeployment(request reconcile.Request,
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

func (r *OCMSearchReconciler) PGDeployment(instance *cachev1.OCMSearch) *appsv1.Deployment {

	image_sha := os.Getenv("POSTGRES_IMAGE")
	log.V(2).Info("Using postgres image ", image_sha)
	deploymentLabels := map[string]string{
		"name": "search-postgres",
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-postgres",
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
	postgresContainer := corev1.Container{
		Name:  "search-posgres",
		Image: image_sha,
		Ports: []corev1.ContainerPort{
			{
				Name:          "search-postgres",
				ContainerPort: 5432,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			newSecretEnvVar("POSTGRESQL_USER", "database-user", "search-postgres"),
			newSecretEnvVar("POSTGRESQL_PASSWORD", "database-password", "search-postgres"),
			newSecretEnvVar("POSTGRESQL_DATABASE", "database-name", "search-postgres"),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "postgresdb",
				MountPath: "/var/lib/pgsql/data",
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			},
		},
		ReadinessProbe: &corev1.Probe{
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/usr/libexec/check-container"},
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 120,
			TimeoutSeconds:      10,
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"/usr/libexec/check-container", "--live"},
				},
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "postgresdb",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	var replicas int32 = 1
	deployment.Spec.Replicas = &replicas

	deployment.Spec.Template.Spec.Containers = []corev1.Container{postgresContainer}
	deployment.Spec.Template.Spec.Volumes = volumes

	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres deployment")
	}
	return deployment
}

func newSecretEnvVar(name, key, secretName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				Key: key,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
			},
		},
	}
}
