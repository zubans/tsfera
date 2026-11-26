package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/internal/modules/chat"
	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Searcher — узкий интерфейс гибридного поиска (реализуется ai.SearchAndComposeService).
type Searcher interface {
	HybridSearch(ctx context.Context, auth *platform.AuthContext, query string, limit int) ([]ai.SearchResult, error)
}

// ChatAsker — узкий интерфейс диалога с ИИ (реализуется chat.Service).
type ChatAsker interface {
	CreateConversation(ctx context.Context, auth *platform.AuthContext, in chat.CreateConversationInput) (*chat.Conversation, error)
	GetConversation(ctx context.Context, auth *platform.AuthContext, id int64) (*chat.Conversation, error)
	SendMessage(ctx context.Context, auth *platform.AuthContext, convID int64, userText string, overrideMode chat.Mode, overrideModelID int64, emit func(chat.SSEEvent)) (*chat.ChatMessage, error)
}

type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	VisibleTo   func(auth *platform.AuthContext) bool
	Execute     func(ctx context.Context, auth *platform.AuthContext, baseURL string, args json.RawMessage) (string, error)
}

func (h *Handler) buildTools() []mcpTool {
	return []mcpTool{
		// --- Доступны любому валидному токену (в т.ч. ask_only) ---
		{
			Name:        "get_agent_guide",
			Description: "Справка для ИИ-агента: права токена, порядок работы с документацией, доступные инструменты и REST.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{
						"type":        "string",
						"description": "Раздел справки: all, access, connect, workflow, tools, rest, rules (пусто — краткое основное + список разделов)",
						"enum":        []string{"all", "access", "connect", "workflow", "tools", "rest", "rules"},
					},
				},
			},
			VisibleTo: func(auth *platform.AuthContext) bool { return auth != nil },
			Execute: func(ctx context.Context, auth *platform.AuthContext, baseURL string, args json.RawMessage) (string, error) {
				var in struct {
					Topic string `json:"topic"`
				}
				_ = json.Unmarshal(args, &in)
				var spaces []docs.SpaceSummary
				if h.docs != nil {
					spaces, _ = h.docs.ListSpacesForAuth(ctx, auth)
				}
				return BuildAgentGuide(baseURL, auth, spaces, h.visibleTools(auth), in.Topic), nil
			},
		},
		{
			Name:        "list_spaces",
			Description: "Список доступных пространств документации (название пространства и количество документов).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			VisibleTo: func(auth *platform.AuthContext) bool {
				return auth != nil && auth.HasOwnerPermission("docs.view")
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, _ json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				spaces, err := h.docs.ListSpacesForAuth(ctx, auth)
				if err != nil {
					return "", err
				}
				if len(spaces) == 0 {
					return "Доступных пространств документации пока нет.", nil
				}
				var b strings.Builder
				for _, sp := range spaces {
					fmt.Fprintf(&b, "- `%s` — документов: %d\n", sp.Space, sp.Count)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "ask",
			Description: "Задать конкретный вопрос документации Теплосферы: возвращает выжимку со ссылками на документы и thread_id для уточнений.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question":  map[string]any{"type": "string", "description": "Конкретный вопрос к документации"},
					"space":     map[string]any{"type": "string", "description": "Опционально: ограничить контекст указанным пространством"},
					"thread_id": map[string]any{"type": "integer", "description": "ID диалога из предыдущего ответа ask для уточняющего вопроса"},
				},
				"required": []string{"question"},
			},
			VisibleTo: canUseAsk,
			Execute:   h.toolAsk,
		},
		{
			Name:        "log_result",
			Description: "Зафиксировать итог выполненной задачи и принятые решения в документ «Журнал работ агентов».",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":     map[string]any{"type": "string", "description": "Краткий заголовок выполненной задачи"},
					"summary":   map[string]any{"type": "string", "description": "Что сделано и какие изменения внесены"},
					"space":     map[string]any{"type": "string", "description": "Пространство документации (по умолчанию general или первое разрешённое токену)"},
					"decisions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Список принятых решений (необязательно)"},
				},
				"required": []string{"title", "summary"},
			},
			VisibleTo: canUseLogResult,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					Title     string   `json:"title"`
					Summary   string   `json:"summary"`
					Space     string   `json:"space"`
					Decisions []string `json:"decisions"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", platform.NewValidation("Некорректные аргументы log_result", nil)
				}
				doc, err := h.docs.LogAgentResult(ctx, auth, in.Space, in.Title, in.Summary, in.Decisions)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Запись добавлена в «%s» (id=%d, space=%s, v%d).", doc.Title, doc.ID, doc.Space, doc.Version), nil
			},
		},

		// --- Чтение (недоступны ask_only) ---
		{
			Name:        "search",
			Description: "Гибридный поиск (полнотекст + семантика) по документации, вложениям и спецификациям запчастей. Возвращает фрагменты со ссылками.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Поисковый запрос или ключевые слова"},
					"space": map[string]any{"type": "string", "description": "Опционально: фильтр по пространству"},
					"limit": map[string]any{"type": "integer", "description": "Максимум результатов (по умолчанию 8, до 30)"},
				},
				"required": []string{"query"},
			},
			VisibleTo: canUseDocsRead,
			Execute:   h.toolSearch,
		},
		{
			Name:        "list_documents",
			Description: "Список документов (id, заголовок, тип, пространство, версия, теги, статус) с фильтрацией.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"space":    map[string]any{"type": "string", "description": "Фильтр по пространству"},
					"doc_type": map[string]any{"type": "string", "enum": []string{"article", "sop", "equipment_manual", "checklist", "adr", "note"}},
					"status":   map[string]any{"type": "string", "description": "Статус документа (active, draft, published, archived)"},
					"tag":      map[string]any{"type": "string", "description": "Фильтр по тегу"},
					"query":    map[string]any{"type": "string", "description": "Поиск по заголовку и тексту"},
					"limit":    map[string]any{"type": "integer", "description": "Максимум документов (по умолчанию 50, до 200)"},
					"offset":   map[string]any{"type": "integer", "description": "Смещение для пагинации"},
				},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					Space   string `json:"space"`
					DocType string `json:"doc_type"`
					Status  string `json:"status"`
					Tag     string `json:"tag"`
					Query   string `json:"query"`
					Limit   int    `json:"limit"`
					Offset  int    `json:"offset"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 {
					in.Limit = 50
				}
				if in.Limit > 200 {
					in.Limit = 200
				}
				if in.Space != "" && !auth.CanAccessSpace(in.Space) {
					return "", platform.NewForbidden("Токен не имеет доступа к пространству " + in.Space)
				}
				items, err := h.docs.ListDocuments(ctx, auth, docs.DocumentFilter{
					Space:           in.Space,
					DocType:         docs.DocType(in.DocType),
					Status:          in.Status,
					Tag:             in.Tag,
					Query:           in.Query,
					IncludeArchived: in.Status == "archived",
					Limit:           in.Limit,
					Offset:          in.Offset,
				})
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return "Документы по заданным фильтрам не найдены.", nil
				}
				var b strings.Builder
				for _, d := range items {
					tags := ""
					if len(d.Tags) > 0 {
						tags = " | теги: " + strings.Join(d.Tags, ", ")
					}
					fmt.Fprintf(&b, "- id=%d | [%s] %s (space=%s, v%d, status=%s%s)\n",
						d.ID, d.DocType, d.Title, d.Space, d.Version, d.Status, tags)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "get_document",
			Description: "Полный текст документа в Markdown по id: метаданные, текущая версия (для update_document), оглавление, привязки к CRM и список вложений.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "ID документа"},
				},
				"required": []string{"id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.ID <= 0 {
					return "", platform.NewValidation("Укажите корректный id документа", nil)
				}
				doc, err := h.docs.GetDocument(ctx, auth, in.ID)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "[document_id=%d | space=%s | type=%s | version=%d | status=%s]\n",
					doc.ID, doc.Space, doc.DocType, doc.Version, doc.Status)
				if len(doc.Tags) > 0 {
					fmt.Fprintf(&b, "Теги: %s\n", strings.Join(doc.Tags, ", "))
				}
				fmt.Fprintf(&b, "\n# %s\n\n%s", doc.Title, doc.Content)
				if len(doc.EntityLinks) > 0 {
					b.WriteString("\n\n## Привязки к сущностям CRM\n")
					for _, l := range doc.EntityLinks {
						fmt.Fprintf(&b, "- %s #%d: %s", l.EntityType, l.EntityID, l.EntityTitle)
						if l.Note != "" {
							fmt.Fprintf(&b, " (%s)", l.Note)
						}
						b.WriteByte('\n')
					}
				}
				if len(doc.Attachments) > 0 {
					b.WriteString("\n\n## Вложения и схемы\n")
					for _, a := range doc.Attachments {
						if a.Kind == docs.AttachmentKindSchematic {
							fmt.Fprintf(&b, "- [Схема] attachment_id=%d | %s | стр. %d | анализ=%s (для проверки используй inspect_schematic)\n",
								a.ID, a.Filename, a.PageNumber, a.AnalysisStatus)
						} else {
							fmt.Fprintf(&b, "- [Файл] attachment_id=%d | %s (%s, %d байт; для чтения текста используй read_attachment)\n",
								a.ID, a.Filename, a.MimeType, a.SizeBytes)
						}
					}
				}
				return b.String(), nil
			},
		},
		{
			Name:        "get_document_history",
			Description: "История версий (ревизий) документа по id; при передаче revision_id возвращает полный текст конкретной ревизии.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "integer", "description": "ID документа"},
					"revision_id": map[string]any{"type": "integer", "description": "Необязательно: ID конкретной ревизии для чтения её текста"},
				},
				"required": []string{"id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					ID         int64 `json:"id"`
					RevisionID int64 `json:"revision_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.ID <= 0 {
					return "", platform.NewValidation("Укажите корректный id документа", nil)
				}
				if in.RevisionID > 0 {
					rev, err := h.docs.GetRevision(ctx, auth, in.ID, in.RevisionID)
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("[document_id=%d | revision_id=%d | version=%d | %s | %s]\n\n# %s\n\n%s",
						rev.DocumentID, rev.ID, rev.Version, rev.CreatedAt.Format("2006-01-02 15:04"), rev.DiffSummary, rev.Title, rev.Content), nil
				}
				revs, err := h.docs.ListRevisions(ctx, auth, in.ID)
				if err != nil {
					return "", err
				}
				if len(revs) == 0 {
					return "История версий пуста.", nil
				}
				var b strings.Builder
				for _, r := range revs {
					fmt.Fprintf(&b, "- revision_id=%d | v%d | %s | %s | %s\n",
						r.ID, r.Version, r.CreatedAt.Format("2006-01-02 15:04"), r.Title, r.DiffSummary)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "list_attachments",
			Description: "Список вложений и электрических/гидравлических схем документа (attachment_id, имя, тип, страница, статус анализа).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer", "description": "ID документа"},
				},
				"required": []string{"document_id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					DocumentID int64 `json:"document_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.DocumentID <= 0 {
					return "", platform.NewValidation("Укажите корректный document_id", nil)
				}
				doc, err := h.docs.GetDocument(ctx, auth, in.DocumentID)
				if err != nil {
					return "", err
				}
				if len(doc.Attachments) == 0 {
					return "У документа нет вложений и схем.", nil
				}
				var b strings.Builder
				for _, a := range doc.Attachments {
					fmt.Fprintf(&b, "- attachment_id=%d | kind=%s | файл=%s | mime=%s | размер=%d | стр.=%d | analysis=%s | текст=%d симв.\n",
						a.ID, a.Kind, a.Filename, a.MimeType, a.SizeBytes, a.PageNumber, a.AnalysisStatus, len([]rune(a.ExtractedText)))
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "read_attachment",
			Description: "Прочитать извлечённый текст вложения документа (PDF, Excel, Word, текст или описание схемы) частями по 40 000 символов.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"attachment_id": map[string]any{"type": "integer", "description": "ID вложения из list_attachments или get_document"},
					"offset":        map[string]any{"type": "integer", "description": "Смещение в символах от начала (по умолчанию 0)"},
					"limit":         map[string]any{"type": "integer", "description": "Максимум символов в ответе (по умолчанию 40000, до 80000)"},
				},
				"required": []string{"attachment_id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					AttachmentID int64 `json:"attachment_id"`
					Offset       int   `json:"offset"`
					Limit        int   `json:"limit"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.AttachmentID <= 0 {
					return "", platform.NewValidation("Укажите корректный attachment_id", nil)
				}
				chunk, err := h.docs.ReadAttachmentText(ctx, auth, in.AttachmentID, in.Offset, in.Limit)
				if err != nil {
					return "", err
				}
				if chunk.TotalChars == 0 {
					return fmt.Sprintf("Вложение #%d (%s): извлечённый текст отсутствует. Если это схема-изображение, вызовите inspect_schematic.",
						chunk.Attachment.ID, chunk.Attachment.Filename), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "[attachment_id=%d | %s | offset=%d | total_chars=%d | has_more=%v",
					chunk.Attachment.ID, chunk.Attachment.Filename, chunk.Offset, chunk.TotalChars, chunk.HasMore)
				if chunk.HasMore {
					fmt.Fprintf(&b, " | next_offset=%d", chunk.NextOffset)
				}
				b.WriteString("]\n\n")
				b.WriteString(chunk.Text)
				return b.String(), nil
			},
		},
		{
			Name:        "list_comments",
			Description: "Список инлайн-примечаний и рецензий к документу.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer", "description": "ID документа"},
				},
				"required": []string{"document_id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					DocumentID int64 `json:"document_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.DocumentID <= 0 {
					return "", platform.NewValidation("Укажите корректный document_id", nil)
				}
				items, err := h.docs.ListComments(ctx, auth, in.DocumentID)
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return "Примечаний к документу нет.", nil
				}
				var b strings.Builder
				for _, c := range items {
					status := "открыто"
					if c.Resolved {
						status = "решено"
					}
					fmt.Fprintf(&b, "- comment_id=%d | %s | %s | %s",
						c.ID, status, c.AuthorName, c.CreatedAt.Format("2006-01-02 15:04"))
					if c.Quote != "" {
						fmt.Fprintf(&b, " | цитата: «%s»", c.Quote)
					}
					fmt.Fprintf(&b, "\n  %s\n", c.Body)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "get_documents_by_entity",
			Description: "Найти документы и инструкции, привязанные к сущности CRM (contractor, address, equipment, manufacturer, type_equipment, application, contract, certificate).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_type": map[string]any{"type": "string", "description": "Тип сущности CRM"},
					"entity_id":   map[string]any{"type": "integer", "description": "ID сущности CRM"},
				},
				"required": []string{"entity_type", "entity_id"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					EntityType string `json:"entity_type"`
					EntityID   int64  `json:"entity_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.EntityType) == "" || in.EntityID <= 0 {
					return "", platform.NewValidation("Укажите entity_type и entity_id", nil)
				}
				items, err := h.docs.ListDocumentsByEntity(ctx, auth, in.EntityType, in.EntityID)
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return "Связанные документы не найдены.", nil
				}
				var b strings.Builder
				for _, d := range items {
					fmt.Fprintf(&b, "- id=%d | [%s] %s (space=%s, v%d)\n", d.ID, d.DocType, d.Title, d.Space, d.Version)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "inspect_schematic",
			Description: "Визуальная проверка оригинала электрической или гидравлической схемы (вложения-изображения) по конкретному вопросу. Для мелких клемм укажи region (full, top_left, top_right, bottom_left, bottom_right, center или x1,y1,x2,y2).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"attachment_id": map[string]any{"type": "integer", "description": "ID вложения-схемы из list_attachments или get_document"},
					"question":      map[string]any{"type": "string", "description": "Конкретный вопрос по схеме (клеммы, контакты, провода, подключения)"},
					"region":        map[string]any{"type": "string", "description": "Необязательно: область увеличения (full, top_left, top_right, bottom_left, bottom_right, center или x1,y1,x2,y2)"},
				},
				"required": []string{"attachment_id", "question"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					AttachmentID int64  `json:"attachment_id"`
					Question     string `json:"question"`
					Region       string `json:"region"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.AttachmentID <= 0 || strings.TrimSpace(in.Question) == "" {
					return "", platform.NewValidation("Укажите attachment_id и question", nil)
				}
				att, answer, err := h.docs.InspectSchematic(ctx, auth, in.AttachmentID, in.Question, in.Region)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("[Схема: %s, стр. %d, document_id=%d, attachment_id=%d]\n\n%s",
					att.Filename, att.PageNumber, att.DocumentID, att.ID, answer), nil
			},
		},
		{
			Name:        "search_boiler_parts",
			Description: "Поиск запчастей, артикулов (Material No.), позиций на взрыв-схемах и замен артикулов по проиндексированным чертежам котлов.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"brand":       map[string]any{"type": "string", "description": "Производитель котла (Vaillant, BAXI, Protherm и др.)"},
					"model":       map[string]any{"type": "string", "description": "Модель котла"},
					"part_number": map[string]any{"type": "string", "description": "Артикул / Material No. запчасти"},
					"assembly":    map[string]any{"type": "string", "description": "Узел котла"},
					"query":       map[string]any{"type": "string", "description": "Название запчасти или ключевые слова"},
					"limit":       map[string]any{"type": "integer", "description": "Максимум позиций (по умолчанию 25, до 50)"},
				},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					Brand      string `json:"brand"`
					Model      string `json:"model"`
					PartNumber string `json:"part_number"`
					Assembly   string `json:"assembly"`
					Query      string `json:"query"`
					Limit      int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 25
				}
				items, err := h.docs.SearchBoilerParts(ctx, auth, docs.BoilerPartFilter{
					Brand:      in.Brand,
					Model:      in.Model,
					PartNumber: in.PartNumber,
					Assembly:   in.Assembly,
					Query:      in.Query,
					Limit:      in.Limit,
				})
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return "Запчасти в проиндексированных чертежах котлов по указанным параметрам не найдены.", nil
				}
				var b strings.Builder
				for _, p := range items {
					b.WriteString("- ")
					b.WriteString(docs.FormatBoilerPartLine(p))
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        "diagnose_boiler",
			Description: "Диагностика неисправности котла по проиндексированным чертежам и документации: релевантные документы, связанные узлы, артикулы запчастей и схемы.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"brand":   map[string]any{"type": "string", "description": "Производитель котла (необязательно)"},
					"model":   map[string]any{"type": "string", "description": "Модель котла"},
					"symptom": map[string]any{"type": "string", "description": "Симптом неисправности или код ошибки"},
				},
				"required": []string{"symptom"},
			},
			VisibleTo: canUseDocsRead,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					Brand   string `json:"brand"`
					Model   string `json:"model"`
					Symptom string `json:"symptom"`
				}
				if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Symptom) == "" {
					return "", platform.NewValidation("Укажите symptom", nil)
				}
				diag, err := h.docs.DiagnoseBoiler(ctx, auth, in.Brand, in.Model, in.Symptom)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				if len(diag.MatchedDocs) > 0 {
					b.WriteString("## Документы по котлу\n")
					for _, d := range diag.MatchedDocs {
						fmt.Fprintf(&b, "- id=%d | [%s] %s (space=%s)\n", d.ID, d.DocType, d.Title, d.Space)
					}
					b.WriteByte('\n')
				}
				if len(diag.RelatedParts) > 0 {
					b.WriteString("## Связанные узлы и запчасти из чертежей\n")
					for _, p := range diag.RelatedParts {
						b.WriteString("- ")
						b.WriteString(docs.FormatBoilerPartLine(p))
						b.WriteByte('\n')
					}
					b.WriteByte('\n')
				}
				if len(diag.Schematics) > 0 {
					b.WriteString("## Взрыв-схемы и чертежи (inspect_schematic)\n")
					for _, a := range diag.Schematics {
						fmt.Fprintf(&b, "- attachment_id=%d | документ #%d | %s | стр. %d | анализ=%s\n",
							a.ID, a.DocumentID, a.Filename, a.PageNumber, a.AnalysisStatus)
					}
				}
				if b.Len() == 0 {
					return "По указанному котлу и неисправности не найдено проиндексированных чертежей и документов.", nil
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},

		// --- Запись (только токен can_write + право владельца; удаления нет) ---
		{
			Name:        "create_document",
			Description: "Создать новый документ в документации (требуется токен с записью и право docs.create).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":        map[string]any{"type": "string", "description": "Заголовок документа"},
					"content":      map[string]any{"type": "string", "description": "Содержимое в Markdown"},
					"space":        map[string]any{"type": "string", "description": "Пространство (по умолчанию general или первое разрешённое токену)"},
					"doc_type":     map[string]any{"type": "string", "enum": []string{"article", "sop", "equipment_manual", "checklist", "adr", "note"}},
					"status":       map[string]any{"type": "string", "description": "Статус (по умолчанию active)"},
					"tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"visibility":   map[string]any{"type": "string", "enum": []string{"internal", "engineers", "dispatchers", "admins", "public"}},
					"diff_summary": map[string]any{"type": "string", "description": "Краткое описание создания для истории версий"},
				},
				"required": []string{"title", "content"},
			},
			VisibleTo: canUseDocsCreate,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in docs.SaveDocumentInput
				if err := json.Unmarshal(args, &in); err != nil {
					return "", platform.NewValidation("Некорректные аргументы create_document", nil)
				}
				doc, err := h.docs.CreateDocument(ctx, auth, in)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Создан документ id=%d «%s» (space=%s, type=%s, version=%d).",
					doc.ID, doc.Title, doc.Space, doc.DocType, doc.Version), nil
			},
		},
		{
			Name:        "update_document",
			Description: "Обновить существующий документ целиком с обязательным expected_version (защита от перезаписи параллельных правок). При конфликте перечитай документ через get_document и повтори.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":               map[string]any{"type": "integer", "description": "ID документа"},
					"expected_version": map[string]any{"type": "integer", "description": "Текущий номер версии документа из get_document"},
					"content":          map[string]any{"type": "string", "description": "Новое содержимое документа в Markdown"},
					"title":            map[string]any{"type": "string", "description": "Новый заголовок (необязательно)"},
					"space":            map[string]any{"type": "string", "description": "Новое пространство (необязательно)"},
					"doc_type":         map[string]any{"type": "string", "enum": []string{"article", "sop", "equipment_manual", "checklist", "adr", "note"}},
					"status":           map[string]any{"type": "string"},
					"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"visibility":       map[string]any{"type": "string", "enum": []string{"internal", "engineers", "dispatchers", "admins", "public"}},
					"diff_summary":     map[string]any{"type": "string", "description": "Описание внесённых изменений"},
				},
				"required": []string{"id", "expected_version", "content"},
			},
			VisibleTo: canUseDocsUpdate,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var raw struct {
					ID int64 `json:"id"`
					docs.SaveDocumentInput
				}
				if err := json.Unmarshal(args, &raw); err != nil || raw.ID <= 0 {
					return "", platform.NewValidation("Укажите корректный id, expected_version и content", nil)
				}
				doc, err := h.docs.UpdateDocument(ctx, auth, raw.ID, raw.SaveDocumentInput)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Документ id=%d «%s» обновлён до версии v%d.", doc.ID, doc.Title, doc.Version), nil
			},
		},
		{
			Name:        "append_to_document",
			Description: "Дописать новый раздел или фрагмент в конец существующего документа.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":       map[string]any{"type": "integer", "description": "ID документа"},
					"addition": map[string]any{"type": "string", "description": "Добавляемый текст в Markdown"},
				},
				"required": []string{"id", "addition"},
			},
			VisibleTo: canUseDocsUpdate,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					ID       int64  `json:"id"`
					Addition string `json:"addition"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.ID <= 0 {
					return "", platform.NewValidation("Укажите корректный id и addition", nil)
				}
				doc, err := h.docs.AppendDocument(ctx, auth, in.ID, in.Addition)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Документ id=%d «%s» дополнен (новая версия v%d).", doc.ID, doc.Title, doc.Version), nil
			},
		},
		{
			Name:        "add_comment",
			Description: "Добавить инлайн-примечание или рецензию к документу.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer", "description": "ID документа"},
					"body":        map[string]any{"type": "string", "description": "Текст примечания"},
					"quote":       map[string]any{"type": "string", "description": "Необязательно: точная цитата из текста документа"},
				},
				"required": []string{"document_id", "body"},
			},
			VisibleTo: canUseDocsComment,
			Execute: func(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
				if h.docs == nil {
					return "", platform.NewNotConfigured("Модуль документации не активен")
				}
				var in struct {
					DocumentID int64  `json:"document_id"`
					Body       string `json:"body"`
					Quote      string `json:"quote"`
				}
				if err := json.Unmarshal(args, &in); err != nil || in.DocumentID <= 0 {
					return "", platform.NewValidation("Укажите корректный document_id и body", nil)
				}
				c, err := h.docs.AddComment(ctx, auth, in.DocumentID, docs.AddCommentInput{
					Body:  in.Body,
					Quote: in.Quote,
				})
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Добавлено примечание comment_id=%d к документу #%d.", c.ID, c.DocumentID), nil
			},
		},
	}
}

func (h *Handler) toolSearch(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		Space string `json:"space"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return "", platform.NewValidation("Укажите поисковый запрос query", nil)
	}
	if in.Limit <= 0 || in.Limit > 30 {
		in.Limit = 8
	}
	if in.Space != "" && !auth.CanAccessSpace(in.Space) {
		return "", platform.NewForbidden("Токен не имеет доступа к пространству " + in.Space)
	}

	// Сужаем контекст поиска до запрошенного пространства, чтобы CanView отфильтровал и документы, и вложения, и запчасти.
	searchAuth := auth
	if in.Space != "" && auth != nil {
		cp := *auth
		cp.IsToken = true
		cp.TokenCanRead = true
		cp.TokenAllowedSpaces = []string{strings.TrimSpace(in.Space)}
		searchAuth = &cp
	}

	// Сначала пробуем гибридный поиск (FTS + pgvector), фильтруя только источники документации.
	if h.search != nil && searchAuth.HasPermission("ai.search") {
		results, err := h.search.HybridSearch(ctx, searchAuth, in.Query, in.Limit)
		if err == nil {
			var filtered []ai.SearchResult
			for _, r := range results {
				if r.SourceType != "doc" && r.SourceType != "doc_attachment" && r.SourceType != "boiler_parts" {
					continue
				}
				if in.Space != "" && r.Metadata["space"] != "" && !strings.EqualFold(r.Metadata["space"], in.Space) {
					continue
				}
				filtered = append(filtered, r)
			}
			if len(filtered) > 0 {
				var b strings.Builder
				for i, r := range filtered {
					snippet := strings.TrimSpace(r.Snippet)
					if snippet == "" {
						snippet = strings.TrimSpace(r.Content)
					}
					fmt.Fprintf(&b, "[%d] [%s] %s (%s)\n%s\n\n", i+1, r.SourceType, r.Title, r.Path, snippet)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			}
		}
	}

	// Фолбэк на прямой поиск по документам (если индекс пуст или нет ai.search).
	if h.docs == nil {
		return "Подходящие материалы не найдены.", nil
	}
	items, err := h.docs.ListDocuments(ctx, auth, docs.DocumentFilter{
		Space: in.Space,
		Query: in.Query,
		Limit: in.Limit,
	})
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "Подходящие материалы в документации не найдены.", nil
	}
	var b strings.Builder
	for i, d := range items {
		fmt.Fprintf(&b, "[%d] [doc] %s (id=%d, space=%s, type=%s, v%d)\n",
			i+1, d.Title, d.ID, d.Space, d.DocType, d.Version)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (h *Handler) toolAsk(ctx context.Context, auth *platform.AuthContext, _ string, args json.RawMessage) (string, error) {
	var in struct {
		Question string `json:"question"`
		Space    string `json:"space"`
		ThreadID int64  `json:"thread_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		return "", platform.NewValidation("Укажите вопрос question", nil)
	}
	if in.Space != "" && !auth.CanAccessSpace(in.Space) {
		return "", platform.NewForbidden("Токен не имеет доступа к пространству " + in.Space)
	}

	// Создаём внутренний read-only контекст с сохранением ограничения пространств и прав владельца.
	// Если передан space — принудительно сужаем TokenAllowedSpaces до этого пространства.
	readAuth := auth
	if auth != nil {
		cp := *auth
		if cp.IsToken && cp.TokenAskOnly {
			cp.TokenAskOnly = false
			cp.TokenCanRead = true
			cp.TokenCanWrite = false
		}
		if in.Space != "" {
			cp.IsToken = true
			cp.TokenCanRead = true
			cp.TokenCanWrite = false
			cp.TokenAllowedSpaces = []string{strings.TrimSpace(in.Space)}
		}
		readAuth = &cp
	}

	prompt := strings.TrimSpace(in.Question)
	if in.Space != "" {
		prompt = fmt.Sprintf("[Пространство документации: %s]\n%s", strings.TrimSpace(in.Space), prompt)
	}

	if h.chat != nil {
		convID := in.ThreadID
		if convID > 0 {
			conv, err := h.chat.GetConversation(ctx, readAuth, convID)
			if err != nil {
				return "", err
			}
			// Даже токен суперадминистратора через MCP не должен иметь доступа к чужим личным диалогам или чатам карточек CRM.
			if auth != nil && conv.UserID != auth.UserID {
				return "", platform.NewForbidden("Нет доступа к чужому диалогу")
			}
			if strings.TrimSpace(conv.EntityType) != "" {
				return "", platform.NewForbidden("Диалог привязан к сущности CRM и недоступен через MCP документации")
			}
		} else {
			title := "MCP ask"
			if auth != nil && auth.IsToken && auth.TokenName != "" {
				title = fmt.Sprintf("MCP ask (%s)", auth.TokenName)
			}
			conv, err := h.chat.CreateConversation(ctx, readAuth, chat.CreateConversationInput{
				Title: title,
				Mode:  chat.ModeRAG,
			})
			if err != nil {
				return "", err
			}
			convID = conv.ID
		}
		// Используем ModeRAG, чтобы ask работал строго по базе знаний без доступа к инструментам CRM и мутациям.
		msg, err := h.chat.SendMessage(ctx, readAuth, convID, prompt, chat.ModeRAG, 0, nil)
		if err == nil && msg != nil {
			var b strings.Builder
			b.WriteString(strings.TrimSpace(msg.Content))
			if len(msg.Sources) > 0 {
				var docSources []platform.SourceRef
				for _, s := range msg.Sources {
					if s.SourceType == "doc" || s.SourceType == "doc_attachment" || s.SourceType == "boiler_parts" {
						docSources = append(docSources, s)
					}
				}
				if len(docSources) > 0 {
					b.WriteString("\n\n**Источники:**\n")
					for i, s := range docSources {
						fmt.Fprintf(&b, "[%d] %s (%s)\n", i+1, s.Title, s.Path)
					}
				}
			}
			fmt.Fprintf(&b, "\nthread_id: %d", convID)
			return b.String(), nil
		}
		// Если LLM не настроен, аккуратно отдаём найденные фрагменты из документации.
		if !errors.Is(err, platform.ErrNotConfigured) {
			return "", err
		}
	}

	// Фолбэк без LLM: вернуть релевантные фрагменты поиска.
	rawArgs, _ := json.Marshal(map[string]any{
		"query": in.Question,
		"space": in.Space,
		"limit": 6,
	})
	found, err := h.toolSearch(ctx, readAuth, "", rawArgs)
	if err != nil {
		return "", err
	}
	return "LLM-модель для синтеза ответа не настроена; найдены релевантные материалы:\n\n" + found, nil
}
