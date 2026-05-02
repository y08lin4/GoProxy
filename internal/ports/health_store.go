package ports

import "goproxy/internal/domain"

// HealthCheckStore captures persistence operations required by health checks.
type HealthCheckStore interface {
	GetQualityDistribution() (map[string]int, error)
	GetBatchForHealthCheck(batchSize int, skipSGrade bool) ([]domain.Proxy, error)
	UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error
	IncrementFailCount(address string) error
	DisableProxy(address string) error
	Delete(address string) error
}
