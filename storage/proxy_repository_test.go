package storage

import (
	"path/filepath"
	"testing"
)

func TestGetRandomExcludeFilteredDoesNotFallbackToTriedProxy(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.AddProxyWithSource("10.0.0.1:8080", "http", "free"); err != nil {
		t.Fatalf("add proxy: %v", err)
	}

	p, err := store.GetRandomExcludeFiltered([]string{"10.0.0.1:8080"}, "")
	if err == nil {
		t.Fatalf("expected no available proxy, got %#v", p)
	}
}
