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

	tool, handlerFunc := NewSnapshotTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewLogTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewRestoreTool(h)
	s.AddTool(tool, handlerFunc)

	tool, handlerFunc = NewDiffTool(h)
	s.AddTool(tool, handlerFunc)

	return server.NewStreamableHTTPServer(s)
}
