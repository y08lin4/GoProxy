package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewSessionUsesValidCookieToken(t *testing.T) {
	resetSessionsForTest()

	token, err := newSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 32-byte hex token, got length %d", len(token))
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	if !validSession(req) {
		t.Fatal("expected freshly-created session to be valid")
	}
}

func TestValidSessionDeletesExpiredToken(t *testing.T) {
	resetSessionsForTest()

	sessionsMu.Lock()
	sessions["expired"] = time.Now().Add(-time.Second)
	sessionsMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "expired"})
	if validSession(req) {
		t.Fatal("expected expired session to be invalid")
	}

	sessionsMu.Lock()
	_, exists := sessions["expired"]
	sessionsMu.Unlock()
	if exists {
		t.Fatal("expected expired session to be removed")
	}
}

func resetSessionsForTest() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions = make(map[string]time.Time)
}
