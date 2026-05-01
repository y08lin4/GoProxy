package proxy

import "goproxy/storage"

// FailureReporter 统一记录代理使用结果，并处理不可用代理。
type FailureReporter struct {
	storage *storage.Storage
}

func NewFailureReporter(s *storage.Storage) *FailureReporter {
	return &FailureReporter{storage: s}
}

func (r *FailureReporter) Success(p *storage.Proxy) {
	if r == nil || r.storage == nil || p == nil {
		return
	}
	_ = r.storage.RecordProxyUse(p.Address, true)
}

func (r *FailureReporter) Failure(p *storage.Proxy) {
	if r == nil || r.storage == nil || p == nil {
		return
	}
	_ = r.storage.RecordProxyUse(p.Address, false)
	r.Remove(p)
}

// Remove 按代理来源执行下线策略：订阅代理禁用，免费代理删除。
func (r *FailureReporter) Remove(p *storage.Proxy) {
	if r == nil || r.storage == nil || p == nil {
		return
	}
	if p.Source == "custom" {
		_ = r.storage.DisableProxy(p.Address)
		return
	}
	_ = r.storage.Delete(p.Address)
}
