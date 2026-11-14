package chat

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type PgRepository struct {
	db platform.Querier
}

func NewPgRepository(db platform.Querier) *PgRepository {
	return &PgRepository{db: db}
}

func (r *PgRepository) ListConversations(ctx context.Context, userID int64, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, title, mode, model_id, entity_type, entity_id, deleted, created_at, updated_at
		FROM chat_conversations
		WHERE user_id = $1 AND deleted = FALSE
		ORDER BY updated_at DESC, id DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Mode, &c.ModelID, &c.EntityType, &c.EntityID, &c.Deleted, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetConversation(ctx context.Context, id int64) (*Conversation, error) {
	var c Conversation
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, title, mode, model_id, entity_type, entity_id, deleted, created_at, updated_at
		FROM chat_conversations
		WHERE id = $1 AND deleted = FALSE
	`, id).Scan(&c.ID, &c.UserID, &c.Title, &c.Mode, &c.ModelID, &c.EntityType, &c.EntityID, &c.Deleted, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (r *PgRepository) CreateConversation(ctx context.Context, conv *Conversation) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO chat_conversations (user_id, title, mode, model_id, entity_type, entity_id)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at
	`, conv.UserID, conv.Title, string(conv.Mode), conv.ModelID, conv.EntityType, conv.EntityID).
		Scan(&conv.ID, &conv.CreatedAt, &conv.UpdatedAt)
}

func (r *PgRepository) TouchConversation(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx, `UPDATE chat_conversations SET updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *PgRepository) SoftDeleteConversation(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE chat_conversations SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Диалог не найден", id)
}

func (r *PgRepository) ListMessages(ctx context.Context, convID int64) ([]ChatMessage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, conversation_id, role, content, steps, sources, tokens_used, created_at
		FROM chat_messages
		WHERE conversation_id = $1
		ORDER BY id ASC
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var stepsRaw, srcRaw []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &stepsRaw, &srcRaw, &m.TokensUsed, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(stepsRaw, &m.Steps)
		_ = json.Unmarshal(srcRaw, &m.Sources)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PgRepository) AddMessage(ctx context.Context, msg *ChatMessage) error {
	stepsRaw, _ := json.Marshal(msg.Steps)
	if len(stepsRaw) == 0 || string(stepsRaw) == "null" {
		stepsRaw = []byte("[]")
	}
	srcRaw, _ := json.Marshal(msg.Sources)
	if len(srcRaw) == 0 || string(srcRaw) == "null" {
		srcRaw = []byte("[]")
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO chat_messages (conversation_id, role, content, steps, sources, tokens_used)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at
	`, msg.ConversationID, msg.Role, msg.Content, stepsRaw, srcRaw, msg.TokensUsed).
		Scan(&msg.ID, &msg.CreatedAt)
}

func (r *PgRepository) CreateShare(ctx context.Context, s *ConversationShare) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO chat_shares (conversation_id, owner_id, share_token, expires_at)
		VALUES ($1,$2,$3,$4)
		RETURNING id, revoked, created_at
	`, s.ConversationID, s.OwnerID, s.ShareToken, s.ExpiresAt).Scan(&s.ID, &s.Revoked, &s.CreatedAt)
}

func (r *PgRepository) GetShareByToken(ctx context.Context, token string) (*ConversationShare, error) {
	var s ConversationShare
	err := r.db.QueryRow(ctx, `
		SELECT id, conversation_id, owner_id, share_token, revoked, expires_at, created_at
		FROM chat_shares
		WHERE share_token = $1
	`, token).Scan(&s.ID, &s.ConversationID, &s.OwnerID, &s.ShareToken, &s.Revoked, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func (r *PgRepository) RevokeSharesByConversation(ctx context.Context, convID, ownerID int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE chat_shares SET revoked = TRUE
		WHERE conversation_id = $1 AND owner_id = $2 AND revoked = FALSE
	`, convID, ownerID)
	return err
}

func (r *PgRepository) RevokeSharesByOwner(ctx context.Context, ownerID int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE chat_shares SET revoked = TRUE
		WHERE owner_id = $1 AND revoked = FALSE
	`, ownerID)
	return err
}
