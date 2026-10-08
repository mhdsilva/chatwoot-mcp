package chatwoot

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"chatwoot-mcp/internal/core"
)

// templateWire is a permissive projection of a channel template. The endpoint
// documents each item as free-form, and native and Twilio inboxes expose
// different field names, so several candidates are read and normalized.
type templateWire struct {
	Name         string `json:"name"`
	FriendlyName string `json:"friendly_name"`
	Language     string `json:"language"`
	Category     string `json:"category"`
	Status       string `json:"status"`
	Body         string `json:"body"`
	Components   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"components"`
}

func (t templateWire) toCore() core.Template {
	name := t.Name
	if name == "" {
		name = t.FriendlyName
	}
	body := t.Body
	if body == "" {
		for _, component := range t.Components {
			if strings.EqualFold(component.Type, "BODY") && component.Text != "" {
				body = component.Text
				break
			}
		}
	}
	return core.Template{
		Name:     name,
		Language: t.Language,
		Category: t.Category,
		Status:   t.Status,
		Body:     body,
	}
}

// ListTemplates lists the approved message templates cached for one WhatsApp
// inbox. A non-WhatsApp inbox is rejected by Chatwoot with a 422.
func (c *Client) ListTemplates(ctx context.Context, inboxID int64) ([]core.Template, error) {
	path := c.accountPath() + "/inboxes/" + strconv.FormatInt(inboxID, 10) + "/message_templates"
	resource := "inbox " + strconv.FormatInt(inboxID, 10) + " message templates"

	var envelope struct {
		Payload []templateWire `json:"payload"`
	}
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &envelope, resource); err != nil {
		return nil, err
	}
	templates := make([]core.Template, 0, len(envelope.Payload))
	for _, template := range envelope.Payload {
		templates = append(templates, template.toCore())
	}
	return templates, nil
}
