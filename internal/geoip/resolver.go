package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/time/rate"
	"goproxy/internal/domain"
)

// Resolver resolves a proxy client's exit IP, location and optional risk profile.
type Resolver struct {
	limiter *rate.Limiter
}

// NewResolver creates a resolver with an optional global rate limit.
func NewResolver(rps int) *Resolver {
	var limiter *rate.Limiter
	if rps > 0 {
		limiter = rate.NewLimiter(rate.Limit(rps), rps*2)
	}
	return &Resolver{limiter: limiter}
}

// Resolve implements ports.GeoIPResolver.
func (r *Resolver) Resolve(ctx context.Context, client *http.Client) (string, string, domain.IPInfo) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return "", "", domain.IPInfo{}
	}

	if r != nil && r.limiter != nil {
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := r.limiter.Wait(waitCtx); err != nil {
			return "", "", domain.IPInfo{}
		}
	}

	if info := r.tryIPPure(ctx, client); info.IPInfoAvailable && info.IP != "" {
		return info.IP, formatIPPureLocation(info), info
	}

	if ip, loc := r.tryIPAPI(ctx, client); ip != "" {
		return ip, loc, domain.IPInfo{}
	}

	if ip, loc := r.tryIPAPICo(ctx, client); ip != "" {
		return ip, loc, domain.IPInfo{}
	}

	if ip, loc := r.tryIPInfo(ctx, client); ip != "" {
		return ip, loc, domain.IPInfo{}
	}

	if ip := r.tryHTTPBinIP(ctx, client); ip != "" {
		return ip, "UNKNOWN", domain.IPInfo{}
	}

	return "", "", domain.IPInfo{}
}

func (r *Resolver) tryIPPure(ctx context.Context, client *http.Client) domain.IPInfo {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://my.ippure.com/v1/info", nil)
	if err != nil {
		return domain.IPInfo{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return domain.IPInfo{}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.IPInfo{}
	}

	var result struct {
		IP             string `json:"ip"`
		ASN            int    `json:"asn"`
		ASOrganization string `json:"asOrganization"`
		Country        string `json:"country"`
		CountryCode    string `json:"countryCode"`
		Region         string `json:"region"`
		RegionCode     string `json:"regionCode"`
		City           string `json:"city"`
		Timezone       string `json:"timezone"`
		FraudScore     int    `json:"fraudScore"`
		IsResidential  bool   `json:"isResidential"`
		IsBroadcast    bool   `json:"isBroadcast"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.IP == "" {
		return domain.IPInfo{}
	}

	return domain.IPInfo{
		IPInfoAvailable: true,
		IP:              result.IP,
		ASN:             result.ASN,
		ASOrganization:  result.ASOrganization,
		Country:         result.Country,
		CountryCode:     result.CountryCode,
		Region:          result.Region,
		RegionCode:      result.RegionCode,
		City:            result.City,
		Timezone:        result.Timezone,
		FraudScore:      result.FraudScore,
		IsResidential:   result.IsResidential,
		IsBroadcast:     result.IsBroadcast,
	}
}

func formatIPPureLocation(info domain.IPInfo) string {
	location := info.CountryCode
	if location == "" {
		location = info.Country
	}
	if info.City != "" && location != "" {
		return fmt.Sprintf("%s %s", location, info.City)
	}
	if location != "" {
		return location
	}
	return "UNKNOWN"
}

func (r *Resolver) tryIPAPI(ctx context.Context, client *http.Client) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/json/?fields=status,country,countryCode,city,query", nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	var result struct {
		Status      string `json:"status"`
		Query       string `json:"query"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Status != "success" {
		return "", ""
	}

	location := result.CountryCode
	if result.City != "" {
		location = fmt.Sprintf("%s %s", result.CountryCode, result.City)
	}

	return result.Query, location
}

func (r *Resolver) tryIPAPICo(ctx context.Context, client *http.Client) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipapi.co/json/", nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	var result struct {
		IP          string `json:"ip"`
		City        string `json:"city"`
		CountryCode string `json:"country_code"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", ""
	}

	location := result.CountryCode
	if result.City != "" {
		location = fmt.Sprintf("%s %s", result.CountryCode, result.City)
	}

	return result.IP, location
}

func (r *Resolver) tryIPInfo(ctx context.Context, client *http.Client) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	var result struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Country string `json:"country"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", ""
	}

	location := result.Country
	if result.City != "" {
		location = fmt.Sprintf("%s %s", result.Country, result.City)
	}

	return result.IP, location
}

func (r *Resolver) tryHTTPBinIP(ctx context.Context, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://httpbin.org/ip", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Origin string `json:"origin"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	return result.Origin
}
