package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAILike(t *testing.T) {
	baseURL := "https://api.example.com"
	apiKey := "test-key"
	model := "gpt-4"
	prompt := "test {{.FilePath}} {{.Action}}"
	sessionPrompt := "session {{range .Changes}}{{.FilePath}}{{end}}"
	trendsPrompt := "trends {{.Period}}"
	maxTokens := 2048
	temperature := 0.7
	timeout := 30 * time.Second

	p, err := NewOpenAILike(baseURL, apiKey, model, prompt, sessionPrompt, trendsPrompt, maxTokens, temperature, timeout)
	require.NoError(t, err)

	assert.Equal(t, baseURL, p.baseURL)
	assert.Equal(t, apiKey, p.apiKey)
	assert.Equal(t, model, p.model)
	assert.Equal(t, maxTokens, p.maxTokens)
	assert.Equal(t, temperature, p.temperature)
	assert.Equal(t, timeout, p.timeout)
	assert.NotNil(t, p.client)
	assert.Equal(t, time.Duration(0), p.client.Timeout)
	assert.NotNil(t, p.summarizeTmpl)
	assert.NotNil(t, p.sessionTmpl)
	assert.NotNil(t, p.trendsTmpl)
}

func TestNewOpenAILike_InvalidTemplate(t *testing.T) {
	_, err := NewOpenAILike("https://api.example.com", "key", "gpt-4", "{{invalid", "session", "trends", 1024, 0.3, 30*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse summarize prompt template")
}

func TestActionDesc(t *testing.T) {
	assert.Equal(t, "创建", actionDesc("create"))
	assert.Equal(t, "修改", actionDesc("update"))
	assert.Equal(t, "删除", actionDesc("delete"))
	assert.Equal(t, "变更", actionDesc("unknown"))
}

func TestTemplate_Render(t *testing.T) {
	p, err := NewOpenAILike("http://localhost", "", "gpt-4", `文件: {{.FilePath}} 操作: {{.Action}} 来源: {{.Source}} {{.Message}} 变更: {{.Diff}}`, "session", "trends", 1024, 0.3, 5*time.Second)
	require.NoError(t, err)

	_, err = p.Summarize(context.Background(), "+new\n", SummaryContext{
		FilePath: "src/main.go",
		Action:   "create",
		Source:   "opencode",
		Message:  "初始创建",
	})
	// Expect error because localhost isn't running, but we can verify the template rendered
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http request")
}

func TestTemplate_Custom(t *testing.T) {
	var receivedPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		receivedPrompt = reqBody.Messages[0].Content
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer server.Close()

	p, err := NewOpenAILike(server.URL, "", "gpt-4", `Custom: {{.FilePath}} | {{.Action}} | {{.Source}} | {{.Diff}}`, "session", "trends", 1024, 0.3, 5*time.Second)
	require.NoError(t, err)

	_, _ = p.Summarize(context.Background(), "+new\n", SummaryContext{
		FilePath: "test.go",
		Action:   "update",
		Source:   "cursor",
	})

	assert.Contains(t, receivedPrompt, "Custom: test.go")
	assert.Contains(t, receivedPrompt, "修改")
	assert.Contains(t, receivedPrompt, "cursor")
	assert.Contains(t, receivedPrompt, "+new")
}

func TestSummarize_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var reqBody chatCompletionRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "gpt-4o-mini", reqBody.Model)
		assert.Len(t, reqBody.Messages, 1)
		assert.Equal(t, "user", reqBody.Messages[0].Role)
		assert.Contains(t, reqBody.Messages[0].Content, "test-file.go")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "test summary",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	summary, err := p.Summarize(context.Background(), "+new content", SummaryContext{
		FilePath: "test-file.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.NoError(t, err)
	assert.Equal(t, "test summary", summary)
}

func TestSummarize_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "update",
		Source:   "opencode",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error 500")
}

func TestSummarize_NoApiKey(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "ok",
					},
				},
			},
		})
	}))
	defer server.Close()

	// Empty apiKey
	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "Authorization header should be empty when apiKey is empty")
}

func TestSummarize_WithApiKey(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "ok",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "my-secret-key", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.NoError(t, err)
	assert.Equal(t, "Bearer my-secret-key", receivedAuth)
}

func TestSummarize_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": []}`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestSummarize_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestSummarize_RequestContainsCorrectModel(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		receivedModel = reqBody.Model

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "ok",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "custom-model", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, _ = p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	assert.Equal(t, "custom-model", receivedModel)
}

func TestSummarize_PromptContainsAllContextFields(t *testing.T) {
	var receivedPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		receivedPrompt = reqBody.Messages[0].Content

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "ok",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}} {{.Source}} {{.Message}} {{.Diff}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, _ = p.Summarize(context.Background(), "diff content here", SummaryContext{
		FilePath:  "src/app.ts",
		Action:    "update",
		Source:    "cursor",
		Model:     "claude-sonnet",
		Message:   "重构函数",
		Timestamp: "2024-06-01T10:00:00Z",
	})

	assert.Contains(t, receivedPrompt, "src/app.ts")
	assert.Contains(t, receivedPrompt, "修改")
	assert.Contains(t, receivedPrompt, "cursor")
	assert.Contains(t, receivedPrompt, "重构函数")
	assert.Contains(t, receivedPrompt, "diff content here")
}

func TestSummarize_URLConstruction(t *testing.T) {
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "ok",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	_, _ = p.Summarize(context.Background(), "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	assert.Equal(t, "/chat/completions", receivedPath)
}

func TestSummarize_ContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slowServer.Close()

	p, _ := NewOpenAILike(slowServer.URL, "", "gpt-4o-mini", "{{.FilePath}} {{.Action}}", "session", "trends", 1024, 0.3, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.Summarize(ctx, "+content", SummaryContext{
		FilePath: "test.go",
		Action:   "create",
		Source:   "opencode",
	})

	require.Error(t, err)
	// The error should be related to context cancellation
	assert.True(t, strings.Contains(err.Error(), "canceled") ||
		strings.Contains(err.Error(), "context") ||
		strings.Contains(err.Error(), "operation was canceled"),
		"error should be context-related: %v", err)
}
