package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/handler"
	"github.com/changez/changez/internal/storage"
)

func setupMCPHandler(t *testing.T) *handler.Handler {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	require.NoError(t, err)

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

	cfg := config.Defaults()

	fileMuMap := &sync.Map{}
	h := handler.NewHandler(database, bs, ds, &cfg, handler.NewLogger(&cfg).Logger, fileMuMap)
	return h
}

// ========== toolError Tests ==========

func TestToolError(t *testing.T) {
	result, err := toolError("something went wrong")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "something went wrong")
}

// ========== NewMCPHandler Tests ==========

func TestNewMCPHandler_ReturnsNonNil(t *testing.T) {
	h := setupMCPHandler(t)
	httpHandler := NewMCPHandler(h)
	require.NotNil(t, httpHandler)
}

func TestNewMCPHandler_AcceptsRequest(t *testing.T) {
	h := setupMCPHandler(t)
	httpHandler := NewMCPHandler(h)
	require.NotNil(t, httpHandler)

	// SSE 流式 endpoint 无法在 httptest 中完成（handler 不会返回）
	// 通过 POST JSON-RPC 请求验证路由连通性
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	httpHandler.ServeHTTP(w, req)
	// POST JSON-RPC 请求应返回非 404 响应
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestNewMCPHandler_RegistersAllTools(t *testing.T) {
	h := setupMCPHandler(t)

	expectedTools := []string{"changez_log", "changez_restore", "changez_diff", "changez_files", "changez_activity", "changez_stats"}

	toolLog, _ := NewLogTool(h)
	assert.Equal(t, expectedTools[0], toolLog.Name)
	toolRestore, _ := NewRestoreTool(h)
	assert.Equal(t, expectedTools[1], toolRestore.Name)
	toolDiff, _ := NewDiffTool(h)
	assert.Equal(t, expectedTools[2], toolDiff.Name)
	toolFiles, _ := NewFilesTool(h)
	assert.Equal(t, expectedTools[3], toolFiles.Name)
	toolActivity, _ := NewActivityTool(h)
	assert.Equal(t, expectedTools[4], toolActivity.Name)
	toolStats, _ := NewStatsTool(h)
	assert.Equal(t, expectedTools[5], toolStats.Name)
}

// ========== NewLogTool Handler Tests ==========

func TestNewLogTool_Handler_MissingPath(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewLogTool(h)
	require.NotNil(t, handlerFunc)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_log"
	req.Params.Arguments = map[string]interface{}{}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

func TestNewLogTool_Handler_NotFoundPath(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewLogTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_log"
	req.Params.Arguments = map[string]interface{}{
		"path": "/nonexistent/file.go",
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

// ========== NewRestoreTool Handler Tests ==========

func TestNewRestoreTool_Handler_ZeroVersion(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewRestoreTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_restore"
	req.Params.Arguments = map[string]interface{}{
		"path":    "/test.go",
		"version": float64(0),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "must be a positive integer")
}

func TestNewRestoreTool_Handler_NonExistentPath(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewRestoreTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_restore"
	req.Params.Arguments = map[string]interface{}{
		"path":    "/nonexistent.go",
		"version": float64(1),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

// ========== NewDiffTool Handler Tests ==========

func TestNewDiffTool_Handler_ZeroVersion(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewDiffTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_diff"
	req.Params.Arguments = map[string]interface{}{
		"path":     "/test.go",
		"versionA": float64(0),
		"versionB": float64(1),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "must be positive integers")
}

func TestNewDiffTool_Handler_NonExistentPath(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewDiffTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_diff"
	req.Params.Arguments = map[string]interface{}{
		"path":     "/nonexistent.go",
		"versionA": float64(1),
		"versionB": float64(2),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

// ========== NewFilesTool Handler Tests ==========

func TestNewFilesTool_Handler_MissingProject(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewFilesTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_files"
	req.Params.Arguments = map[string]interface{}{}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

func TestNewFilesTool_Handler_NonExistentProject(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewFilesTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_files"
	req.Params.Arguments = map[string]interface{}{
		"project": "nonexistent",
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Returns empty list, not an error
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "[]", text.Text)
}

func TestNewFilesTool_Handler_Success(t *testing.T) {
	h := setupMCPHandler(t)
	createProjectViaHandler(t, h)

	_, handlerFunc := NewFilesTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_files"
	req.Params.Arguments = map[string]interface{}{
		"project": "test",
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "[]", text.Text)
}

// ========== NewActivityTool Handler Tests ==========

func TestNewActivityTool_Handler_Success(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewActivityTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_activity"
	req.Params.Arguments = map[string]interface{}{
		"limit": float64(10),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "[]", text.Text)
}

func TestNewActivityTool_Handler_WithSource(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewActivityTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_activity"
	req.Params.Arguments = map[string]interface{}{
		"source": "opencode",
		"limit":  float64(5),
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "[]", text.Text)
}

// ========== NewStatsTool Handler Tests ==========

func TestNewStatsTool_Handler_Global(t *testing.T) {
	h := setupMCPHandler(t)
	_, handlerFunc := NewStatsTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_stats"
	req.Params.Arguments = map[string]interface{}{}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var stats map[string]interface{}
	err = json.Unmarshal([]byte(text.Text), &stats)
	require.NoError(t, err)
	assert.Contains(t, stats, "projects")
	assert.Contains(t, stats, "files")
	assert.Contains(t, stats, "versions")
}

func TestNewStatsTool_Handler_ByProject(t *testing.T) {
	h := setupMCPHandler(t)
	createProjectViaHandler(t, h)

	_, handlerFunc := NewStatsTool(h)

	req := mcp.CallToolRequest{}
	req.Params.Name = "changez_stats"
	req.Params.Arguments = map[string]interface{}{
		"project": "test",
	}

	result, err := handlerFunc(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var stats map[string]interface{}
	err = json.Unmarshal([]byte(text.Text), &stats)
	require.NoError(t, err)
	assert.Contains(t, stats, "files")
	assert.Contains(t, stats, "versions")
	assert.Contains(t, stats, "sources")
}

// ========== Tool Definition Tests ==========

func TestLogTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewLogTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_log", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "path")
	assert.Contains(t, props, "since")
	assert.Contains(t, props, "until")
	assert.Contains(t, props, "source")
	assert.Contains(t, props, "action")
}

func TestRestoreTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewRestoreTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_restore", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "path")
	assert.Contains(t, props, "version")
}

func TestDiffTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewDiffTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_diff", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "path")
	assert.Contains(t, props, "versionA")
	assert.Contains(t, props, "versionB")
}

func TestFilesTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewFilesTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_files", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "project")
	assert.Contains(t, props, "limit")
	assert.Contains(t, props, "offset")
}

func TestActivityTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewActivityTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_activity", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "project")
	assert.Contains(t, props, "source")
	assert.Contains(t, props, "limit")
}

func TestStatsTool_Definition(t *testing.T) {
	h := setupMCPHandler(t)
	tool, _ := NewStatsTool(h)
	require.NotNil(t, tool)
	assert.Equal(t, "changez_stats", tool.Name)
	props := tool.InputSchema.Properties
	require.NotNil(t, props)
	assert.Contains(t, props, "project")
}

// ========== Helpers ==========

func createProjectViaHandler(t *testing.T, h *handler.Handler) {
	t.Helper()
	body := `{"rootPath":"/home/user/proj","name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project failed: %d %s", w.Code, w.Body.String())
	}
}
