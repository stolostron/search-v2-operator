package addon

import (
	"context"
	"errors"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"slices"
	"testing"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var (
	scheme       = runtime.NewScheme()
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = prometheusv1.AddToScheme(scheme)
	_ = searchv1alpha1.AddToScheme(scheme)
}

func newCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newClusterWithImageRegistries(name, imageRegistriesJSON string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				clusterv1.ClusterImageRegistriesAnnotationKey: imageRegistriesJSON,
			},
		},
	}
}

func newAddon(name, cluster, installNamespace string, annotationValues map[string]string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster,
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: installNamespace,
		},
	}
	addon.SetAnnotations(annotationValues)
	return addon
}

func newAgentAddon(t *testing.T, objects []runtime.Object) agent.AgentAddon {
	registrationOption := newRegistrationOption(nil, SearchAddonName)
	fakeAddonClient := fakeaddon.NewSimpleClientset(objects...)
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithScheme(scheme).
		WithGetValuesFuncs(
			getValue,
			addonfactory.GetValuesFromAddonAnnotation,
			addonfactory.GetAddOnDeploymentConfigValues(
				utils.NewAddOnDeploymentConfigGetter(fakeAddonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnResourceRequirementsValues,
			),
			validateImageOverride,
			addonfactory.GetAgentImageValues(
				utils.NewAddOnDeploymentConfigGetter(fakeAddonClient),
				"global.imageOverrides.search_collector",
				SearchCollectorImage,
			),
		).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		t.Fatalf("failed to build agent %v", err)
	}
	return agentAddon
}

// newSearchCR returns a minimal Search CR named searchInstanceName in the given namespace — the
// only field getCollectorConfigValue/resolveSearchNamespace actually need.
func newSearchCR(namespace string) *searchv1alpha1.Search {
	return &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      searchInstanceName,
			Namespace: namespace,
		},
	}
}

// newMergedCollectorConfig returns a CollectorConfig CR named mergedCollectorConfigName in the
// given namespace, matching the shape of the hub's operator-computed merged-collector-config.
func newMergedCollectorConfig(
	namespace string, spec searchv1alpha1.CollectorConfigSpec,
) *searchv1alpha1.CollectorConfig {
	return &searchv1alpha1.CollectorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mergedCollectorConfigName,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

// newAgentAddonWithHubClient is a variant of newAgentAddon that also wires getCollectorConfigValue
// into the values chain, using the given fake hub client, so tests can exercise the
// merged-collector-config distribution path end to end (through real Helm rendering).
func newAgentAddonWithHubClient(t *testing.T, objects []runtime.Object, hubClient client.Client) agent.AgentAddon {
	registrationOption := newRegistrationOption(nil, SearchAddonName)
	fakeAddonClient := fakeaddon.NewSimpleClientset(objects...)
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithScheme(scheme).
		WithGetValuesFuncs(
			getValue,
			getCollectorConfigValue(hubClient),
			stripCollectorConfigFromAnnotation,
			addonfactory.GetAddOnDeploymentConfigValues(
				utils.NewAddOnDeploymentConfigGetter(fakeAddonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnResourceRequirementsValues,
			),
			validateImageOverride,
			addonfactory.GetAgentImageValues(
				utils.NewAddOnDeploymentConfigGetter(fakeAddonClient),
				"global.imageOverrides.search_collector",
				SearchCollectorImage,
			),
		).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		t.Fatalf("failed to build agent %v", err)
	}
	return agentAddon
}

// findCollectorConfig returns the rendered merged-collector-config CollectorConfig object among
// objs, or nil if it was not rendered. Relies on searchv1alpha1 being registered in `scheme`
// (see init()) so the addon-framework's decoder produces a typed object here, the same way
// findSearchDeployment relies on appsv1 being registered.
func findCollectorConfig(objs []runtime.Object) *searchv1alpha1.CollectorConfig {
	for _, obj := range objs {
		if cc, ok := obj.(*searchv1alpha1.CollectorConfig); ok {
			return cc
		}
	}
	return nil
}

func TestManifest(t *testing.T) {
	annotationsTest := map[string]string{"addon.open-cluster-management.io/values": `{"global":{"nodeSelector":{"node-role.kubernetes.io/infra":""},"imageOverrides":
	{"search_collector":"quay.io/test/search_collector:test"}}}`,
		"addon.open-cluster-management.io/search_memory_limit":    "2000Mi",
		"addon.open-cluster-management.io/search_memory_request":  "1000Mi",
		"addon.open-cluster-management.io/search_rediscover_rate": "4000",
		"addon.open-cluster-management.io/search_heartbeat":       "3000",
		"addon.open-cluster-management.io/search_report_rate":     "2000",
		"addon.open-cluster-management.io/search_args":            "--v=2"}
	annotations250 := map[string]string{"addon.open-cluster-management.io/values": "",
		"addon.open-cluster-management.io/search_memory_limit":    "2000Mi",
		"addon.open-cluster-management.io/search_memory_request":  "1000Mi",
		"addon.open-cluster-management.io/search_rediscover_rate": "4000",
		"addon.open-cluster-management.io/search_args":            "--v=2",
		"addon.open-cluster-management.io/search_heartbeat":       "3000",
		"addon.open-cluster-management.io/search_report_rate":     "2000"}
	tests := []struct {
		name                   string
		cluster                *clusterv1.ManagedCluster
		addon                  *addonapiv1alpha1.ManagedClusterAddOn
		expectedNamespace      string
		expectedImage          string
		expectedCount          int
		expectedLimit          string
		expectedRequest        string
		expectedArgs           string
		expectedHeartBeat      string
		expectedRediscoverRate string
		expectedReportRate     string
	}{
		{
			name:    "case_1",
			cluster: newCluster("cluster1"),
			addon:   newAddon(SearchAddonName, "cluster1", "", annotationsTest),
			// The annotation supplies quay.io/test/search_collector:test which is
			// not from a trusted registry; validateImageOverride must reject it and
			// keep the operator-controlled image.
			expectedNamespace:      "open-cluster-management-agent-addon",
			expectedImage:          "quay.io/stolostron/search_collector:2.7.0",
			expectedCount:          7,
			expectedLimit:          "2000Mi",
			expectedRequest:        "1000Mi",
			expectedArgs:           "--v=2",
			expectedHeartBeat:      "3000",
			expectedRediscoverRate: "4000",
			expectedReportRate:     "2000",
		},
		{
			name:                   "case_2",
			cluster:                newCluster("cluster1"),
			addon:                  newAddon(SearchAddonName, "cluster1", "test", annotations250),
			expectedNamespace:      "test",
			expectedImage:          "quay.io/stolostron/search_collector:2.7.0",
			expectedCount:          7,
			expectedLimit:          "2000Mi",
			expectedRequest:        "1000Mi",
			expectedArgs:           "--v=2",
			expectedHeartBeat:      "3000",
			expectedRediscoverRate: "4000",
			expectedReportRate:     "2000",
		},
	}

	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"
	agentAddon := newAgentAddon(t, nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects, err := agentAddon.Manifests(test.cluster, test.addon)
			if err != nil {
				t.Errorf("failed to get manifests with error %v", err)
			}

			if len(objects) != test.expectedCount {
				t.Errorf("expected objects number is %d, got %d", test.expectedCount, len(objects))
			}

			for _, o := range objects {
				switch object := o.(type) {
				case *appsv1.Deployment:
					if object.Namespace != test.expectedNamespace {
						t.Errorf("expected namespace is %s, but got %s", test.expectedNamespace, object.Namespace)
					}
					if object.Spec.Template.Spec.Containers[0].Image != test.expectedImage {
						t.Errorf("expected image is %s, but got %s", test.expectedImage, object.Spec.Template.Spec.Containers[0].Image)
					}
					if object.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String() != test.expectedLimit {
						t.Errorf("expected limit is %s, but got %s", test.expectedLimit, object.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
					}
					if object.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String() != test.expectedRequest {
						t.Errorf("expected request is %s, but got %s", test.expectedRequest, object.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
					}
					if object.Spec.Template.Spec.Containers[0].Args[0] != test.expectedArgs {
						t.Errorf("expected args is %s, but got %s", test.expectedLimit, object.Spec.Template.Spec.Containers[0].Args[0])
					}
					if object.Spec.Template.Spec.Containers[0].Env[3].Name != "REDISCOVER_RATE_MS" {
						t.Errorf("expected env is REDISCOVER_RATE_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[4].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[4].Name != "HEARTBEAT_MS" {
						t.Errorf("expected env is HEARTBEAT_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[5].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[5].Name != "REPORT_RATE_MS" {
						t.Errorf("expected env is REPORT_RATE_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[6].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[3].Value != "4000" {
						t.Errorf("expected value is 4000, but got %s", object.Spec.Template.Spec.Containers[0].Env[4].Value)
					}
					if object.Spec.Template.Spec.Containers[0].Env[4].Value != "3000" {
						t.Errorf("expected value is 3000, but got %s", object.Spec.Template.Spec.Containers[0].Env[5].Value)
					}
					if object.Spec.Template.Spec.Containers[0].Env[5].Value != "2000" {
						t.Errorf("expected value is 2000, but got %s", object.Spec.Template.Spec.Containers[0].Env[6].Value)
					}

				}
			}

		})
	}
}

func TestCreateOrUpdateRoleBinding(t *testing.T) {
	tests := []struct {
		name            string
		initObjects     []runtime.Object
		clusterName     string
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:        "create a new rolebinding",
			initObjects: []runtime.Object{},
			clusterName: "cluster1",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expecte 2 actions, but got %v", actions)
				}

				createAction := actions[1].(clienttesting.CreateActionImpl)
				createObject := createAction.Object.(*rbacv1.RoleBinding)

				groups := agent.DefaultGroups("cluster1", SearchAddonName)

				if createObject.Subjects[0].Name != groups[0] {
					t.Errorf("Expected group name is %s, but got %s", groups[0], createObject.Subjects[0].Name)
				}
			},
		},
		{
			name: "no update",
			initObjects: []runtime.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleBindingName,
						Namespace: "cluster1",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:     rbacv1.GroupKind,
							APIGroup: "rbac.authorization.k8s.io",
							Name:     agent.DefaultGroups("cluster1", SearchAddonName)[0],
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     clusterRoleName,
					},
				},
			},
			clusterName: "cluster1",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expecte 0 actions, but got %v", actions)
				}
			},
		},
		{
			name: "update rolebinding",
			initObjects: []runtime.Object{
				&rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleBindingName,
						Namespace: "cluster1",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:     rbacv1.GroupKind,
							APIGroup: "rbac.authorization.k8s.io",
							Name:     "test",
						},
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     clusterRoleName,
					},
				},
			},
			clusterName: "cluster1",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expecte 2 actions, but got %v", actions)
				}

				updateAction := actions[1].(clienttesting.UpdateActionImpl)
				updateObject := updateAction.Object.(*rbacv1.RoleBinding)

				groups := agent.DefaultGroups("cluster1", SearchAddonName)

				if updateObject.Subjects[0].Name != groups[0] {
					t.Errorf("Expected group name is %s, but got %s", groups[0], updateObject.Subjects[0].Name)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kubeClient := fakekube.NewSimpleClientset(test.initObjects...)
			err := createOrUpdateRoleBinding(kubeClient, SearchAddonName, test.clusterName)
			if err != nil {
				t.Errorf("createOrUpdateRoleBinding expected no error, but got %v", err)
			}

			test.validateActions(t, kubeClient.Actions())
		})
	}
}

func TestManifestAddonAgent(t *testing.T) {
	cases := []struct {
		name                   string
		managedCluster         *clusterv1.ManagedCluster
		managedClusterAddOn    *addonapiv1alpha1.ManagedClusterAddOn
		configMaps             []runtime.Object
		addOnDeploymentConfigs []runtime.Object
		verifyDeployment       func(t *testing.T, objs []runtime.Object)
	}{
		{
			name:                   "no configs",
			managedCluster:         newCluster("cluster1"),
			managedClusterAddOn:    newAddon(SearchAddonName, "cluster1", "", nil),
			configMaps:             []runtime.Object{},
			addOnDeploymentConfigs: []runtime.Object{},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findSearchDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "klusterlet-addon-search" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

			},
		},
		{
			name:           "addondeploymentconfig",
			managedCluster: newCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := newAddon(SearchAddonName, "cluster1", "", nil)
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							SpecHash: "asdf",
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "deploy-config",
							},
						},
					},
				}
				return addon
			}(),
			addOnDeploymentConfigs: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy-config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							Tolerations:  tolerations,
							NodeSelector: nodeSelector,
						},
					},
				},
			},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findSearchDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "klusterlet-addon-search" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != "quay.io/stolostron/search_collector:2.7.0" {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.NodeSelector, nodeSelector) {
					t.Errorf("unexpected nodeSeletor %v", deployment.Spec.Template.Spec.NodeSelector)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.Tolerations, tolerations) {
					t.Errorf("unexpected tolerations %v", deployment.Spec.Template.Spec.Tolerations)
				}
			},
		},
		{
			name:           "addondeploymentconfig and annotation",
			managedCluster: newCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := newAddon(SearchAddonName, "cluster1", "", nil)
				addon.SetAnnotations(map[string]string{"addon.open-cluster-management.io/values": `{"global":{"imageOverrides":
				{"search_collector":"quay.io/test/search_collector:test"}}}`})
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							SpecHash: "asdf",
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "deploy-config",
							},
						},
					},
				}
				return addon
			}(),
			addOnDeploymentConfigs: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy-config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							Tolerations:  tolerations,
							NodeSelector: nodeSelector,
						},
						ResourceRequirements: []addonapiv1alpha1.ContainerResourceRequirements{
							{
								ContainerID: "deployments:klusterlet-addon-search:collector",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("100m"),
										corev1.ResourceMemory: resource.MustParse("2000Mi"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("10m"),
										corev1.ResourceMemory: resource.MustParse("1000Mi"),
									},
								},
							},
						},
					},
				},
			},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findSearchDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "klusterlet-addon-search" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				// The annotation supplies quay.io/test/search_collector:test which is
				// not from a trusted registry; validateImageOverride must reject it.
				if deployment.Spec.Template.Spec.Containers[0].Image != "quay.io/stolostron/search_collector:2.7.0" {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.NodeSelector, nodeSelector) {
					t.Errorf("unexpected nodeSeletor %v", deployment.Spec.Template.Spec.NodeSelector)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.Tolerations, tolerations) {
					t.Errorf("unexpected tolerations %v", deployment.Spec.Template.Spec.Tolerations)
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("10m")) != 0 {
					t.Errorf("unexpected CPU request: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().Cmp(resource.MustParse("100m")) != 0 {
					t.Errorf("unexpected CPU limit: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Cmp(resource.MustParse("1000Mi")) != 0 {
					t.Errorf("unexpected memory request: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("2000Mi")) != 0 {
					t.Errorf("unexpected memory limit: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
				}
			},
		},
		{
			// with a memory limit annotation, a resourcerequirement with a memory limit, and no resourcerequirement memory request
			// we will see the memory limit annotation be applied, the cpu from the resourcerequirements set, and the memory request set to default
			// as there's no annotation nor resourcerequirement for it
			name:           "addondeploymentconfig with resources and annotation of resources",
			managedCluster: newCluster("cluster1"),
			managedClusterAddOn: func() *addonapiv1alpha1.ManagedClusterAddOn {
				addon := newAddon(SearchAddonName, "cluster1", "", nil)
				addon.SetAnnotations(map[string]string{"addon.open-cluster-management.io/values": "",
					"addon.open-cluster-management.io/search_memory_limit": "2000Mi"})
				addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
					{
						ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
							Group:    "addon.open-cluster-management.io",
							Resource: "addondeploymentconfigs",
						},
						DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
							SpecHash: "asdf",
							ConfigReferent: addonapiv1alpha1.ConfigReferent{
								Namespace: "cluster1",
								Name:      "deploy-config",
							},
						},
					},
				}
				return addon
			}(),
			addOnDeploymentConfigs: []runtime.Object{
				&addonapiv1alpha1.AddOnDeploymentConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy-config",
						Namespace: "cluster1",
					},
					Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
						NodePlacement: &addonapiv1alpha1.NodePlacement{
							Tolerations:  tolerations,
							NodeSelector: nodeSelector,
						},
						ResourceRequirements: []addonapiv1alpha1.ContainerResourceRequirements{
							{
								ContainerID: "deployments:klusterlet-addon-search:collector",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("100m"),
										corev1.ResourceMemory: resource.MustParse("3000Mi"), // annotation takes priority
									},
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("10m"), // lack of memory request in annotation leads to default of 128Mi
									},
								},
							},
						},
					},
				},
			},
			verifyDeployment: func(t *testing.T, objs []runtime.Object) {
				deployment := findSearchDeployment(objs)
				if deployment == nil {
					t.Fatalf("expected deployment, but failed")
				}

				if deployment.Name != "klusterlet-addon-search" {
					t.Errorf("unexpected deployment name  %s", deployment.Name)
				}

				if deployment.Namespace != addonfactory.AddonDefaultInstallNamespace {
					t.Errorf("unexpected deployment namespace  %s", deployment.Namespace)
				}

				if deployment.Spec.Template.Spec.Containers[0].Image != "quay.io/stolostron/search_collector:2.7.0" {
					t.Errorf("unexpected image  %s", deployment.Spec.Template.Spec.Containers[0].Image)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.NodeSelector, nodeSelector) {
					t.Errorf("unexpected nodeSeletor %v", deployment.Spec.Template.Spec.NodeSelector)
				}

				if !equality.Semantic.DeepEqual(deployment.Spec.Template.Spec.Tolerations, tolerations) {
					t.Errorf("unexpected tolerations %v", deployment.Spec.Template.Spec.Tolerations)
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("10m")) != 0 {
					t.Errorf("unexpected CPU request: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().Cmp(resource.MustParse("100m")) != 0 {
					t.Errorf("unexpected CPU limit: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Cmp(resource.MustParse("128Mi")) != 0 {
					t.Errorf("unexpected memory request: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String())
				}

				if deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("2000Mi")) != 0 {
					t.Errorf("unexpected memory limit: %s", deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
				}
			},
		},
	}

	for _, c := range cases {
		agentAddon := newAgentAddon(t, c.addOnDeploymentConfigs)
		objects, err := agentAddon.Manifests(c.managedCluster, c.managedClusterAddOn)
		if err != nil {
			t.Fatalf("failed to get manifests %v", err)
		}

		if len(objects) != 7 {
			t.Fatalf("expected 7 manifests, but %v", objects)
		}

		c.verifyDeployment(t, objects)
	}

}

func findSearchDeployment(objs []runtime.Object) *appsv1.Deployment {
	for _, obj := range objs {
		switch obj := obj.(type) {
		case *appsv1.Deployment:
			return obj
		}
	}

	return nil
}

// TestManifest_AddonAnnotationImageIgnored verifies that an image supplied via
// the addon values annotation — even from a trusted registry — is always
// replaced by validateImageOverride (CVE-2026-71471 / CVE-2026-71473 fix).
// The addon annotation is not a trusted source for image overrides; customers
// must use ManagedClusterImageRegistry instead (see TestManifest_MCIRMirroredImageHonoured).
func TestManifest_AddonAnnotationImageIgnored(t *testing.T) {
	for _, tc := range []struct {
		name        string
		annotations map[string]string
	}{
		{
			name: "untrusted registry",
			annotations: map[string]string{
				"addon.open-cluster-management.io/values": `{"global":{"imageOverrides":{"search_collector":"docker.io/attacker/search_collector:evil"}}}`,
			},
		},
		{
			name: "trusted registry via addon annotation",
			annotations: map[string]string{
				"addon.open-cluster-management.io/values": `{"global":{"imageOverrides":{"search_collector":"quay.io/stolostron/search_collector:custom-tag"}}}`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"
			agentAddon := newAgentAddon(t, nil)
			addon := newAddon(SearchAddonName, "cluster1", "", tc.annotations)
			objects, err := agentAddon.Manifests(newCluster("cluster1"), addon)
			if err != nil {
				t.Fatalf("failed to get manifests: %v", err)
			}
			dep := findSearchDeployment(objects)
			if dep == nil {
				t.Fatal("no Deployment found in manifests")
			}
			got := dep.Spec.Template.Spec.Containers[0].Image
			if got != "quay.io/stolostron/search_collector:2.7.0" {
				t.Errorf("image set via addon annotation must be ignored; got %q, want operator image", got)
			}
		})
	}
}

// TestManifest_MCIRMirroredImageHonoured verifies the primary MCIR use-case:
// when ManagedClusterImageRegistry stamps the "open-cluster-management.io/image-registries"
// annotation on the ManagedCluster with a source→mirror registry mapping,
// GetAgentImageValues applies it to the operator-controlled SearchCollectorImage
// and the mirrored image reaches the ManifestWork.
//
// Critically, the mirror registry can be any arbitrary private registry (e.g.
// registry.customer-corp.internal/) — it is not restricted to Red Hat registries.
// This supports customers in disconnected/air-gapped environments.
//
// This is the regression introduced by commit 7278188 ("validate image overrides #818")
// and fixed by adding GetAgentImageValues as the final GetValuesFunc.
func TestManifest_MCIRMirroredImageHonoured(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"
	agentAddon := newAgentAddon(t, nil)

	for _, tc := range []struct {
		name           string
		registriesJSON string
		expectedImage  string
	}{
		{
			name: "customer private registry (arbitrary mirror)",
			// The customer mirrors quay.io/stolostron/ → registry.customer-corp.internal/acm-mirror/
			registriesJSON: `{"registries":[{"source":"quay.io/stolostron","mirror":"registry.customer-corp.internal/acm-mirror"}]}`,
			expectedImage:  "registry.customer-corp.internal/acm-mirror/search_collector:2.7.0",
		},
		{
			name:           "acm-d mirror registry",
			registriesJSON: `{"registries":[{"source":"quay.io/stolostron","mirror":"quay.io/acm-d"}]}`,
			expectedImage:  "quay.io/acm-d/search_collector:2.7.0",
		},
		{
			name:           "redhat official registry",
			registriesJSON: `{"registries":[{"source":"quay.io/stolostron","mirror":"registry.redhat.io/rhacm2"}]}`,
			expectedImage:  "registry.redhat.io/rhacm2/search_collector:2.7.0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newClusterWithImageRegistries("cluster1", tc.registriesJSON)
			addon := newAddon(SearchAddonName, "cluster1", "", nil)
			objects, err := agentAddon.Manifests(cluster, addon)
			if err != nil {
				t.Fatalf("failed to get manifests: %v", err)
			}
			dep := findSearchDeployment(objects)
			if dep == nil {
				t.Fatal("no Deployment found in manifests")
			}
			got := dep.Spec.Template.Spec.Containers[0].Image
			if got != tc.expectedImage {
				t.Errorf("MCIR-mirrored image must be used in ManifestWork;\n  got:  %q\n  want: %q", got, tc.expectedImage)
			}
		})
	}
}

// TestManifest_MCIRAnnotationAndAddonAnnotationImageIsolation verifies that when
// BOTH the ManagedCluster registry annotation (MCIR) AND an image override in the
// addon annotation are present, only the MCIR-derived image is used — the addon
// annotation image is discarded by validateImageOverride before GetAgentImageValues runs.
func TestManifest_MCIRAnnotationAndAddonAnnotationImageIsolation(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"
	agentAddon := newAgentAddon(t, nil)

	// Addon annotation tries to inject an attacker image.
	addonAnnotations := map[string]string{
		"addon.open-cluster-management.io/values": `{"global":{"imageOverrides":{"search_collector":"docker.io/attacker/evil:latest"}}}`,
	}
	// ManagedCluster has a legitimate MCIR mapping.
	cluster := newClusterWithImageRegistries("cluster1",
		`{"registries":[{"source":"quay.io/stolostron","mirror":"registry.customer-corp.internal/acm-mirror"}]}`)
	addon := newAddon(SearchAddonName, "cluster1", "", addonAnnotations)

	objects, err := agentAddon.Manifests(cluster, addon)
	if err != nil {
		t.Fatalf("failed to get manifests: %v", err)
	}
	dep := findSearchDeployment(objects)
	if dep == nil {
		t.Fatal("no Deployment found in manifests")
	}
	got := dep.Spec.Template.Spec.Containers[0].Image
	// The MCIR-mirrored image must win; the attacker image in the addon annotation must be dropped.
	want := "registry.customer-corp.internal/acm-mirror/search_collector:2.7.0"
	if got != want {
		t.Errorf("MCIR image must win over addon annotation image;\n  got:  %q\n  want: %q", got, want)
	}
}

// --- CollectorConfig distribution to managed clusters ---

// TestResolveSearchNamespace covers resolveSearchNamespace directly: not-found (no error),
// found, ambiguous (multiple CRs named searchInstanceName — must error rather than guess), and
// CRs present but under a different name (must be ignored, not matched).
func TestResolveSearchNamespace(t *testing.T) {
	tests := []struct {
		name          string
		objects       []client.Object
		wantNamespace string
		wantFound     bool
		wantErr       bool
	}{
		{
			name:      "no Search CR at all",
			objects:   nil,
			wantFound: false,
		},
		{
			name:          "exactly one Search CR",
			objects:       []client.Object{newSearchCR("open-cluster-management")},
			wantNamespace: "open-cluster-management",
			wantFound:     true,
		},
		{
			name: "two Search CRs with the well-known name, in different namespaces — ambiguous",
			objects: []client.Object{
				newSearchCR("ns1"),
				newSearchCR("ns2"),
			},
			wantErr: true,
		},
		{
			name: "a Search CR with a different name is ignored, not matched",
			objects: []client.Object{
				&searchv1alpha1.Search{ObjectMeta: metav1.ObjectMeta{Name: "not-the-operator-cr", Namespace: "ns3"}},
			},
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			ns, found, err := resolveSearchNamespace(context.TODO(), hubClient)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if found && ns != tc.wantNamespace {
				t.Errorf("namespace = %q, want %q", ns, tc.wantNamespace)
			}
		})
	}
}

// TestGetCollectorConfigValue_ValuesShape asserts the exact Values shape returned by
// getCollectorConfigValue for a populated merged-collector-config: a "collectorConfig.spec" key
// whose content matches the CR's Spec, converted to a plain JSON-safe value tree (matching the
// convention getValue() already uses via addonfactory.JsonStructToValues).
func TestGetCollectorConfigValue_ValuesShape(t *testing.T) {
	hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newSearchCR("open-cluster-management"),
		newMergedCollectorConfig("open-cluster-management", searchv1alpha1.CollectorConfigSpec{
			CollectionRules: []searchv1alpha1.CollectionRule{
				{
					Action:           searchv1alpha1.ActionInclude,
					ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"example.io"}, Kinds: []string{"Foo"}},
				},
			},
		}),
	).Build()

	values, err := getCollectorConfigValue(hubClient)(
		newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, ok := values["collectorConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected values[\"collectorConfig\"] to be a map[string]interface{}, got %T", values["collectorConfig"])
	}
	spec, ok := cfg["spec"].(addonfactory.Values)
	if !ok {
		t.Fatalf("expected collectorConfig[\"spec\"] to be addonfactory.Values, got %T", cfg["spec"])
	}
	rules, ok := spec["collectionRules"].([]interface{})
	if !ok || len(rules) != 1 {
		t.Fatalf("expected exactly 1 entry under collectionRules, got %#v", spec["collectionRules"])
	}
	rule, ok := rules[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected collectionRules[0] to be a map, got %T", rules[0])
	}
	if rule["action"] != "include" {
		t.Errorf(`expected collectionRules[0].action == "include", got %v`, rule["action"])
	}
}

// TestGetCollectorConfigValue_NotFound_ReturnsEmptyValues covers both "not ready yet" cases —
// no Search CR, and Search CR present but no merged-collector-config yet — which must both
// return an empty Values (no "collectorConfig" key) and no error, not an error.
func TestGetCollectorConfigValue_NotFound_ReturnsEmptyValues(t *testing.T) {
	tests := []struct {
		name    string
		objects []client.Object
	}{
		{name: "no Search CR yet", objects: nil},
		{
			name:    "Search CR exists but merged-collector-config does not",
			objects: []client.Object{newSearchCR("open-cluster-management")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			values, err := getCollectorConfigValue(hubClient)(
				newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(values) != 0 {
				t.Errorf("expected empty Values, got %v", values)
			}
		})
	}
}

// TestGetCollectorConfigValue_AmbiguousSearchCR_ReturnsError covers the "refuse to guess" case:
// getCollectorConfigValue must propagate resolveSearchNamespace's error rather than silently
// picking one of the ambiguous Search CRs or omitting the CollectorConfig CR.
func TestGetCollectorConfigValue_AmbiguousSearchCR_ReturnsError(t *testing.T) {
	hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newSearchCR("ns1"),
		newSearchCR("ns2"),
	).Build()

	_, err := getCollectorConfigValue(hubClient)(newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err == nil {
		t.Fatal("expected an error when multiple Search CRs exist, got nil")
	}
}

// TestGetCollectorConfigValue_TransientGetError_ReturnsError is the key correctness regression
// test: a transient (non-NotFound) error from the hub Get must be propagated, NOT swallowed into
// an empty Values{}. Swallowing it would make the Helm template's `hasKey .Values "collectorConfig"`
// guard skip rendering the CR, and the work-agent — which treats each ManifestWork's manifest
// list as the complete desired state — would interpret that as "delete this CR from every managed
// cluster", turning a transient hub API hiccup into a fleet-wide config wipe.
func TestGetCollectorConfigValue_TransientGetError_ReturnsError(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSearchCR("open-cluster-management")).Build()
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			if _, ok := obj.(*searchv1alpha1.CollectorConfig); ok {
				return errors.New("simulated API server error")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	_, err := getCollectorConfigValue(failingClient)(
		newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err == nil {
		t.Fatal("expected an error when the hub Get fails transiently, got nil — must not be swallowed")
	}
}

// TestGetCollectorConfigValue_NilHubClient_ReturnsEmptyValues is a defensive test: NewAddonManager
// always constructs a real client, but getCollectorConfigValue must not panic if it were ever
// called with a nil one (e.g. a future test wiring mistake).
func TestGetCollectorConfigValue_NilHubClient_ReturnsEmptyValues(t *testing.T) {
	values, err := getCollectorConfigValue(nil)(newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("expected empty Values, got %v", values)
	}
}

// findClusterRole returns the ClusterRole with the given name among objs, or nil.
func findClusterRole(objs []runtime.Object, name string) *rbacv1.ClusterRole {
	for _, obj := range objs {
		if cr, ok := obj.(*rbacv1.ClusterRole); ok && cr.GetName() == name {
			return cr
		}
	}
	return nil
}

// TestManifest_CollectorConfigEditorRoleAlwaysRendered verifies the aggregated
// collectorconfig-editor-role ClusterRole is always present, unconditionally — unlike the
// CollectorConfig CR itself, it doesn't depend on hub state, since it's what grants the
// OCM work-agent (klusterlet-work-sa, bound to the built-in "admin" ClusterRole via aggregation
// on every managed cluster) permission to apply the CollectorConfig CR in the first place. Found
// missing during live end-to-end testing: without this aggregation, the work-agent's ManifestWork
// apply for the CollectorConfig CR failed with "is forbidden" on a real cluster.
func TestManifest_CollectorConfigEditorRoleAlwaysRendered(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"
	agentAddon := newAgentAddon(t, nil)

	objects, err := agentAddon.Manifests(newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err != nil {
		t.Fatalf("failed to get manifests: %v", err)
	}

	cr := findClusterRole(objects, "collectorconfig-editor-role")
	if cr == nil {
		t.Fatal("expected collectorconfig-editor-role ClusterRole to always be rendered, but it was not")
	}
	if cr.Labels["rbac.authorization.k8s.io/aggregate-to-admin"] != "true" {
		t.Errorf(`expected label rbac.authorization.k8s.io/aggregate-to-admin="true", got %q`,
			cr.Labels["rbac.authorization.k8s.io/aggregate-to-admin"])
	}
	found := false
	wantVerbs := []string{"create", "get", "list", "patch", "update", "watch", "delete"}
	for _, r := range cr.Rules {
		if !slices.Contains(r.APIGroups, "search.open-cluster-management.io") {
			continue
		}
		found = true
		assertHasAllVerbs(t, r.Verbs, wantVerbs)
	}
	if !found {
		t.Errorf("expected a rule for apiGroup search.open-cluster-management.io, got %+v", cr.Rules)
	}
}

// assertHasAllVerbs fails the test for each entry of want that is missing from got.
func assertHasAllVerbs(t *testing.T, got, want []string) {
	t.Helper()
	for _, verb := range want {
		if !slices.Contains(got, verb) {
			t.Errorf("expected verb %q on collectorconfigs rule, got %v", verb, got)
		}
	}
}

// TestManifest_CollectorConfigDistribution is the end-to-end test: it exercises real Helm
// rendering (not just getCollectorConfigValue's return value) to confirm the rendered
// CollectorConfig object has the right name, namespace, spec, and — critically — no
// ownerReferences (see the "Correctness requirements" section of the design doc).
func TestManifest_CollectorConfigDistribution(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"

	populatedSpec := searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"example.io"}, Kinds: []string{"Foo"}},
			},
		},
	}

	tests := []struct {
		name           string
		hubObjects     []client.Object
		expectRendered bool
		expectSpec     searchv1alpha1.CollectorConfigSpec
	}{
		{
			name:           "no Search CR yet — CollectorConfig CR omitted, other manifests unaffected",
			hubObjects:     nil,
			expectRendered: false,
		},
		{
			name:           "Search CR exists, merged-collector-config does not yet — omitted",
			hubObjects:     []client.Object{newSearchCR("open-cluster-management")},
			expectRendered: false,
		},
		{
			name: "populated merged-collector-config is rendered with matching spec",
			hubObjects: []client.Object{
				newSearchCR("open-cluster-management"),
				newMergedCollectorConfig("open-cluster-management", populatedSpec),
			},
			expectRendered: true,
			expectSpec:     populatedSpec,
		},
		{
			name: "empty CollectionRules is a meaningful state and is still rendered, not skipped",
			hubObjects: []client.Object{
				newSearchCR("open-cluster-management"),
				newMergedCollectorConfig("open-cluster-management", searchv1alpha1.CollectorConfigSpec{}),
			},
			expectRendered: true,
			expectSpec:     searchv1alpha1.CollectorConfigSpec{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.hubObjects...).Build()
			agentAddon := newAgentAddonWithHubClient(t, nil, hubClient)

			objects, err := agentAddon.Manifests(newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
			if err != nil {
				t.Fatalf("failed to get manifests: %v", err)
			}

			cc := findCollectorConfig(objects)
			if !tc.expectRendered {
				if cc != nil {
					t.Fatalf("expected merged-collector-config CR NOT to be rendered, but got %+v", cc)
				}
				// Other manifests (Deployment, RBAC, ...) must still render normally —
				// getCollectorConfigValue omitting its own key must not affect the rest.
				if findSearchDeployment(objects) == nil {
					t.Error("expected Deployment to still be rendered even when CollectorConfig is omitted")
				}
				return
			}

			if cc == nil {
				t.Fatal("expected merged-collector-config CR to be rendered, but it was not")
			}
			if cc.GetName() != mergedCollectorConfigName {
				t.Errorf("expected name %q, got %q", mergedCollectorConfigName, cc.GetName())
			}
			if cc.GetNamespace() != addonfactory.AddonDefaultInstallNamespace {
				t.Errorf("expected namespace %q, got %q", addonfactory.AddonDefaultInstallNamespace, cc.GetNamespace())
			}
			if !equality.Semantic.DeepEqual(cc.Spec, tc.expectSpec) {
				t.Errorf("spec mismatch:\n got:  %+v\n want: %+v", cc.Spec, tc.expectSpec)
			}
			if len(cc.GetOwnerReferences()) != 0 {
				t.Errorf("rendered CollectorConfig must not carry ownerReferences (would leak the hub's "+
					"Search CR ownerReference to a managed cluster with no matching object), got %+v",
					cc.GetOwnerReferences())
			}
		})
	}
}

// TestManifest_CollectorConfigDistribution_AnnotationCannotOverrideHubConfig is a security
// regression test: a ManagedClusterAddOn's addon.open-cluster-management.io/values annotation
// (settable by anyone with edit access to that object for their own cluster — not necessarily a
// hub cluster-admin) must never be able to set or override the rendered collectorConfig. That key
// may only ever come from the hub's merged-collector-config CR via getCollectorConfigValue.
// Without stripCollectorConfigFromAnnotation, addon-framework's value merge would let this
// later-running provider's "collectorConfig" key silently win over the earlier hub-sourced one.
func TestManifest_CollectorConfigDistribution_AnnotationCannotOverrideHubConfig(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"

	hubSpec := searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"example.io"}, Kinds: []string{"Foo"}},
			},
		},
	}
	maliciousAnnotation := `{"collectorConfig":{"spec":{"collectionRules":` +
		`[{"action":"include","resourceSelector":{"apiGroups":["*"],"kinds":["Secret"]}}]}}}`

	tests := []struct {
		name       string
		hubObjects []client.Object
		expectCC   bool
		expectSpec searchv1alpha1.CollectorConfigSpec
	}{
		{
			name: "hub has a populated config — annotation attack is ignored, hub value wins",
			hubObjects: []client.Object{
				newSearchCR("open-cluster-management"),
				newMergedCollectorConfig("open-cluster-management", hubSpec),
			},
			expectCC:   true,
			expectSpec: hubSpec,
		},
		{
			name:       "hub has no config at all — annotation attack still cannot inject one",
			hubObjects: nil,
			expectCC:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.hubObjects...).Build()
			agentAddon := newAgentAddonWithHubClient(t, nil, hubClient)

			addon := newAddon(SearchAddonName, "cluster1", "",
				map[string]string{addonfactory.AnnotationValuesName: maliciousAnnotation})
			objects, err := agentAddon.Manifests(newCluster("cluster1"), addon)
			if err != nil {
				t.Fatalf("failed to get manifests: %v", err)
			}

			cc := findCollectorConfig(objects)
			if !tc.expectCC {
				if cc != nil {
					t.Fatalf("expected no CollectorConfig to be rendered, but the annotation-injected "+
						"one was: %+v", cc)
				}
				return
			}
			if cc == nil {
				t.Fatal("expected the hub-sourced CollectorConfig to be rendered, but it was not")
			}
			if !equality.Semantic.DeepEqual(cc.Spec, tc.expectSpec) {
				t.Errorf("the annotation-injected spec leaked through:\n got:  %+v\n want: %+v",
					cc.Spec, tc.expectSpec)
			}
		})
	}
}

// TestManifest_CollectorConfigDistribution_TransientErrorAbortsWholeRender confirms that when
// getCollectorConfigValue propagates a transient error, agentAddon.Manifests() itself returns an
// error (rather than a partial/empty manifest list) — this is what makes addon-framework leave
// the previously-applied ManifestWork untouched instead of wiping it (see
// agentdeploy/controller.go's handling of a Manifests() error, cited in getCollectorConfigValue's
// doc comment).
func TestManifest_CollectorConfigDistribution_TransientErrorAbortsWholeRender(t *testing.T) {
	SearchCollectorImage = "quay.io/stolostron/search_collector:2.7.0"

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSearchCR("open-cluster-management")).Build()
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			if _, ok := obj.(*searchv1alpha1.CollectorConfig); ok {
				return errors.New("simulated API server error")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	agentAddon := newAgentAddonWithHubClient(t, nil, failingClient)
	objects, err := agentAddon.Manifests(newCluster("cluster1"), newAddon(SearchAddonName, "cluster1", "", nil))
	if err == nil {
		t.Fatal("expected Manifests() to return an error, got nil")
	}
	if objects != nil {
		t.Errorf("expected no objects on error, got %v", objects)
	}
}
