package optimizer

import (
	"context"
	"log"
	"time"

	"goproxy/config"
	"goproxy/fetcher"
	"goproxy/internal/domain"
	"goproxy/pool"
	"goproxy/validator"
)

// Optimizer 负责在代理池健康时做低频优化替换。
type Optimizer struct {
	fetcher   *fetcher.Fetcher
	validator *validator.Validator
	poolMgr   *pool.Manager
	cfg       *config.Config
}

func NewOptimizer(f *fetcher.Fetcher, v *validator.Validator, pm *pool.Manager, cfg *config.Config) *Optimizer {
	return &Optimizer{
		fetcher:   f,
		validator: v,
		poolMgr:   pm,
		cfg:       cfg,
	}
}

// RunOnce 执行一次优化轮换。
func (o *Optimizer) RunOnce() {
	start := time.Now()
	log.Println("[optimize] 开始执行优化轮换...")

	status, err := o.poolMgr.GetStatus()
	if err != nil {
		log.Printf("[optimize] 获取池状态失败: %v", err)
		return
	}
	if status.State != "healthy" {
		log.Printf("[optimize] 当前池状态为 %s，跳过优化", status.State)
		return
	}

	log.Println("[optimize] 开始抓取优化候选代理...")
	candidates, err := o.fetcher.FetchSmart("optimize", "")
	if err != nil {
		log.Printf("[optimize] 抓取优化候选失败: %v", err)
		return
	}
	log.Printf("[optimize] 抓取到 %d 个候选代理", len(candidates))

	validCandidates := make([]domain.Proxy, 0, len(candidates))
	for result := range o.validator.ValidateStream(candidates) {
		if !result.Valid {
			continue
		}
		latencyMs := int(result.Latency.Milliseconds())
		if latencyMs > o.cfg.MaxLatencyHealthy {
			continue
		}
		validCandidates = append(validCandidates, domain.Proxy{
			Address:      result.Proxy.Address,
			Protocol:     result.Proxy.Protocol,
			ExitIP:       result.ExitIP,
			ExitLocation: result.ExitLocation,
			IPInfo:       result.IPInfo,
			Latency:      latencyMs,
		})
	}

	log.Printf("[optimize] 验证通过 %d 个优质候选（延迟 < %dms）", len(validCandidates), o.cfg.MaxLatencyHealthy)
	if len(validCandidates) == 0 {
		log.Println("[optimize] 没有可替换候选，结束本轮优化")
		return
	}

	replacedCount := 0
	for _, candidate := range validCandidates {
		added, reason := o.poolMgr.TryAddProxy(candidate)
		if added && reason == "replaced" {
			replacedCount++
		}
	}

	log.Printf("[optimize] 优化完成: 替换 %d 个代理，耗时=%v", replacedCount, time.Since(start))
}

// StartBackground 后台定时执行优化。
func (o *Optimizer) StartBackground(ctxs ...context.Context) {
	ctx := context.Background()
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctx = ctxs[0]
	}

	ticker := time.NewTicker(time.Duration(o.cfg.OptimizeInterval) * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[optimize] 优化轮换器已停止")
				return
			case <-ticker.C:
				o.RunOnce()
			}
		}
	}()

	log.Printf("[optimize] 优化轮换器已启动，间隔 %d 分钟", o.cfg.OptimizeInterval)
}
