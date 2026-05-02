package service

import (
	"path/filepath"
	"testing"

	"goproxy/config"
	"goproxy/fetcher"
	"goproxy/internal/domain"
	"goproxy/storage"
)

func TestSourceAdminServiceStatsIncludesExtraSourcesAndDisabledFlags(t *testing.T) {
	store, err := storage.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("storage.New error: %v", err)
	}
	defer store.Close()

	cfg := config.DefaultConfig()
	cfg.ExtraSources = append(cfg.ExtraSources,
		configuredSource("https://extra.example/http.txt", "http", "slow"),
		configuredSource("https://extra.example/socks5.txt", "socks5", "fast"),
	)
	cfg.DisabledSourceURLs = []string{"https://extra.example/socks5.txt"}
	provider := config.StaticProvider{Config: cfg}

	manager := fetcher.NewSourceManager(store.GetDB())
	f := fetcher.New("", "", manager, 100, provider)

	manager.RecordSuccess("https://extra.example/http.txt")
	manager.RecordFail("https://extra.example/socks5.txt", 1, 5, 30)

	service := NewSourceAdminService(f, manager, provider)
	stats, err := service.Stats()
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("expected non-empty source stats")
	}

	var foundEnabled, foundDisabled bool
	for _, stat := range stats {
		switch stat.URL {
		case "https://extra.example/http.txt":
			foundEnabled = true
			if !stat.Enabled || stat.Group != "slow" || stat.Protocol != "http" || stat.SuccessCount != 1 {
				t.Fatalf("unexpected enabled source stat: %#v", stat)
			}
		case "https://extra.example/socks5.txt":
			foundDisabled = true
			if stat.Enabled || stat.Group != "fast" || stat.Protocol != "socks5" || stat.FailCount != 1 || stat.Status != "degraded" {
				t.Fatalf("unexpected disabled source stat: %#v", stat)
			}
		}
	}

	if !foundEnabled || !foundDisabled {
		t.Fatalf("missing expected extra source stats: %#v", stats)
	}
}

func configuredSource(url, protocol, group string) domain.FetchSourceConfig {
	return domain.FetchSourceConfig{URL: url, Protocol: protocol, Group: group}
}
