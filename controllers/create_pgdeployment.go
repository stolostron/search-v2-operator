// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *SearchReconciler) PGDeployment(instance *searchv1alpha1.Search) *appsv1.Deployment {
	deploymentName := postgresDeploymentName
	image_sha := getImageSha(deploymentName, instance)
	log.V(2).Info("Using postgres image ", "name", image_sha)
	deployment := getDeployment(deploymentName, instance)
	postgresql_shared_buffers := r.GetDBConfig(context.TODO(), instance, "POSTGRESQL_SHARED_BUFFERS")
	postgresql_effective_cache_size := r.GetDBConfig(context.TODO(), instance, "POSTGRESQL_EFFECTIVE_CACHE_SIZE")
	postgresql_work_mem := r.GetDBConfig(context.TODO(), instance, "WORK_MEM")
	postgresContainer := corev1.Container{
		Name:  deploymentName,
		Image: image_sha,
		Ports: []corev1.ContainerPort{
			{
				Name:          "search-postgres",
				ContainerPort: 5432,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			newEnvVar("POSTGRESQL_SHARED_BUFFERS", postgresql_shared_buffers),
			newEnvVar("POSTGRESQL_EFFECTIVE_CACHE_SIZE", postgresql_effective_cache_size),
			newEnvVar("WORK_MEM", postgresql_work_mem),
			newSecretEnvVar("POSTGRESQL_USER", "database-user", "search-postgres"),
			newSecretEnvVar("POSTGRESQL_PASSWORD", "database-password", "search-postgres"),
			newSecretEnvVar("POSTGRESQL_DATABASE", "database-name", "search-postgres"),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "postgresdb",
				MountPath: "/var/lib/pgsql/data",
			},
			{
				Name:      "postgresql-cfg",
				MountPath: "/opt/app-root/src/postgresql-cfg",
			},
			{
				Name:      "postgresql-start",
				MountPath: "/opt/app-root/src/postgresql-start",
			},
			{
				Name:      "search-postgres-certs",
				MountPath: "/sslcert",
			},
		},
		ReadinessProbe: &corev1.Probe{
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/usr/libexec/check-container"},
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 120,
			TimeoutSeconds:      10,
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/usr/libexec/check-container", "--live"},
				},
			},
		},
	}
	args := getContainerArgs(deploymentName, instance)
	if args != nil {
		postgresContainer.Args = args
	}
	env := getContainerEnvVar(deploymentName, instance)
	if env != nil {
		postgresContainer.Env = append(postgresContainer.Env, env...)
	}
	postgresContainer.Resources = getResourceRequirements(deploymentName, instance)
	volumes := []corev1.Volume{
		{
			Name: "postgresql-cfg",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: postgresConfigmapName,
					},
				},
			},
		},
		{
			Name: "postgresql-start",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: postgresConfigmapName,
					},
				},
			},
		},
		{
			Name: "search-postgres-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					DefaultMode: &certDefaultMode,
					SecretName:  postgresSecretName,
				},
			},
		},
	}
	postgresVolume := getPostgresVolume(instance)
	volumes = append(volumes, postgresVolume)
	postgresContainer.ImagePullPolicy = getImagePullPolicy(deploymentName, instance)
	falseVal := false
	trueVal := true
	postgresContainer.SecurityContext = &corev1.SecurityContext{
		Privileged:               &falseVal,
		AllowPrivilegeEscalation: &falseVal,
		RunAsNonRoot:             &trueVal,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
	deployment.Spec.Replicas = getReplicaCount(deploymentName, instance)

	deployment.Spec.Template.Spec.SecurityContext = getPodSecurityContext()
	deployment.Spec.Template.Spec.Containers = []corev1.Container{postgresContainer}
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
