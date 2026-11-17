// Copyright Contributors to the Open Cluster Management project
package main

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func transformFor(t *testing.T, target client.Object) toolscache.TransformFunc {
	t.Helper()

	for obj, config := range cacheOptionsByResourceKind() {
		if reflect.TypeOf(obj) == reflect.TypeOf(target) {
			if assert.NotNil(t, config.Transform, "transform for %T", target) {
				return config.Transform
			}
			return nil
		}
	}

	assert.FailNow(t, "no transform registered for %T", target)
	return nil
}

func TestNewCacheOptions(t *testing.T) {
	for _, watchNamespace := range []string{"", "search"} {
		opts := newCacheOptions(watchNamespace)

		for _, kind := range []client.Object{
			&appsv1.Deployment{},
			&clusterv1.ManagedCluster{},
			&corev1.Pod{},
			&corev1.Secret{},
		} {
			transformFor(t, kind)
		}

		if watchNamespace == "" {
			assert.Empty(t, opts.DefaultNamespaces)
			continue
		}

		assert.Len(t, opts.DefaultNamespaces, 1)
		assert.Contains(t, opts.DefaultNamespaces, watchNamespace)
	}
}

func TestDeploymentCacheTransform(t *testing.T) {
	in := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "search-api",
			Namespace:       "search",
			ResourceVersion: "42",
			Labels:          map[string]string{"app": "search"},
			Annotations:     map[string]string{"example": "value"},
			OwnerReferences: []metav1.OwnerReference{{Name: "search"}},
		},
		Spec:   appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
		Status: appsv1.DeploymentStatus{Replicas: 2},
	}

	got, err := transformFor(t, &appsv1.Deployment{})(in)
	assert.NoError(t, err)

	out, ok := got.(*appsv1.Deployment)
	if !assert.True(t, ok, "expected *Deployment, got %T", got) {
		return
	}

	assert.Equal(t, in.Name, out.Name)
	assert.Equal(t, in.Namespace, out.Namespace)
	assert.Equal(t, in.ResourceVersion, out.ResourceVersion)
	assert.Equal(t, in.OwnerReferences, out.OwnerReferences)
	assert.Equal(t, in.Spec, out.Spec)
	assert.Nil(t, out.Labels)
	assert.Nil(t, out.Annotations)
	assert.Equal(t, appsv1.DeploymentStatus{}, out.Status)
}

func TestManagedClusterCacheTransform(t *testing.T) {
	in := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "hub",
			ResourceVersion: "42",
			Labels:          map[string]string{"example": "value"},
			Annotations:     map[string]string{"example": "value"},
		},
		Spec: clusterv1.ManagedClusterSpec{HubAcceptsClient: true},
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{{
				Name:  "hub.open-cluster-management.io",
				Value: "true",
			}},
		},
	}

	got, err := transformFor(t, &clusterv1.ManagedCluster{})(in)
	assert.NoError(t, err)

	out, ok := got.(*clusterv1.ManagedCluster)
	if !assert.True(t, ok, "expected *ManagedCluster, got %T", got) {
		return
	}

	assert.Equal(t, in.Name, out.Name)
	assert.Equal(t, in.ResourceVersion, out.ResourceVersion)
	assert.Equal(t, in.Status.ClusterClaims, out.Status.ClusterClaims)
	assert.Equal(t, clusterv1.ManagedClusterSpec{}, out.Spec)
	assert.Nil(t, out.Labels)
	assert.Nil(t, out.Annotations)
}

func TestPodCacheTransform(t *testing.T) {
	readyConditions := []corev1.PodCondition{{
		Type:               corev1.PodReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ContainersNotReady",
		Message:            "container is not ready",
	}}
	in := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "search-api-123",
			Namespace:       "search",
			ResourceVersion: "42",
			Labels: map[string]string{
				"component": "search-v2-operator",
				"app":       "search",
				"name":      "search-api",
			},
			Annotations: map[string]string{"example": "value"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "api"}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: readyConditions,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "api", Ready: false,
			}},
		},
	}

	got, err := transformFor(t, &corev1.Pod{})(in)
	assert.NoError(t, err)

	out, ok := got.(*corev1.Pod)
	if !assert.True(t, ok, "expected *Pod, got %T", got) {
		return
	}

	assert.Equal(t, in.Name, out.Name)
	assert.Equal(t, in.Namespace, out.Namespace)
	assert.Equal(t, in.ResourceVersion, out.ResourceVersion)
	assert.Equal(t, "search-v2-operator", out.Labels["component"])
	assert.Equal(t, "search", out.Labels["app"])
	assert.Equal(t, "search-api", out.Labels["name"])
	assert.Nil(t, out.Annotations)
	assert.Equal(t, corev1.PodSpec{}, out.Spec)
	assert.Equal(t, corev1.PodStatus{Conditions: readyConditions}, out.Status)
}

func TestSecretCacheTransform(t *testing.T) {
	in := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "search-secret",
			Namespace:       "search",
			ResourceVersion: "42",
			Labels:          map[string]string{"example": "value"},
			Annotations:     map[string]string{"example": "value"},
			OwnerReferences: []metav1.OwnerReference{{Name: "search"}},
		},
		Data:       map[string][]byte{"password": []byte("secret")},
		StringData: map[string]string{"password": "secret"},
	}

	got, err := transformFor(t, &corev1.Secret{})(in)
	assert.NoError(t, err)

	out, ok := got.(*corev1.Secret)
	if !assert.True(t, ok, "expected *Secret, got %T", got) {
		return
	}

	assert.Equal(t, in.Name, out.Name)
	assert.Equal(t, in.Namespace, out.Namespace)
	assert.Equal(t, in.ResourceVersion, out.ResourceVersion)
	assert.Equal(t, in.OwnerReferences, out.OwnerReferences)
	assert.Nil(t, out.Data)
	assert.Nil(t, out.StringData)
	assert.Nil(t, out.Labels)
	assert.Nil(t, out.Annotations)
}

func TestCacheTransformsReturnOriginalObjectForUnexpectedType(t *testing.T) {
	unexpected := &corev1.ConfigMap{}

	for _, kind := range []client.Object{
		&appsv1.Deployment{},
		&clusterv1.ManagedCluster{},
		&corev1.Pod{},
		&corev1.Secret{},
	} {
		t.Run(reflect.TypeOf(kind).String(), func(t *testing.T) {
			got, err := transformFor(t, kind)(unexpected)
			assert.NoError(t, err)
			assert.Same(t, unexpected, got)
		})
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
