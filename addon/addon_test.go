package addon

import (
	"open-cluster-management.io/addon-framework/pkg/utils"
	"testing"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
)

var (
	scheme       = runtime.NewScheme()
	nodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	tolerations  = []corev1.Toleration{{Key: "foo", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}}
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = prometheusv1.AddToScheme(scheme)
}

func newCluster(name string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return cluster
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
	getValuesFunc := getValue
	fakeAddonClient := fakeaddon.NewSimpleClientset(objects...)
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithScheme(scheme).
		WithGetValuesFuncs(getValuesFunc, addonfactory.GetValuesFromAddonAnnotation,
			addonfactory.GetAddOnDeploymentConfigValues(
				utils.NewAddOnDeploymentConfigGetter(fakeAddonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnResourceRequirementsValues,
			)).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		t.Fatalf("failed to build agent %v", err)

	}
	return agentAddon
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
			name:                   "case_1",
			cluster:                newCluster("cluster1"),
			addon:                  newAddon(SearchAddonName, "cluster1", "", annotationsTest),
			expectedNamespace:      "open-cluster-management-agent-addon",
			expectedImage:          "quay.io/test/search_collector:test",
			expectedCount:          4,
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
			expectedCount:          4,
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

				if deployment.Spec.Template.Spec.Containers[0].Image != "quay.io/test/search_collector:test" {
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
	}

	for _, c := range cases {
		agentAddon := newAgentAddon(t, c.addOnDeploymentConfigs)
		objects, err := agentAddon.Manifests(c.managedCluster, c.managedClusterAddOn)
		if err != nil {
			t.Fatalf("failed to get manifests %v", err)
		}

		if len(objects) != 4 {
			t.Fatalf("expected 4 manifests, but %v", objects)
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
