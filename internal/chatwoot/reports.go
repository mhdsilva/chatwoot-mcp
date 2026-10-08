package chatwoot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"chatwoot-mcp/internal/core"
)

var _ core.ReportsAPI = (*Client)(nil)

type optionalNumber struct{ Value *float64 }

func (n *optionalNumber) UnmarshalJSON(data []byte) error {
	data = []byte(strings.TrimSpace(string(data)))
	if string(data) == "null" || string(data) == `""` {
		n.Value = nil
		return nil
	}
	var value float64
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return fmt.Errorf("expected numeric string")
		}
		value = parsed
	} else {
		if len(data) == 0 || (data[0] != '-' && (data[0] < '0' || data[0] > '9')) {
			return fmt.Errorf("expected number")
		}
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("expected number")
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("number is not finite")
	}
	n.Value = &value
	return nil
}

func (c *Client) GetReportSummary(ctx context.Context, req core.ReportSummaryRequest) (core.ReportSummary, error) {
	query := reportRangeQuery(req.Range)
	scope := req.Scope
	if scope == "" {
		scope = core.ReportScopeAccount
	}
	query.Set("type", string(scope))
	if scope != core.ReportScopeAccount {
		query.Set("id", strconv.FormatInt(req.ScopeID, 10))
	}
	var raw json.RawMessage
	path := "/api/v2/accounts/" + strconv.FormatInt(c.accountID, 10) + "/reports/summary"
	if err := c.callWithBearer(ctx, http.MethodGet, path, query, nil, &raw, "report summary", true); err != nil {
		return core.ReportSummary{}, err
	}
	var envelope struct {
		Previous json.RawMessage `json:"previous"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return core.ReportSummary{}, invalidReport("summary")
	}
	current, err := decodeReportMetrics(raw)
	if err != nil {
		return core.ReportSummary{}, invalidReport("current")
	}
	previous, err := decodeReportMetrics(envelope.Previous)
	if err != nil {
		return core.ReportSummary{}, invalidReport("previous")
	}
	return core.ReportSummary{Current: current, Previous: previous}, nil
}

func (c *Client) GetConversationReportingEvents(ctx context.Context, id int64) ([]core.ReportingEvent, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(id, 10) + "/reporting_events"
	var raw json.RawMessage
	if err := c.callWithBearer(ctx, http.MethodGet, path, nil, nil, &raw, "conversation reporting events", true); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		var envelope struct {
			Payload []json.RawMessage `json:"payload"`
		}
		if envelopeErr := json.Unmarshal(raw, &envelope); envelopeErr != nil || envelope.Payload == nil {
			return nil, invalidReport("events")
		}
		items = envelope.Payload
	}
	result := make([]core.ReportingEvent, 0, len(items))
	for i, item := range items {
		var row struct {
			ID             int64          `json:"id"`
			Name           string         `json:"name"`
			Value          optionalNumber `json:"value"`
			Business       optionalNumber `json:"value_in_business_hours"`
			BusinessLegacy optionalNumber `json:"business_value"`
			Start          string         `json:"event_start_time"`
			End            string         `json:"event_end_time"`
			ConversationID int64          `json:"conversation_id"`
			InboxID        int64          `json:"inbox_id"`
			UserID         int64          `json:"user_id"`
		}
		if json.Unmarshal(item, &row) != nil {
			return nil, invalidReport(fmt.Sprintf("events[%d]", i))
		}
		start, err := time.Parse(time.RFC3339Nano, row.Start)
		if err != nil {
			return nil, invalidReport(fmt.Sprintf("events[%d].event_start_time", i))
		}
		end, err := time.Parse(time.RFC3339Nano, row.End)
		if err != nil {
			return nil, invalidReport(fmt.Sprintf("events[%d].event_end_time", i))
		}
		businessValue := row.Business.Value
		if businessValue == nil {
			businessValue = row.BusinessLegacy.Value
		}
		result = append(result, core.ReportingEvent{ID: row.ID, Name: row.Name, ValueSeconds: row.Value.Value, BusinessValueSeconds: businessValue, EventStartTime: start, EventEndTime: end, ConversationID: row.ConversationID, InboxID: row.InboxID, UserID: row.UserID})
	}
	return result, nil
}

func (c *Client) GetGroupedReport(ctx context.Context, req core.GroupedReportRequest) ([]core.GroupedReportRow, error) {
	paths := map[core.ReportGroup]string{core.ReportGroupAgent: "agent", core.ReportGroupTeam: "team", core.ReportGroupInbox: "inbox", core.ReportGroupChannel: "channel"}
	groupPath, ok := paths[req.Group]
	if !ok {
		return nil, &Error{Kind: KindRequest, Resource: "grouped report", Message: "unsupported report group"}
	}
	query := reportRangeQuery(req.Range)
	path := "/api/v2/accounts/" + strconv.FormatInt(c.accountID, 10) + "/summary_reports/" + groupPath
	var raw json.RawMessage
	if err := c.callWithBearer(ctx, http.MethodGet, path, query, nil, &raw, "grouped report", true); err != nil {
		return nil, err
	}
	if req.Group == core.ReportGroupChannel {
		return decodeChannelRows(raw)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		var envelope struct {
			Data    []json.RawMessage `json:"data"`
			Payload []json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			return nil, invalidReport("grouped report")
		}
		items = envelope.Data
		if items == nil {
			items = envelope.Payload
		}
	}
	rows := make([]core.GroupedReportRow, 0, len(items))
	for i, item := range items {
		var row struct {
			ID         optionalNumber `json:"id"`
			Name       string         `json:"name"`
			Count      optionalNumber `json:"conversations_count"`
			Resolved   optionalNumber `json:"resolved_conversations_count"`
			First      optionalNumber `json:"avg_first_response_time"`
			Resolution optionalNumber `json:"avg_resolution_time"`
			Reply      optionalNumber `json:"avg_reply_time"`
		}
		if json.Unmarshal(item, &row) != nil {
			return nil, invalidReport(fmt.Sprintf("rows[%d]", i))
		}
		if row.ID.Value == nil {
			return nil, invalidReport(fmt.Sprintf("rows[%d].id", i))
		}
		id, err := numberAsInt64(row.ID.Value)
		if err != nil {
			return nil, invalidReport(fmt.Sprintf("rows[%d].id", i))
		}
		if _, err := numberAsInt64(row.Count.Value); err != nil {
			return nil, invalidReport(fmt.Sprintf("rows[%d].conversations_count", i))
		}
		if _, err := numberAsInt64(row.Resolved.Value); err != nil {
			return nil, invalidReport(fmt.Sprintf("rows[%d].resolved_conversations_count", i))
		}
		metrics := core.ReportMetrics{ConversationsCount: row.Count.int64ptr(), ResolutionsCount: row.Resolved.int64ptr(), AvgFirstResponseSeconds: row.First.Value, AvgResolutionSeconds: row.Resolution.Value, AvgReplySeconds: row.Reply.Value}
		rows = append(rows, core.GroupedReportRow{Key: strconv.FormatInt(id, 10), ID: id, Name: row.Name, Metrics: metrics})
	}
	return rows, nil
}

func (n optionalNumber) int64ptr() *int64 {
	if n.Value == nil {
		return nil
	}
	v, err := numberAsInt64(n.Value)
	if err != nil {
		return nil
	}
	return &v
}

func numberAsInt64(value *float64) (int64, error) {
	if value == nil {
		return 0, nil
	}
	if math.Trunc(*value) != *value || *value < -9223372036854775808.0 || *value >= 9223372036854775808.0 {
		return 0, fmt.Errorf("expected integral int64")
	}
	return int64(*value), nil
}

func reportRangeQuery(r core.ReportRange) url.Values {
	query := url.Values{}
	query.Set("since", strconv.FormatInt(r.Since.Unix(), 10))
	query.Set("until", strconv.FormatInt(r.Until.Unix(), 10))
	return query
}

func decodeReportMetrics(raw json.RawMessage) (core.ReportMetrics, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return core.ReportMetrics{}, nil
	}
	var row struct {
		Conversations optionalNumber `json:"conversations_count"`
		Incoming      optionalNumber `json:"incoming_messages_count"`
		Outgoing      optionalNumber `json:"outgoing_messages_count"`
		Resolutions   optionalNumber `json:"resolutions_count"`
		Open          optionalNumber `json:"open_count"`
		Pending       optionalNumber `json:"pending_count"`
		Snoozed       optionalNumber `json:"snoozed_count"`
		First         optionalNumber `json:"avg_first_response_time"`
		Resolution    optionalNumber `json:"avg_resolution_time"`
		Reply         optionalNumber `json:"avg_reply_time"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return core.ReportMetrics{}, err
	}
	counts := []*optionalNumber{&row.Conversations, &row.Incoming, &row.Outgoing, &row.Resolutions, &row.Open, &row.Pending, &row.Snoozed}
	for _, count := range counts {
		if _, err := numberAsInt64(count.Value); err != nil {
			return core.ReportMetrics{}, err
		}
	}
	return core.ReportMetrics{ConversationsCount: row.Conversations.int64ptr(), IncomingMessagesCount: row.Incoming.int64ptr(), OutgoingMessagesCount: row.Outgoing.int64ptr(), ResolutionsCount: row.Resolutions.int64ptr(), OpenCount: row.Open.int64ptr(), PendingCount: row.Pending.int64ptr(), SnoozedCount: row.Snoozed.int64ptr(), AvgFirstResponseSeconds: row.First.Value, AvgResolutionSeconds: row.Resolution.Value, AvgReplySeconds: row.Reply.Value}, nil
}

func decodeChannelRows(raw json.RawMessage) ([]core.GroupedReportRow, error) {
	var grouped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &grouped); err != nil {
		return nil, invalidReport("channel report")
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]core.GroupedReportRow, 0, len(keys))
	for _, key := range keys {
		var counts struct {
			Open     optionalNumber `json:"open"`
			Resolved optionalNumber `json:"resolved"`
			Pending  optionalNumber `json:"pending"`
			Snoozed  optionalNumber `json:"snoozed"`
			Total    optionalNumber `json:"total"`
		}
		if json.Unmarshal(grouped[key], &counts) != nil {
			return nil, invalidReport("channel report")
		}
		for _, count := range []*optionalNumber{&counts.Open, &counts.Resolved, &counts.Pending, &counts.Snoozed, &counts.Total} {
			if _, err := numberAsInt64(count.Value); err != nil {
				return nil, invalidReport("channel report")
			}
		}
		rows = append(rows, core.GroupedReportRow{Key: key, Name: key, Metrics: core.ReportMetrics{ConversationsCount: counts.Total.int64ptr(), ResolutionsCount: counts.Resolved.int64ptr(), OpenCount: counts.Open.int64ptr(), PendingCount: counts.Pending.int64ptr(), SnoozedCount: counts.Snoozed.int64ptr()}})
	}
	return rows, nil
}

func invalidReport(field string) error {
	return &Error{Kind: KindInvalid, Resource: "report", Message: "invalid " + field + " value"}
}
