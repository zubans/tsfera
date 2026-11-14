package crm

import (
	"encoding/json"
	"strings"
	"time"
)

// DocumentStatus повторяет статусы документов Теплосферы (включая StatusProcessed = 2 для правила 3 дней).
type DocumentStatus int

const (
	StatusActive    DocumentStatus = 1
	StatusProcessed DocumentStatus = 2
	StatusFailed    DocumentStatus = 3
	StatusCancelled DocumentStatus = 4
)

func (s DocumentStatus) Label() string {
	switch s {
	case StatusActive:
		return "Активна"
	case StatusProcessed:
		return "Проведена"
	case StatusFailed:
		return "Провалена"
	case StatusCancelled:
		return "Отменена"
	default:
		return "Неизвестно"
	}
}

type ContractorType int

const (
	ContractorIndividual ContractorType = 1
	ContractorLegal      ContractorType = 2
)

type TaskStatus int

const (
	TaskStatusNew        TaskStatus = 1
	TaskStatusInProgress TaskStatus = 2
	TaskStatusDone       TaskStatus = 3
	TaskStatusNoAnswer   TaskStatus = 4
	TaskStatusFailed     TaskStatus = 5
	TaskStatusCancelled  TaskStatus = 6
)

func (s TaskStatus) Label() string {
	switch s {
	case TaskStatusNew:
		return "Новая"
	case TaskStatusInProgress:
		return "В работе"
	case TaskStatusDone:
		return "Выполнена"
	case TaskStatusNoAnswer:
		return "Недозвон"
	case TaskStatusFailed:
		return "Провалена"
	case TaskStatusCancelled:
		return "Отменена"
	default:
		return "Неизвестно"
	}
}

type TaskType string

const (
	TaskTypeCall          TaskType = "call"
	TaskTypeQuarterlyCall TaskType = "quarterly_call"
	TaskTypeProlongation  TaskType = "prolongation"
	TaskTypeNoAnswer      TaskType = "no_answer"
	TaskTypeService       TaskType = "service"
	TaskTypeOther         TaskType = "other"
)

func (t TaskType) Label() string {
	switch t {
	case TaskTypeCall:
		return "Звонок"
	case TaskTypeQuarterlyCall:
		return "Ежеквартальный обзвон"
	case TaskTypeProlongation:
		return "Пролонгация договора"
	case TaskTypeNoAnswer:
		return "Недозвон"
	case TaskTypeService:
		return "ТО / Сервис"
	case TaskTypeAppLimit:
		return "Лимит заявок"
	case TaskTypeCallFeedback:
		return "Оценка качества работ"
	case TaskTypeOther:
		return "Прочее"
	default:
		return string(t)
	}
}

var StandardWorkTypes = []string{
	"ТО",
	"Ремонт",
	"Пусконаладка",
	"Диагностика",
	"Монтаж",
	"Аварийный выезд",
	"Прочее",
}

const (
	ContractCategoryStandard     = "standard"
	ContractCategoryPlus         = "plus"
	ContractCategoryAllInclusive = "all_inclusive"
	ContractCategoryEconomy      = "economy"

	PaymentOptionCash   = "cash"
	PaymentOptionBank   = "bank"
	PaymentOptionQRCode = "qr_code"

	TaskResultAnswered           = "answered"
	TaskResultNoAnswer           = "no_answer"
	TaskResultApplicationCreated = "application_created"
	TaskResultRefused            = "refused"

	CallAppealGeneralQuestion = "general_question"
	CallAppealServiceRequest  = "service_request"
	CallAppealBuyParts        = "buy_parts"

	RefKindManufacturer  = "manufacturer"
	RefKindEquipmentType = "equipment_type"
	RefKindWorkType      = "work_type"
	RefKindSpecialNote   = "special_note"
)

type EnumOption struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

var (
	ContractCategories = []EnumOption{
		{ID: 1, Key: ContractCategoryStandard, Label: "Стандарт"},
		{ID: 2, Key: ContractCategoryPlus, Label: "Плюс"},
		{ID: 3, Key: ContractCategoryAllInclusive, Label: "Все включено"},
		{ID: 4, Key: ContractCategoryEconomy, Label: "Эконом"},
	}
	PaymentOptions = []EnumOption{
		{ID: 1, Key: PaymentOptionCash, Label: "Наличные"},
		{ID: 2, Key: PaymentOptionBank, Label: "Оплата по счету"},
		{ID: 3, Key: PaymentOptionQRCode, Label: "Оплата по qr коду"},
		{ID: 4, Key: "card", Label: "Оплата по карте"},
	}
	TaskResults = []EnumOption{
		{ID: 1, Key: TaskResultAnswered, Label: "Дозвонился"},
		{ID: 2, Key: TaskResultNoAnswer, Label: "Не дозвонился"},
		{ID: 3, Key: TaskResultApplicationCreated, Label: "Заявка оформлена"},
		{ID: 4, Key: TaskResultRefused, Label: "Отказ"},
	}
	CallTypesOfAppeal = []EnumOption{
		{ID: 1, Key: CallAppealGeneralQuestion, Label: "Общий вопрос"},
		{ID: 2, Key: CallAppealServiceRequest, Label: "Заявка на обслуживание"},
		{ID: 3, Key: CallAppealBuyParts, Label: "Купить запчасти"},
	}
)

type SubdivisionPhone struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Phone  string `json:"phone"`
	IsMain bool   `json:"is_main,omitempty"`
	IsDuty bool   `json:"is_duty"`
}

type Subdivision struct {
	ID               int64                      `json:"id"`
	Name             string                     `json:"name"`
	Address          string                     `json:"address"`
	Phones           []SubdivisionPhone         `json:"phones,omitempty"`
	SupervisorUserID *int64                     `json:"supervisor_user_id,omitempty"`
	PaymentOptions   []SubdivisionPaymentOption `json:"payment_options,omitempty"`
	Deleted          bool                       `json:"deleted"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
}

type SipAccount struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Domain        string    `json:"domain"`
	Login         string    `json:"login"`
	Password      string    `json:"password,omitempty"`
	Protocol      string    `json:"protocol"`
	Server        string    `json:"server"`
	Port          int       `json:"port"`
	AbonentID     string    `json:"abonent_id"`
	SubdivisionID *int64    `json:"subdivision_id,omitempty"`
	UserID        *int64    `json:"user_id,omitempty"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SoftPhoneConfig выдаётся браузеру только для авторизованного сотрудника и без устаревшего XOR-ключа.
type SoftPhoneConfig struct {
	AbonentID string `json:"abonent_id,omitempty"`
	Enabled   bool   `json:"enabled"`
	Name      string `json:"name,omitempty"`
	Domain    string `json:"domain,omitempty"`
	Login     string `json:"login,omitempty"`
	Password  string `json:"password,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Server    string `json:"server,omitempty"`
	Port      int    `json:"port,omitempty"`
	WSURL     string `json:"ws_url,omitempty"`
}

type Contractor struct {
	ClientCategoryID *int                `json:"client_category_id,omitempty"`
	ID               int64               `json:"id"`
	Name             string              `json:"name"`
	FirstName        string              `json:"first_name"`
	SecondName       string              `json:"second_name"`
	LastName         string              `json:"last_name"`
	SubdivisionID    *int64              `json:"subdivision_id,omitempty"`
	Type             ContractorType      `json:"type"`
	IsLegalEntity    bool                `json:"is_legal_entity"`
	TrafficSourceID  *int64              `json:"traffic_source_id,omitempty"`
	LegalEntity      *LegalEntity        `json:"legal_entity,omitempty"`
	Note             string              `json:"note"`
	CreatedBy        int64               `json:"created_by"`
	Deleted          bool                `json:"deleted"`
	Phones           []ContractorPhone   `json:"phones,omitempty"`
	Emails           []ContractorEmail   `json:"emails,omitempty"`
	Addresses        []ContractorAddress `json:"addresses,omitempty"`
	Score            float64             `json:"score,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type SimilarContractorMatch struct {
	ContractorID   int64  `json:"contractor_id"`
	ContractorName string `json:"contractor_name"`
	MatchedOn      string `json:"matched_on"`
	ContactValue   string `json:"contact_value"`
}

type ContractorPhone struct {
	ID           int64     `json:"id"`
	ContractorID int64     `json:"contractor_id"`
	Phone        string    `json:"phone"`
	Formatted    string    `json:"formatted,omitempty"`
	Note         string    `json:"note"`
	Deleted      bool      `json:"deleted"`
	CreatedAt    time.Time `json:"created_at"`
}

type ContractorEmail struct {
	ID           int64     `json:"id"`
	ContractorID int64     `json:"contractor_id"`
	Email        string    `json:"email"`
	Deleted      bool      `json:"deleted"`
	CreatedAt    time.Time `json:"created_at"`
}

type ContractorAddress struct {
	ID           int64              `json:"id"`
	ContractorID int64              `json:"contractor_id"`
	Address      string             `json:"address"`
	RegionCode   string             `json:"region_code"`
	City         string             `json:"city"`
	Street       string             `json:"street"`
	House        string             `json:"house"`
	Apartment    string             `json:"apartment"`
	Floor        string             `json:"floor"`
	Entrance     string             `json:"entrance"`
	Comment      string             `json:"comment"`
	Deleted      bool               `json:"deleted"`
	Equipments   []AddressEquipment `json:"equipments,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type Equipment struct {
	ID           int64     `json:"id"`
	Type         string    `json:"type"`
	Model        string    `json:"model"`
	Manufacturer string    `json:"manufacturer"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// BuildEquipmentName формирует единое составное наименование оборудования как в Теплосфере.
func BuildEquipmentName(eqType, model, manufacturer string) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{eqType, model, manufacturer} {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

type AddressEquipment struct {
	ID            int64     `json:"id"`
	AddressID     int64     `json:"address_id"`
	EquipmentID   int64     `json:"equipment_id"`
	EquipmentName string    `json:"equipment_name,omitempty"`
	SerialNumber  string    `json:"serial_number"`
	Note          string    `json:"note"`
	Deleted       bool      `json:"deleted"`
	CreatedAt     time.Time `json:"created_at"`
}

type ContractEquipment struct {
	EquipmentID   int64  `json:"equipment_id"`
	EquipmentName string `json:"equipment_name,omitempty"`
	SerialNumber  string `json:"serial_number"`
}

type MaintenanceContract struct {
	ID                int64               `json:"id"`
	ContractorID      int64               `json:"contractor_id"`
	ContractorName    string              `json:"contractor_name,omitempty"`
	AddressID         int64               `json:"address_id"`
	AddressText       string              `json:"address_text,omitempty"`
	ApplicationID     *int64              `json:"application_id,omitempty"`
	Category          string              `json:"category"`
	Number            string              `json:"number"`
	Price             float64             `json:"price"`
	SignedAt          time.Time           `json:"signed_at"`
	ValidFrom         time.Time           `json:"valid_from"`
	ValidTo           time.Time           `json:"valid_to"`
	Status            DocumentStatus      `json:"status"`
	Equipments        []ContractEquipment `json:"equipments,omitempty"`
	Note              string              `json:"note"`
	CreatedBy         int64               `json:"created_by"`
	HasNewerContract  bool                `json:"has_newer_contract,omitempty"`
	NeedsProlongation bool                `json:"needs_prolongation,omitempty"`
	Deleted           bool                `json:"deleted"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

type Application struct {
	PhoneIDs         []int64         `json:"phone_ids"`
	WorkflowStatus   *int            `json:"workflow_status,omitempty"`
	Incomplete       bool            `json:"incomplete"`
	Number           *int64          `json:"number,omitempty"`
	SourceData       json.RawMessage `json:"source_data,omitempty"`
	SourceRelations  json.RawMessage `json:"source_relations,omitempty"`
	ID               int64           `json:"id"`
	ContractorID     int64           `json:"contractor_id"`
	ContractorName   string          `json:"contractor_name,omitempty"`
	AddressID        int64           `json:"address_id"`
	AddressText      string          `json:"address_text,omitempty"`
	EngineerID       int64           `json:"engineer_id"`
	EngineerName     string          `json:"engineer_name,omitempty"`
	EngineerColor    string          `json:"engineer_color,omitempty"`
	SubdivisionID    *int64          `json:"subdivision_id,omitempty"`
	TrafficSourceID  *int64          `json:"traffic_source_id,omitempty"`
	PlannedSum       float64         `json:"planned_sum"`
	WorkWindowFrom   *time.Time      `json:"work_window_from,omitempty"`
	WorkWindowTo     *time.Time      `json:"work_window_to,omitempty"`
	ExecutorIDs      []int64         `json:"executor_ids,omitempty"`
	ExecutorNames    []string        `json:"executor_names,omitempty"`
	AnalyticID       *int64          `json:"analytic_id,omitempty"`
	AnalyticRating   *int            `json:"analytic_rating,omitempty"`
	CreatedBy        int64           `json:"created_by"`
	PlanDatetimeFrom time.Time       `json:"plan_datetime_from"`
	PlanDatetimeTo   time.Time       `json:"plan_datetime_to"`
	Status           DocumentStatus  `json:"status"`
	IsComplete       bool            `json:"is_complete"`
	IsDuty           bool            `json:"is_duty"`
	WorkTypes        []string        `json:"work_types"`
	PriceNote        string          `json:"price_note"`
	Comment          string          `json:"comment"`
	SpecialNotes     string          `json:"special_notes"`
	NotesViewed      bool            `json:"notes_viewed"`
	Rating           *int            `json:"rating,omitempty"`
	RatingComment    string          `json:"rating_comment"`
	EquipmentIDs     []int64         `json:"equipment_ids"`
	SerialNumbers    []string        `json:"serial_numbers"`
	ActsTotalAmount  float64         `json:"acts_total_amount,omitempty"`
	Deleted          bool            `json:"deleted"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// CalendarEvent представляет карточку заявки в календаре со сводкой телефонов, оборудования, суммы актов и флагом непрочитанных замечаний.
type CalendarEvent struct {
	ID              int64          `json:"id"`
	Title           string         `json:"title"`
	Start           time.Time      `json:"start"`
	End             time.Time      `json:"end"`
	ContractorID    int64          `json:"contractor_id"`
	ContractorName  string         `json:"contractor_name"`
	AddressID       int64          `json:"address_id"`
	AddressText     string         `json:"address_text"`
	Phones          []string       `json:"phones"`
	EngineerID      int64          `json:"engineer_id"`
	EngineerName    string         `json:"engineer_name"`
	Color           string         `json:"color"`
	Status          DocumentStatus `json:"status"`
	IsComplete      bool           `json:"is_complete"`
	IsDuty          bool           `json:"is_duty"`
	WorkTypes       []string       `json:"work_types"`
	Equipments      []string       `json:"equipments"`
	SerialNumbers   []string       `json:"serial_numbers"`
	ActsTotalAmount float64        `json:"acts_total_amount"`
	PlannedSum      float64        `json:"planned_sum"`
	WorkWindowFrom  *time.Time     `json:"work_window_from,omitempty"`
	WorkWindowTo    *time.Time     `json:"work_window_to,omitempty"`
	ExecutorIDs     []int64        `json:"executor_ids,omitempty"`
	TrafficSourceID *int64         `json:"traffic_source_id,omitempty"`
	AnalyticID      *int64         `json:"analytic_id,omitempty"`
	AnalyticRating  *int           `json:"analytic_rating,omitempty"`
	SpareParts      []SparePart    `json:"spare_parts,omitempty"`
	SpecialNotes    string         `json:"special_notes"`
	NotesViewed     bool           `json:"notes_viewed"`
	Rating          *int           `json:"rating,omitempty"`
}

type SparePart struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

// CertificateTelemetry фиксирует 9 обязательных контрольных замеров котла из акта выполненных работ.
type CertificateTelemetry struct {
	SmokeChannelVacuum           string `json:"smoke_channel_vacuum,omitempty"`            // Разрежение в дымовом канале
	GasPressureInPipeline        string `json:"gas_pressure_in_the_pipeline,omitempty"`    // Давление газа в подводящем газопроводе
	BurnerGasPressure            string `json:"burner_gas_pressure,omitempty"`             // Давление газа на горелке
	WorkingPressure              string `json:"working_pressure,omitempty"`                // Рабочее давление
	HeatingExpansionTankPressure string `json:"heating_expansion_tank_pressure,omitempty"` // Давление в расширительном баке отопления
	DHWExpansionTankPressure     string `json:"DHW_expansion_tank_pressure,omitempty"`     // Давление в расширительном баке ГВС
	DHWFlow                      string `json:"DHW_flow,omitempty"`                        // Расход ГВС
	PartialHeatSetting           string `json:"partial_heat_setting,omitempty"`            // Настройка частичной мощности
	Voltage                      string `json:"voltage,omitempty"`                         // Напряжение сети
	TypeOfCoolant                string `json:"type_of_coolant,omitempty"`                 // Тип теплоносителя
	CoolantParameters            string `json:"coolant_parameters,omitempty"`              // Параметры теплоносителя
	ContractEligible             *bool  `json:"contract_eligible,omitempty"`               // Подходит под договор ТО
	HasAirConditioner            *bool  `json:"has_air_conditioner,omitempty"`             // Есть кондиционер
}

type Certificate struct {
	Equipments                 json.RawMessage       `json:"equipments"`
	Number                     *int64                `json:"number,omitempty"`
	SourceData                 json.RawMessage       `json:"source_data,omitempty"`
	SourceRelations            json.RawMessage       `json:"source_relations,omitempty"`
	ID                         int64                 `json:"id"`
	ApplicationID              *int64                `json:"application_id,omitempty"`
	MaintenanceContractID      *int64                `json:"maintenance_contract_id,omitempty"`
	WithoutMaintenanceContract bool                  `json:"without_maintenance_contract"`
	ContractorID               int64                 `json:"contractor_id"`
	ContractorName             string                `json:"contractor_name,omitempty"`
	AddressID                  int64                 `json:"address_id"`
	AddressText                string                `json:"address_text,omitempty"`
	EngineerID                 int64                 `json:"engineer_id"`
	EngineerName               string                `json:"engineer_name,omitempty"`
	CreatedBy                  int64                 `json:"created_by"`
	WorkDate                   time.Time             `json:"work_date"`
	TotalAmount                float64               `json:"total_amount"`
	PaidAmount                 float64               `json:"paid_amount"`
	CostOfParts                float64               `json:"cost_of_parts"`
	ReceiptNumber              string                `json:"receipt_number"`
	PaymentMethod              string                `json:"payment_method"`
	WorkTypes                  []string              `json:"work_types"`
	SpareParts                 []SparePart           `json:"spare_parts"`
	Executors                  []CertificateExecutor `json:"executors,omitempty"`
	AnalyticID                 *int64                `json:"analytic_id,omitempty"`
	AnalyticRating             *int                  `json:"analytic_rating,omitempty"`
	Results                    CertificateTelemetry  `json:"results"`
	SpecialNotes               string                `json:"special_notes"`
	OtherNote                  string                `json:"other_note"`
	NotesViewed                bool                  `json:"notes_viewed"`
	Recommendation             string                `json:"recommendation"`
	Comment                    string                `json:"comment"`
	Status                     DocumentStatus        `json:"status"`
	Deleted                    bool                  `json:"deleted"`
	CreatedAt                  time.Time             `json:"created_at"`
	UpdatedAt                  time.Time             `json:"updated_at"`
}

type Task struct {
	MissingDeadline   bool       `json:"missing_deadline"`
	ID                int64      `json:"id"`
	ContractorID      *int64     `json:"contractor_id,omitempty"`
	ContractorName    string     `json:"contractor_name,omitempty"`
	AddressID         *int64     `json:"address_id,omitempty"`
	AddressText       string     `json:"address_text,omitempty"`
	ApplicationID     *int64     `json:"application_id,omitempty"`
	ContractID        *int64     `json:"contract_id,omitempty"`
	PrevTaskID        *int64     `json:"prev_task_id,omitempty"`
	TaskType          TaskType   `json:"task_type"`
	TaskResult        string     `json:"task_result"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	Status            TaskStatus `json:"status"`
	Priority          int        `json:"priority"`
	AssignedUserID    *int64     `json:"assigned_user_id,omitempty"`
	AssignedName      string     `json:"assigned_name,omitempty"`
	SubdivisionID     *int64     `json:"subdivision_id,omitempty"`
	PlanDatetimeTo    time.Time  `json:"plan_datetime_to"`
	ExecutionDatetime *time.Time `json:"execution_datetime,omitempty"`
	DecisionTimeSec   int        `json:"decision_time_sec"`
	IsUrgent          bool       `json:"is_urgent"`
	Note              string     `json:"note"`
	CreatedBy         int64      `json:"created_by"`
	Deleted           bool       `json:"deleted"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type TaskPoolSetting struct {
	ID                       int64      `json:"id"`
	Name                     string     `json:"name"`
	TaskTypes                []TaskType `json:"task_types"`
	Priority                 int        `json:"priority"`
	TimeFrom                 string     `json:"time_from"`
	TimeTo                   string     `json:"time_to"`
	IsOnline                 bool       `json:"is_online"`
	WithSubdivision          bool       `json:"with_subdivision"`
	ForDispatcher            bool       `json:"for_dispatcher"`
	ForEngineer              bool       `json:"for_engineer"`
	TasksMaxCount            int        `json:"tasks_max_count"`
	TransferredTasksMaxCount int        `json:"transferred_tasks_max_count"`
	Active                   bool       `json:"active"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type QueueUser struct {
	UserID         int64      `json:"user_id"`
	Position       int        `json:"position"`
	LastAssignedAt *time.Time `json:"last_assigned_at,omitempty"`
	Active         bool       `json:"active"`
}

type CallRecord struct {
	ID              int64           `json:"id"`
	ExternalCallID  string          `json:"external_call_id"`
	AbonentID       string          `json:"abonent_id"`
	ExtTrackingID   string          `json:"ext_tracking_id"`
	Direction       string          `json:"direction"`
	Status          string          `json:"status"`
	CallerPhone     string          `json:"caller_phone"`
	CalleePhone     string          `json:"callee_phone"`
	SipLogin        string          `json:"sip_login"`
	UserID          *int64          `json:"user_id,omitempty"`
	ContractorID    *int64          `json:"contractor_id,omitempty"`
	ContractorName  string          `json:"contractor_name,omitempty"`
	TrafficSourceID *int64          `json:"traffic_source_id,omitempty"`
	CallerType      int             `json:"caller_type"`
	CallResult      int             `json:"call_result"`
	TypeOfAppeal    string          `json:"type_of_appeal"`
	Comment         string          `json:"comment"`
	RecordURL       string          `json:"record_url"`
	DurationSec     int             `json:"duration_sec"`
	StartedAt       time.Time       `json:"started_at"`
	AnswerTime      *time.Time      `json:"answer_time,omitempty"`
	EndedAt         *time.Time      `json:"ended_at,omitempty"`
	RawPayload      json.RawMessage `json:"raw_payload,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type RefItem struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
}

type ChangeLog struct {
	ID         int64           `json:"id"`
	EntityType string          `json:"entity_type"`
	EntityID   int64           `json:"entity_id"`
	Action     string          `json:"action"`
	UserID     int64           `json:"user_id"`
	UserName   string          `json:"user_name,omitempty"`
	Changes    json.RawMessage `json:"changes"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Notification struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	EntityType string    `json:"entity_type"`
	EntityID   int64     `json:"entity_id"`
	IsRead     bool      `json:"is_read"`
	CreatedAt  time.Time `json:"created_at"`
}

type UserSetting struct {
	UserID    int64           `json:"user_id"`
	Key       string          `json:"key"`
	ValueJSON json.RawMessage `json:"value_json"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type AmountActsReportRow struct {
	EngineerID        int64              `json:"engineer_id"`
	EngineerName      string             `json:"engineer_name"`
	SubdivisionID     *int64             `json:"subdivision_id,omitempty"`
	SubdivisionName   string             `json:"subdivision_name,omitempty"`
	CertificatesCount int                `json:"certificates_count"`
	TotalAmount       float64            `json:"total_amount"`
	PlannedSumTotal   float64            `json:"planned_sum_total"`
	SumTotal          float64            `json:"sum_total"`
	PaidAmount        float64            `json:"paid_amount"`
	DebtAmount        float64            `json:"debt_amount"`
	ContractsAmount   float64            `json:"contracts_amount"`
	ByPaymentOption   map[string]float64 `json:"by_payment_option,omitempty"`
}

type ExpiredTaskReportRow struct {
	TaskID            int64      `json:"task_id"`
	Title             string     `json:"title"`
	TaskType          TaskType   `json:"task_type"`
	Status            TaskStatus `json:"status"`
	AssignedUserID    *int64     `json:"assigned_user_id,omitempty"`
	AssignedName      string     `json:"assigned_name"`
	ContractorName    string     `json:"contractor_name"`
	PlanDatetimeTo    time.Time  `json:"plan_datetime_to"`
	ExecutionDatetime *time.Time `json:"execution_datetime,omitempty"`
	OverdueMinutes    int64      `json:"overdue_minutes"`
}

type SelectOption struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Extra string `json:"extra,omitempty"`
}

type WSEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}
