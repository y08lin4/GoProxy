package service

import (
	"errors"
	"testing"

	"goproxy/internal/domain"
)

type fakeProxyAdminStore struct {
	total       int
	httpCount   int
	socks5Count int
	customCount int
	quality     map[string]int
	listAll     []domain.Proxy
	listByProto map[string][]domain.Proxy
	listPageFn  func(protocol, country string, page, pageSize int) ([]domain.Proxy, int, error)
	countriesFn func(protocol string) ([]string, error)
	deleteCalls []string
}

func (f *fakeProxyAdminStore) Count() (int, error) { return f.total, nil }
func (f *fakeProxyAdminStore) CountByProtocol(protocol string) (int, error) {
	if protocol == "http" {
		return f.httpCount, nil
	}
	return f.socks5Count, nil
}
func (f *fakeProxyAdminStore) CountBySource(source string) (int, error) { return f.customCount, nil }
func (f *fakeProxyAdminStore) GetQualityDistribution() (map[string]int, error) {
	return f.quality, nil
}
func (f *fakeProxyAdminStore) ListProxyPage(protocol string, country string, page int, pageSize int) ([]domain.Proxy, int, error) {
	return f.listPageFn(protocol, country, page, pageSize)
}
func (f *fakeProxyAdminStore) ListProxyCountries(protocol string) ([]string, error) {
	return f.countriesFn(protocol)
}
func (f *fakeProxyAdminStore) GetByProtocol(protocol string) ([]domain.Proxy, error) {
	return f.listByProto[protocol], nil
}
func (f *fakeProxyAdminStore) GetAll() ([]domain.Proxy, error) { return f.listAll, nil }
func (f *fakeProxyAdminStore) GetProxyByAddress(address string) (*domain.Proxy, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeProxyAdminStore) Delete(address string) error {
	f.deleteCalls = append(f.deleteCalls, address)
	return nil
}
func (f *fakeProxyAdminStore) DisableProxy(address string) error { return nil }
func (f *fakeProxyAdminStore) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...domain.IPInfo) error {
	return nil
}

func TestProxyAdminServiceStatsAndQualityDistribution(t *testing.T) {
	store := &fakeProxyAdminStore{
		total:       12,
		httpCount:   5,
		socks5Count: 7,
		customCount: 3,
		quality:     map[string]int{"S": 2, "A": 4},
	}
	service := NewProxyAdminService(store, nil)

	stats := service.Stats(":7777")
	if stats["total"] != 12 || stats["http"] != 5 || stats["socks5"] != 7 || stats["custom_count"] != 3 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats["port"] != ":7777" {
		t.Fatalf("unexpected port in stats: %#v", stats)
	}

	dist, err := service.QualityDistribution()
	if err != nil {
		t.Fatalf("QualityDistribution error: %v", err)
	}
	if dist["S"] != 2 || dist["A"] != 4 {
		t.Fatalf("unexpected quality distribution: %#v", dist)
	}
}

func TestProxyAdminServiceListPageClampsAndRefetchesLastPage(t *testing.T) {
	callPages := make([]int, 0, 2)
	store := &fakeProxyAdminStore{
		listPageFn: func(protocol, country string, page, pageSize int) ([]domain.Proxy, int, error) {
			callPages = append(callPages, page)
			if pageSize != maxProxyPageSize {
				t.Fatalf("expected clamped page size %d, got %d", maxProxyPageSize, pageSize)
			}
			switch page {
			case 99:
				return []domain.Proxy{}, 220, nil
			case 2:
				return []domain.Proxy{{Address: "2.2.2.2:80", Protocol: "http"}}, 220, nil
			default:
				t.Fatalf("unexpected page request: %d", page)
				return nil, 0, nil
			}
		},
		countriesFn: func(protocol string) ([]string, error) {
			if protocol != "http" {
				t.Fatalf("unexpected protocol for countries: %s", protocol)
			}
			return []string{"JP", "US"}, nil
		},
	}
	service := NewProxyAdminService(store, nil)

	page, err := service.ListPage("http", "JP", 99, 999)
	if err != nil {
		t.Fatalf("ListPage error: %v", err)
	}
	if len(callPages) != 2 || callPages[0] != 99 || callPages[1] != 2 {
		t.Fatalf("expected refetch from page 99 to 2, got %#v", callPages)
	}
	if page.Page != 2 || page.PageSize != maxProxyPageSize || page.TotalPages != 2 {
		t.Fatalf("unexpected page metadata: %#v", page)
	}
	if !page.HasPrevious || page.HasNext {
		t.Fatalf("unexpected page navigation flags: %#v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Address != "2.2.2.2:80" {
		t.Fatalf("unexpected page items: %#v", page.Items)
	}
	if len(page.Countries) != 2 || page.Countries[0] != "JP" {
		t.Fatalf("unexpected country list: %#v", page.Countries)
	}
}

func TestProxyAdminServiceDeleteDelegatesToStore(t *testing.T) {
	store := &fakeProxyAdminStore{}
	service := NewProxyAdminService(store, nil)
	if err := service.Delete("1.1.1.1:80"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != "1.1.1.1:80" {
		t.Fatalf("unexpected delete calls: %#v", store.deleteCalls)
	}
}
