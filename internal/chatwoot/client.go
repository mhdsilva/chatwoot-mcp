// Package chatwoot implements the subset of the Chatwoot Application API used
// by the v1 MCP server. It authenticates with the api_access_token header and
// speaks to /api/v1/accounts/{account_id}. The client never retries requests
// and never follows redirects, so a POST that may have reached Chatwoot is
// neither duplicated nor replayed against another host.
package chatwoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"chatwoot-mcp/internal/core"
)

const (
	defaultTimeout           = 30 * time.Second
	conversationPageSize     = 25
	contactSearchPageSize    = 15
	contactConversationsPage = 25
	messagePageSize          = 20
	maxConversationMessages  = 60
	maxResponseBytes         = 8 << 20
	maxErrorBodyBytes        = 8 << 10
)

// Kind classifies a Chatwoot API failure for callers that only hold an error.
type Kind string

const (
	KindUnauthorized Kind = "unauthorized"
	KindForbidden    Kind = "forbidden"
	KindNotFound     Kind = "not_found"
	KindRateLimited  Kind = "rate_limited"
	KindTimeout      Kind = "timeout"
	KindServer       Kind = "server_error"
	KindTransport    Kind = "transport_error"
	KindRequest      Kind = "request_error"
	KindInvalid      Kind = "invalid_response"
)

// Error is a normalized Chatwoot API failure. It never carries the token.
type Error struct {
	Kind               Kind
	StatusCode         int
	Resource           string
	Message            string
	RetryAfter         time.Duration
	ResponseFormat     string
	ResponseDecodeKind string
	ResponseField      string
	ExpectedJSONType   string
	ActualJSONType     string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("chatwoot: ")
	switch e.Kind {
	case KindUnauthorized:
		b.WriteString("unauthorized")
	case KindForbidden:
		b.WriteString("forbidden")
	case KindNotFound:
		b.WriteString("not found")
	case KindRateLimited:
		b.WriteString("rate limited")
	case KindTimeout:
		b.WriteString("timeout")
	case KindServer:
		b.WriteString("server error")
	case KindTransport:
		b.WriteString("transport error")
	case KindRequest:
		b.WriteString("request error")
	case KindInvalid:
		b.WriteString("invalid response")
	default:
		b.WriteString(string(e.Kind))
	}
	if e.StatusCode > 0 {
		fmt.Fprintf(&b, " (%d)", e.StatusCode)
	}
	if e.Resource != "" {
		fmt.Fprintf(&b, " %s", e.Resource)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.RetryAfter > 0 {
		fmt.Fprintf(&b, " (retry after %s)", e.RetryAfter)
	}
	return b.String()
}

func matchesKind(err error, want Kind) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Kind == want
}

// IsUnauthorized reports whether err is an invalid or expired token failure.
func IsUnauthorized(err error) bool { return matchesKind(err, KindUnauthorized) }

// IsForbidden reports whether err is a valid token without the required permission.
func IsForbidden(err error) bool { return matchesKind(err, KindForbidden) }

// IsNotFound reports whether err names a missing resource.
func IsNotFound(err error) bool { return matchesKind(err, KindNotFound) }

// IsRateLimited reports whether err is a Chatwoot rate limit response.
func IsRateLimited(err error) bool { return matchesKind(err, KindRateLimited) }

// IsTimeout reports whether err is a network or context timeout.
func IsTimeout(err error) bool { return matchesKind(err, KindTimeout) }

// IsServerError reports whether err is a 5xx response.
func IsServerError(err error) bool { return matchesKind(err, KindServer) }

// Client is a Chatwoot Application API client for a single account.
type Client struct {
	baseURL    string
	accountID  int64
	token      string
	httpClient *http.Client
}

// NewClient builds a client for one account. A nil httpClient uses the
// default timeout.
func NewClient(settings core.Settings, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	// Work on a copy so the caller's client is left untouched. Errors like
	// ErrUseLastResponse stop the transport from following any redirect, so a
	// POST is never replayed and the token never reaches another host.
	noRedirect := *httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		baseURL:    strings.TrimRight(settings.BaseURL, "/"),
		accountID:  settings.AccountID,
		token:      settings.Token,
		httpClient: &noRedirect,
	}
}

func (c *Client) accountPath() string {
	return "/api/v1/accounts/" + strconv.FormatInt(c.accountID, 10)
}

func (c *Client) url(path string, query url.Values) string {
	if len(query) == 0 {
		return c.baseURL + path
	}
	return c.baseURL + path + "?" + query.Encode()
}

type errorEnvelope struct {
	Error       string `json:"error"`
	Description string `json:"description"`
	Errors      []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, reqBody, resBody any, resource string) error {
	var reader io.Reader
	if reqBody != nil {
		payload, err := json.Marshal(reqBody)
		if err != nil {
			return &Error{Kind: KindRequest, Resource: resource, Message: "encode request: " + err.Error()}
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), reader)
	if err != nil {
		return &Error{Kind: KindRequest, Resource: resource, Message: err.Error()}
	}
	req.Header.Set("api_access_token", c.token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return transportError(ctx, err, resource)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return responseError(resp, resource)
	}
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		message := "unexpected redirect"
		if location := resp.Header.Get("Location"); location != "" {
			message += " to " + location
		}
		return &Error{Kind: KindTransport, StatusCode: resp.StatusCode, Resource: resource, Message: message}
	}
	if resBody == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return nil
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		if isTimeoutError(ctx, readErr) {
			return &Error{Kind: KindTimeout, StatusCode: resp.StatusCode, Resource: resource, Message: readErr.Error()}
		}
		return &Error{Kind: KindTransport, StatusCode: resp.StatusCode, Resource: resource, Message: "read response: " + readErr.Error()}
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(resBody); err != nil {
		if isTimeoutError(ctx, err) {
			return &Error{Kind: KindTimeout, StatusCode: resp.StatusCode, Resource: resource, Message: err.Error()}
		}
		decodeKind, field, expectedType, actualType := responseDecodeDiagnostics(err)
		return &Error{
			Kind:               KindInvalid,
			StatusCode:         resp.StatusCode,
			Resource:           resource,
			Message:            "decode response: " + err.Error(),
			ResponseFormat:     responseFormat(body, resp.Header.Get("Content-Type")),
			ResponseDecodeKind: decodeKind,
			ResponseField:      field,
			ExpectedJSONType:   expectedType,
			ActualJSONType:     actualType,
		}
	}
	return nil
}

// responseFormat returns a small, safe category for a malformed response. It
// never includes the body or arbitrary Content-Type parameters.
func responseFormat(body []byte, contentType string) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "empty"
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "text/html" || bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<!doctype html")) || bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<html")) {
		return "html"
	}
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") || trimmed[0] == '{' || trimmed[0] == '[' {
		return "json"
	}
	return "other"
}

// responseDecodeKind maps decoder errors to stable names without exposing any
// bytes from the upstream response.
func responseDecodeKind(err error) string {
	kind, _, _, _ := responseDecodeDiagnostics(err)
	return kind
}

func responseDecodeDiagnostics(err error) (kind, field, expectedType, actualType string) {
	switch {
	case errors.Is(err, io.EOF):
		return "empty_body", "", "", ""
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated_json", "", "", ""
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		if strings.Contains(syntaxErr.Error(), "unexpected end of JSON input") {
			return "truncated_json", "", "", ""
		}
		return "malformed_json", "", "", ""
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		expected := typeErr.Type
		if expected.Kind() == reflect.Pointer {
			expected = expected.Elem()
		}
		return "unexpected_json_type", typeErr.Field, jsonTypeName(expected.Kind()), jsonValueType(typeErr.Value)
	}
	return "decode_error", "", "", ""
}

func jsonValueType(value string) string {
	switch value {
	case "object", "array", "string", "number", "bool", "null":
		if value == "bool" {
			return "boolean"
		}
		return value
	default:
		return "unknown"
	}
}

func jsonTypeName(kind reflect.Kind) string {
	switch kind {
	case reflect.Struct, reflect.Map:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Invalid:
		return "null"
	default:
		return "unknown"
	}
}

func transportError(ctx context.Context, err error, resource string) *Error {
	if isTimeoutError(ctx, err) {
		return &Error{Kind: KindTimeout, Resource: resource, Message: err.Error()}
	}
	return &Error{Kind: KindTransport, Resource: resource, Message: err.Error()}
}

func isTimeoutError(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func responseError(resp *http.Response, resource string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)

	message := strings.TrimSpace(env.Error)
	if message == "" {
		message = strings.TrimSpace(env.Description)
	}
	if message == "" && len(env.Errors) > 0 {
		parts := make([]string, 0, len(env.Errors))
		for _, item := range env.Errors {
			if m := strings.TrimSpace(item.Message); m != "" {
				parts = append(parts, m)
			}
		}
		message = strings.Join(parts, "; ")
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}

	apiErr := &Error{
		StatusCode: resp.StatusCode,
		Resource:   resource,
		Message:    message,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		apiErr.Kind = KindUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		apiErr.Kind = KindForbidden
	case resp.StatusCode == http.StatusNotFound:
		apiErr.Kind = KindNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		apiErr.Kind = KindRateLimited
	case resp.StatusCode >= 500:
		apiErr.Kind = KindServer
	default:
		apiErr.Kind = KindRequest
	}
	return apiErr
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d.Round(time.Second)
		}
	}
	return 0
}

type messageWire struct {
	ID          int64            `json:"id"`
	Content     string           `json:"content"`
	MessageType int              `json:"message_type"`
	ContentType string           `json:"content_type"`
	Attachments []attachmentWire `json:"attachments"`
	Private     bool             `json:"private"`
	Status      string           `json:"status"`
	CreatedAt   int64            `json:"created_at"`
}

func (m messageWire) toCore() core.Message {
	attachments := make([]core.MessageAttachment, 0, len(m.Attachments))
	for _, attachment := range m.Attachments {
		attachments = append(attachments, attachment.toCore())
	}
	return core.Message{
		ID:          m.ID,
		Content:     m.Content,
		MessageType: m.MessageType,
		ContentType: m.ContentType,
		Attachments: attachments,
		Private:     m.Private,
		Status:      m.Status,
		CreatedAt:   m.CreatedAt,
	}
}

type attachmentWire struct {
	ID          int64  `json:"id"`
	FileType    string `json:"file_type"`
	Extension   string `json:"extension"`
	ContentType string `json:"content_type"`
	FileSize    int64  `json:"file_size"`
}

func (a attachmentWire) toCore() core.MessageAttachment {
	return core.MessageAttachment{
		ID:          a.ID,
		FileType:    a.FileType,
		Extension:   a.Extension,
		ContentType: a.ContentType,
		FileSize:    a.FileSize,
	}
}

type conversationWire struct {
	ID        int64         `json:"id"`
	InboxID   int64         `json:"inbox_id"`
	ContactID int64         `json:"contact_id"`
	Status    string        `json:"status"`
	CanReply  bool          `json:"can_reply"`
	Messages  []messageWire `json:"messages"`
	Meta      struct {
		Sender struct {
			ID int64 `json:"id"`
		} `json:"sender"`
	} `json:"meta"`
}

type conversationMessagesEnvelope struct {
	Payload []messageWire `json:"payload"`
}

func (c conversationWire) toCore() core.Conversation {
	contactID := c.ContactID
	if contactID == 0 {
		contactID = c.Meta.Sender.ID
	}
	messages := make([]core.Message, 0, len(c.Messages))
	for _, m := range c.Messages {
		messages = append(messages, m.toCore())
	}
	return core.Conversation{
		ID:        c.ID,
		InboxID:   c.InboxID,
		ContactID: contactID,
		Status:    c.Status,
		CanReply:  c.CanReply,
		Messages:  messages,
	}
}

type contactWire struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

func (c contactWire) toCore() core.Contact {
	return core.Contact{ID: c.ID, Name: c.Name, Email: c.Email, Phone: c.PhoneNumber}
}

type conversationMeta struct {
	AllCount *int `json:"all_count"`
}

// jsonInt accepts pagination counts encoded as either JSON numbers or numeric
// strings, which Chatwoot installations may return from the search endpoint.
type jsonInt int

func (n *jsonInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return io.ErrUnexpectedEOF
	}
	value := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("expected integer or numeric string")
	}
	*n = jsonInt(parsed)
	return nil
}

type pageMeta struct {
	Count       *jsonInt `json:"count"`
	CurrentPage *jsonInt `json:"current_page"`
	HasMore     *bool    `json:"has_more"`
}

type conversationListEnvelope struct {
	Data struct {
		Meta    *conversationMeta  `json:"meta"`
		Payload []conversationWire `json:"payload"`
	} `json:"data"`
}

type contactSearchEnvelope struct {
	Meta    *pageMeta     `json:"meta"`
	Payload []contactWire `json:"payload"`
}

type contactConversationsEnvelope struct {
	Meta    *pageMeta          `json:"meta"`
	Payload []conversationWire `json:"payload"`
}

// Check verifies the token against the account and profile endpoints and
// returns the account and user names without exposing the token.
func (c *Client) Check(ctx context.Context) (core.Identity, error) {
	var account struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.call(ctx, http.MethodGet, c.accountPath(), nil, nil, &account, "account"); err != nil {
		return core.Identity{}, err
	}

	var profile struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/v1/profile", nil, nil, &profile, "profile"); err != nil {
		return core.Identity{}, err
	}

	accountID := account.ID
	if accountID == 0 {
		accountID = c.accountID
	}
	return core.Identity{AccountID: accountID, AccountName: account.Name, UserName: profile.Name}, nil
}

// ListConversations lists conversations with the documented status, inbox and
// page filters.
func (c *Client) ListConversations(ctx context.Context, opts core.ListOptions) (core.Page[core.Conversation], error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	if opts.Status != "" {
		query.Set("status", opts.Status)
	}
	if opts.InboxID > 0 {
		query.Set("inbox_id", strconv.FormatInt(opts.InboxID, 10))
	}

	var env conversationListEnvelope
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/conversations", query, nil, &env, "conversations"); err != nil {
		return core.Page[core.Conversation]{}, err
	}

	result := core.Page[core.Conversation]{Items: make([]core.Conversation, 0, len(env.Data.Payload))}
	for _, item := range env.Data.Payload {
		result.Items = append(result.Items, item.toCore())
	}
	result.NextPage = nextConversationPage(page, env.Data.Meta, len(env.Data.Payload))
	return result, nil
}

func nextConversationPage(page int, meta *conversationMeta, returned int) int {
	if meta != nil && meta.AllCount != nil {
		if page*conversationPageSize < *meta.AllCount {
			return page + 1
		}
		return 0
	}
	if returned >= conversationPageSize {
		return page + 1
	}
	return 0
}

// GetConversation returns one conversation with its messages.
func (c *Client) GetConversation(ctx context.Context, id int64) (core.Conversation, error) {
	resource := "conversation " + strconv.FormatInt(id, 10)
	var conv conversationWire
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/conversations/"+strconv.FormatInt(id, 10), nil, nil, &conv, resource); err != nil {
		return core.Conversation{}, err
	}
	messages, messageCount, messageCountExact, err := c.conversationMessages(ctx, id)
	if err != nil {
		return core.Conversation{}, err
	}
	result := conv.toCore()
	result.Messages = make([]core.Message, len(messages))
	for i, message := range messages {
		result.Messages[i] = message.toCore()
	}
	result.MessageCount = messageCount
	result.MessageCountExact = messageCountExact
	return result, nil
}

func (c *Client) conversationMessages(ctx context.Context, conversationID int64) ([]messageWire, int, bool, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(conversationID, 10) + "/messages"
	var messages []messageWire
	var before int64
	seen := make(map[int64]struct{}, maxConversationMessages)
	exact := false
	for {
		query := url.Values{}
		if before > 0 {
			query.Set("before", strconv.FormatInt(before, 10))
		}
		var envelope conversationMessagesEnvelope
		if err := c.call(ctx, http.MethodGet, path, query, nil, &envelope, "conversation messages"); err != nil {
			return nil, 0, false, err
		}
		page := envelope.Payload
		if len(page) == 0 {
			exact = true
			break
		}
		older := make([]messageWire, 0, len(page))
		for _, message := range page {
			if _, ok := seen[message.ID]; ok {
				continue
			}
			seen[message.ID] = struct{}{}
			older = append(older, message)
		}
		messages = append(older, messages...)
		if len(messages) > maxConversationMessages {
			messages = messages[len(messages)-maxConversationMessages:]
		}
		if len(page) < messagePageSize {
			exact = true
			break
		}
		if page[0].ID <= 0 || (before > 0 && page[0].ID >= before) {
			break
		}
		before = page[0].ID
		if len(seen) >= maxConversationMessages {
			break
		}
	}
	return messages, len(seen), exact, nil
}

// SearchContacts searches resolved contacts by name, email, phone or identifier.
func (c *Client) SearchContacts(ctx context.Context, query string, page int) (core.Page[core.Contact], error) {
	if page < 1 {
		page = 1
	}
	params := url.Values{}
	params.Set("q", query)
	params.Set("page", strconv.Itoa(page))

	var env contactSearchEnvelope
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/contacts/search", params, nil, &env, "contacts"); err != nil {
		return core.Page[core.Contact]{}, err
	}

	result := core.Page[core.Contact]{Items: make([]core.Contact, 0, len(env.Payload))}
	for _, item := range env.Payload {
		result.Items = append(result.Items, item.toCore())
	}
	result.NextPage = nextPage(page, env.Meta, contactSearchPageSize)
	return result, nil
}

// ContactConversations lists the conversations associated with a contact.
// Chatwoot bounds this response and does not paginate it, so NextPage is only
// set when the response carries explicit pagination metadata.
func (c *Client) ContactConversations(ctx context.Context, contactID int64, page int) (core.Page[core.Conversation], error) {
	if page < 1 {
		page = 1
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	path := c.accountPath() + "/contacts/" + strconv.FormatInt(contactID, 10) + "/conversations"
	resource := "contact " + strconv.FormatInt(contactID, 10) + " conversations"

	var env contactConversationsEnvelope
	if err := c.call(ctx, http.MethodGet, path, query, nil, &env, resource); err != nil {
		return core.Page[core.Conversation]{}, err
	}

	result := core.Page[core.Conversation]{Items: make([]core.Conversation, 0, len(env.Payload))}
	for _, item := range env.Payload {
		result.Items = append(result.Items, item.toCore())
	}
	result.NextPage = nextPage(page, env.Meta, contactConversationsPage)
	return result, nil
}

func nextPage(page int, meta *pageMeta, pageSize int) int {
	if meta == nil {
		return 0
	}
	if meta.HasMore != nil {
		if *meta.HasMore {
			return page + 1
		}
		return 0
	}
	if meta.Count != nil && meta.CurrentPage != nil && int(*meta.CurrentPage)*pageSize < int(*meta.Count) {
		return page + 1
	}
	return 0
}

// CreateMessage sends an outgoing text message. It performs a single POST and
// never retries, so an ambiguous failure cannot produce a duplicate reply.
func (c *Client) CreateMessage(ctx context.Context, conversationID int64, content string) (core.Message, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(conversationID, 10) + "/messages"
	resource := "conversation " + strconv.FormatInt(conversationID, 10) + " message"
	payload := map[string]string{"content": content, "message_type": "outgoing"}

	var msg messageWire
	if err := c.call(ctx, http.MethodPost, path, nil, payload, &msg, resource); err != nil {
		return core.Message{}, err
	}
	return msg.toCore(), nil
}
