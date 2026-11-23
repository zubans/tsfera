package docs

import "time"

type DocType string

const (
	DocTypeArticle         DocType = "article"
	DocTypeSOP             DocType = "sop"
	DocTypeEquipmentManual DocType = "equipment_manual"
	DocTypeChecklist       DocType = "checklist"
	DocTypeADR             DocType = "adr"
	DocTypeNote            DocType = "note"
)

type Visibility string

const (
	VisibilityInternal    Visibility = "internal"
	VisibilityEngineers   Visibility = "engineers"
	VisibilityDispatchers Visibility = "dispatchers"
	VisibilityAdmins      Visibility = "admins"
	VisibilityPublic      Visibility = "public"
)

type Document struct {
	ID          int64         `json:"id"`
	Space       string        `json:"space"`
	DocType     DocType       `json:"doc_type"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Content     string        `json:"content"`
	Status      string        `json:"status"`
	Tags        []string      `json:"tags"`
	Version     int           `json:"version"`
	Visibility  Visibility    `json:"visibility"`
	CreatedBy   int64         `json:"created_by"`
	UpdatedBy   int64         `json:"updated_by"`
	Deleted     bool          `json:"deleted"`
	ArchivedAt  *time.Time    `json:"archived_at,omitempty"`
	Outline     []OutlineItem `json:"outline,omitempty"`
	EntityLinks []EntityLink  `json:"entity_links,omitempty"`
	Attachments []Attachment  `json:"attachments,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type OutlineItem struct {
	Level  int    `json:"level"`
	Title  string `json:"title"`
	Anchor string `json:"anchor"`
	Line   int    `json:"line"`
}

type Revision struct {
	ID          int64     `json:"id"`
	DocumentID  int64     `json:"document_id"`
	Version     int       `json:"version"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	DiffSummary string    `json:"diff_summary"`
	Compressed  bool      `json:"compressed"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type EntityLink struct {
	ID          int64     `json:"id"`
	DocumentID  int64     `json:"document_id"`
	EntityType  string    `json:"entity_type"`
	EntityID    int64     `json:"entity_id"`
	EntityTitle string    `json:"entity_title,omitempty"`
	EntityPath  string    `json:"entity_path,omitempty"`
	Note        string    `json:"note"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type DocComment struct {
	ID         int64      `json:"id"`
	DocumentID int64      `json:"document_id"`
	AuthorID   int64      `json:"author_id"`
	AuthorName string     `json:"author_name"`
	Body       string     `json:"body"`
	Quote      string     `json:"quote"`
	Anchor     string     `json:"anchor"`
	LineFrom   int        `json:"line_from"`
	LineTo     int        `json:"line_to"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *int64     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

const (
	AttachmentKindFile      = "file"
	AttachmentKindSchematic = "schematic"

	AnalysisStatusPending = "pending"
	AnalysisStatusDone    = "done"
	AnalysisStatusFailed  = "failed"
)

type Attachment struct {
	ID             int64      `json:"id"`
	DocumentID     int64      `json:"document_id"`
	Filename       string     `json:"filename"`
	MimeType       string     `json:"mime_type"`
	SizeBytes      int64      `json:"size_bytes"`
	StoragePath    string     `json:"-"`
	ExtractedText  string     `json:"extracted_text,omitempty"`
	Kind           string     `json:"kind"`
	PageNumber     int        `json:"page_number,omitempty"`
	AnalysisStatus string     `json:"analysis_status,omitempty"`
	AnalysisError  string     `json:"analysis_error,omitempty"`
	AnalyzedAt     *time.Time `json:"analyzed_at,omitempty"`
	CreatedBy      int64      `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
}

type SpaceSummary struct {
	Space string `json:"space"`
	Count int    `json:"count"`
}

// BoilerPart — структурированная позиция запчасти, извлечённая из чертежа / взрыв-схемы котла.
type BoilerPart struct {
	ID                  int64     `json:"id"`
	DocumentID          int64     `json:"document_id"`
	DocumentTitle       string    `json:"document_title,omitempty"`
	Brand               string    `json:"brand"`
	BoilerModel         string    `json:"boiler_model"`
	Assembly            string    `json:"assembly"`
	Position            string    `json:"position"`
	PartNumber          string    `json:"part_number"`
	Name                string    `json:"name"`
	Notes               string    `json:"notes,omitempty"`
	ReplacedBy          string    `json:"replaced_by,omitempty"`
	PageNumber          int       `json:"page_number,omitempty"`
	DiagramPage         int       `json:"diagram_page,omitempty"`
	DiagramAttachmentID int64     `json:"diagram_attachment_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// BoilerPartFilter — параметры поиска запчастей по проиндексированным чертежам котлов.
type BoilerPartFilter struct {
	DocumentID int64
	Brand      string
	Model      string
	PartNumber string
	Assembly   string
	Query      string
	Limit      int
}
