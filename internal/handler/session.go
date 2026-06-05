package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// HandleSession handles GET /api/files/session
// Query params: project, sessionId
func (h *Handler) HandleSession(w http.ResponseWriter, r *http.Request) {
	if h.AIWorker == nil {
		writeError(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI 模块未启用")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	project := q.Get("project")
	sessionID := q.Get("sessionId")

	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "sessionId 不能为空")
		return
	}

	result, err := h.ProcessSession(ctx, project, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("查询失败: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleTrends handles GET /api/files/trends
// Query params: project, since, until, limit
func (h *Handler) HandleTrends(w http.ResponseWriter, r *http.Request) {
	if h.AIWorker == nil {
		writeError(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI 模块未启用")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	project := q.Get("project")
	since := q.Get("since")
	until := q.Get("until")

	// Default to last 7 days if not specified
	if since == "" {
		since = time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	}
	if until == "" {
		until = time.Now().Format(time.RFC3339)
	}

	limit := 10
	if l := q.Get("topFiles"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	result, err := h.ProcessTrends(ctx, project, since, until, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("查询失败: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}
