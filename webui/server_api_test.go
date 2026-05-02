package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
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

type fakeWebUISubscriptionStore struct {
	subs                []domain.Subscription
	countByID           map[int64][2]int
	addSubscriptionID   int64
	addSubscriptionArgs struct {
		name, url, filePath, format string
		refreshMin                  int
	}
	addContributedID   int64
	addContributedArgs struct {
		name       string
		url        string
		refreshMin int
	}
	markContributedID       int64
	deleteBySubscriptionID  int64
	deleteBySubscriptionCnt int64
	deletedSubscriptionID   int64
	toggledSubscriptionID   int64
}

func (f *fakeWebUISubscriptionStore) GetSubscriptions() ([]domain.Subscription, error) {
	return f.subs, nil
}
func (f *fakeWebUISubscriptionStore) GetSubscription(id int64) (*domain.Subscription, error) {
	for _, sub := range f.subs {
		if sub.ID == id {
			cp := sub
			return &cp, nil
		}
	}
	return nil, nil
}
func (f *fakeWebUISubscriptionStore) GetStaleSubscriptions(staleDays int) ([]domain.Subscription, error) {
	return nil, nil
}
func (f *fakeWebUISubscriptionStore) AddSubscription(name, url, filePath, format string, refreshMin int) (int64, error) {
	f.addSubscriptionArgs = struct {
		name, url, filePath, format string
		refreshMin                  int
	}{name: name, url: url, filePath: filePath, format: format, refreshMin: refreshMin}
	if f.addSubscriptionID == 0 {
		f.addSubscriptionID = 101
	}
	return f.addSubscriptionID, nil
}
func (f *fakeWebUISubscriptionStore) AddContributedSubscription(name, url string, refreshMin int) (int64, error) {
	f.addContributedArgs = struct {
		name       string
		url        string
		refreshMin int
	}{name: name, url: url, refreshMin: refreshMin}
	if f.addContributedID == 0 {
		f.addContributedID = 202
	}
	return f.addContributedID, nil
}
func (f *fakeWebUISubscriptionStore) MarkSubscriptionContributed(id int64) error {
	f.markContributedID = id
	return nil
}
func (f *fakeWebUISubscriptionStore) DeleteSubscription(id int64) error {
	f.deletedSubscriptionID = id
	return nil
}
func (f *fakeWebUISubscriptionStore) ToggleSubscription(id int64) error {
	f.toggledSubscriptionID = id
	return nil
}
func (f *fakeWebUISubscriptionStore) UpdateSubscriptionFetch(id int64, proxyCount int) error {
	return nil
}
func (f *fakeWebUISubscriptionStore) UpdateSubscriptionSuccess(id int64) error {
	return nil
}
func (f *fakeWebUISubscriptionStore) CountBySubscriptionID(subID int64) (active int, disabled int) {
	pair := f.countByID[subID]
	return pair[0], pair[1]
}
func (f *fakeWebUISubscriptionStore) AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error {
	return nil
}
func (f *fakeWebUISubscriptionStore) DeleteBySubscriptionID(subscriptionID int64) (int64, error) {
	f.deleteBySubscriptionID = subscriptionID
	return f.deleteBySubscriptionCnt, nil
}
func (f *fakeWebUISubscriptionStore) GetRandom() (*domain.Proxy, error) {
	return nil, nil
}
func (f *fakeWebUISubscriptionStore) GetDisabledCustomProxies() ([]domain.Proxy, error) {
	return nil, nil
}
func (f *fakeWebUISubscriptionStore) EnableProxy(address string) error  { return nil }
func (f *fakeWebUISubscriptionStore) DisableProxy(address string) error { return nil }
func (f *fakeWebUISubscriptionStore) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error {
	return nil
}
func (f *fakeWebUISubscriptionStore) CountBySource(source string) (int, error) { return 0, nil }

type fakeWebUISubscriptionRuntime struct {
	validateCalls []struct{ url, filePath string }
	refreshCh     chan int64
	refreshAllCh  chan struct{}
	status        map[string]interface{}
}

func (f *fakeWebUISubscriptionRuntime) ValidateSubscription(url, filePath string) (int, error) {
	f.validateCalls = append(f.validateCalls, struct{ url, filePath string }{url: url, filePath: filePath})
	return 3, nil
}
func (f *fakeWebUISubscriptionRuntime) RefreshSubscription(id int64) error {
	if f.refreshCh != nil {
		f.refreshCh <- id
	}
	return nil
}
func (f *fakeWebUISubscriptionRuntime) RefreshAll() {
	if f.refreshAllCh != nil {
		f.refreshAllCh <- struct{}{}
	}
}
func (f *fakeWebUISubscriptionRuntime) GetStatus() map[string]interface{} {
	if f.status == nil {
		return map[string]interface{}{}
	}
	return f.status
}

var _ ports.SubscriptionStore = (*fakeWebUISubscriptionStore)(nil)
var _ ports.SubscriptionRuntime = (*fakeWebUISubscriptionRuntime)(nil)

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

func TestAPISubscriptionsAndCustomStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	store := &fakeWebUISubscriptionStore{
		subs: []domain.Subscription{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		countByID: map[int64][2]int{
			1: {3, 1},
			2: {5, 0},
		},
	}
	runtime := &fakeWebUISubscriptionRuntime{
		status: map[string]interface{}{
			"subscription_count": 2,
			"refresh_tasks":      []interface{}{map[string]interface{}{"scope": "all", "state": "running"}},
		},
	}
	subAdmin := appservice.NewSubscriptionAdminService(store, runtime, config.StaticProvider{Config: cfg})
	srv := New(cfg, nil, nil, nil, subAdmin, nil, nil, config.StaticProvider{Config: cfg})

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	rr := httptest.NewRecorder()
	srv.apiSubscriptions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected subscriptions status: %d", rr.Code)
	}

	var subs []domain.SubscriptionWithStats
	if err := json.NewDecoder(rr.Body).Decode(&subs); err != nil {
		t.Fatalf("decode subscriptions: %v", err)
	}
	if len(subs) != 2 || subs[0].ActiveCount != 3 || subs[0].DisabledCount != 1 {
		t.Fatalf("unexpected subscriptions payload: %#v", subs)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/custom/status", nil)
	statusRR := httptest.NewRecorder()
	srv.apiCustomStatus(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("unexpected custom status code: %d", statusRR.Code)
	}
	var status map[string]interface{}
	if err := json.NewDecoder(statusRR.Body).Decode(&status); err != nil {
		t.Fatalf("decode custom status: %v", err)
	}
	if status["subscription_count"] != float64(2) {
		t.Fatalf("unexpected custom status: %#v", status)
	}
}

func TestAPISubscriptionAddAndContributeTriggerRefresh(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DATA_DIR", tempDir)

	cfg := config.DefaultConfig()
	cfg.CustomRefreshInterval = 77
	store := &fakeWebUISubscriptionStore{
		addSubscriptionID: 111,
	}
	runtime := &fakeWebUISubscriptionRuntime{refreshCh: make(chan int64, 2)}
	subAdmin := appservice.NewSubscriptionAdminService(store, runtime, config.StaticProvider{Config: cfg})
	srv := New(cfg, nil, nil, nil, subAdmin, nil, nil, config.StaticProvider{Config: cfg})

	addBody := bytes.NewBufferString(`{"name":"SubA","file_content":"demo","refresh_min":0}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/subscription/add", addBody)
	addReq.AddCookie(adminCookie(t))
	addReq.Header.Set("Content-Type", "application/json")
	addRR := httptest.NewRecorder()
	srv.authMiddleware(srv.apiSubscriptionAdd).ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusOK {
		t.Fatalf("unexpected add status: %d body=%s", addRR.Code, addRR.Body.String())
	}
	if store.addSubscriptionArgs.refreshMin != 77 || store.addSubscriptionArgs.format != "auto" {
		t.Fatalf("unexpected add args: %#v", store.addSubscriptionArgs)
	}
	if store.addSubscriptionArgs.filePath == "" || filepath.Dir(store.addSubscriptionArgs.filePath) != filepath.Join(tempDir, "subscriptions") {
		t.Fatalf("expected saved add file under temp dir, got %q", store.addSubscriptionArgs.filePath)
	}
	if len(runtime.validateCalls) == 0 || runtime.validateCalls[0].filePath == "" {
		t.Fatalf("expected validate call for add, got %#v", runtime.validateCalls)
	}

	contribBody := bytes.NewBufferString(`{"name":"ShareA","url":"https://example.com/sub"}`)
	contribReq := httptest.NewRequest(http.MethodPost, "/api/subscription/contribute", contribBody)
	contribReq.Header.Set("Content-Type", "application/json")
	contribRR := httptest.NewRecorder()
	srv.apiSubscriptionContribute(contribRR, contribReq)
	if contribRR.Code != http.StatusOK {
		t.Fatalf("unexpected contribute status: %d body=%s", contribRR.Code, contribRR.Body.String())
	}
	if store.addContributedArgs.refreshMin != 77 || store.addContributedArgs.url != "https://example.com/sub" {
		t.Fatalf("unexpected contribute args: %#v", store.addContributedArgs)
	}

	wantRefresh := map[int64]bool{111: true, 202: true}
	deadline := time.After(time.Second)
	for len(wantRefresh) > 0 {
		select {
		case id := <-runtime.refreshCh:
			delete(wantRefresh, id)
		case <-deadline:
			t.Fatalf("expected refresh IDs not observed: %#v", wantRefresh)
		}
	}
}

func TestAPISubscriptionDeleteRefreshToggleEndpoints(t *testing.T) {
	cfg := config.DefaultConfig()
	store := &fakeWebUISubscriptionStore{deleteBySubscriptionCnt: 2}
	runtime := &fakeWebUISubscriptionRuntime{
		refreshCh:    make(chan int64, 1),
		refreshAllCh: make(chan struct{}, 2),
	}
	subAdmin := appservice.NewSubscriptionAdminService(store, runtime, config.StaticProvider{Config: cfg})
	srv := New(cfg, nil, nil, nil, subAdmin, nil, nil, config.StaticProvider{Config: cfg})

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/subscription/delete", bytes.NewBufferString(`{"id":9}`))
	deleteReq.AddCookie(adminCookie(t))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteRR := httptest.NewRecorder()
	srv.authMiddleware(srv.apiSubscriptionDelete).ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	if store.deleteBySubscriptionID != 9 || store.deletedSubscriptionID != 9 {
		t.Fatalf("unexpected delete store calls: bySub=%d deleted=%d", store.deleteBySubscriptionID, store.deletedSubscriptionID)
	}
	select {
	case <-runtime.refreshAllCh:
	case <-time.After(time.Second):
		t.Fatal("expected refresh-all trigger after delete")
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/subscription/refresh", bytes.NewBufferString(`{"id":12}`))
	refreshReq.AddCookie(adminCookie(t))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshRR := httptest.NewRecorder()
	srv.authMiddleware(srv.apiSubscriptionRefresh).ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("unexpected refresh status: %d body=%s", refreshRR.Code, refreshRR.Body.String())
	}
	select {
	case id := <-runtime.refreshCh:
		if id != 12 {
			t.Fatalf("unexpected refresh id: %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("expected refresh trigger")
	}

	refreshAllReq := httptest.NewRequest(http.MethodPost, "/api/subscription/refresh-all", nil)
	refreshAllReq.AddCookie(adminCookie(t))
	refreshAllRR := httptest.NewRecorder()
	srv.authMiddleware(srv.apiSubscriptionRefreshAll).ServeHTTP(refreshAllRR, refreshAllReq)
	if refreshAllRR.Code != http.StatusOK {
		t.Fatalf("unexpected refresh-all status: %d body=%s", refreshAllRR.Code, refreshAllRR.Body.String())
	}
	select {
	case <-runtime.refreshAllCh:
	case <-time.After(time.Second):
		t.Fatal("expected refresh-all endpoint to trigger runtime")
	}

	toggleReq := httptest.NewRequest(http.MethodPost, "/api/subscription/toggle", bytes.NewBufferString(`{"id":15}`))
	toggleReq.AddCookie(adminCookie(t))
	toggleReq.Header.Set("Content-Type", "application/json")
	toggleRR := httptest.NewRecorder()
	srv.authMiddleware(srv.apiSubscriptionToggle).ServeHTTP(toggleRR, toggleReq)
	if toggleRR.Code != http.StatusOK {
		t.Fatalf("unexpected toggle status: %d body=%s", toggleRR.Code, toggleRR.Body.String())
	}
	if store.toggledSubscriptionID != 15 {
		t.Fatalf("unexpected toggled subscription id: %d", store.toggledSubscriptionID)
	}
}
