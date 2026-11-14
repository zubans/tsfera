package crm

import (
	"context"
	"encoding/json"
	"time"
)

type UserBrief struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	FullName      string `json:"full_name"`
	Color         string `json:"color"`
	SubdivisionID *int64 `json:"subdivision_id,omitempty"`
	IsEngineer    bool   `json:"is_engineer"`
	IsDispatcher  bool   `json:"is_dispatcher"`
	Active        bool   `json:"active"`
}

// UserDirectory абстрагирует чтение сотрудников из модуля users для слабой связности модулей.
type UserDirectory interface {
	GetUserBrief(ctx context.Context, userID int64) (*UserBrief, error)
	ListActiveWorkers(ctx context.Context, isEngineer, isDispatcher bool, subdivisionID *int64) ([]UserBrief, error)
	ListUserIDsByPermission(ctx context.Context, perm string) ([]int64, error)
}

type ContractorFilter struct {
	Query           string
	Limit           int
	Offset          int
	EngineerID      *int64
	SubdivisionID   *int64
	OnlyLegal       bool
	OnlyIndividuals bool
	Now             time.Time
}

type ApplicationFilter struct {
	From          *time.Time
	To            *time.Time
	EngineerID    *int64
	SubdivisionID *int64
	ContractorID  *int64
	Status        *DocumentStatus
	IsComplete    *bool
	OrderBy       string
	Limit         int
	Offset        int
}

type CertificateFilter struct {
	From          *time.Time
	To            *time.Time
	EngineerID    *int64
	ContractorID  *int64
	ApplicationID *int64
	Limit         int
	Offset        int
}

type TaskFilter struct {
	AssignedUserID *int64
	SubdivisionID  *int64
	Status         *TaskStatus
	TaskType       *TaskType
	ContractorID   *int64
	OnlyUrgent     bool
	Limit          int
	Offset         int
}

type ContractFilter struct {
	ContractorID            *int64
	AddressID               *int64
	Status                  *DocumentStatus
	From                    *time.Time
	To                      *time.Time
	OnlyNeedingProlongation bool
	Limit                   int
	Offset                  int
}

type CallFilter struct {
	Phone            string
	ContractorID     *int64
	UserID           *int64
	SubdivisionID    *int64
	Directions       []string
	Statuses         []string
	TypesOfAppeal    []string
	CallerTypes      []int
	CallResults      []int
	TrafficSourceIDs []int64
	From             *time.Time
	To               *time.Time
	Limit            int
	Offset           int
}

type AnalyticFilter struct {
	From          *time.Time
	To            *time.Time
	CertificateID *int64
	ContractorID  *int64
	EngineerID    *int64
	Rating        *int
	Status        *int
	Limit         int
	Offset        int
}

type Repository interface {
	// Subdivisions & SIP
	ListSubdivisions(ctx context.Context) ([]Subdivision, error)
	CreateSubdivision(ctx context.Context, sub *Subdivision) error
	UpdateSubdivision(ctx context.Context, sub *Subdivision) error
	ListSipAccounts(ctx context.Context) ([]SipAccount, error)
	GetSipAccountByUser(ctx context.Context, userID int64) (*SipAccount, error)
	GetSipAccountByLogin(ctx context.Context, login string) (*SipAccount, error)
	GetSipAccountByAbonentID(ctx context.Context, abonentID string) (*SipAccount, error)
	SaveSipAccount(ctx context.Context, sip *SipAccount) error

	// Contractors, phones, emails, addresses
	SearchContractors(ctx context.Context, f ContractorFilter) ([]Contractor, int, error)
	GetContractor(ctx context.Context, id int64) (*Contractor, error)
	CreateContractor(ctx context.Context, c *Contractor) error
	UpdateContractor(ctx context.Context, c *Contractor) error
	SoftDeleteContractor(ctx context.Context, id int64) error
	FindContractorByPhone(ctx context.Context, phone10 string) (*Contractor, error)
	FindUniqueContractorByPhone(ctx context.Context, phone10 string) (*Contractor, error)
	CheckSimilarContractors(ctx context.Context, excludeID *int64, phones10 []string, emails []string) ([]SimilarContractorMatch, error)
	DetachPhoneFromOtherContractors(ctx context.Context, phone10 string, keepContractorID int64) error
	ReplaceContractorPhones(ctx context.Context, contractorID int64, phones []ContractorPhone) error
	ReplaceContractorEmails(ctx context.Context, contractorID int64, emails []ContractorEmail) error

	GetAddress(ctx context.Context, id int64) (*ContractorAddress, error)
	CreateAddress(ctx context.Context, addr *ContractorAddress) error
	UpdateAddress(ctx context.Context, addr *ContractorAddress) error
	SoftDeleteAddress(ctx context.Context, id int64) error
	CountAddressLinks(ctx context.Context, addressID int64) (equipments int, contracts int, apps int, certs int, err error)
	SearchAddresses(ctx context.Context, contractorID *int64, query string, limit int) ([]ContractorAddress, error)

	// Equipment & References
	ListEquipments(ctx context.Context, query string, limit int) ([]Equipment, error)
	CreateEquipment(ctx context.Context, eq *Equipment) error
	UpdateEquipment(ctx context.Context, eq *Equipment) error
	AttachAddressEquipment(ctx context.Context, ae *AddressEquipment) error
	SoftDeleteAddressEquipment(ctx context.Context, id int64) error
	ListRefItems(ctx context.Context, kind string) ([]RefItem, error)
	CreateRefItem(ctx context.Context, item *RefItem) error
	SoftDeleteRefItem(ctx context.Context, id int64) error

	// Contracts
	ListContracts(ctx context.Context, f ContractFilter) ([]MaintenanceContract, error)
	GetContract(ctx context.Context, id int64) (*MaintenanceContract, error)
	CreateContract(ctx context.Context, c *MaintenanceContract) error
	UpdateContract(ctx context.Context, c *MaintenanceContract) error
	SoftDeleteContract(ctx context.Context, id int64) error
	ListContractsNeedingQuarterlyTask(ctx context.Context, now time.Time) ([]MaintenanceContract, error)
	ListContractsNeedingProlongationTask(ctx context.Context, now time.Time) ([]MaintenanceContract, error)

	// Applications & Calendar
	HasEngineerVisibleApplicationInWindow(ctx context.Context, engineerID, contractorID int64, from, to time.Time) (bool, error)
	HasEngineerTimeOverlap(ctx context.Context, engineerID int64, from, to time.Time, excludeAppID *int64) (bool, error)
	ListApplications(ctx context.Context, f ApplicationFilter) ([]Application, error)
	ListCalendarEvents(ctx context.Context, from, to time.Time, engineerID *int64, subdivisionID *int64) ([]CalendarEvent, error)
	GetApplication(ctx context.Context, id int64) (*Application, error)
	CreateApplication(ctx context.Context, app *Application) error
	UpdateApplication(ctx context.Context, app *Application) error
	UpdateApplicationRating(ctx context.Context, id int64, rating int, comment string) error
	MarkApplicationNotesViewed(ctx context.Context, id int64) error
	SoftDeleteApplication(ctx context.Context, id int64) error

	// Certificates
	ListCertificates(ctx context.Context, f CertificateFilter) ([]Certificate, error)
	GetCertificate(ctx context.Context, id int64) (*Certificate, error)
	CreateCertificate(ctx context.Context, cert *Certificate) error
	UpdateCertificate(ctx context.Context, cert *Certificate) error
	SoftDeleteCertificate(ctx context.Context, id int64) error

	// Tasks & Pool
	ListTasks(ctx context.Context, f TaskFilter) ([]Task, error)
	GetTask(ctx context.Context, id int64) (*Task, error)
	CreateTask(ctx context.Context, task *Task) error
	UpdateTask(ctx context.Context, task *Task) error
	SoftDeleteTask(ctx context.Context, id int64) error
	ListUnassignedActiveTasks(ctx context.Context, limit int) ([]Task, error)
	CountUserActivePoolTasks(ctx context.Context, userID int64, transferred bool) (int, error)
	ListOverdueActiveTasks(ctx context.Context, now time.Time) ([]Task, error)
	ListTaskPoolSettings(ctx context.Context) ([]TaskPoolSetting, error)
	SaveTaskPoolSetting(ctx context.Context, s *TaskPoolSetting) error
	ListQueueUsers(ctx context.Context) ([]QueueUser, error)
	TouchQueueUser(ctx context.Context, userID int64, assignedAt time.Time) error

	// Calls
	ListCalls(ctx context.Context, f CallFilter) ([]CallRecord, error)
	GetCall(ctx context.Context, id int64) (*CallRecord, error)
	GetCallByExternalID(ctx context.Context, externalCallID string) (*CallRecord, error)
	UpsertCallByExternalID(ctx context.Context, call *CallRecord) error
	UpdateCallCard(ctx context.Context, call *CallRecord) error

	// Reports, Audit, Notifications, User Settings
	AmountActsReport(ctx context.Context, from, to time.Time, engineerID *int64) ([]AmountActsReportRow, error)
	ExpiredTasksReport(ctx context.Context, now time.Time, userID *int64) ([]ExpiredTaskReportRow, error)
	AddChangeLog(ctx context.Context, entry ChangeLog) error
	ListChangeLogs(ctx context.Context, entityType string, entityID int64, limit int) ([]ChangeLog, error)
	CreateNotification(ctx context.Context, n *Notification) error
	ListUserNotifications(ctx context.Context, userID int64, unreadOnly bool, limit int) ([]Notification, error)
	MarkNotificationRead(ctx context.Context, userID, notificationID int64) error
	MarkAllNotificationsRead(ctx context.Context, userID int64) error
	DeleteOldReadNotifications(ctx context.Context, olderThan time.Time) (int64, error)
	GetUserSetting(ctx context.Context, userID int64, key string) (json.RawMessage, error)
	SaveUserSetting(ctx context.Context, userID int64, key string, val json.RawMessage) error

	// Subdivisions & SIP (extended)
	GetSubdivision(ctx context.Context, id int64) (*Subdivision, error)
	SoftDeleteSubdivision(ctx context.Context, id int64) error
	ListSipUsersSelect(ctx context.Context) ([]SipUserSelectOption, error)

	// Legal entities & addresses (extended)
	UpsertLegalEntity(ctx context.Context, le *LegalEntity) error
	FindAddressByText(ctx context.Context, contractorID int64, address string) (*ContractorAddress, error)

	// Traffic sources
	ListTrafficSources(ctx context.Context, activeOnly bool) ([]TrafficSource, error)
	GetTrafficSource(ctx context.Context, id int64) (*TrafficSource, error)
	CreateTrafficSource(ctx context.Context, ts *TrafficSource) error
	UpdateTrafficSource(ctx context.Context, ts *TrafficSource) error
	DeleteTrafficSource(ctx context.Context, id int64) error

	// Applications & Certificates (extended)
	CountRecentContractorApplications(ctx context.Context, contractorID int64, since time.Time) (int, error)
	HasRecentAppLimitTask(ctx context.Context, contractorID int64, since time.Time) (bool, error)
	FindMatchingContractForCertificate(ctx context.Context, contractorID, addressID int64, workDate time.Time) (*int64, error)

	// Missed call pool
	ListMissedCallsToSync(ctx context.Context, since time.Time) ([]CallRecord, error)
	AddToMissCallPool(ctx context.Context, callID, userID int64) error
	ListMissCallPool(ctx context.Context, userID *int64, onlyUnviewed bool, limit int) ([]MissCallPoolItem, error)
	MarkMissCallViewed(ctx context.Context, callID int64, userID *int64) error
	MarkMissCallsViewedByPhone(ctx context.Context, phone10 string, userID *int64) error

	// Analytics & AnalyticQuestions
	ListAnalyticQuestions(ctx context.Context, activeOnly bool) ([]AnalyticQuestion, error)
	GetAnalyticQuestion(ctx context.Context, id int64) (*AnalyticQuestion, error)
	CreateAnalyticQuestion(ctx context.Context, q *AnalyticQuestion) error
	UpdateAnalyticQuestion(ctx context.Context, q *AnalyticQuestion) error
	IsAnalyticOptionUsed(ctx context.Context, questionID, optionID int64) (bool, error)
	ListAnalytics(ctx context.Context, f AnalyticFilter) ([]Analytic, error)
	GetAnalytic(ctx context.Context, id int64) (*Analytic, error)
	GetAnalyticByCertificate(ctx context.Context, certID int64) (*Analytic, error)
	UpsertAnalytic(ctx context.Context, a *Analytic) error
	UpdateAnalyticRating(ctx context.Context, id int64, rating int, comment string) error

	// Banners & AppSettings
	ListBanners(ctx context.Context, activeOnly bool) ([]Banner, error)
	GetBanner(ctx context.Context, id int64) (*Banner, error)
	CreateBanner(ctx context.Context, b *Banner) error
	UpdateBanner(ctx context.Context, b *Banner) error
	DeleteBanner(ctx context.Context, id int64) error
	ListAppSettings(ctx context.Context) ([]AppSetting, error)
	GetAppSetting(ctx context.Context, key string) (*AppSetting, error)
	SaveAppSetting(ctx context.Context, s *AppSetting) error

	// Extended Reports
	AmountActsReportFiltered(ctx context.Context, f ReportFilter) ([]AmountActsReportRow, error)
	ExpiredTasksReportFiltered(ctx context.Context, now time.Time, from, to *time.Time, userID *int64) ([]ExpiredTaskReportRow, error)
	SubdivisionSumsReport(ctx context.Context, f ReportFilter) ([]SubdivisionSumReportRow, error)
	AmountSparePartsReport(ctx context.Context, f ReportFilter) (*AmountSparePartsReport, error)
	CallConversionReport(ctx context.Context, f ReportFilter) (*CallConversionReport, error)
	ContractorsReport(ctx context.Context, f ReportFilter) ([]ContractorReportRow, error)
	EngineersReport(ctx context.Context, f ReportFilter) ([]EngineerReportRow, error)
	TrafficSourcesReport(ctx context.Context, f ReportFilter) (*TrafficSourceReport, error)
}
