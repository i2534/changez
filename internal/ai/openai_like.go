package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// OpenAILike 兼容 OpenAI Chat Completions API 的 Provider。
// 支持 OpenAI、llama-server、Ollama --api 等。
type OpenAILike struct {
	baseURL        string
	apiKey         string
	model          string
	maxTokens      int
	temperature    float64
	summarizeTmpl  *template.Template
	sessionTmpl    *template.Template
	trendsTmpl     *template.Template
	timeout        time.Duration
	client         *http.Client
}

// NewOpenAILike 创建 OpenAI 兼容的 Provider。
func NewOpenAILike(baseURL, apiKey, model, prompt, sessionPrompt, trendsPrompt string, maxTokens int, temperature float64, timeout time.Duration) (*OpenAILike, error) {
	summarizeTmpl, err := template.New("summarize").Parse(prompt)
	if err != nil {
		return nil, fmt.Errorf("parse summarize prompt template: %w", err)
	}
	sessionTmpl, err := template.New("session").Parse(sessionPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse session prompt template: %w", err)
	}
	trendsTmpl, err := template.New("trends").Parse(trendsPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse trends prompt template: %w", err)
	}
	return &OpenAILike{
		baseURL:        baseURL,
		apiKey:         apiKey,
		model:          model,
		maxTokens:      maxTokens,
		temperature:    temperature,
		summarizeTmpl:  summarizeTmpl,
		sessionTmpl:    sessionTmpl,
		trendsTmpl:     trendsTmpl,
		timeout:        timeout,
		client: &http.Client{},
	}, nil
}

func (o *OpenAILike) Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error) {
	var buf bytes.Buffer
	if err := o.summarizeTmpl.Execute(&buf, struct {
		FilePath string
		Action   string
		Source   string
		Message  string
		Diff     string
	}{
		FilePath: ctxInfo.FilePath,
		Action:   actionDesc(ctxInfo.Action),
		Source:   ctxInfo.Source,
		Message:  ctxInfo.Message,
		Diff:     diff,
	}); err != nil {
		return "", fmt.Errorf("execute prompt template: %w", err)
	}
	return o.doChatCompletion(ctx, buf.String())
}

func (o *OpenAILike) AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error) {
	var buf bytes.Buffer
	if err := o.sessionTmpl.Execute(&buf, struct {
		Changes []SessionChange
	}{
		Changes: changes,
	}); err != nil {
		return "", fmt.Errorf("execute session prompt template: %w", err)
	}
	return o.doChatCompletion(ctx, buf.String())
}

func (o *OpenAILike) AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error) {
	var buf bytes.Buffer
	if err := o.trendsTmpl.Execute(&buf, struct {
		Period          string
		TotalFiles      int
		TotalChanges    int
		SourceBreakdown []SourceCount
		TopFiles        []FileChangeCount
	}{
		Period:       stats.Period,
		TotalFiles:   stats.TotalFiles,
		TotalChanges: stats.TotalChanges,
		SourceBreakdown: func() []SourceCount {
			var result []SourceCount
			for source, count := range stats.SourceBreakdown {
				result = append(result, SourceCount{Source: source, Count: count})
			}
			return result
		}(),
		TopFiles: stats.TopFiles,
	}); err != nil {
		return "", fmt.Errorf("execute trends prompt template: %w", err)
	}
	return o.doChatCompletion(ctx, buf.String())
}

func (o *OpenAILike) doChatCompletion(ctx context.Context, prompt string) (string, error) {
	reqBody := chatCompletionRequest{
		Model: o.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   o.maxTokens,
		Temperature: o.temperature,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(o.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", fmt.Errorf("API error %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("empty content from API")
	}
	return content, nil
}

type SourceCount struct {
	Source string
	Count  int
}

func actionDesc(action string) string {
	switch action {
	case "create":
		return "创建"
	case "update":
		return "修改"
	case "delete":
		return "删除"
	default:
		return "变更"
	}
}

// OpenAI Chat Completions API 请求/响应结构。

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

