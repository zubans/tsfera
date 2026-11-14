package crm

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Module struct {
	Service *Service
	Hub     *WSHub
	handler *Handler
	repo    Repository
}

func NewModule(
	repo Repository,
	users UserDirectory,
	auth platform.Authorizer,
	bus platform.EventBus,
	cfg Config,
	logger *slog.Logger,
) *Module {
	hub := NewWSHub(logger)
	svc := NewService(repo, users, bus, hub, cfg, logger)
	return &Module{
		Service: svc,
		Hub:     hub,
		handler: NewHandler(svc, auth, logger),
		repo:    repo,
	}
}

func (m *Module) Name() string { return "crm" }

func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return migrationsFS
	}
	return sub
}

func (m *Module) MountHTTP(r chi.Router) {
	m.handler.Mount(r)
}

func (m *Module) Permissions() []platform.PermissionDef {
	return platform.WithMigrationPermissions("crm", []platform.PermissionDef{
		{Key: "crm.contractor.view", Group: "CRM", Resource: "contractor", Action: "view", Description: "Просмотр доступных контрагентов"},
		{Key: "crm.contractor.view_all", Group: "CRM", Resource: "contractor", Action: "view_all", Description: "Просмотр всей базы контрагентов (без правила ±3 дней)"},
		{Key: "crm.contractor.create", Group: "CRM", Resource: "contractor", Action: "create", Description: "Создание контрагентов"},
		{Key: "crm.contractor.update", Group: "CRM", Resource: "contractor", Action: "update", Description: "Редактирование контрагентов и контактов"},
		{Key: "crm.contractor.delete", Group: "CRM", Resource: "contractor", Action: "delete", Description: "Удаление контрагентов"},
		{Key: "crm.address.view", Group: "CRM", Resource: "address", Action: "view", Description: "Просмотр адресов"},
		{Key: "crm.address.create", Group: "CRM", Resource: "address", Action: "create", Description: "Создание адресов"},
		{Key: "crm.address.update", Group: "CRM", Resource: "address", Action: "update", Description: "Редактирование адресов и оборудования"},
		{Key: "crm.address.delete", Group: "CRM", Resource: "address", Action: "delete", Description: "Удаление адресов"},
		{Key: "crm.equipment.view", Group: "CRM", Resource: "equipment", Action: "view", Description: "Просмотр справочника оборудования"},
		{Key: "crm.equipment.create", Group: "CRM", Resource: "equipment", Action: "create", Description: "Добавление моделей оборудования"},
		{Key: "crm.ref.manage", Group: "CRM", Resource: "ref", Action: "manage", Description: "Управление справочниками (марки, типы, виды работ, особые отметки)"},
		{Key: "crm.calendar.select_user", Group: "CRM", Resource: "calendar", Action: "select_user", Description: "Выбор любого инженера в календаре"},
		{Key: "crm.calendar.select_subdivision", Group: "CRM", Resource: "calendar", Action: "select_subdivision", Description: "Выбор любого подразделения в календаре"},
		{Key: "crm.contract.view", Group: "CRM", Resource: "contract", Action: "view", Description: "Просмотр договоров ТО"},
		{Key: "crm.contract.create", Group: "CRM", Resource: "contract", Action: "create", Description: "Создание договоров ТО"},
		{Key: "crm.contract.update", Group: "CRM", Resource: "contract", Action: "update", Description: "Редактирование договоров ТО"},
		{Key: "crm.contract.delete", Group: "CRM", Resource: "contract", Action: "delete", Description: "Удаление договоров ТО"},
		{Key: "crm.application.view", Group: "CRM", Resource: "application", Action: "view", Description: "Просмотр заявок и календаря"},
		{Key: "crm.application.create", Group: "CRM", Resource: "application", Action: "create", Description: "Создание заявок"},
		{Key: "crm.application.update", Group: "CRM", Resource: "application", Action: "update", Description: "Редактирование заявок и оценок"},
		{Key: "crm.application.delete", Group: "CRM", Resource: "application", Action: "delete", Description: "Удаление заявок"},
		{Key: "crm.certificate.view", Group: "CRM", Resource: "certificate", Action: "view", Description: "Просмотр актов выполненных работ"},
		{Key: "crm.certificate.create", Group: "CRM", Resource: "certificate", Action: "create", Description: "Создание актов выполненных работ"},
		{Key: "crm.certificate.update", Group: "CRM", Resource: "certificate", Action: "update", Description: "Редактирование актов"},
		{Key: "crm.certificate.delete", Group: "CRM", Resource: "certificate", Action: "delete", Description: "Удаление актов"},
		{Key: "crm.task.view", Group: "CRM", Resource: "task", Action: "view", Description: "Просмотр задач"},
		{Key: "crm.task.create", Group: "CRM", Resource: "task", Action: "create", Description: "Создание задач"},
		{Key: "crm.task.update", Group: "CRM", Resource: "task", Action: "update", Description: "Изменение статуса и ответственного по задаче"},
		{Key: "crm.task.delete", Group: "CRM", Resource: "task", Action: "delete", Description: "Удаление задач"},
		{Key: "crm.pool.manage", Group: "CRM", Resource: "pool", Action: "manage", Description: "Настройка пула задач и запуск фонового распределения"},
		{Key: "crm.call.view", Group: "CRM", Resource: "call", Action: "view", Description: "Просмотр истории звонков"},
		{Key: "crm.call.update", Group: "CRM", Resource: "call", Action: "update", Description: "Управление и перевод звонков"},
		{Key: "crm.report.view", Group: "CRM", Resource: "report", Action: "view", Description: "Просмотр финансовых отчётов и отчётов по просрочке"},
		{Key: "crm.subdivision.view", Group: "CRM", Resource: "subdivision", Action: "view", Description: "Просмотр подразделений"},
		{Key: "crm.subdivision.manage", Group: "CRM", Resource: "subdivision", Action: "manage", Description: "Управление подразделениями"},
		{Key: "crm.sip.manage", Group: "CRM", Resource: "sip", Action: "manage", Description: "Управление SIP-аккаунтами"},
		{Key: "crm.certificate.update_any", Group: "CRM", Resource: "certificate", Action: "update_any", Description: "Редактирование и создание любых актов без ограничений инженера"},
		{Key: "crm.analytic.view", Group: "CRM", Resource: "analytic", Action: "view", Description: "Просмотр анкет контроля качества и вопросов"},
		{Key: "crm.analytic.manage", Group: "CRM", Resource: "analytic", Action: "manage", Description: "Управление анкетами контроля качества и вопросами"},
		{Key: "crm.banner.manage", Group: "CRM", Resource: "banner", Action: "manage", Description: "Управление баннерами (фото до/после для инженеров)"},
		{Key: "crm.traffic_source.manage", Group: "CRM", Resource: "traffic_source", Action: "manage", Description: "Управление источниками трафика"},
		{Key: "crm.setting.manage", Group: "CRM", Resource: "setting", Action: "manage", Description: "Управление глобальными настройками CRM"},
	})
}

func (m *Module) Workers() []platform.PeriodicJob {
	return []platform.PeriodicJob{
		{
			Name:     "crm_quarterly_call_tasks",
			Interval: 1 * time.Hour,
			Run: func(ctx context.Context) error {
				_, err := m.Service.CreateQuarterlyCallTasks(ctx)
				return err
			},
		},
		{
			Name:     "crm_prolongation_tasks",
			Interval: 1 * time.Hour,
			Run: func(ctx context.Context) error {
				_, err := m.Service.CreateProlongationTasks(ctx)
				return err
			},
		},
		{
			Name:     "crm_task_pool_distributor",
			Interval: 30 * time.Second,
			Run: func(ctx context.Context) error {
				_, err := m.Service.DistributeTaskPool(ctx)
				return err
			},
		},
		{
			Name:     "crm_overdue_tasks_notifier",
			Interval: 1 * time.Minute,
			Run: func(ctx context.Context) error {
				_, err := m.Service.CheckOverdueTasks(ctx)
				return err
			},
		},
		{
			Name:     "crm_cleanup_notifications",
			Interval: 24 * time.Hour,
			Run: func(ctx context.Context) error {
				_, err := m.Service.CleanupOldNotifications(ctx)
				return err
			},
		},
		{
			Name:     "crm_miss_call_pool_sync",
			Interval: 1 * time.Minute,
			Run: func(ctx context.Context) error {
				_, err := m.Service.SyncMissedCallPool(ctx)
				return err
			},
		},
	}
}

// Tools поставляет AI-чату и ассистенту инструменты работы с контрагентами, заявками, задачами и звонками с учётом RBAC.
func (m *Module) Tools() []platform.ToolDefinition {
	return []platform.ToolDefinition{
		{
			Name:        "crm_find_contractor",
			Description: "Поиск контрагентов (клиентов) Теплосферы по ФИО, адресу или номеру телефона.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Поисковая строка (ФИО, улица, телефон)"},
					"limit": map[string]any{"type": "integer", "description": "Максимум результатов (по умолчанию 10)"},
				},
				"required": []string{"query"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 20 {
					in.Limit = 10
				}
				items, total, err := m.Service.SearchContractors(ctx, auth, in.Query, in.Limit, 0)
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Контрагенты по запросу не найдены (или недоступны по правам)."}, nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Найдено контрагентов: %d\n", total)
				var refs []platform.SourceRef
				for _, c := range items {
					fmt.Fprintf(&b, "- #%d: %s\n", c.ID, c.Name)
					refs = append(refs, platform.SourceRef{
						SourceType: "crm_contractor",
						SourceID:   strconv.FormatInt(c.ID, 10),
						Title:      c.Name,
						Path:       fmt.Sprintf("/crm/contractors/%d", c.ID),
					})
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "crm_get_contractor_card",
			Description: "Получить подробную карточку контрагента (телефоны, адреса, котлы и серийные номера).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contractor_id": map[string]any{"type": "integer", "description": "ID контрагента"},
				},
				"required": []string{"contractor_id"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					ContractorID int64 `json:"contractor_id"`
				}
				_ = json.Unmarshal(args, &in)
				summary, err := m.Service.ResolveEntity(ctx, auth, "contractor", in.ContractorID)
				if err != nil {
					return platform.ToolResult{}, err
				}
				return platform.ToolResult{
					Content: summary.Markdown,
					Sources: []platform.SourceRef{{
						SourceType: "crm_contractor",
						SourceID:   strconv.FormatInt(in.ContractorID, 10),
						Title:      summary.Title,
						Path:       summary.Path,
					}},
				}, nil
			},
		},
		{
			Name:        "crm_list_applications",
			Description: "Список заявок на сервисные/ремонтные работы (с фильтром по контрагенту или инженеру).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contractor_id": map[string]any{"type": "integer"},
					"engineer_id":   map[string]any{"type": "integer"},
					"limit":         map[string]any{"type": "integer"},
				},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("crm.application.view") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права crm.application.view")
				}
				var in struct {
					ContractorID *int64 `json:"contractor_id"`
					EngineerID   *int64 `json:"engineer_id"`
					Limit        int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 20 {
					in.Limit = 10
				}
				items, err := m.Service.ListApplications(ctx, auth, ApplicationFilter{
					ContractorID: in.ContractorID,
					EngineerID:   in.EngineerID,
					Limit:        in.Limit,
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Заявки не найдены."}, nil
				}
				var b strings.Builder
				var refs []platform.SourceRef
				for _, a := range items {
					fmt.Fprintf(&b, "- Заявка #%d (%s): %s | %s | Инженер: %s | План: %s\n",
						a.ID, a.Status.Label(), a.ContractorName, a.AddressText, a.EngineerName, a.PlanDatetimeFrom.Format("02.01 15:04"))
					refs = append(refs, platform.SourceRef{
						SourceType: "crm_application",
						SourceID:   strconv.FormatInt(a.ID, 10),
						Title:      fmt.Sprintf("Заявка #%d — %s", a.ID, a.ContractorName),
						Path:       fmt.Sprintf("/crm/applications/%d", a.ID),
					})
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "crm_get_application_details",
			Description: "Подробности конкретной заявки и связанных актов выполненных работ.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"application_id": map[string]any{"type": "integer"},
				},
				"required": []string{"application_id"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("crm.application.view") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права crm.application.view")
				}
				var in struct {
					ApplicationID int64 `json:"application_id"`
				}
				_ = json.Unmarshal(args, &in)
				summary, err := m.Service.ResolveEntity(ctx, auth, "application", in.ApplicationID)
				if err != nil {
					return platform.ToolResult{}, err
				}
				certs, _ := m.Service.ListCertificates(ctx, auth, CertificateFilter{ApplicationID: &in.ApplicationID, Limit: 10})
				var b strings.Builder
				b.WriteString(summary.Markdown)
				if len(certs) > 0 {
					b.WriteString("\n## Связанные акты\n")
					for _, c := range certs {
						fmt.Fprintf(&b, "- Акт #%d (%s): %.2f руб., особые отметки: %s\n",
							c.ID, c.WorkDate.Format("02.01.2006"), c.TotalAmount, c.SpecialNotes)
					}
				}
				return platform.ToolResult{
					Content: b.String(),
					Sources: []platform.SourceRef{{
						SourceType: "crm_application",
						SourceID:   strconv.FormatInt(in.ApplicationID, 10),
						Title:      summary.Title,
						Path:       summary.Path,
					}},
				}, nil
			},
		},
		{
			Name:        "crm_list_tasks",
			Description: "Список активных/просроченных задач диспетчерского и сервисного пула.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contractor_id": map[string]any{"type": "integer"},
					"only_urgent":   map[string]any{"type": "boolean"},
					"limit":         map[string]any{"type": "integer"},
				},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("crm.task.view") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права crm.task.view")
				}
				var in struct {
					ContractorID *int64 `json:"contractor_id"`
					OnlyUrgent   bool   `json:"only_urgent"`
					Limit        int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 20 {
					in.Limit = 10
				}
				items, err := m.Service.ListTasks(ctx, auth, TaskFilter{
					ContractorID: in.ContractorID,
					OnlyUrgent:   in.OnlyUrgent,
					Limit:        in.Limit,
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Задачи не найдены."}, nil
				}
				var b strings.Builder
				for _, t := range items {
					fmt.Fprintf(&b, "- #%d [%s] %s (%s) — срок %s\n",
						t.ID, t.Status.Label(), t.Title, t.ContractorName, t.PlanDatetimeTo.Format("02.01 15:04"))
				}
				return platform.ToolResult{Content: b.String()}, nil
			},
		},
		{
			Name:        "crm_get_calls",
			Description: "История звонков по контрагенту или номеру телефона.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"contractor_id": map[string]any{"type": "integer"},
					"phone":         map[string]any{"type": "string"},
					"limit":         map[string]any{"type": "integer"},
				},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("crm.call.view") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права crm.call.view")
				}
				var in struct {
					ContractorID *int64 `json:"contractor_id"`
					Phone        string `json:"phone"`
					Limit        int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.ContractorID != nil {
					ok, err := m.Service.CanViewContractor(ctx, auth, *in.ContractorID)
					if err != nil || !ok {
						return platform.ToolResult{}, platform.NewForbidden("Контрагент недоступен для просмотра звонков")
					}
				}
				items, err := m.Service.ListCalls(ctx, CallFilter{
					ContractorID: in.ContractorID,
					Phone:        in.Phone,
					Limit:        min(max(in.Limit, 5), 20),
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Звонки не найдены."}, nil
				}
				var b strings.Builder
				for _, c := range items {
					fmt.Fprintf(&b, "- [%s] %s -> %s (%s, %d сек., %s)\n",
						c.StartedAt.Format("02.01 15:04"), c.CallerPhone, c.CalleePhone, c.Status, c.DurationSec, c.ContractorName)
				}
				return platform.ToolResult{Content: b.String()}, nil
			},
		},
	}
}

// SearchSources экспортирует карточки контрагентов и каталог оборудования в общий индекс гибридного поиска.
func (m *Module) SearchSources() []platform.IndexSource {
	return []platform.IndexSource{
		&contractorIndexSource{svc: m.Service, repo: m.repo},
		&equipmentIndexSource{repo: m.repo},
	}
}

type contractorIndexSource struct {
	svc  *Service
	repo Repository
}

func (s *contractorIndexSource) SourceType() string { return "crm_contractor" }

func (s *contractorIndexSource) ListForIndex(ctx context.Context, _ *time.Time) ([]platform.IndexItem, error) {
	items, _, err := s.repo.SearchContractors(ctx, ContractorFilter{Limit: 500})
	if err != nil {
		return nil, err
	}
	out := make([]platform.IndexItem, 0, len(items))
	for _, c := range items {
		var b strings.Builder
		fmt.Fprintf(&b, "Контрагент: %s\n", c.Name)
		if c.Note != "" {
			fmt.Fprintf(&b, "Примечание: %s\n", c.Note)
		}
		for _, p := range c.Phones {
			fmt.Fprintf(&b, "Телефон: %s\n", p.Phone)
		}
		for _, a := range c.Addresses {
			fmt.Fprintf(&b, "Адрес: %s\n", a.Address)
			for _, eq := range a.Equipments {
				fmt.Fprintf(&b, "Оборудование: %s S/N %s\n", eq.EquipmentName, eq.SerialNumber)
			}
		}
		out = append(out, platform.IndexItem{
			SourceType: "crm_contractor",
			SourceID:   strconv.FormatInt(c.ID, 10),
			Title:      c.Name,
			Content:    b.String(),
			Path:       fmt.Sprintf("/crm/contractors/%d", c.ID),
			UpdatedAt:  c.UpdatedAt,
		})
	}
	return out, nil
}

func (s *contractorIndexSource) CanView(ctx context.Context, auth *platform.AuthContext, sourceID string, _ map[string]string) (bool, error) {
	id, err := strconv.ParseInt(sourceID, 10, 64)
	if err != nil {
		return false, nil
	}
	return s.svc.CanViewContractor(ctx, auth, id)
}

type equipmentIndexSource struct {
	repo Repository
}

func (s *equipmentIndexSource) SourceType() string { return "crm_equipment" }

func (s *equipmentIndexSource) ListForIndex(ctx context.Context, _ *time.Time) ([]platform.IndexItem, error) {
	items, err := s.repo.ListEquipments(ctx, "", 500)
	if err != nil {
		return nil, err
	}
	out := make([]platform.IndexItem, 0, len(items))
	for _, eq := range items {
		out = append(out, platform.IndexItem{
			SourceType: "crm_equipment",
			SourceID:   strconv.FormatInt(eq.ID, 10),
			Title:      eq.Name,
			Content:    fmt.Sprintf("Оборудование: %s | Тип: %s | Модель: %s | Производитель: %s", eq.Name, eq.Type, eq.Model, eq.Manufacturer),
			Path:       "/crm/equipments",
			UpdatedAt:  eq.UpdatedAt,
		})
	}
	return out, nil
}

func (s *equipmentIndexSource) CanView(_ context.Context, auth *platform.AuthContext, _ string, _ map[string]string) (bool, error) {
	return auth != nil && auth.HasPermission("crm.equipment.view"), nil
}
