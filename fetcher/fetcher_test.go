package fetcher

import (
	"strings"
	"testing"
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
