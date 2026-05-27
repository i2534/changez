// Package db 管理 SQLite 数据库连接、表创建和预置数据。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB 封装数据库连接和常用操作。
type DB struct {
	db *sql.DB
}

// Tx 封装 SQLite 事务。
type Tx struct {
	tx *sql.Tx
}

// Open 打开（或创建）SQLite 数据库，执行初始化。
// dataDir 指向 data/ 目录，数据库文件为 data/changez.db。
func Open(dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "changez.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 启用外键约束
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	d := &DB{db: db}

	// 创建表
	if err := d.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	// 运行数据库迁移
	if err := d.RunMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// 启动恢复：清理崩溃遗留的 orphan 版本记录
	if err := d.RecoverOrphans(context.Background()); err != nil {
		slog.Warn("recover orphans failed during startup", "error", err)
	}

	return d, nil
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Handle 返回底层的 *sql.DB，供外部直接执行 SQL。
func (d *DB) Handle() *sql.DB { return d.db }

// createTables 使用 CREATE TABLE IF NOT EXISTS 创建四张表及其索引。
func (d *DB) createTables() error {
	// projects 表
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT UNIQUE NOT NULL,
			root_path   TEXT UNIQUE NOT NULL,
			extra       TEXT DEFAULT '{}',
			is_deleted  BOOLEAN DEFAULT 0,
			deleted_at  TIMESTAMP,
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create projects table: %w", err)
	}

	// files 表
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id        INTEGER NOT NULL,
			path              TEXT NOT NULL,
			latest_version_id INTEGER,
			created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_id, path),
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (latest_version_id) REFERENCES versions(id)
		)
	`); err != nil {
		return fmt.Errorf("create files table: %w", err)
	}

	// versions 表
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS versions (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id         INTEGER NOT NULL,
			storage_mode    TEXT NOT NULL,
			blob_hash       TEXT,
			delta_offset    INTEGER,
			base_id         INTEGER,
			action          TEXT NOT NULL DEFAULT 'update',
			source_id       INTEGER NOT NULL DEFAULT 4,
			changed_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
			FOREIGN KEY (base_id) REFERENCES versions(id),
			FOREIGN KEY (source_id) REFERENCES sources(id)
		)
	`); err != nil {
		return fmt.Errorf("create versions table: %w", err)
	}

	// sources 表
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS sources (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT UNIQUE NOT NULL,
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create sources table: %w", err)
	}

	// versions 索引
	if _, err := d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_versions_file_time ON versions(file_id, changed_at DESC)
	`); err != nil {
		return fmt.Errorf("create idx_versions_file_time: %w", err)
	}

	if _, err := d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_versions_source ON versions(source_id)
	`); err != nil {
		return fmt.Errorf("create idx_versions_source: %w", err)
	}

	if _, err := d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_versions_changed_at ON versions(changed_at DESC)
	`); err != nil {
		return fmt.Errorf("create idx_versions_changed_at: %w", err)
	}

	return nil
}

// LoadSourceNameToID 加载 source 名称到 id 的映射。
func (d *DB) LoadSourceNameToID(ctx context.Context) (map[string]int64, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id, name FROM sources")
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		result[name] = id
	}
	return result, rows.Err()
}

// ResolvePathsToProjects 批量将文件路径映射到项目 rootPath。
// 仅执行 2 次查询：一次 files 表 IN 匹配，一次 projects 全表（fallback 前缀匹配）。
// 返回 path → rootPath 的映射，未匹配到的路径不会出现在结果中。
func (d *DB) ResolvePathsToProjects(ctx context.Context, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	// 去重
	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}
	uniquePaths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		uniquePaths = append(uniquePaths, p)
	}

	result := make(map[string]string)

	// files 表精确匹配
	placeholders := make([]string, len(uniquePaths))
	args := make([]any, len(uniquePaths))
	for i, p := range uniquePaths {
		placeholders[i] = "?"
		args[i] = p
	}
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT f.path, p.root_path
		FROM files f
		JOIN projects p ON f.project_id = p.id
		WHERE f.path IN (%s) AND p.is_deleted = 0
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("query files for path resolution: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fPath, rPath string
		if err := rows.Scan(&fPath, &rPath); err == nil {
			result[fPath] = rPath
		}
	}
	rows.Close()

	// 2. fallback：对未匹配到的路径做 root_path 前缀匹配
	unmatched := make([]string, 0, len(uniquePaths))
	for _, p := range uniquePaths {
		if _, ok := result[p]; !ok {
			unmatched = append(unmatched, p)
		}
	}
	if len(unmatched) == 0 {
		return result, nil
	}

	projRows, err := d.db.QueryContext(ctx,
		"SELECT root_path FROM projects WHERE is_deleted = 0 ORDER BY LENGTH(root_path) DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("query projects for path resolution: %w", err)
	}
	defer projRows.Close()

	projects := make([]string, 0, 10)
	for projRows.Next() {
		var rPath string
		if err := projRows.Scan(&rPath); err == nil {
			projects = append(projects, rPath)
		}
	}

	for _, p := range unmatched {
		for _, rPath := range projects {
			if len(p) >= len(rPath) && p[:len(rPath)] == rPath {
				result[p] = rPath
				break
			}
		}
	}

	return result, nil
}

// FindProjectByName 根据项目名称查找项目。
func (d *DB) FindProjectByName(ctx context.Context, name string) (map[string]any, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, name, root_path, extra FROM projects WHERE name = ? AND is_deleted = 0 LIMIT 1
	`, name)
	var id int64
	var rootPath, extra string
	if err := row.Scan(&id, &name, &rootPath, &extra); err == nil {
		return map[string]any{
			"id":       id,
			"name":     name,
			"rootPath": rootPath,
			"extra":    extra,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query project by name: %w", err)
	}
	return nil, fmt.Errorf("project %q not found", name)
}

// FindProjectByPath 查找文件所属项目。
// 先尝试通过 files 表的相对路径精确匹配（handler 场景），
// 如果没找到则回退到 root_path 前缀匹配（snapshot/测试场景）。
func (d *DB) FindProjectByPath(ctx context.Context, path string) (map[string]any, error) {
	// 1. 尝试通过 files 表精确匹配
	row := d.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.root_path, p.extra
		FROM files f
		JOIN projects p ON f.project_id = p.id
		WHERE f.path = ? AND p.is_deleted = 0 AND f.is_deleted = 0
		LIMIT 1
	`, path)
	var id int64
	var name, rootPath, extra string
	if err := row.Scan(&id, &name, &rootPath, &extra); err == nil {
		return map[string]any{
			"id":       id,
			"name":     name,
			"rootPath": rootPath,
			"extra":    extra,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query project by path: %w", err)
	}

	// 2. 回退：root_path 前缀匹配（用于绝对路径）
	rows, err := d.db.QueryContext(ctx,
		"SELECT id, name, root_path, extra FROM projects WHERE is_deleted = 0 ORDER BY LENGTH(root_path) DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	var best map[string]any
	var bestLen int
	for rows.Next() {
		var pid int64
		var pname, pRoot, pExtra string
		if err := rows.Scan(&pid, &pname, &pRoot, &pExtra); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		if len(path) >= len(pRoot) && path[:len(pRoot)] == pRoot {
			if len(pRoot) > bestLen {
				best = map[string]any{
					"id":       pid,
					"name":     pname,
					"rootPath": pRoot,
					"extra":    pExtra,
				}
				bestLen = len(pRoot)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if best == nil {
		return nil, fmt.Errorf("未找到匹配路径 %q 的项目", path)
	}
	return best, nil
}

// GetFileByPath 查询指定项目中指定路径的文件。
func (d *DB) GetFileByPath(ctx context.Context, projectID int64, path string) (map[string]any, error) {
	row := d.db.QueryRowContext(ctx,
		"SELECT id, project_id, path, latest_version_id, created_at FROM files WHERE project_id = ? AND path = ? AND is_deleted = 0",
		projectID, path,
	)
	var id, projectID2 int64
	var filePath, createdAt string
	var latestVersionID *int64
	if err := row.Scan(&id, &projectID2, &filePath, &latestVersionID, &createdAt); err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}
	return map[string]any{
		"id":              id,
		"projectID":       projectID2,
		"path":            filePath,
		"latestVersionID": latestVersionID,
		"createdAt":       createdAt,
	}, nil
}

// UpsertFile 注册或更新文件记录。如果文件已存在则返回已有 ID，否则创建新记录。
// 如果文件已被软删除（is_deleted=1），自动恢复（策略 A）。
func (d *DB) UpsertFile(ctx context.Context, projectID int64, path string) (int64, error) {
	// 先查活跃文件
	var id int64
	err := d.db.QueryRowContext(ctx,
		"SELECT id FROM files WHERE project_id = ? AND path = ? AND is_deleted = 0",
		projectID, path,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	// 检查是否是已删除的文件，自动恢复
	err = d.db.QueryRowContext(ctx,
		"SELECT id FROM files WHERE project_id = ? AND path = ? AND is_deleted = 1",
		projectID, path,
	).Scan(&id)
	if err == nil {
		_, err := d.db.ExecContext(ctx,
			"UPDATE files SET is_deleted = 0, deleted_at = NULL WHERE id = ?", id)
		return id, err
	}

	// 新建文件
	result, err := d.db.ExecContext(ctx,
		"INSERT INTO files (project_id, path) VALUES (?, ?)",
		projectID, path,
	)
	if err != nil {
		return 0, fmt.Errorf("insert file: %w", err)
	}
	return result.LastInsertId()
}

// GetLatestVersion 查询文件的最新版本。如果没有版本则返回 nil。
func (d *DB) GetLatestVersion(ctx context.Context, fileID int64) (map[string]any, error) {
	var versionID *int64
	err := d.db.QueryRowContext(ctx,
		"SELECT latest_version_id FROM files WHERE id = ?", fileID,
	).Scan(&versionID)
	if err != nil {
		return nil, fmt.Errorf("query file: %w", err)
	}
	if versionID == nil {
		return nil, nil
	}
	return d.GetVersion(ctx, *versionID)
}

// GetVersion 按 ID 查询版本记录。
func (d *DB) GetVersion(ctx context.Context, id int64) (map[string]any, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT v.id, v.file_id, v.storage_mode, v.blob_hash, v.delta_offset,
			   v.base_id, v.action, v.source_id, v.changed_at
		FROM versions v WHERE v.id = ?
	`, id)

	var vid, fileID int64
	var storageMode, action, changedAt string
	var blobHash *string
	var deltaOffset *int64
	var baseID *int64
	var sourceID int64

	if err := row.Scan(&vid, &fileID, &storageMode, &blobHash, &deltaOffset,
		&baseID, &action, &sourceID, &changedAt); err != nil {
		return nil, fmt.Errorf("version %d not found: %w", id, err)
	}

	return map[string]any{
		"id":          vid,
		"fileID":      fileID,
		"storageMode": storageMode,
		"blobHash":    blobHash,
		"deltaOffset": deltaOffset,
		"baseID":      baseID,
		"action":      action,
		"sourceID":    sourceID,
		"changedAt":   changedAt,
	}, nil
}

// CreateVersion 创建版本记录。
func (d *DB) CreateVersion(
	ctx context.Context,
	fileID int64,
	storageMode string,
	blobHash *string,
	deltaOffset *int64,
	baseID *int64,
	action string,
	sourceID int64,
) (int64, error) {
	result, err := d.db.ExecContext(ctx, `
		INSERT INTO versions (file_id, storage_mode, blob_hash, delta_offset, base_id, action, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, fileID, storageMode, blobHash, deltaOffset, baseID, action, sourceID)
	if err != nil {
		return 0, fmt.Errorf("create version: %w", err)
	}
	return result.LastInsertId()
}

// UpdateLatestVersion 更新文件的最新版本 ID。
func (d *DB) UpdateLatestVersion(ctx context.Context, fileID, versionID int64) error {
	_, err := d.db.ExecContext(ctx,
		"UPDATE files SET latest_version_id = ? WHERE id = ?",
		versionID, fileID,
	)
	return err
}

// UpdateVersionStorage 更新版本记录的存储信息（blob_hash/delta_offset）。
func (d *DB) UpdateVersionStorage(ctx context.Context, versionID int64, storageMode string, blobHash *string, deltaOffset *int64) error {
	_, err := d.db.ExecContext(ctx,
		"UPDATE versions SET storage_mode = ?, blob_hash = ?, delta_offset = ? WHERE id = ?",
		storageMode, blobHash, deltaOffset, versionID,
	)
	return err
}

// BeginTx 开启一个 SQLite 事务。
func (d *DB) BeginTx(ctx context.Context) (*Tx, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) Commit() error {
	return t.tx.Commit()
}

func (t *Tx) Rollback() error {
	return t.tx.Rollback()
}

// CreateVersion 事务内创建版本记录。
func (t *Tx) CreateVersion(
	ctx context.Context,
	fileID int64,
	storageMode string,
	blobHash *string,
	deltaOffset *int64,
	baseID *int64,
	action string,
	sourceID int64,
) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `
		INSERT INTO versions (file_id, storage_mode, blob_hash, delta_offset, base_id, action, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, fileID, storageMode, blobHash, deltaOffset, baseID, action, sourceID)
	if err != nil {
		return 0, fmt.Errorf("create version: %w", err)
	}
	return result.LastInsertId()
}

// UpdateLatestVersion 事务内更新文件的最新版本 ID。
func (t *Tx) UpdateLatestVersion(ctx context.Context, fileID, versionID int64) error {
	_, err := t.tx.ExecContext(ctx,
		"UPDATE files SET latest_version_id = ? WHERE id = ?",
		versionID, fileID,
	)
	return err
}

// UpdateVersionStorage 事务内更新版本记录的存储信息。
func (t *Tx) UpdateVersionStorage(ctx context.Context, versionID int64, storageMode string, blobHash *string, deltaOffset *int64) error {
	_, err := t.tx.ExecContext(ctx,
		"UPDATE versions SET storage_mode = ?, blob_hash = ?, delta_offset = ? WHERE id = ?",
		storageMode, blobHash, deltaOffset, versionID,
	)
	return err
}

// ListVersions 查询文件的版本列表，支持过滤条件。
func (d *DB) ListVersions(
	ctx context.Context,
	fileID int64,
	sourceID *int64,
	action *string,
	since, until *string,
	limit, offset int,
) ([]map[string]any, error) {
	query := `
		SELECT v.id, v.action, v.storage_mode, v.changed_at, s.name as source_name
		FROM versions v
		JOIN sources s ON v.source_id = s.id
		WHERE v.file_id = ?
	`
	args := []any{fileID}

	if sourceID != nil {
		query += " AND v.source_id = ?"
		args = append(args, *sourceID)
	}
	if action != nil {
		query += " AND v.action = ?"
		args = append(args, *action)
	}
	if since != nil {
		query += " AND v.changed_at >= ?"
		args = append(args, *since)
	}
	if until != nil {
		query += " AND v.changed_at <= ?"
		args = append(args, *until)
	}

	query += " ORDER BY v.changed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var versions []map[string]any
	for rows.Next() {
		var id int64
		var action, storageMode, changedAt, sourceName string
		if err := rows.Scan(&id, &action, &storageMode, &changedAt, &sourceName); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		versions = append(versions, map[string]any{
			"versionId":   id,
			"action":      action,
			"storageMode": storageMode,
			"timestamp":   changedAt,
			"source":      sourceName,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if versions == nil {
		versions = []map[string]any{}
	}
	return versions, nil
}

// CountVersions 统计文件版本总数。
func (d *DB) CountVersions(ctx context.Context, fileID int64) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM versions WHERE file_id = ?", fileID,
	).Scan(&count)
	return count, err
}

// GetSourceIDByName 按名称查询来源 ID。不存在返回 ErrNotFound。
func (d *DB) GetSourceIDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := d.db.QueryRowContext(ctx,
		"SELECT id FROM sources WHERE name = ?", name,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("source %q not found: %w", name, err)
	}
	return id, nil
}

// GetOrCreateSourceID 按名称查询来源 ID，不存在则自动创建。空名称统一为 "unknown"。
func (d *DB) GetOrCreateSourceID(ctx context.Context, name string) (int64, error) {
	if strings.TrimSpace(name) == "" {
		name = "unknown"
	}
	var id int64
	err := d.db.QueryRowContext(ctx, "SELECT id FROM sources WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	result, err := d.db.ExecContext(ctx, "INSERT INTO sources (name) VALUES (?)", name)
	if err != nil {
		return 0, fmt.Errorf("create source %q: %w", name, err)
	}
	return result.LastInsertId()
}

// ListSources 查询所有来源及其版本数量。
func (d *DB) ListSources(ctx context.Context) ([]map[string]any, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT s.name, COUNT(v.id) as version_count
		FROM sources s LEFT JOIN versions v ON s.id = v.source_id
		GROUP BY s.id ORDER BY s.name
	`)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer rows.Close()

	result := make([]map[string]any, 0)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"name":          name,
			"version_count": count,
		})
	}
	return result, nil
}

// ListProjects 查询所有未删除的项目。
func (d *DB) ListProjects(ctx context.Context) ([]map[string]any, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.root_path, p.extra, p.created_at,` +
			` (SELECT COUNT(*) FROM files WHERE project_id = p.id AND is_deleted = 0) AS file_count` +
			` FROM projects p WHERE p.is_deleted = 0 ORDER BY p.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []map[string]any
	for rows.Next() {
		var id int64
		var name, rootPath, extra, createdAt string
		var fileCount int64
		if err := rows.Scan(&id, &name, &rootPath, &extra, &createdAt, &fileCount); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, map[string]any{
			"id":        id,
			"name":      name,
			"rootPath":  rootPath,
			"extra":     extra,
			"createdAt": createdAt,
			"fileCount": fileCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if projects == nil {
		projects = []map[string]any{}
	}
	return projects, nil
}

// CreateProject 创建项目记录。
func (d *DB) CreateProject(ctx context.Context, name, rootPath, extra string) (int64, error) {
	result, err := d.db.ExecContext(ctx,
		"INSERT INTO projects (name, root_path, extra) VALUES (?, ?, ?)",
		name, rootPath, extra,
	)
	if err != nil {
		return 0, fmt.Errorf("create project: %w", err)
	}
	return result.LastInsertId()
}

// SoftDeleteProject 软删除项目。
func (d *DB) SoftDeleteProject(ctx context.Context, id int64) error {
	result, err := d.db.ExecContext(ctx,
		"UPDATE projects SET is_deleted = 1, deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND is_deleted = 0",
		id,
	)
	if err != nil {
		return fmt.Errorf("soft delete project: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("项目 %d 不存在或已删除", id)
	}
	return nil
}

// SoftDeleteFile 软删除文件。
func (d *DB) SoftDeleteFile(ctx context.Context, fileID int64) error {
	result, err := d.db.ExecContext(ctx,
		"UPDATE files SET is_deleted = 1, deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND is_deleted = 0",
		fileID,
	)
	if err != nil {
		return fmt.Errorf("soft delete file: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("文件 %d 不存在或已删除", fileID)
	}
	return nil
}

// GetDeletedProjectIDs 获取已软删除的项目 ID 列表。
func (d *DB) GetDeletedProjectIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id FROM projects WHERE is_deleted = 1")
	if err != nil {
		return nil, fmt.Errorf("query deleted projects: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deleted project: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetDeletedFileIDs 获取已软删除的文件 ID 列表（排除属于软删项目的文件）。
func (d *DB) GetDeletedFileIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT f.id FROM files f
		WHERE f.is_deleted = 1
		AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = f.project_id AND p.is_deleted = 1)
	`)
	if err != nil {
		return nil, fmt.Errorf("query deleted files: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deleted file: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetFilesByProject 获取项目下的所有文件 ID。
func (d *DB) GetFilesByProject(ctx context.Context, projectID int64) ([]int64, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT id FROM files WHERE project_id = ?", projectID)
	if err != nil {
		return nil, fmt.Errorf("query files by project: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetBlobHashesByFileIDs 获取指定文件的所有 blob_hash 引用。
func (d *DB) GetBlobHashesByFileIDs(ctx context.Context, fileIDs []int64) ([]string, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(fileIDs))
	args := make([]any, len(fileIDs))
	for i, id := range fileIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := d.db.QueryContext(ctx,
		"SELECT DISTINCT blob_hash FROM versions WHERE file_id IN ("+strings.Join(placeholders, ",")+") AND storage_mode = 'blob' AND blob_hash IS NOT NULL",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query blob hashes: %w", err)
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("scan blob hash: %w", err)
		}
		hashes = append(hashes, hash)
	}
	return hashes, rows.Err()
}

// DeleteFiles 删除文件记录（CASCADE 自动删除关联的 versions）。
func (d *DB) DeleteFiles(ctx context.Context, fileIDs []int64) error {
	if len(fileIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(fileIDs))
	args := make([]any, len(fileIDs))
	for i, id := range fileIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	_, err := d.db.ExecContext(ctx,
		"DELETE FROM files WHERE id IN ("+strings.Join(placeholders, ",")+")",
		args...,
	)
	return err
}

// GetAllBlobHashes 查询 versions 表所有被引用的 blob hash。
func (d *DB) GetAllBlobHashes(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT blob_hash FROM versions WHERE blob_hash IS NOT NULL AND blob_hash != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			continue
		}
		hashes = append(hashes, h)
	}
	return hashes, nil
}

// DeleteProjects 删除项目记录（不 CASCADE，只删 projects 行）。
func (d *DB) DeleteProjects(ctx context.Context, projectIDs []int64) error {
	if len(projectIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(projectIDs))
	args := make([]any, len(projectIDs))
	for i, id := range projectIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	_, err := d.db.ExecContext(ctx,
		"DELETE FROM projects WHERE id IN ("+strings.Join(placeholders, ",")+")",
		args...,
	)
	return err
}

// Query 执行任意查询，返回 *sql.Rows。调用者负责 Close。
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, query, args...)
}

// RecoverOrphans 清理崩溃遗留的 orphan 版本记录。
// 事务方案保证 delta_offset=0 永远不会正常提交，
// 所以启动时直接删除 storage_mode='delta' 且 delta_offset 异常的记录。
func (d *DB) RecoverOrphans(ctx context.Context) error {
	// 将引用 orphan 的 base_id 重定向到同文件的前一个 blob 版本（而非设为 NULL）
	if _, err := d.db.ExecContext(ctx, `
		UPDATE versions
		SET base_id = (
			SELECT v2.id FROM versions v2
			WHERE v2.file_id = versions.file_id
			AND v2.storage_mode = 'blob'
			AND v2.id < versions.id
			ORDER BY v2.id DESC LIMIT 1
		)
		WHERE base_id IN (
			SELECT id FROM versions
			WHERE storage_mode = 'delta'
			AND (delta_offset IS NULL OR delta_offset = 0)
		)
	`); err != nil {
		return fmt.Errorf("relink orphan base_ids: %w", err)
	}

	// 再解除 files.latest_version_id 对这些 orphan 的引用
	if _, err := d.db.ExecContext(ctx, `
		UPDATE files
		SET latest_version_id = NULL
		WHERE latest_version_id IN (
			SELECT id FROM versions
			WHERE storage_mode = 'delta'
			AND (delta_offset IS NULL OR delta_offset = 0)
		)
	`); err != nil {
		return fmt.Errorf("unlink orphan latest_version_ids: %w", err)
	}

	res, err := d.db.ExecContext(ctx, `
		DELETE FROM versions
		WHERE storage_mode = 'delta'
		AND (delta_offset IS NULL OR delta_offset = 0)
	`)
	if err != nil {
		return fmt.Errorf("delete orphan delta versions: %w", err)
	}
	n, _ := res.RowsAffected()

	// 修复 files 表中指向已删除版本的 latest_version_id
	if n > 0 {
		if _, updateErr := d.db.ExecContext(ctx, `
			UPDATE files SET latest_version_id = NULL
			WHERE latest_version_id IS NOT NULL
			AND latest_version_id NOT IN (SELECT id FROM versions)
		`); updateErr != nil {
			return fmt.Errorf("fix orphan latest_version_id: %w", updateErr)
		}
	}
	return nil
}

// GetReferencedBlobHashes 返回所有被 versions 表引用的 blob hash。
func (d *DB) GetReferencedBlobHashes(ctx context.Context) (map[string]bool, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT DISTINCT blob_hash FROM versions
		WHERE storage_mode = 'blob' AND blob_hash IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("query referenced blob hashes: %w", err)
	}
	defer rows.Close()

	hashes := make(map[string]bool)
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes[h] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hashes, nil
}

// GetStats 返回数据库统计信息。
func (d *DB) GetStats(ctx context.Context) (map[string]any, error) {
	stats := make(map[string]any)

	var projectCount int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE is_deleted = 0").Scan(&projectCount); err != nil {
		return nil, err
	}
	stats["projects"] = projectCount

	var fileCount int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM files WHERE is_deleted = 0").Scan(&fileCount); err != nil {
		return nil, err
	}
	stats["files"] = fileCount

	var versionCount int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM versions").Scan(&versionCount); err != nil {
		return nil, err
	}
	stats["versions"] = versionCount

	rows, err := d.db.QueryContext(ctx, `
		SELECT s.name, COUNT(v.id) as cnt
		FROM sources s LEFT JOIN versions v ON s.id = v.source_id
		GROUP BY s.id ORDER BY cnt DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sourceBreakdown := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		sourceBreakdown[name] = count
	}
	stats["sources"] = sourceBreakdown

	return stats, nil
}
