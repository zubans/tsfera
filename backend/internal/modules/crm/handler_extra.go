package crm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

func parseStringList(r *http.Request, keys ...string) []string {
	seen := map[string]bool{}
	var out []string
	q := r.URL.Query()
	for _, k := range keys {
		for _, raw := range q[k] {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" && !seen[part] {
					seen[part] = true
					out = append(out, part)
				}
			}
		}
	}
	return out
}

func parseIntList(r *http.Request, keys ...string) []int {
	seen := map[int]bool{}
	var out []int
	q := r.URL.Query()
	for _, k := range keys {
		for _, raw := range q[k] {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if v, err := strconv.Atoi(part); err == nil && v > 0 && !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func parseOptionalInt(raw string) *int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &v
}

func parseOptionalDateRange(r *http.Request) (*time.Time, *time.Time, error) {
	if dr := strings.TrimSpace(r.URL.Query().Get("range")); dr != "" {
		f, t, err := platform.ParseDateRange(dr)
		if err != nil {
			return nil, nil, err
		}
		return &f, &t, nil
	}
	from, err := parseDateParam(r.URL.Query().Get("from"))
	if err != nil {
		return nil, nil, err
	}
	to, err := parseDateParam(r.URL.Query().Get("to"))
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

func (h *Handler) parseReportFilter(r *http.Request, defaultDays int) (ReportFilter, error) {
	now := h.svc.now()
	from := now.AddDate(0, 0, -defaultDays)
	to := now
	f, t, err := parseOptionalDateRange(r)
	if err != nil {
		return ReportFilter{}, err
	}
	if f != nil {
		from = *f
	}
	if t != nil {
		to = *t
	}
	return ReportFilter{
		From:           from,
		To:             to,
		EngineerID:     parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		SubdivisionIDs: parseInt64List(r, "subdivision_id", "subdivision_ids"),
		UserIDs:        parseInt64List(r, "user_id", "user_ids"),
		PaymentMethods: parseStringList(r, "payment_method", "payment_methods"),
	}, nil
}

func buildContractFilter(r *http.Request) (ContractFilter, error) {
	limit, offset := parseLimitOffset(r)
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		return ContractFilter{}, err
	}
	var status *DocumentStatus
	if st := parseOptionalInt(r.URL.Query().Get("status")); st != nil {
		ds := DocumentStatus(*st)
		status = &ds
	}
	onlyProlong := r.URL.Query().Get("only_needing_prolongation") == "true" ||
		r.URL.Query().Get("only_needing_prolongation") == "1"
	return ContractFilter{
		Query:                   strings.TrimSpace(r.URL.Query().Get("q")),
		ContractorID:            parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		AddressID:               parseOptionalInt64(r.URL.Query().Get("address_id")),
		Status:                  status,
		From:                    from,
		To:                      to,
		OnlyNeedingProlongation: onlyProlong,
		Limit:                   limit,
		Offset:                  offset,
	}, nil
}

func buildApplicationFilter(r *http.Request) (ApplicationFilter, error) {
	limit, offset := parseLimitOffset(r)
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		return ApplicationFilter{}, err
	}
	return ApplicationFilter{
		From:          from,
		To:            to,
		EngineerID:    parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		SubdivisionID: parseOptionalInt64(r.URL.Query().Get("subdivision_id")),
		ContractorID:  parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		OrderBy:       strings.TrimSpace(r.URL.Query().Get("order_by")),
		Limit:         limit,
		Offset:        offset,
	}, nil
}

func buildCertificateFilter(r *http.Request) (CertificateFilter, error) {
	limit, offset := parseLimitOffset(r)
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		return CertificateFilter{}, err
	}
	return CertificateFilter{
		From:          from,
		To:            to,
		EngineerID:    parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		ContractorID:  parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		ApplicationID: parseOptionalInt64(r.URL.Query().Get("application_id")),
		Limit:         limit,
		Offset:        offset,
	}, nil
}

func buildCallFilter(r *http.Request) (CallFilter, error) {
	limit, offset := parseLimitOffset(r)
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		return CallFilter{}, err
	}
	return CallFilter{
		Phone:            strings.TrimSpace(r.URL.Query().Get("phone")),
		ContractorID:     parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		UserID:           parseOptionalInt64(r.URL.Query().Get("user_id")),
		SubdivisionID:    parseOptionalInt64(r.URL.Query().Get("subdivision_id")),
		Directions:       parseStringList(r, "direction", "directions"),
		Statuses:         parseStringList(r, "status", "statuses"),
		TypesOfAppeal:    parseStringList(r, "type_of_appeal", "types_of_appeal"),
		CallerTypes:      parseIntList(r, "caller_type", "caller_types"),
		CallResults:      parseIntList(r, "call_result", "call_results"),
		TrafficSourceIDs: parseInt64List(r, "traffic_source_id", "traffic_source_ids"),
		From:             from,
		To:               to,
		Limit:            limit,
		Offset:           offset,
	}, nil
}

func buildAnalyticFilter(r *http.Request) (AnalyticFilter, error) {
	limit, offset := parseLimitOffset(r)
	from, to, err := parseOptionalDateRange(r)
	if err != nil {
		return AnalyticFilter{}, err
	}
	return AnalyticFilter{
		From:          from,
		To:            to,
		CertificateID: parseOptionalInt64(r.URL.Query().Get("certificate_id")),
		ContractorID:  parseOptionalInt64(r.URL.Query().Get("contractor_id")),
		EngineerID:    parseOptionalInt64(r.URL.Query().Get("engineer_id")),
		Rating:        parseOptionalInt(r.URL.Query().Get("rating")),
		Status:        parseOptionalInt(r.URL.Query().Get("status")),
		Limit:         limit,
		Offset:        offset,
	}, nil
}

func writeExportPayload(w http.ResponseWriter, filename, contentType string, payload []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func exportFormatParam(r *http.Request) string {
	if f := strings.TrimSpace(r.URL.Query().Get("format")); f != "" {
		return f
	}
	return "xlsx"
}

// --- Источники трафика ---

func (h *Handler) listTrafficSources(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true" || r.URL.Query().Get("active") == "1"
	items, err := h.svc.ListTrafficSources(r.Context(), activeOnly)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createTrafficSource(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in TrafficSource
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateTrafficSource(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateTrafficSource(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in TrafficSource
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateTrafficSource(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteTrafficSource(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteTrafficSource(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

// --- Подразделения и SIP ---

func (h *Handler) deleteSubdivision(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteSubdivision(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listSipUsersSelect(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSipUsersSelect(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) legacySipUsersSelect(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListSipUsersSelect(r.Context())
	writeLegacyResult(w, http.StatusOK, items, err)
}

// --- Договоры ТО: продление и печать ---

func (h *Handler) renewContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in RenewContractInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.RenewContract(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) printContract(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	html, err := h.svc.RenderContractPrintHTML(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (h *Handler) printCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	html, err := h.svc.RenderCertificatePrintHTML(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

// --- Экспорт XLSX / CSV ---

func (h *Handler) exportApplications(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := buildApplicationFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportApplications(r.Context(), ac, f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

func (h *Handler) exportCertificates(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := buildCertificateFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportCertificates(r.Context(), ac, f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

func (h *Handler) exportContracts(w http.ResponseWriter, r *http.Request) {
	f, err := buildContractFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportContracts(r.Context(), f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

func (h *Handler) exportCalls(w http.ResponseWriter, r *http.Request) {
	f, err := buildCallFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportCalls(r.Context(), f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

func (h *Handler) exportAnalytics(w http.ResponseWriter, r *http.Request) {
	f, err := buildAnalyticFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportAnalytics(r.Context(), f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

func (h *Handler) exportContractorsReport(w http.ResponseWriter, r *http.Request) {
	f, err := h.parseReportFilter(r, 365)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ct, name, body, err := h.svc.ExportContractorsReport(r.Context(), f, exportFormatParam(r))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	writeExportPayload(w, name, ct, body)
}

// --- Звонки и пул пропущенных звонков ---

func (h *Handler) getCallCard(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetCallCard(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listMissCallPool(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	onlyUnviewed := r.URL.Query().Get("only_unviewed") != "false"
	limit, _ := parseLimitOffset(r)
	items, err := h.svc.ListMissCallPool(r.Context(), ac, onlyUnviewed, limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) markMissCallViewed(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	callID, err := parseIDParam(r, "callId")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.MarkMissCallViewed(r.Context(), ac, callID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- DaData и CloudPayments ---

func (h *Handler) suggestAddresses(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	items, err := h.svc.SuggestAddressesDaData(r.Context(), r.URL.Query().Get("query"), count)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) cloudPaymentsWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Не удалось прочитать тело запроса", nil))
		return
	}
	hmacHeader := r.Header.Get("Content-HMAC")
	if hmacHeader == "" {
		hmacHeader = r.Header.Get("X-Content-HMAC")
	}
	kind := chi.URLParam(r, "kind")
	if err := h.svc.HandleCloudPaymentsWebhook(r.Context(), kind, raw, hmacHeader); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]int{"code": 0})
}

func (h *Handler) getContractorSubscriptions(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetContractorSubscriptions(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) checkAddressSubscription(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CheckAddressSubscription(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

// --- Аналитика / Опросы контроля качества ---

func (h *Handler) listAnalyticQuestions(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true" || r.URL.Query().Get("active") == "1"
	items, err := h.svc.ListAnalyticQuestions(r.Context(), activeOnly)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createAnalyticQuestion(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in AnalyticQuestion
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateAnalyticQuestion(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateAnalyticQuestion(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in AnalyticQuestion
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateAnalyticQuestion(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listAnalytics(w http.ResponseWriter, r *http.Request) {
	f, err := buildAnalyticFilter(r)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListAnalytics(r.Context(), f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getAnalytic(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.GetAnalytic(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) saveAnalyticByCertificate(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	certID, err := parseIDParam(r, "certificateId")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in SaveAnalyticInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SaveAnalyticByCertificate(r.Context(), ac, certID, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) setAnalyticRating(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Rating     int    `json:"rating"`
		Comment    string `json:"comment"`
		CreateTask bool   `json:"create_task"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.SetAnalyticRating(r.Context(), ac, id, in.Rating, in.Comment, in.CreateTask)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

// --- Баннеры и настройки приложения ---

func (h *Handler) listBanners(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true" || r.URL.Query().Get("active") == "1"
	items, err := h.svc.ListBanners(r.Context(), activeOnly)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createBanner(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in Banner
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CreateBanner(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateBanner(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in Banner
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.UpdateBanner(r.Context(), ac, id, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteBanner(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseIDParam(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteBanner(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) listAppSettings(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListAppSettings(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) getAppSetting(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetAppSetting(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) saveAppSetting(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	items, err := h.svc.ListAppSettings(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	action := "create"
	for _, item := range items {
		if item.Key == chi.URLParam(r, "key") {
			action = "update"
		}
	}
	if err := requireCapability(ac, "crm.setting.manage", "crm.setting."+action); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}

	out, err := h.svc.SaveAppSetting(r.Context(), ac, chi.URLParam(r, "key"), in.Name, in.Data)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) getBannersForEngineer(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetBannersForEngineerResolved(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

// --- Расширенные отчёты ---

func (h *Handler) subdivisionSumsReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rows, err := h.svc.SubdivisionSumsReport(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) amountSparePartsReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.AmountSparePartsReport(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) callConversionReport(w http.ResponseWriter, r *http.Request) {
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.CallConversionReport(r.Context(), f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) contractorsReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := h.parseReportFilter(r, 365)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rows, err := h.svc.ContractorsReport(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) engineersReport(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	rows, err := h.svc.EngineersReport(r.Context(), ac, f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) trafficSourcesReport(w http.ResponseWriter, r *http.Request) {
	f, err := h.parseReportFilter(r, 30)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.TrafficSourcesReport(r.Context(), f)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}
