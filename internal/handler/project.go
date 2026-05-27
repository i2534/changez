package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

type ProjectRequest struct {
	Name     string `json:"name,omitempty"`
	RootPath string `json:"rootPath"`
	Extra    string `json:"extra,omitempty"`
}

func (h *Handler) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 POST")
		return
	}

	var req ProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("请求体解析失败: %v", err))
		return
	}

	rootPath := strings.TrimSpace(req.RootPath)
	if rootPath == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "rootPath 不能为空")
		return
	}

	rootPath = filepath.Clean(rootPath)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filepath.Base(rootPath)
	}

	extra := strings.TrimSpace(req.Extra)
	if extra == "" {
		extra = "{}"
	}

	ctx := r.Context()

	id, err := h.DB.CreateProject(ctx, name, rootPath, extra)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "INVALID_REQUEST", fmt.Sprintf("路径 %s 或名称 %s 已存在", rootPath, name))
			return
		}
		h.Logger.Error("create project failed", "name", name, "root_path", rootPath, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("创建项目失败: %v", err))
		return
	}

	h.Logger.Info("project created", "id", id, "name", name, "root_path", rootPath, "extra", extra)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       id,
		"name":     name,
		"rootPath": rootPath,
		"extra":    extra,
	})
}

func (h *Handler) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 DELETE")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	if idStr == "" || idStr == r.URL.Path {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "缺少项目 ID")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("无效的项目 ID: %s", idStr))
		return
	}

	if err := h.DB.SoftDeleteProject(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", err.Error())
		return
	}

	h.Logger.Info("project deleted", "id", id)
	writeJSON(w, http.StatusOK, map[string]any{"message": "项目已删除"})
}

func (h *Handler) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		return
	}

	projects, err := h.DB.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("查询项目列表失败: %v", err))
		return
	}

	if projects == nil {
		projects = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *Handler) HandleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 DELETE")
		return
	}

	project := r.URL.Query().Get("project")
	path := r.URL.Query().Get("path")
	if project == "" || path == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "需要 project 和 path 参数")
		return
	}

	ctx := r.Context()
	prj, err := h.DB.FindProjectByName(ctx, project)
	if err != nil {
		writeError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", err.Error())
		return
	}

	file, err := h.DB.GetFileByPath(ctx, prj["id"].(int64), path)
	if err != nil {
		writeError(w, http.StatusNotFound, "FILE_NOT_FOUND", err.Error())
		return
	}

	fileID := file["id"].(int64)
	fileMu := h.getFileLock(fileID)
	fileMu.Lock()
	defer fileMu.Unlock()

	if err := h.DB.SoftDeleteFile(ctx, fileID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	h.Logger.Info("file deleted", "file_id", fileID, "path", path)
	writeJSON(w, http.StatusOK, map[string]any{"message": "文件已删除"})
}
