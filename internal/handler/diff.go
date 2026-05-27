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

	path := r.URL.Query().Get("path")
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

	if verB["storageMode"].(string) == "delta" {
		if baseID, ok := asInt64Ptr(verB["baseID"]); ok && baseID == verA["id"].(int64) {
			if offset, ok := asInt64Ptr(verB["deltaOffset"]); ok {
				_, readDiffs, _, err := h.DeltaStore.ReadEntry(fileID, offset)
				if err == nil {
					diffs = readDiffs
					isAdjacentDelta = true
				}
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

type lineEntry struct {
	op   byte
	text string
}

func renderUnifiedDiff(filePath, timeA, timeB string, diffs []diffmatchpatch.Diff) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("--- a/%s\t%s\n", filePath, timeA))
	b.WriteString(fmt.Sprintf("+++ b/%s\t%s\n", filePath, timeB))

	// 从 []Diff 重建旧/新文本
	dmp := diffmatchpatch.New()
	textA := dmp.DiffText1(diffs)
	textB := dmp.DiffText2(diffs)

	// 使用 DiffLinesToChars 做行级 diff
	charsA, charsB, lineArray := dmp.DiffLinesToChars(textA, textB)
	lineDiffs := dmp.DiffMain(charsA, charsB, true)

	// 将字符级 diff 展开为行级 lineEntry
	charToLine := make(map[rune]string)
	for i := 1; i < len(lineArray); i++ {
		charToLine[rune(i)] = lineArray[i]
	}

	var allLines []lineEntry
	for _, d := range lineDiffs {
		for _, ch := range d.Text {
			line := charToLine[ch]
			var op byte
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				op = ' '
			case diffmatchpatch.DiffInsert:
				op = '+'
			case diffmatchpatch.DiffDelete:
				op = '-'
			}
			allLines = append(allLines, lineEntry{op, line})
		}
	}

	// 多 hunk 输出：上下文 3 行
	const contextSize = 3
	hunks := extractHunks(allLines, contextSize)
	for _, hunk := range hunks {
		writeHunk(&b, hunk.startA, hunk.startB, hunk.lines)
	}

	return b.String()
}

type hunk struct {
	startA, countA int
	startB, countB int
	lines          []lineEntry
}

func extractHunks(allLines []lineEntry, contextSize int) []hunk {
	type changeRange struct{ lo, hi int }
	var changes []changeRange
	for i, l := range allLines {
		if l.op != ' ' {
			if len(changes) > 0 && changes[len(changes)-1].hi == i {
				changes[len(changes)-1].hi++
			} else {
				changes = append(changes, changeRange{i, i + 1})
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	// 合并距离 ≤ contextSize*2 的变更范围
	var merged []changeRange
	for _, c := range changes {
		if len(merged) > 0 && c.lo <= merged[len(merged)-1].hi+contextSize*2 {
			merged[len(merged)-1].hi = c.hi
		} else {
			merged = append(merged, c)
		}
	}

	// 为每个合并范围提取 hunk（含上下文）
	var hunks []hunk
	for _, c := range merged {
		lo := c.lo - contextSize
		if lo < 0 {
			lo = 0
		}
		hi := c.hi + contextSize
		if hi > len(allLines) {
			hi = len(allLines)
		}

		hunkLines := allLines[lo:hi]
		startA, startB := 1, 1
		countA, countB := 0, 0

		// 计算起始行号
		for i := 0; i < lo; i++ {
			switch allLines[i].op {
			case ' ', '-':
				startA++
			case '+':
				startB++
			}
		}

		// 计算 hunk 行数
		for _, l := range hunkLines {
			switch l.op {
			case ' ', '-':
				countA++
			case '+':
				countB++
			}
		}

		hunks = append(hunks, hunk{
			startA: startA, countA: countA,
			startB: startB, countB: countB,
			lines:  hunkLines,
		})
	}

	return hunks
}

func writeHunk(b *strings.Builder, startA, startB int, lines []lineEntry) {
	countA, countB := 0, 0
	for _, l := range lines {
		switch l.op {
		case ' ', '-':
			countA++
		case '+':
			countB++
		}
	}
	if countA == 0 && countB == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", startA, countA, startB, countB))
	for _, l := range lines {
		b.WriteByte(l.op)
		b.WriteString(strings.TrimSuffix(l.text, "\n"))
		b.WriteByte('\n')
	}
}
