package ai

import (
	"encoding/json"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type ProviderKind string

const (
	ProviderOpenAI       ProviderKind = "openai"
	ProviderAnthropic    ProviderKind = "anthropic"
	ProviderGemini       ProviderKind = "gemini"
	ProviderOllama       ProviderKind = "ollama"
	ProviderOpenAICompat ProviderKind = "openai_compat"
)

type ModelCapability string

const (
	CapChat      ModelCapability = "chat"
	CapEmbedding ModelCapability = "embedding"
)

type Provider struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Kind      ProviderKind `json:"kind"`
	BaseURL   string       `json:"base_url"`
	APIKey    string       `json:"api_key,omitempty"`
	HasAPIKey bool         `json:"has_api_key"`
	Active    bool         `json:"active"`
	IsDefault bool         `json:"is_default"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Model struct {
	ID            int64           `json:"id"`
	ProviderID    int64           `json:"provider_id"`
	ProviderName  string          `json:"provider_name,omitempty"`
	ProviderKind  ProviderKind    `json:"provider_kind,omitempty"`
	Name          string          `json:"name"`
	ModelID       string          `json:"model_id"`
	Capability    ModelCapability `json:"capability"`
	ContextWindow int             `json:"context_window"`
	IsDefault     bool            `json:"is_default"`
	OpenToAll     bool            `json:"open_to_all"`
	Active        bool            `json:"active"`
	CreatedAt     time.Time       `json:"created_at"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type CompletionRequest struct {
	ModelID     int64                     `json:"model_id,omitempty"`
	Messages    []Message                 `json:"messages"`
	Tools       []platform.ToolDefinition `json:"-"`
	Temperature float64                   `json:"temperature,omitempty"`
	MaxTokens   int                       `json:"max_tokens,omitempty"`
	Operation   string                    `json:"operation,omitempty"`
}

type CompletionResponse struct {
	Content          string     `json:"content"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ModelID          int64      `json:"model_id"`
	ModelName        string     `json:"model_name"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
}

type StreamEvent struct {
	Type  string `json:"type"` // delta | done | error
	Delta string `json:"delta,omitempty"`
	Error string `json:"error,omitempty"`
}

type UsageLog struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	ModelID          int64     `json:"model_id"`
	ModelName        string    `json:"model_name"`
	Operation        string    `json:"operation"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	LatencyMs        int64     `json:"latency_ms"`
	Status           string    `json:"status"`
	ErrorText        string    `json:"error_text,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type SearchChunk struct {
	ID          int64             `json:"id"`
	SourceType  string            `json:"source_type"`
	SourceID    string            `json:"source_id"`
	ChunkIndex  int               `json:"chunk_index"`
	Title       string            `json:"title"`
	Anchor      string            `json:"anchor,omitempty"`
	Content     string            `json:"content"`
	Path        string            `json:"path"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ContentHash string            `json:"content_hash"`
	Embedding   []float64         `json:"-"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type SearchResult struct {
	SourceType string            `json:"source_type"`
	SourceID   string            `json:"source_id"`
	Title      string            `json:"title"`
	Anchor     string            `json:"anchor,omitempty"`
	Snippet    string            `json:"snippet"`
	Content    string            `json:"content"`
	Path       string            `json:"path"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Score      float64           `json:"score"`
}

type EntityRef struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
}

type ComposeRequest struct {
	Title       string      `json:"title"`
	Goal        string      `json:"goal"`
	Space       string      `json:"space,omitempty"`
	DocType     string      `json:"doc_type,omitempty"`
	ModelID     int64       `json:"model_id,omitempty"`
	SourceDocID []int64     `json:"source_doc_ids,omitempty"`
	EntityRefs  []EntityRef `json:"entity_refs,omitempty"`
	ExtraNotes  string      `json:"extra_notes,omitempty"`
}
