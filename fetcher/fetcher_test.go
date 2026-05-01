package fetcher

import (
	"strings"
	"testing"

	"goproxy/storage"
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
	proxies := []storage.Proxy{
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
