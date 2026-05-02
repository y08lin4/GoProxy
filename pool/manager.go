package pool

import (
	"log"

	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
)

// Manager coordinates pool persistence while delegating pure decisions to Policy.
type Manager struct {
	storage ports.ProxyPoolStore
	cfg     *config.Config
	policy  *Policy
}

func NewManager(s ports.ProxyPoolStore, cfg *config.Config) *Manager {
	return &Manager{
		storage: s,
		cfg:     cfg,
		policy:  NewPolicy(cfg),
	}
}

type PoolStatus = domain.PoolStatus

// GetStatus returns the current pool status.
func (m *Manager) GetStatus() (*PoolStatus, error) {
	total, _ := m.storage.Count()
	httpCount, _ := m.storage.CountByProtocol("http")
	socks5Count, _ := m.storage.CountByProtocol("socks5")
	httpSlots, socks5Slots := m.cfg.CalculateSlots()

	avgHTTP, _ := m.storage.GetAverageLatency("http")
	avgSOCKS5, _ := m.storage.GetAverageLatency("socks5")
	state := m.determineState(total, httpCount, socks5Count)
	customCount, _ := m.storage.CountBySource("custom")

	return &PoolStatus{
		Total:            total,
		HTTP:             httpCount,
		SOCKS5:           socks5Count,
		HTTPSlots:        httpSlots,
		SOCKS5Slots:      socks5Slots,
		State:            state,
		AvgLatencyHTTP:   avgHTTP,
		AvgLatencySocks5: avgSOCKS5,
		CustomCount:      customCount,
	}, nil
}

func (m *Manager) determineState(total, httpCount, socks5Count int) string {
	return m.policy.DetermineState(total, httpCount, socks5Count)
}

func (m *Manager) NeedsFetch(status *PoolStatus) (bool, string, string) {
	return m.policy.NeedsFetch(status)
}

func (m *Manager) NeedsFetchQuick(status *PoolStatus) bool {
	need, _, _ := m.NeedsFetch(status)
	return need
}

// TryAddProxy tries to add a validated proxy to the pool or replace a worse one.
func (m *Manager) TryAddProxy(p domain.Proxy) (bool, string) {
	// Custom subscription proxies bypass free-proxy slot limits.
	if p.Source == "custom" {
		if err := m.storage.AddProxyWithSource(p.Address, p.Protocol, "custom"); err != nil {
			return false, "db_error"
		}
		m.storage.UpdateExitInfo(p.Address, p.ExitIP, p.ExitLocation, p.Latency, p.IPInfo)
		log.Printf("[pool] ✅ 订阅代理入池: %s (%s) %dms %s", p.Address, p.Protocol, p.Latency, p.ExitLocation)
		return true, "added_custom"
	}

	httpCount, _ := m.storage.CountByProtocol("http")
	socks5Count, _ := m.storage.CountByProtocol("socks5")
	total, _ := m.storage.Count()
	maxSlots, currentCount, allowDirect, allowFloat, shouldReplace := m.policy.SlotDecision(p.Protocol, httpCount, socks5Count, total)

	if allowDirect {
		if err := m.storage.AddProxy(p.Address, p.Protocol); err != nil {
			return false, "db_error"
		}
		m.storage.UpdateExitInfo(p.Address, p.ExitIP, p.ExitLocation, p.Latency, p.IPInfo)
		log.Printf("[pool] ✅ 直接入池: %s (%s %d/%d) %dms %s %s",
			p.Address, p.Protocol, currentCount+1, maxSlots, p.Latency, p.ExitIP, p.ExitLocation)
		return true, "added"
	}

	if allowFloat {
		allowedFloat := int(float64(maxSlots) * 0.1)
		if err := m.storage.AddProxy(p.Address, p.Protocol); err != nil {
			return false, "db_error"
		}
		m.storage.UpdateExitInfo(p.Address, p.ExitIP, p.ExitLocation, p.Latency, p.IPInfo)
		log.Printf("[pool] ✅ 浮动入池: %s (%s %d/%d+%d) %dms",
			p.Address, p.Protocol, currentCount+1, maxSlots, allowedFloat, p.Latency)
		return true, "added_float"
	}

	if shouldReplace {
		return m.tryReplace(p)
	}

	return false, "slots_full"
}

func (m *Manager) tryReplace(newProxy domain.Proxy) (bool, string) {
	candidates, err := m.storage.GetWorstProxies(newProxy.Protocol, 10)
	if err != nil || len(candidates) == 0 {
		return false, "no_candidates"
	}

	worst := candidates[0]
	if m.policy.ShouldReplace(newProxy, worst) {
		if err := m.storage.ReplaceProxy(worst.Address, newProxy); err != nil {
			return false, "replace_error"
		}
		log.Printf("[pool] 🔄 替换: %s(%dms) -> %s(%dms) 提升%.0f%%",
			worst.Address, worst.Latency, newProxy.Address, newProxy.Latency,
			(1-float64(newProxy.Latency)/float64(worst.Latency))*100)
		return true, "replaced"
	}

	return false, "not_better"
}

// AdjustForConfigChange logs slot changes after pool-size/ratio updates.
func (m *Manager) AdjustForConfigChange(oldSize int, oldRatio float64) {
	newHTTP, newSOCKS5 := m.cfg.CalculateSlots()
	oldHTTP := int(float64(oldSize) * oldRatio)
	oldSOCKS5 := oldSize - oldHTTP

	log.Printf("[pool] 配置变更: 容量 %d->%d, HTTP槽位 %d->%d, SOCKS5槽位 %d->%d",
		oldSize, m.cfg.PoolMaxSize, oldHTTP, newHTTP, oldSOCKS5, newSOCKS5)

	httpCount, _ := m.storage.CountByProtocol("http")
	socks5Count, _ := m.storage.CountByProtocol("socks5")

	if httpCount > newHTTP {
		excess := httpCount - newHTTP
		log.Printf("[pool] HTTP 超标 %d 个，标记为替换候选", excess)
	}

	if socks5Count > newSOCKS5 {
		excess := socks5Count - newSOCKS5
		log.Printf("[pool] SOCKS5 超标 %d 个，标记为替换候选", excess)
	}
}
