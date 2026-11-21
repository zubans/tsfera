package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type PgRepository struct {
	db platform.Querier
}

func NewPgRepository(db platform.Querier) *PgRepository {
	return &PgRepository{db: db}
}

func (r *PgRepository) ListProviders(ctx context.Context) ([]Provider, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, kind, base_url, api_key, active, is_default, timeout_seconds, created_at, updated_at
		FROM ai_providers
		ORDER BY is_default DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Active, &p.IsDefault, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.HasAPIKey = p.APIKey != ""
		p.APIKey = ""
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetProvider(ctx context.Context, id int64) (*Provider, error) {
	var p Provider
	err := r.db.QueryRow(ctx, `
		SELECT id, name, kind, base_url, api_key, active, is_default, timeout_seconds, created_at, updated_at
		FROM ai_providers
		WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Active, &p.IsDefault, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.HasAPIKey = p.APIKey != ""
	return &p, nil
}

func (r *PgRepository) SaveProvider(ctx context.Context, p *Provider) error {
	if p.IsDefault {
		_, _ = r.db.Exec(ctx, `UPDATE ai_providers SET is_default = FALSE WHERE is_default = TRUE`)
	}
	if p.ID == 0 {
		return r.db.QueryRow(ctx, `
			INSERT INTO ai_providers (name, kind, base_url, api_key, active, is_default, timeout_seconds)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			RETURNING id, created_at, updated_at
		`, p.Name, string(p.Kind), p.BaseURL, p.APIKey, p.Active, p.IsDefault, p.TimeoutSeconds).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE ai_providers
		SET name=$2, kind=$3, base_url=$4,
		    api_key=CASE WHEN $5='' THEN api_key ELSE $5 END,
		    active=$6, is_default=$7, timeout_seconds=$8, updated_at=now()
		WHERE id = $1
	`, "Провайдер не найден", p.ID, p.Name, string(p.Kind), p.BaseURL, p.APIKey, p.Active, p.IsDefault, p.TimeoutSeconds)
}

func (r *PgRepository) DeleteProvider(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM ai_providers WHERE id = $1`, "Провайдер не найден", id)
}

func scanModel(row pgx.Row) (*Model, error) {
	var m Model
	err := row.Scan(
		&m.ID, &m.ProviderID, &m.ProviderName, &m.ProviderKind,
		&m.Name, &m.ModelID, &m.Capability, &m.ContextWindow,
		&m.IsDefault, &m.OpenToAll, &m.Active, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

const selectModelSQL = `
	SELECT m.id, m.provider_id, p.name, p.kind, m.name, m.model_id, m.capability,
	       m.context_window, m.is_default, m.open_to_all, m.active, m.created_at
	FROM ai_models m
	JOIN ai_providers p ON p.id = m.provider_id
`

func (r *PgRepository) ListModels(ctx context.Context, capability *ModelCapability) ([]Model, error) {
	where := "1=1"
	var args []any
	if capability != nil {
		where = "m.capability = $1"
		args = append(args, string(*capability))
	}
	rows, err := r.db.Query(ctx, selectModelSQL+" WHERE "+where+" ORDER BY m.is_default DESC, m.id ASC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetModel(ctx context.Context, id int64) (*Model, error) {
	return scanModel(r.db.QueryRow(ctx, selectModelSQL+" WHERE m.id = $1", id))
}

func (r *PgRepository) GetDefaultModel(ctx context.Context, capability ModelCapability) (*Model, error) {
	return scanModel(r.db.QueryRow(ctx, selectModelSQL+`
		WHERE m.capability = $1 AND m.active = TRUE AND p.active = TRUE
		ORDER BY m.is_default DESC, p.is_default DESC, m.id ASC
		LIMIT 1
	`, string(capability)))
}

func (r *PgRepository) SaveModel(ctx context.Context, m *Model) error {
	if m.IsDefault {
		_, _ = r.db.Exec(ctx, `UPDATE ai_models SET is_default = FALSE WHERE capability = $1`, string(m.Capability))
	}
	if m.ID == 0 {
		return r.db.QueryRow(ctx, `
			INSERT INTO ai_models (provider_id, name, model_id, capability, context_window, is_default, open_to_all, active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id, created_at
		`, m.ProviderID, m.Name, m.ModelID, string(m.Capability), m.ContextWindow, m.IsDefault, m.OpenToAll, m.Active).
			Scan(&m.ID, &m.CreatedAt)
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE ai_models
		SET provider_id=$2, name=$3, model_id=$4, capability=$5, context_window=$6, is_default=$7, open_to_all=$8, active=$9
		WHERE id = $1
	`, "Модель не найдена", m.ID, m.ProviderID, m.Name, m.ModelID, string(m.Capability), m.ContextWindow, m.IsDefault, m.OpenToAll, m.Active)
}

func (r *PgRepository) DeleteModel(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM ai_models WHERE id = $1`, "Модель не найдена", id)
}

func (r *PgRepository) ListChunkHashes(ctx context.Context, sourceType, sourceID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT content_hash
		FROM ai_chunks
		WHERE source_type = $1 AND source_id = $2
		ORDER BY chunk_index ASC
	`, sourceType, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *PgRepository) ReplaceSourceChunks(ctx context.Context, sourceType, sourceID string, chunks []SearchChunk) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM ai_chunks WHERE source_type=$1 AND source_id=$2`, sourceType, sourceID); err != nil {
		return err
	}
	for _, ch := range chunks {
		metaRaw, _ := json.Marshal(ch.Metadata)
		embRaw, _ := json.Marshal(ch.Embedding)
		if len(embRaw) == 0 {
			embRaw = []byte("[]")
		}
		if _, err := r.db.Exec(ctx, `
			INSERT INTO ai_chunks (source_type, source_id, chunk_index, title, anchor, content, path, metadata, content_hash, embedding, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		`, ch.SourceType, ch.SourceID, ch.ChunkIndex, ch.Title, ch.Anchor, ch.Content, ch.Path, metaRaw, ch.ContentHash, embRaw); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) DeleteSourceChunks(ctx context.Context, sourceType, sourceID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM ai_chunks WHERE source_type=$1 AND source_id=$2`, sourceType, sourceID)
	return err
}

func (r *PgRepository) SearchFTS(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := r.db.Query(ctx, `
		SELECT source_type, source_id, title, anchor, content, path, metadata,
		       ts_rank_cd(to_tsvector('russian', title || ' ' || content), plainto_tsquery('russian', $1))::float8 AS rank
		FROM ai_chunks
		WHERE to_tsvector('russian', title || ' ' || content) @@ plainto_tsquery('russian', $1)
		   OR title ILIKE '%' || $1 || '%'
		   OR content ILIKE '%' || $1 || '%'
		ORDER BY rank DESC, updated_at DESC
		LIMIT $2
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var sr SearchResult
		var metaRaw []byte
		if err := rows.Scan(&sr.SourceType, &sr.SourceID, &sr.Title, &sr.Anchor, &sr.Content, &sr.Path, &metaRaw, &sr.Score); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaRaw, &sr.Metadata)
		sr.Snippet = MakeSnippet(sr.Content, 240)
		out = append(out, sr)
	}
	return out, rows.Err()
}

func (r *PgRepository) SearchVectors(ctx context.Context, queryVec []float64, limit int) ([]SearchResult, error) {
	if len(queryVec) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := r.db.Query(ctx, `
		SELECT source_type, source_id, title, anchor, content, path, metadata, embedding
		FROM ai_chunks
		WHERE jsonb_array_length(embedding) > 0
		LIMIT 2000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []SearchResult
	for rows.Next() {
		var sr SearchResult
		var metaRaw, embRaw []byte
		if err := rows.Scan(&sr.SourceType, &sr.SourceID, &sr.Title, &sr.Anchor, &sr.Content, &sr.Path, &metaRaw, &embRaw); err != nil {
			return nil, err
		}
		var vec []float64
		if err := json.Unmarshal(embRaw, &vec); err != nil {
			continue
		}
		sim := CosineSimilarity(queryVec, vec)
		if sim <= 0.15 {
			continue
		}
		_ = json.Unmarshal(metaRaw, &sr.Metadata)
		sr.Score = sim
		sr.Snippet = MakeSnippet(sr.Content, 240)
		candidates = append(candidates, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (r *PgRepository) LogUsage(ctx context.Context, entry *UsageLog) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO ai_usage_logs (user_id, model_id, model_name, operation, prompt_tokens, completion_tokens, latency_ms, status, error_text)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at
	`, entry.UserID, entry.ModelID, entry.ModelName, entry.Operation,
		entry.PromptTokens, entry.CompletionTokens, entry.LatencyMs, entry.Status, entry.ErrorText,
	).Scan(&entry.ID, &entry.CreatedAt)
}

func (r *PgRepository) ListUsageLogs(ctx context.Context, limit int) ([]UsageLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, model_id, model_name, operation, prompt_tokens, completion_tokens, latency_ms, status, error_text, created_at
		FROM ai_usage_logs
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageLog
	for rows.Next() {
		var u UsageLog
		if err := rows.Scan(&u.ID, &u.UserID, &u.ModelID, &u.ModelName, &u.Operation,
			&u.PromptTokens, &u.CompletionTokens, &u.LatencyMs, &u.Status, &u.ErrorText, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

var _ = fmt.Sprint

func (r *PgRepository) GetTaskModelID(ctx context.Context, task AITask) (*int64, error) {
	var id *int64
	err := r.db.QueryRow(ctx, `SELECT model_id FROM ai_task_assignments WHERE task = $1`, string(task)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return id, err
}

func (r *PgRepository) ListTaskAssignments(ctx context.Context) ([]TaskAssignment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT t.task, t.model_id, COALESCE(m.name, '')
		FROM ai_task_assignments t
		LEFT JOIN ai_models m ON m.id = t.model_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskAssignment
	for rows.Next() {
		var a TaskAssignment
		var task string
		if err := rows.Scan(&task, &a.ModelID, &a.ModelName); err != nil {
			return nil, err
		}
		a.Task = AITask(task)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PgRepository) SaveTaskAssignment(ctx context.Context, task AITask, modelID *int64) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO ai_task_assignments (task, model_id, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (task) DO UPDATE SET model_id = EXCLUDED.model_id, updated_at = now()
	`, string(task), modelID)
	return err
}
