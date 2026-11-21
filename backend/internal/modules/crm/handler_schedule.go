package crm

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

func (h *Handler) mountSchedule(cr chi.Router) {
	cr.With(platform.RequirePermission("crm.calendar.view")).Get("/calendar/load", h.calendarLoad)
	cr.With(platform.RequirePermission("crm.application.view")).Post("/applications/check-slot", h.checkApplicationSlot)
	cr.With(platform.RequirePermission("crm.application.update")).Patch("/applications/{id}/schedule", h.rescheduleApplication)

	cr.With(platform.RequireAnyPermission("crm.timesheet.view", "crm.timesheet.manage", "crm.calendar.view")).Get("/timesheet", h.listTimesheet)
	cr.With(platform.RequirePermission("crm.timesheet.manage")).Post("/timesheet", h.createTimesheetEntry)
	cr.With(platform.RequirePermission("crm.timesheet.manage")).Post("/timesheet/generate", h.generateTimesheet)
	cr.With(platform.RequirePermission("crm.timesheet.manage")).Put("/timesheet/{id}", h.updateTimesheetEntry)
	cr.With(platform.RequirePermission("crm.timesheet.manage")).Delete("/timesheet/{id}", h.deleteTimesheetEntry)

	cr.With(platform.RequireAnyPermission("crm.plan.view", "crm.plan.manage", "crm.plan.view_own")).Get("/daily-plans", h.listDailyPlans)
	cr.With(platform.RequirePermission("crm.plan.manage")).Post("/daily-plans", h.saveDailyPlan)
	cr.With(platform.RequirePermission("crm.plan.manage")).Delete("/daily-plans/{id}", h.deleteDailyPlan)
	cr.With(platform.RequireAnyPermission("crm.plan.view", "crm.plan.manage", "crm.plan.view_own")).Get("/reports/plan-fact", h.planFactReport)
}

// parsePeriod разбирает даты from/to («ГГГГ-ММ-ДД», to включительно) в полуоткрытый интервал по Москве.
func (h *Handler) parsePeriod(r *http.Request) (time.Time, time.Time, error) {
	from, err := h.svc.ParseLocalDate(r.URL.Query().Get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := h.svc.ParseLocalDate(r.URL.Query().Get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to.AddDate(0, 0, 1), nil
}

func (h *Handler) calendarLoad(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	raw := r.URL.Query().Get("date")
	var day time.Time
	if raw == "" {
		day = h.svc.startOfLocalDay(h.svc.now())
	} else {
		d, err := h.svc.ParseLocalDate(raw)
		if err != nil {
			platform.WriteError(w, h.logger, err)
			return
		}
		day = d
	}
	res, err := h.svc.CalendarLoad(r.Context(), ac, day,
		parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		parseOptionalInt64(r.URL.Query().Get("subdivision_id")))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) checkApplicationSlot(w http.ResponseWriter, r *http.Request) {
	var in SlotCheckInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	res, err := h.svc.CheckApplicationSlot(r.Context(), in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) rescheduleApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		EngineerID int64     `json:"engineer_id"`
		From       time.Time `json:"plan_datetime_from"`
		To         time.Time `json:"plan_datetime_to"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	app, err := h.svc.RescheduleApplication(r.Context(), ac, id, in.EngineerID, in.From, in.To)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, app)
}

func (h *Handler) listTimesheet(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	from, to, err := h.parsePeriod(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListTimesheet(r.Context(), ac, from, to,
		parseOptionalInt64(r.URL.Query().Get("subdivision_id")),
		parseOptionalInt64(r.URL.Query().Get("user_id")))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createTimesheetEntry(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in TimesheetEntry
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	item, err := h.svc.CreateTimesheetEntry(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) updateTimesheetEntry(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in TimesheetEntry
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	item, err := h.svc.UpdateTimesheetEntry(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) deleteTimesheetEntry(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteTimesheetEntry(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) generateTimesheet(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in GenerateTimesheetInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	n, err := h.svc.GenerateTimesheet(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]int{"created": n})
}

func (h *Handler) listDailyPlans(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListDailyPlans(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) saveDailyPlan(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		SubdivisionID int64   `json:"subdivision_id"`
		Amount        float64 `json:"amount"`
		EffectiveFrom string  `json:"effective_from"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	item, err := h.svc.SaveDailyPlan(r.Context(), ac, in.SubdivisionID, in.Amount, in.EffectiveFrom)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) deleteDailyPlan(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteDailyPlan(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) planFactReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	from, to, err := h.parsePeriod(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rep, err := h.svc.PlanFact(r.Context(), ac, from, to, parseOptionalInt64(r.URL.Query().Get("subdivision_id")))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rep)
}
