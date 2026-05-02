package service

import (
	"log"

	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
	"goproxy/validator"
)

// ProxyAdminService contains WebUI proxy administration use-cases.
type ProxyAdminService struct {
	store  ports.ProxyAdminStore
	geoIP  ports.GeoIPResolver
	config config.Provider
}

const (
	defaultProxyPageSize = 50
	maxProxyPageSize     = 200
)

func NewProxyAdminService(store ports.ProxyAdminStore, geoIP ports.GeoIPResolver, providers ...config.Provider) *ProxyAdminService {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &ProxyAdminService{store: store, geoIP: geoIP, config: provider}
}

func clampProxyPage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	switch {
	case pageSize <= 0:
		pageSize = defaultProxyPageSize
	case pageSize > maxProxyPageSize:
		pageSize = maxProxyPageSize
	}
	return page, pageSize
}

func (s *ProxyAdminService) Stats(proxyPort string) map[string]interface{} {
	total, _ := s.store.Count()
	httpCount, _ := s.store.CountByProtocol("http")
	socks5Count, _ := s.store.CountByProtocol("socks5")
	customCount, _ := s.store.CountBySource("custom")
	return map[string]interface{}{
		"total":        total,
		"http":         httpCount,
		"socks5":       socks5Count,
		"custom_count": customCount,
		"port":         proxyPort,
	}
}

func (s *ProxyAdminService) QualityDistribution() (map[string]int, error) {
	return s.store.GetQualityDistribution()
}

func (s *ProxyAdminService) List(protocol string) ([]domain.Proxy, error) {
	if protocol != "" {
		return s.store.GetByProtocol(protocol)
	}
	return s.store.GetAll()
}

func (s *ProxyAdminService) ListPage(protocol string, country string, page int, pageSize int) (*domain.ProxyPage, error) {
	page, pageSize = clampProxyPage(page, pageSize)

	items, total, err := s.store.ListProxyPage(protocol, country, page, pageSize)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
		if page > totalPages {
			page = totalPages
			items, total, err = s.store.ListProxyPage(protocol, country, page, pageSize)
			if err != nil {
				return nil, err
			}
		}
	}
	countries, err := s.store.ListProxyCountries(protocol)
	if err != nil {
		return nil, err
	}

	return &domain.ProxyPage{
		Items:       items,
		Total:       total,
		Page:        page,
		PageSize:    pageSize,
		TotalPages:  totalPages,
		Protocol:    protocol,
		Country:     country,
		Countries:   countries,
		HasNext:     totalPages > 0 && page < totalPages,
		HasPrevious: page > 1 && totalPages > 0,
	}, nil
}

func (s *ProxyAdminService) Delete(address string) error {
	return s.store.Delete(address)
}

func (s *ProxyAdminService) RefreshProxyAsync(address string) {
	go func() {
		targetProxy, err := s.store.GetProxyByAddress(address)
		if err != nil {
			log.Printf("[webui] proxy refresh skipped, not found: %s", address)
			return
		}

		cfg := s.config.Get()
		v := validator.NewWithGeoIP(1, cfg.ValidateTimeout, cfg.ValidateURL, s.geoIP)
		log.Printf("[webui] refreshing proxy: %s", address)
		valid, latency, exitIP, exitLocation, ipInfo := v.ValidateOne(*targetProxy)
		if valid {
			latencyMs := int(latency.Milliseconds())
			if err := s.store.UpdateExitInfo(address, exitIP, exitLocation, latencyMs, ipInfo); err != nil {
				log.Printf("[webui] proxy refresh update failed: %s: %v", address, err)
				return
			}
			log.Printf("[webui] proxy refreshed: %s latency=%dms", address, latencyMs)
			return
		}

		if targetProxy.Source == "custom" {
			_ = s.store.DisableProxy(address)
			log.Printf("[webui] custom proxy validation failed, disabled: %s", address)
			return
		}
		_ = s.store.Delete(address)
		log.Printf("[webui] proxy validation failed, removed: %s", address)
	}()
}

func (s *ProxyAdminService) RefreshLatencyAsync() {
	go func() {
		log.Println("[webui] refreshing latency for all proxies...")
		proxies, err := s.store.GetAll()
		if err != nil {
			log.Printf("[webui] get proxies error: %v", err)
			return
		}
		if len(proxies) == 0 {
			log.Println("[webui] no proxies to refresh")
			return
		}

		cfg := s.config.Get()
		validate := validator.NewWithGeoIP(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL, s.geoIP)

		log.Printf("[webui] refreshing latency for %d proxies...", len(proxies))
		updated := 0
		for r := range validate.ValidateStream(proxies) {
			if r.Valid {
				latencyMs := int(r.Latency.Milliseconds())
				_ = s.store.UpdateExitInfo(r.Proxy.Address, r.ExitIP, r.ExitLocation, latencyMs, r.IPInfo)
				updated++
				continue
			}
			if r.Proxy.Source == "custom" {
				_ = s.store.DisableProxy(r.Proxy.Address)
			} else {
				_ = s.store.Delete(r.Proxy.Address)
			}
		}
		log.Printf("[webui] latency refresh done: updated=%d", updated)
	}()
}
