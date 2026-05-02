package main

import (
	"context"
	"log"
	"os"
	"time"

	"goproxy/checker"
	"goproxy/config"
	"goproxy/custom"
	"goproxy/fetcher"
	"goproxy/internal/geoip"
	"goproxy/internal/service"
	"goproxy/logger"
	"goproxy/optimizer"
	"goproxy/pool"
	"goproxy/proxy"
	"goproxy/storage"
	"goproxy/validator"
	"goproxy/webui"
)

func main() {
	// 初始化日志收集器
	logger.Init()

	// 加载配置
	cfg := config.Load()

	// 提示密码信息
	if os.Getenv("WEBUI_PASSWORD") == "" {
		log.Printf("[main] WebUI 使用默认密码: %s（可通过环境变量 WEBUI_PASSWORD 自定义）", config.DefaultPassword)
	} else {
		log.Println("[main] WebUI 密码已通过环境变量 WEBUI_PASSWORD 设置")
	}

	log.Printf("[main] 🎯 智能代理池配置: 容量=%d HTTP=%.0f%% SOCKS5=%.0f%% 延迟标准=%dms",
		cfg.PoolMaxSize, cfg.PoolHTTPRatio*100, (1-cfg.PoolHTTPRatio)*100, cfg.MaxLatencyMs)

	// 初始化存储
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}
	defer store.Close()

	// 初始化出口 IP/地理信息解析器
	geoResolver := geoip.NewResolver(cfg.IPQueryRateLimit)

	// 初始化核心模块
	sourceMgr := fetcher.NewSourceManager(store.GetDB())
	fetch := fetcher.New(cfg.HTTPSourceURL, cfg.SOCKS5SourceURL, sourceMgr, cfg.MaxCandidatesPerSource)
	validate := validator.NewWithGeoIP(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL, geoResolver)
	poolMgr := pool.NewManager(store, cfg)
	healthChecker := checker.NewHealthChecker(store, validate, cfg, poolMgr)
	opt := optimizer.NewOptimizer(fetch, validate, poolMgr, cfg)
	refillSvc := service.NewRefillService(fetch, validate, poolMgr)

	// 清理无效代理（免费代理删除，订阅代理禁用）
	totalDeleted := 0
	if len(cfg.AllowedCountries) > 0 {
		if deleted, err := store.DeleteNotAllowedCountries(cfg.AllowedCountries); err == nil && deleted > 0 {
			log.Printf("[main] 🧹 已清理 %d 个非白名单免费代理 (允许: %v)", deleted, cfg.AllowedCountries)
			totalDeleted += int(deleted)
		}
		if disabled, err := store.DisableNotAllowedCountries(cfg.AllowedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 🔒 已禁用 %d 个非白名单订阅代理", disabled)
		}
	} else if len(cfg.BlockedCountries) > 0 {
		if deleted, err := store.DeleteBlockedCountries(cfg.BlockedCountries); err == nil && deleted > 0 {
			log.Printf("[main] 🧹 已清理 %d 个屏蔽国家免费代理 (屏蔽: %v)", deleted, cfg.BlockedCountries)
			totalDeleted += int(deleted)
		}
		if disabled, err := store.DisableBlockedCountries(cfg.BlockedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 🔒 已禁用 %d 个屏蔽国家订阅代理", disabled)
		}
	}
	if deleted, err := store.DeleteWithoutExitInfo(); err == nil && deleted > 0 {
		log.Printf("[main] 🧹 已清理 %d 个无出口信息的代理", deleted)
		totalDeleted += int(deleted)
	}

	// 创建 HTTP 代理服务器：随机轮换 + 最低延迟
	randomServer := proxy.New(store, cfg, "random", cfg.ProxyPort)
	stableServer := proxy.New(store, cfg, "lowest-latency", cfg.StableProxyPort)

	// 创建 SOCKS5 代理服务器：随机轮换 + 最低延迟
	socks5RandomServer := proxy.NewSOCKS5(store, cfg, "random", cfg.SOCKS5Port)
	socks5StableServer := proxy.NewSOCKS5(store, cfg, "lowest-latency", cfg.StableSOCKS5Port)

	// 初始化订阅管理器
	customMgr := custom.NewManager(store, validate, cfg)

	// 配置变更通知 channel
	configChanged := make(chan struct{}, 1)

	// 启动 WebUI（传递池子管理器和订阅管理器）
	ui := webui.New(store, cfg, poolMgr, customMgr, geoResolver, func() {
		refillSvc.Run(context.Background())
	}, configChanged)
	ui.Start()

	// 首次智能填充（清理后立即触发）
	go func() {
		if totalDeleted > 0 {
			log.Printf("[main] 🚀 清理后立即启动补充填充...")
		} else {
			log.Println("[main] 🚀 启动初始化填充...")
		}
		refillSvc.Run(context.Background())
	}()

	// 启动状态监控协程
	go startStatusMonitor(poolMgr, func() {
		refillSvc.Run(context.Background())
	})

	// 启动健康检查器
	healthChecker.StartBackground()

	// 启动优化轮换器
	opt.StartBackground()

	// 启动订阅管理器
	go customMgr.Start()

	// 监听配置变更
	go watchConfigChanges(configChanged, poolMgr)

	// 启动 HTTP 稳定代理服务（最低延迟模式）
	go func() {
		if err := stableServer.Start(); err != nil {
			log.Fatalf("stable http proxy server: %v", err)
		}
	}()

	// 启动 SOCKS5 稳定代理服务（最低延迟模式）
	go func() {
		if err := socks5StableServer.Start(); err != nil {
			log.Fatalf("stable socks5 proxy server: %v", err)
		}
	}()

	// 启动 SOCKS5 随机代理服务
	go func() {
		if err := socks5RandomServer.Start(); err != nil {
			log.Fatalf("random socks5 proxy server: %v", err)
		}
	}()

	// 启动 HTTP 随机代理服务（阻塞）
	if err := randomServer.Start(); err != nil {
		log.Fatalf("random http proxy server: %v", err)
	}
}

// startStatusMonitor 状态监控协程
func startStatusMonitor(poolMgr *pool.Manager, triggerFetch func()) {
	ticker := time.NewTicker(30 * time.Second)
	log.Println("[monitor] 📡 状态监控器已启动（每30秒检查）")

	for range ticker.C {
		status, err := poolMgr.GetStatus()
		if err != nil {
			continue
		}

		// 每分钟检查池子状态
		needFetch, mode, preferredProtocol := poolMgr.NeedsFetch(status)
		if needFetch {
			log.Printf("[monitor] ⚠️  检测到池子需求: 状态=%s 模式=%s 协议=%s",
				status.State, mode, preferredProtocol)
			// 触发智能填充
			go triggerFetch()
		}
	}
}

// watchConfigChanges 监听配置变更
func watchConfigChanges(configChanged <-chan struct{}, poolMgr *pool.Manager) {
	var oldSize int
	var oldRatio float64

	cfg := config.Get()
	oldSize = cfg.PoolMaxSize
	oldRatio = cfg.PoolHTTPRatio

	for range configChanged {
		newCfg := config.Get()
		if newCfg.PoolMaxSize != oldSize || newCfg.PoolHTTPRatio != oldRatio {
			log.Printf("[config] 🔧 配置变更检测: 容量 %d→%d 比例 %.2f→%.2f",
				oldSize, newCfg.PoolMaxSize, oldRatio, newCfg.PoolHTTPRatio)
			poolMgr.AdjustForConfigChange(oldSize, oldRatio)
			oldSize = newCfg.PoolMaxSize
			oldRatio = newCfg.PoolHTTPRatio
		}
	}
}
