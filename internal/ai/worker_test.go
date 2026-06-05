package ai

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	summary string
	err     error
}

func (m *mockProvider) Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error) {
	return m.summary, m.err
}

func (m *mockProvider) AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error) {
	return m.summary, m.err
}

func (m *mockProvider) AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error) {
	return m.summary, m.err
}

func setupWorkerTest(t *testing.T) (*Worker, *db.DB, *storage.BlobStore, *storage.DeltaStore, string) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		database.Close()
	})

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

	// Create ai_summaries table for tests
	_, err = database.Handle().Exec(`
		CREATE TABLE IF NOT EXISTS ai_summaries (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER UNIQUE NOT NULL,
			summary    TEXT,
			status     TEXT NOT NULL DEFAULT 'pending',
			model      TEXT,
			retries    INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE
		)
	`)
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

	provider := &mockProvider{summary: "test summary", err: nil}
	w := NewWorker(provider, database, bs, ds, cfg, logger)

	return w, database, bs, ds, dir
}

func createTestVersion(t *testing.T, db *db.DB, bs *storage.BlobStore, content string, action string) (int64, int64, int64) {
	t.Helper()
	ctx := context.Background()

	projectID, err := db.CreateProject(ctx, "test-project", "/test-project", "{}")
	require.NoError(t, err)

	fileID, err := db.UpsertFile(ctx, projectID, "/test-project/test.go")
	require.NoError(t, err)

	hash, err := bs.Store([]byte(content))
	require.NoError(t, err)

	blobHash := hash
	versionID, err := db.CreateVersion(ctx, fileID, "blob", &blobHash, nil, nil, action, 1, "", "", "")
	require.NoError(t, err)

	return projectID, fileID, versionID
}

func TestComputeUnifiedDiff(t *testing.T) {
	oldContent := "line1\nline2\nline3\n"
	newContent := "line1\nmodified_line2\nline3\nline4\n"

	diff := computeUnifiedDiff(oldContent, newContent)

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, "-line2")
	assert.Contains(t, diff, "+modified_line2")
	assert.Contains(t, diff, "+line4")
	assert.Contains(t, diff, " line1")
	assert.Contains(t, diff, " line3")
}

func TestComputeUnifiedDiff_NoChanges(t *testing.T) {
	content := "line1\nline2\nline3\n"

	diff := computeUnifiedDiff(content, content)

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, " line1")
	assert.Contains(t, diff, " line2")
	assert.Contains(t, diff, " line3")
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
			if line == "" {
				continue
			}
			if line == "--- original" || line == "+++ modified" {
				continue
			}
			assert.True(t, strings.HasPrefix(line, " "), "content line should start with space: %q", line)
		}
}

func TestComputeUnifiedDiff_EmptyToContent(t *testing.T) {
	diff := computeUnifiedDiff("", "new content\n")

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, "+new content")
}

func TestComputeUnifiedDiff_ContentToEmpty(t *testing.T) {
	diff := computeUnifiedDiff("old content\n", "")

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, "-old content")
}

func TestMarkCompleted(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, _, versionID := createTestVersion(t, database, bs, "content", "create")

	_, err := database.Handle().ExecContext(context.Background(),
		"INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')", versionID)
	require.NoError(t, err)

	var summaryID int64
	var status string
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT id, status FROM ai_summaries WHERE version_id = ?", versionID).Scan(&summaryID, &status)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)

	err = w.markCompleted(context.Background(), summaryID, "generated summary")
	require.NoError(t, err)

	var newStatus, summary string
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT status, summary FROM ai_summaries WHERE id = ?", summaryID).Scan(&newStatus, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", newStatus)
	assert.Equal(t, "generated summary", summary)
}

func TestMarkFailed(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, _, versionID := createTestVersion(t, database, bs, "content", "create")

	_, err := database.Handle().ExecContext(context.Background(),
		"INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')", versionID)
	require.NoError(t, err)

	var summaryID int64
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT id FROM ai_summaries WHERE version_id = ?", versionID).Scan(&summaryID)
	require.NoError(t, err)

	err = w.markFailed(context.Background(), summaryID, "API timeout")
	require.NoError(t, err)

	var newStatus, errorMsg string
	var retries int
	err = database.Handle().QueryRowContext(context.Background(),
		"SELECT status, summary, retries FROM ai_summaries WHERE id = ?", summaryID).Scan(&newStatus, &errorMsg, &retries)
	require.NoError(t, err)
	// First failure: retries becomes 1, status stays 'pending' (max_retries defaults to 3)
	assert.Equal(t, "pending", newStatus)
	assert.Equal(t, "API timeout", errorMsg)
	assert.Equal(t, 1, retries)
}

func TestRebuildContent_Blob(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	originalContent := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"

	_, _, versionID := createTestVersion(t, database, bs, originalContent, "create")

	ver, err := database.GetVersion(context.Background(), versionID)
	require.NoError(t, err)

	content, err := w.rebuildContent(context.Background(), ver)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(content))
}

func TestGetDiffForVersion_Create(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	content := "func hello() {}\n"

	_, fileID, versionID := createTestVersion(t, database, bs, content, "create")

	diff, err := w.getDiffForVersion(context.Background(), versionID, fileID)
	require.NoError(t, err)

	assert.Contains(t, diff, "[新文件创建]")
	assert.Contains(t, diff, content)
}

func TestGetDiffForVersion_Delete(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, fileID, versionID := createTestVersion(t, database, bs, "some content", "create")

	_, err := database.CreateVersion(context.Background(), fileID, "delete", nil, nil, &versionID, "delete", 1, "", "", "")
	require.NoError(t, err)

	diff, err := w.getDiffForVersion(context.Background(), versionID+1, fileID)
	require.NoError(t, err)

	assert.Equal(t, "[文件被删除]", diff)
}

func TestGetDiffForVersion_WrongFileID(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, _, versionID := createTestVersion(t, database, bs, "content", "create")

	_, err := w.getDiffForVersion(context.Background(), versionID, 99999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to file")
}

func TestGetDiffForVersion_Update(t *testing.T) {
	w, database, bs, _, _ := setupWorkerTest(t)

	_, fileID, versionID := createTestVersion(t, database, bs, "line1\nline2\n", "create")

	content2 := "line1\nmodified_line2\nline3\n"
	hash2, err := bs.Store([]byte(content2))
	require.NoError(t, err)

	blobHash := hash2
	_, err = database.CreateVersion(context.Background(), fileID, "blob", &blobHash, nil, &versionID, "update", 1, "", "", "")
	require.NoError(t, err)

	diff, err := w.getDiffForVersion(context.Background(), versionID+1, fileID)
	require.NoError(t, err)

	assert.NotContains(t, diff, "[新文件创建]")
	assert.NotContains(t, diff, "[文件被删除]")
	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, "-line2")
	assert.Contains(t, diff, "+modified_line2")
	assert.Contains(t, diff, "+line3")
}

func TestWorker_NewWorker(t *testing.T) {
	w, _, _, _, _ := setupWorkerTest(t)

	assert.NotNil(t, w)
	assert.NotNil(t, w.provider)
	assert.NotNil(t, w.db)
	assert.NotNil(t, w.blobStore)
	assert.NotNil(t, w.deltaStore)
	assert.NotNil(t, w.cfg)
	assert.NotNil(t, w.logger)
}

func TestComputeUnifiedDiff_SingleCharChange(t *testing.T) {
	diff := computeUnifiedDiff("abc", "axc")

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
}

func TestComputeUnifiedDiff_MultiLine(t *testing.T) {
	old := strings.Repeat("old line\n", 100)
	new := strings.Repeat("new line\n", 100)

	diff := computeUnifiedDiff(old, new)

	assert.Contains(t, diff, "--- original")
	assert.Contains(t, diff, "+++ modified")
	assert.Contains(t, diff, "-old line")
	assert.Contains(t, diff, "+new line")
}

func TestMarkCompleted_NonExistent(t *testing.T) {
	w, _, _, _, _ := setupWorkerTest(t)

	err := w.markCompleted(context.Background(), 99999, "summary")
	require.NoError(t, err)
}

func TestMarkFailed_NonExistent(t *testing.T) {
	w, _, _, _, _ := setupWorkerTest(t)

	err := w.markFailed(context.Background(), 99999, "error")
	require.NoError(t, err)
}
