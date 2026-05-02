package ports

import "goproxy/internal/domain"

// ProxyPoolStore captures the proxy persistence operations required by pool management.
//
// It keeps pool decisions independent from the concrete SQLite storage implementation.
type ProxyPoolStore interface {
	Count() (int, error)
	CountByProtocol(protocol string) (int, error)
	CountBySource(source string) (int, error)
	GetAverageLatency(protocol string) (int, error)

	AddProxy(address, protocol string) error
	AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error
	UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error

	GetWorstProxies(protocol string, limit int) ([]domain.Proxy, error)
	ReplaceProxy(oldAddress string, newProxy domain.Proxy) error
}
