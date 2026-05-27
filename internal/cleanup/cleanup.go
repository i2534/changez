// Package cleanup 实现定时清理软删除数据和孤儿存储文件。
package cleanup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/storage"
)

var deltaFileRe = regexp.MustCompile(`^(\d+)\.delta$`)

// Cleanup 管理定时清理任务。
type Cleanup struct {
	db         *db.DB
	blobStore  *storage.BlobStore
	deltaStore *storage.DeltaStore
	cfg        *config.CleanupCfg
	fileMuMap  *sync.Map
	Logger     *slog.Logger
}

// New 创建 Cleanup 实例。
func New(
	database *db.DB,
	blobStore *storage.BlobStore,
	deltaStore *storage.DeltaStore,
	cfg *config.CleanupCfg,
	logger *slog.Logger,
	fileMuMap *sync.Map,
) *Cleanup {
	return &Cleanup{
		db:         database,
		blobStore:  blobStore,
		deltaStore: deltaStore,
		cfg:        cfg,
		Logger:     logger,
		fileMuMap:  fileMuMap,
	}
}

// Run 启动定时器循环，按配置间隔执行清理。
func (c *Cleanup) Run(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	interval, err := time.ParseDuration(c.cfg.Interval)
	if err != nil {
		c.Logger.Error("cleanup invalid interval", "interval", c.cfg.Interval, "error", err)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.Logger.Info("cleanup timer started", "interval", interval.String())

	for {
		select {
		case <-ctx.Done():
			c.Logger.Info("cleanup timer stopped")
			return
		case <-ticker.C:
			if err := c.CleanupOnce(ctx); err != nil {
				c.Logger.Error("cleanup failed", "error", err)
			}
		}
	}
}

// CleanupOnce 执行一次完整的清理流程。
func (c *Cleanup) CleanupOnce(ctx context.Context) error {
	c.Logger.Info("cleanup started")

	if err := c.cleanupDeletedProjects(ctx); err != nil {
		c.Logger.Error("cleanup deleted projects failed", "error", err)
	}

	if err := c.cleanupDeletedFiles(ctx); err != nil {
		c.Logger.Error("cleanup deleted files failed", "error", err)
	}

	if err := c.cleanupOrphanBlobs(ctx); err != nil {
		c.Logger.Error("cleanup orphan blobs failed", "error", err)
	}

	if err := c.cleanupOrphanDeltas(ctx); err != nil {
		c.Logger.Error("cleanup orphan deltas failed", "error", err)
	}

	c.Logger.Info("cleanup completed")
	return nil
}

// cleanupDeletedProjects 清理已软删除的项目。
func (c *Cleanup) cleanupDeletedProjects(ctx context.Context) error {
	ids, err := c.db.GetDeletedProjectIDs(ctx)
	if err != nil {
		return fmt.Errorf("get deleted project ids: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	c.Logger.Info("cleaning deleted projects", "count", len(ids))

	for _, projectID := range ids {
		fileIDs, err := c.db.GetFilesByProject(ctx, projectID)
		if err != nil {
			c.Logger.Error("get files by project failed", "project_id", projectID, "error", err)
			continue
		}

		for _, fileID := range fileIDs {
			mu := c.getFileLock(fileID)
			mu.Lock()

			if err := c.db.DeleteFiles(ctx, []int64{fileID}); err != nil {
				c.Logger.Error("delete file failed", "file_id", fileID, "error", err)
			}

			c.deleteDeltaFile(fileID)
			mu.Unlock()
		}

		if err := c.db.DeleteProjects(ctx, []int64{projectID}); err != nil {
			c.Logger.Error("delete project failed", "project_id", projectID, "error", err)
		}
	}

	return nil
}

// cleanupDeletedFiles 清理已软删除的文件（排除已属于软删项目的文件，避免重复处理）。
func (c *Cleanup) cleanupDeletedFiles(ctx context.Context) error {
	ids, err := c.db.GetDeletedFileIDs(ctx)
	if err != nil {
		return fmt.Errorf("get deleted file ids: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	c.Logger.Info("cleaning deleted files", "count", len(ids))

	for _, fileID := range ids {
		mu := c.getFileLock(fileID)
		mu.Lock()

		if err := c.db.DeleteFiles(ctx, []int64{fileID}); err != nil {
			c.Logger.Error("delete file failed", "file_id", fileID, "error", err)
		}

		c.deleteDeltaFile(fileID)
		mu.Unlock()
	}

	return nil
}

// cleanupOrphanBlobs 清理未被任何 version 引用的 blob 文件。
func (c *Cleanup) cleanupOrphanBlobs(ctx context.Context) error {
	hashes, err := c.db.GetAllBlobHashes(ctx)
	if err != nil {
		return fmt.Errorf("get all blob hashes: %w", err)
	}
	referenced := make(map[string]bool)
	for _, h := range hashes {
		referenced[h] = true
	}

	removed, err := c.blobStore.RemoveOrphanBlobs(referenced)
	if err != nil {
		return fmt.Errorf("remove orphan blobs: %w", err)
	}
	if removed > 0 {
		c.Logger.Info("removed orphan blobs", "count", removed)
	}
	return nil
}

// cleanupOrphanDeltas 清理无主的 delta 文件。
func (c *Cleanup) cleanupOrphanDeltas(ctx context.Context) error {
	deltasDir := c.deltaStore.Dir()
	entries, err := os.ReadDir(deltasDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read deltas dir: %w", err)
	}

	rows, err := c.db.Query(ctx, "SELECT id FROM files")
	if err != nil {
		return fmt.Errorf("query file ids: %w", err)
	}
	fileIDs := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		fileIDs[id] = true
	}
	rows.Close()

	for _, entry := range entries {
		if deltaFileRe.MatchString(entry.Name()) == false {
			continue
		}
		var fileID int64
		fmt.Sscanf(entry.Name(), "%d.delta", &fileID)
		if !fileIDs[fileID] {
			path := filepath.Join(deltasDir, entry.Name())
			if err := os.Remove(path); err != nil {
				c.Logger.Warn("remove orphan delta failed", "path", path, "error", err)
			} else {
				c.Logger.Info("removed orphan delta", "path", path)
			}
		}
	}

	return nil
}

// deleteDeltaFile 删除指定文件的 delta 文件。
func (c *Cleanup) deleteDeltaFile(fileID int64) {
	deltasDir := c.deltaStore.Dir()
	path := filepath.Join(deltasDir, fmt.Sprintf("%d.delta", fileID))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		c.Logger.Warn("remove delta file failed", "path", path, "error", err)
	}
}

// getFileLock 返回 fileID 对应的排他锁。
func (c *Cleanup) getFileLock(fileID int64) *sync.RWMutex {
	mu, _ := c.fileMuMap.LoadOrStore(fileID, &sync.RWMutex{})
	return mu.(*sync.RWMutex)
}
