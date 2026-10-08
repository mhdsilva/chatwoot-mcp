package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/analytics"
	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

// fakeAnalytics records exactly one call per analytics method and returns the
// configured result or error, so tests assert the MCP adapter's input mapping
// and output bounding without touching the real service.
type fakeAnalytics struct {
	queueResult analytics.QueueResult
	queueErr    error
	queueReq    analytics.QueueRequest
	queueCalls  int

	summaryResult analytics.SummaryResult
	summaryErr    error
	summaryReq    analytics.SummaryRequest
	summaryCalls  int

	metricsResult analytics.ConversationMetricsResult
	metricsErr    error
	metricsID     int64
	metricsCalls  int

	compareResult analytics.ComparisonResult
	compareErr    error
	compareReq    analytics.CompareRequest
	compareCalls  int
}

func (f *fakeAnalytics) ListAttentionQueue(_ context.Context, request analytics.QueueRequest) (analytics.QueueResult, error) {
	f.queueCalls++
	f.queueReq = request
	return f.queueResult, f.queueErr
}

func (f *fakeAnalytics) GetSummary(_ context.Context, request analytics.SummaryRequest) (analytics.SummaryResult, error) {
	f.summaryCalls++
	f.summaryReq = request
	return f.summaryResult, f.summaryErr
}

func (f *fakeAnalytics) GetConversationMetrics(_ context.Context, id int64) (analytics.ConversationMetricsResult, error) {
	f.metricsCalls++
	f.metricsID = id
	return f.metricsResult, f.metricsErr
}

func (f *fakeAnalytics) ComparePerformance(_ context.Context, request analytics.CompareRequest) (analytics.ComparisonResult, error) {
	f.compareCalls++
	f.compareReq = request
	return f.compareResult, f.compareErr
}

func TestListAttentionQueueToolPassesFiltersAndPreservesScan(t *testing.T) {
	observed := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	activity := observed.Add(-2 * time.Hour)
	fake := &fakeAnalytics{queueResult: analytics.QueueResult{
		ObservedAt: observed,
		Conversations: []analytics.AttentionConversation{{
			ConversationID: 123,
			ContactID:      45,
			InboxID:        2,
			ChannelType:    "Channel::Whatsapp",
			Status:         "open",
			Priority:       "high",
			AssigneeID:     8,
			TeamID:         3,
			WaitingSince:   observed.Add(-90 * time.Minute),
			WaitingSeconds: 5400,
			LastActivityAt: &activity,
			UnreadCount:    2,
		}},
		ScannedPages:         3,
		ScannedConversations: 61,
		Complete:             false,
		NextPage:             5,
		Truncated:            true,
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_attention_queue",
		Arguments: map[string]any{
			"status": "pending", "inbox_id": 2, "team_id": 3,
			"assignee_id": 8, "limit": 30, "start_page": 2,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.queueCalls != 1 {
		t.Fatalf("queue calls = %d, want exactly 1", fake.queueCalls)
	}
	want := analytics.QueueRequest{Status: "pending", InboxID: 2, TeamID: 3, AssigneeID: 8, Limit: 30, StartPage: 2}
	if fake.queueReq != want {
		t.Fatalf("service request = %#v, want %#v", fake.queueReq, want)
	}
	data := structuredData(t, res)
	if data["observed_at"] == nil || data["complete"] != false || data["next_page"] != float64(5) || data["truncated"] != true {
		t.Fatalf("scan fields not preserved: %#v", data)
	}
	if data["scanned_pages"] != float64(3) || data["scanned_conversations"] != float64(61) {
		t.Fatalf("scan counters not preserved: %#v", data)
	}
	rows, _ := data["conversations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("conversations = %#v", data["conversations"])
	}
	row := rows[0].(map[string]any)
	if row["conversation_id"] != float64(123) || row["contact_id"] != float64(45) || row["waiting_seconds"] != float64(5400) || row["unread_count"] != float64(2) {
		t.Fatalf("queue row not preserved: %#v", row)
	}
	blob, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, forbidden := range []string{"\"content\"", "\"messages\"", "text_body"} {
		if strings.Contains(string(blob), forbidden) {
			t.Fatalf("queue output carries message-content field %s: %s", forbidden, blob)
		}
	}
}

func TestListAttentionQueueToolDefaultsToZeroRequest(t *testing.T) {
	fake := &fakeAnalytics{queueResult: analytics.QueueResult{Conversations: []analytics.AttentionConversation{}}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_attention_queue", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.queueReq != (analytics.QueueRequest{}) {
		t.Fatalf("service request = %#v, want the zero request so the service applies defaults", fake.queueReq)
	}
}

func TestListAttentionQueueToolTruncatesOversizedText(t *testing.T) {
	long := strings.Repeat("界", 400) // 1200 bytes, 3-byte runes
	fake := &fakeAnalytics{queueResult: analytics.QueueResult{
		Conversations: []analytics.AttentionConversation{{
			ConversationID: 1,
			ChannelType:    long,
			Status:         long,
			Priority:       long,
		}},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_attention_queue", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatal("text_truncated = false, want true for oversized queue text")
	}
	rows, _ := structuredData(t, res)["conversations"].([]any)
	row := rows[0].(map[string]any)
	for _, field := range []string{"channel_type", "status", "priority"} {
		value, _ := row[field].(string)
		if len(value) > MaxTextBytes {
			t.Fatalf("%s = %d bytes, want <= %d", field, len(value), MaxTextBytes)
		}
		if !utf8.ValidString(value) {
			t.Fatalf("%s is not valid UTF-8 after truncation", field)
		}
	}
}

func TestGetAnalyticsSummaryToolPassesRangeAndScope(t *testing.T) {
	since := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)
	currentCount := int64(120)
	currentResponse := 92.4
	fake := &fakeAnalytics{summaryResult: analytics.SummaryResult{
		Since: since, Until: until, Scope: "agent", ScopeID: 8,
		Current:  core.ReportMetrics{ConversationsCount: &currentCount, AvgFirstResponseSeconds: &currentResponse},
		Previous: core.ReportMetrics{},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_analytics_summary",
		Arguments: map[string]any{
			"since": "2026-10-01T00:00:00-03:00", "until": "2026-10-08T00:00:00-03:00",
			"scope": "agent", "scope_id": 8,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.summaryCalls != 1 {
		t.Fatalf("summary calls = %d, want exactly 1", fake.summaryCalls)
	}
	want := analytics.SummaryRequest{Since: "2026-10-01T00:00:00-03:00", Until: "2026-10-08T00:00:00-03:00", Scope: "agent", ScopeID: 8}
	if fake.summaryReq != want {
		t.Fatalf("service request = %#v, want %#v", fake.summaryReq, want)
	}
	data := structuredData(t, res)
	if data["scope"] != "agent" || data["scope_id"] != float64(8) {
		t.Fatalf("scope not preserved: %#v", data)
	}
	current, _ := data["current"].(map[string]any)
	if current["conversations_count"] != float64(120) || current["avg_first_response_time"] != float64(92.4) {
		t.Fatalf("current metrics not preserved: %#v", current)
	}
	if value, ok := current["resolutions_count"]; ok && value != nil {
		t.Fatalf("missing metric = %#v, want null", value)
	}
	previous, _ := data["previous"].(map[string]any)
	if previous["conversations_count"] != nil {
		t.Fatalf("missing previous metric = %#v, want null", previous["conversations_count"])
	}
}

func TestGetAnalyticsSummaryToolTruncatesScope(t *testing.T) {
	long := strings.Repeat("é", 400) // 800 bytes, 2-byte runes
	fake := &fakeAnalytics{summaryResult: analytics.SummaryResult{Scope: long}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_analytics_summary",
		Arguments: map[string]any{"since": "2026-10-01T00:00:00Z", "until": "2026-10-02T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatal("text_truncated = false, want true for an oversized scope")
	}
	scope, _ := structuredData(t, res)["scope"].(string)
	if len(scope) > MaxTextBytes || !utf8.ValidString(scope) {
		t.Fatalf("scope = %d bytes, want <= %d valid UTF-8", len(scope), MaxTextBytes)
	}
}

func TestGetConversationMetricsToolPreservesSecondsTotalsAndTruncation(t *testing.T) {
	firstResponse := 80.0
	resolution := 3600.0
	avgReply := 140.0
	fake := &fakeAnalytics{metricsResult: analytics.ConversationMetricsResult{
		ConversationID: 123,
		Summary: analytics.ConversationMetricsSummary{
			FirstResponseSeconds:         &firstResponse,
			FirstResponseBusinessSeconds: nil,
			ResolutionSeconds:            &resolution,
			ReplyEvents:                  4,
			AvgReplySeconds:              &avgReply,
		},
		Events: []core.ReportingEvent{{
			ID: 10, Name: "first_response", ValueSeconds: &firstResponse,
			EventStartTime: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			EventEndTime:   time.Date(2026, 10, 8, 12, 1, 20, 0, time.UTC),
			InboxID:        2, UserID: 8,
		}},
		TotalEvents: 60, ReturnedEvents: 50, Truncated: true,
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation_metrics",
		Arguments: map[string]any{"conversation_id": 123},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.metricsCalls != 1 || fake.metricsID != 123 {
		t.Fatalf("service call = (%d calls, id %d)", fake.metricsCalls, fake.metricsID)
	}
	data := structuredData(t, res)
	if data["conversation_id"] != float64(123) || data["total_events"] != float64(60) || data["returned_events"] != float64(50) || data["truncated"] != true {
		t.Fatalf("totals not preserved: %#v", data)
	}
	summary, _ := data["summary"].(map[string]any)
	if summary["first_response_seconds"] != float64(80) || summary["resolution_seconds"] != float64(3600) || summary["reply_events"] != float64(4) || summary["avg_reply_seconds"] != float64(140) {
		t.Fatalf("seconds not preserved: %#v", summary)
	}
	if value, ok := summary["first_response_business_seconds"]; ok && value != nil {
		t.Fatalf("missing business seconds = %#v, want null", value)
	}
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %#v", data["events"])
	}
	event := events[0].(map[string]any)
	if event["name"] != "first_response" || event["value_seconds"] != float64(80) || event["user_id"] != float64(8) {
		t.Fatalf("event not preserved: %#v", event)
	}
}

func TestGetConversationMetricsToolTruncatesOversizedEventName(t *testing.T) {
	long := strings.Repeat("界", 400)
	fake := &fakeAnalytics{metricsResult: analytics.ConversationMetricsResult{
		ConversationID: 123,
		Events:         []core.ReportingEvent{{ID: 1, Name: long}},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation_metrics",
		Arguments: map[string]any{"conversation_id": 123},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatal("text_truncated = false, want true for an oversized event name")
	}
	events, _ := structuredData(t, res)["events"].([]any)
	name, _ := events[0].(map[string]any)["name"].(string)
	if len(name) > MaxTextBytes || !utf8.ValidString(name) {
		t.Fatalf("event name = %d bytes, want <= %d valid UTF-8", len(name), MaxTextBytes)
	}
}

func TestComparePerformanceToolPassesArgumentsAndPreservesDeltas(t *testing.T) {
	absolute := 10.0
	percent := 12.5
	fake := &fakeAnalytics{compareResult: analytics.ComparisonResult{
		GroupBy: "agent",
		Rows: []analytics.ComparisonRow{{
			Key: "8", ID: 8, Name: "Ana",
			Delta: analytics.MetricDeltas{
				ConversationsCount: &analytics.MetricDelta{Absolute: absolute, Percent: &percent},
			},
		}},
		TotalRows: 30, ReturnedRows: 20, Truncated: true,
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "compare_performance",
		Arguments: map[string]any{
			"since": "2026-10-01T00:00:00-03:00", "until": "2026-10-08T00:00:00-03:00",
			"group_by": "agent", "limit": 20,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.compareCalls != 1 {
		t.Fatalf("compare calls = %d, want exactly 1", fake.compareCalls)
	}
	want := analytics.CompareRequest{Since: "2026-10-01T00:00:00-03:00", Until: "2026-10-08T00:00:00-03:00", GroupBy: "agent", Limit: 20}
	if fake.compareReq != want {
		t.Fatalf("service request = %#v, want %#v", fake.compareReq, want)
	}
	data := structuredData(t, res)
	if data["group_by"] != "agent" || data["total_rows"] != float64(30) || data["returned_rows"] != float64(20) || data["truncated"] != true {
		t.Fatalf("comparison fields not preserved: %#v", data)
	}
	if data["current_range"] == nil || data["previous_range"] == nil {
		t.Fatalf("ranges not preserved: %#v", data)
	}
	rows, _ := data["rows"].([]any)
	row := rows[0].(map[string]any)
	if row["key"] != "8" || row["id"] != float64(8) || row["name"] != "Ana" {
		t.Fatalf("row identity not preserved: %#v", row)
	}
	delta, _ := row["delta"].(map[string]any)
	conversations, _ := delta["conversations_count"].(map[string]any)
	if conversations["absolute"] != float64(10) || conversations["percent"] != float64(12.5) {
		t.Fatalf("delta not preserved: %#v", delta)
	}
}

func TestComparePerformanceToolTruncatesOversizedRowText(t *testing.T) {
	long := strings.Repeat("界", 400)
	fake := &fakeAnalytics{compareResult: analytics.ComparisonResult{
		Rows: []analytics.ComparisonRow{{Key: long, Name: long}},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "compare_performance",
		Arguments: map[string]any{"since": "2026-10-01T00:00:00Z", "until": "2026-10-02T00:00:00Z", "group_by": "agent"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatal("text_truncated = false, want true for oversized row text")
	}
	rows, _ := structuredData(t, res)["rows"].([]any)
	row := rows[0].(map[string]any)
	for _, field := range []string{"key", "name"} {
		value, _ := row[field].(string)
		if len(value) > MaxTextBytes || !utf8.ValidString(value) {
			t.Fatalf("%s = %d bytes, want <= %d valid UTF-8", field, len(value), MaxTextBytes)
		}
	}
}

func TestAnalyticsForbiddenErrorCarriesCodeWithoutLeaking(t *testing.T) {
	const secret = "super-secret-token-value"
	fake := &fakeAnalytics{queueErr: &analytics.Error{
		Code:    analytics.CodeForbidden,
		Message: "queue detail " + secret,
		APIError: &chatwoot.Error{
			Kind:       chatwoot.KindForbidden,
			StatusCode: 403,
			Message:    "upstream body: " + secret,
		},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_attention_queue", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for a forbidden analytics call")
	}
	e := structuredError(t, res)
	if e["code"] != string(analytics.CodeForbidden) {
		t.Fatalf("code = %v, want %q", e["code"], analytics.CodeForbidden)
	}
	if e["status_code"] != float64(403) {
		t.Fatalf("status_code = %v, want 403", e["status_code"])
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("analytics secret leaked into result: %s", blob)
	}
	if strings.Contains(resultText(res), secret) {
		t.Fatalf("analytics secret leaked into content text: %s", resultText(res))
	}
}

func TestAnalyticsUnsupportedFeatureCarriesFixedMessage(t *testing.T) {
	fake := &fakeAnalytics{summaryErr: &analytics.Error{Code: analytics.CodeUnsupportedFeature, Message: "Chatwoot reports are not configured"}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_analytics_summary",
		Arguments: map[string]any{"since": "2026-10-01T00:00:00Z", "until": "2026-10-02T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for an unsupported analytics feature")
	}
	e := structuredError(t, res)
	if e["code"] != string(analytics.CodeUnsupportedFeature) {
		t.Fatalf("code = %v, want %q", e["code"], analytics.CodeUnsupportedFeature)
	}
	want := "this analytics feature is not available in the configured Chatwoot version"
	if e["message"] != want {
		t.Fatalf("message = %q, want the fixed %q", e["message"], want)
	}
}

func TestAnalyticsInvalidResponseSurfacesSafeDiagnostics(t *testing.T) {
	const secret = "sensitive-upstream-payload"
	fake := &fakeAnalytics{metricsErr: &analytics.Error{
		Code: analytics.CodeInvalidResponse,
		APIError: &chatwoot.Error{
			Kind:               chatwoot.KindInvalid,
			StatusCode:         200,
			Message:            "unexpected body: " + secret,
			ResponseFormat:     "json",
			ResponseDecodeKind: "unexpected_json_type",
			ResponseField:      "payload",
			ExpectedJSONType:   "array",
			ActualJSONType:     "object",
		},
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation_metrics",
		Arguments: map[string]any{"conversation_id": 123},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for an invalid analytics response")
	}
	e := structuredError(t, res)
	if e["code"] != string(analytics.CodeInvalidResponse) || e["status_code"] != float64(200) || e["response_format"] != "json" || e["decode_error"] != "unexpected_json_type" || e["field"] != "payload" || e["expected_json_type"] != "array" || e["actual_json_type"] != "object" {
		t.Fatalf("error = %#v, want invalid_response with sanitized diagnostics", e)
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("upstream body leaked into result: %s", blob)
	}
}

func TestAnalyticsUpstreamMessageNeverEchoed(t *testing.T) {
	const secret = "super-secret-token-value"
	fake := &fakeAnalytics{compareErr: &analytics.Error{
		Code:    analytics.CodeUpstream,
		Message: "upstream said: " + secret + " " + strings.Repeat("界", 1000),
	}}
	session, ctx := connectSession(t, &fakeService{}, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "compare_performance",
		Arguments: map[string]any{"since": "2026-10-01T00:00:00Z", "until": "2026-10-02T00:00:00Z", "group_by": "agent"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	e := structuredError(t, res)
	if e["code"] != string(analytics.CodeUpstream) {
		t.Fatalf("code = %v, want %q", e["code"], analytics.CodeUpstream)
	}
	message, _ := e["message"].(string)
	if message != fixedErrorMessage(string(analytics.CodeUpstream)) {
		t.Fatalf("message = %q, want the fixed message", message)
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("upstream secret leaked into result: %s", blob)
	}
}
