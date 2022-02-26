// Copyright Contributors to the Open Cluster Management project
package controllers

func generateLabels(key, val string) map[string]string {
	allLabels := map[string]string{
		"component": "search-v2-operator",
	}
	allLabels[key] = val
	return allLabels
}

func getServiceAccountName() string {
	return "search-serviceaccount"
}

func getImagePullSecret() string {
	return "search-pull-secret"
}
func getRoleName() string {
	return "search"
}
func getRoleBindingName() string {
	return "search"
}
