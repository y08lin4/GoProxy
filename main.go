package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
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

const managedServers = 5

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Init()
	cfg := config.Load()
	provider := config.StaticProvider{Config: cfg}

	if os.Getenv("WEBUI_PASSWORD") == "" {
		log.Printf("[main] WebUI 使用默认密码: %s（可通过环境变量 WEBUI_PASSWORD 自定义）", config.DefaultPassword)
	} else {
		log.Println("[main] WebUI 密码已通过环境变量 WEBUI_PASSWORD 设置")
	}

	log.Printf("[main] 智能代理池配置: 容量=%d HTTP=%.0f%% SOCKS5=%.0f%% 延迟阈值=%dms",
		cfg.PoolMaxSize, cfg.PoolHTTPRatio*100, (1-cfg.PoolHTTPRatio)*100, cfg.MaxLatencyMs)

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}
	defer store.Close()

	geoResolver := geoip.NewResolver(cfg.IPQueryRateLimit)
	sourceMgr := fetcher.NewSourceManager(store.GetDB())
	fetch := fetcher.New(cfg.HTTPSourceURL, cfg.SOCKS5SourceURL, sourceMgr, cfg.MaxCandidatesPerSource, provider)
	validate := validator.NewWithGeoIP(cfg.ValidateConcurrency, cfg.ValidateTimeout, cfg.ValidateURL, geoResolver, provider)
	poolMgr := pool.NewManager(store, cfg)
	healthChecker := checker.NewHealthChecker(store, validate, cfg, poolMgr)
	opt := optimizer.NewOptimizer(fetch, validate, poolMgr, cfg)
	refillSvc := service.NewRefillService(fetch, validate, poolMgr, provider)
	customMgr := custom.NewManager(store, validate, cfg, provider)
	defer customMgr.Stop()
	proxyAdmin := service.NewProxyAdminService(store, geoResolver, provider)
	sourceAdmin := service.NewSourceAdminService(fetch, sourceMgr, provider)
	subscriptionAdmin := service.NewSubscriptionAdminService(store, customMgr, provider)

	totalDeleted := cleanupInvalidProxies(store, cfg)

	randomServer := proxy.New(store, cfg, "random", cfg.ProxyPort)
	stableServer := proxy.New(store, cfg, "lowest-latency", cfg.StableProxyPort)
	socks5RandomServer := proxy.NewSOCKS5(store, cfg, "random", cfg.SOCKS5Port)
	socks5StableServer := proxy.NewSOCKS5(store, cfg, "lowest-latency", cfg.StableSOCKS5Port)

	configChanged := make(chan struct{}, 1)
	ui := webui.New(cfg, poolMgr, proxyAdmin, sourceAdmin, subscriptionAdmin, func() {
		refillSvc.Run(ctx)
	}, configChanged, provider)

	serverErrCh := make(chan error, managedServers)
	serverDoneCh := make(chan string, managedServers)

	go runServer(ctx, "webui", serverErrCh, serverDoneCh, func() error { return ui.Run(ctx) })
	go runServer(ctx, "stable http proxy server", serverErrCh, serverDoneCh, func() error { return stableServer.Run(ctx) })
	go runServer(ctx, "stable socks5 proxy server", serverErrCh, serverDoneCh, func() error { return socks5StableServer.Run(ctx) })
	go runServer(ctx, "random socks5 proxy server", serverErrCh, serverDoneCh, func() error { return socks5RandomServer.Run(ctx) })
	go runServer(ctx, "random http proxy server", serverErrCh, serverDoneCh, func() error { return randomServer.Run(ctx) })

	go func() {
		if totalDeleted > 0 {
			log.Printf("[main] 清理完成后立即启动补池流程...")
		} else {
			log.Println("[main] 启动初始化补池流程...")
		}
		refillSvc.Run(ctx)
	}()

	go startStatusMonitor(ctx, poolMgr, func() {
		refillSvc.Run(ctx)
	})
	healthChecker.StartBackground(ctx)
	opt.StartBackground(ctx)
	customMgr.Start(ctx)
	go watchConfigChanges(ctx, configChanged, poolMgr, provider)

	var shutdownErr error
	select {
	case <-ctx.Done():
		log.Println("[main] 收到退出信号，正在停止服务...")
	case err := <-serverErrCh:
		shutdownErr = err
		log.Printf("[main] 检测到服务异常，准备退出: %v", err)
		stop()
	}

	waitForServers(serverDoneCh)

	if shutdownErr != nil {
		log.Fatalf("[main] server stopped: %v", shutdownErr)
	}
	log.Println("[main] 所有服务已停止")
}

func cleanupInvalidProxies(store *storage.Storage, cfg *config.Config) int {
	totalDeleted := 0

	if len(cfg.AllowedCountries) > 0 {
		if deleted, err := store.DeleteNotAllowedCountries(cfg.AllowedCountries); err == nil && deleted > 0 {
			log.Printf("[main] 已清理 %d 个非白名单免费代理（允许国家: %v）", deleted, cfg.AllowedCountries)
			totalDeleted += int(deleted)
		}
		if disabled, err := store.DisableNotAllowedCountries(cfg.AllowedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 已禁用 %d 个非白名单订阅代理", disabled)
		}
	} else if len(cfg.BlockedCountries) > 0 {
		if deleted, err := store.DeleteBlockedCountries(cfg.BlockedCountries); err == nil && deleted > 0 {
			log.Printf("[main] 已清理 %d 个屏蔽国家免费代理（屏蔽国家: %v）", deleted, cfg.BlockedCountries)
			totalDeleted += int(deleted)
		}
		if disabled, err := store.DisableBlockedCountries(cfg.BlockedCountries); err == nil && disabled > 0 {
			log.Printf("[main] 已禁用 %d 个屏蔽国家订阅代理", disabled)
		}
	}

	if deleted, err := store.DeleteWithoutExitInfo(); err == nil && deleted > 0 {
		log.Printf("[main] 已清理 %d 个缺少出口信息的代理", deleted)
		totalDeleted += int(deleted)
	}

	return totalDeleted
}

func runServer(ctx context.Context, name string, errCh chan<- error, doneCh chan<- string, run func() error) {
	defer func() {
		doneCh <- name
	}()

	if err := run(); err != nil && ctx.Err() == nil {
		errCh <- fmt.Errorf("%s: %w", name, err)
		return
	}
	log.Printf("[main] %s stopped", name)
}

func waitForServers(doneCh <-chan string) {
	timeout := time.NewTimer(8 * time.Second)
	defer timeout.Stop()

	for i := 0; i < managedServers; i++ {
		select {
		case name := <-doneCh:
			log.Printf("[main] 已停止服务: %s", name)
		case <-timeout.C:
			log.Println("[main] 等待服务停止超时，直接退出")
			return
		}
	}
}

// startStatusMonitor 周期性检查代理池状态，并在需要时触发补池。
func startStatusMonitor(ctx context.Context, poolMgr *pool.Manager, triggerFetch func()) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("[monitor] 状态监控器已启动（每 30 秒检查一次）")

	for {
		select {
		case <-ctx.Done():
			log.Println("[monitor] 状态监控器已停止")
			return
		case <-ticker.C:
			status, err := poolMgr.GetStatus()
			if err != nil {
				continue
			}

			needFetch, mode, preferredProtocol := poolMgr.NeedsFetch(status)
			if needFetch {
				log.Printf("[monitor] 检测到补池需求: 状态=%s 模式=%s 协议=%s",
					status.State, mode, preferredProtocol)
				go triggerFetch()
			}
		}
	}
}

// watchConfigChanges 监听配置变更，并在池子容量或比例变化时调整池状态。
func watchConfigChanges(ctx context.Context, configChanged <-chan struct{}, poolMgr *pool.Manager, provider config.Provider) {
	cfg := provider.Get()
	oldSize := cfg.PoolMaxSize
	oldRatio := cfg.PoolHTTPRatio

	for {
		select {
		case <-ctx.Done():
			log.Println("[config] 配置监听已停止")
			return
		case <-configChanged:
			newCfg := provider.Get()
			if newCfg.PoolMaxSize != oldSize || newCfg.PoolHTTPRatio != oldRatio {
				log.Printf("[config] 检测到配置变更: 容量 %d→%d 比例 %.2f→%.2f",
					oldSize, newCfg.PoolMaxSize, oldRatio, newCfg.PoolHTTPRatio)
				poolMgr.AdjustForConfigChange(oldSize, oldRatio)
				oldSize = newCfg.PoolMaxSize
				oldRatio = newCfg.PoolHTTPRatio
			}
		}
	}
}
