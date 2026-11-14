package ai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Handler struct {
	repo    Repository
	gateway *Gateway
	search  *SearchAndComposeService
	auth    platform.Authorizer
	logger  *slog.Logger
}

func NewHandler(repo Repository, gateway *Gateway, search *SearchAndComposeService, auth platform.Authorizer, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{repo: repo, gateway: gateway, search: search, auth: auth, logger: logger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.auth, false))
		pr.Route("/api/v1/ai", func(ar chi.Router) {
			ar.With(platform.RequirePermission("ai.search")).Get("/search", h.hybridSearch)
			ar.With(platform.RequirePermission("ai.admin")).Post("/reindex", h.reindex)

			ar.With(platform.RequirePermission("ai.use")).Get("/models", h.listModels)
			ar.With(platform.RequirePermission("ai.admin")).Post("/models", h.saveModel)
			ar.With(platform.RequirePermission("ai.admin")).Delete("/models/{id}", h.deleteModel)

			ar.With(platform.RequirePermission("ai.admin")).Get("/providers", h.listProviders)
			ar.With(platform.RequirePermission("ai.admin")).Post("/providers", h.saveProvider)
			ar.With(platform.RequirePermission("ai.admin")).Delete("/providers/{id}", h.deleteProvider)

			ar.With(platform.RequirePermission("ai.compose")).Post("/compose", h.composeSSE)
			ar.With(platform.RequirePermission("ai.admin")).Get("/usage", h.listUsage)
		})
	})
}

func (h *Handler) hybridSearch(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.search.HybridSearch(r.Context(), ac, r.URL.Query().Get("q"), limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) reindex(w http.ResponseWriter, r *http.Request) {
	count, err := h.search.ReindexAll(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]int{"reindexed": count})
}

func (h *Handler) listProviders(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListProviders(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) saveProvider(w http.ResponseWriter, r *http.Request) {
	var in Provider
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || in.Kind == "" {
		platform.WriteError(w, h.logger, platform.NewValidation("Укажите название и тип провайдера", nil))
		return
	}
	if err := h.repo.SaveProvider(r.Context(), &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	in.HasAPIKey = in.APIKey != ""
	in.APIKey = ""
	platform.WriteJSON(w, http.StatusOK, in)
}

func (h *Handler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	if err := h.repo.DeleteProvider(r.Context(), id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var capPtr *ModelCapability
	if capRaw := r.URL.Query().Get("capability"); capRaw != "" {
		c := ModelCapability(capRaw)
		capPtr = &c
	}
	models, err := h.repo.ListModels(r.Context(), capPtr)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	// Фильтруем доступные модели для не-администраторов.
	if ac != nil && !ac.IsSuperAdmin && !ac.HasPermission("ai.admin") && h.gateway.access != nil {
		var visible []Model
		for _, m := range models {
			if !m.Active {
				continue
			}
			ok, err := h.gateway.access.CanAccessModel(r.Context(), ac, m.ID, m.OpenToAll)
			if err == nil && ok {
				visible = append(visible, m)
			}
		}
		platform.WriteJSON(w, http.StatusOK, visible)
		return
	}
	platform.WriteJSON(w, http.StatusOK, models)
}

func (h *Handler) saveModel(w http.ResponseWriter, r *http.Request) {
	var in Model
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if in.ProviderID <= 0 || strings.TrimSpace(in.ModelID) == "" {
		platform.WriteError(w, h.logger, platform.NewValidation("Укажите provider_id и model_id", nil))
		return
	}
	if in.Name == "" {
		in.Name = in.ModelID
	}
	if in.Capability == "" {
		in.Capability = CapChat
	}
	if in.ContextWindow <= 0 {
		in.ContextWindow = 32768
	}
	if err := h.repo.SaveModel(r.Context(), &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, in)
}

func (h *Handler) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	if err := h.repo.DeleteModel(r.Context(), id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) composeSSE(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var req ComposeRequest
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	writeSSE := func(ev StreamEvent) error {
		b, _ := json.Marshal(ev)
		_, err := fmt.Fprintf(w, "data: %s\n\n", b)
		if err == nil && flusher != nil {
			flusher.Flush()
		}
		return err
	}

	_, err := h.search.ComposeStream(r.Context(), ac, req, func(delta string) error {
		return writeSSE(StreamEvent{Type: "delta", Delta: delta})
	})
	if err != nil {
		_ = writeSSE(StreamEvent{Type: "error", Error: err.Error()})
		return
	}
	_ = writeSSE(StreamEvent{Type: "done"})
}

func (h *Handler) listUsage(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.repo.ListUsageLogs(r.Context(), limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}
