// Copyright Contributors to the Open Cluster Management project
package controllers

func getLabels(val string) map[string]string {
	all_vals := map[string]string{
		"component": "search-v2-operator",
	}
	all_vals["name"] = val
	return all_vals
}
