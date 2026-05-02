package domain

import "time"

// Proxy is the core proxy entity used across fetch, validation, pool and proxy serving.
type Proxy struct {
	ID           int64  `json:"id"`
	Address      string `json:"address"`
	Protocol     string `json:"protocol"`
	ExitIP       string `json:"exit_ip"`
	ExitLocation string `json:"exit_location"`
	IPInfo
	Latency        int       `json:"latency"`
	QualityGrade   string    `json:"quality_grade"`
	UseCount       int       `json:"use_count"`
	SuccessCount   int       `json:"success_count"`
	FailCount      int       `json:"fail_count"`
	LastUsed       time.Time `json:"last_used"`
	LastCheck      time.Time `json:"last_check"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
	Source         string    `json:"source"`
	SubscriptionID int64     `json:"subscription_id"`
}

// IPInfo stores exit IP profile information returned by external IP intelligence APIs.
type IPInfo struct {
	IPInfoAvailable bool   `json:"ip_info_available"`
	IP              string `json:"ip"`
	ASN             int    `json:"asn"`
	ASOrganization  string `json:"as_organization"`
	Country         string `json:"country"`
	CountryCode     string `json:"country_code"`
	Region          string `json:"region"`
	RegionCode      string `json:"region_code"`
	City            string `json:"city"`
	Timezone        string `json:"timezone"`
	FraudScore      int    `json:"fraud_score"`
	IsResidential   bool   `json:"is_residential"`
	IsBroadcast     bool   `json:"is_broadcast"`
}

// Subscription describes a custom proxy subscription source.
type Subscription struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	FilePath    string    `json:"file_path"`
	Format      string    `json:"format"`
	RefreshMin  int       `json:"refresh_min"`
	LastFetch   time.Time `json:"last_fetch"`
	LastSuccess time.Time `json:"last_success"`
	Status      string    `json:"status"`
	ProxyCount  int       `json:"proxy_count"`
	CreatedAt   time.Time `json:"created_at"`
	Contributed bool      `json:"contributed"`
}

// SubscriptionWithStats augments one subscription with current active/disabled proxy counts.
type SubscriptionWithStats struct {
	Subscription
	ActiveCount   int `json:"active_count"`
	DisabledCount int `json:"disabled_count"`
}

// ProxyPage is a paginated proxy-list response for admin/read-only APIs.
type ProxyPage struct {
	Items       []Proxy  `json:"items"`
	Total       int      `json:"total"`
	Page        int      `json:"page"`
	PageSize    int      `json:"page_size"`
	TotalPages  int      `json:"total_pages"`
	Protocol    string   `json:"protocol,omitempty"`
	Country     string   `json:"country,omitempty"`
	Countries   []string `json:"countries,omitempty"`
	HasNext     bool     `json:"has_next"`
	HasPrevious bool     `json:"has_previous"`
}

// RefreshTaskStatus describes the latest state of a subscription refresh workflow.
type RefreshTaskStatus struct {
	Key            string    `json:"key"`
	SubscriptionID int64     `json:"subscription_id,omitempty"`
	Scope          string    `json:"scope"`
	State          string    `json:"state"`
	Message        string    `json:"message,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	NodeCount      int       `json:"node_count,omitempty"`
	ValidCount     int       `json:"valid_count,omitempty"`
}

// FetchSourceConfig describes one configurable upstream proxy-list source.
type FetchSourceConfig struct {
	URL      string `json:"url"`
	Protocol string `json:"protocol"`
	Group    string `json:"group"` // fast / slow
}

// SourceStatus records fetch source health/circuit-breaker state.
type SourceStatus struct {
	ID               int64
	URL              string
	SuccessCount     int
	FailCount        int
	ConsecutiveFails int
	LastSuccess      time.Time
	LastFail         time.Time
	Status           string
	DisabledUntil    time.Time
}

// SourceRuntimeStatus combines configured source metadata and runtime health stats.
type SourceRuntimeStatus struct {
	URL              string    `json:"url"`
	Protocol         string    `json:"protocol"`
	Group            string    `json:"group"`
	Status           string    `json:"status"`
	Enabled          bool      `json:"enabled"`
	BuiltIn          bool      `json:"built_in"`
	SuccessCount     int       `json:"success_count"`
	FailCount        int       `json:"fail_count"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	LastSuccess      time.Time `json:"last_success,omitempty"`
	LastFail         time.Time `json:"last_fail,omitempty"`
	DisabledUntil    time.Time `json:"disabled_until,omitempty"`
	SuccessRate      float64   `json:"success_rate"`
	HealthScore      int       `json:"health_score"`
}

// Source describes an upstream proxy list source.
type Source struct {
	URL      string
	Protocol string
}

// PoolStatus summarizes current proxy-pool capacity and quality.
type PoolStatus struct {
	Total            int
	HTTP             int
	SOCKS5           int
	HTTPSlots        int
	SOCKS5Slots      int
	State            string
	AvgLatencyHTTP   int
	AvgLatencySocks5 int
	CustomCount      int
}

// ValidationResult is the result of validating one proxy candidate.
type ValidationResult struct {
	Proxy        Proxy
	Valid        bool
	Latency      time.Duration
	ExitIP       string
	ExitLocation string
	IPInfo       IPInfo
}
