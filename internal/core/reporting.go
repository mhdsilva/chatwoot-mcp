package core

import (
	"context"
	"time"
)

// ReportScope selects the entity a report is computed against.
type ReportScope string

const (
	ReportScopeAccount ReportScope = "account"
	ReportScopeAgent   ReportScope = "agent"
	ReportScopeInbox   ReportScope = "inbox"
	ReportScopeTeam    ReportScope = "team"
	ReportScopeLabel   ReportScope = "label"
)

// ReportGroup selects the grouping dimension of a grouped report.
type ReportGroup string

const (
	ReportGroupAgent   ReportGroup = "agent"
	ReportGroupTeam    ReportGroup = "team"
	ReportGroupInbox   ReportGroup = "inbox"
	ReportGroupChannel ReportGroup = "channel"
)

// ReportRange bounds a report window in absolute time.
type ReportRange struct {
	Since time.Time
	Until time.Time
}

// ReportSummaryRequest asks for the current/previous metric summary of one scope.
type ReportSummaryRequest struct {
	Range   ReportRange
	Scope   ReportScope
	ScopeID int64
}

// GroupedReportRequest asks for metrics grouped by one dimension.
type GroupedReportRequest struct {
	Range ReportRange
	Group ReportGroup
}

// ReportMetrics holds the reporting metric values. Pointers distinguish
// "absent from the API response" from a real zero.
type ReportMetrics struct {
	ConversationsCount      *int64   `json:"conversations_count"`
	IncomingMessagesCount   *int64   `json:"incoming_messages_count"`
	OutgoingMessagesCount   *int64   `json:"outgoing_messages_count"`
	ResolutionsCount        *int64   `json:"resolutions_count"`
	OpenCount               *int64   `json:"open_count"`
	PendingCount            *int64   `json:"pending_count"`
	SnoozedCount            *int64   `json:"snoozed_count"`
	AvgFirstResponseSeconds *float64 `json:"avg_first_response_time"`
	AvgResolutionSeconds    *float64 `json:"avg_resolution_time"`
	AvgReplySeconds         *float64 `json:"avg_reply_time"`
}

// ReportSummary pairs the current window metrics with the previous window.
type ReportSummary struct {
	Current  ReportMetrics `json:"current"`
	Previous ReportMetrics `json:"previous"`
}

// ReportingEvent is one conversation reporting event row.
type ReportingEvent struct {
	ID                   int64     `json:"id"`
	Name                 string    `json:"name"`
	ValueSeconds         *float64  `json:"value"`
	BusinessValueSeconds *float64  `json:"business_value"`
	EventStartTime       time.Time `json:"event_start_time"`
	EventEndTime         time.Time `json:"event_end_time"`
	ConversationID       int64     `json:"conversation_id"`
	InboxID              int64     `json:"inbox_id"`
	UserID               int64     `json:"user_id"`
}

// GroupedReportRow is one grouped-report bucket.
type GroupedReportRow struct {
	Key     string
	ID      int64
	Name    string
	Metrics ReportMetrics
}

// ReportsAPI is the reporting surface the analytics module consumes.
type ReportsAPI interface {
	GetReportSummary(context.Context, ReportSummaryRequest) (ReportSummary, error)
	GetConversationReportingEvents(context.Context, int64) ([]ReportingEvent, error)
	GetGroupedReport(context.Context, GroupedReportRequest) ([]GroupedReportRow, error)
}
