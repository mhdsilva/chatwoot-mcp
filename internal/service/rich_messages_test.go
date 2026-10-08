package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R'}

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestSendAttachmentSendsLocalFile(t *testing.T) {
	path := writeTempFile(t, "pic.png", pngHeader)
	fake := &fakeAPI{
		getConv:       core.Conversation{ID: 42, ChannelType: channelWebWidget, CanReply: true},
		createMessage: core.Message{ID: 900, Status: "sent", Attachments: []core.MessageAttachment{{ID: 1, ContentType: "image/png"}}},
	}
	result, err := New(fake).SendAttachment(context.Background(), 42, path, "veja")
	if err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", fake.createCalls)
	}
	req := fake.createReqs[0]
	if req.Attachment == nil || req.Attachment.Filename != "pic.png" || !strings.HasPrefix(req.Attachment.ContentType, "image/") {
		t.Fatalf("request = %#v", req)
	}
	if req.Content != "veja" {
		t.Fatalf("content = %q", req.Content)
	}
	if result.Filename != "pic.png" || result.Size != int64(len(pngHeader)) || result.Delivery != DeliveryAcceptedByAPI {
		t.Fatalf("result = %#v", result)
	}
}

func TestSendAttachmentRejectsRemoteURL(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SendAttachment(context.Background(), 42, "https://example.com/pic.png", "")
	requireCode(t, err, CodeInvalidInput)
	if fake.getCalls != 0 || fake.createCalls != 0 {
		t.Fatalf("API called for a remote URL")
	}
}

func TestSendAttachmentRejectsRelativePath(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SendAttachment(context.Background(), 42, "pic.png", "")
	requireCode(t, err, CodeInvalidInput)
}

func TestSendAttachmentRejectsDirectory(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SendAttachment(context.Background(), 42, t.TempDir(), "")
	requireCode(t, err, CodeInvalidInput)
}

func TestSendAttachmentRejectsMissingFile(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).SendAttachment(context.Background(), 42, filepath.Join(t.TempDir(), "missing.png"), "")
	requireCode(t, err, CodeInvalidInput)
}

func TestSendAttachmentRejectsUnsupportedChannel(t *testing.T) {
	path := writeTempFile(t, "pic.png", pngHeader)
	fake := &fakeAPI{getConv: core.Conversation{ID: 42, ChannelType: channelSms, CanReply: true}}
	_, err := New(fake).SendAttachment(context.Background(), 42, path, "")
	requireCode(t, err, CodeChannelUnsupported)
	if fake.createCalls != 0 {
		t.Fatalf("uploaded to an unsupported channel")
	}
}

func TestSendAttachmentEnforcesChannelSize(t *testing.T) {
	large := append(append([]byte{}, pngHeader...), bytes.Repeat([]byte{0}, (3<<20)+1)...)
	path := writeTempFile(t, "big.png", large)
	fake := &fakeAPI{getConv: core.Conversation{ID: 42, ChannelType: channelTiktok, CanReply: true}}
	_, err := New(fake).SendAttachment(context.Background(), 42, path, "")
	requireCode(t, err, CodeChannelUnsupported)
	if fake.createCalls != 0 {
		t.Fatalf("uploaded a file above the channel limit")
	}
}

func TestSendAttachmentRequiresCanReply(t *testing.T) {
	path := writeTempFile(t, "pic.png", pngHeader)
	fake := &fakeAPI{getConv: core.Conversation{ID: 42, ChannelType: channelWebWidget, CanReply: false}}
	_, err := New(fake).SendAttachment(context.Background(), 42, path, "")
	requireCode(t, err, CodeCannotReply)
	if fake.createCalls != 0 {
		t.Fatalf("uploaded to a conversation that cannot reply")
	}
}

func TestSendAttachmentDeliveryUnknownWithoutRetry(t *testing.T) {
	path := writeTempFile(t, "pic.png", pngHeader)
	fake := &fakeAPI{
		getConv:   core.Conversation{ID: 42, ChannelType: channelWebWidget, CanReply: true},
		createErr: &chatwoot.Error{Kind: chatwoot.KindTimeout, Resource: "conversation 42 message"},
	}
	_, err := New(fake).SendAttachment(context.Background(), 42, path, "")
	requireCode(t, err, CodeDeliveryUnknown)
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want exactly 1", fake.createCalls)
	}
}

func TestListMessageTemplatesOnWhatsApp(t *testing.T) {
	fake := &fakeAPI{
		inbox:     core.Inbox{ID: 5, ChannelType: channelWhatsapp},
		templates: []core.Template{{Name: "welcome", Language: "en_US"}},
	}
	templates, err := New(fake).ListMessageTemplates(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListMessageTemplates: %v", err)
	}
	if fake.templatesInbox != 5 || len(templates) != 1 || templates[0].Name != "welcome" {
		t.Fatalf("templates = %#v (inbox %d)", templates, fake.templatesInbox)
	}
}

func TestListMessageTemplatesRejectsNonWhatsApp(t *testing.T) {
	fake := &fakeAPI{inbox: core.Inbox{ID: 5, ChannelType: channelEmail}}
	_, err := New(fake).ListMessageTemplates(context.Background(), 5)
	requireCode(t, err, CodeChannelUnsupported)
	if fake.templatesCalls != 0 {
		t.Fatalf("listed templates for a non-WhatsApp inbox")
	}
}

func TestSendTemplateRequiresWhatsApp(t *testing.T) {
	fake := &fakeAPI{getConv: core.Conversation{ID: 42, ChannelType: channelEmail}}
	_, err := New(fake).SendTemplate(context.Background(), 42, TemplateInput{Name: "welcome", Language: "en_US", Category: "UTILITY"})
	requireCode(t, err, CodeChannelUnsupported)
	if fake.createCalls != 0 {
		t.Fatalf("sent a template on a non-WhatsApp channel")
	}
}

func TestSendTemplateValidatesInput(t *testing.T) {
	tests := []struct {
		name string
		in   TemplateInput
	}{
		{"missing name", TemplateInput{Language: "en_US", Category: "UTILITY"}},
		{"missing language", TemplateInput{Name: "welcome", Category: "UTILITY"}},
		{"bad category", TemplateInput{Name: "welcome", Language: "en_US", Category: "PROMO"}},
		{"bad content mode", TemplateInput{Name: "welcome", Language: "en_US", Category: "UTILITY", ContentMode: "guess"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeAPI{}
			_, err := New(fake).SendTemplate(context.Background(), 42, test.in)
			requireCode(t, err, CodeInvalidInput)
			if fake.getCalls != 0 || fake.createCalls != 0 {
				t.Fatalf("API called for invalid template input")
			}
		})
	}
}

func TestSendTemplateRejectsUnapprovedTemplate(t *testing.T) {
	fake := &fakeAPI{
		getConv:   core.Conversation{ID: 42, InboxID: 5, ChannelType: channelWhatsapp},
		templates: []core.Template{{Name: "other", Language: "en_US"}},
	}
	_, err := New(fake).SendTemplate(context.Background(), 42, TemplateInput{Name: "welcome", Language: "en_US", Category: "UTILITY"})
	requireCode(t, err, CodeTemplateNotFound)
	if fake.createCalls != 0 {
		t.Fatalf("sent an unapproved template")
	}
}

func TestSendTemplateSendsApprovedTemplateOutsideWindow(t *testing.T) {
	fake := &fakeAPI{
		getConv:       core.Conversation{ID: 42, InboxID: 5, ChannelType: channelWhatsapp, CanReply: false},
		templates:     []core.Template{{Name: "welcome", Language: "en_US"}},
		createMessage: core.Message{ID: 901, Status: "sent"},
	}
	result, err := New(fake).SendTemplate(context.Background(), 42, TemplateInput{
		Name:            "welcome",
		Language:        "en_US",
		Category:        "UTILITY",
		ProcessedParams: map[string]any{"body": map[string]any{"1": "Ana"}},
	})
	if err != nil {
		t.Fatalf("SendTemplate: %v", err)
	}
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", fake.createCalls)
	}
	req := fake.createReqs[0]
	if req.Template == nil || req.Template.Name != "welcome" || req.Template.Category != "UTILITY" {
		t.Fatalf("request = %#v", req)
	}
	if req.Content != "welcome" {
		t.Fatalf("content = %q, want the template name default", req.Content)
	}
	if result.Message.ID != 901 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSendTemplateDeliveryUnknownWithoutRetry(t *testing.T) {
	fake := &fakeAPI{
		getConv:   core.Conversation{ID: 42, InboxID: 5, ChannelType: channelWhatsapp},
		createErr: &chatwoot.Error{Kind: chatwoot.KindTransport, Resource: "conversation 42 message"},
		templates: []core.Template{},
	}
	_, err := New(fake).SendTemplate(context.Background(), 42, TemplateInput{Name: "welcome", Language: "en_US", Category: "UTILITY"})
	requireCode(t, err, CodeDeliveryUnknown)
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want exactly 1", fake.createCalls)
	}
}
