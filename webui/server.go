package webui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"goproxy/config"
	"goproxy/custom"
	"goproxy/internal/ports"
	appservice "goproxy/internal/service"
	"goproxy/logger"
	"goproxy/pool"
	"goproxy/storage"
)

// in-memory sessions
var (
	sessions   = make(map[string]time.Time)
	sessionsMu sync.Mutex
)

const (
	sessionTTL                 = 24 * time.Hour
	maxSubscriptionUploadBytes = 10 << 20 // 10 MiB
)

func newSession() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	now := time.Now()

	sessionsMu.Lock()
	cleanupExpiredSessionsLocked(now)
	sessions[token] = now.Add(sessionTTL)
	sessionsMu.Unlock()
	return token, nil
}

func validSession(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil {
		return false
	}

	now := time.Now()
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	expiry, ok := sessions[cookie.Value]
	if !ok {
		return false
	}
	if !now.Before(expiry) {
		delete(sessions, cookie.Value)
		return false
	}
	return true
}

func cleanupExpiredSessionsLocked(now time.Time) {
	for token, expiry := range sessions {
		if !now.Before(expiry) {
			delete(sessions, token)
		}
	}
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

type FetchTrigger func()

type Server struct {
	storage       *storage.Storage
	cfg           *config.Config
	poolMgr       *pool.Manager
	customMgr     *custom.Manager
	proxyAdmin    *appservice.ProxyAdminService
	fetchTrigger  FetchTrigger
	configChanged chan<- struct{}
}

func New(s *storage.Storage, cfg *config.Config, pm *pool.Manager, cm *custom.Manager, geoIP ports.GeoIPResolver, ft FetchTrigger, cc chan<- struct{}) *Server {
	return &Server{
		storage:       s,
		cfg:           cfg,
		poolMgr:       pm,
		customMgr:     cm,
		proxyAdmin:    appservice.NewProxyAdminService(s, geoIP),
		fetchTrigger:  ft,
		configChanged: cc,
	}
}

func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	mux := http.NewServeMux()

	// 添加日志中间件
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[webui] %s %s | Host: %s | RemoteAddr: %s",
			r.Method, r.URL.Path, r.Host, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// 只读 API（访客可访问）
	mux.HandleFunc("/api/stats", s.readOnlyMiddleware(s.apiStats))
	mux.HandleFunc("/api/proxies", s.readOnlyMiddleware(s.apiProxies))
	mux.HandleFunc("/api/logs", s.readOnlyMiddleware(s.apiLogs))
	mux.HandleFunc("/api/pool/status", s.readOnlyMiddleware(s.apiPoolStatus))
	mux.HandleFunc("/api/pool/quality", s.readOnlyMiddleware(s.apiQualityDistribution))
	mux.HandleFunc("/api/config", s.readOnlyMiddleware(s.apiConfig))
	mux.HandleFunc("/api/auth/check", s.apiAuthCheck) // 检查登录状态

	// 管理员 API（需要登录）
	mux.HandleFunc("/api/proxy/delete", s.authMiddleware(s.apiDeleteProxy))
	mux.HandleFunc("/api/proxy/refresh", s.authMiddleware(s.apiRefreshProxy))
	mux.HandleFunc("/api/fetch", s.authMiddleware(s.apiFetch))
	mux.HandleFunc("/api/refresh-latency", s.authMiddleware(s.apiRefreshLatency))
	mux.HandleFunc("/api/config/save", s.authMiddleware(s.apiConfigSave))

	// 订阅管理 API
	mux.HandleFunc("/api/subscriptions", s.readOnlyMiddleware(s.apiSubscriptions))
	mux.HandleFunc("/api/custom/status", s.readOnlyMiddleware(s.apiCustomStatus))
	mux.HandleFunc("/api/subscription/contribute", s.apiSubscriptionContribute) // 访客可用
	mux.HandleFunc("/api/subscription/add", s.authMiddleware(s.apiSubscriptionAdd))
	mux.HandleFunc("/api/subscription/delete", s.authMiddleware(s.apiSubscriptionDelete))
	mux.HandleFunc("/api/subscription/refresh", s.authMiddleware(s.apiSubscriptionRefresh))
	mux.HandleFunc("/api/subscription/refresh-all", s.authMiddleware(s.apiSubscriptionRefreshAll))
	mux.HandleFunc("/api/subscription/toggle", s.authMiddleware(s.apiSubscriptionToggle))

	log.Printf("WebUI listening on %s", s.cfg.WebUIPort)
	server := &http.Server{Addr: s.cfg.WebUIPort, Handler: loggedMux}
	listener, err := net.Listen("tcp", s.cfg.WebUIPort)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			log.Printf("[webui] shutdown error: %v", err)
		}
	}()

	err = server.Serve(listener)
	if err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
		return err
	}
	return nil
}

func (s *Server) Start(ctxs ...context.Context) {
	ctx := context.Background()
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctx = ctxs[0]
	}
	go func() {
		if err := s.Run(ctx); err != nil {
			log.Printf("[webui] server stopped with error: %v", err)
		}
	}()
}

// authMiddleware 管理员权限中间件（必须登录）
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validSession(r) {
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				jsonError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

// readOnlyMiddleware 只读中间件（访客可访问，但会标记是否为管理员）
func (s *Server) readOnlyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 访客和管理员都可以访问，通过 validSession 判断权限
		next(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// 允许访客访问（只读模式），管理员登录后有完整权限
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTML)
		return
	}
	password := r.FormValue("password")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	if hash != s.cfg.WebUIPasswordHash {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTMLWithError)
		return
	}
	token, err := newSession()
	if err != nil {
		http.Error(w, "create session failed", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, r, token, 0, time.Now().Add(sessionTTL))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}
	setSessionCookie(w, r, "", -1, time.Now().Add(-time.Hour))
	http.Redirect(w, r, "/login", http.StatusFound)
}

// apiAuthCheck 检查当前用户是否为管理员
func (s *Server) apiAuthCheck(w http.ResponseWriter, r *http.Request) {
	isAdmin := validSession(r)
	jsonOK(w, map[string]interface{}{
		"isAdmin": isAdmin,
		"mode": func() string {
			if isAdmin {
				return "admin"
			}
			return "guest"
		}(),
	})
}

func (s *Server) apiStats(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.proxyAdmin.Stats(s.cfg.ProxyPort))
}

func (s *Server) apiProxies(w http.ResponseWriter, r *http.Request) {
	protocol := r.URL.Query().Get("protocol")
	proxies, err := s.proxyAdmin.List(protocol)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, proxies)
}

func (s *Server) apiDeleteProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	_ = s.proxyAdmin.Delete(req.Address)
	jsonOK(w, map[string]string{"status": "deleted"})
}

func (s *Server) apiRefreshProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Address == "" {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.proxyAdmin.RefreshProxyAsync(req.Address)
	jsonOK(w, map[string]string{"status": "refresh started"})
}

func (s *Server) apiFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go s.fetchTrigger()
	jsonOK(w, map[string]string{"status": "fetch started"})
}

func (s *Server) apiRefreshLatency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.proxyAdmin.RefreshLatencyAsync()
	jsonOK(w, map[string]string{"status": "refresh started"})
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	lines := logger.GetLines(100)
	jsonOK(w, map[string]interface{}{"lines": lines})
}

// apiConfig 获取配置
func (s *Server) apiConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	httpSlots, socks5Slots := cfg.CalculateSlots()

	jsonOK(w, map[string]interface{}{
		// 池子配置
		"pool_max_size":         cfg.PoolMaxSize,
		"pool_http_ratio":       cfg.PoolHTTPRatio,
		"pool_min_per_protocol": cfg.PoolMinPerProtocol,
		"pool_http_slots":       httpSlots,
		"pool_socks5_slots":     socks5Slots,

		// 延迟配置
		"max_latency_ms":        cfg.MaxLatencyMs,
		"max_latency_emergency": cfg.MaxLatencyEmergency,
		"max_latency_healthy":   cfg.MaxLatencyHealthy,

		// 验证配置
		"validate_concurrency":      cfg.ValidateConcurrency,
		"validate_timeout":          cfg.ValidateTimeout,
		"max_candidates_per_source": cfg.MaxCandidatesPerSource,

		// 健康检查配置
		"health_check_interval":   cfg.HealthCheckInterval,
		"health_check_batch_size": cfg.HealthCheckBatchSize,

		// 优化配置
		"optimize_interval": cfg.OptimizeInterval,
		"replace_threshold": cfg.ReplaceThreshold,

		// 地理过滤配置
		"blocked_countries": cfg.BlockedCountries,
		"allowed_countries": cfg.AllowedCountries,

		// 自定义订阅代理配置
		"custom_proxy_mode":       cfg.CustomProxyMode,
		"custom_priority":         cfg.CustomPriority,
		"custom_free_priority":    cfg.CustomFreePriority,
		"custom_probe_interval":   cfg.CustomProbeInterval,
		"custom_refresh_interval": cfg.CustomRefreshInterval,
	})
}

// apiConfigSave 保存配置
func (s *Server) apiConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PoolMaxSize            int      `json:"pool_max_size"`
		PoolHTTPRatio          float64  `json:"pool_http_ratio"`
		PoolMinPerProtocol     int      `json:"pool_min_per_protocol"`
		MaxLatencyMs           int      `json:"max_latency_ms"`
		MaxLatencyEmergency    int      `json:"max_latency_emergency"`
		MaxLatencyHealthy      int      `json:"max_latency_healthy"`
		ValidateConcurrency    int      `json:"validate_concurrency"`
		ValidateTimeout        int      `json:"validate_timeout"`
		MaxCandidatesPerSource *int     `json:"max_candidates_per_source"`
		HealthCheckInterval    int      `json:"health_check_interval"`
		HealthCheckBatchSize   int      `json:"health_check_batch_size"`
		OptimizeInterval       int      `json:"optimize_interval"`
		ReplaceThreshold       float64  `json:"replace_threshold"`
		BlockedCountries       []string `json:"blocked_countries"`
		AllowedCountries       []string `json:"allowed_countries"`
		CustomProxyMode        string   `json:"custom_proxy_mode"`
		CustomPriority         *bool    `json:"custom_priority"`
		CustomFreePriority     *bool    `json:"custom_free_priority"`
		CustomProbeInterval    int      `json:"custom_probe_interval"`
		CustomRefreshInterval  int      `json:"custom_refresh_interval"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 验证配置有效性
	if req.PoolMaxSize <= 0 || req.PoolHTTPRatio <= 0 || req.PoolHTTPRatio > 1 {
		jsonError(w, "invalid pool config", http.StatusBadRequest)
		return
	}

	// 记录旧配置
	oldCfg := config.Get()
	oldSize := oldCfg.PoolMaxSize
	oldRatio := oldCfg.PoolHTTPRatio

	// 更新配置
	newCfg := *oldCfg
	newCfg.PoolMaxSize = req.PoolMaxSize
	newCfg.PoolHTTPRatio = req.PoolHTTPRatio
	newCfg.PoolMinPerProtocol = req.PoolMinPerProtocol
	newCfg.MaxLatencyMs = req.MaxLatencyMs
	newCfg.MaxLatencyEmergency = req.MaxLatencyEmergency
	newCfg.MaxLatencyHealthy = req.MaxLatencyHealthy
	newCfg.ValidateConcurrency = req.ValidateConcurrency
	newCfg.ValidateTimeout = req.ValidateTimeout
	if req.MaxCandidatesPerSource != nil {
		newCfg.MaxCandidatesPerSource = *req.MaxCandidatesPerSource
	}
	newCfg.HealthCheckInterval = req.HealthCheckInterval
	newCfg.HealthCheckBatchSize = req.HealthCheckBatchSize
	newCfg.OptimizeInterval = req.OptimizeInterval
	newCfg.ReplaceThreshold = req.ReplaceThreshold
	newCfg.BlockedCountries = req.BlockedCountries
	newCfg.AllowedCountries = req.AllowedCountries
	if req.CustomProxyMode != "" {
		newCfg.CustomProxyMode = req.CustomProxyMode
	}
	if req.CustomPriority != nil {
		newCfg.CustomPriority = *req.CustomPriority
		if *req.CustomPriority {
			newCfg.CustomFreePriority = false // 互斥
		}
	}
	if req.CustomFreePriority != nil {
		newCfg.CustomFreePriority = *req.CustomFreePriority
		if *req.CustomFreePriority {
			newCfg.CustomPriority = false // 互斥
		}
	}
	if req.CustomProbeInterval > 0 {
		newCfg.CustomProbeInterval = req.CustomProbeInterval
	}
	if req.CustomRefreshInterval > 0 {
		newCfg.CustomRefreshInterval = req.CustomRefreshInterval
	}

	if err := config.Save(&newCfg); err != nil {
		jsonError(w, "save config error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 通知配置变更
	select {
	case s.configChanged <- struct{}{}:
	default:
	}

	// 如果池子大小或比例变更，调整池子
	if oldSize != req.PoolMaxSize || oldRatio != req.PoolHTTPRatio {
		go s.poolMgr.AdjustForConfigChange(oldSize, oldRatio)
	}

	log.Printf("[config] 配置已更新: 池子=%d HTTP=%.0f%% 延迟=%dms",
		req.PoolMaxSize, req.PoolHTTPRatio*100, req.MaxLatencyMs)
	jsonOK(w, map[string]string{"status": "saved"})
}

// apiPoolStatus 获取池子状态
func (s *Server) apiPoolStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.poolMgr.GetStatus()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, status)
}

// apiQualityDistribution 获取质量分布
func (s *Server) apiQualityDistribution(w http.ResponseWriter, r *http.Request) {
	dist, err := s.storage.GetQualityDistribution()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, dist)
}

// ========== 订阅管理 API ==========

// apiSubscriptions 获取订阅列表（含每个订阅的可用/不可用代理数）
func (s *Server) apiSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.storage.GetSubscriptions()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if subs == nil {
		subs = []storage.Subscription{}
	}

	// 附加每个订阅的代理统计
	type subWithStats struct {
		storage.Subscription
		ActiveCount   int `json:"active_count"`
		DisabledCount int `json:"disabled_count"`
	}
	var result []subWithStats
	for _, sub := range subs {
		active, disabled := s.storage.CountBySubscriptionID(sub.ID)
		result = append(result, subWithStats{
			Subscription:  sub,
			ActiveCount:   active,
			DisabledCount: disabled,
		})
	}
	jsonOK(w, result)
}

// apiCustomStatus 获取订阅代理状态
func (s *Server) apiCustomStatus(w http.ResponseWriter, r *http.Request) {
	if s.customMgr == nil {
		jsonOK(w, map[string]interface{}{
			"singbox_running":    false,
			"singbox_nodes":      0,
			"custom_count":       0,
			"disabled_count":     0,
			"subscription_count": 0,
		})
		return
	}
	jsonOK(w, s.customMgr.GetStatus())
}

// apiSubscriptionContribute 访客贡献订阅（支持 URL 和文件上传，需验证通过才入库）
func (s *Server) apiSubscriptionContribute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		FileContent string `json:"file_content"`
	}
	if err := decodeJSONLimited(w, r, &req, maxSubscriptionUploadBytes); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.URL == "" && req.FileContent == "" {
		jsonError(w, "请填写订阅 URL 或上传配置文件", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "贡献订阅"
	}

	// 如果上传了文件，保存到本地
	filePath := ""
	if req.FileContent != "" {
		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = "."
		}
		subDir := filepath.Join(dataDir, "subscriptions")
		os.MkdirAll(subDir, 0755)
		filePath = filepath.Join(subDir, fmt.Sprintf("contribute_%d.yaml", time.Now().UnixMilli()))
		if err := os.WriteFile(filePath, []byte(req.FileContent), 0644); err != nil {
			jsonError(w, "保存文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		filePath, _ = filepath.Abs(filePath)
	}

	// 先验证能解析出节点
	if s.customMgr != nil {
		nodeCount, err := s.customMgr.ValidateSubscription(req.URL, filePath)
		if err != nil {
			if filePath != "" {
				os.Remove(filePath)
			}
			jsonError(w, "订阅验证失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[webui] 访客贡献订阅验证通过: %s (%d 个节点)", req.Name, nodeCount)
	}

	// 入库
	refreshMin := config.Get().CustomRefreshInterval
	var id int64
	var err error
	if req.URL != "" {
		id, err = s.storage.AddContributedSubscription(req.Name, req.URL, refreshMin)
	} else {
		// 文件上传的贡献，用 AddSubscription + contributed 标记
		id, err = s.storage.AddSubscription(req.Name, "", filePath, "auto", refreshMin)
		if err == nil {
			// 标记为贡献
			err = s.storage.MarkSubscriptionContributed(id)
		}
	}
	if err != nil {
		if filePath != "" {
			os.Remove(filePath)
		}
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 异步刷新入池
	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(id); err != nil {
				log.Printf("[webui] 贡献订阅刷新失败: %v", err)
			}
		}()
	}

	log.Printf("[webui] 🎁 访客贡献订阅: %s (url=%v file=%v)", req.Name, req.URL != "", filePath != "")
	jsonOK(w, map[string]interface{}{"status": "contributed", "id": id})
}

// apiSubscriptionAdd 添加订阅
func (s *Server) apiSubscriptionAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		FileContent string `json:"file_content"` // 上传的文件内容（Base64 编码）
		RefreshMin  int    `json:"refresh_min"`
	}
	if err := decodeJSONLimited(w, r, &req, maxSubscriptionUploadBytes); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.URL == "" && req.FileContent == "" {
		jsonError(w, "请填写订阅 URL 或上传配置文件", http.StatusBadRequest)
		return
	}
	if req.RefreshMin <= 0 {
		req.RefreshMin = config.Get().CustomRefreshInterval
	}
	if req.Name == "" {
		req.Name = "订阅"
	}

	// 如果上传了文件内容，保存到本地
	filePath := ""
	if req.FileContent != "" {
		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = "."
		}
		subDir := filepath.Join(dataDir, "subscriptions")
		os.MkdirAll(subDir, 0755)
		filePath = filepath.Join(subDir, fmt.Sprintf("sub_%d.yaml", time.Now().UnixMilli()))
		if err := os.WriteFile(filePath, []byte(req.FileContent), 0644); err != nil {
			jsonError(w, "保存文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		filePath, _ = filepath.Abs(filePath)
	}

	// 先验证：拉取并解析，确认能解析出节点后再入库
	if s.customMgr != nil {
		nodeCount, err := s.customMgr.ValidateSubscription(req.URL, filePath)
		if err != nil {
			// 清理已保存的文件
			if filePath != "" {
				os.Remove(filePath)
			}
			jsonError(w, "订阅验证失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[webui] 订阅验证通过: %s (%d 个节点)", req.Name, nodeCount)
	}

	id, err := s.storage.AddSubscription(req.Name, req.URL, filePath, "auto", req.RefreshMin)
	if err != nil {
		jsonError(w, "add subscription error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 验证已通过，异步执行入池
	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(id); err != nil {
				log.Printf("[webui] 订阅刷新失败: %v", err)
			}
		}()
	}

	log.Printf("[webui] 添加订阅: %s (url=%v file=%v)", req.Name, req.URL != "", filePath != "")
	jsonOK(w, map[string]interface{}{"status": "added", "id": id})
}

// apiSubscriptionDelete 删除订阅
func (s *Server) apiSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 先删除该订阅关联的代理
	if s.customMgr != nil {
		deleted, _ := s.storage.DeleteBySubscriptionID(req.ID)
		if deleted > 0 {
			log.Printf("[webui] 清理订阅 #%d 关联的 %d 个代理", req.ID, deleted)
		}
	}

	if err := s.storage.DeleteSubscription(req.ID); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 重建 sing-box 配置（剩余订阅的节点）
	if s.customMgr != nil {
		go s.customMgr.RefreshAll()
	}

	log.Printf("[webui] 删除订阅 #%d", req.ID)
	jsonOK(w, map[string]string{"status": "deleted"})
}

// apiSubscriptionRefresh 刷新单个订阅
func (s *Server) apiSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.customMgr != nil {
		go func() {
			if err := s.customMgr.RefreshSubscription(req.ID); err != nil {
				log.Printf("[webui] 订阅 #%d 刷新失败: %v", req.ID, err)
			}
		}()
	}

	jsonOK(w, map[string]string{"status": "refresh started"})
}

// apiSubscriptionRefreshAll 刷新所有订阅
func (s *Server) apiSubscriptionRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.customMgr != nil {
		go s.customMgr.RefreshAll()
	}

	jsonOK(w, map[string]string{"status": "refresh all started"})
}

// apiSubscriptionToggle 切换订阅状态
func (s *Server) apiSubscriptionToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID <= 0 {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := s.storage.ToggleSubscription(req.ID); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "toggled"})
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
