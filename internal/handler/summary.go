package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
)

// HandleSummary handles GET /api/files/summary
// Query params: project, path, since, until, limit, offset
func (h *Handler) HandleSummary(w http.ResponseWriter, r *http.Request) {
	if h.AIWorker == nil {
		writeError(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI 模块未启用")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	project := q.Get("project")
	path := q.Get("path")
	since := q.Get("since")
	until := q.Get("until")

	limit := 20
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	offset := 0
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	result, err := h.ProcessSummary(ctx, project, path, since, until, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleSummaryRefresh handles POST /api/files/summary/refresh
// Query params: project, path, version (optional)
func (h *Handler) HandleSummaryRefresh(w http.ResponseWriter, r *http.Request) {
	if h.AIWorker == nil {
		writeError(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI 模块未启用")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	project := q.Get("project")
	path := q.Get("path")
	versionStr := q.Get("version")

	var versionID *int64
	if versionStr != "" {
		vid, err := strconv.ParseInt(versionStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "version 必须是整数")
			return
		}
		versionID = &vid
	}

	// Find versions to refresh
	var versionsToRefresh []int64

	if versionID != nil {
		// Refresh specific version
		versionsToRefresh = []int64{*versionID}
	} else {
		// Find latest version for the file
		query := `
			SELECT v.id FROM versions v
			JOIN files f ON v.file_id = f.id
			JOIN projects p ON f.project_id = p.id
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

		query += " ORDER BY v.changed_at DESC LIMIT 1"

		row := h.DB.Handle().QueryRowContext(ctx, query, args...)
		var vid int64
		if err := row.Scan(&vid); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "未找到版本")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("查询失败: %v", err))
			return
		}
		versionsToRefresh = []int64{vid}
	}

	// Insert or update ai_summaries with pending status
	refreshed := 0
	for _, vid := range versionsToRefresh {
		_, err := h.DB.Handle().ExecContext(ctx, `
			INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')
			ON CONFLICT(version_id) DO UPDATE SET status = 'pending', updated_at = CURRENT_TIMESTAMP
		`, vid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("刷新失败: %v", err))
			return
		}
		refreshed++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":   fmt.Sprintf("已排队 %d 个版本等待摘要生成", refreshed),
		"refreshed": refreshed,
	})
}
