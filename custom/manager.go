package custom

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/proxy"
	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
	"goproxy/validator"
)

const maxSubscriptionFetchBytes = 10 << 20 // 10 MiB

// Manager 订阅管理器
type Manager struct {
	storage      ports.SubscriptionStore
	validator    *validator.Validator
	singbox      *SingBoxProcess
	config       config.Provider
	stopCh       chan struct{}
	stopOnce     sync.Once
	taskMu       sync.RWMutex
	refreshTasks map[string]domain.RefreshTaskStatus
	refreshMu    sync.Mutex // 防止并发刷新
}

// NewManager 创建订阅管理器
func NewManager(store ports.SubscriptionStore, v *validator.Validator, cfg *config.Config, providers ...config.Provider) *Manager {
	dataDir := ""
	if d := os.Getenv("DATA_DIR"); d != "" {
		dataDir = d
	}
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}

	return &Manager{
		storage:      store,
		validator:    v,
		singbox:      NewSingBoxProcess(cfg.SingBoxPath, dataDir, cfg.SingBoxBasePort),
		config:       provider,
		stopCh:       make(chan struct{}),
		refreshTasks: make(map[string]domain.RefreshTaskStatus),
	}
}

func subscriptionTaskKey(subID int64) string {
	return fmt.Sprintf("subscription:%d", subID)
}

func (m *Manager) setRefreshTask(task domain.RefreshTaskStatus) {
	if task.Key == "" {
		return
	}
	task.UpdatedAt = time.Now()
	m.taskMu.Lock()
	m.refreshTasks[task.Key] = task
	m.taskMu.Unlock()
}

func (m *Manager) markRefreshRunning(key string, scope string, subID int64, message string) {
	now := time.Now()
	m.setRefreshTask(domain.RefreshTaskStatus{
		Key:            key,
		SubscriptionID: subID,
		Scope:          scope,
		State:          "running",
		Message:        message,
		StartedAt:      now,
		UpdatedAt:      now,
	})
}

func (m *Manager) markRefreshState(key string, state string, message string, nodeCount int, validCount int) {
	m.taskMu.Lock()
	task := m.refreshTasks[key]
	if task.Key == "" {
		task.Key = key
	}
	task.State = state
	task.Message = message
	task.NodeCount = nodeCount
	task.ValidCount = validCount
	task.UpdatedAt = time.Now()
	if task.StartedAt.IsZero() {
		task.StartedAt = task.UpdatedAt
	}
	if state == "success" || state == "failed" {
		task.FinishedAt = task.UpdatedAt
	}
	m.refreshTasks[key] = task
	m.taskMu.Unlock()
}

func (m *Manager) snapshotRefreshTasks() []domain.RefreshTaskStatus {
	m.taskMu.RLock()
	defer m.taskMu.RUnlock()

	tasks := make([]domain.RefreshTaskStatus, 0, len(m.refreshTasks))
	for _, task := range m.refreshTasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	return tasks
}

// Start 启动后台循环
func (m *Manager) Start(ctxs ...context.Context) {
	log.Println("[custom] 订阅管理器启动")
	if len(ctxs) > 0 && ctxs[0] != nil {
		go func(ctx context.Context) {
			<-ctx.Done()
			m.Stop()
		}(ctxs[0])
	}

	// 启动时立即刷新所有订阅
	go m.initialRefresh()

	// 订阅刷新循环
	go m.refreshLoop()

	// 探测唤醒循环
	go m.probeLoop()
}

// Stop 停止管理器
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.singbox.Stop()
		log.Println("[custom] 订阅管理器已停止")
	})
}

// initialRefresh 启动时刷新所有活跃订阅
func (m *Manager) initialRefresh() {
	select {
	case <-m.stopCh:
		return
	case <-time.After(3 * time.Second): // 等待其他模块初始化
	}
	subs, err := m.storage.GetSubscriptions()
	if err != nil || len(subs) == 0 {
		return
	}

	activeSubs := 0
	for _, sub := range subs {
		if sub.Status == "active" {
			activeSubs++
		}
	}
	if activeSubs == 0 {
		return
	}

	log.Printf("[custom] 启动刷新，共 %d 个活跃订阅", activeSubs)
	m.RefreshAll()
}

// refreshLoop 订阅刷新循环
func (m *Manager) refreshLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkAndRefresh()
		}
	}
}

// checkAndRefresh 检查并刷新到期的订阅 + 清理长期无可用节点的订阅
func (m *Manager) checkAndRefresh() {
	// 清理连续 7 天无可用节点的订阅
	m.cleanupStaleSubscriptions()

	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		log.Printf("[custom] 获取订阅列表失败: %v", err)
		return
	}

	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		// 检查是否到刷新时间
		if !sub.LastFetch.IsZero() && time.Since(sub.LastFetch) < time.Duration(sub.RefreshMin)*time.Minute {
			continue
		}
		log.Printf("[custom] 🔄 订阅 [%s] 到期，开始刷新", sub.Name)
		if err := m.RefreshSubscription(sub.ID); err != nil {
			log.Printf("[custom] ❌ 订阅 [%s] 刷新失败: %v", sub.Name, err)
		}
	}
}

// cleanupStaleSubscriptions 清理连续 7 天无可用节点的订阅
func (m *Manager) cleanupStaleSubscriptions() {
	staleSubs, err := m.storage.GetStaleSubscriptions(7)
	if err != nil || len(staleSubs) == 0 {
		return
	}

	for _, sub := range staleSubs {
		deleted, _ := m.storage.DeleteBySubscriptionID(sub.ID)
		m.storage.DeleteSubscription(sub.ID)
		log.Printf("[custom] 🗑️ 自动移除订阅 [%s]：连续 7 天无可用节点（清理 %d 个代理）", sub.Name, deleted)
	}

	// 重建 sing-box 配置
	if len(staleSubs) > 0 {
		m.RefreshAll()
	}
}

// probeLoop 探测唤醒循环
func (m *Manager) probeLoop() {
	// 等待初始化完成
	select {
	case <-m.stopCh:
		return
	case <-time.After(5 * time.Second):
	}

	for {
		cfg := m.config.Get()
		interval := time.Duration(cfg.CustomProbeInterval) * time.Minute
		if interval < time.Minute {
			interval = 10 * time.Minute
		}

		select {
		case <-m.stopCh:
			return
		case <-time.After(interval):
			m.probeDisabled()
		}
	}
}

// probeDisabled 探测被禁用的订阅代理
func (m *Manager) probeDisabled() {
	disabled, err := m.storage.GetDisabledCustomProxies()
	if err != nil || len(disabled) == 0 {
		return
	}

	log.Printf("[custom] 🔍 探测 %d 个禁用的订阅代理", len(disabled))

	cfg := m.config.Get()
	recovered := 0
	recoveredSubs := make(map[int64]bool)
	for _, proxy := range disabled {
		valid, latency, exitIP, exitLocation, ipInfo := m.validator.ValidateOne(proxy)
		if valid {
			// 检查地理过滤：恢复前确认不在屏蔽列表中
			if exitLocation != "" && isGeoBlocked(exitLocation, cfg) {
				log.Printf("[custom] 代理 %s 验证通过但被地理过滤 (%s)，保持禁用", proxy.Address, exitLocation)
				m.storage.UpdateExitInfo(proxy.Address, exitIP, exitLocation, int(latency.Milliseconds()), ipInfo)
				continue
			}
			m.storage.EnableProxy(proxy.Address)
			m.storage.UpdateExitInfo(proxy.Address, exitIP, exitLocation, int(latency.Milliseconds()), ipInfo)
			recovered++
			recoveredSubs[proxy.SubscriptionID] = true
			log.Printf("[custom] ✅ 代理 %s 恢复可用 (%dms)", proxy.Address, latency.Milliseconds())
		}
	}
	// 有恢复的代理则更新对应订阅的 last_success
	for subID := range recoveredSubs {
		if subID > 0 {
			m.storage.UpdateSubscriptionSuccess(subID)
		}
	}

	if recovered > 0 {
		log.Printf("[custom] 探测完成：%d/%d 恢复可用", recovered, len(disabled))
	}
}

// RefreshSubscription 刷新��个订阅
func (m *Manager) RefreshSubscription(subID int64) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	taskKey := subscriptionTaskKey(subID)
	m.markRefreshRunning(taskKey, "subscription", subID, "开始刷新订阅")

	sub, err := m.storage.GetSubscription(subID)
	if err != nil {
		m.markRefreshState(taskKey, "failed", fmt.Sprintf("获取订阅失败: %v", err), 0, 0)
		return fmt.Errorf("获取订阅失败: %w", err)
	}

	// 获取订阅内容
	data, err := m.fetchSubscriptionData(sub)
	if err != nil {
		m.markRefreshState(taskKey, "failed", fmt.Sprintf("拉取订阅失败: %v", err), 0, 0)
		return fmt.Errorf("拉取订阅内容失败: %w", err)
	}

	// 解析节点
	nodes, err := Parse(data, sub.Format)
	if err != nil {
		m.markRefreshState(taskKey, "failed", fmt.Sprintf("解析订阅失败: %v", err), 0, 0)
		return fmt.Errorf("解析订阅内容失败: %w", err)
	}

	if len(nodes) == 0 {
		log.Printf("[custom] ⚠️ 订阅 [%s] 无有效节点", sub.Name)
		m.markRefreshState(taskKey, "failed", "未解析到有效节点", 0, 0)
		return nil
	}

	log.Printf("[custom] 订阅 [%s] 解析到 %d 个节点", sub.Name, len(nodes))

	// 先删除该订阅的旧代理
	oldDeleted, _ := m.storage.DeleteBySubscriptionID(subID)
	if oldDeleted > 0 {
		log.Printf("[custom] 🧹 清理订阅 [%s] 旧代理 %d 个", sub.Name, oldDeleted)
	}

	// 分类节点
	var directNodes []ParsedNode
	var tunnelNodes []ParsedNode
	for _, node := range nodes {
		if node.IsDirect() {
			directNodes = append(directNodes, node)
		} else {
			tunnelNodes = append(tunnelNodes, node)
		}
	}

	// 收集所有入池的代理（带正确的协议信息）
	var allProxies []domain.Proxy

	// 处理可直接使用的 HTTP/SOCKS5 节点
	for _, node := range directNodes {
		addr := node.DirectAddress()
		proto := node.DirectProtocol()
		m.storage.AddProxyWithSource(addr, proto, "custom", subID)
		allProxies = append(allProxies, domain.Proxy{Address: addr, Protocol: proto, Source: "custom"})
	}
	if len(directNodes) > 0 {
		log.Printf("[custom] 📥 %d 个 HTTP/SOCKS5 节点直接入池", len(directNodes))
	}

	// 处理需要 sing-box 转换的节点
	if len(tunnelNodes) > 0 {
		// 收集所有订阅的 tunnel 节点（需合并）
		allTunnelNodes, err := m.collectAllTunnelNodes()
		if err != nil {
			log.Printf("[custom] ⚠️ 收集 tunnel 节点失败: %v", err)
		}
		// 将当前订阅的 tunnel 节点也加入，去重
		nodeMap := make(map[string]ParsedNode)
		for _, n := range allTunnelNodes {
			nodeMap[n.NodeKey()] = n
		}
		for _, n := range tunnelNodes {
			nodeMap[n.NodeKey()] = n
		}
		var mergedNodes []ParsedNode
		for _, n := range nodeMap {
			mergedNodes = append(mergedNodes, n)
		}

		if err := m.singbox.Reload(mergedNodes); err != nil {
			log.Printf("[custom] ❌ sing-box 重载失败: %v", err)
		} else {
			portMap := m.singbox.GetPortMap()
			for _, node := range tunnelNodes {
				key := node.NodeKey()
				if port, ok := portMap[key]; ok {
					addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
					m.storage.AddProxyWithSource(addr, "socks5", "custom", subID)
					allProxies = append(allProxies, domain.Proxy{Address: addr, Protocol: "socks5", Source: "custom"})
				}
			}
			log.Printf("[custom] 📥 %d 个加密节点通过 sing-box 转换入池", len(tunnelNodes))
		}
	}

	// 验证新入池的代理
	if len(allProxies) == 0 {
		m.markRefreshState(taskKey, "failed", "没有可入池节点", len(nodes), 0)
	} else {
		m.markRefreshState(taskKey, "validating", "节点已入池，正在验证可用性", len(nodes), 0)
		go m.finalizeRefreshTask(taskKey, subID, len(nodes), allProxies)
	}

	// 更新订阅信息（记录实际入池的代理数）
	m.storage.UpdateSubscriptionFetch(subID, len(allProxies))
	log.Printf("[custom] ✅ 订阅 [%s] 刷新完成，解析 %d 节点，入池 %d 个", sub.Name, len(nodes), len(allProxies))

	return nil
}

// RefreshAll 刷新所有活跃订阅
func (m *Manager) RefreshAll() {
	m.markRefreshRunning("all", "all", 0, "开始批量刷新所有订阅")
	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		log.Printf("[custom] 获取订阅列表失败: %v", err)
		m.markRefreshState("all", "failed", fmt.Sprintf("获取订阅列表失败: %v", err), 0, 0)
		return
	}

	triggered := 0
	failed := 0
	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		if err := m.RefreshSubscription(sub.ID); err != nil {
			log.Printf("[custom] ❌ 订阅 [%s] 刷新失败: %v", sub.Name, err)
			failed++
		} else {
			triggered++
		}
	}
	if failed > 0 && triggered == 0 {
		m.markRefreshState("all", "failed", fmt.Sprintf("批量刷新失败: %d 个订阅刷新失败", failed), triggered, 0)
		return
	}
	message := fmt.Sprintf("已触发 %d 个订阅刷新", triggered)
	if failed > 0 {
		message += fmt.Sprintf("，其中 %d 个失败", failed)
	}
	m.markRefreshState("all", "success", message, triggered, 0)
}

// collectAllTunnelNodes 收集所有订阅中需要 tunnel 的节点
func (m *Manager) collectAllTunnelNodes() ([]ParsedNode, error) {
	subs, err := m.storage.GetSubscriptions()
	if err != nil {
		return nil, err
	}

	var allNodes []ParsedNode
	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}
		data, err := m.fetchSubscriptionData(&sub)
		if err != nil {
			continue
		}
		nodes, err := Parse(data, sub.Format)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if !node.IsDirect() {
				allNodes = append(allNodes, node)
			}
		}
	}
	return allNodes, nil
}

// fetchSubscriptionData 获取订阅数据
func (m *Manager) fetchSubscriptionData(sub *domain.Subscription) ([]byte, error) {
	// 优先使用本地文件
	if sub.FilePath != "" {
		data, err := os.ReadFile(sub.FilePath)
		if err != nil {
			return nil, fmt.Errorf("读取文件 %s 失败: %w", sub.FilePath, err)
		}
		return data, nil
	}

	// 从 URL 拉取
	if sub.URL == "" {
		return nil, fmt.Errorf("订阅未配置 URL 或文件路径")
	}

	// 尝试拉取（直连 → 代理）
	data, err := m.fetchWithRetry(sub.URL)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// fetchWithRetry 尝试拉取 URL（直连 → 代理，多种方式）
func (m *Manager) fetchWithRetry(urlStr string) ([]byte, error) {
	// 先尝试直连
	data, err := m.fetchURL(urlStr, nil)
	if err == nil {
		return data, nil
	}
	log.Printf("[custom] 直连订阅 URL 失败: %v，尝试通过代理访问...", err)

	// 直连失败，尝试通过池中已有代理访问
	for i := 0; i < 3; i++ {
		p, pErr := m.storage.GetRandom()
		if pErr != nil {
			break
		}
		data, err = m.fetchURL(urlStr, p)
		if err == nil {
			log.Printf("[custom] ✅ 通过代理 %s 成功访问订阅 URL", p.Address)
			return data, nil
		}
		log.Printf("[custom] 代理 %s 访问订阅 URL 失败: %v", p.Address, err)
	}

	return nil, fmt.Errorf("直连和代理均无法访问订阅 URL: %w", err)
}

// fetchURL 通过指定代理（或直连）拉取 URL 内容
func (m *Manager) fetchURL(urlStr string, p *domain.Proxy) ([]byte, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported subscription URL scheme: %s", parsedURL.Scheme)
	}

	transport := &http.Transport{}

	if p != nil {
		// Route subscription fetch through the selected proxy.
		switch p.Protocol {
		case "socks5":
			dialer, err := proxy.SOCKS5("tcp", p.Address, nil, proxy.Direct)
			if err != nil {
				return nil, err
			}
			transport.Dial = dialer.Dial
		default: // http
			proxyURL, err := url.Parse(fmt.Sprintf("http://%s", p.Address))
			if err != nil {
				return nil, err
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	// 用 v2rayN UA，大部分机场都会返回完整的节点信息
	req.Header.Set("User-Agent", "v2rayN")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return readSubscriptionResponse(resp.Body)
}

func readSubscriptionResponse(r io.Reader) ([]byte, error) {
	limited := &io.LimitedReader{R: r, N: maxSubscriptionFetchBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxSubscriptionFetchBytes {
		return nil, fmt.Errorf("subscription response exceeds %d bytes", maxSubscriptionFetchBytes)
	}
	return data, nil
}

// validateCustomProxies 验证订阅代理，返回可用数
func (m *Manager) validateCustomProxies(proxies []domain.Proxy, subID int64) int {
	if len(proxies) == 0 {
		return 0
	}

	log.Printf("[custom] 🔍 开始验证 %d 个订阅代理", len(proxies))

	cfg := m.config.Get()
	resultCh := m.validator.ValidateStream(proxies)
	valid, invalid := 0, 0
	for result := range resultCh {
		if result.Valid {
			latencyMs := int(result.Latency.Milliseconds())
			m.storage.UpdateExitInfo(result.Proxy.Address, result.ExitIP, result.ExitLocation, latencyMs, result.IPInfo)
			// 检查地理过滤
			if result.ExitLocation != "" && isGeoBlocked(result.ExitLocation, cfg) {
				m.storage.DisableProxy(result.Proxy.Address)
				invalid++
			} else {
				m.storage.EnableProxy(result.Proxy.Address)
				valid++
			}
		} else {
			invalid++
			m.storage.DisableProxy(result.Proxy.Address)
		}
	}

	// 有可用节点则更新 last_success
	if valid > 0 && subID > 0 {
		m.storage.UpdateSubscriptionSuccess(subID)
	}

	log.Printf("[custom] 验证完成：%d 可用，%d 不可用", valid, invalid)
	return valid
}

func (m *Manager) finalizeRefreshTask(taskKey string, subID int64, nodeCount int, proxies []domain.Proxy) {
	valid := m.validateCustomProxies(proxies, subID)
	if valid > 0 {
		m.markRefreshState(taskKey, "success", fmt.Sprintf("刷新完成，%d/%d 个节点可用", valid, len(proxies)), nodeCount, valid)
		return
	}
	m.markRefreshState(taskKey, "failed", "刷新完成，但没有可用节点", nodeCount, 0)
}

// GetStatus 获取订阅管理器状态
func (m *Manager) GetStatus() map[string]interface{} {
	customCount, _ := m.storage.CountBySource("custom")
	disabled, _ := m.storage.GetDisabledCustomProxies()
	subs, _ := m.storage.GetSubscriptions()

	return map[string]interface{}{
		"singbox_running":    m.singbox.IsRunning(),
		"singbox_nodes":      m.singbox.GetNodeCount(),
		"custom_count":       customCount,
		"disabled_count":     len(disabled),
		"subscription_count": len(subs),
		"refresh_tasks":      m.snapshotRefreshTasks(),
	}
}

// ValidateSubscription 验证订阅能否解析出节点（不入库，仅检查）
func (m *Manager) ValidateSubscription(url, filePath string) (int, error) {
	var data []byte
	var err error

	if filePath != "" {
		data, err = os.ReadFile(filePath)
		if err != nil {
			return 0, fmt.Errorf("读取文件失败: %w", err)
		}
	} else if url != "" {
		data, err = m.fetchWithRetry(url)
		if err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("未提供 URL 或文件")
	}

	nodes, err := Parse(data, "auto")
	if err != nil {
		return 0, err
	}
	if len(nodes) == 0 {
		return 0, fmt.Errorf("解析结果为空，未找到有效代理节点")
	}

	return len(nodes), nil
}

// isGeoBlocked 检查代理出口位置是否被地理过滤
func isGeoBlocked(exitLocation string, cfg *config.Config) bool {
	if exitLocation == "" || len(exitLocation) < 2 {
		return false
	}
	countryCode := exitLocation[:2]

	// 白名单模式优先
	if len(cfg.AllowedCountries) > 0 {
		for _, allowed := range cfg.AllowedCountries {
			if countryCode == allowed {
				return false
			}
		}
		return true // 不在白名单中
	}

	// 黑名单模式
	for _, blocked := range cfg.BlockedCountries {
		if countryCode == blocked {
			return true
		}
	}
	return false
}

// GetSingBox 获取 sing-box 进程管理器
func (m *Manager) GetSingBox() *SingBoxProcess {
	return m.singbox
}
