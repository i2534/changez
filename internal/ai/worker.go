package ai

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/dbutil"
	"github.com/changez/changez/internal/storage"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// Worker 后台 AI 摘要生成器。
type Worker struct {
	provider   Provider
	db         *db.DB
	blobStore  *storage.BlobStore
	deltaStore *storage.DeltaStore
	cfg        *config.AICfg
	logger     *slog.Logger
}

// NewWorker 创建新的 AI Worker。
func NewWorker(provider Provider, database *db.DB, blobStore *storage.BlobStore, deltaStore *storage.DeltaStore, cfg *config.AICfg, logger *slog.Logger) *Worker {
	return &Worker{
		provider:   provider,
		db:         database,
		blobStore:  blobStore,
		deltaStore: deltaStore,
		cfg:        cfg,
		logger:     logger,
	}
}

// Run 后台运行，消费待处理的摘要、会话和趋势记录。
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("ai worker started",
		"model", w.cfg.Model,
		"batch_size", w.cfg.Triggers.BatchSize,
	)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	tick := 0
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("ai worker stopped")
			return
		case <-ticker.C:
			w.processPending(ctx)
			w.processPendingSessions(ctx)
			w.processPendingTrends(ctx)

			tick++
			if tick%60 == 0 {
				w.logProgress(ctx)
			}
		}
	}
}

func (w *Worker) logProgress(ctx context.Context) {
	type counts struct {
		pending, completed, failed int64
	}
	var summary, session, trend counts

	row := w.db.Handle().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		FROM ai_summaries`)
	if err := row.Scan(&summary.pending, &summary.completed, &summary.failed); err != nil {
		return
	}

	row = w.db.Handle().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		FROM ai_sessions`)
	if err := row.Scan(&session.pending, &session.completed, &session.failed); err != nil {
		return
	}

	row = w.db.Handle().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0)
		FROM ai_trends`)
	if err := row.Scan(&trend.pending, &trend.completed, &trend.failed); err != nil {
		return
	}

	w.logger.Info("worker progress",
		"summaries", fmt.Sprintf("pending:%d completed:%d failed:%d", summary.pending, summary.completed, summary.failed),
		"sessions", fmt.Sprintf("pending:%d completed:%d failed:%d", session.pending, session.completed, session.failed),
		"trends", fmt.Sprintf("pending:%d completed:%d failed:%d", trend.pending, trend.completed, trend.failed),
	)
}

// processPending 查询 pending 记录，批量生成摘要。
func (w *Worker) processPending(ctx context.Context) {
	batchSize := w.cfg.Triggers.BatchSize
	if batchSize <= 0 {
		batchSize = 5
	}

	rows, err := w.db.Handle().QueryContext(ctx, `
		SELECT a.id, a.version_id, v.file_id, f.path, p.root_path, v.action, v.changed_at,
		       v.session_id, v.model, v.message, s.name as source_name
		FROM ai_summaries a
		JOIN versions v ON a.version_id = v.id
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE a.status = 'pending'
		ORDER BY a.created_at ASC
		LIMIT ?
	`, batchSize)
	if err != nil {
		w.logger.Warn("query pending summaries failed", "error", err)
		return
	}
	defer rows.Close()

	type pendingItem struct {
		summaryID int64
		versionID int64
		fileID    int64
		filePath  string
		rootPath  string
		action    string
		changedAt string
		sourceName string
	}

	var items []pendingItem
	for rows.Next() {
		var item pendingItem
		if err := rows.Scan(&item.summaryID, &item.versionID, &item.fileID, &item.filePath, &item.rootPath, &item.action, &item.changedAt, new(sql.NullString), new(sql.NullString), new(sql.NullString), &item.sourceName); err != nil {
			w.logger.Warn("scan pending summary failed", "error", err)
			continue
		}
		items = append(items, item)
	}
	rows.Close()

	if len(items) == 0 {
		return
	}

	var wg sync.WaitGroup
	var successCount int32

	for _, item := range items {
		wg.Add(1)
		go func(item pendingItem) {
			defer wg.Done()

			diff, err := w.getDiffForVersion(ctx, item.versionID, item.fileID, 0)
			if err != nil {
				w.logger.Warn("get diff failed", "version_id", item.versionID, "error", err)
				_ = w.markFailed(ctx, item.summaryID, err.Error())
				return
			}

			relPath := item.filePath
			if len(item.filePath) > len(item.rootPath) && strings.HasPrefix(item.filePath, item.rootPath) {
				relPath = item.filePath[len(item.rootPath):]
				if len(relPath) > 0 && relPath[0] == '/' {
					relPath = relPath[1:]
				}
			}

			ctxInfo := SummaryContext{
				FilePath:  relPath,
				Action:    item.action,
				Source:    item.sourceName,
				Timestamp: item.changedAt,
			}

			summary, err := w.provider.Summarize(ctx, diff, ctxInfo)
			if err != nil {
				w.logger.Warn("summarize failed", "version_id", item.versionID, "error", err)
				_ = w.markFailed(ctx, item.summaryID, err.Error())
				return
			}

			if strings.TrimSpace(summary) == "" {
				w.logger.Warn("empty summary from provider", "version_id", item.versionID)
				_ = w.markFailed(ctx, item.summaryID, "empty response from provider")
				return
			}

			if err := w.markCompleted(ctx, item.summaryID, summary); err != nil {
				w.logger.Warn("mark completed failed", "summary_id", item.summaryID, "error", err)
				return
			}

			atomic.AddInt32(&successCount, 1)
			logSummary := summary
			if len([]rune(logSummary)) > 80 {
				logSummary = string([]rune(logSummary)[:80])
			}
			w.logger.Info("summary generated",
				"version_id", item.versionID,
				"file", relPath,
				"summary", logSummary,
			)
		}(item)
	}

	wg.Wait()
	if count := int(successCount); count > 0 {
		w.logger.Info("batch processing complete", "processed", count)
	}
}

// getDiffForVersion 获取版本的 diff 内容，maxSize 控制截断大小（0=使用配置默认值）。
func (w *Worker) getDiffForVersion(ctx context.Context, versionID int64, fileID int64, maxSize int) (string, error) {
	ver, err := w.db.GetVersion(ctx, versionID)
	if err != nil {
		return "", fmt.Errorf("get version: %w", err)
	}
	if ver["fileID"].(int64) != fileID {
		return "", fmt.Errorf("version %d does not belong to file %d", versionID, fileID)
	}

	storageMode := ver["storageMode"].(string)

	if storageMode == "delete" {
		return "[文件被删除]", nil
	}

	currentContent, err := w.rebuildContent(ctx, ver)
	if err != nil {
		return "", fmt.Errorf("rebuild current content: %w", err)
	}

	// Truncate oversized diffs to avoid exceeding LLM context limits
	maxDiffSize := maxSize
	if maxDiffSize <= 0 {
		maxDiffSize = w.cfg.Triggers.MaxDiffSize
	}
	if maxDiffSize <= 0 {
		maxDiffSize = 64 * 1024 // 64KB default
	}

	if baseVerID, ok := dbutil.AsInt64Ptr(ver["baseID"]); ok {
		baseVer, err := w.db.GetVersion(ctx, baseVerID)
		if err != nil {
			return "", fmt.Errorf("get base version: %w", err)
		}

		baseAction := baseVer["action"].(string)
		if baseAction == "delete" {
			newFileContent := fmt.Sprintf("[新文件创建]\n%s", string(currentContent))
			return truncateDiff(newFileContent, maxDiffSize), nil
		}

		baseContent, err := w.rebuildContent(ctx, baseVer)
		if err != nil {
			return "", fmt.Errorf("rebuild base content: %w", err)
		}

		diff := computeUnifiedDiff(string(baseContent), string(currentContent))
		return truncateDiff(diff, maxDiffSize), nil
	}

	newFileContent := fmt.Sprintf("[新文件创建]\n%s", string(currentContent))
	return truncateDiff(newFileContent, maxDiffSize), nil
}

// rebuildContent 根据版本记录重建完整文件内容。
func (w *Worker) rebuildContent(ctx context.Context, ver map[string]any) ([]byte, error) {
	storageMode := ver["storageMode"].(string)

	switch storageMode {
	case "blob":
		hash, ok := dbutil.AsStringPtr(ver["blobHash"])
		if !ok {
			return nil, fmt.Errorf("blob mode but hash is nil")
		}
		return w.blobStore.Read(hash)

	case "delta":
		return w.rebuildFromDeltaChain(ctx, ver)

	default:
		return nil, fmt.Errorf("unsupported storage mode: %s", storageMode)
	}
}

// rebuildFromDeltaChain 从指定版本回溯到最近的 blob checkpoint，重建完整内容。
func (w *Worker) rebuildFromDeltaChain(ctx context.Context, ver map[string]any) ([]byte, error) {
	dmp := diffmatchpatch.New()

	type deltaStep struct {
		diffs []diffmatchpatch.Diff
	}

	var steps []deltaStep
	currentVer := ver
	for depth := 0; depth < 1000; depth++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch currentVer["storageMode"].(string) {
		case "blob":
			hash, ok := dbutil.AsStringPtr(currentVer["blobHash"])
			if !ok {
				return nil, fmt.Errorf("blob checkpoint but hash is nil")
			}
			content, err := w.blobStore.Read(hash)
			if err != nil {
				return nil, fmt.Errorf("read blob checkpoint: %w", err)
			}

			for i := len(steps) - 1; i >= 0; i-- {
				content = []byte(dmp.DiffText2(steps[i].diffs))
			}
			return content, nil

		case "delta":
			offset, ok := dbutil.AsInt64Ptr(currentVer["deltaOffset"])
			if !ok {
				return nil, fmt.Errorf("delta mode but offset is nil")
			}
			fileID := currentVer["fileID"].(int64)

			_, diffs, _, err := w.deltaStore.ReadEntry(fileID, offset)
			if err != nil {
				return nil, fmt.Errorf("read delta entry: %w", err)
			}
			steps = append(steps, deltaStep{diffs: diffs})

			bid, ok := dbutil.AsInt64Ptr(currentVer["baseID"])
			if !ok {
				return nil, fmt.Errorf("delta mode but base_id is nil")
			}
			baseVer, err := w.db.GetVersion(ctx, bid)
			if err != nil {
				return nil, fmt.Errorf("query base version %d: %w", bid, err)
			}
			currentVer = baseVer

		default:
			return nil, fmt.Errorf("unsupported storage mode: %s", currentVer["storageMode"].(string))
		}
	}
	return nil, fmt.Errorf("delta chain too long, exceeded max depth 1000")
}

// computeUnifiedDiff 使用 diffmatchpatch 生成统一格式的 diff。
// 与 handler/diff.go:renderUnifiedDiff 共享 DiffLinesToChars + DiffMain 核心逻辑，
// 但输出简化格式（无文件头时间戳、无 hunk 分割），适用于 AI 摘要场景。
func truncateDiff(diff string, maxBytes int) string {
	if len(diff) <= maxBytes {
		return diff
	}
	// Binary search for the largest byte position ≤ maxBytes that falls on a rune boundary
	lo, hi := 0, maxBytes
	for lo < hi {
		mid := (lo + hi + 1) / 2
		// Find the end of the rune that contains byte position mid
		runeEnd := mid
		for runeEnd < len(diff) && (diff[runeEnd]&0xE0) == 0x80 {
			runeEnd++
		}
		if runeEnd <= maxBytes {
			lo = runeEnd
		} else {
			// Back up to start of this rune
			runeStart := mid - 1
			for runeStart > 0 && (diff[runeStart]&0xE0) == 0x80 {
				runeStart--
			}
			hi = runeStart
		}
	}
	cutoff := lo
	if cutoff == 0 {
		cutoff = len([]byte(string(rune([]rune(diff)[0]))))
	}
	return diff[:cutoff] + fmt.Sprintf("\n// ... truncated (%d total bytes)", len(diff))
}

func computeUnifiedDiff(oldContent, newContent string) string {
	dmp := diffmatchpatch.New()
	charsA, charsB, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	lineDiffs := dmp.DiffMain(charsA, charsB, true)

	charToLine := make(map[rune]string)
	for i := 1; i < len(lineArray); i++ {
		charToLine[rune(i)] = lineArray[i]
	}

	var b strings.Builder
	b.WriteString("--- original\n")
	b.WriteString("+++ modified\n")

	const contextLines = 3
	consecutiveEqual := 0

	for _, d := range lineDiffs {
		for _, ch := range d.Text {
			line := charToLine[ch]
			var prefix byte
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				prefix = ' '
				consecutiveEqual++
				if consecutiveEqual > contextLines*2 {
					if consecutiveEqual == contextLines*2+1 {
						b.WriteString("...\n")
					}
					continue
				}
			case diffmatchpatch.DiffInsert:
				prefix = '+'
				consecutiveEqual = 0
			case diffmatchpatch.DiffDelete:
				prefix = '-'
				consecutiveEqual = 0
			}
			b.WriteByte(prefix)
			b.WriteString(line)
		}
	}

	return b.String()
}

func (w *Worker) markCompleted(ctx context.Context, summaryID int64, summary string) error {
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_summaries SET summary = ?, status = 'completed', updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		summary, summaryID,
	)
	return err
}

func (w *Worker) markFailed(ctx context.Context, summaryID int64, errorMsg string) error {
	maxRetries := w.cfg.Triggers.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_summaries SET summary = ?, retries = retries + 1, status = CASE WHEN retries + 1 >= ? THEN 'failed' ELSE 'pending' END, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		errorMsg, maxRetries, summaryID,
	)
	return err
}

func (w *Worker) processPendingSessions(ctx context.Context) {
	row := w.db.Handle().QueryRowContext(ctx, `
		SELECT id, session_id, project_name FROM ai_sessions WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1
	`)
	var sessionID int64
	var sessionSid string
	var projectName string
	if err := row.Scan(&sessionID, &sessionSid, &projectName); err != nil {
		return
	}

	// Build query with project filter
	query := `
		SELECT f.path, p.root_path, v.action, v.message, v.changed_at, v.id, v.file_id
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE v.session_id = ? AND p.is_deleted = 0 AND f.is_deleted = 0
	`
	args := []any{sessionSid}
	if projectName != "" {
		query += " AND p.name = ?"
		args = append(args, projectName)
	}
	query += " ORDER BY v.changed_at ASC"

	rows, err := w.db.Handle().QueryContext(ctx, query, args...)
	if err != nil {
		w.logger.Warn("query session versions failed", "session_id", sessionSid, "error", err)
		return
	}
	defer rows.Close()

	var changes []SessionChange
	maxDiffSize := w.cfg.Triggers.MaxSessionDiff
	if maxDiffSize <= 0 {
		maxDiffSize = 8 * 1024
	}

	for rows.Next() {
		var (
			filePath  string
			rootPath  string
			action    string
			message   string
			changedAt string
			versionID int64
			fileID    int64
		)
		if err := rows.Scan(&filePath, &rootPath, &action, &message, &changedAt, &versionID, &fileID); err != nil {
			continue
		}

		relPath := filePath
		if len(filePath) > len(rootPath) && strings.HasPrefix(filePath, rootPath) {
			relPath = filePath[len(rootPath):]
			if len(relPath) > 0 && relPath[0] == '/' {
				relPath = relPath[1:]
			}
		}

		var diff string
		if action != "delete" {
			if d, getErr := w.getDiffForVersion(ctx, versionID, fileID, maxDiffSize); getErr == nil {
				diff = d
			}
		} else {
			diff = "[文件被删除]"
		}

		changes = append(changes, SessionChange{
			FilePath:  relPath,
			Action:    action,
			Diff:      diff,
			Message:   message,
			Timestamp: changedAt,
		})
	}

	// Cap total changes to prevent context overflow
	const maxChanges = 100
	if len(changes) > maxChanges {
		w.logger.Warn("session changes capped", "session_id", sessionSid, "total", len(changes), "capped", maxChanges)
		changes = changes[:maxChanges]
	}

	if len(changes) == 0 {
		_ = w.markSessionCompleted(ctx, sessionID, "本次会话无变更记录")
		return
	}

summary, err := w.provider.AnalyzeSession(ctx, changes)
	if err != nil {
		w.logger.Warn("analyze session failed", "session_id", sessionSid, "error", err)
		_ = w.markSessionFailed(ctx, sessionID, err.Error())
		return
	}

	if strings.TrimSpace(summary) == "" {
		w.logger.Warn("empty session summary, marking failed", "session_id", sessionSid)
		_ = w.markSessionFailed(ctx, sessionID, "empty response from provider")
		return
	}

	if err := w.markSessionCompleted(ctx, sessionID, summary); err != nil {
		w.logger.Warn("mark session completed failed", "session_id", sessionSid, "error", err)
		_ = w.markSessionFailed(ctx, sessionID, fmt.Sprintf("mark completed failed: %v", err))
	}

	w.logger.Info("session analysis completed", "session_id", sessionSid, "changes", len(changes))
}

func (w *Worker) processPendingTrends(ctx context.Context) {
	row := w.db.Handle().QueryRowContext(ctx, `
		SELECT id, project_name, period_start, period_end FROM ai_trends WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1
	`)
	var trendID int64
	var projectName, periodStart, periodEnd string
	if err := row.Scan(&trendID, &projectName, &periodStart, &periodEnd); err != nil {
		return
	}

	var totalChanges int
	var totalFiles int64
	if err := w.db.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT f.path)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
			AND v.changed_at >= ? AND v.changed_at <= ?
	`, projectName, periodStart, periodEnd).Scan(&totalChanges, &totalFiles); err != nil {
		w.logger.Warn("query trend stats failed", "project", projectName, "error", err)
		_ = w.markTrendFailed(ctx, trendID, err.Error())
		return
	}

	sourceBreakdown := make(map[string]int)
	if srcRows, err := w.db.Handle().QueryContext(ctx, `
		SELECT s.name, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
AND v.changed_at >= ? AND v.changed_at <= ?
		GROUP BY s.name
	`, projectName, periodStart, periodEnd); err != nil {
		w.logger.Warn("query trend source breakdown failed", "project", projectName, "error", err)
	} else {
		defer srcRows.Close()
		for srcRows.Next() {
			var name string
			var count int
			if srcRows.Scan(&name, &count) == nil {
				sourceBreakdown[name] = count
			}
		}
	}

	topFiles := make([]FileChangeCount, 0)
	if fileRows, err := w.db.Handle().QueryContext(ctx, `
		SELECT f.path, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
AND v.changed_at >= ? AND v.changed_at <= ?
		GROUP BY f.path, v.file_id
		ORDER BY COUNT(*) DESC
		LIMIT 10
	`, projectName, periodStart, periodEnd); err != nil {
		w.logger.Warn("query trend top files failed", "project", projectName, "error", err)
	} else {
		defer fileRows.Close()
		for fileRows.Next() {
			var fc FileChangeCount
			if fileRows.Scan(&fc.FilePath, &fc.Count) == nil {
				topFiles = append(topFiles, fc)
			}
		}
	}

	stats := TrendStats{
		Period:          fmt.Sprintf("%s ~ %s", periodStart, periodEnd),
		TotalFiles:      int(totalFiles),
		TotalChanges:    totalChanges,
		SourceBreakdown: sourceBreakdown,
		TopFiles:        topFiles,
	}

	summary, err := w.provider.AnalyzeTrends(ctx, stats)
	if err != nil {
		w.logger.Warn("analyze trends failed", "project", projectName, "error", err)
		_ = w.markTrendFailed(ctx, trendID, err.Error())
		return
	}

	if strings.TrimSpace(summary) == "" {
		w.logger.Warn("empty trends summary, marking failed", "project", projectName)
		_ = w.markTrendFailed(ctx, trendID, "empty response from provider")
		return
	}

	if err := w.markTrendCompleted(ctx, trendID, summary); err != nil {
		w.logger.Warn("mark trend completed failed", "project", projectName, "error", err)
		_ = w.markTrendFailed(ctx, trendID, fmt.Sprintf("mark completed failed: %v", err))
	}

	w.logger.Info("trend analysis completed", "project", projectName, "period", stats.Period)
}

func (w *Worker) markSessionCompleted(ctx context.Context, id int64, summary string) error {
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_sessions SET summary = ?, status = 'completed', model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		summary, w.cfg.Model, id,
	)
	return err
}

func (w *Worker) markSessionFailed(ctx context.Context, id int64, errorMsg string) error {
	maxRetries := w.cfg.Triggers.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_sessions SET summary = ?, retries = retries + 1, status = CASE WHEN retries + 1 >= ? THEN 'failed' ELSE 'pending' END, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		errorMsg, maxRetries, id,
	)
	return err
}

func (w *Worker) markTrendCompleted(ctx context.Context, id int64, summary string) error {
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_trends SET summary = ?, status = 'completed', model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		summary, w.cfg.Model, id,
	)
	return err
}

func (w *Worker) markTrendFailed(ctx context.Context, id int64, errorMsg string) error {
	maxRetries := w.cfg.Triggers.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	_, err := w.db.Handle().ExecContext(ctx,
		`UPDATE ai_trends SET summary = ?, retries = retries + 1, status = CASE WHEN retries + 1 >= ? THEN 'failed' ELSE 'pending' END, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		errorMsg, maxRetries, id,
	)
	return err
}
