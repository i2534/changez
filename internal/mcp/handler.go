package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/server"

	"github.com/changez/changez/internal/handler"
)

func NewMCPHandler(h *handler.Handler) http.Handler {
	s := server.NewMCPServer("changez", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	// changez_snapshot 已移除——数据生产由 hook 脚本通过 HTTP API 完成，
	// AI agent 不需要手动提交快照。如需恢复可在此重新注册 NewSnapshotTool。
	tool, handlerFunc := NewLogTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewRestoreTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewDiffTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewFilesTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewActivityTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewStatsTool(h)
	s.AddTool(tool, handlerFunc)

	return server.NewStreamableHTTPServer(s)
}
