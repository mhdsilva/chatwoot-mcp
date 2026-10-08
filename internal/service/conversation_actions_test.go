package service

import (
	"context"
	"testing"
	"time"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

func TestAddPrivateNoteSendsPrivateOutgoing(t *testing.T) {
	fake := &fakeAPI{
		getConv:       core.Conversation{ID: 42},
		createMessage: core.Message{ID: 500, Content: "nota", Private: true, Status: "sent"},
	}
	svc := New(fake)

	result, err := svc.AddPrivateNote(context.Background(), 42, "nota")
	if err != nil {
		t.Fatalf("AddPrivateNote: %v", err)
	}
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", fake.createCalls)
	}
	req := fake.createReqs[0]
	if !req.Private || req.Content != "nota" || req.ConversationID != 42 {
		t.Fatalf("request = %#v, want private outgoing note for conversation 42", req)
	}
	if !result.Private || result.Message.Private != true || result.Delivery != DeliveryAcceptedByAPI {
		t.Fatalf("result = %#v", result)
	}
}

func TestAddPrivateNoteRejectsEmptyContent(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).AddPrivateNote(context.Background(), 42, "   ")
	requireCode(t, err, CodeInvalidInput)
	if fake.getCalls != 0 || fake.createCalls != 0 {
		t.Fatalf("API called for empty note (get=%d create=%d)", fake.getCalls, fake.createCalls)
	}
}

func TestAddPrivateNoteRejectsNonPositiveID(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).AddPrivateNote(context.Background(), 0, "nota")
	requireCode(t, err, CodeInvalidInput)
	if fake.createCalls != 0 {
		t.Fatalf("created note for id 0")
	}
}

func TestAddPrivateNoteRejectsConversationMismatch(t *testing.T) {
	fake := &fakeAPI{getConv: core.Conversation{ID: 99}}
	_, err := New(fake).AddPrivateNote(context.Background(), 42, "nota")
	requireCode(t, err, CodeConversationMismatch)
	if fake.createCalls != 0 {
		t.Fatalf("created note despite a conversation mismatch")
	}
}

func TestAddPrivateNoteTimeoutIsUnknownWithoutRetry(t *testing.T) {
	fake := &fakeAPI{
		getConv:   core.Conversation{ID: 42},
		createErr: &chatwoot.Error{Kind: chatwoot.KindTimeout, Resource: "conversation 42 message"},
	}
	_, err := New(fake).AddPrivateNote(context.Background(), 42, "nota")
	requireCode(t, err, CodeDeliveryUnknown)
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want exactly 1", fake.createCalls)
	}
}

func TestSetConversationStatusAcceptsDocumentedStates(t *testing.T) {
	for _, status := range []string{"open", "pending", "resolved", "snoozed"} {
		t.Run(status, func(t *testing.T) {
			fake := &fakeAPI{setStatusConv: core.Conversation{ID: 42, Status: status}}
			result, err := New(fake).SetConversationStatus(context.Background(), 42, status, "")
			if err != nil {
				t.Fatalf("SetConversationStatus: %v", err)
			}
			if len(fake.setStatusReqs) != 1 || fake.setStatusReqs[0].Status != status {
				t.Fatalf("requests = %#v, want status %q", fake.setStatusReqs, status)
			}
			if result.Status != status {
				t.Fatalf("result status = %q, want %q", result.Status, status)
			}
		})
	}
}

func TestSetConversationStatusRejectsUnknownState(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SetConversationStatus(context.Background(), 42, "archived", "")
	requireCode(t, err, CodeInvalidInput)
	if len(fake.setStatusReqs) != 0 {
		t.Fatalf("API called for an unknown status")
	}
}

func TestSetConversationStatusSnoozedUntilOnlyWithSnoozed(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SetConversationStatus(context.Background(), 42, "open", "2030-07-21T17:32:28Z")
	requireCode(t, err, CodeInvalidInput)
	if len(fake.setStatusReqs) != 0 {
		t.Fatalf("API called with snoozed_until for status open")
	}
}

func TestSetConversationStatusRejectsInvalidSnoozeTime(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SetConversationStatus(context.Background(), 42, "snoozed", "tomorrow")
	requireCode(t, err, CodeInvalidInput)
}

func TestSetConversationStatusPassesSnoozedUntil(t *testing.T) {
	fake := &fakeAPI{setStatusConv: core.Conversation{ID: 42, Status: "snoozed", SnoozedUntil: "2030-07-21T17:32:28Z"}}
	result, err := New(fake).SetConversationStatus(context.Background(), 42, "snoozed", "2030-07-21T17:32:28Z")
	if err != nil {
		t.Fatalf("SetConversationStatus: %v", err)
	}
	if len(fake.setStatusReqs) != 1 || fake.setStatusReqs[0].SnoozedUntil == nil {
		t.Fatalf("snoozed_until not forwarded: %#v", fake.setStatusReqs)
	}
	want, _ := time.Parse(time.RFC3339, "2030-07-21T17:32:28Z")
	if !fake.setStatusReqs[0].SnoozedUntil.Equal(want) {
		t.Fatalf("snoozed_until = %v, want %v", fake.setStatusReqs[0].SnoozedUntil, want)
	}
	if result.SnoozedUntil != "2030-07-21T17:32:28Z" {
		t.Fatalf("result snoozed_until = %q", result.SnoozedUntil)
	}
}

func TestSetConversationStatusClassifiesAPIError(t *testing.T) {
	fake := &fakeAPI{setStatusErr: &chatwoot.Error{Kind: chatwoot.KindNotFound, StatusCode: 404}}
	_, err := New(fake).SetConversationStatus(context.Background(), 42, "resolved", "")
	requireCode(t, err, CodeNotFound)
}

func TestSetPriorityAcceptsDocumentedValues(t *testing.T) {
	for _, priority := range []string{"none", "low", "medium", "high", "urgent"} {
		t.Run(priority, func(t *testing.T) {
			fake := &fakeAPI{setPriorityConv: core.Conversation{ID: 42, Priority: priority}}
			result, err := New(fake).SetPriority(context.Background(), 42, priority)
			if err != nil {
				t.Fatalf("SetPriority: %v", err)
			}
			if len(fake.setPriorityReqs) != 1 || fake.setPriorityReqs[0].Priority != priority {
				t.Fatalf("requests = %#v", fake.setPriorityReqs)
			}
			if result.Priority != priority {
				t.Fatalf("result priority = %q, want %q", result.Priority, priority)
			}
		})
	}
}

func TestSetPriorityRejectsUnknownValue(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SetPriority(context.Background(), 42, "critical")
	requireCode(t, err, CodeInvalidInput)
	if len(fake.setPriorityReqs) != 0 {
		t.Fatalf("API called for an unknown priority")
	}
}

func TestSetPriorityRejectsNonPositiveID(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SetPriority(context.Background(), -1, "high")
	requireCode(t, err, CodeInvalidInput)
	if len(fake.setPriorityReqs) != 0 {
		t.Fatalf("API called for a non-positive conversation id")
	}
}
