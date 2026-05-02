package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"goproxy/internal/domain"
)

// dialUpstreamProxy establishes a TCP tunnel to target through an upstream proxy.
func dialUpstreamProxy(p *domain.Proxy, target string, timeout time.Duration) (net.Conn, error) {
	switch p.Protocol {
	case "http":
		return dialHTTPConnectProxy(p.Address, target, timeout)
	case "socks5":
		return dialSOCKS5Proxy(p.Address, target, timeout)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

// buildUpstreamHTTPClient builds an HTTP client that sends requests through p.
func buildUpstreamHTTPClient(p *domain.Proxy, timeout time.Duration) (*http.Client, error) {
	switch p.Protocol {
	case "http":
		proxyURL, err := url.Parse(fmt.Sprintf("http://%s", p.Address))
		if err != nil {
			return nil, err
		}
		return &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			Timeout:   timeout,
		}, nil
	case "socks5":
		return &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					return dialSOCKS5Proxy(p.Address, address, timeout)
				},
			},
			Timeout: timeout,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

func dialHTTPConnectProxy(proxyAddr, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, timeout)
	if err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read upstream CONNECT response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("upstream proxy connect failed: %s", resp.Status)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}

	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(b)
	}
	return c.Conn.Read(b)
}

func dialSOCKS5Proxy(proxyAddr, target string, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}

	handshake := make([]byte, 2)
	if _, err := io.ReadFull(conn, handshake); err != nil {
		conn.Close()
		return nil, err
	}
	if handshake[0] != 0x05 || handshake[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 handshake failed")
	}

	req, err := buildSOCKS5ConnectRequest(target)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	if err := readSOCKS5ConnectReply(conn); err != nil {
		conn.Close()
		return nil, err
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func buildSOCKS5ConnectRequest(target string) ([]byte, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 0 || portNum > 65535 {
		return nil, fmt.Errorf("invalid target port: %s", port)
	}

	req := []byte{0x05, 0x01, 0x00} // VER, CMD=CONNECT, RSV
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			ip16 := ip.To16()
			if ip16 == nil {
				return nil, fmt.Errorf("invalid ip address: %s", host)
			}
			req = append(req, 0x04)
			req = append(req, ip16...)
		}
	} else {
		if len(host) > 255 {
			return nil, fmt.Errorf("domain name too long: %s", host)
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portNum))
	req = append(req, portBytes...)
	return req, nil
}

func readSOCKS5ConnectReply(r io.Reader) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return err
	}

	if header[0] != 0x05 {
		return fmt.Errorf("invalid socks5 reply version: %d", header[0])
	}
	if header[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed, code: %d", header[1])
	}

	var addrLen int
	switch header[3] {
	case 0x01:
		addrLen = net.IPv4len
	case 0x03:
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(r, lenByte); err != nil {
			return err
		}
		addrLen = int(lenByte[0])
	case 0x04:
		addrLen = net.IPv6len
	default:
		return fmt.Errorf("unsupported socks5 reply address type: %d", header[3])
	}

	if _, err := io.CopyN(io.Discard, r, int64(addrLen+2)); err != nil {
		return err
	}
	return nil
}
