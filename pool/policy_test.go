package pool

import (
	"testing"

	"goproxy/config"
	"goproxy/internal/domain"
)

func testPolicy() *Policy {
	return NewPolicy(&config.Config{
		PoolMaxSize:        100,
		PoolHTTPRatio:      0.3,
		PoolMinPerProtocol: 10,
		ReplaceThreshold:   0.7,
	})
}

func TestPolicyDetermineState(t *testing.T) {
	p := testPolicy()

	cases := []struct {
		name       string
		total      int
		httpCount  int
		socksCount int
		want       string
	}{
		{name: "missing http", total: 50, httpCount: 0, socksCount: 50, want: "emergency"},
		{name: "missing socks5", total: 50, httpCount: 30, socksCount: 0, want: "emergency"},
		{name: "low total", total: 5, httpCount: 2, socksCount: 3, want: "emergency"},
		{name: "low protocol slot", total: 50, httpCount: 3, socksCount: 47, want: "critical"},
		{name: "not full", total: 80, httpCount: 30, socksCount: 50, want: "warning"},
		{name: "healthy", total: 100, httpCount: 30, socksCount: 70, want: "healthy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.DetermineState(tc.total, tc.httpCount, tc.socksCount)
			if got != tc.want {
				t.Fatalf("DetermineState() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPolicyNeedsFetch(t *testing.T) {
	p := testPolicy()

	status := &domain.PoolStatus{State: "warning", HTTP: 10, SOCKS5: 60, HTTPSlots: 30, SOCKS5Slots: 70}
	need, mode, protocol := p.NeedsFetch(status)
	if !need || mode != "refill" || protocol != "http" {
		t.Fatalf("NeedsFetch() = (%v, %q, %q), want (true, refill, http)", need, mode, protocol)
	}

	status = &domain.PoolStatus{State: "healthy", HTTP: 30, SOCKS5: 70, HTTPSlots: 30, SOCKS5Slots: 70}
	need, mode, protocol = p.NeedsFetch(status)
	if need || mode != "" || protocol != "" {
		t.Fatalf("NeedsFetch healthy = (%v, %q, %q), want no fetch", need, mode, protocol)
	}
}

func TestPolicySlotDecision(t *testing.T) {
	p := testPolicy()

	_, _, direct, floating, replace := p.SlotDecision("http", 10, 70, 80)
	if !direct || floating || replace {
		t.Fatalf("expected direct add, got direct=%v floating=%v replace=%v", direct, floating, replace)
	}

	_, _, direct, floating, replace = p.SlotDecision("http", 30, 60, 90)
	if direct || !floating || replace {
		t.Fatalf("expected floating add, got direct=%v floating=%v replace=%v", direct, floating, replace)
	}

	_, _, direct, floating, replace = p.SlotDecision("http", 33, 70, 100)
	if direct || floating || !replace {
		t.Fatalf("expected replace, got direct=%v floating=%v replace=%v", direct, floating, replace)
	}
}

func TestPolicyShouldReplace(t *testing.T) {
	p := testPolicy()
	if !p.ShouldReplace(domain.Proxy{Latency: 600}, domain.Proxy{Latency: 1000}) {
		t.Fatal("expected 600ms proxy to replace 1000ms proxy at threshold 0.7")
	}
	if p.ShouldReplace(domain.Proxy{Latency: 800}, domain.Proxy{Latency: 1000}) {
		t.Fatal("did not expect 800ms proxy to replace 1000ms proxy at threshold 0.7")
	}
}
