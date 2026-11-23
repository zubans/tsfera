package docs

import (
	"context"
	"time"
)

type DocumentFilter struct {
	Space           string
	DocType         DocType
	Status          string
	Tag             string
	Query           string
	IncludeArchived bool
	Limit           int
	Offset          int
}

type Repository interface {
	// SaveDocumentRevision атомарно сохраняет документ и ревизию; expectedVersion=0 создаёт документ.
	SaveDocumentRevision(ctx context.Context, doc *Document, expectedVersion int, rev *Revision) error
	ListDocuments(ctx context.Context, f DocumentFilter) ([]Document, error)
	ListSpaces(ctx context.Context) ([]SpaceSummary, error)
	GetDocument(ctx context.Context, id int64) (*Document, error)
	GetDocumentBySlug(ctx context.Context, space, slug string) (*Document, error)
	CreateDocument(ctx context.Context, doc *Document) error
	// UpdateDocumentWithVersion обновляет документ только при совпадении expectedVersion; иначе ErrConflict.
	UpdateDocumentWithVersion(ctx context.Context, doc *Document, expectedVersion int) error
	SoftDeleteDocument(ctx context.Context, id int64) error

	AddRevision(ctx context.Context, rev *Revision) error
	ListRevisions(ctx context.Context, docID int64) ([]Revision, error)
	GetRevision(ctx context.Context, docID, revisionID int64) (*Revision, error)
	CompressRevisionsBefore(ctx context.Context, cutoff time.Time) (int, error)

	ListEntityLinksByDocument(ctx context.Context, docID int64) ([]EntityLink, error)
	ListDocumentsByEntity(ctx context.Context, entityType string, entityID int64) ([]Document, error)
	AddEntityLink(ctx context.Context, link *EntityLink) error
	DeleteEntityLink(ctx context.Context, linkID int64) error

	ListComments(ctx context.Context, docID int64) ([]DocComment, error)
	AddComment(ctx context.Context, c *DocComment) error
	SetCommentResolved(ctx context.Context, docID, commentID, userID int64, resolved bool) (*DocComment, error)
	DeleteComment(ctx context.Context, docID, commentID int64) error

	ListAttachments(ctx context.Context, docID int64) ([]Attachment, error)
	ListAllAttachments(ctx context.Context, limit int) ([]Attachment, error)
	ListPendingSchematicAttachments(ctx context.Context, limit int) ([]Attachment, error)
	GetAttachment(ctx context.Context, id int64) (*Attachment, error)
	AddAttachment(ctx context.Context, att *Attachment) error
	UpdateAttachmentAnalysis(ctx context.Context, id int64, status, extractedText, errText string, analyzedAt *time.Time) error
	DeleteAttachment(ctx context.Context, id int64) error

	ReplaceBoilerParts(ctx context.Context, documentID int64, parts []BoilerPart) error
	ListBoilerPartsByDocument(ctx context.Context, documentID int64) ([]BoilerPart, error)
	SearchBoilerParts(ctx context.Context, f BoilerPartFilter) ([]BoilerPart, error)
	ListAllBoilerParts(ctx context.Context, limit int) ([]BoilerPart, error)
}
