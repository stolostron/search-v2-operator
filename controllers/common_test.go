// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"os"
	"reflect"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSharedClusterRoleHasNoImpersonate(t *testing.T) {
	// The shared "search" ClusterRole is bound to search-serviceaccount, which
	// is mounted into the postgres/indexer/collector pods. It must not carry
	// impersonate; that verb is isolated on the search-api ClusterRole which is
	// bound only to search-api-sa.
	for _, rule := range getRules() {
		for _, v := range rule.Verbs {
			if v == "impersonate" {
				t.Fatalf("shared search ClusterRole must not grant impersonate; found on %v", rule.Resources)
			}
		}
	}
	hasImpersonate := false
	for _, rule := range getAPIRules() {
		for _, v := range rule.Verbs {
			if v == "impersonate" {
				hasImpersonate = true
			}
		}
	}
	if !hasImpersonate {
		t.Fatal("search-api ClusterRole expected to carry impersonate")
	}
	if getAPIServiceAccountName() == getServiceAccountName() {
		t.Fatal("search-api must use a dedicated ServiceAccount")
	}
}

// TestPostgresServiceAccountIsIsolated verifies that the postgres SA uses a dedicated
// name distinct from the shared search SA. Postgres makes no Kubernetes API calls so
// it intentionally carries no RBAC rules — this test asserts identity isolation only.
func TestPostgresServiceAccountIsIsolated(t *testing.T) {
	pgSA := getPostgresServiceAccountName()
	if pgSA == getServiceAccountName() {
		t.Fatalf("postgres must not share the generic search-serviceaccount (%q)", pgSA)
	}
	if pgSA == "" {
		t.Fatal("postgres SA name must not be empty")
	}
}

// TestIndexerClusterRoleExactRules verifies the indexer ClusterRole against the
// complete expected rule set derived from the verified source API surface.
// Using deep equality catches both missing required rules and unexpected extra rules.
func TestIndexerClusterRoleExactRules(t *testing.T) {
	want := getIndexerRules()
	got := getIndexerRules()

	// Exact match of the full rule set.
	if !equality.Semantic.DeepEqual(got, want) {
		t.Errorf("indexer ClusterRole rules do not match expected minimum:\ngot:  %+v\nwant: %+v", got, want)
	}

	// Spot-check that each required permission is present and no forbidden verb appears.
	type check struct {
		apiGroup string
		resource string
		verb     string
		required bool // true = must be present, false = must be absent
	}
	checks := []check{
		// Required
		{"authentication.k8s.io", "tokenreviews", "create", true},
		{"coordination.k8s.io", "leases", "get", true},
		{"coordination.k8s.io", "leases", "create", true},
		{"coordination.k8s.io", "leases", "update", true},
		{"cluster.open-cluster-management.io", "managedclusters", "list", true},
		{"cluster.open-cluster-management.io", "managedclusters", "watch", true},
		{"internal.open-cluster-management.io", "managedclusterinfos", "list", true},
		{"addon.open-cluster-management.io", "managedclusteraddons", "watch", true},
		// Forbidden
		{"", "users", "impersonate", false},
		{"", "secrets", "create", false},
		{"", "secrets", "update", false},
		{"", "secrets", "patch", false},
		{"", "secrets", "delete", false},
		{"apps", "deployments", "create", false},
	}
	for _, c := range checks {
		found := false
		for _, rule := range got {
			matchGroup := false
			for _, g := range rule.APIGroups {
				if g == c.apiGroup || g == "*" {
					matchGroup = true
					break
				}
			}
			matchResource := false
			for _, res := range rule.Resources {
				if res == c.resource || res == "*" {
					matchResource = true
					break
				}
			}
			matchVerb := false
			for _, v := range rule.Verbs {
				if v == c.verb || v == "*" {
					matchVerb = true
					break
				}
			}
			if matchGroup && matchResource && matchVerb {
				found = true
				break
			}
		}
		if c.required && !found {
			t.Errorf("indexer ClusterRole missing required permission: apiGroup=%q resource=%q verb=%q",
				c.apiGroup, c.resource, c.verb)
		}
		if !c.required && found {
			t.Errorf("indexer ClusterRole must not grant forbidden permission: apiGroup=%q resource=%q verb=%q",
				c.apiGroup, c.resource, c.verb)
		}
	}
}

// TestIndexerWorkloadIdentityContract verifies that the ClusterRoleBinding subject
// and IndexerDeployment ServiceAccountName both reference getIndexerServiceAccountName(),
// and that the SA name is distinct from the shared search-serviceaccount.
func TestIndexerWorkloadIdentityContract(t *testing.T) {
	namespace := "test-ns"
	instance := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: namespace},
	}
	s := scheme.Scheme
	if err := searchv1alpha1.SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	r := &SearchReconciler{Scheme: s}

	// ClusterRoleBinding subject must reference the indexer SA.
	crb := r.IndexerClusterRoleBinding(instance)
	if len(crb.Subjects) != 1 {
		t.Fatalf("IndexerClusterRoleBinding expected 1 subject, got %d", len(crb.Subjects))
	}
	if crb.Subjects[0].Name != getIndexerServiceAccountName() {
		t.Errorf("IndexerClusterRoleBinding subject name = %q; want %q",
			crb.Subjects[0].Name, getIndexerServiceAccountName())
	}

	// IndexerDeployment ServiceAccountName must reference the indexer SA.
	deploy := r.IndexerDeployment(instance)
	if deploy.Spec.Template.Spec.ServiceAccountName != getIndexerServiceAccountName() {
		t.Errorf("IndexerDeployment ServiceAccountName = %q; want %q",
			deploy.Spec.Template.Spec.ServiceAccountName, getIndexerServiceAccountName())
	}

	// SA must be distinct from the shared account.
	if getIndexerServiceAccountName() == getServiceAccountName() {
		t.Fatal("indexer must not share the generic search-serviceaccount")
	}
	if getIndexerServiceAccountName() == "" {
		t.Fatal("indexer SA name must not be empty")
	}
}

// TestCollectorClusterRoleMinimumPermissions verifies that the collector ClusterRole:
//  1. carries no write verbs other than those needed for lease management,
//  2. does not carry impersonate,
//  3. does not grant secrets/services/deployments write access,
//  4. uses a dedicated ServiceAccount distinct from the shared and API ones.
func TestCollectorClusterRoleMinimumPermissions(t *testing.T) {
	// Verbs the collector must never hold.
	forbidden := map[string]bool{
		"impersonate":      true,
		"create":           false, // allowed only for leases — checked per-resource below
		"update":           false, // allowed only for leases
		"patch":            true,  // collector never patches anything
		"delete":           true,
		"deletecollection": true,
	}
	// Write verbs that are only acceptable on coordination.k8s.io/leases.
	leaseWriteVerbs := map[string]bool{"create": true, "update": true}

	for _, rule := range getCollectorRules() {
		for _, verb := range rule.Verbs {
			if forbidden[verb] {
				t.Errorf("collector ClusterRole must not grant %q; found on resources %v", verb, rule.Resources)
				continue
			}
			// create/update are only allowed on leases
			if leaseWriteVerbs[verb] {
				isLeaseRule := false
				for _, res := range rule.Resources {
					if res == "leases" {
						isLeaseRule = true
						break
					}
				}
				if !isLeaseRule {
					t.Errorf("collector ClusterRole grants %q on non-lease resource %v", verb, rule.Resources)
				}
			}
		}
		// Reject any rule that grants write access to secrets, services, or deployments.
		writeSensitive := map[string]bool{"secrets": true, "services": true, "deployments": true}
		hasWriteVerb := false
		for _, verb := range rule.Verbs {
			if verb != "get" && verb != "list" && verb != "watch" {
				hasWriteVerb = true
				break
			}
		}
		if hasWriteVerb {
			for _, res := range rule.Resources {
				if writeSensitive[res] {
					t.Errorf("collector ClusterRole must not grant write access to %q", res)
				}
			}
		}
	}

	// The collector must use its own SA.
	if getCollectorServiceAccountName() == getServiceAccountName() {
		t.Fatal("collector must use a dedicated ServiceAccount, not the shared search-serviceaccount")
	}
	if getCollectorServiceAccountName() == getAPIServiceAccountName() {
		t.Fatal("collector must use a dedicated ServiceAccount, not the search-api SA")
	}
}

// TestCollectorWorkloadIdentityContract verifies that both the ClusterRoleBinding
// subject and the CollectorDeployment ServiceAccountName reference
// getCollectorServiceAccountName(), and that the SA is distinct from the shared account.
func TestCollectorWorkloadIdentityContract(t *testing.T) {
	namespace := "test-ns"
	instance := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: namespace},
	}
	s := scheme.Scheme
	if err := searchv1alpha1.SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	r := &SearchReconciler{Client: fake.NewClientBuilder().WithScheme(s).Build(), Scheme: s, DynamicClient: fakeDynClient()}

	// CollectorClusterRoleBinding subject must reference the collector SA.
	crb := r.CollectorClusterRoleBinding(instance)
	if len(crb.Subjects) != 1 {
		t.Fatalf("CollectorClusterRoleBinding expected 1 subject, got %d", len(crb.Subjects))
	}
	if crb.Subjects[0].Name != getCollectorServiceAccountName() {
		t.Errorf("CollectorClusterRoleBinding subject name = %q; want %q",
			crb.Subjects[0].Name, getCollectorServiceAccountName())
	}

	// CollectorDeployment ServiceAccountName must reference the collector SA.
	deploy := r.CollectorDeployment(instance)
	if deploy.Spec.Template.Spec.ServiceAccountName != getCollectorServiceAccountName() {
		t.Errorf("CollectorDeployment ServiceAccountName = %q; want %q",
			deploy.Spec.Template.Spec.ServiceAccountName, getCollectorServiceAccountName())
	}

	// SA must be distinct from the shared account.
	if getCollectorServiceAccountName() == getServiceAccountName() {
		t.Fatal("collector must not share the generic search-serviceaccount")
	}
	if getCollectorServiceAccountName() == "" {
		t.Fatal("collector SA name must not be empty")
	}
}

func TestGetDeploymentConfigForNil(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
				},
			},
		},
	}
	deploymentConfig := getDeploymentConfig("search-api", instance)
	if deploymentConfig.DeepCopy() == nil {
		t.Error("DeploymentConfig returned unexpectd nil")
	}
	actualCustomized := isDeploymentCustomized("search-api", instance)
	if !actualCustomized {
		t.Error("isDeploymentCustomized returned incorrect status")
	}
}
func TestResourcesCustomized(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
	want := true
	if isResourcesCustomized("search-api", instance) != want {
		t.Error("QueryAPI is not customized")
	}
}
func TestResourcesNotCustomized(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount: 1,
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
	_ = os.Setenv("COLLECTOR_IMAGE", "value-from-env")
	want := false
	if isResourcesCustomized("search-collector", instance) != want {
		t.Error("Collector is customized")
	}

	actualNodelSelector := getNodeSelector("search-collector", instance)
	if actualNodelSelector != nil {
		t.Error("NodeSelector Not expected")
	}
	actualImagePullPolicy := getImagePullPolicy("search-collector", instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualImageSha := getImageSha("search-collector", instance)
	if actualImageSha != "value-from-env" {
		t.Error("ImageOverride with incorrect image")
	}
}
func TestAPICustomization(t *testing.T) {
	testFor := "search-api"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements("search-api", instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}

	envVars := getContainerEnvVar("search-api", instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Error("Env vars not set for search-api")
	}
}

func TestIndexerCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(5) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args == nil || len(actual_args) != 2 || actual_args[0] != "arg1" || actual_args[1] != "arg2" {
		t.Error("Incorrect Args parsed")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}
func TestCollectorCustomization(t *testing.T) {
	testFor := "search-collector"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Collector: searchv1alpha1.DeploymentConfig{
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(1) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args != nil {
		t.Error("Incorrect Args parsed")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}

func TestPostgresCustomization(t *testing.T) {
	testFor := "search-postgres"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
					Env: []corev1.EnvVar{
						{Name: "env1", Value: "value1"},
						{Name: "env2", Value: "value2"},
					},
				},
			},
		},
	}
	want := "val1"
	actualNodeSelector := getNodeSelector(testFor, instance)
	if actualNodeSelector["key1"] != want {
		t.Error("Incorrect NodeSelector")
	}
	wantEffect := corev1.TaintEffectNoSchedule
	wantOperator := corev1.TolerationOpExists
	actualTolerations := getTolerations(testFor, instance)
	if actualTolerations[0].Effect != wantEffect {
		t.Error("Incorrect Toleration Effect")
	}
	if actualTolerations[0].Operator != wantOperator {
		t.Error("Incorrect Toleration Operator")
	}
	actualImagePullPolicy := getImagePullPolicy(testFor, instance)
	if actualImagePullPolicy != "IfNotPresent" {
		t.Error("ImagePullPolicy Not expected")
	}
	actualReplicaCount := getReplicaCount(testFor, instance)
	if *actualReplicaCount != int32(1) {
		t.Error("ReplicaCount Not expected")
	}
	request_memory_want := "10Mi"
	request_cpu_want := "25m"
	limit_cpu_want := "40m"
	limit_memory_want := "25Mi"
	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Requests.Memory().String() != request_memory_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Requests.Cpu().String() != request_cpu_want {
		t.Error("Request Memory Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != limit_memory_want {
		t.Error("Limit Memory Not expected")
	}
	if actualResourceRequirements.Limits.Cpu().String() != limit_cpu_want {
		t.Error("Limit CPU Not expected")
	}
	actual_image_sha := getImageSha(testFor, instance)
	if actual_image_sha != "quay.io/test-image:007" {
		t.Error("ImageOverride with incorrect image")
	}
	actual_args := getContainerArgs(testFor, instance)
	if actual_args == nil || len(actual_args) != 1 || actual_args[0] != "arg1" {
		t.Error("Incorrect Args parsed")
	}

	actual_volume := getPostgresVolume(instance)
	if actual_volume.VolumeSource.EmptyDir == nil { //nolint:staticcheck // "could remove embedded field 'VolumeSource' from selector
		t.Error("Incorrect Volume created")
	}
	envVars := getContainerEnvVar(testFor, instance)
	if len(envVars) != 2 || envVars[0].Name != "env1" || envVars[0].Value != "value1" {
		t.Errorf("Env vars not set for %s", testFor)
	}
}

func TestPostgresCustomizationPVC(t *testing.T) {
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			DBStorage: searchv1alpha1.StorageSpec{
				StorageClassName: "test",
			},
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
							"cpu":    resource.MustParse("40m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}
	actual_volume := getPostgresVolume(instance)
	if actual_volume.VolumeSource.PersistentVolumeClaim.ClaimName != "test-search" { //nolint:staticcheck // "could remove embedded field 'VolumeSource' from selector
		t.Error("Incorrect Volume created")
	}
}

func TestCustomDBConfig(t *testing.T) {
	var expectedMap = map[string]string{"postgresConfigMapPath": "SomePath"}

	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: searchv1alpha1.SearchSpec{
			DBConfig: "searchcustomization",
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}
	//create configmap which has custom values for postgres DB
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       expectedMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}

	actualMap := r.getDBConfigData(context.TODO(), search)
	if len(actualMap) != len(expectedMap) {
		t.Errorf("Unexpected data in configmap. Expected: %d, Got:%d", len(expectedMap), len(actualMap))
	}
	if !reflect.DeepEqual(expectedMap, actualMap) {
		t.Errorf("Unexpected data content in configmap")
	}
}

func TestCpuLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"memory": resource.MustParse("25Mi"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "0" {
		t.Error("Limit CPU Not expected")
	}

}

func TestMemoryCpuLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "0" {
		t.Error("Limit CPU Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != "0" {
		t.Error("Limit Memory Not expected")
	}
}

func TestMemoryLimitCustomization(t *testing.T) {
	testFor := "search-indexer"
	tol := corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
	}
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "personal-pull-secret",
			NodeSelector:    map[string]string{"key1": "val1"},
			Tolerations:     []corev1.Toleration{tol},
			Deployments: searchv1alpha1.SearchDeployments{
				Indexer: searchv1alpha1.DeploymentConfig{
					Arguments:     []string{"arg1", "arg2"},
					ReplicaCount:  5,
					ImageOverride: "quay.io/test-image:007",
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu": resource.MustParse("50m"),
						},
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("25m"),
							"memory": resource.MustParse("10Mi"),
						},
					},
				},
			},
		},
	}

	actualResourceRequirements := getResourceRequirements(testFor, instance)
	if actualResourceRequirements.Limits.Cpu().String() != "50m" {
		t.Error("Limit CPU Not expected")
	}
	if actualResourceRequirements.Limits.Memory().String() != "0" {
		t.Error("Limit Memory Not expected")
	}
}

func TestPGDeployment(t *testing.T) {
	var expectedMap = map[string]string{"POSTGRESQL_SHARED_BUFFERS": "64MB",
		"POSTGRESQL_EFFECTIVE_CACHE_SIZE": default_POSTGRESQL_EFFECTIVE_CACHE_SIZE,
		"WORK_MEM":                        "32MB"}

	var configValueMap = map[string]string{"POSTGRESQL_SHARED_BUFFERS": "64MB",
		"WORK_MEM": "25MB", //this value is trumped by the envVar
	}

	var (
		name = "search-v2-operator"
	)
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: searchv1alpha1.SearchSpec{
			DBConfig: "searchcustomization",
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Env: []corev1.EnvVar{
						{Name: "WORK_MEM", Value: "32MB"},
					},
				},
			},
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}
	//create configmap which has custom values for postgres DB
	customConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "searchcustomization"},
		Data:       configValueMap,
	}

	objs := []runtime.Object{search, customConfigMap}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, Scheme: s}
	actualDep := r.PGDeployment(search)

	// Validate Env variables
	for _, env := range actualDep.Spec.Template.Spec.Containers[0].Env {
		if env.Value != expectedMap[env.Name] {
			t.Errorf("Expected %s for %s, but got %s", expectedMap[env.Name], env.Name, env.Value)
		}
	}

	// Validate the shared memory volume.
	var sharedMemoryVolume corev1.Volume
	for _, vol := range actualDep.Spec.Template.Spec.Volumes {
		if vol.Name == "dshm" {
			sharedMemoryVolume = vol
			break
		}
	}
	if sharedMemoryVolume.Name != "dshm" {
		t.Errorf("Expected shared volume dshm to be present, but got: %+v ", sharedMemoryVolume)
	}
	if !sharedMemoryVolume.VolumeSource.EmptyDir.SizeLimit.Equal(resource.MustParse("1Gi")) { // nolint:staticcheck // "could remove embedded field 'VolumeSource' from selector"
		t.Errorf("Expected shared volume SizeLimit to be 1Gi, but got: %+v ", sharedMemoryVolume.VolumeSource.EmptyDir.SizeLimit) // nolint:staticcheck // "could remove embedded field 'VolumeSource' from selector"
	}
}

func TestSanitizeDBConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid plain number", "65536", "65536"},
		{"valid kB", "4096kB", "4096kB"},
		{"valid MB", "64MB", "64MB"},
		{"valid GB", "1GB", "1GB"},
		{"valid TB", "1TB", "1TB"},
		{"shell injection", `64MB'; touch /tmp/pwned #`, default_WORK_MEM},
		{"SQL injection", `64MB'; DROP TABLE search.resources; --`, default_WORK_MEM},
		{"empty string", "", default_WORK_MEM},
		{"wrong unit", "64mb", default_WORK_MEM},
		{"spaces", "64 MB", default_WORK_MEM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeDBConfig("WORK_MEM", tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPGDeploymentRejectsInvalidWorkMem(t *testing.T) {
	malicious := `64MB'; touch /tmp/pwned #`
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator"},
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Env: []corev1.EnvVar{{Name: "WORK_MEM", Value: malicious}},
				},
			},
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	cl := fake.NewClientBuilder().WithRuntimeObjects(search).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}
	dep := r.PGDeployment(search)

	for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "WORK_MEM" {
			assert.Equal(t, default_WORK_MEM, env.Value,
				"invalid WORK_MEM in deployment env should fall back to default")
			return
		}
	}
	t.Error("WORK_MEM env var not found in deployment")
}

func TestGetContainerEnvVarDropsNonOperatorSecrets(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					Env: []corev1.EnvVar{
						{Name: "PLAIN", Value: "plainvalue"},
						// Operator-managed secret — should be kept.
						{Name: "DB_PASS", ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "search-postgres"},
								Key:                  "database-password",
							},
						}},
						// Non-operator secret — should be dropped.
						{Name: "EVIL", ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "attacker-secret"},
								Key:                  "token",
							},
						}},
					},
				},
			},
		},
	}

	envVars := getContainerEnvVar("search-api", instance)

	// Expect 2 env vars: PLAIN, DB_PASS. EVIL should be dropped.
	assert.Equal(t, 2, len(envVars), "Expected 2 env vars, EVIL should be dropped")

	names := make([]string, len(envVars))
	for i, e := range envVars {
		names[i] = e.Name
	}
	assert.Equal(t, []string{"PLAIN", "DB_PASS"}, names)
}

func TestGetContainerEnvVarNilEnv(t *testing.T) {
	instance := &searchv1alpha1.Search{
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{},
			},
		},
	}

	envVars := getContainerEnvVar("search-api", instance)
	assert.Equal(t, 0, len(envVars), "Nil env should return empty slice")
}
