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
	CONDITION_VM_ACTIONS  = "VirtualMachineActionsReady"
	clusterPermissionName = "virtual-machine-actions"
	managedSAName         = "vm-actor"
)

var (
	clusterPermissionGvr = schema.GroupVersionResource{
		Group:    "rbac.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "clusterpermissions",
	}
	appSearchVMLabels = map[string]interface{}{
		"app":     "search",
		"feature": "virtual-machine",
	}
)

// Reconcile configuration for virtual machine actions.
//  1. Check the virtual-machine-preview=true annotation.
//  2. Validate dependencies.
//     a. ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//     b. ClusterProxy addon is enabled in the MultiClusterEngine CR.
//  3. Enable virtual machine actions in the console-mce-config.
//  4. Create a ManagedServiceAccount for each managed cluster.
//  5. Create a ClusterPermission for each managed cluster.
func (r *SearchReconciler) reconcileVirtualMachineConfiguration(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {

	if instance.ObjectMeta.Annotations["virtual-machine-preview"] == "true" { //nolint:staticcheck // "could remove embedded field 'ObjectMeta' from selector
		log.V(1).Info("The virtual-machine-preview=true annotation is present. Updating configuration.")

		err := r.validateVirtualMachineDependencies(ctx)
		if err != nil {
			log.Error(err, "Failed to validate dependencies for virtual machine actions.")
			return &reconcile.Result{}, err
		}

		err = r.configureVirtualMachineActions(ctx)
		if err != nil {
			log.Error(err, "Failed to configure virtual machine actions.")
			r.updateStatusCondition(ctx, instance, metav1.Condition{
				Type:               CONDITION_VM_ACTIONS,
				Status:             metav1.ConditionFalse,
				Reason:             "ConfigurationError",
				Message:            "Failed to configure virtual machine actions.",
				LastTransitionTime: metav1.Now(),
			})
			return &reconcile.Result{}, err
		}

		r.updateStatusCondition(ctx, instance, metav1.Condition{
			Type:               CONDITION_VM_ACTIONS,
			Status:             metav1.ConditionTrue,
			Reason:             "None",
			Message:            "Virtual machine actions are enabled.",
			LastTransitionTime: metav1.Now(),
		})

	} else {
		log.V(3).Info("The virtual-machine-preview annotation is false or not present.")

		// Use the status conditions to determine if VM actions was enabled before this reconcile.
		vmActionsConditionIndex := -1
		for index, condition := range instance.Status.Conditions {
			if condition.Type == CONDITION_VM_ACTIONS {
				vmActionsConditionIndex = index
				break
			}
		}
		if vmActionsConditionIndex > -1 {
			log.V(1).Info("The virtual-machine-preview annotation was removed. Removing configuration.")
			err := r.disableVirtualMachineActions(ctx)
			if err != nil {
				log.Error(err, "Failed to disable virtual machine actions.")
				r.updateStatusCondition(ctx, instance, metav1.Condition{
					Type:               CONDITION_VM_ACTIONS,
					Status:             metav1.ConditionFalse,
					Reason:             "ConfigurationError",
					Message:            "Failed to remove configuration for virtual machine actions.",
					LastTransitionTime: metav1.Now(),
				})
				return &reconcile.Result{}, err
			}

			// Remove the status condition.
			instance.Status.Conditions = append(instance.Status.Conditions[:vmActionsConditionIndex],
				instance.Status.Conditions[vmActionsConditionIndex+1:]...)

			err = r.commitSearchCRInstanceState(ctx, instance)
			if err != nil {
				log.Error(err, "Failed to update the VirtualMachineActionsReady status condition.")
				return &reconcile.Result{}, err
			}
		}
	}
	return &reconcile.Result{}, nil
}

// Validate dependencies.
//  1. The ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//  2. The ClusterProxy addon is enabled in the MultiClusterEngine CR.
func (r *SearchReconciler) validateVirtualMachineDependencies(ctx context.Context) error {
	log.V(2).Info("Checking dependencies for virtual machine actions.")

	// Verify that MulticlusterEngine operator is installed and configured.
	mces, err := r.DynamicClient.Resource(multiclusterengineResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil || len(mces.Items) == 0 {
		log.Error(err, "Failed to validate dependency MulticlusterEngine operator.")
		return fmt.Errorf("failed to validate dependency MulticlusterEngine operator")
	} else {
		log.V(5).Info("Found MulticlusterEngine instance.")
	}

	// Verify that the add-ons ManagedServiceAccount and ClusterProxy are enabled.
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
		return fmt.Errorf("the Managed Service Account add-on is not enabled in MulticlusterEngine")
	}
	if !clusterProxyEnabled {
		return fmt.Errorf("the Cluster Proxy add-on is not enabled in MulticlusterEngine")
	}
	log.V(2).Info("Validated dependencies for virtual machine actions.")
	return nil
}

// Configure virtual machine actions.
//  1. Enable virtual machine actions feature in the console.
//     Add VIRTUAL_MACHINE_ACTIONS=enabled to configmap console-mce-config in the MCE namespace.
//  2. Create a ManagedServiceAccount for each managed hub.
//  3. Create a ClusterPermission for each managed hub.
func (r *SearchReconciler) configureVirtualMachineActions(ctx context.Context) error {
	errList := []error{} // Using this to allow partial errors and combine at the end.

	// 1. Enable virtual machine actions feature in the console.
	err := r.updateConsoleConfigVM(ctx, true)
	logAndTrackError(&errList, err, "Failed to set VIRTUAL_MACHINE_ACTIONS=enabled in console-mce-config.")

	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	logAndTrackError(&errList, err, "Failed to list the managed clusters while configuring virtual machine actions.")
	if err == nil && clusterList != nil {
		for _, cluster := range clusterList.Items {
			// FUTURE: Check if kubevirt.io is installed in the managed cluster.

			// 2. Create a ManagedServiceAccount
			err = r.createVMManagedServiceAccount(ctx, cluster.GetName())
			logAndTrackError(&errList, err, "Failed to create ManagedServiceAccount",
				"name", managedSAName, "namespace", cluster.GetName())

			// 3. Create a ClusterPermission
			err = r.createVMClusterPermission(ctx, cluster.GetName())
			logAndTrackError(&errList, err, "Failed to create ClusterPermission",
				"name", clusterPermissionName, "namespace", cluster.GetName())
		}
	}

	// Combine all errors.
	if len(errList) > 0 {
		err = fmt.Errorf("failed to configure virtual machine actions. Errors: %v", errList)
		return err
	}

	log.Info("Configured virtual machine actions.")
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
				"name":   managedSAName,
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
		log.V(5).Info("Found existing ManagedServiceAccount", "name", managedSAName, "namespace", cluster)
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
				"name":   clusterPermissionName,
				"labels": appSearchVMLabels,
			},
			"spec": map[string]interface{}{
				"clusterRole": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{
							"apiGroups": []interface{}{"subresources.kubevirt.io"},
							"resources": []interface{}{
								"virtualmachines/start",
								"virtualmachines/stop",
								"virtualmachines/restart",
								"virtualmachineinstances/pause",
								"virtualmachineinstances/unpause",
							},
							"verbs": []interface{}{"update"},
						},
						map[string]interface{}{
							"apiGroups": []interface{}{"snapshot.kubevirt.io"},
							"resources": []interface{}{
								"virtualmachinesnapshots",
							},
							"verbs": []interface{}{"create", "delete"},
						},
						map[string]interface{}{
							"apiGroups": []interface{}{"snapshot.kubevirt.io"},
							"resources": []interface{}{
								"virtualmachinerestores",
							},
							"verbs": []interface{}{"create", "delete"},
						},
					},
				},
				"clusterRoleBinding": map[string]interface{}{
					"subject": map[string]interface{}{
						"kind":      "ServiceAccount",
						"name":      managedSAName,
						"namespace": "open-cluster-management-agent-addon",
					},
				},
			},
		},
	}
	_, err := r.DynamicClient.Resource(clusterPermissionGvr).Namespace(cluster).
		Create(ctx, clusterPermission, metav1.CreateOptions{})

	if err != nil && errors.IsAlreadyExists(err) {
		log.V(5).Info("Found existing ClusterPermission", "name", clusterPermissionName, "namespace", cluster)
		return nil
	}
	return err
}

// Logic to disable virtual machine actions.
//  1. Disable virtual machine actions in the console.
//  2. Delete ManagedServiceAccount for each managed cluster.
//  3. Delete ClusterPermission for each managed cluster.
func (r *SearchReconciler) disableVirtualMachineActions(ctx context.Context) error {
	errList := []error{}

	// 1. Disable virtual machine actions in the console.
	err := r.updateConsoleConfigVM(ctx, false)
	logAndTrackError(&errList, err, "Failed to remove the VIRTUAL_MACHINE_ACTIONS in configmap console-mce-config.")

	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil || clusterList == nil {
		logAndTrackError(&errList, err, "Failed to list the managed clusters while removing virtual machine actions.")
	}
	for _, cluster := range clusterList.Items {
		// 2. Delete the ManagedServiceAccount.
		err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
			Delete(ctx, managedSAName, metav1.DeleteOptions{})

		if err != nil && !errors.IsNotFound(err) { // Ignore NotFound errors.
			logAndTrackError(&errList, err, "Failed to delete ManagedServiceAccount",
				"name", managedSAName, "namespace", cluster.GetName())
		}

		// 2. Delete the ClusterPermission.
		err = r.DynamicClient.Resource(clusterPermissionGvr).Namespace(cluster.GetName()).
			Delete(ctx, clusterPermissionName, metav1.DeleteOptions{})

		if err != nil && !errors.IsNotFound(err) { // Ignore NotFound error.
			logAndTrackError(&errList, err, "Failed to delete ClusterPermission.",
				"name", clusterPermissionName, "namespace", cluster.GetName())
		}
	}

	// Combine all errors.
	if len(errList) > 0 {
		err = fmt.Errorf("failed to disable virtual machine actions. Errors: %v", errList)
		return err
	}

	log.V(1).Info("Removed configuration for virtual machine actions.")
	return nil
}

// Update flag VIRTUAL_MACHINE_ACTIONS in console-mce-config.
// oc patch configmap {name} -n {namespace} -p '{"data": {"VIRTUAL_MACHINE_ACTIONS": "enabled"}}'
func (r *SearchReconciler) updateConsoleConfigVM(ctx context.Context, enabled bool) error {
	key := "VIRTUAL_MACHINE_ACTIONS"
	name := "console-mce-config"
	namespace, err := r.getMCETargetNamespace(ctx)
	if err != nil {
		log.Error(err, "Failed to get the mce installed namespace. Using multicluster-engine as default namespace.")
		namespace = "multicluster-engine"
	}

	consoleConfig, err := r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).
		Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Error getting console configmap", "name", name, "namespace", namespace)
		return fmt.Errorf("error getting configmap %s in namespace %s", name, namespace)
	}
	existingValue := consoleConfig.Object["data"].(map[string]interface{})[key]

	// Update configmap if needed.
	if enabled && existingValue != "enabled" {
		log.V(5).Info("Adding key to configmap", "key", key, "namespace", namespace, "name", name)
		consoleConfig.Object["data"].(map[string]interface{})[key] = "enabled"
	} else if !enabled && existingValue == "enabled" {
		log.V(5).Info("Removing key from configmap", "key", key, "namespace", namespace, "name", name)
		delete(consoleConfig.Object["data"].(map[string]interface{}), key)
	} else {
		log.V(5).Info("Key already set in configmap", "name", name, "namespace", namespace, "key", key, "value", existingValue)
		return nil
	}

	// Write the updated configmap.
	_, err = r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).
		Update(ctx, consoleConfig, metav1.UpdateOptions{})

	return err
}

func (r *SearchReconciler) updateStatusCondition(ctx context.Context, instance *searchv1alpha1.Search,
	status metav1.Condition) {
	// Find existing status condition.
	existingConditionIndex := -1
	for i, condition := range instance.Status.Conditions {
		if condition.Type == status.Type {
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
		return
	}

	// write instance with the new status.
	err := r.commitSearchCRInstanceState(ctx, instance)
	if err != nil {
		log.Error(err, "Failed to update status condition in search instance.", "type", status.Type)
		return
	}

	log.Info("Successfully updated status condition in search instance.", "type", status.Type)
}
