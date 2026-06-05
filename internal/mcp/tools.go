package mcp

import (
	"context"
	"encoding/json"

	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/changez/changez/internal/handler"
)

func toolError(msg string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(msg), nil
}

func NewLogTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_log",
		mcp.WithDescription("View the version history of a file."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path of the file")),
		mcp.WithString("since", mcp.Description("Start time (ISO 8601)")),
		mcp.WithString("until", mcp.Description("End time (ISO 8601)")),
		mcp.WithString("source", mcp.Description("Filter by source (e.g. opencode, claudecode, cursor, human)")),
		mcp.WithString("action", mcp.Description("Filter by action"), mcp.Enum("create", "update", "delete")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
		mcp.WithNumber("offset", mcp.Description("Result offset (default 0)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return toolError(err.Error())
		}
		result, err := h.ProcessLog(ctx, path,
			req.GetString("since", ""),
			req.GetString("until", ""),
			req.GetString("source", ""),
			req.GetString("action", ""),
			int(req.GetInt("limit", 20)),
			int(req.GetInt("offset", 0)),
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewRestoreTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_restore",
		mcp.WithDescription("Restore a file to a specific version. Returns the full file content."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path of the file")),
		mcp.WithNumber("version", mcp.Required(), mcp.Description("Version ID to restore")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return toolError(err.Error())
		}
		version := int64(req.GetInt("version", 0))
		if version <= 0 {
			return toolError("version must be a positive integer")
		}
		result, err := h.ProcessRestore(ctx, path, version)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewDiffTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_diff",
		mcp.WithDescription("Show the unified diff between two versions of a file."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path of the file")),
		mcp.WithNumber("versionA", mcp.Required(), mcp.Description("First version ID")),
		mcp.WithNumber("versionB", mcp.Required(), mcp.Description("Second version ID")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return toolError(err.Error())
		}
		versionA := int64(req.GetInt("versionA", 0))
		versionB := int64(req.GetInt("versionB", 0))
		if versionA <= 0 || versionB <= 0 {
			return toolError("versionA and versionB must be positive integers")
		}
		result, err := h.ProcessDiff(ctx, path, versionA, versionB)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewFilesTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_files",
		mcp.WithDescription("List files in a project."),
		mcp.WithString("project", mcp.Required(), mcp.Description("Project name")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
		mcp.WithNumber("offset", mcp.Description("Result offset (default 0)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := req.RequireString("project")
		if err != nil {
			return toolError(err.Error())
		}
		files, err := h.ProcessFiles(ctx, project,
			int(req.GetInt("limit", 50)),
			int(req.GetInt("offset", 0)),
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(files)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewActivityTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_activity",
		mcp.WithDescription("Get recent file change activity across projects."),
		mcp.WithString("project", mcp.Description("Filter by project name")),
		mcp.WithString("source", mcp.Description("Filter by source (e.g. opencode, claudecode, cursor, human)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int(req.GetInt("limit", 20))
		if limit > 100 {
			limit = 100
		}
		activity, err := h.ProcessActivity(ctx,
			req.GetString("project", ""),
			req.GetString("source", ""),
			limit,
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(activity)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewStatsTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_stats",
		mcp.WithDescription("Get statistics about projects. Without project, returns global stats."),
		mcp.WithString("project", mcp.Description("Project name (optional, returns global stats if omitted)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stats, err := h.ProcessStats(ctx, req.GetString("project", ""))
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(stats)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewSummaryTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_summary",
		mcp.WithDescription("Get AI-generated summaries of file changes."),
		mcp.WithString("project", mcp.Description("Filter by project name")),
		mcp.WithString("path", mcp.Description("Filter by file path")),
		mcp.WithString("since", mcp.Description("Start time (ISO 8601)")),
		mcp.WithString("until", mcp.Description("End time (ISO 8601)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
		mcp.WithNumber("offset", mcp.Description("Result offset (default 0)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := h.ProcessSummary(ctx,
			req.GetString("project", ""),
			req.GetString("path", ""),
			req.GetString("since", ""),
			req.GetString("until", ""),
			int(req.GetInt("limit", 20)),
			int(req.GetInt("offset", 0)),
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewSessionTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_session",
		mcp.WithDescription("Get AI-generated session report for a coding session."),
		mcp.WithString("project", mcp.Description("Filter by project name")),
		mcp.WithString("sessionId", mcp.Required(), mcp.Description("Session ID to analyze")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := req.RequireString("sessionId")
		if err != nil {
			return toolError(err.Error())
		}
		result, err := h.ProcessSession(ctx,
			req.GetString("project", ""),
			sessionID,
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}

func NewTrendsTool(h *handler.Handler) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("changez_trends",
		mcp.WithDescription("Get AI-generated trend analysis for project changes."),
		mcp.WithString("project", mcp.Required(), mcp.Description("Project name")),
		mcp.WithString("since", mcp.Description("Start time (ISO 8601), defaults to 7 days ago")),
		mcp.WithString("until", mcp.Description("End time (ISO 8601), defaults to now")),
		mcp.WithNumber("topFiles", mcp.Description("Number of top files to include (default 10)")),
	)

	handlerFunc := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := req.RequireString("project")
		if err != nil {
			return toolError(err.Error())
		}
		result, err := h.ProcessTrends(ctx,
			project,
			req.GetString("since", ""),
			req.GetString("until", ""),
			int(req.GetInt("topFiles", 10)),
		)
		if err != nil {
			return toolError(err.Error())
		}
		resultJSON, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(resultJSON)), nil
	}

	return tool, handlerFunc
}
