package addon

import (
	"context"
	"embed"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"reflect"
	"regexp"
	"strconv"

	"github.com/cloudflare/cfssl/log"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	imagevalidation "github.com/stolostron/search-v2-operator/internal"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SearchAddonName = "search-collector"

	// the clusterRole has been installed with the search-operator deployment
	clusterRoleName = "open-cluster-management:addons:search-collector"
	roleBindingName = "open-cluster-management:addons:search-collector"

	GroupName = "rbac.authorization.k8s.io"

	// mergedCollectorConfigName is the name of the operator-computed CollectorConfig CR on the
	// hub (see controllers.createOrUpdateMergedCollectorConfig) whose Spec is distributed, as-is,
	// to every managed cluster running the search-collector addon.
	mergedCollectorConfigName = "merged-collector-config"

	// searchInstanceName duplicates controllers.OperatorName. This package cannot import
	// controllers — controllers already imports addon, so importing back would create a cycle.
	// There is always supposed to be exactly one Search CR cluster-wide, named this.
	searchInstanceName = "search-v2-operator"
)

//go:embed manifests
//go:embed manifests/chart
//go:embed manifests/chart/templates/_helpers.tpl
var ChartFS embed.FS

var Scheme = runtime.NewScheme()

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
	GlobalValues           GlobalValues           `json:"global,"`
	KubernetesDistribution string                 `json:"kubernetesDistribution"`
	Prometheus             map[string]interface{} `json:"prometheus"`
	UserArgs               UserArgs               `json:"userargs,"`
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
		Prometheus: map[string]interface{}{},
	}
	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			addonValues.KubernetesDistribution = cc.Value
			break
		}
	}
	// enable prometheus if on OpenShift
	addonValues.Prometheus["enabled"] = addonValues.KubernetesDistribution == "OpenShift"

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

// validateImageOverride closes the CVE-2026-71471 / CVE-2026-71473 attack vector:
// any image injected via the "addon.open-cluster-management.io/values" annotation
// on the ManagedClusterAddOn (func [1], GetValuesFromAddonAnnotation) is discarded
// by unconditionally resetting the image to the operator-controlled SearchCollectorImage.
//
// This func runs as func [3] — after GetValuesFromAddonAnnotation but before
// GetAgentImageValues. GetAgentImageValues (func [4], the last func) then applies
// the ManagedCluster's "open-cluster-management.io/image-registries" registry
// mapping rules to SearchCollectorImage to produce the MCIR-mirrored image.
// Because GetAgentImageValues derives its output solely from the operator-controlled
// base image via prefix replacement, the final image is always a deterministic
// transform of a known-good image — never an arbitrary injection.
func validateImageOverride(_ *clusterv1.ManagedCluster,
	_ *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	return addonfactory.Values{
		"global": map[string]interface{}{
			"imageOverrides": map[string]interface{}{
				"search_collector": SearchCollectorImage,
			},
		},
	}, nil
}

// resolveSearchNamespace returns the namespace of the live Search CR (there is always supposed
// to be exactly one, named searchInstanceName) via the hub client. found is false (with err nil)
// if no such CR exists yet — e.g. a fresh install racing with the operator's own first reconcile
// — which callers should treat as "not ready yet", not as an error. Mirrors
// controllers.IntegrationCollectorConfigSeeder.resolveSearch's lookup and its rationale for
// refusing to guess when more than one match is found (duplicated rather than imported, for the
// same import-cycle reason as searchInstanceName above).
func resolveSearchNamespace(ctx context.Context, hubClient client.Client) (namespace string, found bool, err error) {
	list := &searchv1alpha1.SearchList{}
	if err := hubClient.List(ctx, list); err != nil {
		return "", false, err
	}
	var match *searchv1alpha1.Search
	for i := range list.Items {
		if list.Items[i].Name == searchInstanceName {
			if match != nil {
				return "", false, fmt.Errorf(
					"found multiple Search CRs named %q (namespaces %q and %q) — refusing to guess which one to use",
					searchInstanceName, match.Namespace, list.Items[i].Namespace)
			}
			match = &list.Items[i]
		}
	}
	if match == nil {
		return "", false, nil
	}
	return match.Namespace, true, nil
}

// getCollectorConfigValue reads the hub's merged-collector-config CollectorConfig CR (computed by
// controllers.createOrUpdateMergedCollectorConfig from user + integration-team rules) and injects
// only its Spec into the Helm values, under the "collectorConfig.spec" key, so
// collectorconfig_cr.yaml can render a matching CR into every managed cluster running the
// search-collector addon. This is what lets search-collector pick up the hub's collection rules
// on managed clusters without any ACM Policy.
//
// Correctness requirements:
//
//   - Only .Spec is ever read — never the full hub object. The hub's merged-collector-config
//     carries a controller ownerReference pointing at the hub Search CR, which has no matching
//     object on a managed cluster; extracting only Spec makes it structurally impossible to leak
//     that ownerReference into the rendered CR.
//
//   - "Not found yet" (no Search CR, or no merged-collector-config CR yet — both legitimate
//     during a fresh install race, or after the Search CR's deletion cascade has already removed
//     merged-collector-config) is reported by omitting the "collectorConfig" key entirely
//     (addonfactory.Values{}, nil), not as an error.
//
//   - Any other error is propagated (nil, err) — NOT swallowed into an empty Values{}. The
//     addon-framework re-renders the full manifest list from scratch on every pass; silently
//     returning empty Values{} here on a transient error would make the Helm template's
//     `{{- if hasKey .Values "collectorConfig" }}` guard skip rendering the CR, and the
//     work-agent — which treats each ManifestWork's manifest list as the complete desired state —
//     would then DELETE a previously-delivered CollectorConfig CR from every managed cluster.
//     Propagating the error instead makes addon-framework mark the ManagedClusterAddOn's Applied
//     condition False and leave the previously-applied ManifestWork untouched.
func getCollectorConfigValue(hubClient client.Client) addonfactory.GetValuesFunc {
	return func(_ *clusterv1.ManagedCluster, _ *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
		if hubClient == nil {
			// Defensive only — NewAddonManager always constructs a real client.
			return addonfactory.Values{}, nil
		}
		ctx := context.TODO()

		namespace, found, err := resolveSearchNamespace(ctx, hubClient)
		if err != nil {
			return nil, err
		}
		if !found {
			return addonfactory.Values{}, nil
		}

		cc := &searchv1alpha1.CollectorConfig{}
		err = hubClient.Get(ctx, types.NamespacedName{Name: mergedCollectorConfigName, Namespace: namespace}, cc)
		if err != nil {
			if errors.IsNotFound(err) {
				return addonfactory.Values{}, nil
			}
			return nil, err
		}

		// Round-trip through JSON (matching the convention getValue() already uses via
		// addonfactory.JsonStructToValues) rather than passing the typed struct straight through,
		// so the value tree reaching the Helm template is plain map[string]interface{}/[]interface{}
		// all the way down — the same shape every other GetValuesFunc in this file produces.
		specValues, err := addonfactory.JsonStructToValues(cc.Spec)
		if err != nil {
			return nil, err
		}
		return addonfactory.Values{
			"collectorConfig": map[string]interface{}{
				"spec": specValues,
			},
		}, nil
	}
}

// stripCollectorConfigFromAnnotation wraps addonfactory.GetValuesFromAddonAnnotation to drop any
// "collectorConfig" key present in the addon.open-cluster-management.io/values annotation on a
// ManagedClusterAddOn, before merging those values into the chain.
//
// collectorConfig must only ever come from the hub's merged-collector-config CR via
// getCollectorConfigValue above — that provider runs earlier in the WithGetValuesFuncs chain, but
// addon-framework's value merge lets a LATER provider's key override an EARLIER one, and
// GetValuesFromAddonAnnotation accepts arbitrary top-level keys with no schema restriction.
// Without this guard, anyone with permission to edit a ManagedClusterAddOn's annotations for
// their own cluster (not necessarily a hub cluster-admin) could set
// {"collectorConfig":{"spec":{...}}} on that annotation and override the fleet-wide collection
// policy for that cluster — including when the hub has no merged-collector-config yet, since an
// empty hub result does not remove an annotation-provided key from the merge.
func stripCollectorConfigFromAnnotation(
	cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	values, err := addonfactory.GetValuesFromAddonAnnotation(cluster, addon)
	if err != nil {
		return values, err
	}
	if _, ok := values["collectorConfig"]; ok {
		klog.Warningf(
			"ignoring collectorConfig set via the %s annotation on ManagedClusterAddOn %s/%s: "+
				"this value may only be set by the hub's merged-collector-config CR",
			addonfactory.AnnotationValuesName, addon.GetNamespace(), addon.GetName())
		delete(values, "collectorConfig")
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

// NewAddonManager builds the search-collector addon manager: it registers the Prometheus and
// search v1alpha1 API types onto the shared Scheme, constructs the addon/kube/hub clients the
// GetValuesFuncs below depend on, and wires the addon-framework factory (base defaults,
// CollectorConfig distribution, annotation overrides, node placement, and probe-rollout values,
// in that order) into a ready-to-run AddonManager. Returns an error — rather than continuing with
// a partially-initialized Scheme or client — on any setup failure, since every downstream
// GetValuesFunc assumes these succeeded.
func NewAddonManager(kubeConfig *rest.Config) (addonmanager.AddonManager, error) {
	if SearchCollectorImage == "" {
		return nil, fmt.Errorf("the search-collector pod image is empty")
	}
	err := prometheusv1.AddToScheme(Scheme)
	if err != nil {
		klog.Errorf("failed to add Prometheus scheme to scheme: %v", err)
	}
	if err := searchv1alpha1.AddToScheme(Scheme); err != nil {
		klog.Errorf("failed to add search v1alpha1 scheme to scheme: %v", err)
		return nil, err
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
	// hubClient is used only by getCollectorConfigValue, to read the hub's
	// merged-collector-config CollectorConfig CR. Scheme already has searchv1alpha1 registered
	// above.
	hubClient, err := client.New(kubeConfig, client.Options{Scheme: Scheme})
	if err != nil {
		klog.Errorf("unable to create hub client for CollectorConfig distribution: %v", err)
		return nil, err
	}
	agentAddon, err := addonfactory.NewAgentAddonFactory(SearchAddonName, ChartFS, ChartDir).
		WithScheme(Scheme).
		WithConfigGVRs(
			utils.AddOnDeploymentConfigGVR,
		).WithGetValuesFuncs(
		// [0] Base defaults: SearchCollectorImage, pull policy, proxy config, user args
		// from the per-cluster addon annotations (memory limits, heartbeat, etc.).
		getValue,
		// [1] Distribute the hub's merged-collector-config CollectorConfig Spec to every
		// managed cluster. Independent top-level "collectorConfig" key; does not interact
		// with [2]-[5].
		getCollectorConfigValue(hubClient),
		// [2] Merge non-image values from the addon annotation (e.g. memory limits,
		// container args). May also set an image — deliberately ignored by [3]. Strips any
		// "collectorConfig" key the annotation might carry — see
		// stripCollectorConfigFromAnnotation's doc comment for why.
		stripCollectorConfigFromAnnotation,
		// [3] Node placement and resource requirements from AddOnDeploymentConfig.
		addonfactory.GetAddOnDeploymentConfigValues(
			utils.NewAddOnDeploymentConfigGetter(addonClient),
			addonfactory.ToAddOnNodePlacementValues,
			addonfactory.ToAddOnResourceRequirementsValues),
		// [4] Security gate (CVE-2026-71471 / CVE-2026-71473): unconditionally reset
		// the image to SearchCollectorImage, discarding any image set by [2] via the
		// addon annotation. This closes the arbitrary-image-injection attack vector.
		validateImageOverride,
		// [5] MCIR support: apply the "open-cluster-management.io/image-registries"
		// registry mapping rules from the ManagedCluster annotation to
		// SearchCollectorImage. This transforms the known-good base image's registry
		// prefix to the customer's mirror registry — supporting arbitrary private
		// registries in disconnected/air-gapped environments. Runs last so it wins.
		// AddOnDeploymentConfig.Spec.Registries takes precedence if both are set.
		addonfactory.GetAgentImageValues(
			utils.NewAddOnDeploymentConfigGetter(addonClient),
			"global.imageOverrides.search_collector",
			SearchCollectorImage,
		),
	).WithAgentDeployTriggerClusterFilter(
		// Trigger redeployment when the MCIR annotation on the ManagedCluster changes
		// so that newly applied or updated registry mirrors take effect immediately.
		func(old, new *clusterv1.ManagedCluster) bool {
			return old.GetAnnotations()[clusterv1.ClusterImageRegistriesAnnotationKey] !=
				new.GetAnnotations()[clusterv1.ClusterImageRegistriesAnnotationKey]
		},
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
	if override := instance.Spec.Deployments.Collector.ImageOverride; override != "" {
		if err := imagevalidation.ValidateImageRepo(override); err != nil {
			klog.Errorf("Ignoring invalid collector image override: %v", err)
		} else {
			SearchCollectorImage = override
		}
	}
	go startAddon(ctx)
}
