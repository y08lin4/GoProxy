package service

import (
	"sort"
	"strings"

	"goproxy/config"
	"goproxy/fetcher"
	"goproxy/internal/domain"
)

// SourceAdminService exposes configured source catalog and runtime health stats.
type SourceAdminService struct {
	fetcher *fetcher.Fetcher
	manager *fetcher.SourceManager
	config  config.Provider
}

func NewSourceAdminService(fetch *fetcher.Fetcher, manager *fetcher.SourceManager, providers ...config.Provider) *SourceAdminService {
	provider := config.Provider(config.GlobalProvider{})
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	return &SourceAdminService{fetcher: fetch, manager: manager, config: provider}
}

func (s *SourceAdminService) Stats() ([]domain.SourceRuntimeStatus, error) {
	if s == nil || s.fetcher == nil || s.manager == nil {
		return nil, nil
	}
	cfg := s.config.Get()
	stats, err := s.manager.GetSourceStats(s.fetcher.SourceCatalog(), cfg.DisabledSourceURLs)
	if err != nil {
		return nil, err
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].HealthScore == stats[j].HealthScore {
			return strings.Compare(stats[i].URL, stats[j].URL) < 0
		}
		return stats[i].HealthScore > stats[j].HealthScore
	})
	return stats, nil
}
