package router

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/changez/changez/internal/compact"
	"github.com/changez/changez/internal/config"
	"github.com/changez/changez/internal/db"
	"github.com/changez/changez/internal/handler"
	"github.com/changez/changez/internal/mcp"
	"github.com/changez/changez/internal/storage"
)

func New(
	database *db.DB,
	blobStore *storage.BlobStore,
	deltaStore *storage.DeltaStore,
	cfg *config.Config,
	sourceIDs map[string]int64,
	token string,
	fileMuMap *sync.Map,
	compactor *compact.Compactor,
	logger *slog.Logger,
	webFS *embed.FS,
) http.Handler {
	h := handler.NewHandler(database, blobStore, deltaStore, cfg, sourceIDs, logger, fileMuMap)
	h.Compact = compactor

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/api/ui/auth-required", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"required": token != ""})
	})

	mcpHandler := mcp.NewMCPHandler(h)
	mux.Handle("/mcp", authMiddleware(mcpHandler, token))
	mux.Handle("/mcp/", authMiddleware(mcpHandler, token))

	apiMux := http.NewServeMux()

	apiMux.HandleFunc("/api/snapshot", h.HandleSnapshot)

	apiMux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		h.HandleListFiles(w, r)
	})

	// 文件操作子路由（path 通过 query parameter 传递）
	apiMux.HandleFunc("/api/files/versions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleLog(w, r)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		}
	})
	apiMux.HandleFunc("/api/files/restore", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleRestore(w, r)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		}
	})
	apiMux.HandleFunc("/api/files/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleDiff(w, r)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		}
	})

	apiMux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleListProjects(w, r)
		case http.MethodPost:
			h.HandleCreateProject(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET/POST")
		}
	})

	apiMux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			h.HandleDeleteProject(w, r)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 DELETE")
	})

	apiMux.HandleFunc("/api/stats", h.HandleStats)
	apiMux.HandleFunc("/api/snapshots/latest", h.HandleLatestSnapshot)
	apiMux.HandleFunc("/api/recent-activity", h.HandleRecentActivity)

	mux.Handle("/api/", authMiddleware(loggingMiddleware(apiMux, h.Logger), token))

if webFS != nil {
		subFS, err := fs.Sub(*webFS, "dist")
		if err == nil {
			fileServer := http.FileServer(http.FS(subFS))
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/mcp") && r.URL.Path != "/health" {
					path := strings.TrimPrefix(r.URL.Path, "/")
					if path == "" {
						path = "index.html"
					}
					f, err := subFS.Open(path)
					if err == nil {
						f.Close()
						fileServer.ServeHTTP(w, r)
						return
					}
					indexPath := "dist/index.html"
					indexData, err := webFS.ReadFile(indexPath)
					if err == nil {
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						w.WriteHeader(http.StatusOK)
						//nolint:errcheck
						w.Write(indexData)
						return
					}
					http.NotFound(w, r)
				}
			})
		}
	}

	return mux
}

// statusCaptureWriter 包装 ResponseWriter 以捕获 HTTP 状态码。
type statusCaptureWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCaptureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func loggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cw := &statusCaptureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(cw, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", cw.status,
			"duration", time.Since(start).String(),
		)
	})
}

// authMiddleware 返回 Bearer token 认证中间件。token 为空时不认证。
func authMiddleware(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			//nolint:errcheck
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "invalid or missing token",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}



func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
