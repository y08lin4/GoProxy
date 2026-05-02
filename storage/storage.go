package storage

import (
	"database/sql"
	"fmt"
	"log"

	"goproxy/internal/domain"

	_ "modernc.org/sqlite"
)

type Proxy = domain.Proxy
type IPInfo = domain.IPInfo
type Subscription = domain.Subscription
type SourceStatus = domain.SourceStatus

type Storage struct {
	db *sql.DB
}

func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite 单写

	s := &Storage{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) initSchema() error {
	// 创建代理表
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS proxies (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			address        TEXT NOT NULL UNIQUE,
			protocol       TEXT NOT NULL,
			exit_ip        TEXT NOT NULL DEFAULT '',
			exit_location  TEXT NOT NULL DEFAULT '',
			ip_info_available INTEGER NOT NULL DEFAULT 0,
			ip             TEXT NOT NULL DEFAULT '',
			asn            INTEGER NOT NULL DEFAULT 0,
			as_organization TEXT NOT NULL DEFAULT '',
			country        TEXT NOT NULL DEFAULT '',
			country_code   TEXT NOT NULL DEFAULT '',
			region         TEXT NOT NULL DEFAULT '',
			region_code    TEXT NOT NULL DEFAULT '',
			city           TEXT NOT NULL DEFAULT '',
			timezone       TEXT NOT NULL DEFAULT '',
			fraud_score    INTEGER NOT NULL DEFAULT 0,
			is_residential INTEGER NOT NULL DEFAULT 0,
			is_broadcast   INTEGER NOT NULL DEFAULT 0,
			latency        INTEGER NOT NULL DEFAULT 0,
			quality_grade  TEXT NOT NULL DEFAULT 'C',
			use_count      INTEGER NOT NULL DEFAULT 0,
			success_count  INTEGER NOT NULL DEFAULT 0,
			fail_count     INTEGER NOT NULL DEFAULT 0,
			last_used      DATETIME,
			last_check     DATETIME,
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status         TEXT NOT NULL DEFAULT 'active'
		)
	`)
	if err != nil {
		return err
	}

	// 创建索引
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_protocol_latency ON proxies(protocol, latency)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_quality_grade ON proxies(quality_grade, latency)`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_status ON proxies(status)`)

	// 创建源状态表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS source_status (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			url               TEXT NOT NULL UNIQUE,
			success_count     INTEGER NOT NULL DEFAULT 0,
			fail_count        INTEGER NOT NULL DEFAULT 0,
			consecutive_fails INTEGER NOT NULL DEFAULT 0,
			last_success      DATETIME,
			last_fail         DATETIME,
			status            TEXT NOT NULL DEFAULT 'active',
			disabled_until    DATETIME
		)
	`)
	if err != nil {
		return err
	}

	// 迁移：处理旧的 location 字段（如果存在）
	var hasOldLocation int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='location'`).Scan(&hasOldLocation)
	if err == nil && hasOldLocation > 0 {
		log.Println("[storage] migrating: renaming location to exit_location")
		// 如果有旧的 location 字段，先添加新字段再复制数据
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`)
		s.db.Exec(`UPDATE proxies SET exit_location = location WHERE location != ''`)
	}

	// 迁移：添加 exit_ip 字段
	var hasExitIP int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='exit_ip'`).Scan(&hasExitIP)
	if err == nil && hasExitIP == 0 {
		log.Println("[storage] migrating: adding exit_ip column")
		_, err = s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_ip TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("migrate exit_ip column: %w", err)
		}
	}

	// 迁移：添加 exit_location 字段
	var hasExitLocation int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='exit_location'`).Scan(&hasExitLocation)
	if err == nil && hasExitLocation == 0 {
		log.Println("[storage] migrating: adding exit_location column")
		_, err = s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("migrate exit_location column: %w", err)
		}
	}

	// 迁移：添加 latency 字段
	var hasLatency int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='latency'`).Scan(&hasLatency)
	if err == nil && hasLatency == 0 {
		log.Println("[storage] migrating: adding latency column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN latency INTEGER NOT NULL DEFAULT 0`)
	}

	// 迁移：添加质量等级字段
	var hasQuality int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='quality_grade'`).Scan(&hasQuality)
	if hasQuality == 0 {
		log.Println("[storage] migrating: adding quality_grade column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN quality_grade TEXT NOT NULL DEFAULT 'C'`)
	}

	// 迁移：添加使用统计字段
	var hasUseCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='use_count'`).Scan(&hasUseCount)
	if hasUseCount == 0 {
		log.Println("[storage] migrating: adding usage tracking columns")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN last_used DATETIME`)
	}

	// 迁移：添加状态字段
	var hasStatus int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='status'`).Scan(&hasStatus)
	if hasStatus == 0 {
		log.Println("[storage] migrating: adding status column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`)
	}

	// 迁移：添加 source 字段（区分免费代理和订阅代理）
	var hasSource int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='source'`).Scan(&hasSource)
	if hasSource == 0 {
		log.Println("[storage] migrating: adding source column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN source TEXT NOT NULL DEFAULT 'free'`)
	}
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_source ON proxies(source, status)`)

	// 迁移：添加 subscription_id 字段
	var hasSubID int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='subscription_id'`).Scan(&hasSubID)
	if hasSubID == 0 {
		log.Println("[storage] migrating: adding subscription_id column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN subscription_id INTEGER NOT NULL DEFAULT 0`)
	}

	// 迁移：添加 IPPure IP 画像字段
	ipInfoColumns := []struct {
		name string
		def  string
	}{
		{"ip_info_available", "INTEGER NOT NULL DEFAULT 0"},
		{"ip", "TEXT NOT NULL DEFAULT ''"},
		{"asn", "INTEGER NOT NULL DEFAULT 0"},
		{"as_organization", "TEXT NOT NULL DEFAULT ''"},
		{"country", "TEXT NOT NULL DEFAULT ''"},
		{"country_code", "TEXT NOT NULL DEFAULT ''"},
		{"region", "TEXT NOT NULL DEFAULT ''"},
		{"region_code", "TEXT NOT NULL DEFAULT ''"},
		{"city", "TEXT NOT NULL DEFAULT ''"},
		{"timezone", "TEXT NOT NULL DEFAULT ''"},
		{"fraud_score", "INTEGER NOT NULL DEFAULT 0"},
		{"is_residential", "INTEGER NOT NULL DEFAULT 0"},
		{"is_broadcast", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, col := range ipInfoColumns {
		if err := s.ensureProxyColumn(col.name, col.def); err != nil {
			return err
		}
	}

	// 创建订阅表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS subscriptions (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT NOT NULL DEFAULT '',
			url           TEXT NOT NULL DEFAULT '',
			file_path     TEXT NOT NULL DEFAULT '',
			format        TEXT NOT NULL DEFAULT 'clash',
			refresh_min   INTEGER NOT NULL DEFAULT 60,
			last_fetch    DATETIME,
			status        TEXT NOT NULL DEFAULT 'active',
			proxy_count   INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// 迁移：订阅表添加 contributed 和 last_success 字段
	var hasContributed int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='contributed'`).Scan(&hasContributed)
	if hasContributed == 0 {
		s.db.Exec(`ALTER TABLE subscriptions ADD COLUMN contributed INTEGER NOT NULL DEFAULT 0`)
	}
	var hasLastSuccess int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='last_success'`).Scan(&hasLastSuccess)
	if hasLastSuccess == 0 {
		s.db.Exec(`ALTER TABLE subscriptions ADD COLUMN last_success DATETIME`)
	}

	return nil
}

func (s *Storage) ensureProxyColumn(name, definition string) error {
	var exists int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name=?`, name).Scan(&exists)
	if err != nil || exists > 0 {
		return err
	}
	log.Printf("[storage] migrating: adding %s column", name)
	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE proxies ADD COLUMN %s %s`, name, definition)); err != nil {
		return fmt.Errorf("migrate %s column: %w", name, err)
	}
	return nil
}

// Close 关闭数据库
func (s *Storage) Close() error {
	return s.db.Close()
}

// GetDB 获取数据库实例（供其他模块使用）
func (s *Storage) GetDB() *sql.DB {
	return s.db
}
