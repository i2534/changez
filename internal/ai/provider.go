// Package ai 提供 AI 摘要生成能力。
// 通过 Provider 接口抽象 LLM 调用，支持 OpenAI 兼容接口。
package ai

import "context"

// Provider AI 服务提供商接口。
type Provider interface {
	// Summarize 生成单个 diff 变更摘要。
	Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error)

	// AnalyzeSession 生成会话级变更报告。
	AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error)

	// AnalyzeTrends 生成趋势分析报告。
	AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error)
}

// SummaryContext 生成摘要所需的上下文信息。
type SummaryContext struct {
	FilePath  string
	Action    string // create/update/delete
	Source    string // opencode/claudecode/cursor
	Model     string
	Message   string
	Timestamp string
}

// SessionChange 会话级分析所需的单次变更信息。
type SessionChange struct {
	FilePath  string
	Action    string
	Diff      string
	Model     string
	Message   string
	Timestamp string
}

// TrendStats 趋势分析所需的统计数据。
type TrendStats struct {
	Period          string
	TotalFiles      int
	TotalChanges    int
	SourceBreakdown map[string]int
	TopFiles        []FileChangeCount
}

// FileChangeCount 文件变更次数统计。
type FileChangeCount struct {
	FilePath string
	Count    int
}


