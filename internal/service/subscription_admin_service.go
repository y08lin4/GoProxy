package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"goproxy/config"
	"goproxy/internal/domain"
	"goproxy/internal/ports"
)

// SubscriptionAdminService contains WebUI subscription administration use-cases.
type SubscriptionAdminService struct {
	store   ports.SubscriptionStore
	runtime ports.SubscriptionRuntime
	config  config.Provider
}

func NewSubscriptionAdminService(store ports.SubscriptionStore, runtime ports.SubscriptionRuntime, providers ...config.Provider) *SubscriptionAdminService {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &SubscriptionAdminService{store: store, runtime: runtime, config: provider}
}

func (s *SubscriptionAdminService) List() ([]domain.SubscriptionWithStats, error) {
	subs, err := s.store.GetSubscriptions()
	if err != nil {
		return nil, err
	}
	if subs == nil {
		return []domain.SubscriptionWithStats{}, nil
	}

	result := make([]domain.SubscriptionWithStats, 0, len(subs))
	for _, sub := range subs {
		active, disabled := s.store.CountBySubscriptionID(sub.ID)
		result = append(result, domain.SubscriptionWithStats{
			Subscription:  sub,
			ActiveCount:   active,
			DisabledCount: disabled,
		})
	}
	return result, nil
}

func (s *SubscriptionAdminService) Status() map[string]interface{} {
	if s.runtime == nil {
		return map[string]interface{}{
			"singbox_running":    false,
			"singbox_nodes":      0,
			"custom_count":       0,
			"disabled_count":     0,
			"subscription_count": 0,
			"refresh_tasks":      []interface{}{},
		}
	}
	return s.runtime.GetStatus()
}

func subscriptionDir() string {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "subscriptions")
}

func saveSubscriptionFile(prefix string, content string) (string, error) {
	subDir := subscriptionDir()
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return "", err
	}
	filePath := filepath.Join(subDir, fmt.Sprintf("%s_%d.yaml", prefix, time.Now().UnixMilli()))
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", err
	}
	return filepath.Abs(filePath)
}

func (s *SubscriptionAdminService) defaultRefreshMin(refreshMin int) int {
	if refreshMin > 0 {
		return refreshMin
	}
	return s.config.Get().CustomRefreshInterval
}

func (s *SubscriptionAdminService) validateSubscription(url, filePath string) error {
	if s.runtime == nil {
		return nil
	}
	_, err := s.runtime.ValidateSubscription(url, filePath)
	return err
}

func (s *SubscriptionAdminService) Contribute(name, url, fileContent string) (int64, error) {
	if name == "" {
		name = "贡献订阅"
	}
	refreshMin := s.defaultRefreshMin(0)
	return s.createSubscription(name, url, fileContent, refreshMin, true)
}

func (s *SubscriptionAdminService) Add(name, url, fileContent string, refreshMin int) (int64, error) {
	if name == "" {
		name = "订阅"
	}
	refreshMin = s.defaultRefreshMin(refreshMin)
	return s.createSubscription(name, url, fileContent, refreshMin, false)
}

func (s *SubscriptionAdminService) createSubscription(name, url, fileContent string, refreshMin int, contributed bool) (int64, error) {
	filePath := ""
	if fileContent != "" {
		var err error
		prefix := "sub"
		if contributed {
			prefix = "contribute"
		}
		filePath, err = saveSubscriptionFile(prefix, fileContent)
		if err != nil {
			return 0, fmt.Errorf("保存文件失败: %w", err)
		}
	}

	if err := s.validateSubscription(url, filePath); err != nil {
		if filePath != "" {
			_ = os.Remove(filePath)
		}
		return 0, fmt.Errorf("订阅验证失败: %w", err)
	}

	var (
		id  int64
		err error
	)
	if url != "" && contributed {
		id, err = s.store.AddContributedSubscription(name, url, refreshMin)
	} else {
		id, err = s.store.AddSubscription(name, url, filePath, "auto", refreshMin)
		if err == nil && contributed {
			err = s.store.MarkSubscriptionContributed(id)
		}
	}
	if err != nil {
		if filePath != "" {
			_ = os.Remove(filePath)
		}
		return 0, err
	}

	if s.runtime != nil {
		go func() { _ = s.runtime.RefreshSubscription(id) }()
	}
	return id, nil
}

func (s *SubscriptionAdminService) Delete(id int64) error {
	deleted, err := s.store.DeleteBySubscriptionID(id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSubscription(id); err != nil {
		return err
	}
	if deleted > 0 && s.runtime != nil {
		go s.runtime.RefreshAll()
	}
	return nil
}

func (s *SubscriptionAdminService) Refresh(id int64) {
	if s.runtime == nil {
		return
	}
	go func() { _ = s.runtime.RefreshSubscription(id) }()
}

func (s *SubscriptionAdminService) RefreshAll() {
	if s.runtime == nil {
		return
	}
	go s.runtime.RefreshAll()
}

func (s *SubscriptionAdminService) Toggle(id int64) error {
	return s.store.ToggleSubscription(id)
}
