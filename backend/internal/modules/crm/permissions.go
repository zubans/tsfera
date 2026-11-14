package crm

import (
	"context"
	"github.com/zubans/tsfera/backend/pkg/platform"
	"net/http"
)

func requireCapability(auth *platform.AuthContext, permissions ...string) error {
	for _, permission := range permissions {
		if auth.HasPermission(permission) {
			return nil
		}
	}
	return platform.NewForbidden("Недостаточно прав")
}

func (h *Handler) updateApplicationComment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Comment string `json:"comment"`
	}
	if err = platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	app, err := h.svc.UpdateApplicationComment(r.Context(), ac, id, in.Comment)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, app)
}
func (s *Service) UpdateApplicationComment(ctx context.Context, auth *platform.AuthContext, id int64, comment string) (*Application, error) {
	if err := requireCapability(auth, "crm.application.update_comment"); err != nil {
		return nil, err
	}
	app, err := s.GetApplication(ctx, auth, id)
	if err != nil {
		return nil, err
	}
	// Update only the comment: a narrow permission must not overwrite another
	// concurrent edit of the document through a whole-record read/modify/write.
	repo, ok := s.repo.(interface {
		UpdateApplicationComment(context.Context, int64, string) error
	})
	if !ok {
		return nil, platform.NewNotConfigured("Обновление комментария не поддерживается хранилищем")
	}
	if err = repo.UpdateApplicationComment(ctx, id, comment); err != nil {
		return nil, err
	}
	app.Comment = comment
	s.logChange(ctx, auth, "application", id, "comment", map[string]string{"comment": comment})
	return app, nil
}
func (r *PgRepository) UpdateApplicationComment(ctx context.Context, id int64, comment string) error {
	return platform.ExecExpectingOne(ctx, r.db, `UPDATE crm_applications SET comment=$2,updated_at=now() WHERE id=$1 AND NOT deleted`, "Заявка не найдена", id, comment)
}

func scopedReport(auth *platform.AuthContext, f ReportFilter, kind string) (ReportFilter, error) {
	if auth == nil || auth.IsSuperAdmin || auth.HasPermission("crm.report.view") || auth.HasPermission("crm.report."+kind+".allow_select_subdivision") {
		return f, nil
	}
	if auth.SubdivisionID == nil {
		return f, platform.NewForbidden("Не назначено подразделение для отчёта")
	}
	// Explicit query parameters must not bypass the same restriction.
	f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	return f, nil
}
