package addon

import (
	"context"
	"embed"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"

	"github.com/cloudflare/cfssl/log"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	SearchAddonName = "search-collector"

	// the clusterRole has been installed with the search-operator deployment
	clusterRoleName = "open-cluster-management:addons:search-collector"
	roleBindingName = "open-cluster-management:addons:search-collector"

	GroupName = "rbac.authorization.k8s.io"
)

//go:embed manifests
//go:embed manifests/chart
//go:embed manifests/chart/templates/_helpers.tpl
var ChartFS embed.FS

const ChartDir = "manifests/chart"
const resourceRegex = "^(\\+|-)?(([0-9]+(\\.[0-9]*)?)|(\\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\\+|-)?(([0-9]+(\\.[0-9]*)?)|(\\.[0-9]+))))?$"

var addonLog = ctrl.Log.WithName("addon")

var SearchCollectorImage string = os.Getenv("COLLECTOR_IMAGE")

type GlobalValues struct {
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,"`
	ImagePullSecret string            `json:"imagePullSecret"`
	ImageOverrides  map[string]string `json:"imageOverrides,"`
	ProxyConfig     map[string]string `json:"proxyConfig,"`
}

type UserArgs struct {
	ContainerArgs  string `json:"containerArgs,"`
	LimitMemory    string `json:"limitMemory,"`
	RequestMemory  string `json:"requestMemory,"`
	RediscoverRate int    `json:"rediscoverRate,"`
	HeartBeat      int    `json:"heartBeat,"`
	ReportRate     int    `json:"reportRate,"`
}

type Values struct {
	GlobalValues GlobalValues `json:"global,"`
	UserArgs     UserArgs     `json:"userargs,"`
}

func getValue(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	addonValues := Values{
		GlobalValues: GlobalValues{
			ImagePullPolicy: corev1.PullIfNotPresent,
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"search_collector": SearchCollectorImage,
			},

			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_memory_limit"]; ok {
		match, err := regexp.MatchString(resourceRegex, val)
		if err != nil {
			addonLog.Info("Error parsing memory limit for cluster %s", cluster.Name)
		} else if match {
			addonValues.UserArgs.LimitMemory = val
		}
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_memory_request"]; ok {
		match, err := regexp.MatchString(resourceRegex, val)
		if err != nil {
			addonLog.Info("Error parsing memory request for cluster %s", cluster.Name)
		} else if match {
			addonValues.UserArgs.RequestMemory = val
		}
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_args"]; ok {
		addonValues.UserArgs.ContainerArgs = val
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_rediscover_rate"]; ok {
		intVal, err := strconv.Atoi(val)
		if err == nil {
			addonValues.UserArgs.RediscoverRate = intVal
		}

	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_heartbeat"]; ok {
		intVal, err := strconv.Atoi(val)
		if err == nil {
			addonValues.UserArgs.HeartBeat = intVal
		}
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/search_report_rate"]; ok {
		intVal, err := strconv.Atoi(val)
		if err == nil {
			addonValues.UserArgs.ReportRate = intVal
		}
	}

	values, err := addonfactory.JsonStructToValues(addonValues)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func newRegistrationOption(kubeClient kubernetes.Interface, addonName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, addonName),
		CSRApproveCheck:   utils.DefaultCSRApprover(addonName),
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
			return createOrUpdateRoleBinding(kubeClient, addonName, cluster.Name)
		},
	}
}

// createOrUpdateRoleBinding create or update a role binding for a given cluster
func createOrUpdateRoleBinding(kubeClient kubernetes.Interface, addonName, clusterName string) error {
	acmRoleBinding := newRoleBindingForClusterRole(roleBindingName, clusterRoleName, clusterName, addonName)

	binding, err := kubeClient.RbacV1().RoleBindings(clusterName).Get(context.TODO(), roleBindingName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = kubeClient.RbacV1().RoleBindings(clusterName).Create(context.TODO(), acmRoleBinding, metav1.CreateOptions{})
		}
		return err
	}

	needUpdate := false
	if !reflect.DeepEqual(acmRoleBinding.RoleRef, binding.RoleRef) {
		needUpdate = true
		binding.RoleRef = acmRoleBinding.RoleRef
	}
	if !reflect.DeepEqual(acmRoleBinding.Subjects, binding.Subjects) {
		needUpdate = true
		binding.Subjects = acmRoleBinding.Subjects
	}
	if needUpdate {
		_, err = kubeClient.RbacV1().RoleBindings(clusterName).Update(context.TODO(), binding, metav1.UpdateOptions{})
		return err
	}

	return nil
}

func newRoleBindingForClusterRole(name, clusterRoleName, clusterName, addonName string) *rbacv1.RoleBinding {
	groups := agent.DefaultGroups(clusterName, addonName)
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.GroupKind,
				APIGroup: GroupName,
				Name:     groups[0],
			},
		},
	}
}

func NewAddonManager(kubeConfig *rest.Config) (addonmanager.AddonManager, error) {
	if SearchCollectorImage == "" {
		return nil, fmt.Errorf("the search-collector pod image is empty")
	}
	addonMgr, err := addonmanager.New(kubeConfig)
	if err != nil {
		klog.Errorf("unable to setup addon manager: %v", err)
		return nil, err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("unable to setup addon client: %v", err)
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("unable to create kube client: %v", err)
		return nil, err
	}
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithConfigGVRs(
			utils.AddOnDeploymentConfigGVR,
		).WithGetValuesFuncs(
		getValue,
		addonfactory.GetValuesFromAddonAnnotation,
		addonfactory.GetAddOnDeloymentConfigValues(
			addonfactory.NewAddOnDeloymentConfigGetter(addonClient),
			addonfactory.ToAddOnNodePlacementValues),
	).WithAgentRegistrationOption(newRegistrationOption(kubeClient, SearchAddonName)).
		BuildHelmAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return addonMgr, err
	}
	err = addonMgr.AddAgent(agentAddon)
	return addonMgr, err
}

func startAddon(ctx context.Context) {
	controller := "controller: "
	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Error(err, "unable to get kubeConfig , addon cannot be installed ", controller, "SearchOperator")
		return
	}
	addonMgr, err := NewAddonManager(kubeConfig)
	if err != nil {
		klog.Error(err, " unable to create a new  addon manager ", controller, "SearchOperator")
	} else {
		klog.Info("starting search addon manager")
		err = addonMgr.Start(ctx)
		if err != nil {
			klog.Error(err, "unable to start a new  addon manager ", controller, "SearchOperator")
		}
	}
}

/*
Addon needs to be started only at the first time when CR is created.
No need to start every reconcile
*/
func CreateAddonOnce(ctx context.Context, instance *searchv1alpha1.Search) {
	log.Info("Starting Search Addon")
	if instance.Spec.Deployments.Collector.ImageOverride != "" {
		SearchCollectorImage = instance.Spec.Deployments.Collector.ImageOverride
	}
	go startAddon(ctx)
}
