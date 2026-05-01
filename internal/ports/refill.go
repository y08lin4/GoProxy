package ports

import (
	"context"

	"goproxy/internal/domain"
)

// SmartFetcher fetches proxy candidates according to pool demand.
type SmartFetcher interface {
	FetchSmart(mode string, preferredProtocol string) ([]domain.Proxy, error)
}

// ProxyValidator streams validation results for proxy candidates.
type ProxyValidator interface {
	ValidateStreamContext(ctx context.Context, proxies []domain.Proxy) <-chan domain.ValidationResult
}

// PoolManager captures the pool operations needed by the refill workflow.
type PoolManager interface {
	GetStatus() (*domain.PoolStatus, error)
	NeedsFetch(status *domain.PoolStatus) (bool, string, string)
	NeedsFetchQuick(status *domain.PoolStatus) bool
	TryAddProxy(p domain.Proxy) (bool, string)
}
