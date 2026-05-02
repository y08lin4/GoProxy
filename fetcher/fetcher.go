package fetcher

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"goproxy/internal/domain"
)

type Source = domain.Source

// 快速更新源（5-30分钟更新）- 用于紧急和补充模式
var fastUpdateSources = []Source{
	// ProxyScraper - 每30分钟更新
	{"https://raw.githubusercontent.com/ProxyScraper/ProxyScraper/main/http.txt", "http"},
	{"https://raw.githubusercontent.com/ProxyScraper/ProxyScraper/main/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/ProxyScraper/ProxyScraper/main/socks5.txt", "socks5"},
	// monosans - 每小时更新
	{"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt", "http"},
	// prxchk - 频繁更新
	{"https://raw.githubusercontent.com/prxchk/proxy-list/main/http.txt", "http"},
	{"https://raw.githubusercontent.com/prxchk/proxy-list/main/socks5.txt", "socks5"},
	{"https://raw.githubusercontent.com/prxchk/proxy-list/main/socks4.txt", "socks5"},
	// sunny9577 - 自动抓取更新
	{"https://cdn.jsdelivr.net/gh/sunny9577/proxy-scraper/generated/http_proxies.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/sunny9577/proxy-scraper/generated/socks5_proxies.txt", "socks5"},
	{"https://cdn.jsdelivr.net/gh/sunny9577/proxy-scraper/generated/socks4_proxies.txt", "socks5"},
	// Proxyfind 参考源：ProxyScrape API / OpenProxyList / Proxifly（更新快、纯文本）
	{"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=10000&country=all&ssl=all&anonymity=all", "http"},
	{"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&timeout=10000&country=all", "socks5"},
	{"https://raw.githubusercontent.com/roosterkid/openproxylist/main/HTTPS_RAW.txt", "http"},
	{"https://raw.githubusercontent.com/roosterkid/openproxylist/main/SOCKS5_RAW.txt", "socks5"},
	{"https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/http/data.txt", "http"},
	{"https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/socks5/data.txt", "socks5"},
	// Proxyfind 参考源：OpenProxyList / ProxySpace（API/高频纯文本）
	{"https://api.openproxylist.xyz/http.txt", "http"},
	{"https://api.openproxylist.xyz/socks4.txt", "socks5"},
	{"https://api.openproxylist.xyz/socks5.txt", "socks5"},
	{"https://proxyspace.pro/http.txt", "http"},
	{"https://proxyspace.pro/https.txt", "http"},
}

// 慢速更新源（每天更新）- 用于优化轮换模式
var slowUpdateSources = []Source{
	// TheSpeedX - 每天更新，量大
	{"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt", "http"},
	{"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/socks5.txt", "socks5"},
	// monosans SOCKS
	{"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt", "socks5"},
	// databay-labs - 备用源
	{"https://cdn.jsdelivr.net/gh/databay-labs/free-proxy-list/http.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/databay-labs/free-proxy-list/socks5.txt", "socks5"},
	// Anonym0usWork1221 - 量大质量尚可
	{"https://cdn.jsdelivr.net/gh/Anonym0usWork1221/Free-Proxies/proxy_files/http_proxies.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/Anonym0usWork1221/Free-Proxies/proxy_files/socks5_proxies.txt", "socks5"},
	{"https://cdn.jsdelivr.net/gh/Anonym0usWork1221/Free-Proxies/proxy_files/socks4_proxies.txt", "socks5"},
	// ALIILAPRO
	{"https://cdn.jsdelivr.net/gh/ALIILAPRO/Proxy/http.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/ALIILAPRO/Proxy/socks4.txt", "socks5"},
	// vakhov/fresh-proxy-list
	{"https://cdn.jsdelivr.net/gh/vakhov/fresh-proxy-list/http.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/vakhov/fresh-proxy-list/socks5.txt", "socks5"},
	{"https://cdn.jsdelivr.net/gh/vakhov/fresh-proxy-list/socks4.txt", "socks5"},
	// Zaeem20
	{"https://cdn.jsdelivr.net/gh/Zaeem20/FREE_PROXIES_LIST/http.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/Zaeem20/FREE_PROXIES_LIST/socks4.txt", "socks5"},
	// hookzof - socks5 专项
	{"https://cdn.jsdelivr.net/gh/hookzof/socks5_list/proxy.txt", "socks5"},
	// proxy4parsing
	{"https://cdn.jsdelivr.net/gh/proxy4parsing/proxy-list/http.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/proxy4parsing/proxy-list/socks5.txt", "socks5"},
	// Proxyfind 参考源：mmpx12 / clarketm / rdavydov / 其他 GitHub 文本源
	{"https://raw.githubusercontent.com/mmpx12/proxy-list/master/http.txt", "http"},
	{"https://raw.githubusercontent.com/mmpx12/proxy-list/master/https.txt", "http"},
	{"https://raw.githubusercontent.com/mmpx12/proxy-list/master/socks5.txt", "socks5"},
	{"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt", "http"},
	{"https://raw.githubusercontent.com/rdavydov/proxy-list/main/proxies/http.txt", "http"},
	{"https://raw.githubusercontent.com/hendrikbgr/Free-Proxy-Repo/master/proxy_list.txt", "http"},
	{"https://raw.githubusercontent.com/saisuiu/Lionkings-Http-Proxys-Proxies/main/free.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/ALIILAPRO/Proxy/socks5.txt", "socks5"},
	{"https://cdn.jsdelivr.net/gh/Zaeem20/FREE_PROXIES_LIST/socks5.txt", "socks5"},
	// Proxyfind 补充源：Jetkai / ShiftyTR / ErcinDedeoglu
	{"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-http.txt", "http"},
	{"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-https.txt", "http"},
	{"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-socks5.txt", "socks5"},
	{"https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/http.txt", "http"},
	{"https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/https.txt", "http"},
	{"https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/socks5.txt", "socks5"},
	{"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/main/proxies/http.txt", "http"},
	{"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/main/proxies/https.txt", "http"},
	{"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/main/proxies/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/main/proxies/socks5.txt", "socks5"},
	// Proxyfind 补充源：已有源家族缺失的协议文件
	{"https://raw.githubusercontent.com/mmpx12/proxy-list/master/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/rdavydov/proxy-list/main/proxies/socks4.txt", "socks5"},
	{"https://raw.githubusercontent.com/rdavydov/proxy-list/main/proxies/socks5.txt", "socks5"},
	{"https://raw.githubusercontent.com/roosterkid/openproxylist/main/SOCKS4_RAW.txt", "socks5"},
	{"https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/https/data.txt", "http"},
	{"https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/socks4/data.txt", "socks5"},
	{"https://cdn.jsdelivr.net/gh/Anonym0usWork1221/Free-Proxies/proxy_files/https_proxies.txt", "http"},
	{"https://cdn.jsdelivr.net/gh/Zaeem20/FREE_PROXIES_LIST/https.txt", "http"},
	// Proxyfind 补充源：其他已验证纯文本 HTTP 源
	{"https://raw.githubusercontent.com/B4RC0DE-TM/proxy-list/main/HTTP.txt", "http"},
	{"https://raw.githubusercontent.com/almroot/proxylist/master/list.txt", "http"},
	{"https://raw.githubusercontent.com/aslisk/proxyhttps/main/https.txt", "http"},
	{"https://raw.githubusercontent.com/opsxcq/proxy-list/master/list.txt", "http"},
}

// 所有源
var allSources = append(fastUpdateSources, slowUpdateSources...)

type Fetcher struct {
	sources                []Source
	client                 *http.Client
	sourceManager          *SourceManager
	maxCandidatesPerSource int
}

func New(httpURL, socks5URL string, sourceManager *SourceManager, maxCandidatesPerSource int) *Fetcher {
	return &Fetcher{
		sources:                allSources,
		sourceManager:          sourceManager,
		maxCandidatesPerSource: maxCandidatesPerSource,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchSmart 智能抓取：根据模式和协议需求选择源
func (f *Fetcher) FetchSmart(mode string, preferredProtocol string) ([]domain.Proxy, error) {
	var sources []Source

	switch mode {
	case "emergency":
		// 紧急模式：忽略断路器，强制使用所有源（包括被禁用的）
		sources = f.filterAvailableSources(allSources, preferredProtocol, true)
		log.Printf("[fetch] 🚨 紧急模式: 使用 %d 个源（忽略断路器）", len(sources))

	case "refill":
		// 补充模式：使用快更新源
		sources = f.filterAvailableSources(fastUpdateSources, preferredProtocol, false)
		log.Printf("[fetch] 🔄 补充模式: 使用 %d 个快更新源", len(sources))

	case "optimize":
		// 优化模式：随机选择2-3个慢更新源
		sources = f.selectRandomSources(slowUpdateSources, 3, preferredProtocol)
		log.Printf("[fetch] ⚡ 优化模式: 使用 %d 个源", len(sources))

	default:
		sources = f.filterAvailableSources(fastUpdateSources, preferredProtocol, false)
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no available sources")
	}

	return f.fetchFromSources(sources)
}

// filterAvailableSources 过滤可用的源（通过断路器）
// ignoreCircuitBreaker: 是否忽略断路器（Emergency 模式下使用）
func (f *Fetcher) filterAvailableSources(sources []Source, preferredProtocol string, ignoreCircuitBreaker bool) []Source {
	var available []Source
	for _, src := range sources {
		// 检查断路器（紧急模式下忽略）
		if !ignoreCircuitBreaker && f.sourceManager != nil && !f.sourceManager.CanUseSource(src.URL) {
			continue
		}
		// 如果指定了协议偏好，优先该协议的源
		if preferredProtocol != "" && src.Protocol != "" && src.Protocol != preferredProtocol {
			continue
		}
		available = append(available, src)
	}
	return available
}

// selectRandomSources 随机选择N个源
func (f *Fetcher) selectRandomSources(sources []Source, count int, preferredProtocol string) []Source {
	available := f.filterAvailableSources(sources, preferredProtocol, false)
	if len(available) <= count {
		return available
	}

	// 随机打乱
	shuffled := make([]Source, len(available))
	copy(shuffled, available)
	for i := range shuffled {
		j := i + int(time.Now().UnixNano())%(len(shuffled)-i)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled[:count]
}

// fetchFromSources 从指定源列表抓取
func (f *Fetcher) fetchFromSources(sources []Source) ([]domain.Proxy, error) {
	type result struct {
		proxies []domain.Proxy
		source  Source
		err     error
	}

	ch := make(chan result, len(sources))
	for _, src := range sources {
		go func(s Source) {
			proxies, err := f.fetchFromURL(s.URL, s.Protocol)
			ch <- result{proxies: proxies, source: s, err: err}
		}(src)
	}

	var all []domain.Proxy
	seen := make(map[string]bool)
	for range sources {
		r := <-ch
		if r.err != nil {
			log.Printf("[fetch] ❌ %s error: %v", r.source.URL, r.err)
			if f.sourceManager != nil {
				f.sourceManager.RecordFail(r.source.URL, 3, 5, 30)
			}
			continue
		}
		r.proxies = limitProxyCandidates(r.proxies, f.maxCandidatesPerSource)

		// 记录成功
		if f.sourceManager != nil {
			f.sourceManager.RecordSuccess(r.source.URL)
		}

		// 去重
		var deduped []domain.Proxy
		for _, p := range r.proxies {
			if !seen[p.Address] {
				seen[p.Address] = true
				deduped = append(deduped, p)
			}
		}
		log.Printf("[fetch] ✅ %d 个 %s 代理 from %s", len(deduped), r.source.Protocol, r.source.URL)
		all = append(all, deduped...)
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no proxies fetched")
	}
	log.Printf("[fetch] 总共抓取: %d 个代理（去重后）", len(all))
	return all, nil
}

// Fetch 从所有来源并发抓取代理
func (f *Fetcher) Fetch() ([]domain.Proxy, error) {
	type result struct {
		proxies []domain.Proxy
		source  Source
		err     error
	}

	ch := make(chan result, len(f.sources))
	for _, src := range f.sources {
		go func(s Source) {
			proxies, err := f.fetchFromURL(s.URL, s.Protocol)
			ch <- result{proxies: proxies, source: s, err: err}
		}(src)
	}

	var all []domain.Proxy
	seen := make(map[string]bool)
	for range f.sources {
		r := <-ch
		if r.err != nil {
			log.Printf("fetch %s error: %v", r.source.URL, r.err)
			continue
		}
		r.proxies = limitProxyCandidates(r.proxies, f.maxCandidatesPerSource)
		// 去重
		var deduped []domain.Proxy
		for _, p := range r.proxies {
			if !seen[p.Address] {
				seen[p.Address] = true
				deduped = append(deduped, p)
			}
		}
		log.Printf("fetched %d %s proxies from %s", len(deduped), r.source.Protocol, r.source.URL)
		all = append(all, deduped...)
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no proxies fetched")
	}
	log.Printf("total fetched: %d proxies (deduped)", len(all))
	return all, nil
}

func (f *Fetcher) fetchFromURL(url, protocol string) ([]domain.Proxy, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "GoProxy/1.0 (+https://github.com/isboyjc/GoProxy)")
	req.Header.Set("Accept", "text/plain,*/*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	return parseProxyList(resp.Body, protocol)
}

func limitProxyCandidates(proxies []domain.Proxy, limit int) []domain.Proxy {
	if limit <= 0 || len(proxies) <= limit {
		return proxies
	}
	return proxies[:limit]
}

func parseProxyList(r io.Reader, protocol string) ([]domain.Proxy, error) {
	var proxies []domain.Proxy
	defaultProtocol := normalizeProtocol(protocol)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		addr := line
		proto := defaultProtocol
		// Support protocol://host:port lines.
		if idx := strings.Index(line, "://"); idx != -1 {
			proto = normalizeProtocol(line[:idx])
			addr = line[idx+3:]
		}
		if proto != "http" && proto != "socks5" {
			continue
		}
		addr, ok := normalizeProxyAddress(addr)
		if !ok {
			continue
		}
		proxies = append(proxies, domain.Proxy{
			Address:  addr,
			Protocol: proto,
		})
	}
	return proxies, scanner.Err()
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http", "https":
		// Public "HTTPS" lists usually mean HTTP proxies that support CONNECT.
		return "http"
	case "socks4", "socks5":
		// Keep the previous behavior: try SOCKS4 candidates through the SOCKS5 validator and let validation reject bad ones.
		return "socks5"
	default:
		return ""
	}
}

func normalizeProxyAddress(addr string) (string, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}
	if fields := strings.Fields(addr); len(fields) > 0 {
		addr = fields[0]
	}
	addr = strings.TrimRight(addr, "/")

	parts := strings.Split(addr, ":")
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil || port <= 0 || port > 65535 {
		return "", false
	}
	return parts[0] + ":" + strconv.Itoa(port), true
}
