package analytics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

type reportsFake struct {
	summary         core.ReportSummary
	summaryErr      error
	events          []core.ReportingEvent
	eventsErr       error
	groups          map[core.ReportGroup][]core.GroupedReportRow
	groupErrs       []error
	summaryCalls    int
	eventCalls      int
	groupCalls      []core.GroupedReportRequest
	summaryRequests []core.ReportSummaryRequest
}

func (f *reportsFake) GetReportSummary(_ context.Context, req core.ReportSummaryRequest) (core.ReportSummary, error) {
	f.summaryCalls++
	f.summaryRequests = append(f.summaryRequests, req)
	return f.summary, f.summaryErr
}
func (f *reportsFake) GetConversationReportingEvents(context.Context, int64) ([]core.ReportingEvent, error) {
	f.eventCalls++
	return f.events, f.eventsErr
}
func (f *reportsFake) GetGroupedReport(_ context.Context, req core.GroupedReportRequest) ([]core.GroupedReportRow, error) {
	f.groupCalls = append(f.groupCalls, req)
	if len(f.groupErrs) > 0 {
		err := f.groupErrs[0]
		f.groupErrs = f.groupErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return f.groups[req.Group], nil
}

func TestSummaryValidatesRangeAndScopeBeforeCallingAPI(t *testing.T) {
	fake := &reportsFake{}
	service := New(nil, fake, nil)
	validSince, validUntil := "2026-10-01T00:00:00-03:00", "2026-10-02T00:00:00-03:00"
	for _, tc := range []struct {
		name                string
		since, until, scope string
		scopeID             int64
	}{
		{"malformed", "yesterday", validUntil, "account", 0},
		{"missing offset", "2026-10-01T00:00:00", "2026-10-02T00:00:00", "account", 0},
		{"equal", validSince, validSince, "account", 0},
		{"reversed", validUntil, validSince, "account", 0},
		{"over max", "2026-01-01T00:00:00Z", "2026-07-03T00:00:01Z", "account", 0},
		{"bad scope", validSince, validUntil, "user", 1},
		{"account id", validSince, validUntil, "account", 2},
		{"missing scoped id", validSince, validUntil, "agent", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.GetSummary(context.Background(), SummaryRequest{Since: tc.since, Until: tc.until, Scope: tc.scope, ScopeID: tc.scopeID})
			if CodeOf(err) != CodeInvalidInput {
				t.Fatalf("error = %v", err)
			}
			if fake.summaryCalls != 0 {
				t.Fatalf("API calls = %d", fake.summaryCalls)
			}
		})
	}
}

func TestSummaryNormalizesAndPreservesOptionalMetrics(t *testing.T) {
	count := int64(3)
	avg := 92.4
	fake := &reportsFake{summary: core.ReportSummary{Current: core.ReportMetrics{ConversationsCount: &count, AvgFirstResponseSeconds: &avg}}}
	service := New(nil, fake, nil)
	got, err := service.GetSummary(context.Background(), SummaryRequest{Since: "2026-10-01T00:00:00-03:00", Until: "2026-10-02T00:00:00-03:00"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Since.Location() != time.UTC || got.Until.Location() != time.UTC || got.Since.Format(time.RFC3339) != "2026-10-01T03:00:00Z" || got.Scope != "account" || got.ScopeID != 0 {
		t.Fatalf("normalized result = %#v", got)
	}
	if got.Current.ConversationsCount != &count || got.Current.AvgFirstResponseSeconds != &avg || got.Current.OutgoingMessagesCount != nil {
		t.Fatalf("metrics = %#v", got.Current)
	}
	req := fake.summaryRequests[0]
	if req.Scope != core.ReportScopeAccount || req.ScopeID != 0 || !req.Range.Since.Equal(got.Since) {
		t.Fatalf("API request = %#v", req)
	}
}

func TestConversationMetricsAggregateAllAndReturnNewest50(t *testing.T) {
	fake := &reportsFake{}
	for i := 0; i < 60; i++ {
		name := "reply_time"
		value := float64(i)
		if i == 1 || i == 2 {
			name = "first_response"
		}
		if i == 3 || i == 4 {
			name = "resolution"
		}
		fake.events = append(fake.events, core.ReportingEvent{ID: int64(i + 1), Name: name, ValueSeconds: &value, BusinessValueSeconds: &value, EventStartTime: time.Unix(int64(i), 0).UTC()})
	}
	service := New(nil, fake, nil)
	got, err := service.GetConversationMetrics(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalEvents != 60 || got.ReturnedEvents != 50 || !got.Truncated || got.Events[0].ID != 11 || got.Events[49].ID != 60 {
		t.Fatalf("counts/retained = %d/%d/%v %v..%v", got.TotalEvents, got.ReturnedEvents, got.Truncated, got.Events[0].ID, got.Events[49].ID)
	}
	if got.Summary.FirstResponseSeconds == nil || *got.Summary.FirstResponseSeconds != 1 || got.Summary.ResolutionSeconds == nil || *got.Summary.ResolutionSeconds != 4 || got.Summary.ReplyEvents != 56 {
		t.Fatalf("summary = %#v", got.Summary)
	}
	if got.Summary.AvgReplySeconds == nil || *got.Summary.AvgReplySeconds != (1760.0/56) {
		t.Fatalf("avg reply = %v", got.Summary.AvgReplySeconds)
	}
}

func TestConversationMetricsEmptyAndInvalidID(t *testing.T) {
	fake := &reportsFake{}
	service := New(nil, fake, nil)
	got, err := service.GetConversationMetrics(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalEvents != 0 || got.Events == nil || got.Summary.AvgReplySeconds != nil {
		t.Fatalf("empty result = %#v", got)
	}
	_, err = service.GetConversationMetrics(context.Background(), 0)
	if CodeOf(err) != CodeInvalidInput || fake.eventCalls != 1 {
		t.Fatalf("invalid ID err/calls = %v/%d", err, fake.eventCalls)
	}
}

func TestConversationMetricsPreservesMissingValues(t *testing.T) {
	fake := &reportsFake{events: []core.ReportingEvent{{Name: "first_response"}, {Name: "reply_time"}, {Name: "reply_time", ValueSeconds: ptrFloat(10)}}}
	got, err := New(nil, fake, nil).GetConversationMetrics(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.FirstResponseSeconds != nil || got.Summary.FirstResponseBusinessSeconds != nil || got.Summary.ReplyEvents != 2 || got.Summary.AvgReplySeconds == nil || *got.Summary.AvgReplySeconds != 10 {
		t.Fatalf("summary = %#v", got.Summary)
	}
}

func TestComparisonUsesPreviousEqualElapsedRangeAcrossDST(t *testing.T) {
	since := time.Date(2026, 11, 1, 1, 30, 0, 0, time.FixedZone("-04", -4*60*60))
	until := since.Add(25 * time.Hour)
	fake := &reportsFake{groups: map[core.ReportGroup][]core.GroupedReportRow{core.ReportGroupAgent: {}}}
	service := New(nil, fake, nil)
	got, err := service.ComparePerformance(context.Background(), CompareRequest{Since: since.Format(time.RFC3339), Until: until.Format(time.RFC3339), GroupBy: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.groupCalls) != 2 || !fake.groupCalls[0].Range.Since.Equal(since.UTC()) || !fake.groupCalls[0].Range.Until.Equal(until.UTC()) {
		t.Fatalf("current request = %#v", fake.groupCalls)
	}
	wantSince := since.Add(-until.Sub(since))
	if !fake.groupCalls[1].Range.Since.Equal(wantSince.UTC()) || !fake.groupCalls[1].Range.Until.Equal(since.UTC()) || !got.PreviousRange.Since.Equal(wantSince.UTC()) {
		t.Fatalf("previous range = %#v, want %s to %s", got.PreviousRange, wantSince, since)
	}
}

func TestComparisonJoinsRowsByStableKey(t *testing.T) {
	previous := int64(5)
	current := int64(8)
	prevTime := 10.0
	currTime := 6.0
	fake := &reportsFake{groups: map[core.ReportGroup][]core.GroupedReportRow{
		core.ReportGroupAgent: {{Key: "agent:2", ID: 2, Name: "New", Metrics: core.ReportMetrics{ConversationsCount: &current, AvgFirstResponseSeconds: &currTime}}},
	}}
	fake.groupErrs = []error{nil}
	// The fake returns the same group for both calls; replace its rows after call one.
	mutating := &comparisonReportsFake{first: fake.groups[core.ReportGroupAgent], second: []core.GroupedReportRow{{Key: "agent:2", ID: 2, Name: "Old", Metrics: core.ReportMetrics{ConversationsCount: &previous, AvgFirstResponseSeconds: &prevTime}}}}
	service := New(nil, mutating, nil)
	got, err := service.ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Key != "agent:2" || got.Rows[0].Name != "New" || got.Rows[0].Delta.ConversationsCount == nil || got.Rows[0].Delta.ConversationsCount.Absolute != 3 || got.Rows[0].Delta.ConversationsCount.Percent == nil || *got.Rows[0].Delta.ConversationsCount.Percent != 60 {
		t.Fatalf("comparison = %#v", got)
	}
}

type comparisonReportsFake struct {
	first, second       []core.GroupedReportRow
	calls               int
	ranges              []core.GroupedReportRequest
	errFirst, errSecond error
}

func (f *comparisonReportsFake) GetReportSummary(context.Context, core.ReportSummaryRequest) (core.ReportSummary, error) {
	return core.ReportSummary{}, nil
}
func (f *comparisonReportsFake) GetConversationReportingEvents(context.Context, int64) ([]core.ReportingEvent, error) {
	return nil, nil
}
func (f *comparisonReportsFake) GetGroupedReport(_ context.Context, r core.GroupedReportRequest) ([]core.GroupedReportRow, error) {
	f.calls++
	f.ranges = append(f.ranges, r)
	if f.calls == 1 {
		return f.first, f.errFirst
	}
	return f.second, f.errSecond
}

func ptrFloat(value float64) *float64 { return &value }

func TestComparisonOmitsPercentWhenPreviousIsZeroOrMissing(t *testing.T) {
	zero := int64(0)
	current := int64(3)
	fake := &comparisonReportsFake{first: []core.GroupedReportRow{{Key: "a", Metrics: core.ReportMetrics{ConversationsCount: &current}}, {Key: "b", Metrics: core.ReportMetrics{ConversationsCount: &current}}}, second: []core.GroupedReportRow{{Key: "a", Metrics: core.ReportMetrics{ConversationsCount: &zero}}}}
	got, err := New(nil, fake, nil).ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].Key != "a" || got.Rows[0].Delta.ConversationsCount == nil || got.Rows[0].Delta.ConversationsCount.Percent != nil {
		t.Fatalf("zero-previous delta = %#v", got.Rows[0])
	}
	if got.Rows[1].Key != "b" || got.Rows[1].Delta.ConversationsCount != nil {
		t.Fatalf("missing-previous delta = %#v", got.Rows[1])
	}
}

func TestComparisonFallsBackToDecimalIDForMissingKey(t *testing.T) {
	current, previous := int64(4), int64(2)
	fake := &comparisonReportsFake{first: []core.GroupedReportRow{{ID: 17, Metrics: core.ReportMetrics{ConversationsCount: &current}}}, second: []core.GroupedReportRow{{ID: 17, Metrics: core.ReportMetrics{ConversationsCount: &previous}}}}
	got, err := New(nil, fake, nil).ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Key != "17" || got.Rows[0].ID != 17 {
		t.Fatalf("rows = %#v", got.Rows)
	}
}

func TestComparisonRejectsInvalidGroupBeforeCallingAPI(t *testing.T) {
	fake := &comparisonReportsFake{}
	_, err := New(nil, fake, nil).ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "label"})
	if CodeOf(err) != CodeInvalidInput || fake.calls != 0 {
		t.Fatalf("error/calls = %v/%d", err, fake.calls)
	}
}

func TestComparisonFailsWholeResultWhenEitherPeriodFails(t *testing.T) {
	for _, failure := range []int{1, 2} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			fake := &comparisonReportsFake{first: []core.GroupedReportRow{{Key: "current"}}, second: []core.GroupedReportRow{{Key: "previous"}}}
			if failure == 1 {
				fake.errFirst = &chatwoot.Error{Kind: chatwoot.KindForbidden}
			} else {
				fake.errSecond = &chatwoot.Error{Kind: chatwoot.KindForbidden}
			}
			_, err := New(nil, fake, nil).ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "team"})
			if err == nil || CodeOf(err) != CodeForbidden {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestComparisonBoundsRowsDeterministically(t *testing.T) {
	rows := []core.GroupedReportRow{{Key: "z"}, {Key: "a"}, {Key: "m"}}
	fake := &comparisonReportsFake{first: rows, second: rows}
	got, err := New(nil, fake, nil).ComparePerformance(context.Background(), CompareRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z", GroupBy: "team", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRows != 3 || got.ReturnedRows != 2 || !got.Truncated || got.Rows[0].Key != "a" || got.Rows[1].Key != "m" {
		t.Fatalf("result = %#v", got)
	}
}

func TestAnalyticsMapsReportNotFoundByOperation(t *testing.T) {
	unsupported := &reportsFake{summaryErr: &chatwoot.Error{Kind: chatwoot.KindNotFound}}
	_, err := New(nil, unsupported, nil).GetSummary(context.Background(), SummaryRequest{Since: "2026-10-01T00:00:00Z", Until: "2026-10-02T00:00:00Z"})
	if CodeOf(err) != CodeUnsupportedFeature {
		t.Fatalf("summary 404 = %v", err)
	}
	notfound := &reportsFake{eventsErr: &chatwoot.Error{Kind: chatwoot.KindNotFound}}
	_, err = New(nil, notfound, nil).GetConversationMetrics(context.Background(), 42)
	if CodeOf(err) != CodeNotFound {
		t.Fatalf("events 404 = %v", err)
	}
}
