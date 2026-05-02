package fetcher

import (
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	"goproxy/internal/domain"
)

// SourceManager 负责维护抓取源的断路器和运行状态。
type SourceManager struct {
	db *sql.DB
	mu sync.RWMutex
}

func NewSourceManager(db *sql.DB) *SourceManager {
	return &SourceManager{db: db}
}

// CanUseSource 判断某个源当前是否允许使用。
func (sm *SourceManager) CanUseSource(url string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var status string
	var disabledUntil sql.NullTime
	err := sm.db.QueryRow(
		`SELECT status, disabled_until FROM source_status WHERE url = ?`,
		url,
	).Scan(&status, &disabledUntil)

	// 源不存在时视为可用。
	if err != nil {
		return true
	}

	if status == "disabled" && disabledUntil.Valid {
		if time.Now().Before(disabledUntil.Time) {
			return false
		}
		// 冷却期已过，恢复为可用状态。
		sm.db.Exec(`UPDATE source_status SET status = 'active', consecutive_fails = 0 WHERE url = ?`, url)
		return true
	}

	return status != "disabled"
}

// RecordSuccess 记录一次抓取成功。
func (sm *SourceManager) RecordSuccess(url string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.db.Exec(`
		INSERT INTO source_status (url, success_count, consecutive_fails, last_success, status)
		VALUES (?, 1, 0, CURRENT_TIMESTAMP, 'active')
		ON CONFLICT(url) DO UPDATE SET
			success_count = success_count + 1,
			consecutive_fails = 0,
			last_success = CURRENT_TIMESTAMP,
			status = 'active'
	`, url)
}

// RecordFail 记录一次抓取失败，并按阈值降级或禁用源。
func (sm *SourceManager) RecordFail(url string, failThreshold, disableThreshold, cooldownMinutes int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.db.Exec(`
		INSERT INTO source_status (url, fail_count, consecutive_fails, last_fail)
		VALUES (?, 1, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(url) DO UPDATE SET
			fail_count = fail_count + 1,
			consecutive_fails = consecutive_fails + 1,
			last_fail = CURRENT_TIMESTAMP
	`, url)

	var consecutiveFails int
	sm.db.QueryRow(`SELECT consecutive_fails FROM source_status WHERE url = ?`, url).Scan(&consecutiveFails)

	if consecutiveFails >= disableThreshold {
		disabledUntil := time.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
		sm.db.Exec(
			`UPDATE source_status SET status = 'disabled', disabled_until = ? WHERE url = ?`,
			disabledUntil, url,
		)
		log.Printf("[source] 禁用源（连续失败 %d 次）: %s，冷却 %d 分钟", consecutiveFails, url, cooldownMinutes)
		return
	}

	if consecutiveFails >= failThreshold {
		sm.db.Exec(`UPDATE source_status SET status = 'degraded' WHERE url = ?`, url)
		log.Printf("[source] 降级源（连续失败 %d 次）: %s", consecutiveFails, url)
	}
}

// GetSourceStats 将配置源和运行时 source_status 聚合成可展示状态。
func (sm *SourceManager) GetSourceStats(catalog []domain.FetchSourceConfig, disabledURLs []string) ([]domain.SourceRuntimeStatus, error) {
	disabled := make(map[string]struct{}, len(disabledURLs))
	for _, url := range disabledURLs {
		url = strings.TrimSpace(url)
		if url != "" {
			disabled[url] = struct{}{}
		}
	}

	rows, err := sm.db.Query(`
		SELECT url, success_count, fail_count, consecutive_fails,
		       last_success, last_fail, status, disabled_until
		FROM source_status
		ORDER BY success_count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type runtimeRow struct {
		successCount     int
		failCount        int
		consecutiveFails int
		lastSuccess      sql.NullTime
		lastFail         sql.NullTime
		status           string
		disabledUntil    sql.NullTime
	}

	runtime := make(map[string]runtimeRow)
	for rows.Next() {
		var url string
		var row runtimeRow
		if err := rows.Scan(&url, &row.successCount, &row.failCount, &row.consecutiveFails, &row.lastSuccess, &row.lastFail, &row.status, &row.disabledUntil); err != nil {
			return nil, err
		}
		runtime[url] = row
	}

	stats := make([]domain.SourceRuntimeStatus, 0, len(catalog))
	for i, src := range catalog {
		row, ok := runtime[src.URL]
		status := "idle"
		if ok && row.status != "" {
			status = row.status
		}

		attempts := row.successCount + row.failCount
		successRate := 0.0
		healthScore := 50
		if attempts > 0 {
			successRate = float64(row.successCount) / float64(attempts) * 100
			healthScore = int(successRate + 0.5)
		}
		healthScore -= row.consecutiveFails * 10
		if status == "disabled" {
			healthScore -= 20
		} else if status == "degraded" {
			healthScore -= 10
		}
		if healthScore < 0 {
			healthScore = 0
		}
		if healthScore > 100 {
			healthScore = 100
		}

		stat := domain.SourceRuntimeStatus{
			URL:              src.URL,
			Protocol:         src.Protocol,
			Group:            src.Group,
			Status:           status,
			Enabled:          true,
			BuiltIn:          i < len(builtinSourceCatalog()),
			SuccessCount:     row.successCount,
			FailCount:        row.failCount,
			ConsecutiveFails: row.consecutiveFails,
			SuccessRate:      successRate,
			HealthScore:      healthScore,
		}
		if _, disabledByConfig := disabled[src.URL]; disabledByConfig {
			stat.Enabled = false
		}
		if row.lastSuccess.Valid {
			stat.LastSuccess = row.lastSuccess.Time
		}
		if row.lastFail.Valid {
			stat.LastFail = row.lastFail.Time
		}
		if row.disabledUntil.Valid {
			stat.DisabledUntil = row.disabledUntil.Time
		}
		stats = append(stats, stat)
	}

	return stats, nil
}
