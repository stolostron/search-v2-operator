// Copyright Contributors to the Open Cluster Management project
package controllers

func generateLabels(key, val string) map[string]string {
	all_vals := map[string]string{
		"component": "search-v2-operator",
	}
	all_vals[key] = val
	return all_vals
}
