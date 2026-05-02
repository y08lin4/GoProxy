package validator

import (
	"context"
	"testing"
	"time"

	"goproxy/internal/domain"
)

func TestValidateStreamContextCanceledBeforeDispatch(t *testing.T) {
	v := New(1, 1, "http://127.0.0.1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := v.ValidateStreamContext(ctx, []domain.Proxy{
		{Address: "127.0.0.1:1", Protocol: "http"},
	})

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("ValidateStreamContext did not close after cancellation")
	}
}
