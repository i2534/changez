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
	sourceID, err := h.DB.GetOrCreateSourceID(ctx, req.Source)
	if err != nil {
		return []SnapshotResult{{Path: "", Status: "error", Reason: fmt.Sprintf("source 处理失败: %v", err)}}
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
		versionID, err := h.DB.CreateVersion(ctx, fileID, "delete", nil, nil, &baseVerID, "delete", sourceID, "", "", "")
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
		case "delete":
			// 文件此前被软删除，视同无上一版本，走新建 blob 路径。
			latestVer = nil
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

		versionID, txErr := tx.CreateVersion(ctx, fileID, "blob", &hash, nil, nil, action, sourceID, sessionID, model, message)
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

		h.triggerAISummary(ctx, versionID)
		return SnapshotResult{Path: filePath, Status: "captured", VersionID: &versionID}
	}

	prevContent, err := h.rebuildContent(ctx, latestVer)
	if err != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("重建上一版本内容失败: %v", err)}
	}

	diffs := storage.ComputeDiffs(string(prevContent), content)

	baseID := latestVer["id"].(int64)

	tx, txErr := h.DB.BeginTx(ctx)
	if txErr != nil {
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("开启事务失败: %v", txErr)}
	}

	versionID, txErr := tx.CreateVersion(ctx, fileID, "delta", nil, nil, &baseID, action, sourceID, sessionID, model, message)
	if txErr != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			slog.Error("rollback failed", "error", rbErr)
		}
		return SnapshotResult{Path: filePath, Status: "error", Reason: fmt.Sprintf("写入版本记录失败: %v", txErr)}
	}

	// TODO: DeltaStore.Append 在事务外执行。如果后续步骤失败导致事务回滚，
	// delta 文件会留下孤儿数据（不影响正确性但会膨胀文件）。
	// 长期方案：将 Append 移入事务或采用两阶段提交。
	threshold := h.Config.Compact.DeltaCompressThreshold
	offset, _, txErr := h.DeltaStore.Append(fileID, versionID, diffs, threshold)
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

	h.triggerAISummary(ctx, versionID)
	return SnapshotResult{Path: filePath, Status: "captured", VersionID: &versionID}
}

// triggerAISummary 在快照成功后向 ai_summaries 表插入 pending 记录（fire-and-forget）。
func (h *Handler) triggerAISummary(ctx context.Context, versionID int64) {
	if h.AIWorker == nil {
		return
	}
	if !h.Config.AI.Triggers.OnSnapshot {
		return
	}
	_, err := h.DB.Handle().ExecContext(ctx, `
		INSERT OR IGNORE INTO ai_summaries (version_id, status) VALUES (?, 'pending')
	`, versionID)
	if err != nil {
		h.Logger.Warn("trigger AI summary insert failed", "version_id", versionID, "error", err)
	}
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

func (h *Handler) ProcessActivity(ctx context.Context, project, source string, limit int) ([]ActivityItem, error) {
	query := `
		SELECT v.id AS versionId, f.id AS fileId, f.path AS filePath,
		       p.id AS projectId, p.name AS projectName,
		       v.action, s.name AS source, v.changed_at AS timestamp
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE p.is_deleted = 0
	`
	args := []any{}

	if project != "" {
		query += " AND p.name = ?"
		args = append(args, project)
	}

	if source != "" {
		query += " AND s.name = ?"
		args = append(args, source)
	}

	query += " ORDER BY v.changed_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activity := make([]ActivityItem, 0)
	for rows.Next() {
		var item ActivityItem
		if err := rows.Scan(&item.VersionID, &item.FileID, &item.FilePath, &item.ProjectID, &item.ProjectName, &item.Action, &item.Source, &item.Timestamp); err != nil {
			return nil, err
		}
		activity = append(activity, item)
	}

	return activity, nil
}

func (h *Handler) ProcessFiles(ctx context.Context, project string, limit, offset int) ([]map[string]any, error) {
	query := `
		SELECT f.path, f.latest_version_id, f.created_at
		FROM files f
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
		ORDER BY f.path LIMIT ? OFFSET ?
	`
	rows, err := h.DB.Query(ctx, query, project, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]map[string]any, 0)
	for rows.Next() {
		var filePath string
		var latestVersionID *int64
		var createdAt string
		if err := rows.Scan(&filePath, &latestVersionID, &createdAt); err != nil {
			return nil, err
		}
		files = append(files, map[string]any{
			"path":            filePath,
			"latestVersionId": latestVersionID,
			"createdAt":       createdAt,
		})
	}

	return files, nil
}

func (h *Handler) ProcessStats(ctx context.Context, project string) (map[string]any, error) {
	if project != "" {
		return h.ProcessStatsByProject(ctx, project)
	}
	return h.DB.GetStats(ctx)
}

// ProcessSummary 查询 AI 摘要（共享核心逻辑，被 HandleSummary 和 MCP 调用）。
func (h *Handler) ProcessSummary(ctx context.Context, project, path, since, until string, limit, offset int) (map[string]any, error) {
	if h.AIWorker == nil {
		return nil, fmt.Errorf("AI 模块未启用")
	}

	query := `
		SELECT v.id, v.action, v.changed_at, s.name as source,
		       a.summary, a.status as summary_status, a.model as ai_model
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		LEFT JOIN ai_summaries a ON v.id = a.version_id
		WHERE p.is_deleted = 0 AND f.is_deleted = 0
	`
	args := []any{}

	if project != "" {
		query += " AND p.name = ?"
		args = append(args, project)
	}
	if path != "" {
		query += " AND f.path = ?"
		args = append(args, path)
	}
	if since != "" {
		query += " AND v.changed_at >= ?"
		args = append(args, since)
	}
	if until != "" {
		query += " AND v.changed_at <= ?"
		args = append(args, until)
	}

	query += " ORDER BY v.changed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	type summaryEntry struct {
		VersionID     int64  `json:"versionId"`
		Action        string `json:"action"`
		Timestamp     string `json:"timestamp"`
		Source        string `json:"source"`
		Summary       string `json:"summary,omitempty"`
		SummaryStatus string `json:"summaryStatus,omitempty"`
		AIModel       string `json:"aiModel,omitempty"`
	}

	var entries []summaryEntry
	for rows.Next() {
		var e summaryEntry
		var summary, summaryStatus, aiModel *string
		if err := rows.Scan(&e.VersionID, &e.Action, &e.Timestamp, &e.Source, &summary, &summaryStatus, &aiModel); err != nil {
			return nil, fmt.Errorf("扫描失败: %w", err)
		}
		if summary != nil {
			e.Summary = *summary
		}
		if summaryStatus != nil {
			e.SummaryStatus = *summaryStatus
		}
		if aiModel != nil {
			e.AIModel = *aiModel
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []summaryEntry{}
	}

	return map[string]any{
		"summaries": entries,
	}, nil
}

// ProcessSession 查询会话级 AI 分析（共享核心逻辑，被 HandleSession 和 MCP 调用）。
func (h *Handler) ProcessSession(ctx context.Context, project, sessionID string) (map[string]any, error) {
	if h.AIWorker == nil {
		return nil, fmt.Errorf("AI 模块未启用")
	}

	// Check cache first
	var cachedSummary *string
	var cachedStatus string
	var cachedModel *string
	query := `SELECT summary, status, model FROM ai_sessions WHERE session_id = ?`
	args := []any{sessionID}
	if project != "" {
		query += " AND project_name = ?"
		args = append(args, project)
	}
	row := h.DB.Handle().QueryRowContext(ctx, query, args...)
	if err := row.Scan(&cachedSummary, &cachedStatus, &cachedModel); err == nil {
		result := map[string]any{
			"sessionId": sessionID,
			"status":    cachedStatus,
		}
		if cachedSummary != nil {
			result["summary"] = *cachedSummary
		}
		if cachedModel != nil {
			result["model"] = *cachedModel
		}
		// Always return changes list
		changes, err := h.getSessionChanges(ctx, project, sessionID)
		if err != nil {
			return nil, fmt.Errorf("查询变更列表失败: %w", err)
		}
		result["changes"] = changes
		return result, nil
	}

	// No cache - trigger analysis
	changes, err := h.getSessionChanges(ctx, project, sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询变更列表失败: %w", err)
	}

	// Determine project name
	projectName := project
	if projectName == "" && len(changes) > 0 {
		projectName = changes[0].ProjectName
	}

	// Insert pending record
	_, err = h.DB.Handle().ExecContext(ctx, `
		INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')
		ON CONFLICT(session_id) DO UPDATE SET status = 'pending', updated_at = CURRENT_TIMESTAMP
	`, sessionID, projectName)
	if err != nil {
		return nil, fmt.Errorf("插入待处理记录失败: %w", err)
	}

	result := map[string]any{
		"sessionId": sessionID,
		"status":    "pending",
		"changes":   changes,
	}
	return result, nil
}

type SessionChangeEntry struct {
	FilePath    string `json:"filePath"`
	ProjectName string `json:"projectName"`
	Action      string `json:"action"`
	Message     string `json:"message,omitempty"`
	Timestamp   string `json:"timestamp"`
}

type FileChangeCount struct {
	FilePath string `json:"filePath"`
	Count    int    `json:"count"`
}

func (h *Handler) getSessionChanges(ctx context.Context, project, sessionID string) ([]SessionChangeEntry, error) {
	query := `
		SELECT f.path, p.name, v.action, v.message, v.changed_at
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE v.session_id = ? AND p.is_deleted = 0 AND f.is_deleted = 0
	`
	args := []any{sessionID}
	if project != "" {
		query += " AND p.name = ?"
		args = append(args, project)
	}
	query += " ORDER BY v.changed_at ASC"

	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []SessionChangeEntry
	for rows.Next() {
		var c SessionChangeEntry
		if err := rows.Scan(&c.FilePath, &c.ProjectName, &c.Action, &c.Message, &c.Timestamp); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	if changes == nil {
		changes = []SessionChangeEntry{}
	}
	return changes, nil
}

// ProcessTrends 查询趋势分析（共享核心逻辑，被 HandleTrends 和 MCP 调用）。
func (h *Handler) ProcessTrends(ctx context.Context, project, since, until string, topFilesLimit int) (map[string]any, error) {
	if h.AIWorker == nil {
		return nil, fmt.Errorf("AI 模块未启用")
	}

	// Get basic stats
	var totalChanges int
	var totalFiles int64
	statsQuery := `
		SELECT COUNT(*), COUNT(DISTINCT f.path)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.is_deleted = 0 AND f.is_deleted = 0 AND v.changed_at >= ? AND v.changed_at <= ?
	`
	args := []any{since, until}
	if project != "" {
		statsQuery += " AND p.name = ?"
		args = append(args, project)
	}
	if err := h.DB.Handle().QueryRowContext(ctx, statsQuery, args...).Scan(&totalChanges, &totalFiles); err != nil {
		return nil, fmt.Errorf("查询统计失败: %w", err)
	}

	// Get source breakdown
	sourceBreakdown := make(map[string]int)
	srcQuery := `
		SELECT s.name, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE p.is_deleted = 0 AND f.is_deleted = 0 AND v.changed_at >= ? AND v.changed_at <= ?
	`
	srcArgs := []any{since, until}
	if project != "" {
		srcQuery += " AND p.name = ?"
		srcArgs = append(srcArgs, project)
	}
	srcQuery += " GROUP BY s.name ORDER BY COUNT(*) DESC"
	srcRows, err := h.DB.Query(ctx, srcQuery, srcArgs...)
	if err != nil {
		return nil, fmt.Errorf("查询来源分布失败: %w", err)
	}
	defer srcRows.Close()
	for srcRows.Next() {
		var name string
		var count int
		if err := srcRows.Scan(&name, &count); err != nil {
			continue
		}
		sourceBreakdown[name] = count
	}

	// Get top files
	topFiles := make([]FileChangeCount, 0)
	fileQuery := `
		SELECT f.path, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.is_deleted = 0 AND f.is_deleted = 0 AND v.changed_at >= ? AND v.changed_at <= ?
	`
	fileArgs := []any{since, until}
	if project != "" {
		fileQuery += " AND p.name = ?"
		fileArgs = append(fileArgs, project)
	}
	fileQuery += " GROUP BY f.path ORDER BY COUNT(*) DESC LIMIT ?"
	fileArgs = append(fileArgs, topFilesLimit)
	fileRows, err := h.DB.Query(ctx, fileQuery, fileArgs...)
	if err != nil {
		return nil, fmt.Errorf("查询活跃文件失败: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var fc FileChangeCount
		if err := fileRows.Scan(&fc.FilePath, &fc.Count); err != nil {
			continue
		}
		topFiles = append(topFiles, fc)
	}

	result := map[string]any{
		"period":          fmt.Sprintf("%s ~ %s", since, until),
		"totalFiles":      totalFiles,
		"totalChanges":    totalChanges,
		"sourceBreakdown": sourceBreakdown,
		"topFiles":        topFiles,
	}

	// Check for cached trend summary
	if project != "" {
		var cachedSummary *string
		var cachedStatus string
		var cachedModel *string
		row := h.DB.Handle().QueryRowContext(ctx, `
			SELECT summary, status, model FROM ai_trends
			WHERE project_name = ? AND period_start = date(?) AND period_end = date(?)
		`, project, since, until)
		if err := row.Scan(&cachedSummary, &cachedStatus, &cachedModel); err == nil {
			result["status"] = cachedStatus
			if cachedSummary != nil {
				result["summary"] = *cachedSummary
			}
			if cachedModel != nil {
				result["model"] = *cachedModel
			}
		} else {
			// Insert pending
			h.DB.Handle().ExecContext(ctx, `
				INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, date(?), date(?), 'pending')
			`, project, since, until)
			result["status"] = "pending"
		}
	}

	return result, nil
}
