package proxy

import (
	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
)

// Selector 负责按当前配置和协议要求选择上游代理。
type Selector struct {
	storage ports.ProxySelectionStore
	config  config.Provider
}

func NewSelector(s ports.ProxySelectionStore, providers ...config.Provider) *Selector {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &Selector{storage: s, config: provider}
}

// Select 根据使用模式和选择策略获取代理。
//
// protocol 为空时不限制协议；传入 "socks5" 时只选择 SOCKS5 上游。
func (s *Selector) Select(tried []string, protocol string, lowestLatency bool) (*domain.Proxy, error) {
	cfg := s.config.Get()

	sourceFilter := sourceFilterFromMode(cfg.CustomProxyMode)

	// 混用 + 优先模式：先尝试优先源，无可用则 fallback 到全部。
	if cfg.CustomProxyMode == "mixed" && (cfg.CustomPriority || cfg.CustomFreePriority) {
		preferSource := "custom"
		if cfg.CustomFreePriority {
			preferSource = "free"
		}
		if p, err := s.selectFiltered(tried, protocol, lowestLatency, preferSource); err == nil {
			return p, nil
		}
		return s.selectFiltered(tried, protocol, lowestLatency, "")
	}

	return s.selectFiltered(tried, protocol, lowestLatency, sourceFilter)
}

func (s *Selector) selectFiltered(tried []string, protocol string, lowestLatency bool, sourceFilter string) (*domain.Proxy, error) {
	if protocol != "" {
		if lowestLatency {
			return s.storage.GetLowestLatencyByProtocolExcludeFiltered(protocol, tried, sourceFilter)
		}
		return s.storage.GetRandomByProtocolExcludeFiltered(protocol, tried, sourceFilter)
	}

	if lowestLatency {
		return s.storage.GetLowestLatencyExcludeFiltered(tried, sourceFilter)
	}
	return s.storage.GetRandomExcludeFiltered(tried, sourceFilter)
}

// sourceFilterFromMode 根据使用模式返回来源过滤值。
func sourceFilterFromMode(mode string) string {
	switch mode {
	case "custom_only":
		return "custom"
	case "free_only":
		return "free"
	default:
		return "" // mixed
	}
}
