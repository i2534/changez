// Package config 测试配置加载和解析功能。
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	assert.Equal(t, "127.0.0.1:8760", cfg.Listen)
	assert.Equal(t, "", cfg.Token)
	assert.Equal(t, int64(10485760), cfg.Storage.MaxFileSize)
	assert.True(t, cfg.Compact.Enabled)
	assert.Equal(t, "24h", cfg.Compact.Interval)
	assert.Equal(t, 50, cfg.Compact.MaxDeltaChain)
	assert.Equal(t, 512, cfg.Compact.DeltaCompressThreshold)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "changez.log", cfg.Log.File)
}

func TestLoad_FileNotExists(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")

	assert.NoError(t, err)
	assert.Equal(t, Defaults(), cfg)
}

func TestLoad_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `listen: "0.0.0.0:9000"
token: "my-secret-token"
storage:
  max_file_size: 20971520
compact:
  enabled: false
  interval: "12h"
  max_delta_chain: 100
  delta_compress_threshold: 1024
log:
  level: "debug"
  file: "/var/log/changez.log"
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0o644)
	assert.NoError(t, err)

	cfg, err := Load(configPath)

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", cfg.Listen)
	assert.Equal(t, "my-secret-token", cfg.Token)
	assert.Equal(t, int64(20971520), cfg.Storage.MaxFileSize)
	assert.False(t, cfg.Compact.Enabled)
	assert.Equal(t, "12h", cfg.Compact.Interval)
	assert.Equal(t, 100, cfg.Compact.MaxDeltaChain)
	assert.Equal(t, 1024, cfg.Compact.DeltaCompressThreshold)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "/var/log/changez.log", cfg.Log.File)
}

func TestLoad_MergeDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 只设置 listen，其他字段应保持默认值
	yamlContent := `listen: "0.0.0.0:3000"
`
	err := os.WriteFile(configPath, []byte(yamlContent), 0o644)
	assert.NoError(t, err)

	cfg, err := Load(configPath)

	assert.NoError(t, err)
	// 覆盖的字段
	assert.Equal(t, "0.0.0.0:3000", cfg.Listen)
	// 保持默认值的字段
	assert.Equal(t, "", cfg.Token)
	assert.Equal(t, int64(10485760), cfg.Storage.MaxFileSize)
	assert.True(t, cfg.Compact.Enabled)
	assert.Equal(t, "24h", cfg.Compact.Interval)
	assert.Equal(t, 50, cfg.Compact.MaxDeltaChain)
	assert.Equal(t, 512, cfg.Compact.DeltaCompressThreshold)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "changez.log", cfg.Log.File)
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `listen: "0.0.0.0:3000"
  invalid indentation
token: "test"
`
	err := os.WriteFile(configPath, []byte(invalidYAML), 0o644)
	assert.NoError(t, err)

	cfg, err := Load(configPath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse config yaml")
	// 即使出错，仍返回默认配置
	assert.Equal(t, Defaults(), cfg)
}

func TestFindConfig_EmptyString(t *testing.T) {
	path, err := FindConfig("")

	assert.NoError(t, err)
	cwd, err := os.Getwd()
	assert.NoError(t, err)
	expected := filepath.Join(cwd, "config.yaml")
	assert.Equal(t, expected, path)
}

func TestFindConfig_RelativePath(t *testing.T) {
	path, err := FindConfig("relative/path/config.yaml")

	assert.NoError(t, err)
	assert.True(t, filepath.IsAbs(path))
	assert.Contains(t, path, "relative/path/config.yaml")
}

func TestFindConfig_AbsolutePath(t *testing.T) {
	absPath := "/absolute/path/config.yaml"
	path, err := FindConfig(absPath)

	assert.NoError(t, err)
	assert.Equal(t, absPath, path)
}
