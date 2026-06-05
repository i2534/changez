package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeSession_Success(t *testing.T) {
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
						"content": "session analysis summary",
					},
				},
			},
		})
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "summarize", "{{range .Changes}}{{.FilePath}}: {{.Action}}{{end}}", "trends", 1024, 0.3, 5*time.Second)

	changes := []SessionChange{
		{FilePath: "src/main.go", Action: "create", Diff: "+package main", Message: "init", Timestamp: "2024-01-01T00:00:00Z"},
		{FilePath: "src/util.go", Action: "update", Diff: "+func helper()", Message: "add util", Timestamp: "2024-01-01T00:01:00Z"},
	}

	summary, err := p.AnalyzeSession(context.Background(), changes)

	require.NoError(t, err)
	assert.Equal(t, "session analysis summary", summary)
}

func TestAnalyzeSession_TemplateRendersChanges(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "summarize",
		"Changes:\n{{range .Changes}}File: {{.FilePath}} | Action: {{.Action}} | Diff: {{.Diff}} | Message: {{.Message}} | Time: {{.Timestamp}}\n{{end}}",
		"trends", 1024, 0.3, 5*time.Second)

	changes := []SessionChange{
		{FilePath: "a.go", Action: "create", Diff: "+new file", Message: "create a", Timestamp: "2024-01-01T00:00:00Z"},
		{FilePath: "b.go", Action: "update", Diff: "+changed", Message: "update b", Timestamp: "2024-01-01T00:01:00Z"},
	}

	_, _ = p.AnalyzeSession(context.Background(), changes)

	assert.Contains(t, receivedPrompt, "Changes:")
	assert.Contains(t, receivedPrompt, "File: a.go")
	assert.Contains(t, receivedPrompt, "Action: create")
	assert.Contains(t, receivedPrompt, "Diff: +new file")
	assert.Contains(t, receivedPrompt, "Message: create a")
	assert.Contains(t, receivedPrompt, "Time: 2024-01-01T00:00:00Z")
	assert.Contains(t, receivedPrompt, "File: b.go")
	assert.Contains(t, receivedPrompt, "Action: update")
	assert.Contains(t, receivedPrompt, "Diff: +changed")
	assert.Contains(t, receivedPrompt, "Message: update b")
}

func TestAnalyzeSession_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4o-mini", "summarize", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.AnalyzeSession(context.Background(), []SessionChange{
		{FilePath: "test.go", Action: "create", Diff: "+content"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error 500")
}

func TestAnalyzeSession_EmptyChanges(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "summarize", "Changes:{{range .Changes}}{{.FilePath}}{{end}}", "trends", 1024, 0.3, 5*time.Second)

	summary, err := p.AnalyzeSession(context.Background(), []SessionChange{})

	require.NoError(t, err)
	assert.Equal(t, "ok", summary)
	assert.Contains(t, receivedPrompt, "Changes:")
}

func TestAnalyzeSession_ContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slowServer.Close()

	p, _ := NewOpenAILike(slowServer.URL, "", "gpt-4o-mini", "summarize", "session", "trends", 1024, 0.3, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.AnalyzeSession(ctx, []SessionChange{
		{FilePath: "test.go", Action: "create", Diff: "+content"},
	})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "canceled") ||
		strings.Contains(err.Error(), "context") ||
		strings.Contains(err.Error(), "operation was canceled"),
		"error should be context-related: %v", err)
}

func TestAnalyzeSession_SessionTmplInvalid(t *testing.T) {
	_, err := NewOpenAILike("https://api.example.com", "key", "gpt-4", "summarize", "{{invalid", "trends", 1024, 0.3, 30*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse session prompt template")
}

func TestAnalyzeSession_WithApiKey(t *testing.T) {
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

	p, _ := NewOpenAILike(server.URL, "secret-key", "gpt-4", "summarize", "session", "trends", 1024, 0.3, 5*time.Second)

	_, _ = p.AnalyzeSession(context.Background(), []SessionChange{
		{FilePath: "test.go", Action: "create", Diff: "+content"},
	})

	assert.Equal(t, "Bearer secret-key", receivedAuth)
}

func TestAnalyzeSession_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": []}`))
	}))
	defer server.Close()

	p, _ := NewOpenAILike(server.URL, "", "gpt-4", "summarize", "session", "trends", 1024, 0.3, 5*time.Second)

	_, err := p.AnalyzeSession(context.Background(), []SessionChange{
		{FilePath: "test.go", Action: "create", Diff: "+content"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func setupSessionTest(t *testing.T) (*Worker, *db.DB, *storage.BlobStore, *storage.DeltaStore, string) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		database.Close()
	})

	_, err = database.Handle().Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

	_, err = database.Handle().Exec(`
		CREATE TABLE IF NOT EXISTS ai_sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL,
			project_name TEXT NOT NULL,
			summary      TEXT,
			model        TEXT,
			status       TEXT NOT NULL DEFAULT 'pending',
			created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	_, err = database.Handle().Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_sessions_id ON ai_sessions(session_id)`)
	require.NoError(t, err)
	_, err = database.Handle().Exec(`CREATE INDEX IF NOT EXISTS idx_ai_sessions_status ON ai_sessions(status)`)
	require.NoError(t, err)

	logger := slog.Default()
	cfg := &config.AICfg{
		Enabled:  true,
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Timeout:  "30s",
		Triggers: config.AITriggerCfg{
			OnSnapshot: true,
			BatchSize:  5,
		},
	}

	provider := &mockProvider{summary: "session analysis result", err: nil}
	w := NewWorker(provider, database, bs, ds, cfg, logger)

	return w, database, bs, ds, dir
}

func TestMarkSessionCompleted(t *testing.T) {
	w, database, _, _, _ := setupSessionTest(t)
	ctx := context.Background()

	_, err := database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'test-project', 'pending')",
		"ses-test-123")
	require.NoError(t, err)

	var sessionID int64
	var status string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT id, status FROM ai_sessions WHERE session_id = ?", "ses-test-123").Scan(&sessionID, &status)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)

	err = w.markSessionCompleted(ctx, sessionID, "analysis complete")
	require.NoError(t, err)

	var newStatus, summary, model string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary, model FROM ai_sessions WHERE id = ?", sessionID).Scan(&newStatus, &summary, &model)
	require.NoError(t, err)
	assert.Equal(t, "completed", newStatus)
	assert.Equal(t, "analysis complete", summary)
	assert.Equal(t, "gpt-4o-mini", model)
}

func TestMarkSessionFailed(t *testing.T) {
	w, database, _, _, _ := setupSessionTest(t)
	ctx := context.Background()

	_, err := database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'test-project', 'pending')",
		"ses-test-456")
	require.NoError(t, err)

	var sessionID int64
	err = database.Handle().QueryRowContext(ctx,
		"SELECT id FROM ai_sessions WHERE session_id = ?", "ses-test-456").Scan(&sessionID)
	require.NoError(t, err)

	err = w.markSessionFailed(ctx, sessionID, "API timeout")
	require.NoError(t, err)

	var newStatus, errorMsg string
	var retries int
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary, retries FROM ai_sessions WHERE id = ?", sessionID).Scan(&newStatus, &errorMsg, &retries)
	require.NoError(t, err)
	assert.Equal(t, "pending", newStatus)
	assert.Equal(t, "API timeout", errorMsg)
	assert.Equal(t, 1, retries)
}

func TestProcessPendingSessions_NoPending(t *testing.T) {
	w, _, _, _, _ := setupSessionTest(t)
	ctx := context.Background()

	w.processPendingSessions(ctx)
}

func TestProcessPendingSessions_Completes(t *testing.T) {
	w, database, bs, _, _ := setupSessionTest(t)
	ctx := context.Background()

	projectID, err := database.CreateProject(ctx, "session-test", "/session-test", "{}")
	require.NoError(t, err)

	fileID, err := database.UpsertFile(ctx, projectID, "/session-test/main.go")
	require.NoError(t, err)

	content := "package main\n\nfunc main() {}\n"
	hash, err := bs.Store([]byte(content))
	require.NoError(t, err)

	blobHash := hash
	sessionSid := "ses-completes-test"
	_, err = database.CreateVersion(ctx, fileID, "blob", &blobHash, nil, nil, "create", 1, sessionSid, "gpt-4", "init project")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'session-test', 'pending')",
		sessionSid)
	require.NoError(t, err)

	var statusBefore string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status FROM ai_sessions WHERE session_id = ?", sessionSid).Scan(&statusBefore)
	require.NoError(t, err)
	assert.Equal(t, "pending", statusBefore)

	w.processPendingSessions(ctx)

	var statusAfter, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_sessions WHERE session_id = ?", sessionSid).Scan(&statusAfter, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", statusAfter)
	assert.Equal(t, "session analysis result", summary)
}

func TestProcessPendingSessions_EmptySession(t *testing.T) {
	w, database, _, _, _ := setupSessionTest(t)
	ctx := context.Background()

	sessionSid := "ses-empty-test"
	_, err := database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'empty-project', 'pending')",
		sessionSid)
	require.NoError(t, err)

	w.processPendingSessions(ctx)

	var status, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_sessions WHERE session_id = ?", sessionSid).Scan(&status, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "本次会话无变更记录", summary)
}

func TestProcessPendingSessions_ProviderError(t *testing.T) {
	w, database, bs, _, _ := setupSessionTest(t)
	ctx := context.Background()

	testErr := errors.New("provider analysis failed")
	w.provider = &mockProvider{summary: "", err: testErr}

	projectID, err := database.CreateProject(ctx, "error-test", "/error-test", "{}")
	require.NoError(t, err)

	fileID, err := database.UpsertFile(ctx, projectID, "/error-test/main.go")
	require.NoError(t, err)

	content := "package main\n"
	hash, err := bs.Store([]byte(content))
	require.NoError(t, err)

	blobHash := hash
	sessionSid := "ses-error-test"
	_, err = database.CreateVersion(ctx, fileID, "blob", &blobHash, nil, nil, "create", 1, sessionSid, "gpt-4", "init")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'error-test', 'pending')",
		sessionSid)
	require.NoError(t, err)

	w.processPendingSessions(ctx)

	var status, errorMsg string
	var retries int
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary, retries FROM ai_sessions WHERE session_id = ?", sessionSid).Scan(&status, &errorMsg, &retries)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)
	assert.Contains(t, errorMsg, "provider analysis failed")
	assert.Equal(t, 1, retries)
}

func TestProcessPendingSessions_DeleteAction(t *testing.T) {
	w, database, bs, _, _ := setupSessionTest(t)
	ctx := context.Background()

	var capturedChanges []SessionChange
	w.provider = &sessionCapturingProvider{
		mockProvider: &mockProvider{summary: "delete analysis", err: nil},
		changes:      &capturedChanges,
	}

	projectID, err := database.CreateProject(ctx, "delete-test", "/delete-test", "{}")
	require.NoError(t, err)

	fileID, err := database.UpsertFile(ctx, projectID, "/delete-test/removed.go")
	require.NoError(t, err)

	sessionSid := "ses-delete-test"
	_, err = database.CreateVersion(ctx, fileID, "delete", nil, nil, nil, "delete", 1, sessionSid, "gpt-4", "remove file")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, 'delete-test', 'pending')",
		sessionSid)
	require.NoError(t, err)

	_ = bs

	w.processPendingSessions(ctx)

	require.Len(t, capturedChanges, 1)
	assert.Equal(t, "removed.go", capturedChanges[0].FilePath)
	assert.Equal(t, "delete", capturedChanges[0].Action)
	assert.Equal(t, "[文件被删除]", capturedChanges[0].Diff)
}

type sessionCapturingProvider struct {
	*mockProvider
	changes *[]SessionChange
}

func (s *sessionCapturingProvider) AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error) {
	*s.changes = changes
	return s.mockProvider.AnalyzeSession(ctx, changes)
}

func (s *sessionCapturingProvider) Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error) {
	return s.mockProvider.Summarize(ctx, diff, ctxInfo)
}

func (s *sessionCapturingProvider) AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error) {
	return s.mockProvider.AnalyzeTrends(ctx, stats)
}

func TestSessionChange_Structure(t *testing.T) {
	sc := SessionChange{
		FilePath:  "src/main.go",
		Action:    "update",
		Diff:      "--- original\n+++ modified\n-old\n+new",
		Message:   "refactor main function",
		Timestamp: "2024-06-01T10:00:00Z",
	}

	assert.Equal(t, "src/main.go", sc.FilePath)
	assert.Equal(t, "update", sc.Action)
	assert.Contains(t, sc.Diff, "--- original")
	assert.Contains(t, sc.Diff, "+++ modified")
	assert.Equal(t, "refactor main function", sc.Message)
	assert.Equal(t, "2024-06-01T10:00:00Z", sc.Timestamp)
}

func TestSessionChange_EmptyFields(t *testing.T) {
	sc := SessionChange{}

	assert.Empty(t, sc.FilePath)
	assert.Empty(t, sc.Action)
	assert.Empty(t, sc.Diff)
	assert.Empty(t, sc.Message)
	assert.Empty(t, sc.Timestamp)
}

func TestSessionChange_SliceOperations(t *testing.T) {
	changes := []SessionChange{
		{FilePath: "a.go", Action: "create"},
		{FilePath: "b.go", Action: "update"},
		{FilePath: "c.go", Action: "delete"},
	}

	assert.Len(t, changes, 3)
	assert.Equal(t, "a.go", changes[0].FilePath)
	assert.Equal(t, "update", changes[1].Action)
	assert.Equal(t, "delete", changes[2].Action)

	var emptyChanges []SessionChange
	assert.Empty(t, emptyChanges)
}
