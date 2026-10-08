package analytics

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	"chatwoot-mcp/internal/core"
)

const (
	maxReportRange        = 183 * 24 * time.Hour
	defaultCompareLimit   = 20
	maxCompareLimit       = 50
	maxConversationEvents = 50
)

type SummaryRequest struct {
	Since   string `json:"since"`
	Until   string `json:"until"`
	Scope   string `json:"scope,omitempty"`
	ScopeID int64  `json:"scope_id,omitempty"`
}

type SummaryResult struct {
	Since    time.Time          `json:"since"`
	Until    time.Time          `json:"until"`
	Scope    string             `json:"scope"`
	ScopeID  int64              `json:"scope_id"`
	Current  core.ReportMetrics `json:"current"`
	Previous core.ReportMetrics `json:"previous"`
}

type ConversationMetricsSummary struct {
	FirstResponseSeconds         *float64 `json:"first_response_seconds,omitempty"`
	FirstResponseBusinessSeconds *float64 `json:"first_response_business_seconds,omitempty"`
	ResolutionSeconds            *float64 `json:"resolution_seconds,omitempty"`
	ResolutionBusinessSeconds    *float64 `json:"resolution_business_seconds,omitempty"`
	ReplyEvents                  int      `json:"reply_events"`
	AvgReplySeconds              *float64 `json:"avg_reply_seconds,omitempty"`
}

type ConversationMetricsResult struct {
	ConversationID int64                      `json:"conversation_id"`
	Summary        ConversationMetricsSummary `json:"summary"`
	Events         []core.ReportingEvent      `json:"events"`
	TotalEvents    int                        `json:"total_events"`
	ReturnedEvents int                        `json:"returned_events"`
	Truncated      bool                       `json:"truncated"`
}

type CompareRequest struct {
	Since   string `json:"since"`
	Until   string `json:"until"`
	GroupBy string `json:"group_by"`
	Limit   int    `json:"limit,omitempty"`
}

type MetricDelta struct {
	Absolute float64  `json:"absolute"`
	Percent  *float64 `json:"percent,omitempty"`
}

type MetricDeltas struct {
	ConversationsCount      *MetricDelta `json:"conversations_count,omitempty"`
	IncomingMessagesCount   *MetricDelta `json:"incoming_messages_count,omitempty"`
	OutgoingMessagesCount   *MetricDelta `json:"outgoing_messages_count,omitempty"`
	ResolutionsCount        *MetricDelta `json:"resolutions_count,omitempty"`
	OpenCount               *MetricDelta `json:"open_count,omitempty"`
	PendingCount            *MetricDelta `json:"pending_count,omitempty"`
	SnoozedCount            *MetricDelta `json:"snoozed_count,omitempty"`
	AvgFirstResponseSeconds *MetricDelta `json:"avg_first_response_seconds,omitempty"`
	AvgResolutionSeconds    *MetricDelta `json:"avg_resolution_seconds,omitempty"`
	AvgReplySeconds         *MetricDelta `json:"avg_reply_seconds,omitempty"`
}

type ComparisonRow struct {
	Key      string             `json:"key"`
	ID       int64              `json:"id,omitempty"`
	Name     string             `json:"name,omitempty"`
	Current  core.ReportMetrics `json:"current"`
	Previous core.ReportMetrics `json:"previous"`
	Delta    MetricDeltas       `json:"delta"`
}

type ComparisonResult struct {
	CurrentRange  core.ReportRange `json:"current_range"`
	PreviousRange core.ReportRange `json:"previous_range"`
	GroupBy       string           `json:"group_by"`
	Rows          []ComparisonRow  `json:"rows"`
	TotalRows     int              `json:"total_rows"`
	ReturnedRows  int              `json:"returned_rows"`
	Truncated     bool             `json:"truncated"`
}

func (s *service) GetSummary(ctx context.Context, request SummaryRequest) (SummaryResult, error) {
	rangeValue, err := parseReportRange(request.Since, request.Until)
	if err != nil {
		return SummaryResult{}, err
	}
	scope := core.ReportScope(request.Scope)
	if scope == "" {
		scope = core.ReportScopeAccount
	}
	switch scope {
	case core.ReportScopeAccount:
		if request.ScopeID != 0 {
			return SummaryResult{}, invalidInput("scope_id must be zero for account scope")
		}
	case core.ReportScopeAgent, core.ReportScopeInbox, core.ReportScopeTeam, core.ReportScopeLabel:
		if request.ScopeID <= 0 {
			return SummaryResult{}, invalidInput("scope_id must be positive for non-account scope")
		}
	default:
		return SummaryResult{}, invalidInput("scope must be account, agent, inbox, team, or label")
	}
	if s.reports == nil {
		return SummaryResult{}, &Error{Code: CodeUnsupportedFeature, Message: "Chatwoot reports are not configured"}
	}
	result, err := s.reports.GetReportSummary(ctx, core.ReportSummaryRequest{Range: rangeValue, Scope: scope, ScopeID: request.ScopeID})
	if err != nil {
		return SummaryResult{}, mapReportError(err, true)
	}
	return SummaryResult{Since: rangeValue.Since, Until: rangeValue.Until, Scope: string(scope), ScopeID: request.ScopeID, Current: result.Current, Previous: result.Previous}, nil
}

func (s *service) GetConversationMetrics(ctx context.Context, id int64) (ConversationMetricsResult, error) {
	if id <= 0 {
		return ConversationMetricsResult{}, invalidInput("conversation_id must be positive")
	}
	if s.reports == nil {
		return ConversationMetricsResult{}, &Error{Code: CodeUnsupportedFeature, Message: "Chatwoot reports are not configured"}
	}
	events, err := s.reports.GetConversationReportingEvents(ctx, id)
	if err != nil {
		return ConversationMetricsResult{}, mapReportError(err, false)
	}
	summary := ConversationMetricsSummary{}
	replyTotal := 0.0
	replyValueCount := 0
	for _, event := range events {
		switch event.Name {
		case "first_response":
			if summary.FirstResponseSeconds == nil {
				summary.FirstResponseSeconds = event.ValueSeconds
				summary.FirstResponseBusinessSeconds = event.BusinessValueSeconds
			}
		case "resolution":
			summary.ResolutionSeconds = event.ValueSeconds
			summary.ResolutionBusinessSeconds = event.BusinessValueSeconds
		case "reply_time":
			summary.ReplyEvents++
			if event.ValueSeconds != nil {
				replyTotal += *event.ValueSeconds
				replyValueCount++
			}
		}
	}
	if replyValueCount > 0 {
		average := replyTotal / float64(replyValueCount)
		summary.AvgReplySeconds = &average
	}
	retained := events
	truncated := len(events) > maxConversationEvents
	if truncated {
		retained = events[len(events)-maxConversationEvents:]
	}
	if retained == nil {
		retained = []core.ReportingEvent{}
	}
	return ConversationMetricsResult{ConversationID: id, Summary: summary, Events: retained, TotalEvents: len(events), ReturnedEvents: len(retained), Truncated: truncated}, nil
}

func (s *service) ComparePerformance(ctx context.Context, request CompareRequest) (ComparisonResult, error) {
	currentRange, err := parseReportRange(request.Since, request.Until)
	if err != nil {
		return ComparisonResult{}, err
	}
	group := core.ReportGroup(request.GroupBy)
	switch group {
	case core.ReportGroupAgent, core.ReportGroupTeam, core.ReportGroupInbox, core.ReportGroupChannel:
	default:
		return ComparisonResult{}, invalidInput("group_by must be agent, team, inbox, or channel")
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultCompareLimit
	}
	if limit < 0 {
		return ComparisonResult{}, invalidInput("limit must not be negative")
	}
	if limit > maxCompareLimit {
		limit = maxCompareLimit
	}
	if s.reports == nil {
		return ComparisonResult{}, &Error{Code: CodeUnsupportedFeature, Message: "Chatwoot reports are not configured"}
	}
	duration := currentRange.Until.Sub(currentRange.Since)
	previousRange := core.ReportRange{Since: currentRange.Since.Add(-duration), Until: currentRange.Since}
	currentRows, err := s.reports.GetGroupedReport(ctx, core.GroupedReportRequest{Range: currentRange, Group: group})
	if err != nil {
		return ComparisonResult{}, mapReportError(err, true)
	}
	previousRows, err := s.reports.GetGroupedReport(ctx, core.GroupedReportRequest{Range: previousRange, Group: group})
	if err != nil {
		return ComparisonResult{}, mapReportError(err, true)
	}
	rows, err := joinComparisonRows(currentRows, previousRows)
	if err != nil {
		return ComparisonResult{}, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	total := len(rows)
	truncated := total > limit
	if truncated {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []ComparisonRow{}
	}
	return ComparisonResult{CurrentRange: currentRange, PreviousRange: previousRange, GroupBy: string(group), Rows: rows, TotalRows: total, ReturnedRows: len(rows), Truncated: truncated}, nil
}

func parseReportRange(sinceValue, untilValue string) (core.ReportRange, error) {
	since, err := time.Parse(time.RFC3339, sinceValue)
	if err != nil {
		return core.ReportRange{}, invalidInput("since must be RFC 3339 with an explicit offset")
	}
	until, err := time.Parse(time.RFC3339, untilValue)
	if err != nil {
		return core.ReportRange{}, invalidInput("until must be RFC 3339 with an explicit offset")
	}
	if !since.Before(until) {
		return core.ReportRange{}, invalidInput("since must be before until")
	}
	duration := until.Sub(since)
	if duration > maxReportRange {
		return core.ReportRange{}, invalidInput("range must not exceed 183 days")
	}
	return core.ReportRange{Since: since.UTC(), Until: until.UTC()}, nil
}

func joinComparisonRows(currentRows, previousRows []core.GroupedReportRow) ([]ComparisonRow, error) {
	current := make(map[string]core.GroupedReportRow, len(currentRows))
	previous := make(map[string]core.GroupedReportRow, len(previousRows))
	if err := indexGroupedRows(current, currentRows); err != nil {
		return nil, err
	}
	if err := indexGroupedRows(previous, previousRows); err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(current)+len(previous))
	for key := range current {
		keys[key] = struct{}{}
	}
	for key := range previous {
		keys[key] = struct{}{}
	}
	rows := make([]ComparisonRow, 0, len(keys))
	for key := range keys {
		cur, hasCurrent := current[key]
		prev, hasPrevious := previous[key]
		var curMetrics, prevMetrics core.ReportMetrics
		if hasCurrent {
			curMetrics = cur.Metrics
		}
		if hasPrevious {
			prevMetrics = prev.Metrics
		}
		name := cur.Name
		if name == "" {
			name = prev.Name
		}
		id := cur.ID
		if id == 0 {
			id = prev.ID
		}
		rows = append(rows, ComparisonRow{Key: key, ID: id, Name: name, Current: curMetrics, Previous: prevMetrics, Delta: metricDeltas(curMetrics, prevMetrics)})
	}
	return rows, nil
}

func indexGroupedRows(target map[string]core.GroupedReportRow, rows []core.GroupedReportRow) error {
	for _, row := range rows {
		key := row.Key
		if key == "" && row.ID != 0 {
			key = strconv.FormatInt(row.ID, 10)
		}
		if key == "" {
			return invalidInput("grouped report row has no stable key or ID")
		}
		row.Key = key
		target[key] = row
	}
	return nil
}

func metricDeltas(current, previous core.ReportMetrics) MetricDeltas {
	return MetricDeltas{
		ConversationsCount: deltaInt(current.ConversationsCount, previous.ConversationsCount), IncomingMessagesCount: deltaInt(current.IncomingMessagesCount, previous.IncomingMessagesCount), OutgoingMessagesCount: deltaInt(current.OutgoingMessagesCount, previous.OutgoingMessagesCount),
		ResolutionsCount: deltaInt(current.ResolutionsCount, previous.ResolutionsCount), OpenCount: deltaInt(current.OpenCount, previous.OpenCount), PendingCount: deltaInt(current.PendingCount, previous.PendingCount), SnoozedCount: deltaInt(current.SnoozedCount, previous.SnoozedCount),
		AvgFirstResponseSeconds: deltaFloat(current.AvgFirstResponseSeconds, previous.AvgFirstResponseSeconds), AvgResolutionSeconds: deltaFloat(current.AvgResolutionSeconds, previous.AvgResolutionSeconds), AvgReplySeconds: deltaFloat(current.AvgReplySeconds, previous.AvgReplySeconds),
	}
}

func deltaInt(current, previous *int64) *MetricDelta {
	if current == nil || previous == nil {
		return nil
	}
	return makeDelta(float64(*current), float64(*previous))
}
func deltaFloat(current, previous *float64) *MetricDelta {
	if current == nil || previous == nil {
		return nil
	}
	return makeDelta(*current, *previous)
}
func makeDelta(current, previous float64) *MetricDelta {
	absolute := current - previous
	var percent *float64
	if previous != 0 {
		value := absolute / previous * 100
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			percent = &value
		}
	}
	return &MetricDelta{Absolute: absolute, Percent: percent}
}
