package fetcher

import (
	"strings"
	"testing"

	"goproxy/config"
	"goproxy/internal/domain"
)

func TestParseProxyListNormalizesProtocolsAndPorts(t *testing.T) {
	input := strings.NewReader(`
# comment
1.1.1.1:80
http://2.2.2.2:8080
https://3.3.3.3:443
socks5://4.4.4.4:1080
socks4://5.5.5.5:1080
6.6.6.6:0
7.7.7.7:70000
bad-line
`)

	proxies, err := parseProxyList(input, "http")
	if err != nil {
		t.Fatalf("parseProxyList returned error: %v", err)
	}

	want := []struct {
		addr  string
		proto string
	}{
		{"1.1.1.1:80", "http"},
		{"2.2.2.2:8080", "http"},
		{"3.3.3.3:443", "http"},
		{"4.4.4.4:1080", "socks5"},
		{"5.5.5.5:1080", "socks5"},
	}

	if len(proxies) != len(want) {
		t.Fatalf("got %d proxies, want %d: %#v", len(proxies), len(want), proxies)
	}
	for i, p := range proxies {
		if p.Address != want[i].addr || p.Protocol != want[i].proto {
			t.Fatalf("proxy[%d] = (%q, %q), want (%q, %q)", i, p.Address, p.Protocol, want[i].addr, want[i].proto)
		}
	}
}

func TestLimitProxyCandidates(t *testing.T) {
	proxies := []domain.Proxy{
		{Address: "1.1.1.1:80"},
		{Address: "2.2.2.2:80"},
		{Address: "3.3.3.3:80"},
	}

	limited := limitProxyCandidates(proxies, 2)
	if len(limited) != 2 {
		t.Fatalf("got %d proxies, want 2", len(limited))
	}
	if limited[0].Address != "1.1.1.1:80" || limited[1].Address != "2.2.2.2:80" {
		t.Fatalf("unexpected limited proxies: %#v", limited)
	}

	unlimited := limitProxyCandidates(proxies, 0)
	if len(unlimited) != len(proxies) {
		t.Fatalf("limit 0 should keep all proxies, got %d", len(unlimited))
	}
}

func TestBuiltInSourcesAreUniqueAndSupported(t *testing.T) {
	seen := make(map[string]struct{}, len(allSources))
	for _, source := range allSources {
		if source.URL == "" {
			t.Fatal("built-in source URL must not be empty")
		}
		if _, ok := seen[source.URL]; ok {
			t.Fatalf("duplicate built-in source URL: %s", source.URL)
		}
		seen[source.URL] = struct{}{}

		switch source.Protocol {
		case "http", "socks5":
		default:
			t.Fatalf("source %s uses unsupported protocol %q", source.URL, source.Protocol)
		}
	}
}

func TestSourceCatalogMergesExtraSourcesAndDisabledURLs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ExtraSources = []domain.FetchSourceConfig{
		{URL: "https://example.com/http.txt", Protocol: "http", Group: "slow"},
		{URL: "https://example.com/socks5.txt", Protocol: "socks5", Group: "fast"},
		{URL: fastUpdateSources[0].URL, Protocol: "http", Group: "fast"}, // duplicate built-in
	}
	cfg.DisabledSourceURLs = []string{fastUpdateSources[0].URL}

	fetcher := New("", "", nil, 100, config.StaticProvider{Config: cfg})
	catalog := fetcher.SourceCatalog()

	foundExtra := false
	for _, src := range catalog {
		if src.URL == "https://example.com/http.txt" && src.Protocol == "http" && src.Group == "slow" {
			foundExtra = true
		}
	}
	if !foundExtra {
		t.Fatalf("extra source missing from catalog: %#v", catalog)
	}

	fastSources, _, _ := fetcher.activeSources()
	for _, src := range fastSources {
		if src.URL == fastUpdateSources[0].URL {
			t.Fatalf("disabled source %s should not be active", src.URL)
		}
	}
}
