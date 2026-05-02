package ports

// SubscriptionRuntime captures runtime operations backed by the custom subscription manager.
type SubscriptionRuntime interface {
	ValidateSubscription(url, filePath string) (int, error)
	RefreshSubscription(id int64) error
	RefreshAll()
	GetStatus() map[string]interface{}
}
