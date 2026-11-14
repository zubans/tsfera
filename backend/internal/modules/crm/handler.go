package crm

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	// Публичный вебхук МТС ВАТС (авторизация по x-auth-token внутри сервиса).
	r.Post("/api/v1/crm/webhooks/mts", h.mtsWebhook)
	r.Post("/api/v1/crm/webhooks/mango/events/{kind}", h.mangoWebhook)
	r.Post("/api/v1/crm/webhooks/mango/result/{kind}", h.mangoWebhook)
	// Legacy-путь вебхука для плавного переключения телефонии.
	r.Post("/telephony/webhook", h.mtsWebhook)
	// Публичные вебхуки CloudPayments (pay / recurrent, авторизация Content-HMAC).
	r.Post("/api/v1/crm/webhooks/cloudpayments/{kind}", h.cloudPaymentsWebhook)

	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.auth, false))

		// WebSocket для уведомлений, звонков, срочных задач и автообновления календаря.
		pr.Get("/ws/crm", h.serveWS)

		pr.Route("/api/v1/crm", func(cr chi.Router) {
			// Контрагенты и адреса
			cr.With(platform.RequirePermission("crm.contractor.view")).Get("/contractors", h.listContractors)
			cr.With(platform.RequirePermission("crm.contractor.view")).Post("/contractors/check-similar", h.checkSimilarContractors)
			cr.With(platform.RequirePermission("crm.contractor.view")).Get("/contractors/{id}", h.getContractor)
			cr.With(platform.RequirePermission("crm.contractor.view")).Get("/contractors/{id}/subscriptions", h.getContractorSubscriptions)
			cr.With(platform.RequirePermission("crm.contractor.create")).Post("/contractors", h.createContractor)
			cr.With(platform.RequirePermission("crm.contractor.update")).Put("/contractors/{id}", h.updateContractor)
			cr.With(platform.RequirePermission("crm.contractor.delete")).Delete("/contractors/{id}", h.deleteContractor)

			cr.With(platform.RequirePermission("crm.address.view")).Get("/addresses", h.listAddresses)
			cr.With(platform.RequirePermission("crm.address.view")).Get("/addresses/suggest", h.suggestAddresses)
			cr.With(platform.RequirePermission("crm.address.view")).Get("/addresses/{id}/check-subscription", h.checkAddressSubscription)
			cr.With(platform.RequirePermission("crm.address.create")).Post("/addresses", h.createAddress)
			cr.With(platform.RequirePermission("crm.address.update")).Put("/addresses/{id}", h.updateAddress)
			cr.With(platform.RequirePermission("crm.address.delete")).Delete("/addresses/{id}", h.deleteAddress)

			// Оборудование, привязка к адресам и справочники
			cr.With(platform.RequirePermission("crm.equipment.view")).Get("/equipments", h.listEquipments)
			cr.With(platform.RequirePermission("crm.equipment.create")).Post("/equipments", h.createEquipment)
			cr.With(platform.RequirePermission("crm.address.update")).Post("/address-equipments", h.attachAddressEquipment)
			cr.With(platform.RequirePermission("crm.address.update")).Delete("/address-equipments/{id}", h.detachAddressEquipment)
			cr.Get("/refs/{kind}", h.listRefItems)
			cr.Post("/refs/{kind}", h.createRefItem)
			cr.Delete("/refs/{kind}/{id}", h.deleteRefItem)

			// Источники трафика
			cr.Get("/traffic-sources", h.listTrafficSources)
			cr.With(platform.RequireAnyPermission("crm.traffic_source.manage", "crm.traffic_source.create")).Post("/traffic-sources", h.createTrafficSource)
			cr.With(platform.RequireAnyPermission("crm.traffic_source.manage", "crm.traffic_source.update")).Put("/traffic-sources/{id}", h.updateTrafficSource)
			cr.With(platform.RequireAnyPermission("crm.traffic_source.manage", "crm.traffic_source.delete")).Delete("/traffic-sources/{id}", h.deleteTrafficSource)

			// Договоры ТО
			cr.With(platform.RequirePermission("crm.contract.view")).Get("/contracts", h.listContracts)
			cr.With(platform.RequirePermission("crm.contract.export")).Get("/contracts/export", h.exportContracts)
			cr.With(platform.RequirePermission("crm.contract.view")).Get("/contracts/{id}", h.getContract)
			cr.With(platform.RequirePermission("crm.contract.view")).Get("/contracts/{id}/print", h.printContract)
			cr.With(platform.RequirePermission("crm.contract.create")).Post("/contracts", h.createContract)
			cr.With(platform.RequirePermission("crm.contract.create")).Post("/contracts/{id}/renew", h.renewContract)
			cr.With(platform.RequirePermission("crm.contract.update")).Put("/contracts/{id}", h.updateContract)
			cr.With(platform.RequirePermission("crm.contract.delete")).Delete("/contracts/{id}", h.deleteContract)

			// Заявки и Календарь
			cr.With(platform.RequirePermission("crm.application.view")).Get("/applications", h.listApplications)
			cr.With(platform.RequirePermission("crm.application.view")).Get("/applications/export", h.exportApplications)
			cr.With(platform.RequirePermission("crm.calendar.view")).Get("/applications/calendar", h.listCalendarEvents)
			cr.With(platform.RequirePermission("crm.application.view")).Get("/applications/{id}", h.getApplication)
			cr.With(platform.RequirePermission("crm.application.create")).Post("/applications", h.createApplication)
			cr.With(platform.RequirePermission("crm.application.update")).Put("/applications/{id}", h.updateApplication)
			cr.With(platform.RequirePermission("crm.application.close")).Post("/applications/{id}/complete", h.completeApplication)
			cr.With(platform.RequirePermission("crm.application.rating")).Patch("/applications/{id}/rating", h.rateApplication)
			cr.With(platform.RequirePermission("crm.application.update")).Post("/applications/{id}/notes-viewed", h.markApplicationNotesViewed)
			cr.With(platform.RequirePermission("crm.application.delete")).Delete("/applications/{id}", h.deleteApplication)

			cr.With(platform.RequirePermission("crm.application.update_comment")).Patch("/applications/{id}/comment", h.updateApplicationComment)

			// Акты выполненных работ, печать и экспорт
			cr.With(platform.RequirePermission("crm.certificate.view")).Get("/certificates", h.listCertificates)
			cr.With(platform.RequirePermission("crm.certificate.export")).Get("/certificates/export", h.exportCertificates)
			cr.With(platform.RequirePermission("crm.certificate.export")).Get("/certificates/export.csv", h.exportCertificatesCSV)
			cr.With(platform.RequirePermission("crm.certificate.view")).Get("/certificates/{id}", h.getCertificate)
			cr.With(platform.RequirePermission("crm.certificate.view")).Get("/certificates/{id}/print", h.printCertificate)
			cr.With(platform.RequirePermission("crm.certificate.create")).Post("/certificates", h.createCertificate)
			cr.With(platform.RequirePermission("crm.certificate.update")).Put("/certificates/{id}", h.updateCertificate)
			cr.With(platform.RequirePermission("crm.certificate.delete")).Delete("/certificates/{id}", h.deleteCertificate)

			// Аналитика и вопросы контроля качества
			cr.With(platform.RequirePermission("crm.analytic.view")).Get("/analytic-questions", h.listAnalyticQuestions)
			cr.With(platform.RequireAnyPermission("crm.analytic.manage", "crm.analytic_question.create")).Post("/analytic-questions", h.createAnalyticQuestion)
			cr.With(platform.RequireAnyPermission("crm.analytic.manage", "crm.analytic_question.update")).Put("/analytic-questions/{id}", h.updateAnalyticQuestion)
			cr.With(platform.RequirePermission("crm.analytic.view")).Get("/analytics", h.listAnalytics)
			cr.With(platform.RequirePermission("crm.analytic.view")).Get("/analytics/export", h.exportAnalytics)
			cr.With(platform.RequirePermission("crm.analytic.view")).Get("/analytics/{id}", h.getAnalytic)
			cr.With(platform.RequireAnyPermission("crm.analytic.manage", "crm.analytic.create", "crm.analytic.update")).Post("/analytics/by-certificate/{certificateId}", h.saveAnalyticByCertificate)
			cr.With(platform.RequireAnyPermission("crm.analytic.manage", "crm.analytic.update")).Patch("/analytics/{id}/rating", h.setAnalyticRating)

			// Задачи и настройки пула
			cr.With(platform.RequirePermission("crm.task.view")).Get("/tasks", h.listTasks)
			cr.With(platform.RequirePermission("crm.task.view")).Get("/tasks/{id}", h.getTask)
			cr.With(platform.RequirePermission("crm.task.create")).Post("/tasks", h.createTask)
			cr.With(platform.RequirePermission("crm.task.update")).Put("/tasks/{id}", h.updateTask)
			cr.With(platform.RequirePermission("crm.task.execute")).Post("/tasks/{id}/execute", h.executeTask)
			cr.With(platform.RequirePermission("crm.task.delete")).Delete("/tasks/{id}", h.deleteTask)
			cr.With(platform.RequireAnyPermission("crm.pool.manage", "crm.pool.view")).Get("/pool-settings", h.listPoolSettings)
			cr.With(platform.RequireAnyPermission("crm.pool.manage", "crm.pool.update")).Post("/pool-settings", h.savePoolSetting)
			cr.With(platform.RequirePermission("crm.pool.manage")).Post("/pool-settings/run", h.runTaskJobs)

			// Звонки, пул пропущенных, SoftPhone, Подразделения, SIP
			cr.With(platform.RequirePermission("crm.call.view")).Get("/calls", h.listCalls)
			cr.With(platform.RequirePermission("crm.call.export")).Get("/calls/export", h.exportCalls)
			cr.With(platform.RequirePermission("crm.call.view")).Get("/calls/current", h.getCurrentCall)
			cr.With(platform.RequirePermission("crm.call.view")).Get("/calls/{id}", h.getCallCard)
			cr.With(platform.RequirePermission("crm.call.update")).Put("/calls/{id}", h.updateCallCard)
			cr.With(platform.RequirePermission("crm.call.view")).Get("/calls/{id}/recording", h.getCallRecording)
			cr.With(platform.RequirePermission("crm.call.update")).Post("/calls/{id}/transcribe", h.transcribeCall)
			cr.With(platform.RequirePermission("crm.call.update")).Post("/calls/transfer", h.transferCall)
			cr.With(platform.RequirePermission("crm.call.view")).Get("/miss-calls", h.listMissCallPool)
			cr.With(platform.RequirePermission("crm.call.update")).Post("/miss-calls/{callId}/viewed", h.markMissCallViewed)
			cr.Get("/softphone/config", h.softphoneConfig)
			cr.With(platform.RequirePermission("crm.subdivision.view")).Get("/subdivisions", h.listSubdivisions)
			cr.With(platform.RequireAnyPermission("crm.subdivision.manage", "crm.subdivision.create")).Post("/subdivisions", h.createSubdivision)
			cr.With(platform.RequireAnyPermission("crm.subdivision.manage", "crm.subdivision.update")).Put("/subdivisions/{id}", h.updateSubdivision)
			cr.With(platform.RequireAnyPermission("crm.subdivision.manage", "crm.subdivision.delete")).Delete("/subdivisions/{id}", h.deleteSubdivision)
			cr.With(platform.RequireAnyPermission("crm.sip.manage", "crm.sip.view")).Get("/sip-accounts", h.listSipAccounts)
			cr.With(platform.RequireAnyPermission("crm.sip.manage", "crm.sip.create", "crm.sip.update")).Post("/sip-accounts", h.saveSipAccount)
			cr.Get("/sip-accounts/select-with-user-name", h.listSipUsersSelect)

			// Баннеры и системные настройки приложения
			cr.Get("/banners/for-engineer", h.getBannersForEngineer)
			cr.With(platform.RequireAnyPermission("crm.banner.manage", "crm.banner.view")).Get("/banners", h.listBanners)
			cr.With(platform.RequireAnyPermission("crm.banner.manage", "crm.banner.create")).Post("/banners", h.createBanner)
			cr.With(platform.RequireAnyPermission("crm.banner.manage", "crm.banner.update")).Put("/banners/{id}", h.updateBanner)
			cr.With(platform.RequireAnyPermission("crm.banner.manage", "crm.banner.delete")).Delete("/banners/{id}", h.deleteBanner)
			cr.With(platform.RequireAnyPermission("crm.setting.manage", "crm.setting.view")).Get("/app-settings", h.listAppSettings)
			cr.With(platform.RequireAnyPermission("crm.setting.manage", "crm.setting.view")).Get("/app-settings/{key}", h.getAppSetting)
			cr.With(platform.RequireAnyPermission("crm.setting.manage", "crm.setting.update", "crm.setting.create")).Put("/app-settings/{key}", h.saveAppSetting)

			// Отчёты, аудит, уведомления, пользовательские настройки, справочные списки
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.sum_executor.view")).Get("/reports/amount-acts", h.amountActsReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.expired_tasks.view")).Get("/reports/expired-tasks", h.expiredTasksReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.sum_subdivision.view")).Get("/reports/sum-by-subdivision", h.subdivisionSumsReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.amount_spare_parts.view")).Get("/reports/amount-spare-parts", h.amountSparePartsReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.call_conversion.view")).Get("/reports/call-conversion", h.callConversionReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.contractors.view")).Get("/reports/contractors", h.contractorsReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.contractors.view")).Get("/reports/contractors/export", h.exportContractorsReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.engineers.view")).Get("/reports/engineers", h.engineersReport)
			cr.With(platform.RequireAnyPermission("crm.report.view", "crm.report.traffic_sources.view")).Get("/reports/traffic-sources", h.trafficSourcesReport)
			cr.Get("/change-logs/{entityType}/{id}", h.listChangeLogs)
			cr.Get("/notifications", h.listNotifications)
			cr.Post("/notifications/{id}/read", h.markNotificationRead)
			cr.Post("/notifications/read-all", h.markAllNotificationsRead)
			cr.Get("/settings/{key}", h.getUserSetting)
			cr.Put("/settings/{key}", h.saveUserSetting)
			cr.Get("/enums", h.listEnums)
			cr.Get("/workers", h.listWorkers)
		})

		// Мобильные / legacy-совместимые эндпоинты в формате {data, errors}.
		pr.Route("/api", func(mr chi.Router) {
			mr.With(platform.RequirePermission("crm.application.view")).Get("/doc/application-for-technical-work", h.legacyApplications)
			mr.With(platform.RequirePermission("crm.application.view")).Get("/doc/application-for-technical-work/{id}", h.legacyGetApplication)
			mr.With(platform.RequirePermission("crm.application.create")).Post("/doc/application-for-technical-work", h.legacyCreateApplication)
			mr.With(platform.RequirePermission("crm.application.update")).Post("/doc/application-for-technical-work/{id}", h.legacyUpdateApplication)
			mr.With(platform.RequirePermission("crm.application.update")).Put("/doc/application-for-technical-work/{id}", h.legacyUpdateApplication)

			mr.With(platform.RequirePermission("crm.certificate.view")).Get("/doc/certificate-of-completion", h.legacyCertificates)
			mr.With(platform.RequirePermission("crm.certificate.view")).Get("/doc/certificate-of-completion/{id}", h.legacyGetCertificate)
			mr.With(platform.RequirePermission("crm.certificate.create")).Post("/doc/certificate-of-completion", h.legacyCreateCertificate)
			mr.With(platform.RequirePermission("crm.certificate.update")).Post("/doc/certificate-of-completion/{id}", h.legacyUpdateCertificate)
			mr.With(platform.RequirePermission("crm.certificate.update")).Put("/doc/certificate-of-completion/{id}", h.legacyUpdateCertificate)
			// Алиас исторической опечатки из legacy Yii2 API для совместимости со старыми APK.
			mr.With(platform.RequirePermission("crm.certificate.view")).Get("/doc/certificate-of-competition", h.legacyCertificates)
			mr.With(platform.RequirePermission("crm.certificate.view")).Get("/doc/certificate-of-competition/{id}", h.legacyGetCertificate)
			mr.With(platform.RequirePermission("crm.certificate.create")).Post("/doc/certificate-of-competition", h.legacyCreateCertificate)
			mr.With(platform.RequirePermission("crm.certificate.update")).Post("/doc/certificate-of-competition/{id}", h.legacyUpdateCertificate)
			mr.With(platform.RequirePermission("crm.certificate.update")).Put("/doc/certificate-of-competition/{id}", h.legacyUpdateCertificate)

			mr.With(platform.RequirePermission("crm.contract.view")).Get("/doc/maintenance-contract", h.legacyContracts)
			mr.With(platform.RequirePermission("crm.contract.view")).Get("/doc/maintenance-contract/{id}", h.legacyGetContract)
			mr.With(platform.RequirePermission("crm.contract.create")).Post("/doc/maintenance-contract", h.legacyCreateContract)
			mr.With(platform.RequirePermission("crm.contract.update")).Post("/doc/maintenance-contract/{id}", h.legacyUpdateContract)
			mr.With(platform.RequirePermission("crm.contract.update")).Put("/doc/maintenance-contract/{id}", h.legacyUpdateContract)

			mr.With(platform.RequirePermission("crm.contractor.view")).Get("/ref/contractor", h.legacyContractors)
			mr.With(platform.RequirePermission("crm.contractor.view")).Get("/ref/contractor/{id}", h.legacyGetContractor)
			mr.With(platform.RequirePermission("crm.contractor.create")).Post("/ref/contractor", h.legacyCreateContractor)
			mr.With(platform.RequirePermission("crm.contractor.update")).Post("/ref/contractor/{id}", h.legacyUpdateContractor)
			mr.With(platform.RequirePermission("crm.contractor.update")).Put("/ref/contractor/{id}", h.legacyUpdateContractor)
			mr.With(platform.RequirePermission("crm.address.create")).Post("/ref/contractor/{id}/addresses", h.legacyCreateContractorAddress)
			mr.With(platform.RequirePermission("crm.contractor.view")).Get("/ref/contractor/selects/{id}", h.legacyGetContractor)

			mr.With(platform.RequirePermission("crm.address.view")).Get("/ref/address", h.legacyAddresses)
			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/equipment", h.legacyEquipments)
			mr.With(platform.RequirePermission("crm.equipment.create")).Post("/ref/equipment", h.legacyCreateEquipment)
			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/equipment/select-by-address-id/{id}", h.legacyEquipmentsByAddress)
			mr.With(platform.RequirePermission("crm.address.update")).Post("/ref/equipment/update-by-address/{id}", h.legacyAttachEquipmentByAddress)
			mr.With(platform.RequirePermission("crm.address.update")).Put("/ref/equipment/update-by-address/{id}", h.legacyAttachEquipmentByAddress)

			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/manufacturer", h.legacyRefByKind(RefKindManufacturer))
			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/type-equipment", h.legacyRefByKind(RefKindEquipmentType))
			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/type-of-job", h.legacyRefByKind(RefKindWorkType))
			mr.With(platform.RequirePermission("crm.equipment.view")).Get("/ref/special-note", h.legacyRefByKind(RefKindSpecialNote))
			mr.Get("/ref/sip-account/select-with-user-name", h.legacySipUsersSelect)

			mr.Get("/enum/work-types", h.legacyEnumWorkTypes)
			mr.Get("/enum/document-statuses", h.legacyEnumStatuses)
			mr.Get("/enum/maintenance-contract-category", h.legacyEnumContractCategories)
			mr.Get("/enum/payment-option", h.legacyEnumPaymentOptions)
			mr.Get("/enum/task-results", h.legacyEnumTaskResults)
			mr.Get("/enum/call-types-of-appeal", h.legacyEnumCallTypesOfAppeal)
		})
	})
}

func parseIDParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, platform.NewValidation("Некорректный идентификатор", nil)
	}
	return id, nil
}

func parseOptionalInt64(raw string) *int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

func parseInt64List(r *http.Request, keys ...string) []int64 {
	seen := map[int64]bool{}
	var out []int64
	q := r.URL.Query()
	for _, k := range keys {
		for _, raw := range q[k] {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if v, err := strconv.ParseInt(part, 10, 64); err == nil && v > 0 && !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func parseLimitOffset(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}

func parseDateParam(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	from, _, err := platform.ParseDateRange(raw)
	if err != nil {
		return nil, err
	}
	return &from, nil
}

func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request) {
	ac, ok := platform.AuthFromContext(r.Context())
	if !ok || h.svc.hub == nil {
		platform.WriteError(w, h.logger, platform.NewUnauthorized("Требуется авторизация"))
		return
	}
	h.svc.hub.ServeWS(w, r, ac.UserID)
}

func (h *Handler) mtsWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Не удалось прочитать тело запроса", nil))
		return
	}
	token := r.Header.Get("x-auth-token")
	if token == "" {
		token = r.Header.Get("X-Webhook-Token")
	}
	rec, err := h.svc.HandleMTSWebhook(r.Context(), token, body)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if rec == nil {
		platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "alive"})
		return
	}
	platform.WriteJSON(w, http.StatusOK, rec)
}

func (h *Handler) listContractors(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	q := r.URL.Query()
	queryText := q.Get("q")
	if queryText == "" {
		queryText = q.Get("query")
	}
	f := ContractorFilter{
		Query:           queryText,
		OnlyLegal:       q.Get("only_legal") == "true" || q.Get("only_legal") == "1",
		OnlyIndividuals: q.Get("only_individuals") == "true" || q.Get("only_individuals") == "1",
		Limit:           limit,
		Offset:          offset,
	}
	items, total, err := h.svc.SearchContractorsFiltered(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *Handler) checkSimilarContractors(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExcludeID *int64   `json:"exclude_id"`
		Phones    []string `json:"phones"`
		Emails    []string `json:"emails"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	matches, err := h.svc.CheckSimilarContractors(r.Context(), in.ExcludeID, in.Phones, in.Emails)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (h *Handler) getContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	c, err := h.svc.GetContractor(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) createContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in SaveContractorInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	c, err := h.svc.CreateContractor(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) updateContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in SaveContractorInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	c, err := h.svc.UpdateContractor(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) deleteContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteContractor(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listAddresses(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r)
	cid := parseOptionalInt64(r.URL.Query().Get("contractor_id"))
	items, err := h.svc.SearchAddresses(r.Context(), cid, r.URL.Query().Get("q"), limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in ContractorAddress
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateAddress(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in ContractorAddress
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateAddress(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteAddress(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listEquipments(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.ListEquipments(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createEquipment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		Type         string `json:"type"`
		Model        string `json:"model"`
		Manufacturer string `json:"manufacturer"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	eq, err := h.svc.CreateEquipment(r.Context(), ac, in.Type, in.Model, in.Manufacturer)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, eq)
}

func (h *Handler) attachAddressEquipment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in AddressEquipment
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.AttachAddressEquipment(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) detachAddressEquipment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DetachAddressEquipment(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listRefItems(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	if err := requireCapability(ac, "crm.ref.manage", "crm.equipment.view", "crm.ref."+chi.URLParam(r, "kind")+".view"); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}

	items, err := h.svc.ListRefItems(r.Context(), chi.URLParam(r, "kind"))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createRefItem(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		Name string `json:"name"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateRefItem(r.Context(), ac, chi.URLParam(r, "kind"), in.Name)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) deleteRefItem(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteRefItem(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listContracts(w http.ResponseWriter, r *http.Request) {
	f, err := buildContractFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListContracts(r.Context(), f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getContract(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetContract(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in MaintenanceContract
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateContract(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in MaintenanceContract
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateContract(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteContract(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listApplications(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := buildApplicationFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListApplications(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) listCalendarEvents(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	now := h.svc.now()
	from := now.AddDate(0, 0, -7)
	to := now.AddDate(0, 0, 14)
	if f, err := parseDateParam(r.URL.Query().Get("from")); err == nil && f != nil {
		from = *f
	}
	if t, err := parseDateParam(r.URL.Query().Get("to")); err == nil && t != nil {
		to = *t
	}
	items, err := h.svc.ListCalendarEvents(
		r.Context(),
		ac,
		from,
		to,
		parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		parseOptionalInt64(r.URL.Query().Get("subdivision_id")),
	)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetApplication(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Application
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateApplication(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in Application
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateApplication(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) completeApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		IsComplete *bool  `json:"is_complete"`
		Comment    string `json:"comment"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	complete := true
	if in.IsComplete != nil {
		complete = *in.IsComplete
	}
	out, err := h.svc.CompleteApplication(r.Context(), ac, id, complete, in.Comment)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) rateApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.RateApplication(r.Context(), ac, id, in.Rating, in.Comment); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) markApplicationNotesViewed(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.MarkApplicationNotesViewed(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) deleteApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteApplication(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listCertificates(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	from, _ := parseDateParam(r.URL.Query().Get("from"))
	to, _ := parseDateParam(r.URL.Query().Get("to"))
	items, err := h.svc.ListCertificates(r.Context(), ac, CertificateFilter{
		From:          from,
		To:            to,
		EngineerID:    parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		ContractorID:  parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		ApplicationID: parseOptionalInt64(r.URL.Query().Get("application_id")),
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) exportCertificatesCSV(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	from, _ := parseDateParam(r.URL.Query().Get("from"))
	to, _ := parseDateParam(r.URL.Query().Get("to"))
	raw, err := h.svc.ExportCertificatesCSV(r.Context(), ac, CertificateFilter{
		From:       from,
		To:         to,
		EngineerID: parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		Limit:      200,
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="certificates.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h *Handler) getCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetCertificate(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Certificate
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveCertificate(r.Context(), ac, 0, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in Certificate
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveCertificate(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteCertificate(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	var status *TaskStatus
	if stRaw := r.URL.Query().Get("status"); stRaw != "" {
		if st, err := strconv.Atoi(stRaw); err == nil {
			sVal := TaskStatus(st)
			status = &sVal
		}
	}
	var taskType *TaskType
	if ttRaw := strings.TrimSpace(r.URL.Query().Get("task_type")); ttRaw != "" {
		ttVal := TaskType(ttRaw)
		taskType = &ttVal
	}
	items, err := h.svc.ListTasks(r.Context(), ac, TaskFilter{
		AssignedUserID: parseOptionalInt64(r.URL.Query().Get("assigned_user_id")),
		SubdivisionID:  parseOptionalInt64(r.URL.Query().Get("subdivision_id")),
		ContractorID:   parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		Status:         status,
		TaskType:       taskType,
		OnlyUrgent:     r.URL.Query().Get("urgent") == "true",
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetTask(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Task
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateTask(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in Task
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateTask(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) executeTask(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in ExecuteTaskInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.ExecuteTask(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteTask(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listPoolSettings(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListTaskPoolSettings(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) savePoolSetting(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in TaskPoolSetting
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveTaskPoolSetting(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) runTaskJobs(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.CreateQuarterlyCallTasks(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	p, err := h.svc.CreateProlongationTasks(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	a, err := h.svc.DistributeTaskPool(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	u, err := h.svc.CheckOverdueTasks(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]int{
		"quarterly_created":    q,
		"prolongation_created": p,
		"pool_assigned":        a,
		"overdue_flagged":      u,
	})
}

func (h *Handler) listCalls(w http.ResponseWriter, r *http.Request) {
	f, err := buildCallFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListCalls(r.Context(), f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getCurrentCall(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	items, err := h.svc.GetCurrentCall(r.Context(), ac)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) updateCallCard(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in UpdateCallCardInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateCallCard(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) getCallRecording(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, data, err := h.svc.GetCallRecordMP3(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) transcribeCall(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.TranscribeCall(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) transferCall(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		ExternalCallID string `json:"external_call_id"`
		TargetPhone    string `json:"target_phone"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.BlindTransfer(r.Context(), ac, in.ExternalCallID, in.TargetPhone); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "transferred"})
}

func (h *Handler) softphoneConfig(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	cfg, err := h.svc.GetSoftPhoneConfig(r.Context(), ac)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, cfg)
}

func (h *Handler) listSubdivisions(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSubdivisions(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createSubdivision(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in SubdivisionInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveSubdivisionFull(r.Context(), ac, 0, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateSubdivision(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in SubdivisionInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveSubdivisionFull(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listSipAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSipAccounts(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) saveSipAccount(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in SipAccount
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	action := "create"
	if in.ID != 0 {
		action = "update"
	}
	if err := requireCapability(ac, "crm.sip.manage", "crm.sip."+action); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}

	out, err := h.svc.SaveSipAccount(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) amountActsReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rows, err := h.svc.AmountActsReportFiltered(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) expiredTasksReport(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rows, err := h.svc.ExpiredTasksReportFiltered(r.Context(), from, to, parseOptionalInt64(r.URL.Query().Get("user_id")))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) listChangeLogs(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.ListChangeLogs(r.Context(), chi.URLParam(r, "entityType"), id, limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.ListNotifications(r.Context(), ac, r.URL.Query().Get("unread") == "true", limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.MarkNotificationRead(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	if err := h.svc.MarkAllNotificationsRead(r.Context(), ac); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) getUserSetting(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	val, err := h.svc.GetUserSetting(r.Context(), ac, chi.URLParam(r, "key"))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(val)
}

func (h *Handler) saveUserSetting(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var raw json.RawMessage
	if err := platform.DecodeJSON(r, &raw); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.SaveUserSetting(r.Context(), ac, chi.URLParam(r, "key"), raw); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) listEnums(w http.ResponseWriter, r *http.Request) {
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"work_types": StandardWorkTypes,
		"document_statuses": []map[string]any{
			{"id": StatusActive, "label": StatusActive.Label()},
			{"id": StatusProcessed, "label": StatusProcessed.Label()},
			{"id": StatusFailed, "label": StatusFailed.Label()},
			{"id": StatusCancelled, "label": StatusCancelled.Label()},
		},
		"task_types": []map[string]any{
			{"id": TaskTypeCall, "label": TaskTypeCall.Label()},
			{"id": TaskTypeQuarterlyCall, "label": TaskTypeQuarterlyCall.Label()},
			{"id": TaskTypeProlongation, "label": TaskTypeProlongation.Label()},
			{"id": TaskTypeNoAnswer, "label": TaskTypeNoAnswer.Label()},
			{"id": TaskTypeService, "label": TaskTypeService.Label()},
			{"id": TaskTypeAppLimit, "label": TaskTypeAppLimit.Label()},
			{"id": TaskTypeCallFeedback, "label": TaskTypeCallFeedback.Label()},
			{"id": TaskTypeOther, "label": TaskTypeOther.Label()},
		},
		"task_statuses": []map[string]any{
			{"id": TaskStatusNew, "label": TaskStatusNew.Label()},
			{"id": TaskStatusInProgress, "label": TaskStatusInProgress.Label()},
			{"id": TaskStatusDone, "label": TaskStatusDone.Label()},
			{"id": TaskStatusNoAnswer, "label": TaskStatusNoAnswer.Label()},
			{"id": TaskStatusFailed, "label": TaskStatusFailed.Label()},
			{"id": TaskStatusCancelled, "label": TaskStatusCancelled.Label()},
		},
		"contract_categories":  ExtendedContractCategories,
		"payment_options":      PaymentOptions,
		"task_results":         TaskResults,
		"call_types_of_appeal": ExtendedCallTypesOfAppeal,
		"caller_types":         CallerTypeOptions,
		"call_results":         CallResultOptions,
		"traffic_source_types": TrafficSourceTypes,
		"call_directions":      CallDirections,
		"call_statuses":        CallStatuses,
	})
}

func (h *Handler) listWorkers(w http.ResponseWriter, r *http.Request) {
	if h.svc.users == nil {
		platform.WriteJSON(w, http.StatusOK, []UserBrief{})
		return
	}
	isEng := r.URL.Query().Get("engineer") == "true"
	isDisp := r.URL.Query().Get("dispatcher") == "true"
	items, err := h.svc.users.ListActiveWorkers(r.Context(), isEng, isDisp, parseOptionalInt64(r.URL.Query().Get("subdivision_id")))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

// --- Legacy / Mobile API {data, errors} compatibility ---

func writeLegacyResult(w http.ResponseWriter, status int, data any, err error) {
	if err != nil {
		st := platform.HTTPStatusForError(err)
		platform.WriteJSON(w, st, platform.LegacyAPIEnvelope{
			Data:   nil,
			Errors: []string{err.Error()},
		})
		return
	}
	platform.WriteJSON(w, status, platform.LegacyAPIEnvelope{Data: data, Errors: nil})
}

func legacyModeRange(base time.Time, mode string) (time.Time, time.Time) {
	y, m, d := base.Date()
	loc := base.Location()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "day":
		return dayStart, dayStart.Add(24*time.Hour - time.Second)
	case "week":
		wd := (int(dayStart.Weekday()) + 6) % 7 // понедельник = 0
		wStart := dayStart.AddDate(0, 0, -wd)
		return wStart, wStart.AddDate(0, 0, 7).Add(-time.Second)
	case "month":
		mStart := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		return mStart, mStart.AddDate(0, 1, 0).Add(-time.Second)
	case "offline":
		return dayStart.AddDate(0, 0, -3), dayStart.AddDate(0, 0, 4).Add(-time.Second)
	default:
		return dayStart, dayStart.Add(24*time.Hour - time.Second)
	}
}

func (h *Handler) legacyApplications(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	f := ApplicationFilter{
		EngineerID: parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		Limit:      limit,
		Offset:     offset,
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	dateStr := strings.TrimSpace(r.URL.Query().Get("date"))
	if dateStr != "" || mode != "" {
		base := h.svc.now()
		if dateStr != "" {
			if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
				base = parsed
			} else if parsedRFC, err := time.Parse(time.RFC3339, dateStr); err == nil {
				base = parsedRFC
			}
		}
		if mode == "" {
			mode = "day"
		}
		from, to := legacyModeRange(base, mode)
		f.From = &from
		f.To = &to
	}
	items, err := h.svc.ListApplications(r.Context(), ac, f)
	if err != nil {
		writeLegacyResult(w, http.StatusOK, nil, err)
		return
	}
	if strings.EqualFold(mode, "offline") {
		type offlineApp struct {
			Application
			Contractor *Contractor `json:"contractor,omitempty"`
		}
		bundle := make([]offlineApp, 0, len(items))
		for _, app := range items {
			c, _ := h.svc.GetContractor(r.Context(), ac, app.ContractorID)
			bundle = append(bundle, offlineApp{Application: app, Contractor: c})
		}
		writeLegacyResult(w, http.StatusOK, bundle, nil)
		return
	}
	writeLegacyResult(w, http.StatusOK, items, nil)
}

func (h *Handler) legacyGetApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	out, err := h.svc.GetApplication(r.Context(), ac, id)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCreateApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Application
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.CreateApplication(r.Context(), ac, in)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyUpdateApplication(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in Application
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.UpdateApplication(r.Context(), ac, id, in)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCertificates(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	items, err := h.svc.ListCertificates(r.Context(), ac, CertificateFilter{
		EngineerID:    parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		ApplicationID: parseOptionalInt64(r.URL.Query().Get("application_id")),
		Limit:         limit,
		Offset:        offset,
	})
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyGetCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	out, err := h.svc.GetCertificate(r.Context(), ac, id)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCreateCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Certificate
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.SaveCertificate(r.Context(), ac, 0, in)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyUpdateCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in Certificate
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.SaveCertificate(r.Context(), ac, id, in)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyContracts(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r)
	items, err := h.svc.ListContracts(r.Context(), ContractFilter{
		ContractorID: parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		AddressID:    parseOptionalInt64(r.URL.Query().Get("address_id")),
		Limit:        limit,
		Offset:       offset,
	})
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyGetContract(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	out, err := h.svc.GetContract(r.Context(), id)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCreateContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in MaintenanceContract
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.CreateContract(r.Context(), ac, in)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyUpdateContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in MaintenanceContract
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.UpdateContract(r.Context(), ac, id, in)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyContractors(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, offset := parseLimitOffset(r)
	items, _, err := h.svc.SearchContractors(r.Context(), ac, r.URL.Query().Get("q"), limit, offset)
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyGetContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	out, err := h.svc.GetContractor(r.Context(), ac, id)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCreateContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in SaveContractorInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.CreateContractor(r.Context(), ac, in)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyUpdateContractor(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in SaveContractorInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.UpdateContractor(r.Context(), ac, id, in)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyCreateContractorAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	contractorID, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in ContractorAddress
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	in.ContractorID = contractorID
	out, err := h.svc.CreateAddress(r.Context(), ac, in)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyAddresses(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.SearchAddresses(r.Context(), parseOptionalInt64(r.URL.Query().Get("contractor_id")), r.URL.Query().Get("q"), limit)
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyEquipments(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.ListEquipments(r.Context(), r.URL.Query().Get("q"), limit)
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyCreateEquipment(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		Type         string `json:"type"`
		Model        string `json:"model"`
		Manufacturer string `json:"manufacturer"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	out, err := h.svc.CreateEquipment(r.Context(), ac, in.Type, in.Model, in.Manufacturer)
	writeLegacyResult(w, http.StatusCreated, out, err)
}

func (h *Handler) legacyEquipmentsByAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	addrID, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	items, err := h.svc.GetAddressEquipments(r.Context(), ac, addrID)
	writeLegacyResult(w, http.StatusOK, items, err)
}

func (h *Handler) legacyAttachEquipmentByAddress(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	addrID, err := parseIDParam(r, "id")
	if err != nil {
		writeLegacyResult(w, http.StatusBadRequest, nil, err)
		return
	}
	var in AddressEquipment
	if err := platform.DecodeJSON(r, &in); err != nil {
		writeLegacyResult(w, http.StatusUnprocessableEntity, nil, err)
		return
	}
	in.AddressID = addrID
	out, err := h.svc.AttachAddressEquipment(r.Context(), ac, in)
	writeLegacyResult(w, http.StatusOK, out, err)
}

func (h *Handler) legacyRefByKind(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := h.svc.ListRefItems(r.Context(), kind)
		writeLegacyResult(w, http.StatusOK, items, err)
	}
}

func (h *Handler) legacyEnumWorkTypes(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, StandardWorkTypes, nil)
}

func (h *Handler) legacyEnumStatuses(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, []map[string]any{
		{"id": StatusActive, "name": StatusActive.Label()},
		{"id": StatusProcessed, "name": StatusProcessed.Label()},
		{"id": StatusFailed, "name": StatusFailed.Label()},
		{"id": StatusCancelled, "name": StatusCancelled.Label()},
	}, nil)
}

func (h *Handler) legacyEnumContractCategories(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, ExtendedContractCategories, nil)
}

func (h *Handler) legacyEnumPaymentOptions(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, PaymentOptions, nil)
}

func (h *Handler) legacyEnumTaskResults(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, TaskResults, nil)
}

func (h *Handler) legacyEnumCallTypesOfAppeal(w http.ResponseWriter, r *http.Request) {
	writeLegacyResult(w, http.StatusOK, ExtendedCallTypesOfAppeal, nil)
}
