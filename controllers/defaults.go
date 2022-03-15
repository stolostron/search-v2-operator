// Copyright Contributors to the Open Cluster Management project
package controllers

const (
	default_API_CPURequest    = "25m"
	default_API_MemoryLimit   = "1Gi"
	default_API_MemoryRequest = "1Gi"

	default_Indexer_CPURequest    = "25m"
	default_Indexer_MemoryLimit   = "1Gi"
	default_Indexer_MemoryRequest = "32Mi"

	default_Collector_CPURequest    = "25m"
	default_Collector_MemoryLimit   = "768Mi"
	default_Collector_MemoryRequest = "64Mi"

	default_Postgres_CPURequest    = "25m"
	default_Postgres_MemoryLimit   = "4Gi"
	default_Postgres_MemoryRequest = "128Mi"

	default_API_Replicas       = 2
	default_Indexer_Replicas   = 1
	default_Collector_Replicas = 1
	default_Postgres_Replicas  = 1
)

var defaultResoureMap map[string]map[string]string
var defaultReplicaMap map[string]int32

func init() {
	log.Info("Initializing default values")
	apiResourceMap := map[string]string{
		"CPURequest":    default_API_CPURequest,
		"MemoryLimit":   default_API_MemoryLimit,
		"MemoryRequest": default_API_MemoryRequest,
	}
	indexerResourceMap := map[string]string{
		"CPURequest":    default_Indexer_CPURequest,
		"MemoryLimit":   default_Indexer_MemoryLimit,
		"MemoryRequest": default_Indexer_MemoryRequest,
	}
	collectorResourceMap := map[string]string{
		"CPURequest":    default_Collector_CPURequest,
		"MemoryLimit":   default_Collector_MemoryLimit,
		"MemoryRequest": default_Collector_MemoryRequest,
	}
	postgresResourceMap := map[string]string{
		"CPURequest":    default_Postgres_CPURequest,
		"MemoryLimit":   default_Postgres_MemoryLimit,
		"MemoryRequest": default_Postgres_MemoryRequest,
	}
	defaultResoureMap = map[string]map[string]string{
		apiDeploymentName:       apiResourceMap,
		collectorDeploymentName: collectorResourceMap,
		indexerDeploymentName:   indexerResourceMap,
		postgresDeploymentName:  postgresResourceMap,
	}
	defaultReplicaMap = map[string]int32{
		apiDeploymentName:       default_API_Replicas,
		collectorDeploymentName: default_Collector_Replicas,
		indexerDeploymentName:   default_Indexer_Replicas,
		postgresDeploymentName:  default_Postgres_Replicas,
	}
}
