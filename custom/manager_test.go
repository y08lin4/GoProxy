package custom

import (
	"bytes"
	"testing"
)

func TestReadSubscriptionResponseAllowsBoundedBody(t *testing.T) {
	data, err := readSubscriptionResponse(bytes.NewReader([]byte("proxy-data")))
	if err != nil {
		t.Fatalf("read subscription response: %v", err)
	}
	if string(data) != "proxy-data" {
		t.Fatalf("unexpected data %q", data)
	}
}

func TestReadSubscriptionResponseRejectsOversizedBody(t *testing.T) {
	oversized := bytes.Repeat([]byte{'x'}, maxSubscriptionFetchBytes+1)
	if _, err := readSubscriptionResponse(bytes.NewReader(oversized)); err == nil {
		t.Fatal("expected oversized subscription response to fail")
	}
}
