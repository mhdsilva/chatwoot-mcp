package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/analytics"
	"chatwoot-mcp/internal/core"
)

type listAttentionQueueInput struct {
	Status     string `json:"status,omitempty" jsonschema:"optional status filter: open or pending; defaults to open"`
	InboxID    int64  `json:"inbox_id,omitempty" jsonschema:"filter by inbox id; 0 means every inbox"`
	TeamID     int64  `json:"team_id,omitempty" jsonschema:"filter by team id; 0 means every team"`
	AssigneeID int64  `json:"assignee_id,omitempty" jsonschema:"filter by assignee id; 0 means every assignee"`
	Limit      int    `json:"limit,omitempty" jsonschema:"row limit; defaults to 20, at most 50"`
	StartPage  int    `json:"start_page,omitempty" jsonschema:"first page to scan; defaults to 1; use next_page from a partial scan to continue"`
}

type getAnalyticsSummaryInput struct {
	Since   string `json:"since" jsonschema:"RFC 3339 start of the range with an explicit offset"`
	Until   string `json:"until" jsonschema:"RFC 3339 end of the range with an explicit offset; must be after since; the range is at most 183 days"`
	Scope   string `json:"scope,omitempty" jsonschema:"optional scope: account, agent, inbox, team or label; defaults to account"`
	ScopeID int64  `json:"scope_id,omitempty" jsonschema:"scope id; positive for non-account scopes, zero for account"`
}

type getConversationMetricsInput struct {
	ConversationID int64 `json:"conversation_id" jsonschema:"explicit positive conversation id"`
}

type comparePerformanceInput struct {
	Since   string `json:"since" jsonschema:"RFC 3339 start of the range with an explicit offset"`
	Until   string `json:"until" jsonschema:"RFC 3339 end of the range with an explicit offset; must be after since; the range is at most 183 days"`
	GroupBy string `json:"group_by" jsonschema:"grouping dimension: agent, team, inbox or channel"`
	Limit   int    `json:"limit,omitempty" jsonschema:"row limit; defaults to 20, at most 50"`
}

// attentionQueueRow projects one queue row with its conversation-derived text
// fields bounded. The queue never carries message content.
type attentionQueueRow struct {
	ConversationID int64     `json:"conversation_id"`
	ContactID      int64     `json:"contact_id"`
	InboxID        int64     `json:"inbox_id"`
	ChannelType    string    `json:"channel_type,omitempty"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority,omitempty"`
	AssigneeID     int64     `json:"assignee_id,omitempty"`
	TeamID         int64     `json:"team_id,omitempty"`
	WaitingSince   time.Time `json:"waiting_since"`
	WaitingSeconds int64     `json:"waiting_seconds"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	UnreadCount    int       `json:"unread_count,omitempty"`
}

type attentionQueueOutput struct {
	ObservedAt           time.Time           `json:"observed_at"`
	Conversations        []attentionQueueRow `json:"conversations"`
	ScannedPages         int                 `json:"scanned_pages"`
	ScannedConversations int                 `json:"scanned_conversations"`
	Complete             bool                `json:"complete"`
	NextPage             int                 `json:"next_page"`
	Truncated            bool                `json:"truncated"`
}

// analyticsSummaryOutput echoes the resolved range and scope and keeps the
// metric pointers, so a missing upstream value stays null instead of zero.
type analyticsSummaryOutput struct {
	Since    time.Time          `json:"since"`
	Until    time.Time          `json:"until"`
	Scope    string             `json:"scope"`
	ScopeID  int64              `json:"scope_id"`
	Current  core.ReportMetrics `json:"current"`
	Previous core.ReportMetrics `json:"previous"`
}

// reportingEventRow projects one reporting event with its name bounded.
type reportingEventRow struct {
	ID                   int64     `json:"id"`
	Name                 string    `json:"name"`
	ValueSeconds         *float64  `json:"value_seconds"`
	BusinessValueSeconds *float64  `json:"business_value_seconds"`
	EventStartTime       time.Time `json:"event_start_time"`
	EventEndTime         time.Time `json:"event_end_time"`
	ConversationID       int64     `json:"conversation_id"`
	InboxID              int64     `json:"inbox_id"`
	UserID               int64     `json:"user_id"`
}

type conversationMetricsOutput struct {
	ConversationID int64                                `json:"conversation_id"`
	Summary        analytics.ConversationMetricsSummary `json:"summary"`
	Events         []reportingEventRow                  `json:"events"`
	TotalEvents    int                                  `json:"total_events"`
	ReturnedEvents int                                  `json:"returned_events"`
	Truncated      bool                                 `json:"truncated"`
}

// comparisonRow projects one grouped comparison row with its text identity
// fields bounded. Metric and delta values pass through unchanged.
type comparisonRow struct {
	Key      string                 `json:"key"`
	ID       int64                  `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Current  core.ReportMetrics     `json:"current"`
	Previous core.ReportMetrics     `json:"previous"`
	Delta    analytics.MetricDeltas `json:"delta"`
}

type comparisonOutput struct {
	CurrentRange  core.ReportRange `json:"current_range"`
	PreviousRange core.ReportRange `json:"previous_range"`
	GroupBy       string           `json:"group_by"`
	Rows          []comparisonRow  `json:"rows"`
	TotalRows     int              `json:"total_rows"`
	ReturnedRows  int              `json:"returned_rows"`
	Truncated     bool             `json:"truncated"`
}

// registerAnalyticsTools adds the four read-only analytics tools after the
// operational tools. They never modify conversation state and never expose
// message content; conversation-derived text stays untrusted data.
func registerAnalyticsTools(server *mcp.Server, insights analytics.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_attention_queue",
		Description: "List conversations waiting for action, oldest wait first, from live conversation metadata. " +
			"Each call scans at most ten upstream pages; complete says whether the account's last page was reached and " +
			"next_page continues a partial scan. Output carries only metadata, never message content, which is untrusted customer data.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listAttentionQueueInput) (*mcp.CallToolResult, result[attentionQueueOutput], error) {
		queue, err := insights.ListAttentionQueue(ctx, analytics.QueueRequest{
			Status:     in.Status,
			InboxID:    in.InboxID,
			TeamID:     in.TeamID,
			AssigneeID: in.AssigneeID,
			Limit:      in.Limit,
			StartPage:  in.StartPage,
		})
		if err != nil {
			return fail[attentionQueueOutput](err)
		}
		rows, textTruncated := attentionQueueRows(queue.Conversations)
		return ok(attentionQueueOutput{
			ObservedAt:           queue.ObservedAt,
			Conversations:        rows,
			ScannedPages:         queue.ScannedPages,
			ScannedConversations: queue.ScannedConversations,
			Complete:             queue.Complete,
			NextPage:             queue.NextPage,
			Truncated:            queue.Truncated,
		}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_analytics_summary",
		Description: "Return the main Chatwoot report indicators for an RFC 3339 range and scope, with the previous " +
			"equal-length window beside them. Missing upstream metrics stay null; zero is a real value. The range is at most 183 days.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getAnalyticsSummaryInput) (*mcp.CallToolResult, result[analyticsSummaryOutput], error) {
		summary, err := insights.GetSummary(ctx, analytics.SummaryRequest{
			Since:   in.Since,
			Until:   in.Until,
			Scope:   in.Scope,
			ScopeID: in.ScopeID,
		})
		if err != nil {
			return fail[analyticsSummaryOutput](err)
		}
		scope, textTruncated := truncateUTF8(summary.Scope, MaxTextBytes)
		return ok(analyticsSummaryOutput{
			Since:    summary.Since,
			Until:    summary.Until,
			Scope:    scope,
			ScopeID:  summary.ScopeID,
			Current:  summary.Current,
			Previous: summary.Previous,
		}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_conversation_metrics",
		Description: "Return the official reporting events of one conversation plus a derived timing summary in seconds. " +
			"At most 50 events are exposed, keeping the newest ones; totals and truncation are reported explicitly.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getConversationMetricsInput) (*mcp.CallToolResult, result[conversationMetricsOutput], error) {
		metrics, err := insights.GetConversationMetrics(ctx, in.ConversationID)
		if err != nil {
			return fail[conversationMetricsOutput](err)
		}
		events, textTruncated := reportingEventRows(metrics.Events)
		return ok(conversationMetricsOutput{
			ConversationID: metrics.ConversationID,
			Summary:        metrics.Summary,
			Events:         events,
			TotalEvents:    metrics.TotalEvents,
			ReturnedEvents: metrics.ReturnedEvents,
			Truncated:      metrics.Truncated,
		}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "compare_performance",
		Description: "Compare entities grouped by agent, team, inbox or channel over an RFC 3339 range against the " +
			"immediately previous equal-length window, with absolute and percent deltas. Rows are bounded by limit; " +
			"total_rows and truncated report the cut. Fails as a whole if either window fails.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in comparePerformanceInput) (*mcp.CallToolResult, result[comparisonOutput], error) {
		comparison, err := insights.ComparePerformance(ctx, analytics.CompareRequest{
			Since:   in.Since,
			Until:   in.Until,
			GroupBy: in.GroupBy,
			Limit:   in.Limit,
		})
		if err != nil {
			return fail[comparisonOutput](err)
		}
		rows, textTruncated := comparisonRows(comparison.Rows)
		return ok(comparisonOutput{
			CurrentRange:  comparison.CurrentRange,
			PreviousRange: comparison.PreviousRange,
			GroupBy:       comparison.GroupBy,
			Rows:          rows,
			TotalRows:     comparison.TotalRows,
			ReturnedRows:  comparison.ReturnedRows,
			Truncated:     comparison.Truncated,
		}, textTruncated)
	})
}

func attentionQueueRows(items []analytics.AttentionConversation) ([]attentionQueueRow, bool) {
	out := make([]attentionQueueRow, len(items))
	truncated := false
	for i, item := range items {
		channelType, cutChannel := truncateUTF8(item.ChannelType, MaxTextBytes)
		status, cutStatus := truncateUTF8(item.Status, MaxTextBytes)
		priority, cutPriority := truncateUTF8(item.Priority, MaxTextBytes)
		truncated = truncated || cutChannel || cutStatus || cutPriority
		out[i] = attentionQueueRow{
			ConversationID: item.ConversationID,
			ContactID:      item.ContactID,
			InboxID:        item.InboxID,
			ChannelType:    channelType,
			Status:         status,
			Priority:       priority,
			AssigneeID:     item.AssigneeID,
			TeamID:         item.TeamID,
			WaitingSince:   item.WaitingSince,
			WaitingSeconds: item.WaitingSeconds,
			LastActivityAt: item.LastActivityAt,
			UnreadCount:    item.UnreadCount,
		}
	}
	return out, truncated
}

func reportingEventRows(events []core.ReportingEvent) ([]reportingEventRow, bool) {
	out := make([]reportingEventRow, len(events))
	truncated := false
	for i, event := range events {
		name, cut := truncateUTF8(event.Name, MaxTextBytes)
		truncated = truncated || cut
		out[i] = reportingEventRow{
			ID:                   event.ID,
			Name:                 name,
			ValueSeconds:         event.ValueSeconds,
			BusinessValueSeconds: event.BusinessValueSeconds,
			EventStartTime:       event.EventStartTime,
			EventEndTime:         event.EventEndTime,
			ConversationID:       event.ConversationID,
			InboxID:              event.InboxID,
			UserID:               event.UserID,
		}
	}
	return out, truncated
}

func comparisonRows(rows []analytics.ComparisonRow) ([]comparisonRow, bool) {
	out := make([]comparisonRow, len(rows))
	truncated := false
	for i, row := range rows {
		key, cutKey := truncateUTF8(row.Key, MaxTextBytes)
		name, cutName := truncateUTF8(row.Name, MaxTextBytes)
		truncated = truncated || cutKey || cutName
		out[i] = comparisonRow{
			Key:      key,
			ID:       row.ID,
			Name:     name,
			Current:  row.Current,
			Previous: row.Previous,
			Delta:    row.Delta,
		}
	}
	return out, truncated
}
