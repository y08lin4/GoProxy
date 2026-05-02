package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goproxy/config"
	"goproxy/internal/domain"
	appservice "goproxy/internal/service"
)

type fakeWebUIProxyAdminStore struct {
	page      domain.ProxyPage
	protocols []string
	countries []string
}

func (f *fakeWebUIProxyAdminStore) Count() (int, error)                          { return 0, nil }
func (f *fakeWebUIProxyAdminStore) CountByProtocol(protocol string) (int, error) { return 0, nil }
func (f *fakeWebUIProxyAdminStore) CountBySource(source string) (int, error)     { return 0, nil }
func (f *fakeWebUIProxyAdminStore) GetQualityDistribution() (map[string]int, error) {
	return map[string]int{}, nil
}
func (f *fakeWebUIProxyAdminStore) GetByProtocol(protocol string) ([]domain.Proxy, error) {
	return nil, nil
}
func (f *fakeWebUIProxyAdminStore) GetAll() ([]domain.Proxy, error) { return nil, nil }
func (f *fakeWebUIProxyAdminStore) GetProxyByAddress(address string) (*domain.Proxy, error) {
	return nil, nil
}
func (f *fakeWebUIProxyAdminStore) Delete(address string) error { return nil }
func (f *fakeWebUIProxyAdminStore) DisableProxy(address string) error {
	return nil
}
func (f *fakeWebUIProxyAdminStore) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error {
	return nil
}
func (f *fakeWebUIProxyAdminStore) ListProxyPage(protocol string, country string, page int, pageSize int) ([]domain.Proxy, int, error) {
	f.page.Protocol = protocol
	f.page.Country = country
	f.page.Page = page
	f.page.PageSize = pageSize
	return f.page.Items, f.page.Total, nil
}
func (f *fakeWebUIProxyAdminStore) ListProxyCountries(protocol string) ([]string, error) {
	return f.countries, nil
}

func adminCookie(t *testing.T) *http.Cookie {
	t.Helper()
	resetSessionsForTest()
	token, err := newSession()
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	return &http.Cookie{Name: "session", Value: token}
}

func TestAPIProxiesReturnsPaginatedPayload(t *testing.T) {
	cfg := config.DefaultConfig()
	store := &fakeWebUIProxyAdminStore{
		page: domain.ProxyPage{
			Items: []domain.Proxy{{Address: "1.1.1.1:80", Protocol: "http"}},
			Total: 40,
		},
		countries: []string{"JP", "US"},
	}
	proxyAdmin := appservice.NewProxyAdminService(store, nil, config.StaticProvider{Config: cfg})
	srv := New(cfg, nil, proxyAdmin, nil, nil, nil, nil, config.StaticProvider{Config: cfg})

	req := httptest.NewRequest(http.MethodGet, "/api/proxies?protocol=http&country=JP&page=2&page_size=20", nil)
	rr := httptest.NewRecorder()
	srv.apiProxies(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var page domain.ProxyPage
	if err := json.NewDecoder(rr.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if page.Protocol != "http" || page.Country != "JP" || page.Page != 2 || page.PageSize != 20 || page.TotalPages != 2 {
		t.Fatalf("unexpected page metadata: %#v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Address != "1.1.1.1:80" {
		t.Fatalf("unexpected page items: %#v", page.Items)
	}
	if len(page.Countries) != 2 || page.Countries[0] != "JP" {
		t.Fatalf("unexpected countries: %#v", page.Countries)
	}
}

func TestAPIConfigIncludesSourceSettings(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.Load()
	cfg.ExtraSources = []domain.FetchSourceConfig{{URL: "https://example.com/http.txt", Protocol: "http", Group: "slow"}}
	cfg.DisabledSourceURLs = []string{"https://example.com/disabled.txt"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	srv := New(cfg, nil, nil, nil, nil, nil, nil, config.GlobalProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	srv.apiConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	var body struct {
		ExtraSources       []domain.FetchSourceConfig `json:"extra_sources"`
		DisabledSourceURLs []string                   `json:"disabled_source_urls"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.ExtraSources) != 1 || body.ExtraSources[0].URL != "https://example.com/http.txt" {
		t.Fatalf("unexpected extra sources: %#v", body.ExtraSources)
	}
	if len(body.DisabledSourceURLs) != 1 || body.DisabledSourceURLs[0] != "https://example.com/disabled.txt" {
		t.Fatalf("unexpected disabled source URLs: %#v", body.DisabledSourceURLs)
	}
}

func TestAPIConfigSavePersistsSourceSettingsAndSignalsChange(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.Load()
	configChanged := make(chan struct{}, 1)
	srv := New(cfg, nil, nil, nil, nil, nil, configChanged, config.GlobalProvider{})

	payload := map[string]interface{}{
		"pool_max_size":             cfg.PoolMaxSize,
		"pool_http_ratio":           cfg.PoolHTTPRatio,
		"pool_min_per_protocol":     cfg.PoolMinPerProtocol,
		"max_latency_ms":            cfg.MaxLatencyMs,
		"max_latency_emergency":     cfg.MaxLatencyEmergency,
		"max_latency_healthy":       cfg.MaxLatencyHealthy,
		"validate_concurrency":      cfg.ValidateConcurrency,
		"validate_timeout":          cfg.ValidateTimeout,
		"health_check_interval":     cfg.HealthCheckInterval,
		"health_check_batch_size":   cfg.HealthCheckBatchSize,
		"optimize_interval":         cfg.OptimizeInterval,
		"replace_threshold":         cfg.ReplaceThreshold,
		"blocked_countries":         cfg.BlockedCountries,
		"allowed_countries":         cfg.AllowedCountries,
		"custom_proxy_mode":         cfg.CustomProxyMode,
		"custom_priority":           cfg.CustomPriority,
		"custom_free_priority":      cfg.CustomFreePriority,
		"custom_probe_interval":     cfg.CustomProbeInterval,
		"custom_refresh_interval":   cfg.CustomRefreshInterval,
		"extra_sources":             []map[string]string{{"group": "fast", "protocol": "socks5", "url": "https://example.com/socks5.txt"}},
		"disabled_source_urls":      []string{"https://example.com/disabled.txt"},
		"max_candidates_per_source": cfg.MaxCandidatesPerSource,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/config/save", bytes.NewReader(data))
	req.AddCookie(adminCookie(t))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.authMiddleware(srv.apiConfigSave).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	updated := config.Get()
	if len(updated.ExtraSources) != 1 || updated.ExtraSources[0].URL != "https://example.com/socks5.txt" {
		t.Fatalf("unexpected saved extra sources: %#v", updated.ExtraSources)
	}
	if len(updated.DisabledSourceURLs) != 1 || updated.DisabledSourceURLs[0] != "https://example.com/disabled.txt" {
		t.Fatalf("unexpected saved disabled source URLs: %#v", updated.DisabledSourceURLs)
	}

	select {
	case <-configChanged:
	default:
		t.Fatal("expected configChanged notification")
	}
}

func TestAuthMiddlewareRejectsUnauthorizedAPI(t *testing.T) {
	cfg := config.DefaultConfig()
	srv := New(cfg, nil, nil, nil, nil, nil, nil, config.StaticProvider{Config: cfg})
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", nil)
	rr := httptest.NewRecorder()

	called := false
	srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})(rr, req)

	if called {
		t.Fatal("next handler should not be called")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
}
