// Package webfront 嵌入 Web UI 静态文件。
package webfront

import "embed"

//go:embed dist/*
var FS embed.FS
