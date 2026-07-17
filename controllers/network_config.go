// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// networkConfigGVR is the GroupVersionResource for the OpenShift network.config cluster CR.
var networkConfigGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "networks",
}

// GetServiceCIDR reads the cluster service network CIDR from the network.config/cluster CR.
// This is required to build correct ipBlock egress rules in NetworkPolicies for the
// kubernetes.default.svc ClusterIP, which OVN-Kubernetes routes via the service load balancer
// and cannot be matched by pod/namespace selectors.
// Falls back to "172.30.0.0/16" (the default OpenShift service CIDR) on any error.
func GetServiceCIDR(cfg *rest.Config) string {
	const fallback = "172.30.0.0/16"
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "GetServiceCIDR: unable to create dynamic client, using fallback", "cidr", fallback)
		return fallback
	}

	obj, err := dynClient.Resource(networkConfigGVR).Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "GetServiceCIDR: unable to get network.config/cluster, using fallback", "cidr", fallback)
		return fallback
	}

	// spec.serviceNetwork is a []string
	raw, found, err := unstructuredNestedSlice(obj.Object, "spec", "serviceNetwork")
	if err != nil || !found || len(raw) == 0 {
		log.Info("GetServiceCIDR: serviceNetwork not found in network.config, using fallback", "cidr", fallback)
		return fallback
	}

	cidr := fmt.Sprintf("%v", raw[0])
	log.Info("GetServiceCIDR: resolved cluster service CIDR", "cidr", cidr)
	return cidr
}

// unstructuredNestedSlice retrieves a []interface{} from a nested map path.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	m := obj
	for i, f := range fields[:len(fields)-1] {
		v, ok := m[f]
		if !ok {
			return nil, false, nil
		}
		m, ok = v.(map[string]interface{})
		if !ok {
			return nil, false, fmt.Errorf("field %q at index %d is not a map", f, i)
		}
	}
	last := fields[len(fields)-1]
	v, ok := m[last]
	if !ok {
		return nil, false, nil
	}
	// marshal/unmarshal to get []interface{}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false, err
	}
	var result []interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, false, err
	}
	return result, true, nil
}
