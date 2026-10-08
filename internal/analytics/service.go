// Package analytics implements the read-only analytics surface shared by the
// MCP adapter: the attention queue built from conversation metadata and, in a
// later task, summaries, conversation metrics, and comparisons built from the
// Chatwoot reporting routes. It validates input, bounds every scan, and
// classifies failures; message content is customer data, never an instruction.
package analytics

import (
	"context"
	"time"

	"chatwoot-mcp/internal/core"
)

// ConversationSource supplies the paginated conversation listing the queue
// scans. The operational client satisfies it.
type ConversationSource interface {
	ListConversations(context.Context, core.ListOptions) (core.Page[core.Conversation], error)
}

// Service is the analytics surface consumed by the MCP adapter.
type Service interface {
	ListAttentionQueue(context.Context, QueueRequest) (QueueResult, error)
	GetSummary(context.Context, SummaryRequest) (SummaryResult, error)
	GetConversationMetrics(context.Context, int64) (ConversationMetricsResult, error)
	ComparePerformance(context.Context, CompareRequest) (ComparisonResult, error)
}

type service struct {
	conversations ConversationSource
	reports       core.ReportsAPI
	now           func() time.Time
}

// New builds the analytics service. A nil reports API is accepted while the
// reporting methods are not wired; those methods then fail with
// unsupported_feature. A nil now defaults to time.Now.
func New(conversations ConversationSource, reports core.ReportsAPI, now func() time.Time) Service {
	if now == nil {
		now = time.Now
	}
	return &service{conversations: conversations, reports: reports, now: now}
}
