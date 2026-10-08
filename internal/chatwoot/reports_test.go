package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"chatwoot-mcp/internal/core"
)

func TestGetReportSummary(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/accounts/7/reports/summary" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("since") != "1790812800" || r.URL.Query().Get("until") != "1791417600" {
			t.Errorf("query = %v", r.URL.Query())
		}
		if r.URL.Query().Get("type") != "agent" || r.URL.Query().Get("id") != "9" {
			t.Errorf("scope query = %v", r.URL.Query())
		}
		assertReportAuth(t, r)
		_, _ = w.Write([]byte(`{"conversations_count":"4","avg_first_response_time":12.5,"previous":{"conversations_count":0,"avg_first_response_time":null}}`))
	})
	start := time.Unix(1790812800, 0).UTC()
	summary, err := client.GetReportSummary(context.Background(), core.ReportSummaryRequest{Range: core.ReportRange{Since: start, Until: time.Unix(1791417600, 0).UTC()}, Scope: core.ReportScopeAgent, ScopeID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current.ConversationsCount == nil || *summary.Current.ConversationsCount != 4 || summary.Current.AvgFirstResponseSeconds == nil || *summary.Current.AvgFirstResponseSeconds != 12.5 {
		t.Fatalf("current = %#v", summary.Current)
	}
	if summary.Previous.ConversationsCount == nil || *summary.Previous.ConversationsCount != 0 || summary.Previous.AvgFirstResponseSeconds != nil {
		t.Fatalf("previous = %#v", summary.Previous)
	}
}

func TestGetConversationReportingEvents(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/7/conversations/123/reporting_events" {
			t.Errorf("path = %q", r.URL.Path)
		}
		assertReportAuth(t, r)
		_, _ = w.Write([]byte(`{"meta":{"count":1,"current_page":1,"total_pages":1},"payload":[{"id":1,"name":"first_response","value":"8.5","value_in_business_hours":5,"event_start_time":"2026-10-01T00:00:00Z","event_end_time":"2026-10-01T00:00:08Z","conversation_id":123,"inbox_id":2,"user_id":3}]}`))
	})
	events, err := client.GetConversationReportingEvents(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ValueSeconds == nil || *events[0].ValueSeconds != 8.5 || events[0].BusinessValueSeconds == nil || *events[0].BusinessValueSeconds != 5 || events[0].ConversationID != 123 {
		t.Fatalf("events = %#v", events)
	}
}

func TestGetGroupedReport(t *testing.T) {
	for _, group := range []core.ReportGroup{core.ReportGroupAgent, core.ReportGroupTeam, core.ReportGroupInbox, core.ReportGroupChannel} {
		t.Run(string(group), func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/accounts/7/summary_reports/"+string(group) {
					t.Errorf("path = %q", r.URL.Path)
				}
				assertReportAuth(t, r)
				if group == core.ReportGroupChannel {
					_, _ = w.Write([]byte(`{"whatsapp":{"open":2,"resolved":3,"pending":1,"snoozed":0,"total":6}}`))
					return
				}
				_, _ = w.Write([]byte(`[{"id":8,"name":"Ana","conversations_count":"5","resolved_conversations_count":2,"avg_first_response_time":3.5,"avg_resolution_time":null,"avg_reply_time":""}]`))
			})
			rows, err := client.GetGroupedReport(context.Background(), core.GroupedReportRequest{Range: core.ReportRange{Since: time.Unix(1790812800, 0), Until: time.Unix(1791417600, 0)}, Group: group})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows = %#v", rows)
			}
			if group == core.ReportGroupChannel {
				if rows[0].Key != "whatsapp" || rows[0].Metrics.OpenCount == nil || *rows[0].Metrics.OpenCount != 2 {
					t.Fatalf("row = %#v", rows[0])
				}
			} else if rows[0].ID != 8 || rows[0].Name != "Ana" || rows[0].Metrics.ConversationsCount == nil || *rows[0].Metrics.ConversationsCount != 5 || rows[0].Metrics.AvgFirstResponseSeconds == nil || rows[0].Metrics.AvgResolutionSeconds != nil {
				t.Fatalf("row = %#v", rows[0])
			}
		})
	}
	client, _ := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unknown group must not issue request") })
	_, err := client.GetGroupedReport(context.Background(), core.GroupedReportRequest{Group: "unknown"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindRequest {
		t.Fatalf("error = %v, want KindRequest", err)
	}
}

func TestReportNumberVariants(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    float64
		missing bool
		bad     bool
	}{
		{`0`, 0, false, false}, {`"12.25"`, 12.25, false, false}, {`null`, 0, true, false}, {`""`, 0, true, false}, {`"oops"`, 0, false, true}, {`true`, 0, false, true}, {`"NaN"`, 0, false, true},
	} {
		var got optionalNumber
		err := json.Unmarshal([]byte(tc.raw), &got)
		if tc.bad {
			if err == nil {
				t.Errorf("%s: expected error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.raw, err)
			continue
		}
		if tc.missing != (got.Value == nil) {
			t.Errorf("%s: missing = %v", tc.raw, got.Value == nil)
		}
		if got.Value != nil && *got.Value != tc.want {
			t.Errorf("%s: value = %v", tc.raw, *got.Value)
		}
	}
}

func TestReportFailuresAreTyped(t *testing.T) {
	for status, want := range map[int]Kind{401: KindUnauthorized, 403: KindForbidden, 404: KindNotFound, 429: KindRateLimited, 500: KindServer} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"failure"}`))
			})
			_, err := client.GetReportSummary(context.Background(), core.ReportSummaryRequest{})
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Kind != want {
				t.Fatalf("error = %v, want %s", err, want)
			}
		})
	}
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"conversations_count":"secret-token-invalid"}`))
	})
	_, err := client.GetReportSummary(context.Background(), core.ReportSummaryRequest{})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindInvalid || apiErr.Message == "" {
		t.Fatalf("error = %#v", err)
	}
	if apiErr != nil && apiErr.Message == "secret-token-invalid" {
		t.Fatal("error leaked response field")
	}
	client, _ = newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) })
	_, err = client.GetReportSummary(context.Background(), core.ReportSummaryRequest{})
	if !errors.As(err, &apiErr) || apiErr.Kind != KindInvalid {
		t.Fatalf("malformed JSON error = %v", err)
	}

	client, _ = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = client.GetReportSummary(ctx, core.ReportSummaryRequest{})
	if !errors.As(err, &apiErr) || apiErr.Kind != KindTimeout {
		t.Fatalf("timeout error = %v", err)
	}

	redirectRequests := 0
	client, _ = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		redirectRequests++
		http.Redirect(w, r, "https://other.invalid/report", http.StatusFound)
	})
	_, err = client.GetReportSummary(context.Background(), core.ReportSummaryRequest{})
	if !errors.As(err, &apiErr) || apiErr.Kind != KindTransport || redirectRequests != 1 {
		t.Fatalf("redirect error/calls = %v/%d", err, redirectRequests)
	}
}

func assertReportAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("api_access_token") != testToken || r.Header.Get("Authorization") != "Bearer "+testToken {
		t.Errorf("auth headers = %q / %q", r.Header.Get("api_access_token"), r.Header.Get("Authorization"))
	}
}
