package service

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
)

// RefillService coordinates source fetching, validation, and pool insertion.
//
// It is intentionally kept as an application-level service: fetcher, validator
// and pool keep their focused responsibilities, while the end-to-end refill
// workflow no longer lives in main.go.
type RefillService struct {
	fetcher   ports.SmartFetcher
	validator ports.ProxyValidator
	pool      ports.PoolManager

	config  config.Provider
	running atomic.Bool
}

func NewRefillService(fetch ports.SmartFetcher, validate ports.ProxyValidator, poolMgr ports.PoolManager, providers ...config.Provider) *RefillService {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &RefillService{
		fetcher:   fetch,
		validator: validate,
		pool:      poolMgr,
		config:    provider,
	}
}

// Run performs one smart fetch/validate/fill cycle. Concurrent calls are
// coalesced: if a refill is already running, the new call returns immediately.
func (s *RefillService) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.running.CompareAndSwap(false, true) {
		log.Println("[refill] 抓取正在运行，跳过")
		return
	}
	defer s.running.Store(false)

	status, err := s.pool.GetStatus()
	if err != nil {
		log.Printf("[refill] 获取池子状态失败: %v", err)
		return
	}

	cfg := s.config.Get()
	log.Printf("[refill] 池子状态: %s | HTTP=%d/%d SOCKS5=%d/%d 总计=%d/%d",
		status.State, status.HTTP, status.HTTPSlots, status.SOCKS5, status.SOCKS5Slots,
		status.Total, cfg.PoolMaxSize)

	needFetch, mode, preferredProtocol := s.pool.NeedsFetch(status)
	if !needFetch {
		log.Println("[refill] 池子健康，无需抓取")
		return
	}

	log.Printf("[refill] 智能抓取: 模式=%s 协议偏好=%s", mode, preferredProtocol)
	candidates, err := s.fetcher.FetchSmart(mode, preferredProtocol)
	if err != nil {
		log.Printf("[refill] 抓取失败: %v", err)
		return
	}

	httpCandidates, socks5Candidates := splitCandidatesByProtocol(candidates)
	log.Printf("[refill] 抓取到 %d 个候选代理（SOCKS5=%d HTTP=%d），按协议并发验证...",
		len(candidates), len(socks5Candidates), len(httpCandidates))

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var addedCount atomic.Int32
	var validCount atomic.Int32
	var rejectedNoExit atomic.Int32
	var rejectedLatency atomic.Int32
	var rejectedGeo atomic.Int32
	var rejectedFull atomic.Int32

	processResult := func(result domain.ValidationResult) {
		if !result.Valid {
			return
		}

		validCount.Add(1)
		latencyMs := int(result.Latency.Milliseconds())

		cfg := s.config.Get()
		maxLatency := cfg.GetLatencyThreshold(status.State)

		if result.ExitIP == "" || result.ExitLocation == "" {
			rejectedNoExit.Add(1)
			return
		}

		if latencyMs > maxLatency {
			rejectedLatency.Add(1)
			return
		}

		proxyToAdd := domain.Proxy{
			Address:      result.Proxy.Address,
			Protocol:     result.Proxy.Protocol,
			ExitIP:       result.ExitIP,
			ExitLocation: result.ExitLocation,
			IPInfo:       result.IPInfo,
			Latency:      latencyMs,
		}

		if added, reason := s.pool.TryAddProxy(proxyToAdd); added {
			addedCount.Add(1)
		} else if reason == "slots_full" {
			rejectedFull.Add(1)
		} else if len(result.ExitLocation) >= 2 {
			countryCode := result.ExitLocation[:2]
			for _, blocked := range cfg.BlockedCountries {
				if countryCode == blocked {
					rejectedGeo.Add(1)
					break
				}
			}
		}
	}

	poolFilled := func() bool {
		currentStatus, err := s.pool.GetStatus()
		if err != nil {
			return false
		}
		return !s.pool.NeedsFetchQuick(currentStatus)
	}

	var wg sync.WaitGroup
	validateGroup := func(name string, proxies []domain.Proxy) {
		defer wg.Done()

		count := 0
		for result := range s.validator.ValidateStreamContext(runCtx, proxies) {
			processResult(result)
			count++
			if count%20 == 0 && poolFilled() {
				log.Printf("[refill] %s 验证中检测到池子已满，取消剩余验证", name)
				cancel()
				break
			}
		}
		log.Printf("[refill] %s 验证完成，处理 %d 个", name, count)
	}

	if len(socks5Candidates) > 0 {
		wg.Add(1)
		go validateGroup("SOCKS5", socks5Candidates)
	}
	if len(httpCandidates) > 0 {
		wg.Add(1)
		go validateGroup("HTTP", httpCandidates)
	}

	wg.Wait()

	finalStatus, err := s.pool.GetStatus()
	if err != nil {
		log.Printf("[refill] 填充完成，但获取最终池子状态失败: %v", err)
		return
	}
	log.Printf("[refill] 填充完成: 候选%d 通过%d 入池%d | 拒绝[无出口:%d 延迟:%d 地理:%d 满:%d] | 最终 %s HTTP=%d SOCKS5=%d",
		len(candidates), validCount.Load(), addedCount.Load(),
		rejectedNoExit.Load(), rejectedLatency.Load(), rejectedGeo.Load(), rejectedFull.Load(),
		finalStatus.State, finalStatus.HTTP, finalStatus.SOCKS5)
}

func splitCandidatesByProtocol(candidates []domain.Proxy) (httpCandidates, socks5Candidates []domain.Proxy) {
	for _, c := range candidates {
		if c.Protocol == "http" {
			httpCandidates = append(httpCandidates, c)
		} else {
			socks5Candidates = append(socks5Candidates, c)
		}
	}
	return httpCandidates, socks5Candidates
}
