package ports

import "goproxy/internal/domain"

// ProxySelectionStore captures read operations needed to choose an upstream proxy.
type ProxySelectionStore interface {
	GetLowestLatencyByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*domain.Proxy, error)
	GetRandomByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*domain.Proxy, error)
	GetLowestLatencyExcludeFiltered(excludes []string, sourceFilter string) (*domain.Proxy, error)
	GetRandomExcludeFiltered(excludes []string, sourceFilter string) (*domain.Proxy, error)
}

// ProxyUsageStore captures write operations needed to report upstream proxy usage.
type ProxyUsageStore interface {
	RecordProxyUse(address string, success bool) error
	DisableProxy(address string) error
	Delete(address string) error
}

// ProxyRuntimeStore is the complete persistence boundary required by proxy servers.
type ProxyRuntimeStore interface {
	ProxySelectionStore
	ProxyUsageStore
}
