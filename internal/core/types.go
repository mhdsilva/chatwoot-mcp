package core

import "fmt"

// Settings are local connection credentials. Token must never be serialized
// into API, MCP, or panel responses.
type Settings struct {
	BaseURL   string `json:"base_url"`
	AccountID int64  `json:"account_id"`
	Token     string `json:"-"`
}

// PublicSettings is the token-free projection of Settings that panel and MCP
// responses may serialize. Token presence is exposed only as a boolean.
type PublicSettings struct {
	BaseURL   string `json:"base_url"`
	AccountID int64  `json:"account_id"`
	HasToken  bool   `json:"has_token"`
}

// Public returns the token-free projection of s.
func (s Settings) Public() PublicSettings {
	return PublicSettings{
		BaseURL:   s.BaseURL,
		AccountID: s.AccountID,
		HasToken:  s.Token != "",
	}
}

// String redacts the token so Settings cannot leak it through fmt verbs or logs.
func (s Settings) String() string {
	return fmt.Sprintf("Settings{BaseURL:%q, AccountID:%d, Token:<redacted>}", s.BaseURL, s.AccountID)
}

type Identity struct {
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	UserName    string `json:"user_name"`
}

type Message struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	Private   bool   `json:"private"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

type Conversation struct {
	ID        int64     `json:"id"`
	InboxID   int64     `json:"inbox_id"`
	ContactID int64     `json:"contact_id"`
	Status    string    `json:"status"`
	CanReply  bool      `json:"can_reply"`
	Messages  []Message `json:"messages"`
}

type Contact struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type Page[T any] struct {
	Items    []T `json:"items"`
	NextPage int `json:"next_page"`
}

type ListOptions struct {
	Page    int
	Status  string
	InboxID int64
}
