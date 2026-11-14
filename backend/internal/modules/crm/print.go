package crm

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// RenderContractPrintHTML формирует печатную HTML-форму договора ТО (аналог maintenance-contract/print.php).
func (s *Service) RenderContractPrintHTML(ctx context.Context, auth *platform.AuthContext, id int64) (string, error) {
	c, err := s.repo.GetContract(ctx, id)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", platform.NewNotFound("Договор ТО не найден")
	}
	contractor, err := s.GetContractor(ctx, auth, c.ContractorID)
	if err != nil {
		return "", err
	}

	var eqRows strings.Builder
	for i, eq := range c.Equipments {
		eqRows.WriteString(fmt.Sprintf(
			"<tr><td>%d</td><td>%s</td><td>%s</td></tr>",
			i+1,
			html.EscapeString(eq.EquipmentName),
			html.EscapeString(eq.SerialNumber),
		))
	}
	if eqRows.Len() == 0 {
		eqRows.WriteString(`<tr><td colspan="3">Оборудование не указано</td></tr>`)
	}

	legalBlock := ""
	if contractor.LegalEntity != nil && (contractor.IsLegalEntity || contractor.Type == ContractorLegal) {
		le := contractor.LegalEntity
		legalBlock = fmt.Sprintf(`
			<div class="box">
				<h3>Реквизиты юридического лица</h3>
				<p><strong>Наименование:</strong> %s</p>
				<p><strong>Руководитель:</strong> %s</p>
				<p><strong>ИНН / КПП:</strong> %s / %s | <strong>ОГРН:</strong> %s</p>
				<p><strong>Юр. адрес:</strong> %s</p>
				<p><strong>Банк / БИК:</strong> %s / %s</p>
				<p><strong>Р/с:</strong> %s | <strong>К/с:</strong> %s</p>
			</div>`,
			html.EscapeString(le.Name),
			html.EscapeString(le.DirectorName),
			html.EscapeString(le.INN), html.EscapeString(le.KPP), html.EscapeString(le.OGRN),
			html.EscapeString(le.LegalAddress),
			html.EscapeString(le.BankName), html.EscapeString(le.BIK),
			html.EscapeString(le.CheckingAccount), html.EscapeString(le.CorrespondingAccount),
		)
	}

	phones := make([]string, 0, len(contractor.Phones))
	for _, ph := range contractor.Phones {
		phones = append(phones, platform.FormatPhoneRU(ph.Phone))
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8" />
<title>Договор ТО №%s</title>
<style>
  body { font-family: "Times New Roman", serif; max-width: 820px; margin: 24px auto; color: #111; line-height: 1.45; }
  h1, h2, h3 { margin: 8px 0; }
  .center { text-align: center; }
  .meta { display: flex; justify-content: space-between; margin: 16px 0; }
  .box { border: 1px solid #cbd5e1; padding: 12px; margin: 12px 0; }
  table { width: 100%%; border-collapse: collapse; margin: 12px 0; }
  th, td { border: 1px solid #333; padding: 6px 8px; text-align: left; }
  .signs { display: flex; justify-content: space-between; margin-top: 36px; }
  @media print { .no-print { display: none; } body { margin: 0; } }
</style>
</head>
<body>
  <div class="no-print" style="margin-bottom: 16px;">
    <button onclick="window.print()">Печать</button>
  </div>
  <h2 class="center">ДОГОВОР ТЕХНИЧЕСКОГО ОБСЛУЖИВАНИЯ № %s</h2>
  <div class="meta">
    <span><strong>Дата заключения:</strong> %s</span>
    <span><strong>Срок действия:</strong> %s — %s</span>
  </div>
  <div class="box">
    <p><strong>Заказчик:</strong> %s</p>
    <p><strong>Телефоны:</strong> %s</p>
    <p><strong>Адрес объекта:</strong> %s</p>
    <p><strong>Тариф / категория:</strong> %s</p>
    <p><strong>Стоимость договора:</strong> %.2f ₽</p>
  </div>
  %s
  <h3>Обслуживаемое оборудование</h3>
  <table>
    <thead><tr><th>№</th><th>Оборудование</th><th>Серийный номер</th></tr></thead>
    <tbody>%s</tbody>
  </table>
  <p><strong>Примечание:</strong> %s</p>
  <div class="signs">
    <div>Исполнитель: ______________________</div>
    <div>Заказчик: ______________________</div>
  </div>
</body>
</html>`,
		html.EscapeString(c.Number),
		html.EscapeString(c.Number),
		c.SignedAt.Format("02.01.2006"),
		c.ValidFrom.Format("02.01.2006"),
		c.ValidTo.Format("02.01.2006"),
		html.EscapeString(contractor.Name),
		html.EscapeString(strings.Join(phones, ", ")),
		html.EscapeString(c.AddressText),
		html.EscapeString(ContractCategoryLabel(c.Category)),
		c.Price,
		legalBlock,
		eqRows.String(),
		html.EscapeString(c.Note),
	), nil
}

// RenderCertificatePrintHTML формирует печатную HTML-форму акта выполненных работ (физлица и юрлица).
func (s *Service) RenderCertificatePrintHTML(ctx context.Context, auth *platform.AuthContext, id int64) (string, error) {
	cert, err := s.GetCertificate(ctx, auth, id)
	if err != nil {
		return "", err
	}
	contractor, err := s.GetContractor(ctx, auth, cert.ContractorID)
	if err != nil {
		return "", err
	}

	engName := cert.EngineerName
	if engName == "" && s.users != nil {
		if u, _ := s.users.GetUserBrief(ctx, cert.EngineerID); u != nil {
			engName = u.FullName
		}
	}

	var execNames []string
	if len(cert.Executors) > 0 {
		for _, ex := range cert.Executors {
			n := ex.FullName
			if n == "" && s.users != nil {
				if u, _ := s.users.GetUserBrief(ctx, ex.UserID); u != nil {
					n = u.FullName
				}
			}
			if n == "" {
				n = fmt.Sprintf("Инженер #%d", ex.UserID)
			}
			execNames = append(execNames, fmt.Sprintf("%s (%.2f ₽)", n, ex.Sum))
		}
	} else if engName != "" {
		execNames = append(execNames, engName)
	}

	var spRows strings.Builder
	for i, sp := range cert.SpareParts {
		spRows.WriteString(fmt.Sprintf(
			"<tr><td>%d</td><td>%s</td><td>%.2f</td><td>%.2f ₽</td><td>%.2f ₽</td></tr>",
			i+1, html.EscapeString(sp.Name), sp.Quantity, sp.Price, sp.Quantity*sp.Price,
		))
	}
	if spRows.Len() == 0 {
		spRows.WriteString(`<tr><td colspan="5">Запчасти не использовались</td></tr>`)
	}

	legalBlock := ""
	if contractor.LegalEntity != nil && (contractor.IsLegalEntity || contractor.Type == ContractorLegal) {
		le := contractor.LegalEntity
		legalBlock = fmt.Sprintf(`
			<div class="box">
				<p><strong>Юр. лицо:</strong> %s (ИНН %s / КПП %s)</p>
				<p><strong>Юр. адрес:</strong> %s</p>
				<p><strong>Банк / БИК / Р/с:</strong> %s / %s / %s</p>
			</div>`,
			html.EscapeString(le.Name), html.EscapeString(le.INN), html.EscapeString(le.KPP),
			html.EscapeString(le.LegalAddress),
			html.EscapeString(le.BankName), html.EscapeString(le.BIK), html.EscapeString(le.CheckingAccount),
		)
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8" />
<title>Акт выполненных работ №%d</title>
<style>
  body { font-family: Arial, sans-serif; max-width: 840px; margin: 24px auto; color: #111; line-height: 1.4; }
  .center { text-align: center; }
  .box { border: 1px solid #cbd5e1; padding: 10px 12px; margin: 10px 0; }
  table { width: 100%%; border-collapse: collapse; margin: 12px 0; }
  th, td { border: 1px solid #333; padding: 6px 8px; text-align: left; }
  .signs { display: flex; justify-content: space-between; margin-top: 32px; }
  @media print { .no-print { display: none; } body { margin: 0; } }
</style>
</head>
<body>
  <div class="no-print" style="margin-bottom: 14px;">
    <button onclick="window.print()">Печать</button>
  </div>
  <h2 class="center">АКТ ВЫПОЛНЕННЫХ РАБОТ № %d</h2>
  <p><strong>Дата работ:</strong> %s | <strong>Квитанция:</strong> %s</p>
  <div class="box">
    <p><strong>Заказчик:</strong> %s</p>
    <p><strong>Адрес объекта:</strong> %s</p>
    <p><strong>Исполнитель(и):</strong> %s</p>
    <p><strong>Виды работ:</strong> %s</p>
  </div>
  %s
  <h3>Использованные запчасти и материалы</h3>
  <table>
    <thead><tr><th>№</th><th>Наименование</th><th>Кол-во</th><th>Цена</th><th>Сумма</th></tr></thead>
    <tbody>%s</tbody>
  </table>
  <div class="box">
    <p><strong>Итого к оплате:</strong> %.2f ₽ | <strong>Оплачено:</strong> %.2f ₽ | <strong>Способ оплаты:</strong> %s</p>
    <p><strong>Рекомендации:</strong> %s</p>
    <p><strong>Комментарий:</strong> %s</p>
  </div>
  <div class="signs">
    <div>Исполнитель: ______________________</div>
    <div>Заказчик: ______________________</div>
  </div>
</body>
</html>`,
		documentNumber(cert.ID, cert.Number), documentNumber(cert.ID, cert.Number),
		cert.WorkDate.Format("02.01.2006"),
		html.EscapeString(cert.ReceiptNumber),
		html.EscapeString(cert.ContractorName),
		html.EscapeString(cert.AddressText),
		html.EscapeString(strings.Join(execNames, "; ")),
		html.EscapeString(strings.Join(cert.WorkTypes, ", ")),
		legalBlock,
		spRows.String(),
		cert.TotalAmount, cert.PaidAmount, html.EscapeString(cert.PaymentMethod),
		html.EscapeString(cert.Recommendation),
		html.EscapeString(cert.Comment),
	), nil
}

func documentNumber(id int64, number *int64) int64 {
	if number != nil {
		return *number
	}
	return id
}

// ExportApplications генерирует CSV или XLSX список заявок.
func (s *Service) ExportApplications(ctx context.Context, auth *platform.AuthContext, f ApplicationFilter, format string) (string, string, []byte, error) {
	f.Limit = 500
	items, err := s.ListApplications(ctx, auth, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Номер", "План с", "План по", "Контрагент", "Адрес", "Инженер", "Статус", "План сумма", "Факт сумма", "Комментарий"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(items))
		for _, a := range items {
			rows = append(rows, []string{
				fmt.Sprint(a.ID), fmt.Sprint(documentNumber(a.ID, a.Number)),
				a.PlanDatetimeFrom.Format("02.01.2006 15:04"),
				a.PlanDatetimeTo.Format("02.01.2006 15:04"),
				a.ContractorName, a.AddressText, a.EngineerName, a.Status.Label(),
				fmt.Sprintf("%.2f", a.PlannedSum), fmt.Sprintf("%.2f", a.ActsTotalAmount), a.Comment,
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "applications.csv", data, err
	}
	rows := make([][]any, 0, len(items))
	for _, a := range items {
		rows = append(rows, []any{
			a.ID, documentNumber(a.ID, a.Number),
			a.PlanDatetimeFrom.Format("02.01.2006 15:04"),
			a.PlanDatetimeTo.Format("02.01.2006 15:04"),
			a.ContractorName, a.AddressText, a.EngineerName, a.Status.Label(),
			a.PlannedSum, a.ActsTotalAmount, a.Comment,
		})
	}
	data, err := BuildXLSX("Заявки", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "applications.xlsx", data, err
}

// ExportCertificates генерирует CSV или XLSX выгрузку актов выполненных работ.
func (s *Service) ExportCertificates(ctx context.Context, auth *platform.AuthContext, f CertificateFilter, format string) (string, string, []byte, error) {
	f.Limit = 500
	items, err := s.ListCertificates(ctx, auth, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Номер", "Дата работ", "Контрагент", "Адрес", "Сумма", "Оплачено", "Запчасти", "Способ оплаты", "Квитанция", "Виды работ"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(items))
		for _, c := range items {
			rows = append(rows, []string{
				fmt.Sprint(c.ID), fmt.Sprint(documentNumber(c.ID, c.Number)), c.WorkDate.Format("02.01.2006"), c.ContractorName, c.AddressText,
				fmt.Sprintf("%.2f", c.TotalAmount), fmt.Sprintf("%.2f", c.PaidAmount), fmt.Sprintf("%.2f", c.CostOfParts),
				c.PaymentMethod, c.ReceiptNumber, strings.Join(c.WorkTypes, ", "),
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "certificates.csv", data, err
	}
	rows := make([][]any, 0, len(items))
	for _, c := range items {
		rows = append(rows, []any{
			c.ID, documentNumber(c.ID, c.Number), c.WorkDate.Format("02.01.2006"), c.ContractorName, c.AddressText,
			c.TotalAmount, c.PaidAmount, c.CostOfParts,
			c.PaymentMethod, c.ReceiptNumber, strings.Join(c.WorkTypes, ", "),
		})
	}
	data, err := BuildXLSX("Акты", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "certificates.xlsx", data, err
}

// ExportContracts генерирует CSV или XLSX выгрузку договоров ТО.
func (s *Service) ExportContracts(ctx context.Context, f ContractFilter, format string) (string, string, []byte, error) {
	f.Limit = 500
	items, err := s.repo.ListContracts(ctx, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Номер", "Категория", "Контрагент", "Адрес", "Действует с", "Действует по", "Сумма", "Статус"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(items))
		for _, c := range items {
			rows = append(rows, []string{
				fmt.Sprint(c.ID), c.Number, ContractCategoryLabel(c.Category), c.ContractorName, c.AddressText,
				c.ValidFrom.Format("02.01.2006"), c.ValidTo.Format("02.01.2006"), fmt.Sprintf("%.2f", c.Price), c.Status.Label(),
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "contracts.csv", data, err
	}
	rows := make([][]any, 0, len(items))
	for _, c := range items {
		rows = append(rows, []any{
			c.ID, c.Number, ContractCategoryLabel(c.Category), c.ContractorName, c.AddressText,
			c.ValidFrom.Format("02.01.2006"), c.ValidTo.Format("02.01.2006"), c.Price, c.Status.Label(),
		})
	}
	data, err := BuildXLSX("Договоры ТО", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "contracts.xlsx", data, err
}

// ExportCalls генерирует CSV или XLSX выгрузку журнала звонков.
func (s *Service) ExportCalls(ctx context.Context, f CallFilter, format string) (string, string, []byte, error) {
	f.Limit = 500
	items, err := s.repo.ListCalls(ctx, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Начало", "Направление", "Статус", "Кто звонил", "Кому", "Контрагент", "Тип звонящего", "Обращение", "Результат", "Длительность (с)", "Комментарий"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(items))
		for _, c := range items {
			rows = append(rows, []string{
				fmt.Sprint(c.ID), c.StartedAt.Format("02.01.2006 15:04"), c.Direction, c.Status,
				c.CallerPhone, c.CalleePhone, c.ContractorName, fmt.Sprint(c.CallerType), c.TypeOfAppeal, fmt.Sprint(c.CallResult),
				fmt.Sprint(c.DurationSec), c.Comment,
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "calls.csv", data, err
	}
	rows := make([][]any, 0, len(items))
	for _, c := range items {
		rows = append(rows, []any{
			c.ID, c.StartedAt.Format("02.01.2006 15:04"), c.Direction, c.Status,
			c.CallerPhone, c.CalleePhone, c.ContractorName, c.CallerType, c.TypeOfAppeal, c.CallResult,
			c.DurationSec, c.Comment,
		})
	}
	data, err := BuildXLSX("Звонки", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "calls.xlsx", data, err
}

// ExportAnalytics генерирует CSV или XLSX выгрузку анкет аналитики технических работ.
func (s *Service) ExportAnalytics(ctx context.Context, f AnalyticFilter, format string) (string, string, []byte, error) {
	f.Limit = 500
	items, err := s.ListAnalytics(ctx, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Номер", "Акт №", "Контрагент", "Инженер", "Оценка", "Комментарий", "Дата создания"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(items))
		for _, a := range items {
			rows = append(rows, []string{
				fmt.Sprint(a.ID), a.Number, fmt.Sprint(a.CertificateID), a.ContractorName, a.EngineerName,
				fmt.Sprint(a.Rating), a.Comment, a.CreatedAt.Format("02.01.2006 15:04"),
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "analytics.csv", data, err
	}
	rows := make([][]any, 0, len(items))
	for _, a := range items {
		rows = append(rows, []any{
			a.ID, a.Number, a.CertificateID, a.ContractorName, a.EngineerName,
			a.Rating, a.Comment, a.CreatedAt.Format("02.01.2006 15:04"),
		})
	}
	data, err := BuildXLSX("Аналитика", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "analytics.xlsx", data, err
}

// ExportContractorsReport генерирует CSV или XLSX отчёта по контрагентам.
func (s *Service) ExportContractorsReport(ctx context.Context, f ReportFilter, format string) (string, string, []byte, error) {
	rowsData, err := s.repo.ContractorsReport(ctx, f)
	if err != nil {
		return "", "", nil, err
	}
	headers := []string{"ID", "Контрагент", "Телефоны", "Адреса", "Актов", "Сумма работ", "Запчасти"}
	if strings.EqualFold(format, "csv") {
		rows := make([][]string, 0, len(rowsData))
		for _, r := range rowsData {
			rows = append(rows, []string{
				fmt.Sprint(r.ContractorID), r.Name, strings.Join(r.Phones, ", "), strings.Join(r.Addresses, "; "),
				fmt.Sprint(r.CertificatesCount), fmt.Sprintf("%.2f", r.FactSum), fmt.Sprintf("%.2f", r.CostOfParts),
			})
		}
		data, err := BuildCSV(headers, rows)
		return "text/csv; charset=utf-8", "contractors-report.csv", data, err
	}
	rows := make([][]any, 0, len(rowsData))
	for _, r := range rowsData {
		rows = append(rows, []any{
			r.ContractorID, r.Name, strings.Join(r.Phones, ", "), strings.Join(r.Addresses, "; "),
			r.CertificatesCount, r.FactSum, r.CostOfParts,
		})
	}
	data, err := BuildXLSX("Отчёт по контрагентам", headers, rows)
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "contractors-report.xlsx", data, err
}
