package handler

import (
	"fmt"
	"net/http"
	"strconv"
)

type ActivityItem struct {
	FileID    int64  `json:"fileId"`
	FilePath  string `json:"filePath"`
	ProjectID int64  `json:"projectId"`
	ProjectName string `json:"projectName"`
	VersionID int64  `json:"versionId"`
	Action    string `json:"action"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

// HandleRecentActivity 处理 GET /api/activity，返回最近的修改活动。
func (h *Handler) HandleRecentActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	h.Logger.Debug("recent activity request", "limit", limit)

	query := `
		SELECT v.id AS versionId, f.id AS fileId, f.path AS filePath,
		       p.id AS projectId, p.name AS projectName,
		       v.action, s.name AS source, v.changed_at AS timestamp
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE p.is_deleted = 0
		ORDER BY v.changed_at DESC
		LIMIT ?
	`

	rows, err := h.DB.Query(r.Context(), query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("查询活动记录失败: %v", err))
		return
	}
	defer rows.Close()

	activity := make([]ActivityItem, 0)
	for rows.Next() {
		var item ActivityItem
		if err := rows.Scan(&item.VersionID, &item.FileID, &item.FilePath, &item.ProjectID, &item.ProjectName, &item.Action, &item.Source, &item.Timestamp); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("扫描活动行失败: %v", err))
			return
		}
		activity = append(activity, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"activity": activity})
}
