package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

var fixedNow = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

type fakeSource struct {
	pages map[int]core.Page[core.Conversation]
	calls []core.ListOptions
	err   error
}

func (f *fakeSource) ListConversations(_ context.Context, opts core.ListOptions) (core.Page[core.Conversation], error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return core.Page[core.Conversation]{}, f.err
	}
	page, ok := f.pages[opts.Page]
	if !ok {
		return core.Page[core.Conversation]{}, fmt.Errorf("unexpected page request: %d", opts.Page)
	}
	// The real client filters status/inbox/team upstream; mirror that here so
	// tests prove which filters travel upstream versus applied locally.
	items := make([]core.Conversation, 0, len(page.Items))
	for _, conversation := range page.Items {
		if opts.Status != "" && conversation.Status != opts.Status {
			continue
		}
		if opts.InboxID > 0 && conversation.InboxID != opts.InboxID {
			continue
		}
		if opts.TeamID > 0 && conversation.TeamID != opts.TeamID {
			continue
		}
		items = append(items, conversation)
	}
	return core.Page[core.Conversation]{Items: items, NextPage: page.NextPage}, nil
}

func waitingConv(id int64, waitingSince int64, assigneeID int64, teamID int64) core.Conversation {
	return core.Conversation{
		ID:             id,
		InboxID:        2,
		ContactID:      40 + id,
		ChannelType:    "Channel::Api",
		Status:         "open",
		AssigneeID:     assigneeID,
		TeamID:         teamID,
		WaitingSince:   waitingSince,
		LastActivityAt: waitingSince + 60,
		UnreadCount:    1,
	}
}

func newService(source ConversationSource) Service {
	return New(source, nil, func() time.Time { return fixedNow })
}

func requireCode(t *testing.T, err error, code Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected analytics error, got %v", err)
	}
	if svcErr.Code != code {
		t.Fatalf("expected code %s, got %s", code, svcErr.Code)
	}
}

func TestAttentionQueueDefaultsFiltersSortsAndLimits(t *testing.T) {
	// Waiting times in seconds before fixedNow (2026-10-08T15:00:00Z).
	// Conversations 7 and 9 share the same waiting_since: ID 7 must come first.
	source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
		1: {Items: []core.Conversation{
			waitingConv(9, fixedNow.Unix()-300, 8, 3),
			waitingConv(7, fixedNow.Unix()-300, 8, 3),
			waitingConv(5, fixedNow.Unix()-900, 8, 3),
			{ID: 4, Status: "open", WaitingSince: 0},
		}, NextPage: 0},
	}}
	svc := newService(source)

	result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
	if err != nil {
		t.Fatalf("ListAttentionQueue: %v", err)
	}

	if len(source.calls) != 1 {
		t.Fatalf("expected 1 source call, got %d", len(source.calls))
	}
	call := source.calls[0]
	if call.Page != 1 || call.Status != "open" || call.InboxID != 0 || call.TeamID != 0 {
		t.Fatalf("unexpected upstream options: %+v", call)
	}

	if result.ObservedAt != fixedNow {
		t.Fatalf("observed_at = %v, want %v", result.ObservedAt, fixedNow)
	}
	if len(result.Conversations) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(result.Conversations))
	}
	wantOrder := []int64{5, 7, 9}
	for i, wantID := range wantOrder {
		if got := result.Conversations[i].ConversationID; got != wantID {
			t.Fatalf("conversation %d: got ID %d, want %d", i, got, wantID)
		}
	}
	first := result.Conversations[0]
	if first.WaitingSeconds != 900 {
		t.Fatalf("waiting_seconds = %d, want 900", first.WaitingSeconds)
	}
	if first.WaitingSince != time.Unix(fixedNow.Unix()-900, 0).UTC() {
		t.Fatalf("waiting_since = %v", first.WaitingSince)
	}
	if first.LastActivityAt == nil || !first.LastActivityAt.Equal(time.Unix(fixedNow.Unix()-840, 0).UTC()) {
		t.Fatalf("last_activity_at = %v", first.LastActivityAt)
	}
	if first.UnreadCount != 1 || first.InboxID != 2 || first.ContactID != 45 ||
		first.ChannelType != "Channel::Api" || first.Status != "open" ||
		first.AssigneeID != 8 || first.TeamID != 3 {
		t.Fatalf("unexpected projection: %+v", first)
	}
	if !result.Complete || result.Truncated || result.NextPage != 0 {
		t.Fatalf("expected complete non-truncated result, got %+v", result)
	}
	if result.ScannedPages != 1 || result.ScannedConversations != 4 {
		t.Fatalf("unexpected scan counters: %+v", result)
	}
}

func TestAttentionConversationOmitsMissingLastActivity(t *testing.T) {
	data, err := json.Marshal(AttentionConversation{ConversationID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "last_activity_at") {
		t.Fatalf("missing last activity was serialized: %s", data)
	}
}

func TestAttentionQueueFiltersTeamAndAssignee(t *testing.T) {
	source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
		1: {Items: []core.Conversation{
			waitingConv(1, fixedNow.Unix()-100, 8, 3),
			waitingConv(2, fixedNow.Unix()-200, 9, 3),
			waitingConv(3, fixedNow.Unix()-300, 8, 4),
			waitingConv(4, fixedNow.Unix()-400, 0, 3),
		}, NextPage: 0},
	}}
	svc := newService(source)

	result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{
		TeamID:     3,
		AssigneeID: 8,
	})
	if err != nil {
		t.Fatalf("ListAttentionQueue: %v", err)
	}

	if source.calls[0].TeamID != 3 {
		t.Fatalf("team filter must be sent upstream, got %+v", source.calls[0])
	}
	if len(result.Conversations) != 1 || result.Conversations[0].ConversationID != 1 {
		t.Fatalf("expected only conversation 1, got %+v", result.Conversations)
	}
	if result.ScannedConversations != 3 {
		t.Fatalf("scanned_conversations = %d, want 3 (team filtered upstream, assignee locally)", result.ScannedConversations)
	}
}

func TestAttentionQueueReportsPartialTenPageScan(t *testing.T) {
	pages := map[int]core.Page[core.Conversation]{}
	for page := 1; page <= 12; page++ {
		pages[page] = core.Page[core.Conversation]{
			Items:    []core.Conversation{waitingConv(int64(page), fixedNow.Unix()-int64(page), 8, 3)},
			NextPage: page + 1,
		}
	}
	source := &fakeSource{pages: pages}
	svc := newService(source)

	result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
	if err != nil {
		t.Fatalf("ListAttentionQueue: %v", err)
	}

	if result.ScannedPages != 10 {
		t.Fatalf("scanned_pages = %d, want 10", result.ScannedPages)
	}
	if result.ScannedConversations != 10 {
		t.Fatalf("scanned_conversations = %d, want 10", result.ScannedConversations)
	}
	if result.Complete {
		t.Fatal("partial scan must not be complete")
	}
	if result.NextPage != 11 {
		t.Fatalf("next_page = %d, want 11", result.NextPage)
	}
	if len(source.calls) != 10 {
		t.Fatalf("expected 10 source calls, got %d", len(source.calls))
	}
}

func TestAttentionQueueStopsOnRepeatedOrBackwardPage(t *testing.T) {
	t.Run("repeated cursor", func(t *testing.T) {
		source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
			1: {Items: []core.Conversation{waitingConv(1, fixedNow.Unix()-100, 8, 3)}, NextPage: 2},
			2: {Items: []core.Conversation{waitingConv(2, fixedNow.Unix()-200, 8, 3)}, NextPage: 2},
		}}
		svc := newService(source)

		result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
		if err != nil {
			t.Fatalf("ListAttentionQueue: %v", err)
		}
		if len(source.calls) != 2 {
			t.Fatalf("must stop without looping, got %d calls", len(source.calls))
		}
		if result.Complete {
			t.Fatal("unusable cursor must not be complete")
		}
		if result.NextPage != 0 {
			t.Fatalf("next_page = %d, want 0 when the cursor is unusable", result.NextPage)
		}
		if result.ScannedPages != 2 {
			t.Fatalf("scanned_pages = %d, want 2", result.ScannedPages)
		}
	})

	t.Run("backward cursor", func(t *testing.T) {
		source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
			1: {Items: []core.Conversation{waitingConv(1, fixedNow.Unix()-100, 8, 3)}, NextPage: 2},
			2: {Items: []core.Conversation{waitingConv(2, fixedNow.Unix()-200, 8, 3)}, NextPage: 1},
		}}
		svc := newService(source)

		result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
		if err != nil {
			t.Fatalf("ListAttentionQueue: %v", err)
		}
		if len(source.calls) != 2 {
			t.Fatalf("must stop without looping, got %d calls", len(source.calls))
		}
		if result.Complete || result.NextPage != 0 {
			t.Fatalf("expected incomplete stop, got %+v", result)
		}
	})
}

func TestAttentionQueueClampsFutureWaitingSince(t *testing.T) {
	source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
		1: {Items: []core.Conversation{
			waitingConv(1, fixedNow.Unix()+600, 8, 3),
			waitingConv(2, fixedNow.Unix()-100, 8, 3),
		}, NextPage: 0},
	}}
	svc := newService(source)

	result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
	if err != nil {
		t.Fatalf("ListAttentionQueue: %v", err)
	}

	if len(result.Conversations) != 2 {
		t.Fatalf("future waiting_since must still be listed, got %d items", len(result.Conversations))
	}
	for _, conv := range result.Conversations {
		if conv.WaitingSeconds < 0 {
			t.Fatalf("waiting_seconds = %d, must never be negative", conv.WaitingSeconds)
		}
	}
	byID := map[int64]AttentionConversation{}
	for _, conv := range result.Conversations {
		byID[conv.ConversationID] = conv
	}
	if byID[1].WaitingSeconds != 0 {
		t.Fatalf("future wait must clamp to zero, got %+v", byID[1])
	}
	if byID[2].WaitingSeconds != 100 {
		t.Fatalf("past wait must be preserved, got %+v", byID[2])
	}
}

func TestAttentionQueueValidatesBeforeCallingSource(t *testing.T) {
	cases := []struct {
		name    string
		request QueueRequest
	}{
		{"unknown status", QueueRequest{Status: "resolved"}},
		{"negative inbox", QueueRequest{InboxID: -1}},
		{"negative team", QueueRequest{TeamID: -1}},
		{"negative assignee", QueueRequest{AssigneeID: -1}},
		{"negative limit", QueueRequest{Limit: -1}},
		{"limit over cap", QueueRequest{Limit: 51}},
		{"negative start page", QueueRequest{StartPage: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeSource{}
			svc := newService(source)

			_, err := svc.ListAttentionQueue(context.Background(), tc.request)
			requireCode(t, err, CodeInvalidInput)
			if len(source.calls) != 0 {
				t.Fatalf("source must not be called on invalid input, got %d calls", len(source.calls))
			}
		})
	}
}

func TestAttentionQueueMapsSourceErrors(t *testing.T) {
	apiErr := &chatwoot.Error{Kind: chatwoot.KindForbidden, StatusCode: 403}
	source := &fakeSource{err: apiErr}
	svc := newService(source)

	_, err := svc.ListAttentionQueue(context.Background(), QueueRequest{})
	requireCode(t, err, CodeForbidden)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.APIError != apiErr {
		t.Fatalf("typed chatwoot error must be retained, got %v", err)
	}
}

func TestAttentionQueueLimitTruncatesSortedRows(t *testing.T) {
	items := make([]core.Conversation, 0, 25)
	for i := 0; i < 25; i++ {
		items = append(items, waitingConv(int64(i+1), fixedNow.Unix()-int64(100+i), 8, 3))
	}
	source := &fakeSource{pages: map[int]core.Page[core.Conversation]{
		1: {Items: items, NextPage: 0},
	}}
	svc := newService(source)

	result, err := svc.ListAttentionQueue(context.Background(), QueueRequest{Limit: 20})
	if err != nil {
		t.Fatalf("ListAttentionQueue: %v", err)
	}
	if len(result.Conversations) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(result.Conversations))
	}
	if !result.Truncated {
		t.Fatal("eligible rows beyond the limit must mark truncated")
	}
	if !result.Complete {
		t.Fatal("truncation must not affect completeness")
	}
	if result.Conversations[0].ConversationID != 25 {
		t.Fatalf("oldest wait must come first, got ID %d", result.Conversations[0].ConversationID)
	}
}
