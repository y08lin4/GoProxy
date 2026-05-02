package ports

import "goproxy/internal/domain"

// ProxyAdminStore captures proxy operations needed by the WebUI admin service.
type ProxyAdminStore interface {
	Count() (int, error)
	CountByProtocol(protocol string) (int, error)
	CountBySource(source string) (int, error)
	GetQualityDistribution() (map[string]int, error)
	ListProxyPage(protocol string, country string, page int, pageSize int) ([]domain.Proxy, int, error)
	ListProxyCountries(protocol string) ([]string, error)
	GetByProtocol(protocol string) ([]domain.Proxy, error)
	GetAll() ([]domain.Proxy, error)
	GetProxyByAddress(address string) (*domain.Proxy, error)
	Delete(address string) error
	DisableProxy(address string) error
	UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error
}
