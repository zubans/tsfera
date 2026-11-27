package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

const (
	maxHistoryApplicationsInContext = 10
	maxContractsInContext           = 10
	maxCertsPerContractorInContext  = 30
)

// buildApplicationContextMarkdown собирает структурированный контекст заявки для ИИ-модели:
// все поля заявки, контакты и реквизиты объекта, полный перечень оборудования заявки (и прочего на адресе),
// связанные акты (с замерами телеметрии), договоры ТО и историю предыдущих заявок контрагента.
// Соблюдает объектное правило доступа ±3 дня (ADR-002): вне окна ±3 дня инженер без полного доступа
// не видит актуальные телефоны/договоры контрагента и чужие заявки других инженеров.
func (s *Service) buildApplicationContextMarkdown(ctx context.Context, auth *platform.AuthContext, app *Application) string {
	canViewFullContractor := auth == nil
	if auth != nil && app.ContractorID > 0 {
		if ok, err := s.CanViewContractor(ctx, auth, app.ContractorID); err == nil && ok {
			canViewFullContractor = true
		}
	}

	var contractor *Contractor
	if app.ContractorID > 0 {
		if c, err := s.repo.GetContractor(ctx, app.ContractorID); err == nil {
			contractor = c
		}
	}

	var certs []Certificate
	var prevApps []Application
	var contracts []MaintenanceContract
	if app.ContractorID > 0 {
		cid := app.ContractorID
		appFilter := ApplicationFilter{
			ContractorID: &cid,
			Limit:        maxHistoryApplicationsInContext + 5,
		}
		certFilter := CertificateFilter{
			ContractorID: &cid,
			Limit:        maxCertsPerContractorInContext,
		}
		if !canViewFullContractor && auth != nil {
			uid := auth.UserID
			appFilter.EngineerID = &uid
			certFilter.EngineerID = &uid
		}
		if list, err := s.repo.ListApplications(ctx, appFilter); err == nil {
			s.enrichApplicationUsers(ctx, list)
			prevApps = list
		}
		if list, err := s.repo.ListCertificates(ctx, certFilter); err == nil {
			certs = list
		}
		// Акты текущей заявки всегда доступны пользователю, имеющему доступ к самой заявке.
		if !canViewFullContractor {
			appID := app.ID
			if ownAppCerts, err := s.repo.ListCertificates(ctx, CertificateFilter{ApplicationID: &appID, Limit: 10}); err == nil {
				seenCert := map[int64]bool{}
				for _, c := range certs {
					seenCert[c.ID] = true
				}
				for _, c := range ownAppCerts {
					if !seenCert[c.ID] {
						certs = append(certs, c)
					}
				}
			}
		}
		if canViewFullContractor {
			if list, err := s.repo.ListContracts(ctx, ContractFilter{
				ContractorID: &cid,
				Limit:        maxContractsInContext,
			}); err == nil {
				contracts = list
			}
		}
	}

	var b strings.Builder

	// Заголовок.
	if app.Number != nil && *app.Number != app.ID {
		fmt.Fprintf(&b, "# Заявка #%d (№%d, %s)\n\n", app.ID, *app.Number, app.Status.Label())
	} else {
		fmt.Fprintf(&b, "# Заявка #%d (%s)\n\n", app.ID, app.Status.Label())
	}

	// 1. Общие сведения.
	b.WriteString("## 1. Общие сведения\n")
	flags := []string{fmt.Sprintf("выполнена: %s", boolRU(app.IsComplete))}
	if app.Incomplete {
		flags = append(flags, "неполная")
	}
	if app.IsDuty {
		flags = append(flags, "дежурная")
	}
	if app.WorkflowStatus != nil {
		flags = append(flags, fmt.Sprintf("этап: %d", *app.WorkflowStatus))
	}
	fmt.Fprintf(&b, "- **Статус:** %s (%s)\n", app.Status.Label(), strings.Join(flags, ", "))
	if !app.PlanDatetimeFrom.IsZero() {
		planStr := app.PlanDatetimeFrom.Format("02.01.2006 15:04")
		if !app.PlanDatetimeTo.IsZero() {
			if app.PlanDatetimeFrom.Format("02.01.2006") == app.PlanDatetimeTo.Format("02.01.2006") {
				planStr += " — " + app.PlanDatetimeTo.Format("15:04")
			} else {
				planStr += " — " + app.PlanDatetimeTo.Format("02.01.2006 15:04")
			}
		}
		if app.WorkWindowFrom != nil && app.WorkWindowTo != nil {
			planStr += fmt.Sprintf(" | Окно прибытия: %s — %s", app.WorkWindowFrom.Format("15:04"), app.WorkWindowTo.Format("15:04"))
		}
		fmt.Fprintf(&b, "- **Плановое время:** %s\n", planStr)
	}
	if app.EngineerName != "" || app.EngineerID > 0 {
		eng := strings.TrimSpace(app.EngineerName)
		if eng == "" {
			eng = fmt.Sprintf("ID %d", app.EngineerID)
		} else if app.EngineerID > 0 {
			eng = fmt.Sprintf("%s (#%d)", eng, app.EngineerID)
		}
		if len(app.ExecutorNames) > 0 {
			eng += " | Соисполнители: " + strings.Join(app.ExecutorNames, ", ")
		}
		fmt.Fprintf(&b, "- **Инженер:** %s\n", eng)
	}
	if !app.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "- **Создана / обновлена:** %s / %s\n",
			app.CreatedAt.Format("02.01.2006 15:04"), app.UpdatedAt.Format("02.01.2006 15:04"))
	}
	b.WriteString("\n")

	// 2. Контрагент и объект.
	b.WriteString("## 2. Контрагент и объект\n")
	contractorType := ""
	if contractor != nil {
		if contractor.IsLegalEntity {
			contractorType = ", юрлицо"
		} else {
			contractorType = ", физлицо"
		}
	}
	fmt.Fprintf(&b, "- **Контрагент:** %s (#%d%s)\n", fallbackStr(app.ContractorName, "—"), app.ContractorID, contractorType)
	if app.AddressID > 0 {
		fmt.Fprintf(&b, "- **Адрес объекта:** %s (#%d)\n", fallbackStr(app.AddressText, "—"), app.AddressID)
	} else {
		fmt.Fprintf(&b, "- **Адрес объекта:** %s\n", fallbackStr(app.AddressText, "—"))
	}
	if canViewFullContractor {
		phones := formatApplicationPhones(app, contractor)
		if len(phones) > 0 {
			fmt.Fprintf(&b, "- **Телефоны:** %s\n", strings.Join(phones, "; "))
		}
		if contractor != nil && strings.TrimSpace(contractor.Note) != "" {
			fmt.Fprintf(&b, "- **Примечание контрагента:** %s\n", strings.TrimSpace(contractor.Note))
		}
	}
	b.WriteString("\n")

	// 3. Оборудование в заявке и на адресе.
	var addrEquipments []AddressEquipment
	if contractor != nil {
		for _, a := range contractor.Addresses {
			if a.ID == app.AddressID {
				addrEquipments = a.Equipments
				break
			}
		}
	}
	addrEqByEquipmentID := map[int64][]AddressEquipment{}
	for _, ae := range addrEquipments {
		addrEqByEquipmentID[ae.EquipmentID] = append(addrEqByEquipmentID[ae.EquipmentID], ae)
	}

	// Точечно догружаем названия из справочника только для тех ID заявки, которых нет на адресе.
	var missingEqIDs []int64
	for _, eqID := range app.EquipmentIDs {
		if len(addrEqByEquipmentID[eqID]) == 0 {
			missingEqIDs = append(missingEqIDs, eqID)
		}
	}
	eqCatalog := map[int64]Equipment{}
	if len(missingEqIDs) > 0 {
		if list, err := s.repo.GetEquipmentsByIDs(ctx, missingEqIDs); err == nil {
			for _, eq := range list {
				eqCatalog[eq.ID] = eq
			}
		}
	}

	b.WriteString("## 3. Оборудование в заявке\n")
	usedAddrEqIDs := map[int64]bool{}
	if len(app.EquipmentIDs) == 0 {
		b.WriteString("- В самой заявке конкретные позиции оборудования не указаны.\n")
	} else {
		fmt.Fprintf(&b, "Всего в заявке позиций: **%d**\n", len(app.EquipmentIDs))
		consumedPerEqID := map[int64]int{}
		for i, eqID := range app.EquipmentIDs {
			var name, sn, note string
			if i < len(app.SerialNumbers) {
				sn = strings.TrimSpace(app.SerialNumbers[i])
			}
			matches := addrEqByEquipmentID[eqID]
			if len(matches) > 0 {
				pickIdx := consumedPerEqID[eqID]
				if pickIdx >= len(matches) {
					pickIdx = len(matches) - 1
				}
				consumedPerEqID[eqID]++
				ae := matches[pickIdx]
				usedAddrEqIDs[ae.ID] = true
				name = strings.TrimSpace(ae.EquipmentName)
				if sn == "" {
					sn = strings.TrimSpace(ae.SerialNumber)
				}
				note = strings.TrimSpace(ae.Note)
			}
			if name == "" {
				if eq, ok := eqCatalog[eqID]; ok {
					name = strings.TrimSpace(eq.Name)
				}
			}
			if name == "" {
				name = fmt.Sprintf("Оборудование #%d", eqID)
			}
			fmt.Fprintf(&b, "%d. **%s** (equipment_id: %d)", i+1, name, eqID)
			if sn != "" {
				fmt.Fprintf(&b, " — S/N: `%s`", sn)
			}
			if note != "" {
				fmt.Fprintf(&b, " | Примечание: %s", note)
			}
			b.WriteString("\n")
		}
	}

	if canViewFullContractor {
		var otherAddrEq []AddressEquipment
		for _, ae := range addrEquipments {
			if !usedAddrEqIDs[ae.ID] {
				otherAddrEq = append(otherAddrEq, ae)
			}
		}
		if len(otherAddrEq) > 0 {
			b.WriteString("\n**Прочее оборудование на адресе (не привязано к заявке):**\n")
			for _, ae := range otherAddrEq {
				fmt.Fprintf(&b, "- **%s** (equipment_id: %d)", fallbackStr(ae.EquipmentName, fmt.Sprintf("Оборудование #%d", ae.EquipmentID)), ae.EquipmentID)
				if sn := strings.TrimSpace(ae.SerialNumber); sn != "" {
					fmt.Fprintf(&b, " — S/N: `%s`", sn)
				}
				if note := strings.TrimSpace(ae.Note); note != "" {
					fmt.Fprintf(&b, " | Примечание: %s", note)
				}
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n")

	// 4. Работы, финансы и комментарии.
	b.WriteString("## 4. Работы, финансы и комментарии\n")
	fmt.Fprintf(&b, "- **Виды работ:** %s\n", fallbackStr(strings.Join(app.WorkTypes, ", "), "не указаны"))
	fmt.Fprintf(&b, "- **Плановая сумма:** %.2f руб. | **Сумма по актам:** %.2f руб.\n", app.PlannedSum, app.ActsTotalAmount)
	if s := strings.TrimSpace(app.PriceNote); s != "" {
		fmt.Fprintf(&b, "- **Примечание по цене:** %s\n", s)
	}
	fmt.Fprintf(&b, "- **Комментарий:** %s\n", fallbackStr(strings.TrimSpace(app.Comment), "нет"))
	if s := strings.TrimSpace(app.SpecialNotes); s != "" {
		fmt.Fprintf(&b, "- **Особые отметки:** %s (просмотрены: %s)\n", s, boolRU(app.NotesViewed))
	}
	if app.Rating != nil || strings.TrimSpace(app.RatingComment) != "" {
		ratingStr := "—"
		if app.Rating != nil {
			ratingStr = fmt.Sprintf("%d/5", *app.Rating)
		}
		if rc := strings.TrimSpace(app.RatingComment); rc != "" {
			ratingStr += " — " + rc
		}
		fmt.Fprintf(&b, "- **Оценка клиента:** %s\n", ratingStr)
	}
	if app.AnalyticRating != nil {
		fmt.Fprintf(&b, "- **Оценка контроля качества (ОКК):** %d/5\n", *app.AnalyticRating)
	}
	b.WriteString("\n")

	// Группируем акты по application_id.
	var appCerts []Certificate
	certsByAppID := map[int64][]Certificate{}
	for _, c := range certs {
		if c.ApplicationID != nil {
			if *c.ApplicationID == app.ID {
				appCerts = append(appCerts, c)
			} else {
				certsByAppID[*c.ApplicationID] = append(certsByAppID[*c.ApplicationID], c)
			}
		}
	}

	// 5. Акты выполненных работ по текущей заявке.
	if len(appCerts) > 0 {
		fmt.Fprintf(&b, "## 5. Акты выполненных работ по заявке #%d (%d)\n", app.ID, len(appCerts))
		for _, c := range appCerts {
			writeCertificateSummaryLine(&b, c)
		}
		b.WriteString("\n")
	}

	// 6. Договоры ТО контрагента.
	if len(contracts) > 0 {
		fmt.Fprintf(&b, "## 6. Договоры ТО контрагента (%d)\n", len(contracts))
		for _, c := range contracts {
			fmt.Fprintf(&b, "- **Договор №%s** (%s): %s — %s, %.2f руб.",
				fallbackStr(c.Number, fmt.Sprintf("#%d", c.ID)),
				c.Status.Label(),
				c.ValidFrom.Format("02.01.2006"),
				c.ValidTo.Format("02.01.2006"),
				c.Price,
			)
			if c.AddressID != app.AddressID && strings.TrimSpace(c.AddressText) != "" {
				fmt.Fprintf(&b, " | Адрес: %s", strings.TrimSpace(c.AddressText))
			}
			if note := strings.TrimSpace(c.Note); note != "" {
				fmt.Fprintf(&b, " | Примечание: %s", note)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// 7. История предыдущих заявок по контрагенту.
	var history []Application
	for _, a := range prevApps {
		if a.ID == app.ID {
			continue
		}
		history = append(history, a)
	}
	sort.Slice(history, func(i, j int) bool {
		if !history[i].PlanDatetimeFrom.Equal(history[j].PlanDatetimeFrom) {
			return history[i].PlanDatetimeFrom.After(history[j].PlanDatetimeFrom)
		}
		return history[i].ID > history[j].ID
	})
	if len(history) > maxHistoryApplicationsInContext {
		history = history[:maxHistoryApplicationsInContext]
	}

	b.WriteString("## 7. История предыдущих заявок контрагента\n")
	if len(history) == 0 {
		if !canViewFullContractor {
			b.WriteString("- Предыдущих заявок этого инженера по данному контрагенту нет (полная история контрагента скрыта вне окна ±3 дней).\n")
		} else {
			b.WriteString("- Предыдущих заявок по данному контрагенту нет.\n")
		}
	} else {
		fmt.Fprintf(&b, "Показаны последние %d заявок:\n", len(history))
		for _, h := range history {
			dateStr := h.PlanDatetimeFrom.Format("02.01.2006")
			if h.PlanDatetimeFrom.IsZero() {
				dateStr = h.CreatedAt.Format("02.01.2006")
			}
			fmt.Fprintf(&b, "- **#%d** от %s (%s", h.ID, dateStr, h.Status.Label())
			if h.IsComplete {
				b.WriteString(", выполнена")
			}
			if eng := strings.TrimSpace(h.EngineerName); eng != "" {
				fmt.Fprintf(&b, ", инж. %s", eng)
			}
			b.WriteString(")")
			if len(h.WorkTypes) > 0 {
				fmt.Fprintf(&b, " — %s", strings.Join(h.WorkTypes, ", "))
			}
			if h.AddressID != app.AddressID && strings.TrimSpace(h.AddressText) != "" {
				fmt.Fprintf(&b, " | Адрес: %s", strings.TrimSpace(h.AddressText))
			}
			if h.ActsTotalAmount > 0 {
				fmt.Fprintf(&b, " | Сумма актов: %.2f руб.", h.ActsTotalAmount)
			} else if h.PlannedSum > 0 {
				fmt.Fprintf(&b, " | План: %.2f руб.", h.PlannedSum)
			}
			if cmt := strings.TrimSpace(h.Comment); cmt != "" {
				fmt.Fprintf(&b, " | Комментарий: %s", truncateContextStr(cmt, 180))
			}
			if sn := strings.TrimSpace(h.SpecialNotes); sn != "" {
				fmt.Fprintf(&b, " | Особые отметки: %s", truncateContextStr(sn, 140))
			}
			b.WriteString("\n")
			for _, hc := range certsByAppID[h.ID] {
				b.WriteString("  - ")
				writeCertificateBrief(&b, hc)
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func formatApplicationPhones(app *Application, contractor *Contractor) []string {
	appPhoneSet := map[int64]bool{}
	for _, pid := range app.PhoneIDs {
		appPhoneSet[pid] = true
	}
	seenDigits := map[string]bool{}
	var phones []string
	if contractor != nil {
		for _, p := range contractor.Phones {
			formatted := platform.FormatPhoneRU(p.Phone)
			seenDigits[p.Phone] = true
			seenDigits[formatted] = true
			entry := formatted
			if note := strings.TrimSpace(p.Note); note != "" {
				entry += " (" + note + ")"
			}
			if appPhoneSet[p.ID] {
				entry += " [указан в заявке]"
			}
			phones = append(phones, entry)
		}
	}
	// Дополняем неразрешёнными телефонами напрямую из source_relations заявки (application_phones), если они не вошли в карточку контрагента.
	if len(app.SourceRelations) > 0 {
		var rel struct {
			AppPhones []struct {
				Phone string `json:"phone"`
			} `json:"application_phones"`
		}
		if json.Unmarshal(app.SourceRelations, &rel) == nil {
			for _, ap := range rel.AppPhones {
				raw := strings.TrimSpace(ap.Phone)
				if raw == "" {
					continue
				}
				formatted := platform.FormatPhoneRU(raw)
				if seenDigits[raw] || seenDigits[formatted] {
					continue
				}
				seenDigits[raw] = true
				seenDigits[formatted] = true
				phones = append(phones, formatted+" [указан в заявке]")
			}
		}
	}
	return phones
}

func writeCertificateSummaryLine(b *strings.Builder, c Certificate) {
	b.WriteString("- ")
	writeCertificateBrief(b, c)
	b.WriteString("\n")
}

func writeCertificateBrief(b *strings.Builder, c Certificate) {
	fmt.Fprintf(b, "**Акт #%d** от %s: %.2f руб. (оплачено %.2f руб.%s)",
		c.ID,
		c.WorkDate.Format("02.01.2006"),
		c.TotalAmount,
		c.PaidAmount,
		formatPaymentSuffix(c.PaymentMethod),
	)
	if c.CostOfParts > 0 {
		fmt.Fprintf(b, " | Стоимость запчастей: %.2f руб.", c.CostOfParts)
	}
	if len(c.WorkTypes) > 0 {
		fmt.Fprintf(b, " | Работы: %s", strings.Join(c.WorkTypes, ", "))
	}
	if len(c.SpareParts) > 0 {
		parts := make([]string, 0, len(c.SpareParts))
		for _, sp := range c.SpareParts {
			if strings.TrimSpace(sp.Name) != "" {
				parts = append(parts, fmt.Sprintf("%s (%.0f шт.)", strings.TrimSpace(sp.Name), sp.Quantity))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(b, " | Запчасти: %s", strings.Join(parts, ", "))
		}
	}
	if telem := formatCertificateTelemetry(c.Results); telem != "" {
		fmt.Fprintf(b, " | Замеры: %s", telem)
	}
	if cmt := strings.TrimSpace(c.Comment); cmt != "" {
		fmt.Fprintf(b, " | Комментарий: %s", truncateContextStr(cmt, 160))
	}
	if rec := strings.TrimSpace(c.Recommendation); rec != "" {
		fmt.Fprintf(b, " | Рекомендации: %s", truncateContextStr(rec, 200))
	}
	if sn := strings.TrimSpace(c.SpecialNotes); sn != "" {
		fmt.Fprintf(b, " | Особые отметки: %s", truncateContextStr(sn, 140))
	}
	if on := strings.TrimSpace(c.OtherNote); on != "" {
		fmt.Fprintf(b, " | Прочее: %s", truncateContextStr(on, 120))
	}
}

func formatCertificateTelemetry(t CertificateTelemetry) string {
	var items []string
	add := func(label, val string) {
		if v := strings.TrimSpace(val); v != "" {
			items = append(items, label+": "+v)
		}
	}
	add("разрежение", t.SmokeChannelVacuum)
	add("газ вх.", t.GasPressureInPipeline)
	add("газ горелка", t.BurnerGasPressure)
	add("раб. давл.", t.WorkingPressure)
	add("РБ отопл.", t.HeatingExpansionTankPressure)
	add("РБ ГВС", t.DHWExpansionTankPressure)
	add("расход ГВС", t.DHWFlow)
	add("част. мощн.", t.PartialHeatSetting)
	add("сеть", t.Voltage)
	add("теплоноситель", t.TypeOfCoolant)
	add("парам. теплонос.", t.CoolantParameters)
	return strings.Join(items, ", ")
}

func formatPaymentSuffix(method string) string {
	m := strings.TrimSpace(method)
	if m == "" {
		return ""
	}
	return ", " + m
}

func boolRU(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

func fallbackStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

func truncateContextStr(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}
