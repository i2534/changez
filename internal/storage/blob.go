// Blob 存储：完整文件快照（zstd 压缩，SHA256 命名）。
package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// BlobStore 管理 blobs/ 目录下的完整文件快照。
type BlobStore struct {
	dir string // data/blobs/
}

// NewBlobStore 创建 Blob 存储实例。
func ContentHash(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}

func NewBlobStore(dataDir string) *BlobStore {
	dir := filepath.Join(dataDir, "blobs")
	return &BlobStore{dir: dir}
}

// EnsureDir 确保 blobs 目录存在。
func (s *BlobStore) EnsureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

// Store 将原始内容压缩后写入 blob 文件，返回 SHA256 hash。
// 如果相同内容的 blob 已存在则跳过写入（幂等）。
func (s *BlobStore) Store(content []byte) (string, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	blobPath := filepath.Join(s.dir, hash)

	// 如果 blob 文件已存在，直接返回 hash
	if _, err := os.Stat(blobPath); err == nil {
		return hash, nil
	}

	// zstd 压缩
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		return "", fmt.Errorf("create zstd writer: %w", err)
	}
	if _, err := w.Write(content); err != nil {
		w.Close()
		return "", fmt.Errorf("zstd write content: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("zstd close: %w", err)
	}

	// 构造 header + compressed content
	header := make([]byte, BlobHeaderSize)
	binary.BigEndian.PutUint32(header[0:4], BlobMagic)
	binary.BigEndian.PutUint16(header[4:6], CompressZstd)

	data := append(header, buf.Bytes()...)

	// 原子写入：先写临时文件再 rename
	tmpPath := blobPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write blob tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, blobPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename blob file: %w", err)
	}

	return hash, nil
}

// Read 根据 SHA256 hash 读取并解压 blob 文件，返回原始内容。
func (s *BlobStore) Read(hash string) ([]byte, error) {
	blobPath := filepath.Join(s.dir, hash)

	data, err := os.ReadFile(blobPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("blob %q not found", hash)
		}
		return nil, fmt.Errorf("read blob file: %w", err)
	}

	// 校验 header
	if len(data) < BlobHeaderSize {
		return nil, fmt.Errorf("blob %q: data too short for header", hash)
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != BlobMagic {
		return nil, fmt.Errorf("blob %q: invalid magic 0x%08X", hash, magic)
	}

	compressMethod := binary.BigEndian.Uint16(data[4:6])
	compressed := data[BlobHeaderSize:]

	// 解压
	switch compressMethod {
	case CompressNone:
		return compressed, nil
	case CompressZstd:
		r, err := zstd.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("blob %q: create zstd reader: %w", hash, err)
		}
		defer r.Close()

		var result bytes.Buffer
		if _, err := result.ReadFrom(r); err != nil {
			return nil, fmt.Errorf("blob %q: zstd decompress: %w", hash, err)
		}
		return result.Bytes(), nil
	default:
		return nil, fmt.Errorf("blob %q: unknown compress method %d", hash, compressMethod)
	}
}

// Dir 返回 blob 存储目录路径。
func (s *BlobStore) Dir() string {
	return s.dir
}

// Remove 删除指定的 blob 文件。
func (s *BlobStore) Remove(hash string) error {
	blobPath := filepath.Join(s.dir, hash)
	if err := os.Remove(blobPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove blob %q: %w", hash, err)
	}
	return nil
}

// RemoveOrphanBlobs 删除 blobs/ 目录下未被引用的孤儿文件。
// referencedHashes 包含所有被 versions 表引用的 blob hash。
// 同时清理遗留的 .tmp 临时文件。
// 返回删除的文件数量。
func (s *BlobStore) RemoveOrphanBlobs(referencedHashes map[string]bool) (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read blobs dir: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// 清理 .tmp 临时文件
		if strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to remove tmp blob", "file", name, "error", err)
			} else {
				removed++
			}
			continue
		}

		// 删除未被引用的 blob 文件
		if !referencedHashes[name] {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to remove orphan blob", "file", name, "error", err)
			} else {
				removed++
			}
		}
	}
	return removed, nil
}
