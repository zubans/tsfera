package docs

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Handler struct {
	svc    *Service
	auth   platform.Authorizer
	logger *slog.Logger
}

func NewHandler(svc *Service, auth platform.Authorizer, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{svc: svc, auth: auth, logger: logger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.auth, false))
		pr.Route("/api/v1/docs", func(dr chi.Router) {
			dr.With(platform.RequirePermission("docs.view")).Get("/spaces", h.listSpaces)
			dr.With(platform.RequirePermission("docs.view")).Get("/documents", h.listDocuments)
			dr.With(platform.RequirePermission("docs.view")).Get("/documents/{id}", h.getDocument)
			dr.With(platform.RequirePermission("docs.create")).Post("/documents", h.createDocument)
			dr.With(platform.RequirePermission("docs.update")).Put("/documents/{id}", h.updateDocument)
			dr.With(platform.RequirePermission("docs.update")).Post("/documents/{id}/append", h.appendDocument)
			dr.With(platform.RequirePermission("docs.delete")).Delete("/documents/{id}", h.deleteDocument)

			// Ревизии и восстановление
			dr.With(platform.RequirePermission("docs.view")).Get("/documents/{id}/revisions", h.listRevisions)
			dr.With(platform.RequirePermission("docs.view")).Get("/documents/{id}/revisions/{revID}", h.getRevision)
			dr.With(platform.RequirePermission("docs.update")).Post("/documents/{id}/revisions/{revID}/restore", h.restoreRevision)

			// Привязки к сущностям CRM
			dr.With(platform.RequirePermission("docs.view")).Get("/by-entity/{entityType}/{entityID}", h.listByEntity)
			dr.With(platform.RequirePermission("docs.update")).Post("/documents/{id}/links", h.addLink)
			dr.With(platform.RequirePermission("docs.update")).Delete("/links/{linkID}", h.deleteLink)

			// Примечания / инлайн-комментарии
			dr.With(platform.RequirePermission("docs.view")).Get("/documents/{id}/comments", h.listComments)
			dr.With(platform.RequirePermission("docs.comment")).Post("/documents/{id}/comments", h.addComment)
			dr.With(platform.RequirePermission("docs.comment")).Post("/documents/{id}/comments/{commentID}/resolve", h.resolveComment)
			dr.With(platform.RequirePermission("docs.comment")).Delete("/documents/{id}/comments/{commentID}", h.deleteComment)

			// Вложения, импорт файлов (.docx/.xlsx/.pdf) и context.md экспорт
			dr.With(platform.RequirePermission("docs.update")).Post("/documents/{id}/attachments", h.uploadAttachment)
			dr.With(platform.RequirePermission("docs.view")).Get("/attachments/{attID}/download", h.downloadAttachment)
			dr.With(platform.RequirePermission("docs.create")).Post("/import", h.importFile)
			dr.With(platform.RequirePermission("docs.create")).Post("/import-docx", h.importFile)
			dr.With(platform.RequirePermission("docs.view")).Get("/context.md", h.exportContext)

			// Индекс запчастей и спецификаций из чертежей котлов
			dr.With(platform.RequirePermission("docs.view")).Get("/boiler-parts", h.searchBoilerParts)
			dr.With(platform.RequirePermission("docs.view")).Get("/documents/{id}/boiler-parts", h.listDocumentBoilerParts)
		})
	})
}

func parseID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, platform.NewValidation("Некорректный идентификатор", nil)
	}
	return id, nil
}

func (h *Handler) listSpaces(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSpaces(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := h.svc.ListDocuments(r.Context(), ac, DocumentFilter{
		Space:           r.URL.Query().Get("space"),
		DocType:         DocType(r.URL.Query().Get("doc_type")),
		Status:          r.URL.Query().Get("status"),
		Tag:             r.URL.Query().Get("tag"),
		Query:           r.URL.Query().Get("q"),
		IncludeArchived: r.URL.Query().Get("archived") == "true",
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getDocument(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.GetDocument(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) createDocument(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in SaveDocumentInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.CreateDocument(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, doc)
}

func (h *Handler) updateDocument(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in SaveDocumentInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.UpdateDocument(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) appendDocument(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Addition string `json:"addition"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.AppendDocument(r.Context(), ac, id, in.Addition)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteDocument(r.Context(), requestAuth(r), id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listRevisions(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	revs, err := h.svc.ListRevisions(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, revs)
}

func (h *Handler) getRevision(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	revID, err := parseID(r, "revID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rev, err := h.svc.GetRevision(r.Context(), ac, id, revID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rev)
}

func (h *Handler) restoreRevision(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	revID, err := parseID(r, "revID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.RestoreRevision(r.Context(), ac, id, revID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) listByEntity(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	eid, err := parseID(r, "entityID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListDocumentsByEntity(r.Context(), ac, chi.URLParam(r, "entityType"), eid)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) addLink(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		EntityType string `json:"entity_type"`
		EntityID   int64  `json:"entity_id"`
		Note       string `json:"note"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	link, err := h.svc.LinkEntity(r.Context(), ac, docID, in.EntityType, in.EntityID, in.Note)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, link)
}

func (h *Handler) deleteLink(w http.ResponseWriter, r *http.Request) {
	linkID, err := parseID(r, "linkID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.UnlinkEntity(r.Context(), linkID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListComments(r.Context(), ac, docID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in AddCommentInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	c, err := h.svc.AddComment(r.Context(), ac, docID, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) resolveComment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	cid, err := parseID(r, "commentID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Resolved bool `json:"resolved"`
	}
	_ = platform.DecodeJSON(r, &in)
	c, err := h.svc.ResolveComment(r.Context(), ac, docID, cid, in.Resolved)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	cid, err := parseID(r, "commentID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteComment(r.Context(), requestAuth(r), docID, cid); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Ошибка разбора multipart формы", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Передайте файл в поле file", nil))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 25<<20))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	att, err := h.svc.SaveAttachment(r.Context(), ac, docID, header.Filename, header.Header.Get("Content-Type"), data)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, att)
}

func (h *Handler) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	attID, err := parseID(r, "attID")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	att, data, err := h.svc.GetAttachmentForDownload(r.Context(), ac, attID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", att.MimeType)
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && strings.HasPrefix(strings.ToLower(att.MimeType), "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+`; filename*=UTF-8''`+url.PathEscape(att.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) importFile(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Ошибка разбора multipart формы", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Передайте файл (.docx, .xlsx или .pdf) в поле file", nil))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 50<<20))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	doc, err := h.svc.ImportFile(r.Context(), ac, r.FormValue("space"), r.FormValue("title"), r.FormValue("doc_type"), header.Filename, data)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, doc)
}

func (h *Handler) exportContext(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, _ := strconv.ParseInt(r.URL.Query().Get("document_id"), 10, 64)
	md, err := h.svc.ExportContextMarkdown(r.Context(), ac, r.URL.Query().Get("space"), docID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}

func (h *Handler) searchBoilerParts(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, _ := strconv.ParseInt(r.URL.Query().Get("document_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.SearchBoilerParts(r.Context(), ac, BoilerPartFilter{
		DocumentID: docID,
		Brand:      r.URL.Query().Get("brand"),
		Model:      r.URL.Query().Get("model"),
		PartNumber: r.URL.Query().Get("part_number"),
		Assembly:   r.URL.Query().Get("assembly"),
		Query:      r.URL.Query().Get("q"),
		Limit:      limit,
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) listDocumentBoilerParts(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	docID, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListDocumentBoilerParts(r.Context(), ac, docID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func requestAuth(r *http.Request) *platform.AuthContext {
	ac, _ := platform.AuthFromContext(r.Context())
	return ac
}
