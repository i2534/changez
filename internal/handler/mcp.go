package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/changez/changez/internal/storage"
)

// ProcessSnapshot 处理快照请求（共享核心逻辑，被 HandleSnapshot 和 MCP 调用）。
func (h *Handler) ProcessSnapshot(ctx context.Context, req *SnapshotRequest) []SnapshotResult {
	sourceID, ok := h.SourceIDs[req.Source]
	if !ok {
		return []SnapshotResult{{Path: "", Status: "error", Reason: fmt.Sprintf("不支持的 source: %s", req.Source)}}
	}

	if len(req.Files) == 0 {
		return []SnapshotResult{{Path: "", Status: "error", Reason: "files 不能为空"}}
	}

	results := make([]SnapshotResult, len(req.Files))

	for i, sf := range req.Files {
		if sf.Path == "" {
			results[i] = SnapshotResult{Path: sf.Path, Status: "error", Reason: "path 不能为空"}
			continue
		}

		result := h.snapshotSingleFile(ctx, sf.Path, sf.Action, sf.Content, sf.Message, sourceID, req.SessionID, req.Model)
		results[i] = result
	}

	return results
}

// snapshotSingleFile handles snapshot for a single file (shared by ProcessSnapshot).
func (h *Handler) snapshotSingleFile(ctx context.Context, filePath, action, content, message string, sourceID int64, sessionID, model string) SnapshotResult {
	project, err := h.DB.FindProjectByPath(ctx, filePath)
	if err != nil {
		h.Logger.Warn("snapshot: project not found", "path", filePath, "error", err)
		return SnapshotResult{Path: filePath, Status: "error", Reason: err.Error()}
	}

	relPath := filePath
	rootPath := project["rootPath"].(string)
	if len(filePath) > len(rootPath) && filePath[:len(rootPath)] == rootPath {
		relPath = strings.TrimPrefix(filePath[len(rootPath):], "/")
	}

	fileID, err := h.DB.UpsertFile(ctx, project["id"].(int64), relPath)
	if err != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("文件注册失败: %v", err)}
	}

	fileMu := h.getFileLock(fileID)
	fileMu.Lock()
	defer fileMu.Unlock()

	latestVer, err := h.DB.GetLatestVersion(ctx, fileID)
	if err != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("查询最新版本失败: %v", err)}
	}

	action = strings.ToLower(action)
	switch action {
	case "create", "update", "delete":
		if action == "create" && latestVer != nil {
			action = "update"
		}
		if action == "update" && latestVer == nil {
			action = "create"
		}
	case "":
		if latestVer == nil {
			action = "create"
		} else {
			action = "update"
		}
	default:
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("不支持的 action: %s", action)}
	}

	if action == "delete" {
		if latestVer == nil {
			return SnapshotResult{Path: filePath, Status: "error", Reason: "文件无版本记录，无法删除"}
		}
		if latestVer["action"].(string) == "delete" {
			return SnapshotResult{Path: filePath, Status: "unchanged"}
		}

		baseVerID := latestVer["id"].(int64)
		versionID, err := h.DB.CreateVersion(ctx, fileID, "delete", nil, nil, &baseVerID, "delete", sourceID)
		if err != nil {
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("写入删除版本失败: %v", err)}
		}
		if err := h.DB.UpdateLatestVersion(ctx, fileID, versionID); err != nil {
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("更新 latest_version 失败: %v", err)}
		}
		return SnapshotResult{Path: filePath, Status: "captured", VersionID: &versionID}
	}

	if int64(len(content)) > h.Config.Storage.MaxFileSize {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("文件大小 %d 超过限制 %d", len(content), h.Config.Storage.MaxFileSize)}
	}

	contentBytes := []byte(content)
	contentHash := storage.ContentHash(contentBytes)

	if latestVer != nil {
		switch latestVer["storageMode"].(string) {
		case "blob":
			// blob 模式直接比对 blob_hash，相同即视为未变更。
			if prevHash, ok := asStringPtr(latestVer["blobHash"]); ok && prevHash == contentHash {
				return SnapshotResult{Path: filePath, Status: "unchanged"}
			}
		case "delta":
			// delta 模式需要先重建出上一版本的完整内容再比对 hash。
			prevContent, err := h.rebuildContent(ctx, latestVer)
			if err != nil {
				return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("重建上一版本内容失败: %v", err)}
			}
			if storage.ContentHash(prevContent) == contentHash {
				return SnapshotResult{Path: filePath, Status: "unchanged"}
			}
		}
	}

	if latestVer == nil {
		hash, err := h.BlobStore.Store(contentBytes)
		if err != nil {
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("存储 blob 失败: %v", err)}
		}

		tx, txErr := h.DB.BeginTx(ctx)
		if txErr != nil {
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("开启事务失败: %v", txErr)}
		}

		versionID, txErr := tx.CreateVersion(ctx, fileID, "blob", &hash, nil, nil, action, sourceID)
		if txErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Error("rollback failed", "error", rbErr)
			}
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("写入版本记录失败: %v", txErr)}
		}

		if txErr := tx.UpdateLatestVersion(ctx, fileID, versionID); txErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Error("rollback failed", "error", rbErr)
			}
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("更新 latest_version 失败: %v", txErr)}
		}

		if txErr := tx.Commit(); txErr != nil {
			tx.Rollback()
			return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("提交事务失败: %v", txErr)}
		}

		return SnapshotResult{Path: filePath, Status: "captured", VersionID: &versionID}
	}

	prevContent, err := h.rebuildContent(ctx, latestVer)
	if err != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("重建上一版本内容失败: %v", err)}
	}

	diffs := storage.ComputeDiffs(string(prevContent), content)

	var meta *storage.DeltaMeta
	if sessionID != "" || model != "" || message != "" {
		meta = &storage.DeltaMeta{
			SessionID: sessionID,
			Model:     model,
			Message:   message,
		}
	}

	baseID := latestVer["id"].(int64)

	tx, txErr := h.DB.BeginTx(ctx)
	if txErr != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("开启事务失败: %v", txErr)}
	}

	versionID, txErr := tx.CreateVersion(ctx, fileID, "delta", nil, nil, &baseID, action, sourceID)
	if txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("写入版本记录失败: %v", txErr)}
	}

	threshold := h.Config.Compact.DeltaCompressThreshold
	offset, _, txErr := h.DeltaStore.Append(fileID, versionID, diffs, meta, threshold)
	if txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("写入 delta 失败: %v", txErr)}
	}

	if txErr := tx.UpdateVersionStorage(ctx, versionID, "delta", nil, &offset); txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("更新 delta_offset 失败: %v", txErr)}
	}

	if txErr := tx.UpdateLatestVersion(ctx, fileID, versionID); txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("更新 latest_version 失败: %v", txErr)}
	}

	if txErr := tx.Commit(); txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("提交事务失败: %v", txErr)}
	}

	h.tryCompact(fileID)

	return SnapshotResult{Path: filePath, Status: "captured", VersionID: &versionID}
}

// ProcessLog 查询版本历史（调用共享核心方法 doLog）。
func (h *Handler) ProcessLog(ctx context.Context, path, since, until, source, action string, limit, offset int) (map[string]any, error) {
	var sourceID *int64
	if source != "" {
		sid, err := h.DB.GetSourceIDByName(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("unknown source: %s", source)
		}
		sourceID = &sid
	}

	var actionFilter *string
	if action != "" {
		actionFilter = &action
	}

	var sinceP, untilP *string
	if since != "" {
		sinceP = &since
	}
	if until != "" {
		untilP = &until
	}

	result, total, err := h.doLog(ctx, path, sourceID, actionFilter, sinceP, untilP, limit, offset)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"file":          path,
		"project":       result.Project,
		"totalVersions": total,
		"versions":      result.Entries,
	}, nil
}

// ProcessRestore 恢复文件内容（调用共享核心方法 doRestore）。
func (h *Handler) ProcessRestore(ctx context.Context, path string, version int64) (map[string]any, error) {
	content, timestamp, err := h.doRestore(ctx, path, version)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"path":      path,
		"version":   version,
		"timestamp": timestamp,
		"content":   string(content),
	}, nil
}

// ProcessDiff 对比两个版本差异（调用共享核心方法 doDiff）。
func (h *Handler) ProcessDiff(ctx context.Context, path string, versionA, versionB int64) (map[string]any, error) {
	diff, err := h.doDiff(ctx, path, versionA, versionB)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"path": path,
		"from": versionA,
		"to":   versionB,
		"diff": diff,
	}, nil
}
