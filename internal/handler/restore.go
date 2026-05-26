package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) HandleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path 参数缺失")
		return
	}

	versionStr := r.URL.Query().Get("version")
	if versionStr == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "version 参数缺失")
		return
	}

	versionID, err := strconv.ParseInt(versionStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "无效的版本号")
		return
	}

	if versionID <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "无效的版本号")
		return
	}

	h.Logger.Info("restore request", "path", path, "version_id", versionID)

	content, timestamp, err := h.doRestore(r.Context(), path, versionID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "不存在") {
			writeError(w, http.StatusNotFound, "VERSION_NOT_FOUND", err.Error())
		} else if strings.Contains(err.Error(), "delete") {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		} else {
			h.Logger.Error("restore rebuild failed", "path", path, "version_id", versionID, "error", err)
			writeError(w, http.StatusInternalServerError, "CORRUPTED_DATA", fmt.Sprintf("数据恢复失败: %v", err))
		}
		return
	}

	h.Logger.Info("restore success", "path", path, "version_id", versionID, "content_size", len(content))
	resp := map[string]any{
		"path":      path,
		"version":   versionID,
		"timestamp": timestamp,
		"content":   string(content),
	}

	writeJSON(w, http.StatusOK, resp)
}

// doRestore resolves project/file, locks, gets version, rebuilds content (shared by HandleRestore and ProcessRestore).
func (h *Handler) doRestore(ctx context.Context, path string, versionID int64) ([]byte, string, error) {
	project, err := h.DB.FindProjectByPath(ctx, path)
	if err != nil {
		return nil, "", fmt.Errorf("project not found: %w", err)
	}

	relPath := path
	rootPath := project["rootPath"].(string)
	if len(path) > len(rootPath) && path[:len(rootPath)] == rootPath {
		relPath = strings.TrimPrefix(path[len(rootPath):], "/")
	}

	file, err := h.DB.GetFileByPath(ctx, project["id"].(int64), relPath)
	if err != nil {
		return nil, "", fmt.Errorf("file not found: %w", err)
	}
	fileID := file["id"].(int64)

	fileMu := h.getFileLock(fileID)
	fileMu.RLock()
	defer fileMu.RUnlock()

	ver, err := h.DB.GetVersion(ctx, versionID)
	if err != nil {
		return nil, "", fmt.Errorf("version not found: %w", err)
	}

	if ver["fileID"].(int64) != fileID {
		return nil, "", fmt.Errorf("version does not belong to this file")
	}

	if ver["action"].(string) == "delete" {
		return nil, "", fmt.Errorf("delete version has no content")
	}

	content, err := h.rebuildContent(ctx, ver)
	if err != nil {
		return nil, "", fmt.Errorf("rebuild content: %w", err)
	}

	return content, ver["changedAt"].(string), nil
}
