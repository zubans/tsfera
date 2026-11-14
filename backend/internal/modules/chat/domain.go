package chat

import (
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Mode string

const (
	ModeAgent Mode = "agent"
	ModeRAG   Mode = "rag"
)

const MaxAgentRounds = 6

type Conversation struct {
	ID         int64         `json:"id"`
	UserID     int64         `json:"user_id"`
	Title      string        `json:"title"`
	Mode       Mode          `json:"mode"`
	ModelID    int64         `json:"model_id"`
	EntityType string        `json:"entity_type,omitempty"`
	EntityID   int64         `json:"entity_id,omitempty"`
	Deleted    bool          `json:"deleted"`
	Messages   []ChatMessage `json:"messages,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type ToolStep struct {
	Name       string `json:"name"`
	Arguments  string `json:"arguments,omitempty"`
	Summary    string `json:"summary"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

type ChatMessage struct {
	ID             int64                `json:"id"`
	ConversationID int64                `json:"conversation_id"`
	Role           string               `json:"role"`
	Content        string               `json:"content"`
	Steps          []ToolStep           `json:"steps,omitempty"`
	Sources        []platform.SourceRef `json:"sources,omitempty"`
	TokensUsed     int                  `json:"tokens_used"`
	CreatedAt      time.Time            `json:"created_at"`
}

type ConversationShare struct {
	ID             int64      `json:"id"`
	ConversationID int64      `json:"conversation_id"`
	OwnerID        int64      `json:"owner_id"`
	ShareToken     string     `json:"share_token"`
	Revoked        bool       `json:"revoked"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type SSEEvent struct {
	Type    string               `json:"type"` // user | step | delta | sources | done | error
	Message *ChatMessage         `json:"message,omitempty"`
	Step    *ToolStep            `json:"step,omitempty"`
	Delta   string               `json:"delta,omitempty"`
	Sources []platform.SourceRef `json:"sources,omitempty"`
	Error   string               `json:"error,omitempty"`
}
