package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"goproxy/config"
	"goproxy/internal/ports"
)

// SOCKS5Server 提供 SOCKS5 协议入口。
type SOCKS5Server struct {
	cfg      *config.Config
	mode     string // "random" or "lowest-latency"
	port     string
	selector *Selector
	reporter *FailureReporter
}

func NewSOCKS5(s ports.ProxyRuntimeStore, cfg *config.Config, mode string, port string) *SOCKS5Server {
	provider := config.StaticProvider{Config: cfg}
	return &SOCKS5Server{
		cfg:      cfg,
		mode:     mode,
		port:     port,
		selector: NewSelector(s, provider),
		reporter: NewFailureReporter(s),
	}
}

func (s *SOCKS5Server) Start() error {
	return s.Run(context.Background())
}

func (s *SOCKS5Server) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	modeDesc := "随机轮换"
	if s.mode == "lowest-latency" {
		modeDesc = "最低延迟"
	}
	authStatus := "无需认证"
	if s.cfg.ProxyAuthEnabled {
		authStatus = fmt.Sprintf("需要认证（用户: %s）", s.cfg.ProxyAuthUsername)
	}

	log.Printf("[socks5] SOCKS5 代理监听 %s [%s] [%s]", s.port, modeDesc, authStatus)

	listener, err := net.Listen("tcp", s.port)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go s.handleConnection(conn)
	}
}

// handleConnection 处理一个 SOCKS5 客户端连接。
func (s *SOCKS5Server) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	if err := s.socks5Handshake(clientConn); err != nil {
		log.Printf("[socks5] 握手失败: %v", err)
		return
	}

	target, err := s.readSOCKS5Request(clientConn)
	if err != nil {
		log.Printf("[socks5] 读取请求失败: %v", err)
		return
	}

	tried := []string{}
	maxRetries := s.cfg.MaxRetry + 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		p, err := s.selector.Select(tried, "socks5", s.mode == "lowest-latency")
		if err != nil {
			log.Printf("[socks5] 没有可用的上游 SOCKS5 代理: %v", err)
			s.sendSOCKS5Reply(clientConn, 0x01)
			return
		}

		tried = append(tried, p.Address)

		upstreamConn, err := dialUpstreamProxy(p, target, time.Duration(s.cfg.ValidateTimeout)*time.Second)
		if err != nil {
			log.Printf("[socks5] 通过 %s 连接 %s 失败（%s）: %v", p.Address, target, p.Protocol, err)
			s.reporter.Failure(p)
			continue
		}

		if err := s.sendSOCKS5Reply(clientConn, 0x00); err != nil {
			upstreamConn.Close()
			return
		}

		s.reporter.Success(p)
		log.Printf("[socks5] %s 通过 %s 建立成功", target, p.Address)

		go io.Copy(upstreamConn, clientConn)
		io.Copy(clientConn, upstreamConn)
		upstreamConn.Close()
		return
	}

	s.sendSOCKS5Reply(clientConn, 0x01)
	log.Printf("[socks5] 所有上游代理均失败: %s", target)
}

// socks5Handshake 处理 SOCKS5 握手。
func (s *SOCKS5Server) socks5Handshake(conn net.Conn) error {
	buf := make([]byte, 257)

	n, err := io.ReadAtLeast(conn, buf, 2)
	if err != nil {
		return err
	}

	version := buf[0]
	if version != 0x05 {
		return fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	nmethods := int(buf[1])
	if n < 2+nmethods {
		if _, err := io.ReadFull(conn, buf[n:2+nmethods]); err != nil {
			return err
		}
	}

	needAuth := s.cfg.ProxyAuthEnabled
	methods := buf[2 : 2+nmethods]

	var selectedMethod byte = 0xFF
	if needAuth {
		for _, method := range methods {
			if method == 0x02 {
				selectedMethod = 0x02
				break
			}
		}
	} else {
		for _, method := range methods {
			if method == 0x00 {
				selectedMethod = 0x00
				break
			}
		}
	}

	if _, err := conn.Write([]byte{0x05, selectedMethod}); err != nil {
		return err
	}
	if selectedMethod == 0xFF {
		return fmt.Errorf("no acceptable authentication method")
	}

	if selectedMethod == 0x02 {
		if err := s.socks5Auth(conn); err != nil {
			return err
		}
	}

	return nil
}

// socks5Auth 处理 SOCKS5 用户名/密码认证。
func (s *SOCKS5Server) socks5Auth(conn net.Conn) error {
	buf := make([]byte, 513)

	n, err := io.ReadAtLeast(conn, buf, 2)
	if err != nil {
		return err
	}

	if buf[0] != 0x01 {
		return fmt.Errorf("unsupported auth version: %d", buf[0])
	}

	ulen := int(buf[1])
	if n < 2+ulen {
		if _, err := io.ReadFull(conn, buf[n:2+ulen]); err != nil {
			return err
		}
		n = 2 + ulen
	}

	username := string(buf[2 : 2+ulen])

	if n < 2+ulen+1 {
		if _, err := io.ReadFull(conn, buf[n:2+ulen+1]); err != nil {
			return err
		}
		n = 2 + ulen + 1
	}

	plen := int(buf[2+ulen])
	if n < 2+ulen+1+plen {
		if _, err := io.ReadFull(conn, buf[n:2+ulen+1+plen]); err != nil {
			return err
		}
	}

	password := string(buf[2+ulen+1 : 2+ulen+1+plen])

	if username != s.cfg.ProxyAuthUsername || password != s.cfg.ProxyAuthPassword {
		conn.Write([]byte{0x01, 0x01})
		return fmt.Errorf("authentication failed")
	}

	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return err
	}
	return nil
}

// readSOCKS5Request 解析客户端的 SOCKS5 CONNECT 请求。
func (s *SOCKS5Server) readSOCKS5Request(conn net.Conn) (string, error) {
	buf := make([]byte, 262)

	n, err := io.ReadAtLeast(conn, buf, 4)
	if err != nil {
		return "", err
	}

	if buf[0] != 0x05 {
		return "", fmt.Errorf("invalid version: %d", buf[0])
	}

	cmd := buf[1]
	if cmd != 0x01 {
		s.sendSOCKS5Reply(conn, 0x07)
		return "", fmt.Errorf("unsupported command: %d", cmd)
	}

	atyp := buf[3]
	var host string
	var addrLen int

	switch atyp {
	case 0x01: // IPv4
		addrLen = 4
		if n < 4+addrLen+2 {
			if _, err := io.ReadFull(conn, buf[n:4+addrLen+2]); err != nil {
				return "", err
			}
		}
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
	case 0x03: // Domain
		addrLen = int(buf[4])
		if n < 4+1+addrLen+2 {
			if _, err := io.ReadFull(conn, buf[n:4+1+addrLen+2]); err != nil {
				return "", err
			}
		}
		host = string(buf[5 : 5+addrLen])
	case 0x04: // IPv6
		addrLen = 16
		if n < 4+addrLen+2 {
			if _, err := io.ReadFull(conn, buf[n:4+addrLen+2]); err != nil {
				return "", err
			}
		}
		host = net.IP(buf[4 : 4+addrLen]).String()
	default:
		s.sendSOCKS5Reply(conn, 0x08)
		return "", fmt.Errorf("unsupported address type: %d", atyp)
	}

	portOffset := 4 + addrLen
	if atyp == 0x03 {
		portOffset = 5 + addrLen
	}
	port := binary.BigEndian.Uint16(buf[portOffset : portOffset+2])

	return fmt.Sprintf("%s:%d", host, port), nil
}

// sendSOCKS5Reply 向客户端返回 SOCKS5 响应。
func (s *SOCKS5Server) sendSOCKS5Reply(conn net.Conn, rep byte) error {
	reply := []byte{
		0x05, // VER
		rep,  // REP
		0x00, // RSV
		0x01, // ATYP: IPv4
		0, 0, 0, 0,
		0, 0,
	}
	_, err := conn.Write(reply)
	return err
}
