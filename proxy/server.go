package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"goproxy/config"
	"goproxy/internal/ports"
)

type Server struct {
	cfg      *config.Config
	mode     string // "random" or "lowest-latency"
	port     string
	selector *Selector
	reporter *FailureReporter
}

func New(s ports.ProxyRuntimeStore, cfg *config.Config, mode string, port string) *Server {
	return &Server{
		cfg:      cfg,
		mode:     mode,
		port:     port,
		selector: NewSelector(s),
		reporter: NewFailureReporter(s),
	}
}

func (s *Server) Start() error {
	modeDesc := "随机轮换"
	if s.mode == "lowest-latency" {
		modeDesc = "最低延迟"
	}
	authStatus := "无认证"
	if s.cfg.ProxyAuthEnabled {
		authStatus = fmt.Sprintf("需认证 (用户: %s)", s.cfg.ProxyAuthUsername)
	}
	log.Printf("proxy server listening on %s [%s] [%s]", s.port, modeDesc, authStatus)
	return http.ListenAndServe(s.port, s)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 认证检查（如果启用）
	if s.cfg.ProxyAuthEnabled {
		if !s.checkAuth(r) {
			w.Header().Set("Proxy-Authenticate", `Basic realm="GoProxy"`)
			http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
			return
		}
	}

	if r.Method == http.MethodConnect {
		s.handleTunnel(w, r)
	} else {
		s.handleHTTP(w, r)
	}
}

// checkAuth 验证代理 Basic Auth
func (s *Server) checkAuth(r *http.Request) bool {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return false
	}

	// 解析 Basic Auth
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return false
	}

	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return false
	}

	username := credentials[0]
	password := credentials[1]

	// 验证用户名和密码
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.cfg.ProxyAuthUsername)) == 1
	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	passwordMatch := subtle.ConstantTimeCompare([]byte(passwordHash), []byte(s.cfg.ProxyAuthPasswordHash)) == 1

	return usernameMatch && passwordMatch
}

// handleHTTP 处理普通 HTTP 请求（带自动重试）
func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	var tried []string
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selector.Select(tried, "", s.mode == "lowest-latency")
		if err != nil {
			http.Error(w, "no available proxy", http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.Address)

		client, err := buildUpstreamHTTPClient(p, time.Duration(s.cfg.ValidateTimeout)*time.Second)
		if err != nil {
			s.reporter.Remove(p)
			continue
		}

		// 转发请求（使用完整 URL，上游代理通过 client transport 设置）
		req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			continue
		}
		req.Header = r.Header.Clone()
		req.Header.Del("Proxy-Connection")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[proxy] %s via %s failed, removing", r.RequestURI, p.Address)
			s.reporter.Failure(p)
			continue
		}
		defer resp.Body.Close()

		// 写回响应
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		s.reporter.Success(p)
		if resp.StatusCode == 429 {
			log.Printf("[proxy] ⚠️  429 %s via %s (protocol=%s)", r.RequestURI, p.Address, p.Protocol)
		} else {
			log.Printf("[proxy] %s via %s -> %d", r.RequestURI, p.Address, resp.StatusCode)
		}
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

// handleTunnel 处理 HTTPS CONNECT 隧道（带自动重试）
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	var tried []string
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		p, err := s.selector.Select(tried, "", s.mode == "lowest-latency")
		if err != nil {
			http.Error(w, "no available proxy", http.StatusServiceUnavailable)
			return
		}

		tried = append(tried, p.Address)

		conn, err := dialUpstreamProxy(p, r.Host, time.Duration(s.cfg.ValidateTimeout)*time.Second)
		if err != nil {
			log.Printf("[tunnel] dial %s via %s failed, removing", r.Host, p.Address)
			s.reporter.Failure(p)
			continue
		}

		s.reporter.Success(p)

		// 告知客户端隧道建立
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			conn.Close()
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			conn.Close()
			return
		}

		fmt.Fprintf(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		log.Printf("[tunnel] %s via %s established", r.Host, p.Address)

		// 双向转发
		go transfer(conn, clientConn)
		go transfer(clientConn, conn)
		return
	}

	http.Error(w, "all proxies failed", http.StatusBadGateway)
}

func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}
