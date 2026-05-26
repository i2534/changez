package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/changez/changez/internal/compact"
	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/handler"
	"github.com/changez/changez/internal/router"
	"github.com/changez/changez/internal/storage"
	webfront "github.com/changez/changez/web"
)

var webFS = &webfront.FS

func main() {
	configPath := flag.String("c", "config.yaml", "")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("create data dir", "error", err)
		os.Exit(1)
	}

	database, err := db.Open(dataDir)
	if err != nil {
		slog.Error("db init", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	sourceIDs, err := database.LoadSourceNameToID(context.Background())
	if err != nil {
		slog.Error("load sources", "error", err)
		os.Exit(1)
	}

	if err := database.RecoverOrphans(context.Background()); err != nil {
		slog.Error("recover orphans", "error", err)
		os.Exit(1)
	}

	blobStore := storage.NewBlobStore(dataDir)
	deltaStore := storage.NewDeltaStore(dataDir)

	if err := blobStore.EnsureDir(); err != nil {
		slog.Error("create blobs dir", "error", err)
		os.Exit(1)
	}
	if err := deltaStore.EnsureDir(); err != nil {
		slog.Error("create deltas dir", "error", err)
		os.Exit(1)
	}

	refHashes, err := database.GetReferencedBlobHashes(context.Background())
	if err != nil {
		slog.Error("get referenced blob hashes", "error", err)
		os.Exit(1)
	}
	if removed, err := blobStore.RemoveOrphanBlobs(refHashes); err != nil {
		slog.Error("remove orphan blobs", "error", err)
		os.Exit(1)
	} else if removed > 0 {
		slog.Info("removed orphan blobs", "count", removed)
	}

	fileMuMap := &sync.Map{}
	loggerWrapper := handler.NewLogger(&cfg)
	defer loggerWrapper.Close()
	logger := loggerWrapper.Logger
	compactor := compact.New(database, blobStore, deltaStore, &cfg.Compact, logger, fileMuMap)
	httpRouter := router.New(database, blobStore, deltaStore, &cfg, sourceIDs, cfg.Token, fileMuMap, compactor, logger, webFS)

	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      httpRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go compactor.Run(ctx)

	errChan := make(chan error, 1)
	go func() {
		slog.Info("changez server starting", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	select {
	case srvErr := <-errChan:
		if srvErr != nil {
			slog.Error("server error", "error", srvErr)
		}
	default:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
}
