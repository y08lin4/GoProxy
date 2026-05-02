package service

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"goproxy/config"
	"goproxy/internal/domain"
)

type fakeSubscriptionStore struct {
	subs                []domain.Subscription
	countByID           map[int64][2]int
	addSubscriptionID   int64
	addSubscriptionArgs struct {
		name, url, filePath, format string
		refreshMin                  int
	}
	addContributedID   int64
	addContributedArgs struct {
		name, url  string
		refreshMin int
	}
	markContributedID       int64
	deleteBySubscriptionID  int64
	deleteBySubscriptionCnt int64
	deletedSubscriptionID   int64
	toggledSubscriptionID   int64
}

func (f *fakeSubscriptionStore) GetSubscriptions() ([]domain.Subscription, error) {
	return f.subs, nil
}
func (f *fakeSubscriptionStore) GetSubscription(id int64) (*domain.Subscription, error) {
	for _, sub := range f.subs {
		if sub.ID == id {
			cp := sub
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeSubscriptionStore) GetStaleSubscriptions(staleDays int) ([]domain.Subscription, error) {
	return nil, nil
}
func (f *fakeSubscriptionStore) AddSubscription(name, url, filePath, format string, refreshMin int) (int64, error) {
	f.addSubscriptionArgs = struct {
		name       string
		url        string
		filePath   string
		format     string
		refreshMin int
	}{name: name, url: url, filePath: filePath, format: format, refreshMin: refreshMin}
	if f.addSubscriptionID == 0 {
		f.addSubscriptionID = 101
	}
	return f.addSubscriptionID, nil
}
func (f *fakeSubscriptionStore) AddContributedSubscription(name, url string, refreshMin int) (int64, error) {
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
func (f *fakeSubscriptionStore) MarkSubscriptionContributed(id int64) error {
	f.markContributedID = id
	return nil
}
func (f *fakeSubscriptionStore) DeleteSubscription(id int64) error {
	f.deletedSubscriptionID = id
	return nil
}
func (f *fakeSubscriptionStore) ToggleSubscription(id int64) error {
	f.toggledSubscriptionID = id
	return nil
}
func (f *fakeSubscriptionStore) UpdateSubscriptionFetch(id int64, proxyCount int) error { return nil }
func (f *fakeSubscriptionStore) UpdateSubscriptionSuccess(id int64) error               { return nil }
func (f *fakeSubscriptionStore) CountBySubscriptionID(subID int64) (active int, disabled int) {
	pair := f.countByID[subID]
	return pair[0], pair[1]
}
func (f *fakeSubscriptionStore) AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error {
	return nil
}
func (f *fakeSubscriptionStore) DeleteBySubscriptionID(subscriptionID int64) (int64, error) {
	f.deleteBySubscriptionID = subscriptionID
	return f.deleteBySubscriptionCnt, nil
}
func (f *fakeSubscriptionStore) GetRandom() (*domain.Proxy, error) {
	return nil, errors.New("not used")
}
func (f *fakeSubscriptionStore) GetDisabledCustomProxies() ([]domain.Proxy, error) { return nil, nil }
func (f *fakeSubscriptionStore) EnableProxy(address string) error                  { return nil }
func (f *fakeSubscriptionStore) DisableProxy(address string) error                 { return nil }
func (f *fakeSubscriptionStore) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error {
	return nil
}
func (f *fakeSubscriptionStore) CountBySource(source string) (int, error) { return 0, nil }

type fakeSubscriptionRuntime struct {
	validateCalls []struct{ url, filePath string }
	refreshCh     chan int64
	refreshAllCh  chan struct{}
	status        map[string]interface{}
}

func (f *fakeSubscriptionRuntime) ValidateSubscription(url, filePath string) (int, error) {
	f.validateCalls = append(f.validateCalls, struct{ url, filePath string }{url: url, filePath: filePath})
	return 3, nil
}
func (f *fakeSubscriptionRuntime) RefreshSubscription(id int64) error {
	if f.refreshCh != nil {
		f.refreshCh <- id
	}
	return nil
}
func (f *fakeSubscriptionRuntime) RefreshAll() {
	if f.refreshAllCh != nil {
		f.refreshAllCh <- struct{}{}
	}
}
func (f *fakeSubscriptionRuntime) GetStatus() map[string]interface{} {
	if f.status == nil {
		return map[string]interface{}{"ok": true}
	}
	return f.status
}

func TestSubscriptionAdminServiceListAndStatus(t *testing.T) {
	store := &fakeSubscriptionStore{
		subs: []domain.Subscription{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		countByID: map[int64][2]int{
			1: {3, 1},
			2: {5, 0},
		},
	}
	runtime := &fakeSubscriptionRuntime{status: map[string]interface{}{"subscription_count": 2}}
	service := NewSubscriptionAdminService(store, runtime)

	list, err := service.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 2 || list[0].ActiveCount != 3 || list[0].DisabledCount != 1 || list[1].ActiveCount != 5 {
		t.Fatalf("unexpected subscription list: %#v", list)
	}

	status := service.Status()
	if status["subscription_count"] != 2 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestSubscriptionAdminServiceAddUsesDefaultRefreshAndTriggersRefresh(t *testing.T) {
	store := &fakeSubscriptionStore{addSubscriptionID: 301}
	runtime := &fakeSubscriptionRuntime{refreshCh: make(chan int64, 1)}
	cfg := config.DefaultConfig()
	cfg.CustomRefreshInterval = 77
	service := NewSubscriptionAdminService(store, runtime, config.StaticProvider{Config: cfg})

	id, err := service.Add("", "https://example.com/sub", "", 0)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if id != 301 {
		t.Fatalf("unexpected add id: %d", id)
	}
	if store.addSubscriptionArgs.refreshMin != 77 || store.addSubscriptionArgs.url != "https://example.com/sub" || store.addSubscriptionArgs.format != "auto" {
		t.Fatalf("unexpected add args: %#v", store.addSubscriptionArgs)
	}
	select {
	case refreshed := <-runtime.refreshCh:
		if refreshed != 301 {
			t.Fatalf("unexpected refresh id: %d", refreshed)
		}
	case <-time.After(time.Second):
		t.Fatal("expected RefreshSubscription to be triggered")
	}
}

func TestSubscriptionAdminServiceContributeFileMarksContribution(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DATA_DIR", tempDir)

	store := &fakeSubscriptionStore{addSubscriptionID: 401}
	runtime := &fakeSubscriptionRuntime{refreshCh: make(chan int64, 1)}
	cfg := config.DefaultConfig()
	cfg.CustomRefreshInterval = 88
	service := NewSubscriptionAdminService(store, runtime, config.StaticProvider{Config: cfg})

	id, err := service.Contribute("", "", "proxy-content")
	if err != nil {
		t.Fatalf("Contribute error: %v", err)
	}
	if id != 401 {
		t.Fatalf("unexpected contributed id: %d", id)
	}
	if store.markContributedID != 401 {
		t.Fatalf("expected contributed mark for id 401, got %d", store.markContributedID)
	}
	if store.addSubscriptionArgs.refreshMin != 88 {
		t.Fatalf("unexpected contribute refresh min: %#v", store.addSubscriptionArgs)
	}
	if store.addSubscriptionArgs.filePath == "" || filepath.Dir(store.addSubscriptionArgs.filePath) != filepath.Join(tempDir, "subscriptions") {
		t.Fatalf("expected file path under DATA_DIR, got %#v", store.addSubscriptionArgs.filePath)
	}
	if len(runtime.validateCalls) != 1 || runtime.validateCalls[0].filePath == "" {
		t.Fatalf("expected validation on saved file path, got %#v", runtime.validateCalls)
	}
	select {
	case <-runtime.refreshCh:
	case <-time.After(time.Second):
		t.Fatal("expected contributed subscription refresh trigger")
	}
}

func TestSubscriptionAdminServiceDeleteAndToggle(t *testing.T) {
	store := &fakeSubscriptionStore{deleteBySubscriptionCnt: 2}
	runtime := &fakeSubscriptionRuntime{refreshAllCh: make(chan struct{}, 1)}
	service := NewSubscriptionAdminService(store, runtime)

	if err := service.Delete(9); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if store.deleteBySubscriptionID != 9 || store.deletedSubscriptionID != 9 {
		t.Fatalf("unexpected delete calls: deleteBySub=%d deleted=%d", store.deleteBySubscriptionID, store.deletedSubscriptionID)
	}
	select {
	case <-runtime.refreshAllCh:
	case <-time.After(time.Second):
		t.Fatal("expected RefreshAll to be triggered after delete")
	}

	if err := service.Toggle(11); err != nil {
		t.Fatalf("Toggle error: %v", err)
	}
	if store.toggledSubscriptionID != 11 {
		t.Fatalf("unexpected toggled subscription id: %d", store.toggledSubscriptionID)
	}
}
