package validator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"goproxy/config"
	"goproxy/internal/domain"
)

type fakeGeoResolver struct {
	exitIP       string
	exitLocation string
	ipInfo       domain.IPInfo
}

func (r fakeGeoResolver) Resolve(ctx context.Context, client *http.Client) (string, string, domain.IPInfo) {
	return r.exitIP, r.exitLocation, r.ipInfo
}

func newTestValidator(cfg *config.Config, resolver fakeGeoResolver) *Validator {
	v := &Validator{
		timeout:            time.Second,
		maxResponseMs:      cfg.MaxResponseMs,
		cfg:                cfg,
		geoIP:              resolver,
		httpConnectChecker: func(ctx context.Context, proxyAddr string, timeout time.Duration) bool { return true },
	}
	v.strategies = v.buildValidationStrategies()
	return v
}

func TestValidationStrategyChainStopsBeforeExitInfoOnSlowResponse(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxResponseMs = 10

	v := newTestValidator(cfg, fakeGeoResolver{
		exitIP:       "1.1.1.1",
		exitLocation: "JP Tokyo",
	})

	state := &validationState{
		ctx:        context.Background(),
		proxy:      domain.Proxy{Address: "1.1.1.1:80", Protocol: "socks5"},
		client:     &http.Client{},
		statusCode: http.StatusNoContent,
		latency:    100 * time.Millisecond,
	}

	if v.runValidationStrategies(state) {
		t.Fatal("expected slow response to fail validation strategies")
	}
	if state.exitIP != "" || state.exitLocation != "" {
		t.Fatal("exit info should not be resolved after max-response strategy failure")
	}
}

func TestValidationStrategyChainRejectsBlockedCountry(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxResponseMs = 0
	cfg.AllowedCountries = nil
	cfg.BlockedCountries = []string{"US"}

	v := newTestValidator(cfg, fakeGeoResolver{
		exitIP:       "2.2.2.2",
		exitLocation: "US New York",
	})

	state := &validationState{
		ctx:        context.Background(),
		proxy:      domain.Proxy{Address: "2.2.2.2:80", Protocol: "socks5"},
		client:     &http.Client{},
		statusCode: http.StatusOK,
		latency:    20 * time.Millisecond,
	}

	if v.runValidationStrategies(state) {
		t.Fatal("expected blocked country to fail validation strategies")
	}
	if state.exitIP == "" || state.exitLocation == "" {
		t.Fatal("expected exit info strategy to run before geo filter failure")
	}
}

func TestValidationStrategyChainRunsHTTPConnectCheckForHTTPProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxResponseMs = 0
	cfg.BlockedCountries = nil
	cfg.AllowedCountries = []string{"JP"}

	called := false
	v := &Validator{
		timeout:       time.Second,
		maxResponseMs: 0,
		cfg:           cfg,
		geoIP: fakeGeoResolver{
			exitIP:       "3.3.3.3",
			exitLocation: "JP Tokyo",
		},
		httpConnectChecker: func(ctx context.Context, proxyAddr string, timeout time.Duration) bool {
			called = true
			return false
		},
	}
	v.strategies = v.buildValidationStrategies()

	state := &validationState{
		ctx:        context.Background(),
		proxy:      domain.Proxy{Address: "3.3.3.3:8080", Protocol: "http"},
		client:     &http.Client{},
		statusCode: http.StatusOK,
		latency:    20 * time.Millisecond,
	}

	if v.runValidationStrategies(state) {
		t.Fatal("expected failed HTTP CONNECT probe to fail validation strategies")
	}
	if !called {
		t.Fatal("expected HTTP CONNECT strategy to invoke checker")
	}
}
