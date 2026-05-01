package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDialHTTPConnectProxyAcceptsAnyHTTPVersionWith200(t *testing.T) {
	addr := serveHTTPConnectResponse(t, "HTTP/1.0 200 Connection Established\r\n\r\n")

	conn, err := dialHTTPConnectProxy(addr, "example.com:443", time.Second)
	if err != nil {
		t.Fatalf("dial http connect proxy: %v", err)
	}
	conn.Close()
}

func TestDialHTTPConnectProxyRejectsNon200(t *testing.T) {
	addr := serveHTTPConnectResponse(t, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")

	conn, err := dialHTTPConnectProxy(addr, "example.com:443", time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected non-200 CONNECT response to fail")
	}
}

func TestReadSOCKS5ConnectReplyConsumesFullDomainReply(t *testing.T) {
	reply := append([]byte{0x05, 0x00, 0x00, 0x03, byte(len("example.com"))}, []byte("example.com")...)
	reply = append(reply, 0x1f, 0x90)
	reply = append(reply, []byte("TLS")...)

	r := bytes.NewReader(reply)
	if err := readSOCKS5ConnectReply(r); err != nil {
		t.Fatalf("read socks5 reply: %v", err)
	}

	remaining, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remaining bytes: %v", err)
	}
	if string(remaining) != "TLS" {
		t.Fatalf("unexpected remaining bytes %q", remaining)
	}
}

func TestReadSOCKS5ConnectReplyRejectsFailureCode(t *testing.T) {
	r := bytes.NewReader([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if err := readSOCKS5ConnectReply(r); err == nil {
		t.Fatal("expected socks5 failure reply to return error")
	}
}

func serveHTTPConnectResponse(t *testing.T, response string) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}

		_, _ = io.WriteString(conn, response)
		if strings.Contains(response, " 200 ") {
			time.Sleep(50 * time.Millisecond)
		}
	}()

	return ln.Addr().String()
}
