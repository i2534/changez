// Package config 加载并解析 config.yaml 配置文件。
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// Config 服务全局配置。
type Config struct {
	Listen  string     `yaml:"listen"` // HTTP 监听地址，如 "127.0.0.1:8760"
	Token   string     `yaml:"token"`  // Bearer token 认证（为空则不认证）
	Storage StorageCfg `yaml:"storage"`
	Compact CompactCfg `yaml:"compact"`
	Cleanup CleanupCfg `yaml:"cleanup"`
	Log     LogCfg     `yaml:"log"`
	AI      AICfg      `yaml:"ai"`
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

// AITriggerCfg AI 触发器配置。
type AITriggerCfg struct {
	OnSnapshot  bool `yaml:"on_snapshot"`  // 是否在快照时触发 AI 分析
	BatchSize   int  `yaml:"batch_size"`   // 批量处理大小
	MaxRetries  int  `yaml:"max_retries"`  // 最大重试次数
	MaxDiffSize int  `yaml:"max_diff_size"` // 单次 diff 最大字节数，超过则截断（默认 64KB）
}

// AICfg AI 模块配置。
type AICfg struct {
	Enabled       bool         `yaml:"enabled"`        // 是否启用 AI 模块
	Provider      string       `yaml:"provider"`       // AI 提供商，默认 "openai"
	BaseURL       string       `yaml:"base_url"`       // API 基础 URL，支持 ${VAR} 环境变量展开
	APIKey        string       `yaml:"api_key"`        // API 密钥，支持 ${VAR} 环境变量展开
	Model         string       `yaml:"model"`          // 模型名称，默认 "gpt-4o-mini"
	MaxTokens     int          `yaml:"max_tokens"`     // 最大输出 token 数，默认 1024
	Temperature   float64      `yaml:"temperature"`    // 采样温度，默认 0.3
	Timeout       string       `yaml:"timeout"`        // 请求超时，默认 "30s"
	Prompt        string       `yaml:"prompt"`         // 摘要 Prompt 模板
	SessionPrompt string       `yaml:"session_prompt"` // 会话分析 Prompt 模板
	TrendsPrompt  string       `yaml:"trends_prompt"`  // 趋势分析 Prompt 模板
	Triggers      AITriggerCfg `yaml:"triggers"`      // 触发器配置
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
		AI: AICfg{
			Enabled:  false,
			Provider: "openai",
			Model:    "gpt-4o-mini",
			MaxTokens:   1024,
			Temperature: 0.3,
			Timeout:     "30s",
			Prompt: `你是一个代码变更分析助手。请分析以下文件变更并生成简洁的中文摘要。

文件: {{.FilePath}}
操作: {{.Action}}
来源: {{.Source}}
{{.Message}}

请简要说明这次变更做了什么，用1-2句话概括。只输出摘要内容，不要加任何前缀或解释。

变更内容:
{{.Diff}}`,
			SessionPrompt: `你是一个代码变更分析助手。请分析以下会话中的代码变更并生成简洁的中文报告。

会话包含以下变更：
{{range .Changes}}
- 文件: {{.FilePath}}
  操作: {{.Action}}
  {{.Message}}
  变更:
  {{.Diff}}
{{end}}

请生成一份简洁的会话报告，包括：
1. 本次会话的主要目标
2. 关键变更点（2-3条）
3. 潜在风险或注意事项

只输出报告内容，不要加任何前缀。`,
			TrendsPrompt: `你是一个代码变更分析助手。请分析以下项目趋势数据并生成简洁的中文报告。

统计周期: {{.Period}}
变更文件数: {{.TotalFiles}}
总变更次数: {{.TotalChanges}}

变更来源分布:
{{range .SourceBreakdown}}
- {{.Source}}: {{.Count}} 次
{{end}}

活跃文件:
{{range .TopFiles}}
- {{.FilePath}}: {{.Count}} 次变更
{{end}}

请分析这些趋势数据，生成一份简洁的趋势报告，包括：
1. 整体变更趋势概述
2. AI 工具使用分布分析
3. 热点文件分析

只输出报告内容，不要加任何前缀。`,
			Triggers: AITriggerCfg{
				OnSnapshot:  true,
				BatchSize:   5,
				MaxRetries:  3,
				MaxDiffSize: 64 * 1024, // 64KB
			},
		},
	}
}

// Load 从指定路径加载配置文件，合并默认值后返回。
// 如果文件不存在，返回默认配置。
// 自动加载 path.local 作为本地覆盖（不被 git 追踪）。
// 支持 ${VAR} 环境变量展开。
func Load(path string) (Config, error) {
	defaults := Defaults()
	cfg := defaults

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

	// 加载 .local 覆盖
	localPath := path + ".local"
	if localData, err := os.ReadFile(localPath); err == nil {
		if err := yaml.Unmarshal(localData, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config yaml (%s): %w", localPath, err)
		}
	}

	// 恢复未覆盖的默认值
	if cfg.AI.Prompt == "" {
		cfg.AI.Prompt = defaults.AI.Prompt
	}
	if cfg.AI.SessionPrompt == "" {
		cfg.AI.SessionPrompt = defaults.AI.SessionPrompt
	}
	if cfg.AI.TrendsPrompt == "" {
		cfg.AI.TrendsPrompt = defaults.AI.TrendsPrompt
	}

	cfg.AI.BaseURL = expandEnv(cfg.AI.BaseURL)
	cfg.AI.APIKey = expandEnv(cfg.AI.APIKey)

	return cfg, nil
}

func expandEnv(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1]
		return os.Getenv(varName)
	})
}
