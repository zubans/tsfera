package docs

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type PgRepository struct {
	db platform.Querier
}

func NewPgRepository(db platform.Querier) *PgRepository {
	return &PgRepository{db: db}
}

func scanDocument(row pgx.Row) (*Document, error) {
	var d Document
	var tagsRaw []byte
	err := row.Scan(
		&d.ID, &d.Space, &d.DocType, &d.Title, &d.Slug, &d.Content, &d.Status,
		&tagsRaw, &d.Version, &d.Visibility, &d.CreatedBy, &d.UpdatedBy,
		&d.Deleted, &d.ArchivedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(tagsRaw, &d.Tags)
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return &d, nil
}

func (r *PgRepository) ListDocuments(ctx context.Context, f DocumentFilter) ([]Document, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := max(0, f.Offset)
	where := []string{"deleted = FALSE"}
	args := []any{}
	idx := 1

	if !f.IncludeArchived {
		where = append(where, "archived_at IS NULL AND status <> 'archived'")
	}
	if s := strings.TrimSpace(f.Space); s != "" {
		where = append(where, fmt.Sprintf("space = $%d", idx))
		args = append(args, s)
		idx++
	}
	if f.DocType != "" {
		where = append(where, fmt.Sprintf("doc_type = $%d", idx))
		args = append(args, string(f.DocType))
		idx++
	}
	if f.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, f.Status)
		idx++
	}
	if f.Tag != "" {
		tagJSON, _ := json.Marshal([]string{f.Tag})
		where = append(where, fmt.Sprintf("tags @> $%d::jsonb", idx))
		args = append(args, string(tagJSON))
		idx++
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, fmt.Sprintf("(title ILIKE '%%' || $%d || '%%' OR content ILIKE '%%' || $%d || '%%')", idx, idx))
		args = append(args, q)
		idx++
	}

	args = append(args, limit, offset)
	sql := fmt.Sprintf(`
		SELECT id, space, doc_type, title, slug, content, status, tags, version, visibility,
		       created_by, updated_by, deleted, archived_at, created_at, updated_at
		FROM docs_documents
		WHERE %s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), idx, idx+1)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListSpaces(ctx context.Context) ([]SpaceSummary, error) {
	rows, err := r.db.Query(ctx, `
		SELECT space, COUNT(*)
		FROM docs_documents
		WHERE deleted = FALSE AND archived_at IS NULL
		GROUP BY space
		ORDER BY space ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpaceSummary
	for rows.Next() {
		var s SpaceSummary
		if err := rows.Scan(&s.Space, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetDocument(ctx context.Context, id int64) (*Document, error) {
	return scanDocument(r.db.QueryRow(ctx, `
		SELECT id, space, doc_type, title, slug, content, status, tags, version, visibility,
		       created_by, updated_by, deleted, archived_at, created_at, updated_at
		FROM docs_documents
		WHERE id = $1 AND deleted = FALSE
	`, id))
}

func (r *PgRepository) GetDocumentBySlug(ctx context.Context, space, slug string) (*Document, error) {
	return scanDocument(r.db.QueryRow(ctx, `
		SELECT id, space, doc_type, title, slug, content, status, tags, version, visibility,
		       created_by, updated_by, deleted, archived_at, created_at, updated_at
		FROM docs_documents
		WHERE space = $1 AND slug = $2 AND deleted = FALSE
	`, space, slug))
}

func (r *PgRepository) CreateDocument(ctx context.Context, doc *Document) error {
	tags, _ := json.Marshal(doc.Tags)
	return r.db.QueryRow(ctx, `
		INSERT INTO docs_documents (
			space, doc_type, title, slug, content, status, tags, version, visibility, created_by, updated_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at
	`, doc.Space, string(doc.DocType), doc.Title, doc.Slug, doc.Content, doc.Status, tags,
		doc.Version, string(doc.Visibility), doc.CreatedBy, doc.UpdatedBy,
	).Scan(&doc.ID, &doc.CreatedAt, &doc.UpdatedAt)
}

func (r *PgRepository) UpdateDocumentWithVersion(ctx context.Context, doc *Document, expectedVersion int) error {
	tags, _ := json.Marshal(doc.Tags)
	err := r.db.QueryRow(ctx, `
		UPDATE docs_documents
		SET space=$2, doc_type=$3, title=$4, slug=$5, content=$6, status=$7, tags=$8,
		    version = version + 1, visibility=$9, updated_by=$10, archived_at=$11, updated_at=now()
		WHERE id = $1 AND deleted = FALSE AND version = $12
		RETURNING version, updated_at
	`, doc.ID, doc.Space, string(doc.DocType), doc.Title, doc.Slug, doc.Content, doc.Status,
		tags, string(doc.Visibility), doc.UpdatedBy, doc.ArchivedAt, expectedVersion,
	).Scan(&doc.Version, &doc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.NewConflict(fmt.Sprintf("Конфликт версий документа (ожидалась версия %d)", expectedVersion))
	}
	return err
}

func (r *PgRepository) SoftDeleteDocument(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE docs_documents SET deleted = TRUE, archived_at = now(), updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Документ не найден", id)
}

func (r *PgRepository) AddRevision(ctx context.Context, rev *Revision) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO docs_revisions (document_id, version, title, content_raw, diff_summary, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, rev.DocumentID, rev.Version, rev.Title, rev.Content, rev.DiffSummary, rev.CreatedBy).
		Scan(&rev.ID, &rev.CreatedAt)
}

func (r *PgRepository) ListRevisions(ctx context.Context, docID int64) ([]Revision, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, document_id, version, title, diff_summary, (content_gz IS NOT NULL), created_by, created_at
		FROM docs_revisions
		WHERE document_id = $1
		ORDER BY version DESC, id DESC
	`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var rev Revision
		if err := rows.Scan(&rev.ID, &rev.DocumentID, &rev.Version, &rev.Title, &rev.DiffSummary, &rev.Compressed, &rev.CreatedBy, &rev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetRevision(ctx context.Context, docID, revisionID int64) (*Revision, error) {
	var rev Revision
	var contentRaw string
	var contentGz []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, document_id, version, title, content_raw, content_gz, diff_summary, created_by, created_at
		FROM docs_revisions
		WHERE document_id = $1 AND id = $2
	`, docID, revisionID).Scan(
		&rev.ID, &rev.DocumentID, &rev.Version, &rev.Title, &contentRaw, &contentGz, &rev.DiffSummary, &rev.CreatedBy, &rev.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(contentGz) > 0 {
		rev.Compressed = true
		plain, err := gunzipBytes(contentGz)
		if err != nil {
			return nil, err
		}
		rev.Content = plain
	} else {
		rev.Content = contentRaw
	}
	return &rev, nil
}

// CompressRevisionsBefore сжимает ревизии старше cutoff в gzip и очищает content_raw для экономии места в БД.
func (r *PgRepository) CompressRevisionsBefore(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, content_raw
		FROM docs_revisions
		WHERE content_gz IS NULL AND content_raw <> '' AND created_at < $1
		LIMIT 200
	`, cutoff)
	if err != nil {
		return 0, err
	}
	type rawItem struct {
		id  int64
		txt string
	}
	var pending []rawItem
	for rows.Next() {
		var it rawItem
		if err := rows.Scan(&it.id, &it.txt); err == nil {
			pending = append(pending, it)
		}
	}
	rows.Close()

	compressed := 0
	for _, it := range pending {
		gz, err := gzipString(it.txt)
		if err != nil {
			continue
		}
		if _, err := r.db.Exec(ctx, `
			UPDATE docs_revisions SET content_gz = $2, content_raw = '' WHERE id = $1
		`, it.id, gz); err == nil {
			compressed++
		}
	}
	return compressed, nil
}

func gzipString(s string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := io.WriteString(gw, s); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(data []byte) (string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer gr.Close()
	out, err := io.ReadAll(io.LimitReader(gr, 16<<20))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r *PgRepository) ListEntityLinksByDocument(ctx context.Context, docID int64) ([]EntityLink, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, document_id, entity_type, entity_id, note, created_by, created_at
		FROM docs_entity_links
		WHERE document_id = $1
		ORDER BY id ASC
	`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityLink
	for rows.Next() {
		var l EntityLink
		if err := rows.Scan(&l.ID, &l.DocumentID, &l.EntityType, &l.EntityID, &l.Note, &l.CreatedBy, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListDocumentsByEntity(ctx context.Context, entityType string, entityID int64) ([]Document, error) {
	rows, err := r.db.Query(ctx, `
		SELECT d.id, d.space, d.doc_type, d.title, d.slug, d.content, d.status, d.tags, d.version, d.visibility,
		       d.created_by, d.updated_by, d.deleted, d.archived_at, d.created_at, d.updated_at
		FROM docs_documents d
		JOIN docs_entity_links l ON l.document_id = d.id
		WHERE d.deleted = FALSE AND l.entity_type = $1 AND l.entity_id = $2
		ORDER BY d.updated_at DESC
	`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *PgRepository) AddEntityLink(ctx context.Context, link *EntityLink) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO docs_entity_links (document_id, entity_type, entity_id, note, created_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (document_id, entity_type, entity_id) DO UPDATE SET note = EXCLUDED.note
		RETURNING id, created_at
	`, link.DocumentID, link.EntityType, link.EntityID, link.Note, link.CreatedBy).Scan(&link.ID, &link.CreatedAt)
}

func (r *PgRepository) DeleteEntityLink(ctx context.Context, linkID int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM docs_entity_links WHERE id = $1`, "Связь не найдена", linkID)
}

func (r *PgRepository) ListComments(ctx context.Context, docID int64) ([]DocComment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, document_id, author_id, author_name, body, quote, anchor, line_from, line_to,
		       resolved, resolved_by, resolved_at, created_at, updated_at
		FROM docs_comments
		WHERE document_id = $1
		ORDER BY created_at ASC, id ASC
	`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocComment
	for rows.Next() {
		var c DocComment
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Quote, &c.Anchor,
			&c.LineFrom, &c.LineTo, &c.Resolved, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PgRepository) AddComment(ctx context.Context, c *DocComment) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO docs_comments (document_id, author_id, author_name, body, quote, anchor, line_from, line_to)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, resolved, created_at, updated_at
	`, c.DocumentID, c.AuthorID, c.AuthorName, c.Body, c.Quote, c.Anchor, c.LineFrom, c.LineTo).
		Scan(&c.ID, &c.Resolved, &c.CreatedAt, &c.UpdatedAt)
}

func (r *PgRepository) SetCommentResolved(ctx context.Context, docID, commentID, userID int64, resolved bool) (*DocComment, error) {
	var c DocComment
	var err error
	if resolved {
		err = r.db.QueryRow(ctx, `
			UPDATE docs_comments SET resolved=TRUE, resolved_by=$3, resolved_at=now(), updated_at=now()
			WHERE document_id=$1 AND id=$2
			RETURNING id, document_id, author_id, author_name, body, quote, anchor, line_from, line_to, resolved, resolved_by, resolved_at, created_at, updated_at
		`, docID, commentID, userID).Scan(&c.ID, &c.DocumentID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Quote, &c.Anchor,
			&c.LineFrom, &c.LineTo, &c.Resolved, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt)
	} else {
		err = r.db.QueryRow(ctx, `
			UPDATE docs_comments SET resolved=FALSE, resolved_by=NULL, resolved_at=NULL, updated_at=now()
			WHERE document_id=$1 AND id=$2
			RETURNING id, document_id, author_id, author_name, body, quote, anchor, line_from, line_to, resolved, resolved_by, resolved_at, created_at, updated_at
		`, docID, commentID).Scan(&c.ID, &c.DocumentID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Quote, &c.Anchor,
			&c.LineFrom, &c.LineTo, &c.Resolved, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Комментарий не найден")
	}
	return &c, err
}

func (r *PgRepository) DeleteComment(ctx context.Context, docID, commentID int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM docs_comments WHERE document_id=$1 AND id=$2`, "Комментарий не найден", docID, commentID)
}

func (r *PgRepository) ListAttachments(ctx context.Context, docID int64) ([]Attachment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, document_id, filename, mime_type, size_bytes, storage_path, extracted_text, created_by, created_at
		FROM docs_attachments
		WHERE document_id = $1
		ORDER BY created_at DESC, id DESC
	`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.DocumentID, &a.Filename, &a.MimeType, &a.SizeBytes, &a.StoragePath, &a.ExtractedText, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListAllAttachments(ctx context.Context, limit int) ([]Attachment, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.document_id, a.filename, a.mime_type, a.size_bytes, a.storage_path, a.extracted_text, a.created_by, a.created_at
		FROM docs_attachments a
		JOIN docs_documents d ON d.id = a.document_id
		WHERE d.deleted = FALSE AND a.extracted_text <> ''
		ORDER BY a.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.DocumentID, &a.Filename, &a.MimeType, &a.SizeBytes, &a.StoragePath, &a.ExtractedText, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetAttachment(ctx context.Context, id int64) (*Attachment, error) {
	var a Attachment
	err := r.db.QueryRow(ctx, `
		SELECT id, document_id, filename, mime_type, size_bytes, storage_path, extracted_text, created_by, created_at
		FROM docs_attachments
		WHERE id = $1
	`, id).Scan(&a.ID, &a.DocumentID, &a.Filename, &a.MimeType, &a.SizeBytes, &a.StoragePath, &a.ExtractedText, &a.CreatedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

func (r *PgRepository) AddAttachment(ctx context.Context, att *Attachment) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO docs_attachments (document_id, filename, mime_type, size_bytes, storage_path, extracted_text, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at
	`, att.DocumentID, att.Filename, att.MimeType, att.SizeBytes, att.StoragePath, att.ExtractedText, att.CreatedBy).
		Scan(&att.ID, &att.CreatedAt)
}

func (r *PgRepository) DeleteAttachment(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM docs_attachments WHERE id = $1`, "Вложение не найдено", id)
}

// SaveDocumentRevision публикует новую версию только после сохранения её истории.
func (r *PgRepository) SaveDocumentRevision(ctx context.Context, doc *Document, expectedVersion int, rev *Revision) error {
	db, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("repository does not support transactions")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	transactional := NewPgRepository(tx)
	saved := *doc
	if expectedVersion == 0 {
		err = transactional.CreateDocument(ctx, &saved)
	} else {
		err = transactional.UpdateDocumentWithVersion(ctx, &saved, expectedVersion)
	}
	if err != nil {
		return err
	}
	revision := *rev
	revision.DocumentID, revision.Version, revision.Title, revision.Content = saved.ID, saved.Version, saved.Title, saved.Content
	if err := transactional.AddRevision(ctx, &revision); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	*doc, *rev = saved, revision
	return nil
}
