package addon

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
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

func newAgentAddon(t *testing.T) agent.AgentAddon {
	registrationOption := newRegistrationOption(nil, SearchAddonName)
	getValuesFunc := getValue
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithScheme(scheme).
		WithGetValuesFuncs(getValuesFunc, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		WithInstallStrategy(agent.InstallAllStrategy("open-cluster-management-agent-addon")).
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
	agentAddon := newAgentAddon(t)
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
					if object.Spec.Template.Spec.Containers[0].Env[4].Name != "REDISCOVER_RATE_MS" {
						t.Errorf("expected env is REDISCOVER_RATE_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[4].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[5].Name != "HEARTBEAT_MS" {
						t.Errorf("expected env is HEARTBEAT_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[5].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[6].Name != "REPORT_RATE_MS" {
						t.Errorf("expected env is REPORT_RATE_MS, but got %s", object.Spec.Template.Spec.Containers[0].Env[6].Name)
					}
					if object.Spec.Template.Spec.Containers[0].Env[4].Value != "4000" {
						t.Errorf("expected value is 4000, but got %s", object.Spec.Template.Spec.Containers[0].Env[4].Value)
					}
					if object.Spec.Template.Spec.Containers[0].Env[5].Value != "3000" {
						t.Errorf("expected value is 3000, but got %s", object.Spec.Template.Spec.Containers[0].Env[5].Value)
					}
					if object.Spec.Template.Spec.Containers[0].Env[6].Value != "2000" {
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
