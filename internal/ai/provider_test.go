package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSummaryContext_Structure(t *testing.T) {
	ctx := SummaryContext{
		FilePath:  "src/main.go",
		Action:    "create",
		Source:    "opencode",
		Model:     "gpt-4o-mini",
		Message:   "初始创建",
		Timestamp: "2024-01-01T00:00:00Z",
	}

	assert.Equal(t, "src/main.go", ctx.FilePath)
	assert.Equal(t, "create", ctx.Action)
	assert.Equal(t, "opencode", ctx.Source)
	assert.Equal(t, "gpt-4o-mini", ctx.Model)
	assert.Equal(t, "初始创建", ctx.Message)
	assert.Equal(t, "2024-01-01T00:00:00Z", ctx.Timestamp)
}

func TestSummaryContext_ZeroValue(t *testing.T) {
	ctx := SummaryContext{}

	assert.Empty(t, ctx.FilePath)
	assert.Empty(t, ctx.Action)
	assert.Empty(t, ctx.Source)
	assert.Empty(t, ctx.Model)
	assert.Empty(t, ctx.Message)
	assert.Empty(t, ctx.Timestamp)
}

func TestProvider_Interface(t *testing.T) {
	var p Provider = &mockProvider{summary: "test", err: nil}
	assert.NotNil(t, p)
}
