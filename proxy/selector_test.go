package proxy

import (
	"path/filepath"
	"testing"

	"goproxy/config"
	"goproxy/storage"
)

func testStorage(t *testing.T) *storage.Storage {
	t.Helper()

	store, err := storage.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func loadSelectorTestConfig(t *testing.T) *config.Config {
	t.Helper()

	t.Setenv("DATA_DIR", t.TempDir())
	cfg := config.Load()
	cfg.CustomProxyMode = "mixed"
	cfg.CustomPriority = false
	cfg.CustomFreePriority = false
	return cfg
}

func TestSelectorPrefersCustomAndFallsBack(t *testing.T) {
	cfg := loadSelectorTestConfig(t)
	cfg.CustomProxyMode = "mixed"
	cfg.CustomPriority = true

	store := testStorage(t)
	mustAddProxyWithSource(t, store, "10.0.0.1:8080", "http", "custom")
	mustAddProxyWithSource(t, store, "10.0.0.2:8080", "http", "free")

	selector := NewSelector(store)
	p, err := selector.Select(nil, "", false)
	if err != nil {
		t.Fatalf("select preferred custom: %v", err)
	}
	if p.Address != "10.0.0.1:8080" || p.Source != "custom" {
		t.Fatalf("expected custom proxy first, got %#v", p)
	}

	p, err = selector.Select([]string{"10.0.0.1:8080"}, "", false)
	if err != nil {
		t.Fatalf("select fallback: %v", err)
	}
	if p.Address != "10.0.0.2:8080" || p.Source != "free" {
		t.Fatalf("expected fallback free proxy, got %#v", p)
	}
}

func TestSelectorFiltersProtocolDuringFallback(t *testing.T) {
	cfg := loadSelectorTestConfig(t)
	cfg.CustomProxyMode = "mixed"
	cfg.CustomPriority = true

	store := testStorage(t)
	mustAddProxyWithSource(t, store, "10.0.0.1:8080", "http", "custom")
	mustAddProxyWithSource(t, store, "10.0.0.2:1080", "socks5", "free")

	selector := NewSelector(store)
	p, err := selector.Select(nil, "socks5", false)
	if err != nil {
		t.Fatalf("select socks5 fallback: %v", err)
	}
	if p.Address != "10.0.0.2:1080" || p.Protocol != "socks5" {
		t.Fatalf("expected socks5 fallback proxy, got %#v", p)
	}
}

func TestFailureReporterFailureDeletesFreeAndDisablesCustom(t *testing.T) {
	store := testStorage(t)
	mustAddProxyWithSource(t, store, "10.0.0.3:8080", "http", "free")
	mustAddProxyWithSource(t, store, "10.0.0.4:8080", "http", "custom")

	reporter := NewFailureReporter(store)
	reporter.Failure(&storage.Proxy{Address: "10.0.0.3:8080", Source: "free"})
	reporter.Failure(&storage.Proxy{Address: "10.0.0.4:8080", Source: "custom"})

	freeCount, err := store.CountBySource("free")
	if err != nil {
		t.Fatalf("count free: %v", err)
	}
	if freeCount != 0 {
		t.Fatalf("expected free proxy to be deleted, count=%d", freeCount)
	}

	disabled, err := store.GetDisabledCustomProxies()
	if err != nil {
		t.Fatalf("get disabled custom: %v", err)
	}
	if len(disabled) != 1 || disabled[0].Address != "10.0.0.4:8080" {
		t.Fatalf("expected disabled custom proxy, got %#v", disabled)
	}
	if disabled[0].FailCount != 1 || disabled[0].UseCount != 1 {
		t.Fatalf("expected failure usage to be recorded, got use=%d fail=%d", disabled[0].UseCount, disabled[0].FailCount)
	}
}

func TestFailureReporterSuccessRecordsUse(t *testing.T) {
	store := testStorage(t)
	mustAddProxyWithSource(t, store, "10.0.0.5:8080", "http", "free")

	reporter := NewFailureReporter(store)
	reporter.Success(&storage.Proxy{Address: "10.0.0.5:8080", Source: "free"})

	var useCount, successCount, failCount int
	if err := store.GetDB().QueryRow(`SELECT use_count, success_count, fail_count FROM proxies WHERE address = ?`, "10.0.0.5:8080").Scan(&useCount, &successCount, &failCount); err != nil {
		t.Fatalf("query usage counters: %v", err)
	}
	if useCount != 1 || successCount != 1 || failCount != 0 {
		t.Fatalf("unexpected usage counters: use=%d success=%d fail=%d", useCount, successCount, failCount)
	}
}

func mustAddProxyWithSource(t *testing.T, store *storage.Storage, address, protocol, source string) {
	t.Helper()
	if err := store.AddProxyWithSource(address, protocol, source); err != nil {
		t.Fatalf("add proxy %s: %v", address, err)
	}
}
