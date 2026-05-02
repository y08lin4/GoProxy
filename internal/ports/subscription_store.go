package ports

import "goproxy/internal/domain"

// SubscriptionStore captures persistence operations required by custom subscription management.
type SubscriptionStore interface {
	GetSubscriptions() ([]domain.Subscription, error)
	GetSubscription(id int64) (*domain.Subscription, error)
	GetStaleSubscriptions(staleDays int) ([]domain.Subscription, error)
	DeleteSubscription(id int64) error
	UpdateSubscriptionFetch(id int64, proxyCount int) error
	UpdateSubscriptionSuccess(id int64) error

	AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error
	DeleteBySubscriptionID(subscriptionID int64) (int64, error)
	GetRandom() (*domain.Proxy, error)
	GetDisabledCustomProxies() ([]domain.Proxy, error)
	EnableProxy(address string) error
	DisableProxy(address string) error
	UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error
	CountBySource(source string) (int, error)
}
