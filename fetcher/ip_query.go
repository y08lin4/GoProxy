package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/time/rate"
	"goproxy/storage"
)

// IPQueryLimiter 全局IP查询限流器
var IPQueryLimiter *rate.Limiter

// InitIPQueryLimiter 初始化限流器
func InitIPQueryLimiter(rps int) {
	IPQueryLimiter = rate.NewLimiter(rate.Limit(rps), rps*2)
}

// GetExitIPInfo 通过代理获取出口 IP、地理位置和 IPPure 风控属性（多源降级）
func GetExitIPInfo(client *http.Client) (string, string, storage.IPInfo) {
	// 等待限流令牌
	if IPQueryLimiter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := IPQueryLimiter.Wait(ctx); err != nil {
			return "", "", storage.IPInfo{}
		}
	}

	// 优先级1：IPPure（IP、位置、ASN、欺诈分、住宅/广播属性）
	if info := tryIPPure(client); info.IPInfoAvailable && info.IP != "" {
		return info.IP, formatIPPureLocation(info), info
	}

	// 优先级2：ip-api.com
	if ip, loc := tryIPAPI(client); ip != "" {
		return ip, loc, storage.IPInfo{}
	}

	// 优先级3：ipapi.co
	if ip, loc := tryIPAPICo(client); ip != "" {
		return ip, loc, storage.IPInfo{}
	}

	// 优先级4：ipinfo.io
	if ip, loc := tryIPInfo(client); ip != "" {
		return ip, loc, storage.IPInfo{}
	}

	// 优先级5：仅获取IP
	if ip := tryHTTPBinIP(client); ip != "" {
		return ip, "UNKNOWN", storage.IPInfo{}
	}

	return "", "", storage.IPInfo{}
}

// tryIPPure 尝试 IPPure MyIP Info API
func tryIPPure(client *http.Client) storage.IPInfo {
	resp, err := client.Get("https://my.ippure.com/v1/info")
	if err != nil {
		return storage.IPInfo{}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return storage.IPInfo{}
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
		return storage.IPInfo{}
	}

	return storage.IPInfo{
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

func formatIPPureLocation(info storage.IPInfo) string {
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

// tryIPAPI 尝试 ip-api.com
func tryIPAPI(client *http.Client) (string, string) {
	resp, err := client.Get("http://ip-api.com/json/?fields=status,country,countryCode,city,query")
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

// tryIPAPICo 尝试 ipapi.co
func tryIPAPICo(client *http.Client) (string, string) {
	resp, err := client.Get("https://ipapi.co/json/")
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

// tryIPInfo 尝试 ipinfo.io
func tryIPInfo(client *http.Client) (string, string) {
	resp, err := client.Get("https://ipinfo.io/json")
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

// tryHTTPBinIP 尝试 httpbin（仅获取IP）
func tryHTTPBinIP(client *http.Client) string {
	resp, err := client.Get("https://httpbin.org/ip")
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
