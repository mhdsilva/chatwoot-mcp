package service

import (
	"context"
	"testing"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

func ptr(value string) *string { return &value }

func TestGetContact(t *testing.T) {
	fake := &fakeAPI{contactDetail: core.ContactDetail{ID: 99, Name: "Ana", Email: "ana@example.com"}}
	contact, err := New(fake).GetContact(context.Background(), 99)
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	if contact.ID != 99 || contact.Name != "Ana" {
		t.Fatalf("contact = %#v", contact)
	}
}

func TestGetContactRejectsNonPositiveID(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).GetContact(context.Background(), 0)
	requireCode(t, err, CodeInvalidInput)
	if fake.contactDetailCalls != 0 {
		t.Fatalf("API called for id 0")
	}
}

func TestUpdateContactSendsOnlyProvidedFields(t *testing.T) {
	fake := &fakeAPI{
		contactDetail: core.ContactDetail{ID: 99, Name: "Ana"},
		updateContact: core.ContactDetail{ID: 99, Name: "Ana Maria", Email: "ana@example.com"},
	}
	contact, err := New(fake).UpdateContact(context.Background(), 99, ptr("Ana Maria"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateContact: %v", err)
	}
	if len(fake.updateReqs) != 1 {
		t.Fatalf("update requests = %#v", fake.updateReqs)
	}
	req := fake.updateReqs[0]
	if req.Name == nil || *req.Name != "Ana Maria" || req.Email != nil || req.Phone != nil {
		t.Fatalf("request = %#v, want only name set", req)
	}
	if fake.contactDetailCalls != 1 {
		t.Fatalf("contact validation calls = %d, want 1", fake.contactDetailCalls)
	}
	if contact.Name != "Ana Maria" {
		t.Fatalf("contact = %#v", contact)
	}
}

func TestUpdateContactRejectsNoFields(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).UpdateContact(context.Background(), 99, nil, nil, nil)
	requireCode(t, err, CodeInvalidInput)
	if fake.contactDetailCalls != 0 || len(fake.updateReqs) != 0 {
		t.Fatalf("API called with no fields")
	}
}

func TestUpdateContactRejectsBlankField(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).UpdateContact(context.Background(), 99, ptr("   "), nil, nil)
	requireCode(t, err, CodeInvalidInput)
	if len(fake.updateReqs) != 0 {
		t.Fatalf("updated with a blank field")
	}
}

func TestUpdateContactValidatesContactExists(t *testing.T) {
	fake := &fakeAPI{contactDetailErr: &chatwoot.Error{Kind: chatwoot.KindNotFound, StatusCode: 404}}
	_, err := New(fake).UpdateContact(context.Background(), 99, ptr("Ana"), nil, nil)
	requireCode(t, err, CodeNotFound)
	if len(fake.updateReqs) != 0 {
		t.Fatalf("updated a contact that does not exist")
	}
}

func TestCreateConversationOnSupportedChannel(t *testing.T) {
	fake := &fakeAPI{
		contactDetail: core.ContactDetail{ID: 99},
		inbox:         core.Inbox{ID: 1, ChannelType: channelWebWidget},
		createConv:    core.Conversation{ID: 77, InboxID: 1, ChannelType: channelWebWidget, Status: "open"},
	}
	result, err := New(fake).CreateConversation(context.Background(), 1, 99, "")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if len(fake.createConvReqs) != 1 || fake.createConvReqs[0].InboxID != 1 || fake.createConvReqs[0].ContactID != 99 {
		t.Fatalf("requests = %#v", fake.createConvReqs)
	}
	if result.ConversationID != 77 || result.InboxID != 1 || result.ChannelType != channelWebWidget || result.Status != "open" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateConversationRejectsUnsupportedChannel(t *testing.T) {
	fake := &fakeAPI{
		contactDetail: core.ContactDetail{ID: 99},
		inbox:         core.Inbox{ID: 1, ChannelType: channelWhatsapp},
	}
	_, err := New(fake).CreateConversation(context.Background(), 1, 99, "")
	requireCode(t, err, CodeChannelUnsupported)
	if len(fake.createConvReqs) != 0 {
		t.Fatalf("created a conversation on a channel that cannot initiate")
	}
}

func TestCreateConversationValidatesContact(t *testing.T) {
	fake := &fakeAPI{contactDetailErr: &chatwoot.Error{Kind: chatwoot.KindNotFound, StatusCode: 404}}
	_, err := New(fake).CreateConversation(context.Background(), 1, 99, "")
	requireCode(t, err, CodeNotFound)
	if fake.inboxCalls != 0 || len(fake.createConvReqs) != 0 {
		t.Fatalf("continued after a missing contact")
	}
}

func TestCreateConversationValidatesInbox(t *testing.T) {
	fake := &fakeAPI{contactDetail: core.ContactDetail{ID: 99}, inboxErr: &chatwoot.Error{Kind: chatwoot.KindNotFound, StatusCode: 404}}
	_, err := New(fake).CreateConversation(context.Background(), 1, 99, "")
	requireCode(t, err, CodeNotFound)
	if len(fake.createConvReqs) != 0 {
		t.Fatalf("created a conversation with a missing inbox")
	}
}

func TestCreateConversationDeliveryUnknownWithoutRetry(t *testing.T) {
	fake := &fakeAPI{
		contactDetail: core.ContactDetail{ID: 99},
		inbox:         core.Inbox{ID: 1, ChannelType: channelAPI},
		createConvErr: &chatwoot.Error{Kind: chatwoot.KindTimeout, Resource: "conversation"},
	}
	_, err := New(fake).CreateConversation(context.Background(), 1, 99, "")
	requireCode(t, err, CodeDeliveryUnknown)
	if len(fake.createConvReqs) != 1 {
		t.Fatalf("create requests = %d, want exactly 1", len(fake.createConvReqs))
	}
}
