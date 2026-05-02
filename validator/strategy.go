package validator

import (
	"context"
	"net/http"
	"time"

	"goproxy/internal/domain"
)

type validationState struct {
	ctx          context.Context
	proxy        domain.Proxy
	client       *http.Client
	statusCode   int
	latency      time.Duration
	exitIP       string
	exitLocation string
	ipInfo       domain.IPInfo
}

type validationStrategy interface {
	Name() string
	Apply(state *validationState) bool
}

type validationStrategyFunc struct {
	name string
	fn   func(state *validationState) bool
}

func (s validationStrategyFunc) Name() string {
	return s.name
}

func (s validationStrategyFunc) Apply(state *validationState) bool {
	return s.fn(state)
}

func namedValidationStrategy(name string, fn func(state *validationState) bool) validationStrategy {
	return validationStrategyFunc{name: name, fn: fn}
}

func (v *Validator) buildValidationStrategies() []validationStrategy {
	return []validationStrategy{
		v.statusCodeStrategy(),
		v.maxResponseStrategy(),
		v.exitInfoStrategy(),
		v.geoPolicyStrategy(),
		v.httpConnectStrategy(),
	}
}

func (v *Validator) runValidationStrategies(state *validationState) bool {
	for _, strategy := range v.strategies {
		if !strategy.Apply(state) {
			return false
		}
	}
	return true
}

func (v *Validator) statusCodeStrategy() validationStrategy {
	return namedValidationStrategy("status-code", func(state *validationState) bool {
		return state.statusCode == http.StatusOK || state.statusCode == http.StatusNoContent
	})
}

func (v *Validator) maxResponseStrategy() validationStrategy {
	return namedValidationStrategy("max-response", func(state *validationState) bool {
		if v.maxResponseMs <= 0 {
			return true
		}
		return state.latency <= time.Duration(v.maxResponseMs)*time.Millisecond
	})
}

func (v *Validator) exitInfoStrategy() validationStrategy {
	return namedValidationStrategy("exit-info", func(state *validationState) bool {
		select {
		case <-state.ctx.Done():
			return false
		default:
		}

		if v.geoIP == nil {
			return false
		}
		exitIP, exitLocation, ipInfo := v.geoIP.Resolve(state.ctx, state.client)
		state.exitIP = exitIP
		state.exitLocation = exitLocation
		state.ipInfo = ipInfo
		return exitIP != "" && exitLocation != ""
	})
}

func (v *Validator) geoPolicyStrategy() validationStrategy {
	return namedValidationStrategy("geo-policy", func(state *validationState) bool {
		if v.cfg == nil || len(state.exitLocation) < 2 {
			return true
		}
		countryCode := state.exitLocation[:2]

		if len(v.cfg.AllowedCountries) > 0 {
			for _, allowed := range v.cfg.AllowedCountries {
				if countryCode == allowed {
					return true
				}
			}
			return false
		}

		for _, blocked := range v.cfg.BlockedCountries {
			if countryCode == blocked {
				return false
			}
		}
		return true
	})
}

func (v *Validator) httpConnectStrategy() validationStrategy {
	return namedValidationStrategy("http-connect", func(state *validationState) bool {
		if state.proxy.Protocol != "http" {
			return true
		}
		checker := v.httpConnectChecker
		if checker == nil {
			checker = checkHTTPSConnectContext
		}
		return checker(state.ctx, state.proxy.Address, v.timeout)
	})
}
