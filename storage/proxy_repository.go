package storage

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"
)

// AddProxy 新增免费代理，已存在则忽略
func (s *Storage) AddProxy(address, protocol string) error {
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO proxies (address, protocol, source) VALUES (?, ?, 'free')`,
		address, protocol,
	)
	if err != nil {
		log.Printf("[storage] AddProxy %s error: %v", address, err)
		return err
	}

	// 检查是否真的插入了
	affected, _ := result.RowsAffected()
	if affected == 0 {
		log.Printf("[storage] AddProxy %s ignored (already exists or constraint)", address)
	}
	return nil
}

// AddProxies 批量新增
func (s *Storage) AddProxies(proxies []Proxy) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO proxies (address, protocol) VALUES (?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, p := range proxies {
		if _, err := stmt.Exec(p.Address, p.Protocol); err != nil {
			log.Printf("insert proxy %s error: %v", p.Address, err)
		}
	}
	return tx.Commit()
}

// GetRandom 随机取一个可用代理（优先选择质量高的）
func (s *Storage) GetRandom() (*Proxy, error) {
	rows, err := s.db.Query(
		`SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status = 'active' AND fail_count < 3
		 ORDER BY
		   CASE quality_grade
		     WHEN 'S' THEN 1
		     WHEN 'A' THEN 2
		     WHEN 'B' THEN 3
		     ELSE 4
		   END,
		   RANDOM()
		 LIMIT 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		return scanProxy(rows)
	}
	return nil, fmt.Errorf("no available proxy")
}

// proxyColumns 代理表查询的标准列列表
const proxyColumns = `id, address, protocol, exit_ip, exit_location,
	ip_info_available, ip, asn, as_organization, country, country_code, region, region_code,
	city, timezone, fraud_score, is_residential, is_broadcast,
	latency, quality_grade, use_count, success_count, fail_count, last_used, last_check,
	created_at, status, source, subscription_id`

// scanProxy 扫描代理行数据
func scanProxy(rows *sql.Rows) (*Proxy, error) {
	p := &Proxy{}
	var lastUsed, lastCheck sql.NullTime
	var source sql.NullString
	var subID sql.NullInt64
	var ipInfoAvailable, isResidential, isBroadcast int
	if err := rows.Scan(&p.ID, &p.Address, &p.Protocol, &p.ExitIP, &p.ExitLocation,
		&ipInfoAvailable, &p.IP, &p.ASN, &p.ASOrganization, &p.Country, &p.CountryCode,
		&p.Region, &p.RegionCode, &p.City, &p.Timezone, &p.FraudScore, &isResidential,
		&isBroadcast, &p.Latency, &p.QualityGrade, &p.UseCount, &p.SuccessCount,
		&p.FailCount, &lastUsed, &lastCheck, &p.CreatedAt, &p.Status, &source, &subID); err != nil {
		return nil, err
	}
	p.IPInfoAvailable = ipInfoAvailable == 1
	p.IsResidential = isResidential == 1
	p.IsBroadcast = isBroadcast == 1
	if lastUsed.Valid {
		p.LastUsed = lastUsed.Time
	}
	if lastCheck.Valid {
		p.LastCheck = lastCheck.Time
	}
	if source.Valid {
		p.Source = source.String
	} else {
		p.Source = "free"
	}
	if subID.Valid {
		p.SubscriptionID = subID.Int64
	}
	return p, nil
}

// GetAll 获取所有可用代理
func (s *Storage) GetAll() ([]Proxy, error) {
	return s.GetAllFiltered("")
}

// GetAllFiltered 获取可用代理（可按来源过滤）
// sourceFilter: "" = 全部, "free" = 仅免费, "custom" = 仅订阅
func (s *Storage) GetAllFiltered(sourceFilter string) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3`
	var args []interface{}
	if sourceFilter != "" {
		query += ` AND source = ?`
		args = append(args, sourceFilter)
	}
	query += ` ORDER BY latency ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// GetRandomExclude 排除指定地址随机取一个
func (s *Storage) GetRandomExclude(excludes []string) (*Proxy, error) {
	return s.GetRandomExcludeFiltered(excludes, "")
}

// GetRandomExcludeFiltered 排除指定地址随机取一个（可按来源过滤）
func (s *Storage) GetRandomExcludeFiltered(excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	var available []Proxy
	for _, p := range proxies {
		if !excludeMap[p.Address] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		if sourceFilter != "" {
			return nil, fmt.Errorf("no available %s proxy", sourceFilter)
		}
		return nil, fmt.Errorf("no available proxy")
	}

	p := available[rand.Intn(len(available))]
	return &p, nil
}

// GetLowestLatencyExclude 排除指定地址后获取延迟最低的代理
func (s *Storage) GetLowestLatencyExclude(excludes []string) (*Proxy, error) {
	return s.GetLowestLatencyExcludeFiltered(excludes, "")
}

// GetLowestLatencyExcludeFiltered 排除指定地址后获取延迟最低的代理（可按来源过滤）
func (s *Storage) GetLowestLatencyExcludeFiltered(excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	for _, p := range proxies {
		if !excludeMap[p.Address] {
			proxy := p
			return &proxy, nil
		}
	}

	return nil, fmt.Errorf("no available proxy")
}

// GetRandomByProtocolExclude 按协议获取随机代理（排除已尝试的）
func (s *Storage) GetRandomByProtocolExclude(protocol string, excludes []string) (*Proxy, error) {
	return s.GetRandomByProtocolExcludeFiltered(protocol, excludes, "")
}

// GetRandomByProtocolExcludeFiltered 按协议获取随机代理（可按来源过滤）
func (s *Storage) GetRandomByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	var available []Proxy
	for _, p := range proxies {
		if p.Protocol == protocol && !excludeMap[p.Address] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no %s proxy available", protocol)
	}

	proxy := available[time.Now().UnixNano()%int64(len(available))]
	return &proxy, nil
}

// GetLowestLatencyByProtocolExclude 按协议获取最低延迟代理（排除已尝试的）
func (s *Storage) GetLowestLatencyByProtocolExclude(protocol string, excludes []string) (*Proxy, error) {
	return s.GetLowestLatencyByProtocolExcludeFiltered(protocol, excludes, "")
}

// GetLowestLatencyByProtocolExcludeFiltered 按协议获取最低延迟代理（可按来源过滤）
func (s *Storage) GetLowestLatencyByProtocolExcludeFiltered(protocol string, excludes []string, sourceFilter string) (*Proxy, error) {
	proxies, err := s.GetAllFiltered(sourceFilter)
	if err != nil {
		return nil, err
	}

	excludeMap := make(map[string]bool)
	for _, e := range excludes {
		excludeMap[e] = true
	}

	for _, p := range proxies {
		if p.Protocol == protocol && !excludeMap[p.Address] {
			proxy := p
			return &proxy, nil
		}
	}

	return nil, fmt.Errorf("no %s proxy available", protocol)
}

// Delete 立即删除指定代理
func (s *Storage) Delete(address string) error {
	_, err := s.db.Exec(`DELETE FROM proxies WHERE address = ?`, address)
	return err
}

// IncrFail 增加失败次数
func (s *Storage) IncrFail(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// ResetFail 重置失败次数（验证通过）
func (s *Storage) ResetFail(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = 0, last_check = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// UpdateLatency 更新代理的延迟信息（毫秒）
func (s *Storage) UpdateLatency(address string, latencyMs int) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET latency = ? WHERE address = ?`,
		latencyMs, address,
	)
	return err
}

// UpdateExitInfo 更新代理的出口 IP、位置和质量等级
func (s *Storage) UpdateExitInfo(address, exitIP, exitLocation string, latencyMs int, ipInfos ...IPInfo) error {
	grade := CalculateQualityGrade(latencyMs)
	if len(ipInfos) > 0 && ipInfos[0].IPInfoAvailable {
		info := ipInfos[0]
		if info.IP == "" {
			info.IP = exitIP
		}
		_, err := s.db.Exec(
			`UPDATE proxies SET
			 exit_ip = ?, exit_location = ?, ip_info_available = 1, ip = ?, asn = ?, as_organization = ?,
			 country = ?, country_code = ?, region = ?, region_code = ?, city = ?, timezone = ?,
			 fraud_score = ?, is_residential = ?, is_broadcast = ?, latency = ?, quality_grade = ?
			 WHERE address = ?`,
			exitIP, exitLocation, info.IP, info.ASN, info.ASOrganization, info.Country, info.CountryCode,
			info.Region, info.RegionCode, info.City, info.Timezone, info.FraudScore,
			boolToInt(info.IsResidential), boolToInt(info.IsBroadcast), latencyMs, grade, address,
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE proxies SET
		 exit_ip = ?, exit_location = ?, ip_info_available = 0, ip = '', asn = 0, as_organization = '',
		 country = '', country_code = '', region = '', region_code = '', city = '', timezone = '',
		 fraud_score = 0, is_residential = 0, is_broadcast = 0, latency = ?, quality_grade = ?
		 WHERE address = ?`,
		exitIP, exitLocation, latencyMs, grade, address,
	)
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// RecordProxyUse 记录代理使用（成功）
func (s *Storage) RecordProxyUse(address string, success bool) error {
	if success {
		_, err := s.db.Exec(
			`UPDATE proxies SET use_count = use_count + 1, success_count = success_count + 1,
			 last_used = CURRENT_TIMESTAMP WHERE address = ?`,
			address,
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE proxies SET use_count = use_count + 1, fail_count = fail_count + 1,
		 last_used = CURRENT_TIMESTAMP WHERE address = ?`,
		address,
	)
	return err
}

// GetWorstProxies 获取指定协议中延迟最高的N个代理（仅免费代理）
func (s *Storage) GetWorstProxies(protocol string, limit int) ([]Proxy, error) {
	rows, err := s.db.Query(
		`SELECT `+proxyColumns+`
		 FROM proxies
		 WHERE protocol = ? AND status = 'active' AND source = 'free'
		   AND quality_grade != 'S'
		   AND (JULIANDAY('now') - JULIANDAY(created_at)) * 1440 > 60
		 ORDER BY latency DESC, fail_count DESC
		 LIMIT ?`, protocol, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// ReplaceProxy 替换代理（删除旧的，添加新的）
func (s *Storage) ReplaceProxy(oldAddress string, newProxy Proxy) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除旧代理
	_, err = tx.Exec(`DELETE FROM proxies WHERE address = ?`, oldAddress)
	if err != nil {
		return err
	}

	// 添加新代理（带完整信息）
	grade := CalculateQualityGrade(newProxy.Latency)
	source := newProxy.Source
	if source == "" {
		source = "free"
	}
	_, err = tx.Exec(
		`INSERT INTO proxies (
			address, protocol, exit_ip, exit_location,
			ip_info_available, ip, asn, as_organization, country, country_code, region, region_code,
			city, timezone, fraud_score, is_residential, is_broadcast,
			latency, quality_grade, status, source
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
		newProxy.Address, newProxy.Protocol, newProxy.ExitIP, newProxy.ExitLocation,
		boolToInt(newProxy.IPInfoAvailable), newProxy.IP, newProxy.ASN, newProxy.ASOrganization,
		newProxy.Country, newProxy.CountryCode, newProxy.Region, newProxy.RegionCode,
		newProxy.City, newProxy.Timezone, newProxy.FraudScore, boolToInt(newProxy.IsResidential),
		boolToInt(newProxy.IsBroadcast), newProxy.Latency, grade, source,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// MarkAsReplacementCandidate 标记代理为替换候选
func (s *Storage) MarkAsReplacementCandidate(addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	placeholders := make([]string, len(addresses))
	args := make([]interface{}, len(addresses))
	for i, addr := range addresses {
		placeholders[i] = "?"
		args[i] = addr
	}
	query := fmt.Sprintf(`UPDATE proxies SET status = 'candidate_replace' WHERE address IN (%s)`,
		fmt.Sprintf("%s", placeholders))
	_, err := s.db.Exec(query, args...)
	return err
}

// GetAverageLatency 获取指定协议的平均延迟
func (s *Storage) GetAverageLatency(protocol string) (int, error) {
	var avg sql.NullFloat64
	err := s.db.QueryRow(
		`SELECT AVG(latency) FROM proxies WHERE protocol = ? AND status = 'active' AND latency > 0`,
		protocol,
	).Scan(&avg)
	if err != nil || !avg.Valid {
		return 0, err
	}
	return int(avg.Float64), nil
}

// GetQualityDistribution 获取质量分布统计
func (s *Storage) GetQualityDistribution() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT quality_grade, COUNT(*) as count
		 FROM proxies
		 WHERE status = 'active' AND fail_count < 3
		 GROUP BY quality_grade`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dist := make(map[string]int)
	for rows.Next() {
		var grade string
		var count int
		if err := rows.Scan(&grade, &count); err != nil {
			return nil, err
		}
		dist[grade] = count
	}
	return dist, nil
}

// GetBatchForHealthCheck 获取一批需要健康检查的代理
func (s *Storage) GetBatchForHealthCheck(batchSize int, skipSGrade bool) ([]Proxy, error) {
	query := `SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3`

	if skipSGrade {
		query += ` AND quality_grade != 'S'`
	}

	query += ` ORDER BY
		COALESCE(last_check, '1970-01-01') ASC,
		quality_grade DESC
		LIMIT ?`

	rows, err := s.db.Query(query, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// CalculateQualityGrade 根据延迟计算质量等级
func CalculateQualityGrade(latencyMs int) string {
	switch {
	case latencyMs <= 500:
		return "S" // 超快
	case latencyMs <= 1000:
		return "A" // 良好
	case latencyMs <= 2000:
		return "B" // 可用
	default:
		return "C" // 淘汰候选
	}
}

// DeleteInvalid 删除失败次数超过阈值的代理（仅免费代理）
func (s *Storage) DeleteInvalid(maxFailCount int) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE fail_count >= ? AND source = 'free'`, maxFailCount)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteBlockedCountries 删除指定国家代码出口的代理
func (s *Storage) DeleteBlockedCountries(countryCodes []string) (int64, error) {
	if len(countryCodes) == 0 {
		return 0, nil
	}

	var totalDeleted int64
	for _, code := range countryCodes {
		// exit_location 格式：如 "CN Beijing" 或 "CN"（仅国家代码）
		// 同时匹配 "CODE" 和 "CODE ..." 两种情况（仅删除免费代理）
		res, err := s.db.Exec(`DELETE FROM proxies WHERE source = 'free' AND (exit_location = ? OR exit_location LIKE ?)`, code, code+" %")
		if err != nil {
			return totalDeleted, err
		}
		affected, _ := res.RowsAffected()
		totalDeleted += affected
	}
	return totalDeleted, nil
}

// DeleteNotAllowedCountries 删除不在白名单中的代理
func (s *Storage) DeleteNotAllowedCountries(allowedCodes []string) (int64, error) {
	if len(allowedCodes) == 0 {
		return 0, nil
	}

	// 构建 WHERE 条件：exit_location 不以任何白名单国家代码开头
	// 即：NOT (exit_location = 'US' OR exit_location LIKE 'US %' OR ...)
	conditions := make([]string, 0, len(allowedCodes)*2)
	args := make([]interface{}, 0, len(allowedCodes)*2)
	for _, code := range allowedCodes {
		conditions = append(conditions, "exit_location = ?", "exit_location LIKE ?")
		args = append(args, code, code+" %")
	}

	query := `DELETE FROM proxies WHERE source = 'free' AND exit_location != '' AND NOT (` + strings.Join(conditions, " OR ") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteWithoutExitInfo 删除没有出口信息的代理（仅免费代理）
func (s *Storage) DeleteWithoutExitInfo() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE source = 'free' AND (exit_ip = '' OR exit_location = '')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DisableBlockedCountries 禁用订阅代理中属于被屏蔽国家的（不删除）
func (s *Storage) DisableBlockedCountries(countryCodes []string) (int64, error) {
	if len(countryCodes) == 0 {
		return 0, nil
	}
	var total int64
	for _, code := range countryCodes {
		res, err := s.db.Exec(
			`UPDATE proxies SET status = 'disabled' WHERE source = 'custom' AND status = 'active' AND (exit_location = ? OR exit_location LIKE ?)`,
			code, code+" %",
		)
		if err != nil {
			return total, err
		}
		affected, _ := res.RowsAffected()
		total += affected
	}
	return total, nil
}

// DisableNotAllowedCountries 禁用订阅代理中不在白名单的（不删除）
func (s *Storage) DisableNotAllowedCountries(allowedCodes []string) (int64, error) {
	if len(allowedCodes) == 0 {
		return 0, nil
	}
	conditions := make([]string, 0, len(allowedCodes)*2)
	args := make([]interface{}, 0, len(allowedCodes)*2)
	for _, code := range allowedCodes {
		conditions = append(conditions, "exit_location = ?", "exit_location LIKE ?")
		args = append(args, code, code+" %")
	}
	query := `UPDATE proxies SET status = 'disabled' WHERE source = 'custom' AND status = 'active' AND exit_location != '' AND NOT (` + strings.Join(conditions, " OR ") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Count 返回可用代理数量（仅免费代理，用于 slot 计算）
func (s *Storage) Count() (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE status IN ('active', 'degraded') AND fail_count < 3 AND source = 'free'`,
	).Scan(&count)
	return count, err
}

// CountAll 返回所有可用代理数量（免费+订阅）
func (s *Storage) CountAll() (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE status IN ('active', 'degraded') AND fail_count < 3`,
	).Scan(&count)
	return count, err
}

// CountByProtocol 按协议统计数量（仅免费代理，用于 slot 计算）
func (s *Storage) CountByProtocol(protocol string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE status IN ('active', 'degraded') AND fail_count < 3 AND source = 'free' AND protocol = ?`,
		protocol,
	).Scan(&count)
	return count, err
}

// IncrementFailCount 增加失败次数
func (s *Storage) IncrementFailCount(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET fail_count = fail_count + 1 WHERE address = ?`,
		address,
	)
	return err
}

// GetByProtocol 按协议获取代理列表
func (s *Storage) GetByProtocol(protocol string) ([]Proxy, error) {
	rows, err := s.db.Query(
		`SELECT `+proxyColumns+`
		 FROM proxies
		 WHERE status IN ('active', 'degraded') AND fail_count < 3 AND protocol = ?
		 ORDER BY latency ASC`, protocol,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// ========== 订阅代理相关方法 ==========

// AddProxyWithSource 新增代理并指定来源和订阅ID
func (s *Storage) AddProxyWithSource(address, protocol, source string, subscriptionID ...int64) error {
	subID := int64(0)
	if len(subscriptionID) > 0 {
		subID = subscriptionID[0]
	}
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO proxies (address, protocol, source, subscription_id) VALUES (?, ?, ?, ?)`,
		address, protocol, source, subID,
	)
	if err != nil {
		log.Printf("[storage] AddProxyWithSource %s error: %v", address, err)
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		// 已存在，更新 source 和 subscription_id
		_, err = s.db.Exec(`UPDATE proxies SET source = ?, subscription_id = ? WHERE address = ?`, source, subID, address)
	}
	return err
}

// DeleteBySubscriptionID 删除指定订阅的所有代理
func (s *Storage) DeleteBySubscriptionID(subscriptionID int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE subscription_id = ?`, subscriptionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DisableProxy 禁用代理（软删除，用于订阅代理）
func (s *Storage) DisableProxy(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET status = 'disabled' WHERE address = ?`,
		address,
	)
	return err
}

// EnableProxy 启用代理（从禁用状态恢复）
func (s *Storage) EnableProxy(address string) error {
	_, err := s.db.Exec(
		`UPDATE proxies SET status = 'active', fail_count = 0 WHERE address = ?`,
		address,
	)
	return err
}

// GetDisabledCustomProxies 获取所有被禁用的订阅代理
func (s *Storage) GetDisabledCustomProxies() ([]Proxy, error) {
	rows, err := s.db.Query(
		`SELECT ` + proxyColumns + `
		 FROM proxies
		 WHERE source = 'custom' AND status = 'disabled'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []Proxy
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, *p)
	}
	return proxies, nil
}

// CountBySource 按来源统计可用代理数量
func (s *Storage) CountBySource(source string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM proxies WHERE source = ? AND status IN ('active', 'degraded') AND fail_count < 3`,
		source,
	).Scan(&count)
	return count, err
}

// DeleteBySource 删除指定来源的所有代理
func (s *Storage) DeleteBySource(source string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE source = ?`, source)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteCustomProxiesNotIn 删除不在给定地址列表中的订阅代理
func (s *Storage) DeleteCustomProxiesNotIn(addresses []string) (int64, error) {
	if len(addresses) == 0 {
		return s.DeleteBySource("custom")
	}
	placeholders := make([]string, len(addresses))
	args := make([]interface{}, len(addresses))
	for i, addr := range addresses {
		placeholders[i] = "?"
		args[i] = addr
	}
	query := `DELETE FROM proxies WHERE source = 'custom' AND address NOT IN (` + strings.Join(placeholders, ",") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ========== 订阅 CRUD ==========
