// Package compact 实现 delta 链压缩整理。
// 双触发：写入时检查 + 定时器扫描。
package compact

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/dbutil"
	"github.com/changez/changez/internal/storage"
)

// 复用 dbutil 中的工具函数，避免在多个包中重复定义。
var (
	asInt64Ptr  = dbutil.AsInt64Ptr
	asStringPtr = dbutil.AsStringPtr
)

// Compactor 管理文件的 compact 操作。
type Compactor struct {
	db         *db.DB
	blobStore  *storage.BlobStore
	deltaStore *storage.DeltaStore
	cfg        *config.CompactCfg
	fileMuMap  *sync.Map
	Logger     *slog.Logger
}

// New 创建 Compactor 实例。
// fileMuMap 需与 handler 共享同一 sync.Map，确保 per-file 锁一致。
func New(
	database *db.DB,
	blobStore *storage.BlobStore,
	deltaStore *storage.DeltaStore,
	cfg *config.CompactCfg,
	logger *slog.Logger,
	fileMuMap *sync.Map,
) *Compactor {
	return &Compactor{
		db:         database,
		blobStore:  blobStore,
		deltaStore: deltaStore,
		cfg:        cfg,
		Logger:     logger,
		fileMuMap:  fileMuMap,
	}
}

// Run 启动定时器循环，按配置间隔扫描所有文件。
// 阻塞直到 ctx 被取消。
func (c *Compactor) Run(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	interval, err := time.ParseDuration(c.cfg.Interval)
	if err != nil {
		c.Logger.Error("compact invalid interval", "interval", c.cfg.Interval, "error", err)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.Logger.Info("compact timer started", "interval", interval.String())

	for {
		select {
		case <-ctx.Done():
			c.Logger.Info("compact timer stopped")
			return
		case <-ticker.C:
			c.scanAndCompact()
		}
	}
}

// CheckAndCompact 获取锁后检查并执行 compact。供定时器使用。
func (c *Compactor) CheckAndCompact(fileID int64) (bool, error) {
	mu := c.getFileLock(fileID)
	mu.Lock()
	defer mu.Unlock()
	return c.checkAndCompactLocked(fileID)
}

// CheckAndCompactLocked 在已持有 fileID 锁的前提下检查并执行 compact。
// 供 handler 在 snapshot 写入后调用（handler 已持锁）。
func (c *Compactor) CheckAndCompactLocked(fileID int64) (bool, error) {
	return c.checkAndCompactLocked(fileID)
}

// checkAndCompactLocked 检查 delta 链长度，超过阈值则执行 compact。
func (c *Compactor) checkAndCompactLocked(fileID int64) (bool, error) {
	if !c.cfg.Enabled {
		return false, nil
	}

	latest, err := c.db.GetLatestVersion(context.Background(), fileID)
	if err != nil {
		return false, fmt.Errorf("get latest version: %w", err)
	}
	if latest == nil {
		return false, nil
	}

	// 最新版是 blob，无需 compact
	if latest["storageMode"].(string) == "blob" {
		return false, nil
	}

	// 统计连续 delta 版本数
	deltaCount := 0
	current := latest
	visited := make(map[int64]bool)
	for deltaCount < 10000 {
		if current["storageMode"].(string) == "delta" {
			vid := current["id"].(int64)
			if visited[vid] {
				return false, fmt.Errorf("cycle detected in delta chain at version %d", vid)
			}
			visited[vid] = true
			deltaCount++
			bid, ok := asInt64Ptr(current["baseID"])
			if !ok {
				break
			}
			next, err := c.db.GetVersion(context.Background(), bid)
			if err != nil {
				return false, fmt.Errorf("walk version %d: %w", bid, err)
			}
			current = next
		} else {
			break
		}
	}
	if deltaCount >= 10000 {
		return false, fmt.Errorf("delta chain depth limit (10000) reached for file %d", fileID)
	}

	if deltaCount < c.cfg.MaxDeltaChain {
		return false, nil
	}

	c.Logger.Info("compact triggered", "file_id", fileID, "delta_chain", deltaCount, "max", c.cfg.MaxDeltaChain)
	if err := c.compactLatest(fileID, latest); err != nil {
		return false, fmt.Errorf("compact file %d: %w", fileID, err)
	}
	return true, nil
}

// compactLatest 将最新版本就地转换为 blob 模式。
func (c *Compactor) compactLatest(fileID int64, latest map[string]any) error {
	content, err := c.rebuildContent(context.Background(), latest)
	if err != nil {
		return fmt.Errorf("rebuild content: %w", err)
	}

	blobHash, err := c.blobStore.Store(content)
	if err != nil {
		return fmt.Errorf("store blob: %w", err)
	}

	versionID := latest["id"].(int64)
	if err := c.db.UpdateVersionStorage(context.Background(), versionID, "blob", &blobHash, nil); err != nil {
		return fmt.Errorf("update version storage: %w", err)
	}

	c.Logger.Info("compact completed", "file_id", fileID, "version_id", versionID, "blob", blobHash)
	return nil
}

// rebuildContent 根据版本记录重建完整文件内容。
func (c *Compactor) rebuildContent(ctx context.Context, ver map[string]any) ([]byte, error) {
	switch ver["storageMode"].(string) {
	case "blob":
		hash, ok := asStringPtr(ver["blobHash"])
		if !ok {
			return nil, fmt.Errorf("blob mode but hash is nil")
		}
		return c.blobStore.Read(hash)
	case "delta":
		return c.rebuildFromDeltaChain(ctx, ver)
	default:
		return nil, fmt.Errorf("unsupported storage mode: %s", ver["storageMode"].(string))
	}
}

// rebuildFromDeltaChain 从指定版本回溯到最近的 blob checkpoint，重建完整内容。
func (c *Compactor) rebuildFromDeltaChain(ctx context.Context, ver map[string]any) ([]byte, error) {
	dmp := diffmatchpatch.New()

	type deltaStep struct {
		diffs []diffmatchpatch.Diff
	}

	var steps []deltaStep
	current := ver
	visited := make(map[int64]bool)

	for depth := 0; depth < 1000; depth++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled while walking delta chain: %w", err)
		}

		switch current["storageMode"].(string) {
		case "blob":
			hash, ok := asStringPtr(current["blobHash"])
			if !ok {
				return nil, fmt.Errorf("blob checkpoint but hash is nil")
			}
			content, err := c.blobStore.Read(hash)
			if err != nil {
				return nil, fmt.Errorf("read blob checkpoint: %w", err)
			}

			for i := len(steps) - 1; i >= 0; i-- {
				content = applyPatch(dmp, content, steps[i].diffs)
			}
			return content, nil

		case "delta":
			vid := current["id"].(int64)
			if visited[vid] {
				return nil, fmt.Errorf("cycle detected in delta chain at version %d", vid)
			}
			visited[vid] = true

			offset, ok := asInt64Ptr(current["deltaOffset"])
			if !ok {
				return nil, fmt.Errorf("delta mode but offset is nil")
			}
			fileID := current["fileID"].(int64)

			_, diffs, _, err := c.deltaStore.ReadEntry(fileID, offset)
			if err != nil {
				return nil, fmt.Errorf("read delta entry: %w", err)
			}
			steps = append(steps, deltaStep{diffs: diffs})

			bid, ok := asInt64Ptr(current["baseID"])
			if !ok {
				return nil, fmt.Errorf("delta mode but base_id is nil")
			}
			next, err := c.db.GetVersion(ctx, bid)
			if err != nil {
				return nil, fmt.Errorf("walk version %d: %w", bid, err)
			}
			current = next

		default:
			return nil, fmt.Errorf("unsupported storage mode: %s", current["storageMode"].(string))
		}
	}

	return nil, fmt.Errorf("delta chain depth limit (1000) reached")
}

// scanAndCompact 扫描所有有版本记录的文件并 compact 链过长的。
func (c *Compactor) scanAndCompact() {
	rows, err := c.db.Query(context.Background(), `
		SELECT f.id FROM files f
		JOIN projects p ON f.project_id = p.id
		WHERE f.latest_version_id IS NOT NULL AND p.is_deleted = 0
	`)
	if err != nil {
		c.Logger.Error("compact scan query failed", "error", err)
		return
	}
	defer rows.Close()

	var fileIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			c.Logger.Error("compact scan row failed", "error", err)
			continue
		}
		fileIDs = append(fileIDs, id)
	}
	if err := rows.Err(); err != nil {
		c.Logger.Error("compact scan iteration failed", "error", err)
		return
	}

	for _, fileID := range fileIDs {
		compacted, err := c.CheckAndCompact(fileID)
		if err != nil {
			c.Logger.Error("compact file failed", "file_id", fileID, "error", err)
		} else if compacted {
			c.Logger.Info("compact scan completed", "file_id", fileID)
		}
	}
}

// applyPatch 应用 go-diff patch。
func applyPatch(dmp *diffmatchpatch.DiffMatchPatch, text []byte, diffs []diffmatchpatch.Diff) []byte {
	patches := dmp.PatchMake(string(text), diffs)
	result, applied := dmp.PatchApply(patches, string(text))
	failed := 0
	for _, ok := range applied {
		if !ok {
			failed++
		}
	}
	if failed > 0 {
		slog.Warn("applyPatch partial failure", "total", len(applied), "failed", failed)
	}
	return []byte(result)
}

// getFileLock 返回 fileID 对应的排他锁。
func (c *Compactor) getFileLock(fileID int64) *sync.RWMutex {
	mu, _ := c.fileMuMap.LoadOrStore(fileID, &sync.RWMutex{})
	return mu.(*sync.RWMutex)
}
