// Copyright Contributors to the Open Cluster Management project
package controllers

const (
	default_API_CPURequest    = "10m"
	default_API_MemoryRequest = "512Mi"

	default_Indexer_CPURequest    = "10m"
	default_Indexer_MemoryRequest = "32Mi"

	default_Collector_CPURequest    = "25m"
	default_Collector_MemoryRequest = "128Mi"

	default_Postgres_CPURequest             = "25m"
	default_Postgres_MemoryLimit            = "4Gi"
	default_Postgres_MemoryRequest          = "1Gi"
	default_Postgres_SharedMemory           = "1Gi"  // Container MemoryLimit * 0.25
	default_POSTGRESQL_EFFECTIVE_CACHE_SIZE = "2GB"  // Container MemoryLimit * 0.5
	default_POSTGRESQL_SHARED_BUFFERS       = "1GB"  // Container MemoryLimit * 0.25
	default_WORK_MEM                        = "64MB" // Container MemoryLimit * 0.25 / max_connections

	default_API_Replicas       = 1
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
		"MemoryRequest": default_API_MemoryRequest,
	}
	indexerResourceMap := map[string]string{
		"CPURequest":    default_Indexer_CPURequest,
		"MemoryRequest": default_Indexer_MemoryRequest,
	}
	collectorResourceMap := map[string]string{
		"CPURequest":    default_Collector_CPURequest,
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
