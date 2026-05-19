package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func (h *Handler) HandleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "仅支持 GET")
		return
	}

	path := extractPathFromURL(r.URL.Path, "/api/files/", "/diff")
	if path == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path 参数缺失")
		return
	}

	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	if fromStr == "" || toStr == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "需要 from 和 to 参数")
		return
	}

	from, err := strconv.ParseInt(fromStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("无效的 from 版本号: %s", fromStr))
		return
	}

	to, err := strconv.ParseInt(toStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", fmt.Sprintf("无效的 to 版本号: %s", toStr))
		return
	}

	h.Logger.Debug("diff request", "path", path, "from", from, "to", to)

	diff, err := h.doDiff(r.Context(), path, from, to)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "VERSION_NOT_FOUND", err.Error())
		} else if strings.Contains(err.Error(), "delete") || strings.Contains(err.Error(), "deleted") {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		} else if strings.Contains(err.Error(), "不属于") {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		} else {
			h.Logger.Error("diff rebuild failed", "path", path, "from", from, "to", to, "error", err)
			writeError(w, http.StatusInternalServerError, "CORRUPTED_DATA", err.Error())
		}
		return
	}

	resp := map[string]any{
		"path": path,
		"from": from,
		"to":   to,
		"diff": diff,
	}
	writeJSON(w, http.StatusOK, resp)
}

// doDiff resolves project/file, locks, gets versions, computes unified diff (shared by HandleDiff and ProcessDiff).
func (h *Handler) doDiff(ctx context.Context, path string, versionA, versionB int64) (string, error) {
	project, err := h.DB.FindProjectByPath(ctx, path)
	if err != nil {
		return "", fmt.Errorf("project not found: %w", err)
	}

	relPath := path
	rootPath := project["rootPath"].(string)
	if len(path) > len(rootPath) && path[:len(rootPath)] == rootPath {
		relPath = strings.TrimPrefix(path[len(rootPath):], "/")
	}

	file, err := h.DB.GetFileByPath(ctx, project["id"].(int64), relPath)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}
	fileID := file["id"].(int64)

	fileMu := h.getFileLock(fileID)
	fileMu.RLock()
	defer fileMu.RUnlock()

	verA, err := h.DB.GetVersion(ctx, versionA)
	if err != nil {
		return "", fmt.Errorf("version %d not found: %w", versionA, err)
	}
	verB, err := h.DB.GetVersion(ctx, versionB)
	if err != nil {
		return "", fmt.Errorf("version %d not found: %w", versionB, err)
	}

	if verA["fileID"].(int64) != fileID {
		return "", fmt.Errorf("version %d does not belong to this file", versionA)
	}
	if verB["fileID"].(int64) != fileID {
		return "", fmt.Errorf("version %d does not belong to this file", versionB)
	}

	if verA["action"].(string) == "delete" {
		return "", fmt.Errorf("version %d is deleted", versionA)
	}
	if verB["action"].(string) == "delete" {
		return "", fmt.Errorf("version %d is deleted", versionB)
	}

	var diffs []diffmatchpatch.Diff
	isAdjacentDelta := false

	if verB["storageMode"].(string) == "delta" && verB["baseID"] != nil && *verB["baseID"].(*int64) == verA["id"].(int64) {
		offset := verB["deltaOffset"].(*int64)
		if offset != nil {
			_, readDiffs, _, err := h.DeltaStore.ReadEntry(fileID, *offset)
			if err == nil {
				diffs = readDiffs
				isAdjacentDelta = true
			}
		}
	}

	if !isAdjacentDelta {
		contentA, err := h.rebuildContent(ctx, verA)
		if err != nil {
			return "", fmt.Errorf("rebuild version %d: %w", versionA, err)
		}
		contentB, err := h.rebuildContent(ctx, verB)
		if err != nil {
			return "", fmt.Errorf("rebuild version %d: %w", versionB, err)
		}

		dmp := diffmatchpatch.New()
		diffs = dmp.DiffMain(string(contentA), string(contentB), true)
		diffs = dmp.DiffCleanupSemantic(diffs)
	}

	return renderUnifiedDiff(path, verA["changedAt"].(string), verB["changedAt"].(string), diffs), nil
}

func renderUnifiedDiff(filePath, timeA, timeB string, diffs []diffmatchpatch.Diff) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("--- a/%s\t%s\n", filePath, timeA))
	b.WriteString(fmt.Sprintf("+++ b/%s\t%s\n", filePath, timeB))

	type lineEntry struct {
		op   rune
		text string
	}

	var allLines []lineEntry
	for _, d := range diffs {
		text := d.Text
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			if d.Type == diffmatchpatch.DiffEqual {
				allLines = append(allLines, lineEntry{' ', line})
			} else if d.Type == diffmatchpatch.DiffInsert {
				allLines = append(allLines, lineEntry{'+', line})
			} else if d.Type == diffmatchpatch.DiffDelete {
				allLines = append(allLines, lineEntry{'-', line})
			}
		}
	}
	if len(allLines) > 0 && allLines[len(allLines)-1].text == "" {
		allLines = allLines[:len(allLines)-1]
	}

	startA, startB := 1, 1
	countA, countB := 0, 0
	for _, l := range allLines {
		switch l.op {
		case ' ':
			countA++
			countB++
		case '-':
			countA++
		case '+':
			countB++
		}
	}

	if countA == 0 && countB == 0 {
		return b.String()
	}

	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", startA, countA, startB, countB))

	for _, l := range allLines {
		if l.op == ' ' {
			b.WriteString(" " + l.text + "\n")
		} else if l.op == '-' {
			b.WriteString("-" + l.text + "\n")
		} else if l.op == '+' {
			b.WriteString("+" + l.text + "\n")
		}
	}

	return b.String()
}
