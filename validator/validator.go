package validator

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
)

type Validator struct {
	concurrency        int
	timeout            time.Duration
	validateURL        string
	maxResponseMs      int
	cfg                *config.Config
	geoIP              ports.GeoIPResolver
	strategies         []validationStrategy
	httpConnectChecker func(ctx context.Context, proxyAddr string, timeout time.Duration) bool
}

func concurrencyBuffer(total, concurrency int) int {
	if total < concurrency*10 {
		return total
	}
	return concurrency * 10
}

func New(concurrency, timeoutSec int, validateURL string) *Validator {
	return NewWithGeoIP(concurrency, timeoutSec, validateURL, nil)
}

func NewWithGeoIP(concurrency, timeoutSec int, validateURL string, geoIP ports.GeoIPResolver) *Validator {
	cfg := config.Get()
	maxMs := 0
	if cfg != nil {
		maxMs = cfg.MaxResponseMs
	}
	v := &Validator{
		concurrency:        concurrency,
		timeout:            time.Duration(timeoutSec) * time.Second,
		validateURL:        validateURL,
		maxResponseMs:      maxMs,
		cfg:                cfg,
		geoIP:              geoIP,
		httpConnectChecker: checkHTTPSConnectContext,
	}
	v.strategies = v.buildValidationStrategies()
	return v
}

type Result = domain.ValidationResult

// HTTPS 测试目标列表，随机选一个验证代理的 CONNECT 隧道能力
var httpsTestTargets = []string{
	"https://www.google.com",
	"https://www.openai.com",
	"https://www.github.com",
	"https://www.cloudflare.com",
	"https://httpbin.org/ip",
}

// checkHTTPSConnect 通过 HTTP 代理实际访问一个随机 HTTPS 网站，验证 CONNECT 隧道是否可用
// 首次失败会换一个目标重试一次，避免目标网站偶尔抽风导致误杀
func checkHTTPSConnect(proxyAddr string, timeout time.Duration) bool {
	return checkHTTPSConnectContext(context.Background(), proxyAddr, timeout)
}

func checkHTTPSConnectContext(ctx context.Context, proxyAddr string, timeout time.Duration) bool {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			TLSHandshakeTimeout: timeout,
		},
		Timeout: timeout,
	}

	// 随机起始索引
	start := int(time.Now().UnixNano() % int64(len(httpsTestTargets)))

	for attempt := 0; attempt < 2; attempt++ {
		idx := (start + attempt) % len(httpsTestTargets)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpsTestTargets[idx], nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// 2xx 或 3xx 都算成功（部分网站会重定向）
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true
		}
	}

	return false
}

// ValidateAll 并发验证所有代理，返回验证结果
func (v *Validator) ValidateAll(proxies []domain.Proxy) []Result {
	var results []Result
	for r := range v.ValidateStream(proxies) {
		results = append(results, r)
	}
	return results
}

// ValidateStream 并发验证，边验证边通过 channel 返回结果
func (v *Validator) ValidateStream(proxies []domain.Proxy) <-chan Result {
	return v.ValidateStreamContext(context.Background(), proxies)
}

// ValidateStreamContext validates proxies concurrently and stops dispatching/sending when ctx is canceled.
func (v *Validator) ValidateStreamContext(ctx context.Context, proxies []domain.Proxy) <-chan Result {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan Result, concurrencyBuffer(len(proxies), v.concurrency))
	sem := make(chan struct{}, v.concurrency)
	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(ch)
		}()

		for _, p := range proxies {
			select {
			case <-ctx.Done():
				return
			default:
			}

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}

			wg.Add(1)
			go func(px domain.Proxy) {
				defer wg.Done()
				defer func() { <-sem }()

				valid, latency, exitIP, exitLocation, ipInfo := v.ValidateOneContext(ctx, px)
				result := Result{Proxy: px, Valid: valid, Latency: latency, ExitIP: exitIP, ExitLocation: exitLocation, IPInfo: ipInfo}
				select {
				case ch <- result:
				case <-ctx.Done():
				}
			}(p)
		}
	}()

	return ch
}

// ValidateOne 验证单个代理是否可用，返回是否有效、延迟、出口IP、地理位置和 IPPure 画像
func (v *Validator) ValidateOne(p domain.Proxy) (bool, time.Duration, string, string, domain.IPInfo) {
	return v.ValidateOneContext(context.Background(), p)
}

// ValidateOneContext validates a single proxy and binds outbound requests to ctx.
func (v *Validator) ValidateOneContext(ctx context.Context, p domain.Proxy) (bool, time.Duration, string, string, domain.IPInfo) {
	if ctx == nil {
		ctx = context.Background()
	}

	state, ok := v.probeConnectivity(ctx, p)
	if !ok {
		return false, state.latency, state.exitIP, state.exitLocation, state.ipInfo
	}
	if !v.runValidationStrategies(state) {
		return false, state.latency, state.exitIP, state.exitLocation, state.ipInfo
	}
	return true, state.latency, state.exitIP, state.exitLocation, state.ipInfo
}

func (v *Validator) probeConnectivity(ctx context.Context, p domain.Proxy) (*validationState, bool) {
	state := &validationState{
		ctx:   ctx,
		proxy: p,
	}

	var client *http.Client
	var err error

	switch p.Protocol {
	case "http":
		client, err = newHTTPClient(p.Address, v.timeout)
	case "socks5":
		client, err = newSOCKS5Client(p.Address, v.timeout)
	default:
		log.Printf("unknown protocol %s for %s", p.Protocol, p.Address)
		return state, false
	}

	if err != nil {
		return state, false
	}
	state.client = client

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.validateURL, nil)
	if err != nil {
		return state, false
	}
	resp, err := client.Do(req)
	state.latency = time.Since(start)
	if err != nil {
		return state, false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	state.statusCode = resp.StatusCode
	return state, true
}

func newHTTPClient(address string, timeout time.Duration) (*http.Client, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://%s", address))
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: timeout,
	}, nil
}

func newSOCKS5Client(address string, timeout time.Duration) (*http.Client, error) {
	dialer, err := proxy.SOCKS5("tcp", address, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &http.Transport{
			Dial: dialer.Dial,
		},
		Timeout: timeout,
	}, nil
}
