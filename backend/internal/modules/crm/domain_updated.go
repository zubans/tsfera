package crm

import (
	"encoding/json"
	"time"
)

// Новые типы задач из обновлённой CRM.
const (
	TaskTypeAppLimit     TaskType = "app_limit"
	TaskTypeCallFeedback TaskType = "call_feedback"
)

// Новые категории договоров ТО (1..10).
const (
	ContractCategoryEconomical = "economical"
	ContractCategoryOptimal    = "optimal"
	ContractCategoryMaximum    = "maximum"
	ContractCategoryWarranty   = "warranty"
	ContractCategoryStandardS  = "standard_s"
	ContractCategoryPlusS      = "plus_s"
)

// Типы звонящих и результаты звонков (карта звонка CallCard).
const (
	CallerTypeClient   = 1
	CallerTypeSupplier = 2
	CallerTypePartner  = 3
	CallerTypeSpam     = 4
	CallerTypeOther    = 5

	CallResultCreateContract       = 1
	CallResultExtendContract       = 2
	CallResultCreateApplication    = 3
	CallResultConsultation         = 4
	CallResultRedirectToDepartment = 5
	CallResultUnsolicitedCall      = 6
	CallResultRecall               = 7
	CallResultNoAnswer             = 8
	CallResultDropped              = 9
	CallResultRefusal              = 10
)

// Активные и архивные типы обращений по звонкам (1..8 legacy, 9..15 актуальные).
const (
	CallAppealToContract    = "to_contract"    // 9: Заключить/продлить договор ТО
	CallAppealServiceWork   = "service_work"   // 10: Провести сервисные/ремонтные работы
	CallAppealSaleEquipment = "sale_equipment" // 11: Покупка оборудования (котлы/кондиционеры)
	CallAppealPartsStore    = "parts_store"    // 12: Магазин запчастей
	CallAppealSpecify       = "specify"        // 13: Уточнить информацию
	CallAppealSpamCall      = "spam_call"      // 14: Спам / нецелевой
	CallAppealOther         = "other_appeal"   // 15: Прочее
)

// Типы источников трафика и анкет обратной связи.
const (
	TrafficSourceTypeSearch      = 1
	TrafficSourceTypeClassifieds = 2
	TrafficSourceTypeReferral    = 3
	TrafficSourceTypeTelephony   = 4

	AnalyticDocTypeCertificate = 1
)

type EnumOptionLegacy struct {
	ID     int    `json:"id"`
	Key    string `json:"key"`
	Label  string `json:"label"`
	Legacy bool   `json:"legacy,omitempty"`
}

var (
	ExtendedContractCategories = []EnumOption{
		{ID: 1, Key: ContractCategoryStandard, Label: "Стандарт"},
		{ID: 2, Key: ContractCategoryPlus, Label: "Плюс"},
		{ID: 3, Key: ContractCategoryAllInclusive, Label: "Все включено"},
		{ID: 4, Key: ContractCategoryEconomy, Label: "Эконом"},
		{ID: 5, Key: ContractCategoryEconomical, Label: "Экономичный"},
		{ID: 6, Key: ContractCategoryOptimal, Label: "Оптимальный"},
		{ID: 7, Key: ContractCategoryMaximum, Label: "Максимальный"},
		{ID: 8, Key: ContractCategoryWarranty, Label: "Гарантийный"},
		{ID: 9, Key: ContractCategoryStandardS, Label: "Стандарт-С"},
		{ID: 10, Key: ContractCategoryPlusS, Label: "Плюс-С"},
	}

	CallerTypeOptions = []EnumOption{
		{ID: CallerTypeClient, Key: "client", Label: "Клиент"},
		{ID: CallerTypeSupplier, Key: "supplier", Label: "Поставщик"},
		{ID: CallerTypePartner, Key: "partner", Label: "Партнёр"},
		{ID: CallerTypeSpam, Key: "spam", Label: "Спам"},
		{ID: CallerTypeOther, Key: "other", Label: "Прочее"},
	}

	CallResultOptions = []EnumOption{
		{ID: CallResultCreateContract, Key: "create_contract", Label: "Оформлен договор"},
		{ID: CallResultExtendContract, Key: "extend_contract", Label: "Продлён договор"},
		{ID: CallResultCreateApplication, Key: "create_application", Label: "Оформлена заявка"},
		{ID: CallResultConsultation, Key: "consultation", Label: "Консультация"},
		{ID: CallResultRedirectToDepartment, Key: "redirect_to_department", Label: "Перевод в другой отдел"},
		{ID: CallResultUnsolicitedCall, Key: "unsolicited_call", Label: "Нецелевой звонок"},
		{ID: CallResultRecall, Key: "recall", Label: "Перезвонить"},
		{ID: CallResultNoAnswer, Key: "no_answer", Label: "Нет ответа"},
		{ID: CallResultDropped, Key: "dropped", Label: "Сброшен"},
		{ID: CallResultRefusal, Key: "refusal", Label: "Отказ"},
	}

	ExtendedCallTypesOfAppeal = []EnumOptionLegacy{
		{ID: 9, Key: CallAppealToContract, Label: "Договор ТО"},
		{ID: 10, Key: CallAppealServiceWork, Label: "Сервисные / ремонтные работы"},
		{ID: 11, Key: CallAppealSaleEquipment, Label: "Продажа оборудования"},
		{ID: 12, Key: CallAppealPartsStore, Label: "Магазин запчастей"},
		{ID: 13, Key: CallAppealSpecify, Label: "Уточнить информацию"},
		{ID: 14, Key: CallAppealSpamCall, Label: "Спам"},
		{ID: 15, Key: CallAppealOther, Label: "Прочее"},
		{ID: 1, Key: CallAppealGeneralQuestion, Label: "Общий вопрос (архив)", Legacy: true},
		{ID: 2, Key: CallAppealServiceRequest, Label: "Заявка на обслуживание (архив)", Legacy: true},
		{ID: 3, Key: CallAppealBuyParts, Label: "Купить запчасти (архив)", Legacy: true},
	}

	TrafficSourceTypes = []EnumOption{
		{ID: TrafficSourceTypeSearch, Key: "search", Label: "Поиск / Карты"},
		{ID: TrafficSourceTypeClassifieds, Key: "classifieds", Label: "Доски объявлений"},
		{ID: TrafficSourceTypeReferral, Key: "referral", Label: "Рекомендации"},
		{ID: TrafficSourceTypeTelephony, Key: "telephony", Label: "Телефония"},
	}

	CallDirections = []EnumOption{
		{ID: 1, Key: "in", Label: "Входящий"},
		{ID: 2, Key: "out", Label: "Исходящий"},
		{ID: 3, Key: "internal", Label: "Внутренний"},
	}

	CallStatuses = []EnumOption{
		{ID: 1, Key: "new", Label: "Новый"},
		{ID: 2, Key: "connected", Label: "Разговор"},
		{ID: 3, Key: "completed", Label: "Завершён"},
		{ID: 4, Key: "missed", Label: "Пропущен"},
		{ID: 5, Key: "failed", Label: "Не состоялся"},
	}
)

// SubdivisionPaymentOption связывает способ оплаты подразделения с загруженным файлом (QR-кодом).
type SubdivisionPaymentOption struct {
	PaymentOptionID int    `json:"payment_option_id"`
	Key             string `json:"key,omitempty"`
	FileID          *int64 `json:"file_id,omitempty"`
	FileURL         string `json:"file_url,omitempty"`
	IsMain          bool   `json:"is_main"`
}

// LegalEntity хранит реквизиты контрагента — юридического лица.
type LegalEntity struct {
	ContractorID         int64     `json:"contractor_id"`
	Name                 string    `json:"name"`
	DirectorName         string    `json:"director_name"`
	ActualAddress        string    `json:"actual_address"`
	LegalAddress         string    `json:"legal_address"`
	BankName             string    `json:"bank_name"`
	INN                  string    `json:"inn"`
	KPP                  string    `json:"kpp"`
	BIK                  string    `json:"bik"`
	CheckingAccount      string    `json:"checking_account"`
	CorrespondingAccount string    `json:"corresponding_account"`
	OKPO                 string    `json:"okpo"`
	OGRN                 string    `json:"ogrn"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// TrafficSource описывает источник обращения клиента.
type TrafficSource struct {
	ID                  int64     `json:"id"`
	Name                string    `json:"name"`
	TrafficSourceTypeID int       `json:"traffic_source_type_id"`
	Sort                int       `json:"sort"`
	Active              bool      `json:"active"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// CertificateExecutor описывает соисполнителя акта и его долю суммы.
type CertificateExecutor struct {
	UserID   int64   `json:"user_id"`
	FullName string  `json:"full_name,omitempty"`
	Sum      float64 `json:"sum"`
}

// SipUserSelectOption возвращает SIP-аккаунт с именем сотрудника.
type SipUserSelectOption struct {
	ID        int64  `json:"id"`
	AbonentID string `json:"abonent_id"`
	Name      string `json:"name"`
	UserID    *int64 `json:"user_id,omitempty"`
	FullName  string `json:"full_name"`
}

// MissCallPoolItem — запись о пропущенном звонке для диспетчерского пула.
type MissCallPoolItem struct {
	ID             int64     `json:"id"`
	CallID         int64     `json:"call_id"`
	UserID         int64     `json:"user_id"`
	IsViewed       bool      `json:"is_viewed"`
	CallerPhone    string    `json:"caller_phone"`
	CalleePhone    string    `json:"callee_phone"`
	StartedAt      time.Time `json:"started_at"`
	ContractorID   *int64    `json:"contractor_id,omitempty"`
	ContractorName string    `json:"contractor_name,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AnalyticQuestionOption — вариант ответа на вопрос аналитики технических работ.
type AnalyticQuestionOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

// AnalyticQuestion — вопрос анкеты контроля качества / аналитики.
type AnalyticQuestion struct {
	ID                     int64                    `json:"id"`
	Name                   string                   `json:"name"`
	AnalyticDocumentTypeID int                      `json:"analytic_document_type_id"`
	Sort                   int                      `json:"sort"`
	Active                 bool                     `json:"active"`
	Options                []AnalyticQuestionOption `json:"options"`
	CreatedAt              time.Time                `json:"created_at"`
	UpdatedAt              time.Time                `json:"updated_at"`
}

// AnalyticAnswer хранит выбранные ответы по одному вопросу в анкете аналитики.
type AnalyticAnswer struct {
	QuestionID  int64    `json:"question_id"`
	Question    string   `json:"question,omitempty"`
	OptionIDs   []int64  `json:"option_ids"`
	OptionNames []string `json:"option_names,omitempty"`
}

// Analytic — карточка аналитики / обзвона обратной связи по акту.
type Analytic struct {
	ID             int64            `json:"id"`
	Number         string           `json:"number"`
	CertificateID  int64            `json:"certificate_id"`
	Rating         int              `json:"rating"`
	Comment        string           `json:"comment"`
	Status         int              `json:"status"`
	Answers        []AnalyticAnswer `json:"answers"`
	ContractorID   int64            `json:"contractor_id,omitempty"`
	ContractorName string           `json:"contractor_name,omitempty"`
	EngineerID     int64            `json:"engineer_id,omitempty"`
	EngineerName   string           `json:"engineer_name,omitempty"`
	CreatedBy      *int64           `json:"created_by,omitempty"`
	Deleted        bool             `json:"deleted"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// Banner — информационный баннер (напоминание/памятка) для инженеров.
type Banner struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	FileID    *int64    `json:"file_id,omitempty"`
	FileURL   string    `json:"file_url,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AppSetting — глобальная настройка CRM (таблица crm_app_settings).
type AppSetting struct {
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// BannersForEngineerConfig хранит подборку баннеров до и после создания акта.
type BannersForEngineerConfig struct {
	BeforeCreate []int64 `json:"before_create"`
	AfterCreate  []int64 `json:"after_create"`
}

// DaDataAddressSuggestion — подсказка адреса из DaData.
type DaDataAddressSuggestion struct {
	Value             string `json:"value"`
	UnrestrictedValue string `json:"unrestricted_value"`
	PostalCode        string `json:"postal_code,omitempty"`
	City              string `json:"city,omitempty"`
	Street            string `json:"street,omitempty"`
	House             string `json:"house,omitempty"`
	Flat              string `json:"flat,omitempty"`
	GeoLat            string `json:"geo_lat,omitempty"`
	GeoLon            string `json:"geo_lon,omitempty"`
}

// Расширенные структуры фильтров для отчётов и списков.
type ReportFilter struct {
	From           time.Time
	To             time.Time
	EngineerID     *int64
	SubdivisionIDs []int64
	UserIDs        []int64
	PaymentMethods []string
}

type SubdivisionSumReportRow struct {
	SubdivisionID   int64              `json:"subdivision_id"`
	SubdivisionName string             `json:"subdivision_name"`
	PlannedSumTotal float64            `json:"planned_sum_total"`
	SumTotal        float64            `json:"sum_total"`
	ByPaymentOption map[string]float64 `json:"by_payment_option"`
}

type AmountSparePartsReport struct {
	TotalCostOfParts  float64 `json:"total_cost_of_parts"`
	CertificatesCount int     `json:"certificates_count"`
}

type CallConversionRow struct {
	TypeOfAppeal string          `json:"type_of_appeal"`
	Label        string          `json:"label"`
	Total        int             `json:"total"`
	Results      map[int]int     `json:"results"`     // key = CallResult (1,2,3,4,7)
	Percents     map[int]float64 `json:"percents"`    // % от Total по каждому результату
	OtherCount   int             `json:"other_count"` // 5,6,8,9,10 и неуказанные
	OtherPercent float64         `json:"other_percent"`
}

type CallConversionReport struct {
	Rows       []CallConversionRow `json:"rows"`
	TargetRow  CallConversionRow   `json:"target_row"`
	SummaryRow CallConversionRow   `json:"summary_row"`
}

type ContractorReportRow struct {
	ContractorID      int64    `json:"contractor_id"`
	Name              string   `json:"name"`
	Phones            []string `json:"phones"`
	Addresses         []string `json:"addresses"`
	CertificatesCount int      `json:"certificates_count"`
	FactSum           float64  `json:"fact_sum"`
	CostOfParts       float64  `json:"cost_of_parts"`
}

type EngineerReportRow struct {
	EngineerID        int64   `json:"engineer_id"`
	EngineerName      string  `json:"engineer_name"`
	CertificatesCount int     `json:"certificates_count"`
	ZeroSumCount      int     `json:"zero_sum_count"`
	ZeroPercent       float64 `json:"zero_percent"`
	TypeJobCertCount  int     `json:"type_job_cert_count"`
	TypeJobFactSum    float64 `json:"type_job_fact_sum"`
	PlannedSum        float64 `json:"planned_sum"`
	FactSum           float64 `json:"fact_sum"`
}

type TrafficSourceReportRow struct {
	TrafficSourceID             *int64  `json:"traffic_source_id,omitempty"`
	Name                        string  `json:"name"`
	Calls                       int     `json:"calls"`
	Applications                int     `json:"applications"`
	ApplicationConversion       float64 `json:"application_conversion"`
	ClosedApplications          int     `json:"closed_applications"`
	ClosedApplicationConversion float64 `json:"closed_application_conversion"`
	Sum                         float64 `json:"sum"`
	SumConversion               float64 `json:"sum_conversion"`
}

type TrafficSourceReport struct {
	Rows       []TrafficSourceReportRow `json:"rows"`
	SummaryRow TrafficSourceReportRow   `json:"summary_row"`
}

// IsValidExtendedContractCategory проверяет допустимость категории договора (1..10).
func IsValidExtendedContractCategory(cat string) bool {
	for _, c := range ExtendedContractCategories {
		if c.Key == cat {
			return true
		}
	}
	return false
}

// ContractCategoryLabel возвращает отображаемое название категории договора ТО.
func ContractCategoryLabel(cat string) string {
	for _, c := range ExtendedContractCategories {
		if c.Key == cat {
			return c.Label
		}
	}
	return cat
}
