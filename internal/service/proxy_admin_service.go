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

func NewProxyAdminService(store ports.ProxyAdminStore, geoIP ports.GeoIPResolver, providers ...config.Provider) *ProxyAdminService {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &ProxyAdminService{store: store, geoIP: geoIP, config: provider}
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

func (s *ProxyAdminService) List(protocol string) ([]domain.Proxy, error) {
	if protocol != "" {
		return s.store.GetByProtocol(protocol)
	}
	return s.store.GetAll()
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
