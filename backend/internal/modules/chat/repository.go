package chat

import "context"

type Repository interface {
	ListConversations(ctx context.Context, userID int64, limit int) ([]Conversation, error)
	GetConversation(ctx context.Context, id int64) (*Conversation, error)
	CreateConversation(ctx context.Context, conv *Conversation) error
	TouchConversation(ctx context.Context, id int64) error
	SoftDeleteConversation(ctx context.Context, id int64) error

	ListMessages(ctx context.Context, convID int64) ([]ChatMessage, error)
	AddMessage(ctx context.Context, msg *ChatMessage) error

	CreateShare(ctx context.Context, s *ConversationShare) error
	GetShareByToken(ctx context.Context, token string) (*ConversationShare, error)
	RevokeSharesByConversation(ctx context.Context, convID, ownerID int64) error
	RevokeSharesByOwner(ctx context.Context, ownerID int64) error
}
