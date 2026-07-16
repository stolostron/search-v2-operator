// Copyright Contributors to the Open Cluster Management project

// Package integrationconfigs embeds the initial integration team CollectorConfig manifests
// shipped in integration_collector_configs/. See ACM-37052.
//
// Integration teams (CNV, OLM, GRC, Kyverno, Gatekeeper, Argo, ACM app lifecycle) contribute a
// plain CollectorConfig YAML file to that directory instead of writing Go code. The operator
// embeds these files at build time and creates/updates the corresponding CRs on the cluster —
// see controllers/create_integration_collectorconfigs.go for the reconcile logic.
package integrationconfigs

import "embed"

//go:embed integration_collector_configs/*.yaml
var FS embed.FS

// Dir is the embedded directory name, used by callers to build paths into FS.
const Dir = "integration_collector_configs"
