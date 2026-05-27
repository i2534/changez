// Package config 加载并解析 config.yaml 配置文件。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 服务全局配置。
type Config struct {
	Listen  string     `yaml:"listen"` // HTTP 监听地址，如 "127.0.0.1:8760"
	Token   string     `yaml:"token"`  // Bearer token 认证（为空则不认证）
	Storage StorageCfg `yaml:"storage"`
	Compact CompactCfg `yaml:"compact"`
	Cleanup CleanupCfg `yaml:"cleanup"`
	Log     LogCfg     `yaml:"log"`
}

// StorageCfg 存储相关配置。
type StorageCfg struct {
	MaxFileSize int64 `yaml:"max_file_size"` // 单文件最大字节数，默认 10MB
}

// CompactCfg 压缩整理配置。
type CompactCfg struct {
	Enabled                bool   `yaml:"enabled"`
	Interval               string `yaml:"interval"`                 // 定时器间隔，如 "24h"
	MaxDeltaChain          int    `yaml:"max_delta_chain"`          // 最大 delta 链长度，超过则 compact
	DeltaCompressThreshold int    `yaml:"delta_compress_threshold"` // delta 压缩阈值（字节），低于此值不压缩
}

// CleanupCfg 定时清理配置。
type CleanupCfg struct {
	Enabled  bool   `yaml:"enabled"`
	Interval string `yaml:"interval"` // 定时器间隔，如 "24h"
}

// LogCfg 日志配置。
type LogCfg struct {
	Level string `yaml:"level"` // 日志级别
	File  string `yaml:"file"`  // 日志文件路径
}

// Defaults 返回带有默认值的配置。
func Defaults() Config {
	return Config{
		Listen: "127.0.0.1:8760",
		Token:  "",
		Storage: StorageCfg{
			MaxFileSize: 10 * 1024 * 1024, // 10MB
		},
		Compact: CompactCfg{
			Enabled:                true,
			Interval:               "24h",
			MaxDeltaChain:          50,
			DeltaCompressThreshold: 512,
		},
		Cleanup: CleanupCfg{
			Enabled:  true,
			Interval: "168h",
		},
		Log: LogCfg{
			Level: "info",
			File:  "changez.log",
		},
	}
}

// Load 从指定路径加载配置文件，合并默认值后返回。
// 如果文件不存在，返回默认配置。
func Load(path string) (Config, error) {
	cfg := Defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config yaml: %w", err)
	}

	return cfg, nil
}
