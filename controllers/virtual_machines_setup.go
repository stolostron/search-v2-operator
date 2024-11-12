// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	CONDITION_VM_ACTIONS = "VirtualMachineActionsReady"
)

var (
	clusterPermissionGvr = schema.GroupVersionResource{
		Group:    "rbac.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "clusterpermissions",
	}
	appSearchVMLabels = map[string]interface{}{
		"app":     "search",
		"feature": "virtual-machines",
	}
)

// Reconcile Virtual Machines feature.
//  1. Check the virtual-machines-preview annotation.
//  2. Validate dependencies.
//     a. The ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//     b. The ClusterProxy addon is enabled in the MultiClusterEngine CR.
//  3. Enable Virtual Machine actions in the console.
//  4. Create a ManagedServiceAccount recource for each Managed Cluster.
//  5. Create a ClusterPermission resource for each Managed Cluster.
func (r *SearchReconciler) reconcileVirtualMachineSetup(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {

	if instance.ObjectMeta.Annotations["virtual-machines-preview"] == "true" {
		log.V(1).Info("The virtual-machines-preview annotation is present. Setting up Virtual machine actions.")

		err := r.validateVirtualMachineDependencies(ctx)
		if err != nil {
			log.Error(err, "Failed to validate virtual machine actions dependencies.")
			return &reconcile.Result{}, err
		}

		err = r.enableVMActions(ctx)
		if err != nil {
			log.Error(err, "Failed to enable virtual machine actions.")
			return &reconcile.Result{}, err
		}

		r.updateVMStatus(ctx, instance, metav1.Condition{
			Type:               CONDITION_VM_ACTIONS,
			Status:             metav1.ConditionTrue,
			Reason:             "VirtualMachineActionsEnabled",
			Message:            "Virtual Machine actions are enabled.",
			LastTransitionTime: metav1.Now(),
		})

	} else {
		log.V(3).Info("The virtual-machines-preview annotation is not present.")

		// Use the status conditions to determine if VM actions was enabled before this reconcile.
		vmActionsConditionIndex := -1
		for index, condition := range instance.Status.Conditions {
			if condition.Type == CONDITION_VM_ACTIONS {
				vmActionsConditionIndex = index
				break
			}
		}
		if vmActionsConditionIndex > -1 {
			log.V(1).Info("Virtual Machine actions were enabled before. Disabling virtual machine actions.")
			err := r.disableVirtualMachineActions(ctx)
			if err != nil {
				log.Error(err, "Failed to disable virtual machine actions.")
				r.updateVMStatus(ctx, instance, metav1.Condition{
					Type:               CONDITION_VM_ACTIONS,
					Status:             metav1.ConditionFalse,
					Reason:             "VirtualMachineActionsDisabled",
					Message:            "Failed to disable virtual machine actions.",
					LastTransitionTime: metav1.Now(),
				})
				return &reconcile.Result{}, err
			}

			r.updateVMStatus(ctx, instance, metav1.Condition{
				Type:               CONDITION_VM_ACTIONS,
				Status:             metav1.ConditionFalse,
				Reason:             "None",
				Message:            "None",
				LastTransitionTime: metav1.Now(),
			})
		}
	}
	return &reconcile.Result{}, nil
}

// Validate dependencies.
//  1. The ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//  2. The ClusterProxy addon is enabled in the MultiClusterEngine CR.
func (r *SearchReconciler) validateVirtualMachineDependencies(ctx context.Context) error {
	log.V(2).Info("Checking virtual machine actions dependencies.")

	// Verify that MulticlusterEngine is installed and has the ManagedServiceAccount and ClusterProxy add-ons enabled.
	mces, err := r.DynamicClient.Resource(multiclusterengineResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil || len(mces.Items) == 0 {
		log.Error(err, "Failed to validate dependency MulticlusterEngine operator.")
		return fmt.Errorf("failed to validate dependency MulticlusterEngine operator")
	} else {
		log.V(5).Info("Found MulticlusterEngine instance.")
	}

	// Verify that MulticlusterEngine add-ons ManagedServiceAccount and ClusterProxy are enabled.
	managedServiceAccountEnabled := false
	clusterProxyEnabled := false
	components := mces.Items[0].Object["spec"].(map[string]interface{})["overrides"].(map[string]interface{})["components"]
	for _, component := range components.([]interface{}) {
		if component.(map[string]interface{})["name"] == "managedserviceaccount" &&
			component.(map[string]interface{})["enabled"] == true {
			log.V(5).Info("Managed Service Account add-on is enabled.")
			managedServiceAccountEnabled = true
		}
		if component.(map[string]interface{})["name"] == "cluster-proxy-addon" &&
			component.(map[string]interface{})["enabled"] == true {
			log.V(5).Info("Cluster Proxy add-on is enabled.")
			clusterProxyEnabled = true
		}
	}
	if !managedServiceAccountEnabled {
		return fmt.Errorf("The managedserviceaccount add-on is not enabled in MulticlusterEngine.")
	}
	if !clusterProxyEnabled {
		return fmt.Errorf("The cluster-proxy-addon is not enabled in MulticlusterEngine.")
	}
	log.V(2).Info("Virtual Machine actions dependencies validated.")
	return nil
}

// Logic to enable virtual machine actions.
//  1. Enable virtual machine actions feature in the console.
//     a. Add VIRTUAL_MACHINE_ACTIONS=enabled to configmap console-mce-config in multicluster-engine namespace.
//  2. Create configuration resources for each Managed Hub.
//     a. Create a ManagedServiceAccount vm-actor.
//     b. Create a ClusterPermission vm-actions if it doesn't exist.
func (r *SearchReconciler) enableVMActions(ctx context.Context) error {
	errList := []error{} // Using this to allow partial errors and combine at the end.

	// 1. Enable virtual machine actions feature in the console.
	err := r.updateConsoleConfigVM(ctx, true)
	logAndTrackError(&errList, err, "Failed to set VIRTUAL_MACHINE_ACTIONS=enabled in console-mce-config.")

	// 2. Create ManagedServiceAccount and ClusterPermission resources for each Managed Cluster.
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	logAndTrackError(&errList, err, "Failed to list the ManagedClusters to configure virtual machine actions.")

	if err == nil && clusterList != nil {
		for _, cluster := range clusterList.Items {
			// TODO: Check if kubevirt.io is installed in the managed cluster.

			// a. Create the ManagedServiceAccount vm-actor.
			err = r.createVMManagedServiceAccount(ctx, cluster.GetName())
			logAndTrackError(&errList, err, "Failed to create ManagedServiceAccount vm-actor", "cluster", cluster.GetName())

			// b. Create the ClusterPermission vm-actions.
			err = r.createVMClusterPermission(ctx, cluster.GetName())
			logAndTrackError(&errList, err, "Failed to create ClusterPermission vm-actions", "cluster", cluster.GetName())
		}
	}

	// Combine all errors.
	if len(errList) > 0 {
		err = fmt.Errorf("Failed to enable virtual machine actions. Errors: %v", errList)
		log.Error(err, "Failed to enable virtual machine actions.")
		return err
	}

	log.Info("Virtual machine actions resources configured.")
	return nil
}

// Create a ManagedServiceAccount in the managed cluster namespace.
// This will create a service account in the managed cluster and sync the secret containing the access token.
func (r *SearchReconciler) createVMManagedServiceAccount(ctx context.Context, cluster string) error {
	managedSA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "authentication.open-cluster-management.io/v1beta1",
			"kind":       "ManagedServiceAccount",
			"metadata": map[string]interface{}{
				"name":   "vm-actor",
				"labels": appSearchVMLabels,
			},
			"spec": map[string]interface{}{
				"rotation": map[string]interface{}{},
			},
		},
	}
	_, err := r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster).
		Create(ctx, managedSA, metav1.CreateOptions{})
	if err != nil && errors.IsAlreadyExists(err) {
		log.V(5).Info("Found ManagedServiceAccount vm-actor.", "namespace", cluster)
		return nil
	}
	return err
}

// Create a ClusterPermission in the managed cluster namespace.
// The ClusterPermission is used to set permissions to the ManagedServiceAccount
func (r *SearchReconciler) createVMClusterPermission(ctx context.Context, cluster string) error {
	clusterPermission := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.open-cluster-management.io/v1alpha1",
			"kind":       "ClusterPermission",
			"metadata": map[string]interface{}{
				"name":   "vm-actions",
				"labels": appSearchVMLabels,
			},
			"spec": map[string]interface{}{
				"clusterRole": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{
							"apiGroups": []string{"subresources.kubevirt.io"},
							"resources": []string{
								"virtualmachines/start",
								"virtualmachines/stop",
								"virtualmachines/restart",
								"virtualmachineinstances/pause",
								"virtualmachineinstances/unpause",
							},
							"verbs": []string{"update"},
						},
					},
				},
				"clusterRoleBinding": map[string]interface{}{
					"subject": map[string]interface{}{
						"kind":      "ServiceAccount",
						"name":      "vm-actor",
						"namespace": "open-cluster-management-agent-addon",
					},
				},
			},
		},
	}
	_, err := r.DynamicClient.Resource(clusterPermissionGvr).Namespace(cluster).
		Create(ctx, clusterPermission, metav1.CreateOptions{})

	if err != nil && errors.IsAlreadyExists(err) {
		log.V(5).Info("Found existing ClusterPermission vm-actions.", "namespace", cluster)
		return nil
	}
	return err
}

// Logic to disable virtual machine actions.
//  1. Disable virtual machine actions in the console.
//  2. Delete configuration resources for each managed cluster.
func (r *SearchReconciler) disableVirtualMachineActions(ctx context.Context) error {
	errList := []error{}

	// 1. Disable virtual machine actions in the console.
	err := r.updateConsoleConfigVM(ctx, false)
	logAndTrackError(&errList, err, "Failed to remove the VIRTUAL_MACHINE_ACTIONS in configmap console-mce-config.")

	// 2. Delete ManagedServiceAccount and ClusterPermission resources.
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil || clusterList == nil {
		logAndTrackError(&errList, err, "Failed to list ManagedClusters.")
	}
	for _, cluster := range clusterList.Items {
		// 2a. Delete the ManagedServiceAccount vm-actor.
		err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
			Delete(ctx, "vm-actor", metav1.DeleteOptions{})

		if err != nil && !errors.IsNotFound(err) { // Ignore NotFound errors.
			logAndTrackError(&errList, err, "Failed to delete ManagedServiceAccount vm-actor", "cluster", cluster.GetName())
		}

		// 2b. Delete the ClusterPermission vm-actions.
		err = r.DynamicClient.Resource(clusterPermissionGvr).Namespace(cluster.GetName()).
			Delete(ctx, "vm-actions", metav1.DeleteOptions{})

		if err != nil && !errors.IsNotFound(err) { // Ignore NotFound error.
			logAndTrackError(&errList, err, "Failed to delete ClusterPermission vm-actions", "namespace", cluster.GetName())
		}
	}

	// Combine all errors.
	if len(errList) > 0 {
		err = fmt.Errorf("Failed to disable virtual machine actions. Errors: %v", errList)
		log.Error(err, "Failed to disable virtual machine actions.")
		return err
	}

	log.V(1).Info("Done deleting virtual machine actions configuration resources.")
	return nil
}

// Update flag VIRTUAL_MACHINE_ACTIONS in console config.
// oc patch configmap {name} -n {namespace} -p '{"data": {"VIRTUAL_MACHINE_ACTIONS": "enabled"}}'
func (r *SearchReconciler) updateConsoleConfigVM(ctx context.Context, enabled bool) error {
	name := "console-mce-config"
	namespace, err := r.getMCETargetNamespace(ctx)
	if err != nil {
		log.Error(err, "Failed to get mce installed namespace. Using multicluster-engine as default namespace.")
		namespace = "multicluster-engine"
	}

	consoleConfig, err := r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).
		Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Error getting console configmap", "name", name, "namespace", namespace)
		return fmt.Errorf("Error getting configmap %s in namespace %s", name, namespace)
	}
	existingValue := consoleConfig.Object["data"].(map[string]interface{})["VIRTUAL_MACHINE_ACTIONS"]

	// Update configmap if needed.
	if enabled && existingValue != "enabled" {
		log.V(5).Info("Adding VIRTUAL_MACHINE_ACTIONS to configmap ", "namespace", namespace, "name", name)
		consoleConfig.Object["data"].(map[string]interface{})["VIRTUAL_MACHINE_ACTIONS"] = "enabled"
	} else if !enabled && existingValue == "enabled" {
		log.V(5).Info("Removing VIRTUAL_MACHINE_ACTIONS from configmap", "namespace", namespace, "name", name)
		delete(consoleConfig.Object["data"].(map[string]interface{}), "VIRTUAL_MACHINE_ACTIONS")
	} else {
		log.V(5).Info("VIRTUAL_MACHINE_ACTIONS already set", "name", name, "namespace", namespace, "value", existingValue)
		return nil
	}

	// Write the updated configmap.
	_, err = r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).
		Update(ctx, consoleConfig, metav1.UpdateOptions{})

	return err
}

func (r *SearchReconciler) updateVMStatus(ctx context.Context, instance *searchv1alpha1.Search,
	status metav1.Condition) {
	// Find existing status condition.
	existingConditionIndex := -1
	for i, condition := range instance.Status.Conditions {
		if condition.Type == "VirtualMachineActionsReady" {
			existingConditionIndex = i
			break
		}
	}
	existingConditions := instance.Status.Conditions
	if existingConditionIndex == -1 {
		// Add new condition.
		instance.Status.Conditions = append(instance.Status.Conditions, status)
	} else if existingConditions[existingConditionIndex].Status != status.Status ||
		existingConditions[existingConditionIndex].Reason != status.Reason ||
		existingConditions[existingConditionIndex].Message != status.Message {
		// Update existing condition, only if anything changed.
		instance.Status.Conditions[existingConditionIndex] = status
	} else {
		// Nothing has changed.
		log.V(3).Info("Virtual Machine actions status condition did not change.")
		return
	}

	// write instance with the new status.
	err := r.commitSearchCRInstanceState(ctx, instance)
	if err != nil {
		log.Error(err, "Failed to update status for Search CR instance.")
		return
	}

	log.Info("Successfully updated virtual machine actions status condition in search CR.")
}
