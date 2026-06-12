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

type e2eMockProvider struct{}

func (m *e2eMockProvider) Summarize(ctx context.Context, diff string, ctxInfo SummaryContext) (string, error) {
	if strings.Contains(diff, "[文件被删除]") {
		return "[文件被删除]", nil
	}
	if strings.Contains(diff, "[新文件创建]") {
		return "新文件创建: " + ctxInfo.FilePath, nil
	}
	return "E2E test summary for " + ctxInfo.Action + " on " + ctxInfo.FilePath, nil
}

func (m *e2eMockProvider) AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error) {
	return "E2E session analysis", nil
}

func (m *e2eMockProvider) AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error) {
	return "E2E trends analysis", nil
}

// TestE2E_SnapshotToSummary_FullFlow verifies the complete AI summary pipeline:
// snapshot → INSERT pending → Worker polls → Provider.Summarize() → UPDATE completed → query summary
func TestE2E_SnapshotToSummary_FullFlow(t *testing.T) {
	ctx := context.Background()

	// ===== 1. Setup =====
	dir := t.TempDir()

	database, err := db.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Enable WAL mode to allow concurrent read/write within processPending
	_, err = database.Handle().Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

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

	provider := &e2eMockProvider{}

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

	logger := slog.Default()
	worker := NewWorker(provider, database, bs, ds, cfg, logger)

	// ===== 2. Create Test Data =====
	projectID, err := database.CreateProject(ctx, "e2e-test", "/tmp/e2e", "{}")
	require.NoError(t, err)

	sourceID, err := database.GetSourceIDByName(ctx, "opencode")
	require.NoError(t, err)
	assert.Equal(t, int64(1), sourceID)

	fileID, err := database.UpsertFile(ctx, projectID, "/tmp/e2e/test.go")
	require.NoError(t, err)

	initialContent := "package main\n"
	hash1, err := bs.Store([]byte(initialContent))
	require.NoError(t, err)

	blobHash1 := hash1
	versionID1, err := database.CreateVersion(ctx, fileID, "blob", &blobHash1, nil, nil, "create", sourceID, "", "", "")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')", versionID1)
	require.NoError(t, err)

	// ===== 3. Verify Pending State =====
	var summaryID1 int64
	var status1 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT id, status FROM ai_summaries WHERE version_id = ?", versionID1).Scan(&summaryID1, &status1)
	require.NoError(t, err)
	assert.Equal(t, "pending", status1)

	// ===== 4. Process Pending (simulate Worker tick) =====
	worker.processPending(ctx)

	// ===== 5. Verify Completed State (create) =====
	var completedStatus1, completedSummary1 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_summaries WHERE id = ?", summaryID1).Scan(&completedStatus1, &completedSummary1)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedStatus1)
	assert.Contains(t, completedSummary1, "新文件创建")
	assert.Contains(t, completedSummary1, "test.go")

	// ===== 6. Test Update Flow =====
	modifiedContent := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n\tfmt.Println(\"world\")\n\tfor i := 0; i < 10; i++ {\n\t\tfmt.Println(i)\n\t}\n}\n"
	hash2, err := bs.Store([]byte(modifiedContent))
	require.NoError(t, err)

	blobHash2 := hash2
	versionID2, err := database.CreateVersion(ctx, fileID, "blob", &blobHash2, nil, &versionID1, "update", sourceID, "", "", "")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')", versionID2)
	require.NoError(t, err)

	var summaryID2 int64
	var status2 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT id, status FROM ai_summaries WHERE version_id = ?", versionID2).Scan(&summaryID2, &status2)
	require.NoError(t, err)
	assert.Equal(t, "pending", status2)

	worker.processPending(ctx)

	var completedStatus2, completedSummary2 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_summaries WHERE id = ?", summaryID2).Scan(&completedStatus2, &completedSummary2)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedStatus2)
	assert.Contains(t, completedSummary2, "E2E test summary for update on test.go")

	// ===== 7. Test Delete Flow =====
	versionID3, err := database.CreateVersion(ctx, fileID, "delete", nil, nil, &versionID2, "delete", sourceID, "", "", "")
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_summaries (version_id, status) VALUES (?, 'pending')", versionID3)
	require.NoError(t, err)

	var summaryID3 int64
	var status3 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT id, status FROM ai_summaries WHERE version_id = ?", versionID3).Scan(&summaryID3, &status3)
	require.NoError(t, err)
	assert.Equal(t, "pending", status3)

	worker.processPending(ctx)

	var completedStatus3, completedSummary3 string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, summary FROM ai_summaries WHERE id = ?", summaryID3).Scan(&completedStatus3, &completedSummary3)
	require.NoError(t, err)
	assert.Equal(t, "completed", completedStatus3)
	assert.Equal(t, "[文件被删除]", completedSummary3)

	// ===== 8. Final Verification =====
	rows, err := database.Handle().QueryContext(ctx, `
		SELECT v.id, v.action, a.status, a.summary
		FROM versions v
		JOIN ai_summaries a ON v.id = a.version_id
		WHERE v.file_id = ?
		ORDER BY v.id
	`, fileID)
	require.NoError(t, err)
	defer rows.Close()

	var results []struct {
		VersionID int64
		Action    string
		Status    string
		Summary   string
	}
	for rows.Next() {
		var r struct {
			VersionID int64
			Action    string
			Status    string
			Summary   string
		}
		require.NoError(t, rows.Scan(&r.VersionID, &r.Action, &r.Status, &r.Summary))
		results = append(results, r)
	}
	require.NoError(t, rows.Err())

	assert.Len(t, results, 3)
	assert.Equal(t, "create", results[0].Action)
	assert.Equal(t, "completed", results[0].Status)
	assert.Contains(t, results[0].Summary, "新文件创建")

	assert.Equal(t, "update", results[1].Action)
	assert.Equal(t, "completed", results[1].Status)
	assert.Contains(t, results[1].Summary, "update")

	assert.Equal(t, "delete", results[2].Action)
	assert.Equal(t, "completed", results[2].Status)
	assert.Equal(t, "[文件被删除]", results[2].Summary)
}
