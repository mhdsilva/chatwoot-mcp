package analytics

import (
	"context"
	"sort"
	"time"

	"chatwoot-mcp/internal/core"
)

const (
	// QueueDefaultStatus is the status filter used when none is given.
	QueueDefaultStatus = "open"
	// QueueDefaultLimit is the row limit used when none is given.
	QueueDefaultLimit = 20
	// QueueMaxLimit is the largest accepted row limit.
	QueueMaxLimit = 50
	// QueueDefaultStartPage is the first page scanned when none is given.
	QueueDefaultStartPage = 1
	// QueueMaxPages bounds every call to at most ten upstream pages.
	QueueMaxPages = 10
)

// QueueRequest selects the attention queue slice to return. Zero IDs do not
// filter their field; zero limit and start page take the defaults.
type QueueRequest struct {
	Status     string `json:"status"`
	InboxID    int64  `json:"inbox_id"`
	TeamID     int64  `json:"team_id"`
	AssigneeID int64  `json:"assignee_id"`
	Limit      int    `json:"limit"`
	StartPage  int    `json:"start_page"`
}

// AttentionConversation is one row of the attention queue. It carries only
// conversation metadata, never message content.
type AttentionConversation struct {
	ConversationID int64      `json:"conversation_id"`
	ContactID      int64      `json:"contact_id"`
	InboxID        int64      `json:"inbox_id"`
	ChannelType    string     `json:"channel_type,omitempty"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority,omitempty"`
	AssigneeID     int64      `json:"assignee_id,omitempty"`
	TeamID         int64      `json:"team_id,omitempty"`
	WaitingSince   time.Time  `json:"waiting_since"`
	WaitingSeconds int64      `json:"waiting_seconds"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
	UnreadCount    int        `json:"unread_count,omitempty"`
}

// QueueResult reports one bounded queue scan. Complete and Truncated are set
// independently: a partial scan is never complete, and truncation says only
// that eligible rows inside the scan were dropped by the limit.
type QueueResult struct {
	ObservedAt           time.Time               `json:"observed_at"`
	Conversations        []AttentionConversation `json:"conversations"`
	ScannedPages         int                     `json:"scanned_pages"`
	ScannedConversations int                     `json:"scanned_conversations"`
	Complete             bool                    `json:"complete"`
	NextPage             int                     `json:"next_page"`
	Truncated            bool                    `json:"truncated"`
}

func (s *service) ListAttentionQueue(ctx context.Context, request QueueRequest) (QueueResult, error) {
	status := request.Status
	if status == "" {
		status = QueueDefaultStatus
	}
	if status != "open" && status != "pending" {
		return QueueResult{}, invalidInput("status must be open or pending")
	}
	if request.InboxID < 0 || request.TeamID < 0 || request.AssigneeID < 0 {
		return QueueResult{}, invalidInput("inbox_id, team_id, and assignee_id must not be negative")
	}
	limit := request.Limit
	if limit == 0 {
		limit = QueueDefaultLimit
	}
	if limit < 0 {
		return QueueResult{}, invalidInput("limit must not be negative")
	}
	if limit > QueueMaxLimit {
		return QueueResult{}, invalidInput("limit must not exceed 50")
	}
	startPage := request.StartPage
	if startPage == 0 {
		startPage = QueueDefaultStartPage
	}
	if startPage < 0 {
		return QueueResult{}, invalidInput("start_page must not be negative")
	}

	observedAt := s.now().UTC()
	result := QueueResult{ObservedAt: observedAt, Conversations: []AttentionConversation{}}

	page := startPage
	for scanned := 0; scanned < QueueMaxPages; scanned++ {
		listing, err := s.conversations.ListConversations(ctx, core.ListOptions{
			Page:    page,
			Status:  status,
			InboxID: request.InboxID,
			TeamID:  request.TeamID,
		})
		if err != nil {
			return QueueResult{}, mapSourceError(err)
		}

		result.ScannedPages++
		result.ScannedConversations += len(listing.Items)
		for _, conversation := range listing.Items {
			if request.AssigneeID > 0 && conversation.AssigneeID != request.AssigneeID {
				continue
			}
			if conversation.WaitingSince <= 0 {
				continue
			}
			result.Conversations = append(result.Conversations, projectConversation(conversation, observedAt))
		}

		next := listing.NextPage
		if next == 0 {
			// The account's last page was reached inside the page budget.
			result.Complete = true
			result.NextPage = 0
			return finalizeQueue(result, limit), nil
		}
		if next <= page {
			// Unusable cursor: repeated or backward. Stop without looping and
			// never report a partial scan as complete.
			result.Complete = false
			result.NextPage = 0
			return finalizeQueue(result, limit), nil
		}
		page = next
	}

	// Page budget exhausted before the account's last page.
	result.Complete = false
	result.NextPage = page
	return finalizeQueue(result, limit), nil
}

// finalizeQueue sorts rows by waiting time then conversation ID, applies the
// row limit, and sets completeness and truncation independently.
func finalizeQueue(result QueueResult, limit int) QueueResult {
	sort.Slice(result.Conversations, func(i, j int) bool {
		left, right := result.Conversations[i], result.Conversations[j]
		if left.WaitingSeconds != right.WaitingSeconds {
			return left.WaitingSeconds > right.WaitingSeconds
		}
		return left.ConversationID < right.ConversationID
	})
	if len(result.Conversations) > limit {
		result.Conversations = result.Conversations[:limit]
		result.Truncated = true
	}
	return result
}

func projectConversation(conversation core.Conversation, observedAt time.Time) AttentionConversation {
	waitingSince := time.Unix(conversation.WaitingSince, 0).UTC()
	waitingSeconds := observedAt.Unix() - conversation.WaitingSince
	if waitingSeconds < 0 {
		// A future waiting_since (clock skew upstream) must never report a
		// negative wait.
		waitingSeconds = 0
	}
	var lastActivityAt *time.Time
	if conversation.LastActivityAt > 0 {
		value := time.Unix(conversation.LastActivityAt, 0).UTC()
		lastActivityAt = &value
	}
	return AttentionConversation{
		ConversationID: conversation.ID,
		ContactID:      conversation.ContactID,
		InboxID:        conversation.InboxID,
		ChannelType:    conversation.ChannelType,
		Status:         conversation.Status,
		Priority:       conversation.Priority,
		AssigneeID:     conversation.AssigneeID,
		TeamID:         conversation.TeamID,
		WaitingSince:   waitingSince,
		WaitingSeconds: waitingSeconds,
		LastActivityAt: lastActivityAt,
		UnreadCount:    conversation.UnreadCount,
	}
}
