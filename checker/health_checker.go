package checker

import (
	"context"
	"log"
	"time"

	"goproxy/config"
	"goproxy/internal/ports"
	"goproxy/pool"
	"goproxy/validator"
)

// HealthChecker 负责定期抽样验证池中代理，并按结果更新或下线。
type HealthChecker struct {
	storage   ports.HealthCheckStore
	validator *validator.Validator
	cfg       *config.Config
	poolMgr   *pool.Manager
}

func NewHealthChecker(s ports.HealthCheckStore, v *validator.Validator, cfg *config.Config, pm *pool.Manager) *HealthChecker {
	return &HealthChecker{
		storage:   s,
		validator: v,
		cfg:       cfg,
		poolMgr:   pm,
	}
}

// RunOnce 执行一次健康检查。
func (hc *HealthChecker) RunOnce() {
	start := time.Now()
	log.Println("[health] 开始执行健康检查...")

	status, err := hc.poolMgr.GetStatus()
	if err != nil {
		log.Printf("[health] 获取池状态失败: %v", err)
		return
	}

	// 当池子健康且高质量代理占比足够高时，跳过 S 级代理的检查。
	skipSGrade := status.State == "healthy"
	dist, _ := hc.storage.GetQualityDistribution()
	sGradeCount := dist["S"]
	if status.Total > 0 && float64(sGradeCount)/float64(status.Total) > 0.3 {
		skipSGrade = true
	}

	proxies, err := hc.storage.GetBatchForHealthCheck(hc.cfg.HealthCheckBatchSize, skipSGrade)
	if err != nil {
		log.Printf("[health] 获取健康检查批次失败: %v", err)
		return
	}
	if len(proxies) == 0 {
		log.Println("[health] 当前没有需要检查的代理")
		return
	}

	log.Printf("[health] 本轮检查 %d 个代理（跳过 S 级: %v）", len(proxies), skipSGrade)

	validCount := 0
	removeCount := 0
	updateCount := 0

	for result := range hc.validator.ValidateStream(proxies) {
		if result.Valid {
			validCount++
			latencyMs := int(result.Latency.Milliseconds())
			if err := hc.storage.UpdateExitInfo(result.Proxy.Address, result.ExitIP, result.ExitLocation, latencyMs, result.IPInfo); err == nil {
				updateCount++
			}
			continue
		}

		hc.storage.IncrementFailCount(result.Proxy.Address)
		if result.Proxy.FailCount+1 >= 3 {
			if result.Proxy.Source == "custom" {
				hc.storage.DisableProxy(result.Proxy.Address)
			} else {
				hc.storage.Delete(result.Proxy.Address)
			}
			removeCount++
		}
	}

	log.Printf("[health] 检查完成: 总数=%d 有效=%d 更新=%d 下线=%d 耗时=%v",
		len(proxies), validCount, updateCount, removeCount, time.Since(start))
}

// StartBackground 后台定时执行健康检查。
func (hc *HealthChecker) StartBackground(ctxs ...context.Context) {
	ctx := context.Background()
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctx = ctxs[0]
	}

	ticker := time.NewTicker(time.Duration(hc.cfg.HealthCheckInterval) * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[health] 健康检查器已停止")
				return
			case <-ticker.C:
				hc.RunOnce()
			}
		}
	}()

	log.Printf("[health] 健康检查器已启动，间隔 %d 分钟", hc.cfg.HealthCheckInterval)
}
