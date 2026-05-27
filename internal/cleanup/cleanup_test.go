package cleanup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTest(t *testing.T) (*Cleanup, *db.DB, *storage.BlobStore, *storage.DeltaStore, string) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bs := storage.NewBlobStore(dir)
	require.NoError(t, bs.EnsureDir())

	ds := storage.NewDeltaStore(dir)
	require.NoError(t, ds.EnsureDir())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	fileMuMap := &sync.Map{}

	cfg := &config.CleanupCfg{
		Enabled:  true,
		Interval: "1h",
	}

	c := New(database, bs, ds, cfg, logger, fileMuMap)
	return c, database, bs, ds, dir
}

func TestCleanupOnce_NoDeletedData(t *testing.T) {
	c, d, _, _, _ := setupTest(t)
	ctx := context.Background()

	_, err := d.CreateProject(ctx, "alive", "/tmp/alive", "{}")
	require.NoError(t, err)

	err = c.CleanupOnce(ctx)
	require.NoError(t, err)

	ids, err := d.GetDeletedProjectIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestCleanupOnce_DeletedProject(t *testing.T) {
	c, d, bs, ds, _ := setupTest(t)
	ctx := context.Background()

	projectID, err := d.CreateProject(ctx, "del-me", "/tmp/del", "{}")
	require.NoError(t, err)

	fileID, err := d.UpsertFile(ctx, projectID, "src/main.go")
	require.NoError(t, err)

	hash, err := bs.Store([]byte("code"))
	require.NoError(t, err)
	blobHash := hash
	offset := int64(0)
	_, err = d.CreateVersion(ctx, fileID, "blob", &blobHash, &offset, nil, "update", 1)
	require.NoError(t, err)

	deltaPath := filepath.Join(ds.Dir(), fmt.Sprintf("%d.delta", fileID))
	require.NoError(t, os.WriteFile(deltaPath, []byte("delta-data"), 0o644))

	err = d.SoftDeleteProject(ctx, projectID)
	require.NoError(t, err)

	err = c.CleanupOnce(ctx)
	require.NoError(t, err)

	projects, err := d.GetDeletedProjectIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, projects)

	rows, err := d.Query(ctx, "SELECT id FROM files WHERE id = ?", fileID)
	require.NoError(t, err)
	assert.False(t, rows.Next())
	rows.Close()

	_, err = os.Stat(deltaPath)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanupOnce_DeletedFile(t *testing.T) {
	c, d, bs, ds, _ := setupTest(t)
	ctx := context.Background()

	projectID, err := d.CreateProject(ctx, "keep-proj", "/tmp/keep", "{}")
	require.NoError(t, err)

	fileID, err := d.UpsertFile(ctx, projectID, "src/main.go")
	require.NoError(t, err)

	hash, err := bs.Store([]byte("code"))
	require.NoError(t, err)
	blobHash := hash
	offset := int64(0)
	_, err = d.CreateVersion(ctx, fileID, "blob", &blobHash, &offset, nil, "update", 1)
	require.NoError(t, err)

	deltaPath := filepath.Join(ds.Dir(), fmt.Sprintf("%d.delta", fileID))
	require.NoError(t, os.WriteFile(deltaPath, []byte("delta-data"), 0o644))

	err = d.SoftDeleteFile(ctx, fileID)
	require.NoError(t, err)

	err = c.CleanupOnce(ctx)
	require.NoError(t, err)

	rows, err := d.Query(ctx, "SELECT id FROM files WHERE id = ?", fileID)
	require.NoError(t, err)
	assert.False(t, rows.Next())
	rows.Close()

	_, err = os.Stat(deltaPath)
	assert.True(t, os.IsNotExist(err))

	ids, err := d.GetDeletedProjectIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestCleanupOnce_OrphanBlob(t *testing.T) {
	c, _, bs, _, _ := setupTest(t)
	ctx := context.Background()

	orphanContent := []byte("orphan-blob-content")
	orphanHash, err := bs.Store(orphanContent)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(bs.Dir(), orphanHash))
	require.NoError(t, err)

	err = c.CleanupOnce(ctx)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(bs.Dir(), orphanHash))
	assert.True(t, os.IsNotExist(err))
}

func TestCleanupOnce_OrphanDelta(t *testing.T) {
	c, _, _, ds, _ := setupTest(t)
	ctx := context.Background()

	orphanDeltaPath := filepath.Join(ds.Dir(), "9999.delta")
	require.NoError(t, os.WriteFile(orphanDeltaPath, []byte("orphan-delta"), 0o644))

	err := c.CleanupOnce(ctx)
	require.NoError(t, err)

	_, err = os.Stat(orphanDeltaPath)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanupOnce_SharedBlob(t *testing.T) {
	c, d, bs, _, _ := setupTest(t)
	ctx := context.Background()

	projectID, err := d.CreateProject(ctx, "shared", "/tmp/shared", "{}")
	require.NoError(t, err)

	fileID1, err := d.UpsertFile(ctx, projectID, "src/a.go")
	require.NoError(t, err)
	fileID2, err := d.UpsertFile(ctx, projectID, "src/b.go")
	require.NoError(t, err)

	content := []byte("shared content")
	hash, err := bs.Store(content)
	require.NoError(t, err)

	blobHash := hash
	offset := int64(0)
	_, err = d.CreateVersion(ctx, fileID1, "blob", &blobHash, &offset, nil, "update", 1)
	require.NoError(t, err)
	_, err = d.CreateVersion(ctx, fileID2, "blob", &blobHash, &offset, nil, "update", 1)
	require.NoError(t, err)

	err = d.SoftDeleteFile(ctx, fileID1)
	require.NoError(t, err)

	err = c.CleanupOnce(ctx)
	require.NoError(t, err)

	rows, err := d.Query(ctx, "SELECT id FROM files WHERE id = ?", fileID1)
	require.NoError(t, err)
	assert.False(t, rows.Next())
	rows.Close()

	rows, err = d.Query(ctx, "SELECT id FROM files WHERE id = ?", fileID2)
	require.NoError(t, err)
	assert.True(t, rows.Next())
	rows.Close()

	_, err = os.Stat(filepath.Join(bs.Dir(), hash))
	assert.NoError(t, err)
}

func TestRun_Disabled(t *testing.T) {
	c, _, _, _, _ := setupTest(t)
	c.cfg.Enabled = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run should return immediately when Enabled=false")
	}
}

func TestRun_ContextCancel(t *testing.T) {
	c, _, _, _, _ := setupTest(t)
	c.cfg.Interval = "50ms"

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run should exit after context is cancelled")
	}
}
