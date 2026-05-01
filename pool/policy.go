package pool

import (
	"goproxy/config"
	"goproxy/internal/domain"
)

// Policy contains pure pool-capacity and replacement decisions.
type Policy struct {
	cfg *config.Config
}

func NewPolicy(cfg *config.Config) *Policy {
	return &Policy{cfg: cfg}
}

func (p *Policy) DetermineState(total, httpCount, socks5Count int) string {
	httpSlots, socks5Slots := p.cfg.CalculateSlots()

	if httpCount == 0 || socks5Count == 0 {
		return "emergency"
	}

	emergencyThreshold := int(float64(p.cfg.PoolMaxSize) * 0.1)
	if total < emergencyThreshold {
		return "emergency"
	}

	if httpCount < int(float64(httpSlots)*0.2) || socks5Count < int(float64(socks5Slots)*0.2) {
		return "critical"
	}

	healthyThreshold := int(float64(p.cfg.PoolMaxSize) * 0.95)
	if total < healthyThreshold {
		return "warning"
	}

	return "healthy"
}

func (p *Policy) NeedsFetch(status *domain.PoolStatus) (bool, string, string) {
	if status.HTTP == 0 {
		return true, "emergency", "http"
	}
	if status.SOCKS5 == 0 {
		return true, "emergency", "socks5"
	}

	if status.State == "emergency" {
		return true, "emergency", ""
	}

	if status.State == "critical" || status.State == "warning" {
		httpPct := safeRatio(status.HTTP, status.HTTPSlots)
		socks5Pct := safeRatio(status.SOCKS5, status.SOCKS5Slots)

		if httpPct < 0.5 && socks5Pct < 0.5 {
			return true, "refill", ""
		}
		if httpPct < 0.5 {
			return true, "refill", "http"
		}
		if socks5Pct < 0.5 {
			return true, "refill", "socks5"
		}
		return true, "refill", ""
	}

	return false, "", ""
}

func (p *Policy) SlotDecision(protocol string, httpCount, socks5Count, total int) (maxSlots int, currentCount int, allowDirect bool, allowFloat bool, shouldReplace bool) {
	httpSlots, socks5Slots := p.cfg.CalculateSlots()
	if protocol == "http" {
		maxSlots = httpSlots
		currentCount = httpCount
	} else {
		maxSlots = socks5Slots
		currentCount = socks5Count
	}

	if currentCount < maxSlots {
		return maxSlots, currentCount, true, false, false
	}

	allowedFloat := int(float64(maxSlots) * 0.1)
	if total < p.cfg.PoolMaxSize && currentCount < maxSlots+allowedFloat {
		return maxSlots, currentCount, false, true, false
	}

	if currentCount >= maxSlots || total >= p.cfg.PoolMaxSize {
		return maxSlots, currentCount, false, false, true
	}

	return maxSlots, currentCount, false, false, false
}

func (p *Policy) ShouldReplace(newProxy, worst domain.Proxy) bool {
	return float64(newProxy.Latency) < float64(worst.Latency)*p.cfg.ReplaceThreshold
}

func safeRatio(count, slots int) float64 {
	if slots <= 0 {
		return 1
	}
	return float64(count) / float64(slots)
}
