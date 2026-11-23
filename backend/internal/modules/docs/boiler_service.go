package docs

import (
	"context"
	"fmt"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// BoilerDiagnosis — структурированный результат поиска по неисправности котла в чертежах и документации.
type BoilerDiagnosis struct {
	Brand        string       `json:"brand,omitempty"`
	Model        string       `json:"model,omitempty"`
	Symptom      string       `json:"symptom"`
	MatchedDocs  []Document   `json:"matched_docs,omitempty"`
	RelatedParts []BoilerPart `json:"related_parts,omitempty"`
	Schematics   []Attachment `json:"schematics,omitempty"`
}

func (s *Service) indexBoilerBlueprintFromPages(
	ctx context.Context,
	auth *platform.AuthContext,
	doc *Document,
	filename string,
	pages []string,
	schematicsByPage map[int][]Attachment,
) {
	meta := ParseBoilerBlueprint(filename, pages, schematicsByPage)
	if len(meta.Parts) > 0 {
		if err := s.repo.ReplaceBoilerParts(ctx, doc.ID, meta.Parts); err != nil {
			s.logger.Warn("failed to save parsed boiler parts", "doc_id", doc.ID, "count", len(meta.Parts), "err", err)
		} else {
			s.publishChange(ctx, "docs.boiler_parts.indexed", doc.ID)
		}
	}

	// Автоматически добавляем теги бренда и признак чертежа/спецификации, если их ещё нет.
	var addTags []string
	if meta.Brand != "" {
		addTags = append(addTags, meta.Brand)
	}
	if len(meta.Parts) > 0 || meta.IsSpec {
		addTags = append(addTags, "чертеж", "запчасти")
	}
	if len(addTags) == 0 {
		return
	}
	merged := normalizeTags(append(append([]string{}, doc.Tags...), addTags...))
	if len(merged) == len(doc.Tags) {
		return
	}
	if updated, err := s.UpdateDocument(ctx, auth, doc.ID, SaveDocumentInput{
		Space:           doc.Space,
		DocType:         doc.DocType,
		Title:           doc.Title,
		Slug:            doc.Slug,
		Content:         doc.Content,
		Status:          doc.Status,
		Tags:            merged,
		Visibility:      doc.Visibility,
		ExpectedVersion: doc.Version,
		DiffSummary:     "Автоматическая разметка тегов чертежа котла",
	}); err == nil {
		doc.Tags = updated.Tags
		doc.Version = updated.Version
	}
}

// ListDocumentBoilerParts возвращает все запчасти, извлечённые из чертежей указанного документа.
func (s *Service) ListDocumentBoilerParts(ctx context.Context, auth *platform.AuthContext, docID int64) ([]BoilerPart, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	if !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	parts, err := s.repo.ListBoilerPartsByDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if parts == nil {
		return []BoilerPart{}, nil
	}
	return parts, nil
}

// SearchBoilerParts ищет запчасти по всем доступным пользователю чертежам котлов.
func (s *Service) SearchBoilerParts(ctx context.Context, auth *platform.AuthContext, f BoilerPartFilter) ([]BoilerPart, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 30
	}
	raw, err := s.repo.SearchBoilerParts(ctx, f)
	if err != nil {
		return nil, err
	}
	// Фильтруем результаты по правам доступа к родительскому документу (с кэшированием в рамках вызова).
	allowed := map[int64]bool{}
	out := make([]BoilerPart, 0, len(raw))
	for _, p := range raw {
		ok, seen := allowed[p.DocumentID]
		if !seen {
			doc, docErr := s.repo.GetDocument(ctx, p.DocumentID)
			ok = docErr == nil && doc != nil && CanViewDocument(auth, doc)
			allowed[p.DocumentID] = ok
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// DiagnoseBoiler ищет в документации и чертежах котла сведения по неисправности/коду ошибки
// и подбирает связанные узлы, артикулы запчастей и вложения-схемы именно для указанной модели.
func (s *Service) DiagnoseBoiler(ctx context.Context, auth *platform.AuthContext, brand, model, symptom string) (*BoilerDiagnosis, error) {
	symptom = strings.TrimSpace(symptom)
	model = strings.TrimSpace(normalizeBlueprintText(model))
	brand = strings.TrimSpace(brand)
	if symptom == "" && model == "" {
		return nil, platform.NewValidation("Укажите модель котла или описание неисправности/код ошибки", nil)
	}

	diag := &BoilerDiagnosis{
		Brand:   brand,
		Model:   model,
		Symptom: symptom,
	}

	// 1. Ищем документы по модели котла (или по симптому, если модель не задана).
	docQuery := model
	if docQuery == "" {
		docQuery = symptom
	}
	docs, err := s.ListDocuments(ctx, auth, DocumentFilter{
		Query: docQuery,
		Limit: 8,
	})
	if err != nil {
		return nil, err
	}
	diag.MatchedDocs = docs

	// 2. Ищем запчасти напрямую по тексту симптома + по расширенным ключевым словам узлов.
	seenParts := map[int64]bool{}
	addParts := func(items []BoilerPart) {
		for _, p := range items {
			if !seenParts[p.ID] {
				seenParts[p.ID] = true
				diag.RelatedParts = append(diag.RelatedParts, p)
			}
		}
	}

	if symptom != "" {
		direct, err := s.SearchBoilerParts(ctx, auth, BoilerPartFilter{
			Brand: brand,
			Model: model,
			Query: symptom,
			Limit: 15,
		})
		if err == nil {
			addParts(direct)
		}
	}

	for _, kw := range ExpandFaultKeywords(symptom) {
		if len(diag.RelatedParts) >= 25 {
			break
		}
		items, err := s.SearchBoilerParts(ctx, auth, BoilerPartFilter{
			Brand: brand,
			Model: model,
			Query: kw,
			Limit: 6,
		})
		if err == nil {
			addParts(items)
		}
	}

	// 3. Собираем вложения-схемы, относящиеся к найденным документам и запчастям.
	seenAtts := map[int64]bool{}
	for _, p := range diag.RelatedParts {
		if p.DiagramAttachmentID > 0 && !seenAtts[p.DiagramAttachmentID] {
			if att, attErr := s.repo.GetAttachment(ctx, p.DiagramAttachmentID); attErr == nil && att != nil {
				seenAtts[att.ID] = true
				diag.Schematics = append(diag.Schematics, *att)
			}
		}
	}
	for _, d := range docs {
		if len(diag.Schematics) >= 12 {
			break
		}
		atts, attErr := s.repo.ListAttachments(ctx, d.ID)
		if attErr != nil {
			continue
		}
		for _, a := range atts {
			if a.Kind == AttachmentKindSchematic && !seenAtts[a.ID] {
				seenAtts[a.ID] = true
				diag.Schematics = append(diag.Schematics, a)
			}
		}
	}

	return diag, nil
}

// FormatBoilerPartLine форматирует запись о запчасти для ответа ИИ-инструмента и поискового индекса.
func FormatBoilerPartLine(p BoilerPart) string {
	var b strings.Builder
	if p.Brand != "" {
		b.WriteString(p.Brand)
		b.WriteByte(' ')
	}
	if p.BoilerModel != "" {
		b.WriteString(p.BoilerModel)
	}
	if p.Assembly != "" {
		fmt.Fprintf(&b, " | Узел: %s", p.Assembly)
	}
	if p.Position != "" {
		fmt.Fprintf(&b, " | Поз. %s", p.Position)
	}
	if p.PartNumber != "" {
		fmt.Fprintf(&b, " | Арт. %s", p.PartNumber)
	} else {
		b.WriteString(" | Арт. —")
	}
	if p.Name != "" {
		fmt.Fprintf(&b, " — %s", p.Name)
	}
	if p.Notes != "" {
		fmt.Fprintf(&b, " (%s)", p.Notes)
	}
	if p.ReplacedBy != "" {
		fmt.Fprintf(&b, " [ЗАМЕНА НА: %s]", p.ReplacedBy)
	}
	if p.PageNumber > 0 {
		fmt.Fprintf(&b, " [док #%d «%s», стр. %d", p.DocumentID, p.DocumentTitle, p.PageNumber)
		if p.DiagramPage > 0 {
			fmt.Fprintf(&b, ", чертёж стр. %d", p.DiagramPage)
		}
		if p.DiagramAttachmentID > 0 {
			fmt.Fprintf(&b, ", attachment_id=%d", p.DiagramAttachmentID)
		}
		b.WriteByte(']')
	}
	return b.String()
}
