package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeTrends_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var reqBody chatCompletionRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "gpt-4o-mini", reqBody.Model)
		assert.Len(t, reqBody.Messages, 1)
		assert.Equal(t, "user", reqBody.Messages[0].Role)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "趋势分析完成",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "{{.FilePath}}", "session", "{{.Period}}", 1024, 0.3, 5*time.Second)

	summary, err := p.AnalyzeTrends(context.Background(), TrendStats{
		Period:       "2024-01-01 ~ 2024-01-31",
		TotalFiles:   10,
		TotalChanges: 50,
		SourceBreakdown: map[string]int{
			"opencode": 30,
			"cursor":   20,
		},
		TopFiles: []FileChangeCount{
			{FilePath: "main.go", Count: 15},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "趋势分析完成", summary)
}

func TestAnalyzeTrends_TemplateRendersStats(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "{{.FilePath}}", "session",
		"Period: {{.Period}} | Files: {{.TotalFiles}} | Changes: {{.TotalChanges}} | Sources: {{range .SourceBreakdown}}{{.Source}}={{.Count}} {{end}} | Top: {{range .TopFiles}}{{.FilePath}}={{.Count}} {{end}}",
		1024, 0.3, 5*time.Second)

	_, _ = p.AnalyzeTrends(context.Background(), TrendStats{
		Period:       "2024-01 ~ 2024-02",
		TotalFiles:   5,
		TotalChanges: 20,
		SourceBreakdown: map[string]int{
			"opencode":   10,
			"claudecode": 5,
			"cursor":     5,
		},
		TopFiles: []FileChangeCount{
			{FilePath: "a.go", Count: 8},
			{FilePath: "b.go", Count: 3},
		},
	})

	assert.Contains(t, receivedPrompt, "2024-01 ~ 2024-02")
	assert.Contains(t, receivedPrompt, "Files: 5")
	assert.Contains(t, receivedPrompt, "Changes: 20")
	assert.Contains(t, receivedPrompt, "opencode=10")
	assert.Contains(t, receivedPrompt, "claudecode=5")
	assert.Contains(t, receivedPrompt, "cursor=5")
	assert.Contains(t, receivedPrompt, "a.go=8")
	assert.Contains(t, receivedPrompt, "b.go=3")
}

func TestAnalyzeTrends_SourceBreakdownConversion(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "{{.FilePath}}", "session",
		"{{range .SourceBreakdown}}{{.Source}}:{{.Count}} {{end}}",
		1024, 0.3, 5*time.Second)

	stats := TrendStats{
		SourceBreakdown: map[string]int{
			"opencode":   30,
			"cursor":     20,
			"claudecode": 50,
		},
	}

	_, _ = p.AnalyzeTrends(context.Background(), stats)

	assert.Contains(t, receivedPrompt, "opencode:30")
	assert.Contains(t, receivedPrompt, "cursor:20")
	assert.Contains(t, receivedPrompt, "claudecode:50")
}

func TestAnalyzeTrends_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "{{.FilePath}}", "session", "{{.Period}}", 1024, 0.3, 5*time.Second)

	_, err := p.AnalyzeTrends(context.Background(), TrendStats{
		Period:       "2024-01 ~ 2024-02",
		TotalChanges: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error 500")
}

func TestAnalyzeTrends_EmptyStats(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "{{.FilePath}}", "session",
		"Period: {{.Period}} | Files: {{.TotalFiles}} | Changes: {{.TotalChanges}}",
		1024, 0.3, 5*time.Second)

	_, err := p.AnalyzeTrends(context.Background(), TrendStats{})
	require.NoError(t, err)

	assert.Contains(t, receivedPrompt, "Files: 0")
	assert.Contains(t, receivedPrompt, "Changes: 0")
}

func TestAnalyzeTrends_ContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slowServer.Close()

	p, _ := NewOpenAILike(slowServer.URL, "", "gpt-4", "{{.FilePath}}", "session", "{{.Period}}", 1024, 0.3, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.AnalyzeTrends(ctx, TrendStats{
		Period: "2024-01 ~ 2024-02",
	})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "canceled") ||
		strings.Contains(err.Error(), "context") ||
		strings.Contains(err.Error(), "operation was canceled"),
		"error should be context-related: %v", err)
}

func TestAnalyzeTrends_TrendsTmplInvalid(t *testing.T) {
	_, err := NewOpenAILike("http://localhost", "", "gpt-4", "{{.FilePath}}", "session", "{{invalid", 1024, 0.3, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse trends prompt template")
}

func TestMarkTrendCompleted(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, _, _ = createTestVersion(t, database, bs, "content", "create")

	_, err := database.Handle().ExecContext(context.Background(),
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'pending')",
		"test-project", "2024-01-01", "2024-01-31")
	require.NoError(t, err)

	var trendID int64
	var status string
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT id, status FROM ai_trends WHERE project_name = ?", "test-project").Scan(&trendID, &status)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)

	err = w.markTrendCompleted(context.Background(), trendID, "trend summary here")
	require.NoError(t, err)

	var newStatus, summary, model string
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT status, summary, model FROM ai_trends WHERE id = ?", trendID).Scan(&newStatus, &summary, &model)
	require.NoError(t, err)
	assert.Equal(t, "completed", newStatus)
	assert.Equal(t, "trend summary here", summary)
	assert.Equal(t, "gpt-4o-mini", model)
}

func TestMarkTrendFailed(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, _, _ = createTestVersion(t, database, bs, "content", "create")

	_, err := database.Handle().ExecContext(context.Background(),
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'pending')",
		"test-project", "2024-01-01", "2024-01-31")
	require.NoError(t, err)

	var trendID int64
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT id FROM ai_trends WHERE project_name = ?", "test-project").Scan(&trendID)
	require.NoError(t, err)

	err = w.markTrendFailed(context.Background(), trendID, "API timeout")
	require.NoError(t, err)

	var newStatus, errorMsg string
	var retries int
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT status, summary, retries FROM ai_trends WHERE id = ?", trendID).Scan(&newStatus, &errorMsg, &retries)
	require.NoError(t, err)
	assert.Equal(t, "pending", newStatus)
	assert.Equal(t, "API timeout", errorMsg)
	assert.Equal(t, 1, retries)
}

func TestProcessPendingTrends_NoPending(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	mock := &mockProviderWithCapture{
		summary: "summary",
	}
	w.provider = mock

	_, _, _ = createTestVersion(t, database, bs, "content", "create")

	_, err := database.Handle().ExecContext(context.Background(),
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'completed')",
		"test-project", "2024-01-01", "2024-01-31")
	require.NoError(t, err)

	w.processPendingTrends(context.Background())

	assert.False(t, mock.called)
}

func TestProcessPendingTrends_Completes(t *testing.T) {
	ctx := context.Background()
	w, database, bs, _, _ := setupWorkerTest(t)

	mock := &mockProviderWithCapture{
		summary: "trend analysis complete",
	}
	w.provider = mock

	projectID, err := database.CreateProject(ctx, "trend-test", "/trend-test", "{}")
	require.NoError(t, err)

	fileID1, err := database.UpsertFile(ctx, projectID, "/trend-test/a.go")
	require.NoError(t, err)
	fileID2, err := database.UpsertFile(ctx, projectID, "/trend-test/b.go")
	require.NoError(t, err)

	hash1, err := bs.Store([]byte("package a"))
	require.NoError(t, err)
	hash2, err := bs.Store([]byte("package b"))
	require.NoError(t, err)

	sourceID, err := database.GetSourceIDByName(ctx, "opencode")
	require.NoError(t, err)

	periodStart := "2024-06-01T00:00:00Z"
	periodEnd := "2024-06-30T23:59:59Z"

	blobHash := hash1
	_, err = database.CreateVersion(ctx, fileID1, "blob", &blobHash, nil, nil, "create", sourceID, "", "", "")
	require.NoError(t, err)

	blobHash = hash2
	_, err = database.CreateVersion(ctx, fileID2, "blob", &blobHash, nil, nil, "create", sourceID, "", "", "")
	require.NoError(t, err)

	blobHash = hash1
	_, err = database.CreateVersion(ctx, fileID1, "blob", &blobHash, nil, nil, "update", sourceID, "", "", "")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"UPDATE versions SET changed_at = ? WHERE file_id = ?", periodStart, fileID1)
	require.NoError(t, err)
	_, err = database.Handle().ExecContext(ctx,
		"UPDATE versions SET changed_at = ? WHERE file_id = ?", periodEnd, fileID2)
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'pending')",
		"trend-test", periodStart, periodEnd)
	require.NoError(t, err)

	w.processPendingTrends(ctx)

	var status, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_trends WHERE project_name = ?", "trend-test").Scan(&status, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "trend analysis complete", summary)

	assert.True(t, mock.called)
	assert.Equal(t, 3, mock.capturedStats.TotalChanges)
	assert.Equal(t, 2, mock.capturedStats.TotalFiles)
	assert.Equal(t, 3, mock.capturedStats.SourceBreakdown["opencode"])
	assert.Contains(t, mock.capturedStats.Period, periodStart)
	assert.Contains(t, mock.capturedStats.Period, periodEnd)
	assert.Len(t, mock.capturedStats.TopFiles, 2)
}

func TestTrendStats_Structure(t *testing.T) {
	stats := TrendStats{
		Period:       "2024-Q1",
		TotalFiles:   42,
		TotalChanges: 150,
		SourceBreakdown: map[string]int{
			"opencode":   80,
			"claudecode": 40,
			"cursor":     30,
		},
		TopFiles: []FileChangeCount{
			{FilePath: "main.go", Count: 50},
			{FilePath: "util.go", Count: 30},
		},
	}

	assert.Equal(t, "2024-Q1", stats.Period)
	assert.Equal(t, 42, stats.TotalFiles)
	assert.Equal(t, 150, stats.TotalChanges)
	assert.Equal(t, map[string]int{
		"opencode":   80,
		"claudecode": 40,
		"cursor":     30,
	}, stats.SourceBreakdown)
	assert.Len(t, stats.TopFiles, 2)
	assert.Equal(t, "main.go", stats.TopFiles[0].FilePath)
	assert.Equal(t, 50, stats.TopFiles[0].Count)
}

func TestFileChangeCount_Structure(t *testing.T) {
	fcc := FileChangeCount{
		FilePath: "src/components/Button.tsx",
		Count:    25,
	}

	assert.Equal(t, "src/components/Button.tsx", fcc.FilePath)
	assert.Equal(t, 25, fcc.Count)
}

type mockProviderWithCapture struct {
	summary       string
	err           error
	called        bool
	capturedStats TrendStats
	capturedMu    sync.Mutex
}

func (m *mockProviderWithCapture) Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error) {
	return m.summary, m.err
}

func (m *mockProviderWithCapture) AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error) {
	return m.summary, m.err
}

func (m *mockProviderWithCapture) AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error) {
	m.capturedMu.Lock()
	m.called = true
	m.capturedStats = stats
	m.capturedMu.Unlock()
	return m.summary, m.err
}
