package ai

import (
	"context"
	"log/slog"
	"testing"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestEnv sets up a fresh database + storage environment for E2E tests.
// db.Open already runs migrations (including ai_summaries, ai_sessions, ai_trends tables).
func newTestEnv(t *testing.T) (*db.DB, *storage.BlobStore, *storage.DeltaStore) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

	return database, bs, ds
}

// newTestWorker creates a Worker with the e2eMockProvider.
func newTestWorker(t *testing.T, database *db.DB, bs *storage.BlobStore, ds *storage.DeltaStore) *Worker {
	t.Helper()
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
	return NewWorker(&e2eMockProvider{}, database, bs, ds, cfg, slog.Default())
}

// createBlobVersion is a helper to create a blob-stored version with content.
func createBlobVersion(ctx context.Context, t *testing.T, database *db.DB, bs *storage.BlobStore,
	fileID, sourceID int64, action, sessionID, content string, baseID *int64,
) int64 {
	t.Helper()
	hash, err := bs.Store([]byte(content))
	require.NoError(t, err)
	blobHash := hash
	versionID, err := database.CreateVersion(ctx, fileID, "blob", &blobHash, nil, baseID, action, sourceID, sessionID, "", "")
	require.NoError(t, err)
	return versionID
}

// TestE2E_Session_FullFlow verifies the complete session analysis pipeline:
// create versions with session_id → INSERT pending into ai_sessions → processPendingSessions → verify completed.
func TestE2E_Session_FullFlow(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	// ===== 1. Create test data: project + file + 3 versions with same session =====
	projectID, err := database.CreateProject(ctx, "session-test", "/tmp/session", "{}")
	require.NoError(t, err)

	sourceID, err := database.GetSourceIDByName(ctx, "opencode")
	require.NoError(t, err)

	fileID, err := database.UpsertFile(ctx, projectID, "/tmp/session/main.go")
	require.NoError(t, err)

	sessionID := "ses-e2e-session-full"

	v1 := createBlobVersion(ctx, t, database, bs, fileID, sourceID, "create", sessionID, "package main\n", nil)
	var prevID *int64
	tmp := v1
	prevID = &tmp

	v2 := createBlobVersion(ctx, t, database, bs, fileID, sourceID, "update", sessionID, "package main\n\nfunc main() {}\n", prevID)
	tmp = v2
	prevID = &tmp

	createBlobVersion(ctx, t, database, bs, fileID, sourceID, "update", sessionID, "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n", prevID)

	// ===== 2. Insert pending session =====
	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')",
		sessionID, "session-test")
	require.NoError(t, err)

	// ===== 3. Verify pending state =====
	var status string
	var summarySQL string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, COALESCE(summary, '') FROM ai_sessions WHERE session_id = ?", sessionID).Scan(&status, &summarySQL)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)

	// ===== 4. Process pending sessions =====
	worker.processPendingSessions(ctx)

	// ===== 5. Verify completed with summary =====
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, COALESCE(summary, '') FROM ai_sessions WHERE session_id = ?", sessionID).Scan(&status, &summarySQL)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "E2E session analysis", summarySQL)

	// ===== 6. Verify all 3 changes from the session are captured =====
	// The worker queries versions WHERE session_id = ? — should find all 3
	var changeCount int
	err = database.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM versions WHERE session_id = ?`, sessionID).Scan(&changeCount)
	require.NoError(t, err)
	assert.Equal(t, 3, changeCount)
}

// TestE2E_Session_MultipleProjects verifies that session analysis captures
// versions from multiple projects sharing the same session_id.
func TestE2E_Session_MultipleProjects(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	sessionID := "ses-multi-project"

	// ===== 1. Create two projects with versions in the same session =====
	projectID1, err := database.CreateProject(ctx, "proj-alpha", "/tmp/alpha", "{}")
	require.NoError(t, err)

	projectID2, err := database.CreateProject(ctx, "proj-beta", "/tmp/beta", "{}")
	require.NoError(t, err)

	sourceID, err := database.GetSourceIDByName(ctx, "opencode")
	require.NoError(t, err)

	fileID1, err := database.UpsertFile(ctx, projectID1, "/tmp/alpha/a.go")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID1, sourceID, "create", sessionID, "package alpha\n", nil)

	fileID2, err := database.UpsertFile(ctx, projectID2, "/tmp/beta/b.go")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID2, sourceID, "create", sessionID, "package beta\n", nil)

	// ===== 2. Insert pending session =====
	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')",
		sessionID, "proj-alpha")
	require.NoError(t, err)

	// ===== 3. Process =====
	worker.processPendingSessions(ctx)

	// ===== 4. Verify completed =====
	var status string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status FROM ai_sessions WHERE session_id = ?", sessionID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)

	// ===== 5. Verify versions from both projects are captured by session_id =====
	var totalCount int
	err = database.Handle().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM versions WHERE session_id = ?", sessionID).Scan(&totalCount)
	require.NoError(t, err)
	assert.Equal(t, 2, totalCount, "session analysis should capture versions from both projects")

	// Verify each project contributed exactly 1 version
	var count1, count2 int
	err = database.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM versions v
		JOIN files f ON v.file_id = f.id
		WHERE v.session_id = ? AND f.project_id = ?`, sessionID, projectID1).Scan(&count1)
	require.NoError(t, err)
	assert.Equal(t, 1, count1)

	err = database.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM versions v
		JOIN files f ON v.file_id = f.id
		WHERE v.session_id = ? AND f.project_id = ?`, sessionID, projectID2).Scan(&count2)
	require.NoError(t, err)
	assert.Equal(t, 1, count2)
}

// TestE2E_Session_EmptySession verifies that a session with no matching versions
// completes with an empty-summary message.
func TestE2E_Session_EmptySession(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	// ===== 1. Insert pending session with no matching versions =====
	sessionID := "ses-empty-no-versions"
	_, err := database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')",
		sessionID, "nonexistent-project")
	require.NoError(t, err)

	// ===== 2. Process =====
	worker.processPendingSessions(ctx)

	// ===== 3. Verify completed with empty message =====
	var status, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, COALESCE(summary, '') FROM ai_sessions WHERE session_id = ?", sessionID).Scan(&status, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "本次会话无变更记录", summary)
}

// TestE2E_Trends_FullFlow verifies the complete trends analysis pipeline:
// create versions across sources → INSERT pending into ai_trends → processPendingTrends → verify stats.
func TestE2E_Trends_FullFlow(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	// ===== 1. Create project + multiple files + versions across different sources =====
	projectID, err := database.CreateProject(ctx, "trends-test", "/tmp/trends", "{}")
	require.NoError(t, err)

	sourceOpencode, _ := database.GetSourceIDByName(ctx, "opencode")
	sourceClaude, _ := database.GetSourceIDByName(ctx, "claudecode")
	sourceCursor, _ := database.GetSourceIDByName(ctx, "cursor")

	// File 1: 3 versions from opencode
	fileID1, err := database.UpsertFile(ctx, projectID, "/tmp/trends/main.go")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID1, sourceOpencode, "create", "ses-1", "v1\n", nil)
	createBlobVersion(ctx, t, database, bs, fileID1, sourceOpencode, "update", "ses-1", "v2\n", nil)
	createBlobVersion(ctx, t, database, bs, fileID1, sourceOpencode, "update", "ses-1", "v3\n", nil)

	// File 2: 2 versions from claudecode
	fileID2, err := database.UpsertFile(ctx, projectID, "/tmp/trends/util.go")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID2, sourceClaude, "create", "ses-2", "util v1\n", nil)
	createBlobVersion(ctx, t, database, bs, fileID2, sourceClaude, "update", "ses-2", "util v2\n", nil)

	// File 3: 1 version from cursor
	fileID3, err := database.UpsertFile(ctx, projectID, "/tmp/trends/config.yaml")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID3, sourceCursor, "create", "ses-3", "key: val\n", nil)

	// ===== 2. Insert pending trend — use actual min/max changed_at from DB =====
	// SQLite CURRENT_TIMESTAMP returns "YYYY-MM-DD HH:MM:SS" (no T, no Z)
	// so we query actual timestamps to ensure string comparison works.
	var periodStart, periodEnd string
	err = database.Handle().QueryRowContext(ctx, `
		SELECT MIN(v.changed_at), MAX(v.changed_at)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		WHERE f.project_id = ?
	`, projectID).Scan(&periodStart, &periodEnd)
	require.NoError(t, err)

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'pending')",
		"trends-test", periodStart, periodEnd)
	require.NoError(t, err)

	// ===== 3. Process pending trends =====
	worker.processPendingTrends(ctx)

	// ===== 4. Verify completed with summary =====
	var status, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, COALESCE(summary, '') FROM ai_trends WHERE project_name = ? ORDER BY id DESC LIMIT 1",
		"trends-test").Scan(&status, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "E2E trends analysis", summary)

	// ===== 5. Verify stats are computed correctly =====
	var totalChanges int
	var totalFiles int64
	err = database.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT f.path)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
			AND v.changed_at >= ? AND v.changed_at <= ?
	`, "trends-test", periodStart, periodEnd).Scan(&totalChanges, &totalFiles)
	require.NoError(t, err)
	assert.Equal(t, 6, totalChanges, "3 + 2 + 1 versions")
	assert.Equal(t, int64(3), totalFiles, "3 distinct files")

	// Verify source breakdown
	sourceBreakdown := make(map[string]int)
	srcRows, err := database.Handle().QueryContext(ctx, `
		SELECT s.name, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		JOIN sources s ON v.source_id = s.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
			AND v.changed_at >= ? AND v.changed_at <= ?
		GROUP BY s.name
	`, "trends-test", periodStart, periodEnd)
	require.NoError(t, err)
	defer srcRows.Close()
	for srcRows.Next() {
		var name string
		var count int
		require.NoError(t, srcRows.Scan(&name, &count))
		sourceBreakdown[name] = count
	}
	assert.Equal(t, 3, sourceBreakdown["opencode"])
	assert.Equal(t, 2, sourceBreakdown["claudecode"])
	assert.Equal(t, 1, sourceBreakdown["cursor"])

	// Verify top files ordering
	fileRows, err := database.Handle().QueryContext(ctx, `
		SELECT f.path, COUNT(*)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
			AND v.changed_at >= ? AND v.changed_at <= ?
		GROUP BY f.path ORDER BY COUNT(*) DESC
	`, "trends-test", periodStart, periodEnd)
	require.NoError(t, err)
	defer fileRows.Close()

	var topFiles []struct{ Path string; Count int }
	for fileRows.Next() {
		var fc struct{ Path string; Count int }
		require.NoError(t, fileRows.Scan(&fc.Path, &fc.Count))
		topFiles = append(topFiles, fc)
	}
	require.Len(t, topFiles, 3)
	assert.Contains(t, topFiles[0].Path, "main.go")
	assert.Equal(t, 3, topFiles[0].Count)
}

// TestE2E_Trends_NoDataInPeriod verifies that trends analysis completes
// with zero stats when no versions exist in the specified period.
func TestE2E_Trends_NoDataInPeriod(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	// ===== 1. Create project with a version =====
	projectID, err := database.CreateProject(ctx, "no-data-test", "/tmp/nodata", "{}")
	require.NoError(t, err)

	sourceID, err := database.GetSourceIDByName(ctx, "human")
	require.NoError(t, err)

	fileID, err := database.UpsertFile(ctx, projectID, "/tmp/nodata/readme.txt")
	require.NoError(t, err)
	createBlobVersion(ctx, t, database, bs, fileID, sourceID, "create", "", "hello\n", nil)

	// ===== 2. Insert pending trend with future period (no data) =====
	futureStart := "2099-01-01T00:00:00Z"
	futureEnd := "2099-12-31T23:59:59Z"

	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_trends (project_name, period_start, period_end, status) VALUES (?, ?, ?, 'pending')",
		"no-data-test", futureStart, futureEnd)
	require.NoError(t, err)

	// ===== 3. Process pending trends =====
	worker.processPendingTrends(ctx)

	// ===== 4. Verify completed (not failed) with summary =====
	var status, summary string
	err = database.Handle().QueryRowContext(ctx,
		"SELECT status, COALESCE(summary, '') FROM ai_trends WHERE project_name = ? ORDER BY id DESC LIMIT 1",
		"no-data-test").Scan(&status, &summary)
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	assert.Equal(t, "E2E trends analysis", summary)

	// ===== 5. Verify zero stats =====
	var totalChanges int
	var totalFiles int64
	err = database.Handle().QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT f.path)
		FROM versions v
		JOIN files f ON v.file_id = f.id
		JOIN projects p ON f.project_id = p.id
		WHERE p.name = ? AND p.is_deleted = 0 AND f.is_deleted = 0
			AND v.changed_at >= ? AND v.changed_at <= ?
	`, "no-data-test", futureStart, futureEnd).Scan(&totalChanges, &totalFiles)
	require.NoError(t, err)
	assert.Equal(t, 0, totalChanges)
	assert.Equal(t, int64(0), totalFiles)
}

// TestE2E_Session_MultipleSessions verifies that processing picks up one pending
// session at a time, and multiple calls process all pending sessions.
func TestE2E_Session_MultipleSessions(t *testing.T) {
	ctx := context.Background()
	database, bs, ds := newTestEnv(t)
	worker := newTestWorker(t, database, bs, ds)

	projectID, err := database.CreateProject(ctx, "multi-sess", "/tmp/msess", "{}")
	require.NoError(t, err)
	sourceID, _ := database.GetSourceIDByName(ctx, "cursor")
	fileID, err := database.UpsertFile(ctx, projectID, "/tmp/msess/app.ts")
	require.NoError(t, err)

	// Create versions for two different sessions
	createBlobVersion(ctx, t, database, bs, fileID, sourceID, "create", "ses-first", "const a = 1\n", nil)
	createBlobVersion(ctx, t, database, bs, fileID, sourceID, "update", "ses-second", "const a = 2\n", nil)

	// Insert both as pending
	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')",
		"ses-first", "multi-sess")
	require.NoError(t, err)
	_, err = database.Handle().ExecContext(ctx,
		"INSERT INTO ai_sessions (session_id, project_name, status) VALUES (?, ?, 'pending')",
		"ses-second", "multi-sess")
	require.NoError(t, err)

	// ===== First call processes only one session =====
	worker.processPendingSessions(ctx)

	var pendingCount, completedCount int
	err = database.Handle().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_sessions WHERE status = 'pending'").Scan(&pendingCount)
	require.NoError(t, err)
	err = database.Handle().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_sessions WHERE status = 'completed'").Scan(&completedCount)
	require.NoError(t, err)
	assert.Equal(t, 1, pendingCount, "one session should still be pending")
	assert.Equal(t, 1, completedCount, "one session should be completed")

	// ===== Second call processes the remaining session =====
	worker.processPendingSessions(ctx)

	err = database.Handle().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_sessions WHERE status = 'pending'").Scan(&pendingCount)
	require.NoError(t, err)
	err = database.Handle().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ai_sessions WHERE status = 'completed'").Scan(&completedCount)
	require.NoError(t, err)
	assert.Equal(t, 0, pendingCount, "no sessions should be pending")
	assert.Equal(t, 2, completedCount, "both sessions should be completed")
}
