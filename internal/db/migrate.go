// Package db 包含数据库迁移逻辑。
// 每次启动自动执行未完成的迁移，迁移状态记录在 schema_migrations 表。
package db

import (
	"fmt"
	"log/slog"
	"strings"
)

type migration struct {
	version int
	name    string
	up      func(*DB) error
}

var allMigrations = []migration{
	{1, "seed sources", seedSources},
	{2, "rename claude-code to claudecode", migrateClaudeCode},
	{3, "add files is_deleted and deleted_at", migrateFilesSoftDelete},
	{4, "add versions session_id, model, message", migrateVersionsMetadata},
	{5, "create ai_summaries table", migrateAISummaries},
	{6, "create ai_sessions table", migrateAISessions},
	{7, "create ai_trends table", migrateAITrends},
	{8, "add retries to ai_sessions and ai_trends", migrateAIRetries},
}

// RunMigrations 创建迁移跟踪表，按顺序执行未完成的迁移。
func (d *DB) RunMigrations() error {
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version   INTEGER PRIMARY KEY,
			name      TEXT NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var currentVersion int
	err := d.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("query current migration: %w", err)
	}

	for _, m := range allMigrations {
		if m.version <= currentVersion {
			continue
		}

		if _, err := d.db.Exec(`
			INSERT INTO schema_migrations (version, name) VALUES (?, ?)`,
			m.version, m.name); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}

		if err := m.up(d); err != nil {
			return fmt.Errorf("migrate %d (%s): %w", m.version, m.name, err)
		}
	}

	return nil
}

func seedSources(d *DB) error {
	sources := []string{"opencode", "claudecode", "cursor", "human"}
	for _, name := range sources {
		if _, err := d.db.Exec("INSERT OR IGNORE INTO sources (name) VALUES (?)", name); err != nil {
			return fmt.Errorf("seed source %q: %w", name, err)
		}
	}
	return nil
}

func migrateClaudeCode(d *DB) error {
	_, err := d.db.Exec(`
		UPDATE versions SET source_id = (
			SELECT id FROM sources WHERE name = 'claudecode'
		) WHERE source_id = (
			SELECT id FROM sources WHERE name = 'claude-code'
		)`)
	if err != nil {
		return fmt.Errorf("update versions: %w", err)
	}

	if _, err := d.db.Exec("DELETE FROM sources WHERE name = 'claude-code'"); err != nil {
		slog.Warn("failed to delete old source 'claude-code'", "error", err)
	}
	return nil
}

func migrateFilesSoftDelete(d *DB) error {
	for _, sql := range []string{
		"ALTER TABLE projects ADD COLUMN is_deleted BOOLEAN DEFAULT 0 NOT NULL",
		"ALTER TABLE projects ADD COLUMN deleted_at TIMESTAMP",
		"ALTER TABLE files ADD COLUMN is_deleted BOOLEAN DEFAULT 0 NOT NULL",
		"ALTER TABLE files ADD COLUMN deleted_at TIMESTAMP",
	} {
		if _, err := d.db.Exec(sql); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate soft delete: %w", err)
			}
		}
	}
	return nil
}

func migrateVersionsMetadata(d *DB) error {
	for _, sql := range []string{
		"ALTER TABLE versions ADD COLUMN session_id TEXT",
		"ALTER TABLE versions ADD COLUMN model TEXT",
		"ALTER TABLE versions ADD COLUMN message TEXT",
	} {
		if _, err := d.db.Exec(sql); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate versions metadata: %w", err)
			}
		}
	}
	return nil
}

func migrateAISummaries(d *DB) error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS ai_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL UNIQUE,
			summary TEXT,
			model TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			retries INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("create ai_summaries: %w", err)
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_summaries_status ON ai_summaries(status)`)
	return err
}

func migrateAISessions(d *DB) error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS ai_sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL,
			project_name TEXT NOT NULL,
			summary      TEXT,
			model        TEXT,
			status       TEXT NOT NULL DEFAULT 'pending',
			retries      INTEGER NOT NULL DEFAULT 0,
			created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create ai_sessions: %w", err)
	}
	if _, err := d.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_sessions_id ON ai_sessions(session_id)`); err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_sessions_status ON ai_sessions(status)`)
	return err
}

func migrateAITrends(d *DB) error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS ai_trends (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL,
			period_start TEXT NOT NULL,
			period_end   TEXT NOT NULL,
			summary      TEXT,
			model        TEXT,
			status       TEXT NOT NULL DEFAULT 'pending',
			retries      INTEGER NOT NULL DEFAULT 0,
			created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create ai_trends: %w", err)
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_trends_project ON ai_trends(project_name)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_trends_status ON ai_trends(status)`)
	return err
}

func migrateAIRetries(d *DB) error {
	for _, sql := range []string{
		"ALTER TABLE ai_sessions ADD COLUMN retries INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE ai_trends ADD COLUMN retries INTEGER NOT NULL DEFAULT 0",
	} {
		if _, err := d.db.Exec(sql); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migrate ai retries: %w", err)
			}
		}
	}
	return nil
}
