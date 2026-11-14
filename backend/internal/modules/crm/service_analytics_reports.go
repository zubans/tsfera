package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// --- Traffic Sources ---

func (s *Service) ListTrafficSources(ctx context.Context, activeOnly bool) ([]TrafficSource, error) {
	return s.repo.ListTrafficSources(ctx, activeOnly)
}

func (s *Service) CreateTrafficSource(ctx context.Context, auth *platform.AuthContext, in TrafficSource) (*TrafficSource, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите название источника трафика", map[string]string{"name": "Обязательное поле"})
	}
	if in.TrafficSourceTypeID <= 0 {
		in.TrafficSourceTypeID = TrafficSourceTypeSearch
	}
	if err := s.repo.CreateTrafficSource(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "traffic_source", in.ID, "create", in)
	return &in, nil
}

func (s *Service) UpdateTrafficSource(ctx context.Context, auth *platform.AuthContext, id int64, in TrafficSource) (*TrafficSource, error) {
	existing, err := s.repo.GetTrafficSource(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите название источника трафика", map[string]string{"name": "Обязательное поле"})
	}
	existing.Name = in.Name
	if in.TrafficSourceTypeID > 0 {
		existing.TrafficSourceTypeID = in.TrafficSourceTypeID
	}
	existing.Sort = in.Sort
	existing.Active = in.Active
	if err := s.repo.UpdateTrafficSource(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "traffic_source", id, "update", existing)
	return existing, nil
}

func (s *Service) DeleteTrafficSource(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.DeleteTrafficSource(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "traffic_source", id, "delete", nil)
	return nil
}

// --- SIP Select ---

func (s *Service) ListSipUsersSelect(ctx context.Context) ([]SipUserSelectOption, error) {
	items, err := s.repo.ListSipUsersSelect(ctx)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range items {
		if items[i].UserID != nil {
			if u := s.resolveUserCached(ctx, *items[i].UserID, cache); u != nil {
				items[i].FullName = u.FullName
			}
		}
	}
	return items, nil
}

// --- Analytic Questions & Feedback Analytics ---

func normalizeQuestionOptions(in []AnalyticQuestionOption) []AnalyticQuestionOption {
	var nextID int64 = 1
	for _, o := range in {
		if o.ID >= nextID {
			nextID = o.ID + 1
		}
	}
	out := make([]AnalyticQuestionOption, 0, len(in))
	for i, o := range in {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			continue
		}
		id := o.ID
		if id <= 0 {
			id = nextID
			nextID++
		}
		sortOrder := o.Sort
		if sortOrder == 0 {
			sortOrder = (i + 1) * 10
		}
		out = append(out, AnalyticQuestionOption{ID: id, Name: name, Sort: sortOrder})
	}
	return out
}

func (s *Service) ListAnalyticQuestions(ctx context.Context, activeOnly bool) ([]AnalyticQuestion, error) {
	return s.repo.ListAnalyticQuestions(ctx, activeOnly)
}

func (s *Service) CreateAnalyticQuestion(ctx context.Context, auth *platform.AuthContext, in AnalyticQuestion) (*AnalyticQuestion, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите формулировку вопроса", map[string]string{"name": "Обязательное поле"})
	}
	if in.AnalyticDocumentTypeID <= 0 {
		in.AnalyticDocumentTypeID = AnalyticDocTypeCertificate
	}
	in.Options = normalizeQuestionOptions(in.Options)
	if err := s.repo.CreateAnalyticQuestion(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "analytic_question", in.ID, "create", in)
	return &in, nil
}

func (s *Service) UpdateAnalyticQuestion(ctx context.Context, auth *platform.AuthContext, id int64, in AnalyticQuestion) (*AnalyticQuestion, error) {
	existing, err := s.repo.GetAnalyticQuestion(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите формулировку вопроса", map[string]string{"name": "Обязательное поле"})
	}
	nextOptions := normalizeQuestionOptions(in.Options)
	keptIDs := make(map[int64]bool, len(nextOptions))
	for _, opt := range nextOptions {
		keptIDs[opt.ID] = true
	}
	// Нельзя удалять вариант ответа, уже использованный в сохранённых анкетах аналитики.
	for _, oldOpt := range existing.Options {
		if !keptIDs[oldOpt.ID] {
			used, err := s.repo.IsAnalyticOptionUsed(ctx, existing.ID, oldOpt.ID)
			if err != nil {
				return nil, err
			}
			if used {
				return nil, platform.NewRuleError(fmt.Sprintf("Вариант %q уже используется в анкетах и не может быть удалён", oldOpt.Name))
			}
		}
	}

	existing.Name = in.Name
	if in.AnalyticDocumentTypeID > 0 {
		existing.AnalyticDocumentTypeID = in.AnalyticDocumentTypeID
	}
	existing.Sort = in.Sort
	existing.Active = in.Active
	existing.Options = nextOptions
	if err := s.repo.UpdateAnalyticQuestion(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "analytic_question", id, "update", existing)
	return existing, nil
}

type SaveAnalyticInput struct {
	Number     string           `json:"number"`
	Rating     int              `json:"rating"`
	Comment    string           `json:"comment"`
	Status     int              `json:"status"`
	Answers    []AnalyticAnswer `json:"answers"`
	CreateTask bool             `json:"create_task"`
}

func (s *Service) ListAnalytics(ctx context.Context, f AnalyticFilter) ([]Analytic, error) {
	items, err := s.repo.ListAnalytics(ctx, f)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range items {
		if items[i].EngineerID > 0 {
			if u := s.resolveUserCached(ctx, items[i].EngineerID, cache); u != nil {
				items[i].EngineerName = u.FullName
			}
		}
	}
	return items, nil
}

func (s *Service) GetAnalytic(ctx context.Context, id int64) (*Analytic, error) {
	a, err := s.repo.GetAnalytic(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.EngineerID > 0 && s.users != nil {
		if u, _ := s.users.GetUserBrief(ctx, a.EngineerID); u != nil {
			a.EngineerName = u.FullName
		}
	}
	return a, nil
}

func (s *Service) SaveAnalyticByCertificate(ctx context.Context, auth *platform.AuthContext, certID int64, in SaveAnalyticInput) (*Analytic, error) {
	cert, err := s.repo.GetCertificate(ctx, certID)
	if err != nil {
		return nil, err
	}
	if cert == nil {
		return nil, platform.NewNotFound("Акт выполненных работ не найден")
	}
	if in.Rating < 0 || in.Rating > 5 {
		return nil, platform.NewValidation("Оценка должна быть от 0 до 5", map[string]string{"rating": "От 0 до 5"})
	}
	existing, err := s.repo.GetAnalyticByCertificate(ctx, certID)
	if err != nil {
		return nil, err
	}
	var uid *int64
	if auth != nil {
		idCopy := auth.UserID
		uid = &idCopy
	}
	a := &Analytic{
		CertificateID: certID,
		Number:        strings.TrimSpace(in.Number),
		Rating:        in.Rating,
		Comment:       strings.TrimSpace(in.Comment),
		Status:        in.Status,
		Answers:       in.Answers,
		CreatedBy:     uid,
	}
	if a.Number == "" {
		a.Number = fmt.Sprintf("АН-%d", certID)
	}
	if a.Status == 0 {
		a.Status = int(StatusProcessed)
	}
	if existing != nil {
		a.ID = existing.ID
	}
	if err := s.repo.UpsertAnalytic(ctx, a); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "analytic", a.ID, "save", a)

	if in.CreateTask {
		s.spawnCallFeedbackTask(ctx, cert, a)
	}
	return s.GetAnalytic(ctx, a.ID)
}

func (s *Service) SetAnalyticRating(ctx context.Context, auth *platform.AuthContext, id int64, rating int, comment string, createTask bool) (*Analytic, error) {
	if rating < 1 || rating > 5 {
		return nil, platform.NewValidation("Оценка должна быть от 1 до 5", map[string]string{"rating": "От 1 до 5"})
	}
	a, err := s.repo.GetAnalytic(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateAnalyticRating(ctx, id, rating, strings.TrimSpace(comment)); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "analytic", id, "rate", map[string]any{"rating": rating, "comment": comment})
	if createTask {
		if cert, _ := s.repo.GetCertificate(ctx, a.CertificateID); cert != nil {
			a.Rating = rating
			a.Comment = strings.TrimSpace(comment)
			s.spawnCallFeedbackTask(ctx, cert, a)
		}
	}
	return s.GetAnalytic(ctx, id)
}

func (s *Service) spawnCallFeedbackTask(ctx context.Context, cert *Certificate, analytic *Analytic) {
	var engSubID *int64
	if s.users != nil && cert.EngineerID > 0 {
		if eng, err := s.users.GetUserBrief(ctx, cert.EngineerID); err == nil && eng != nil {
			engSubID = eng.SubdivisionID
		}
	}
	var assignedUserID *int64
	if s.users != nil && cert.CreatedBy > 0 {
		if creator, err := s.users.GetUserBrief(ctx, cert.CreatedBy); err == nil && creator != nil && creator.SubdivisionID != nil {
			if sub, err := s.repo.GetSubdivision(ctx, *creator.SubdivisionID); err == nil && sub != nil && sub.SupervisorUserID != nil {
				sup := *sub.SupervisorUserID
				assignedUserID = &sup
			}
		}
	}
	if assignedUserID == nil && engSubID != nil {
		if sub, err := s.repo.GetSubdivision(ctx, *engSubID); err == nil && sub != nil && sub.SupervisorUserID != nil {
			sup := *sub.SupervisorUserID
			assignedUserID = &sup
		}
	}
	cid := cert.ContractorID
	aid := cert.AddressID
	task := &Task{
		ContractorID:    &cid,
		AddressID:       &aid,
		ApplicationID:   cert.ApplicationID,
		TaskType:        TaskTypeCallFeedback,
		Title:           fmt.Sprintf("Обратная связь по акту №%d (оценка %d/5)", cert.ID, analytic.Rating),
		Description:     analytic.Comment,
		Status:          TaskStatusNew,
		Priority:        80,
		AssignedUserID:  assignedUserID,
		SubdivisionID:   engSubID,
		PlanDatetimeTo:  s.now().Add(24 * time.Hour),
		DecisionTimeSec: 3600 * 8,
		IsUrgent:        analytic.Rating > 0 && analytic.Rating <= 3,
	}
	if err := s.repo.CreateTask(ctx, task); err == nil && assignedUserID != nil {
		s.notifyTaskAssigned(ctx, task)
	}
}

// --- Banners & App Settings ---

func (s *Service) ListBanners(ctx context.Context, activeOnly bool) ([]Banner, error) {
	return s.repo.ListBanners(ctx, activeOnly)
}

func (s *Service) CreateBanner(ctx context.Context, auth *platform.AuthContext, in Banner) (*Banner, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите название баннера", map[string]string{"name": "Обязательное поле"})
	}
	if err := s.repo.CreateBanner(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "banner", in.ID, "create", in)
	return &in, nil
}

func (s *Service) UpdateBanner(ctx context.Context, auth *platform.AuthContext, id int64, in Banner) (*Banner, error) {
	existing, err := s.repo.GetBanner(ctx, id)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, platform.NewValidation("Укажите название баннера", map[string]string{"name": "Обязательное поле"})
	}
	existing.Name = in.Name
	existing.Text = in.Text
	existing.FileID = in.FileID
	existing.Active = in.Active
	if err := s.repo.UpdateBanner(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "banner", id, "update", existing)
	return existing, nil
}

func (s *Service) DeleteBanner(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.DeleteBanner(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "banner", id, "delete", nil)
	return nil
}

func (s *Service) ListAppSettings(ctx context.Context) ([]AppSetting, error) {
	return s.repo.ListAppSettings(ctx)
}

func (s *Service) GetAppSetting(ctx context.Context, key string) (*AppSetting, error) {
	return s.repo.GetAppSetting(ctx, key)
}

func (s *Service) SaveAppSetting(ctx context.Context, auth *platform.AuthContext, key string, name string, data json.RawMessage) (*AppSetting, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, platform.NewValidation("Укажите ключ настройки", nil)
	}
	if strings.TrimSpace(name) == "" {
		name = key
	}
	setting := &AppSetting{Key: key, Name: strings.TrimSpace(name), Data: data}
	if err := s.repo.SaveAppSetting(ctx, setting); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "app_setting", 0, "save", setting)
	return setting, nil
}

// GetBannersForEngineerResolved возвращает активные баннеры для показа инженеру до и после создания акта.
func (s *Service) GetBannersForEngineerResolved(ctx context.Context) (map[string][]Banner, error) {
	out := map[string][]Banner{
		"before_create": {},
		"after_create":  {},
	}
	setting, err := s.repo.GetAppSetting(ctx, "app.banners_for_engineer")
	if err != nil {
		// Если настройка ещё не создана, возвращаем пустые списки без ошибки.
		return out, nil
	}
	var cfg BannersForEngineerConfig
	if err := json.Unmarshal(setting.Data, &cfg); err != nil {
		return out, nil
	}
	all, err := s.repo.ListBanners(ctx, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Banner, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	for _, id := range cfg.BeforeCreate {
		if b, ok := byID[id]; ok {
			out["before_create"] = append(out["before_create"], b)
		}
	}
	for _, id := range cfg.AfterCreate {
		if b, ok := byID[id]; ok {
			out["after_create"] = append(out["after_create"], b)
		}
	}
	return out, nil
}

// --- Extended Reports (with User/Subdivision Resolution) ---

func (s *Service) AmountActsReportFiltered(ctx context.Context, auth *platform.AuthContext, f ReportFilter) ([]AmountActsReportRow, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil && len(f.SubdivisionIDs) == 0 {
		f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	}
	if f.EngineerID != nil && len(f.UserIDs) == 0 {
		f.UserIDs = []int64{*f.EngineerID}
	}
	rows, err := s.repo.AmountActsReportFiltered(ctx, f)
	if err != nil {
		return nil, err
	}
	subs, _ := s.repo.ListSubdivisions(ctx)
	subNames := map[int64]string{}
	for _, sb := range subs {
		subNames[sb.ID] = sb.Name
	}
	cache := map[int64]*UserBrief{}
	filtered := make([]AmountActsReportRow, 0, len(rows))
	for _, row := range rows {
		if u := s.resolveUserCached(ctx, row.EngineerID, cache); u != nil {
			row.EngineerName = u.FullName
			row.SubdivisionID = u.SubdivisionID
			if u.SubdivisionID != nil {
				row.SubdivisionName = subNames[*u.SubdivisionID]
			}
		} else {
			row.EngineerName = fmt.Sprintf("Инженер #%d", row.EngineerID)
		}
		filtered = append(filtered, row)
	}
	return filtered, nil
}

func (s *Service) ExpiredTasksReportFiltered(ctx context.Context, from, to *time.Time, userID *int64) ([]ExpiredTaskReportRow, error) {
	rows, err := s.repo.ExpiredTasksReportFiltered(ctx, s.now(), from, to, userID)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range rows {
		if rows[i].AssignedUserID != nil {
			if u := s.resolveUserCached(ctx, *rows[i].AssignedUserID, cache); u != nil {
				rows[i].AssignedName = u.FullName
			}
		}
	}
	return rows, nil
}

func (s *Service) SubdivisionSumsReport(ctx context.Context, auth *platform.AuthContext, f ReportFilter) ([]SubdivisionSumReportRow, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil && len(f.SubdivisionIDs) == 0 {
		f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	}
	return s.repo.SubdivisionSumsReport(ctx, f)
}

func (s *Service) AmountSparePartsReport(ctx context.Context, auth *platform.AuthContext, f ReportFilter) (*AmountSparePartsReport, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil && len(f.SubdivisionIDs) == 0 {
		f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	}
	return s.repo.AmountSparePartsReport(ctx, f)
}

func (s *Service) CallConversionReport(ctx context.Context, f ReportFilter) (*CallConversionReport, error) {
	return s.repo.CallConversionReport(ctx, f)
}

func (s *Service) ContractorsReport(ctx context.Context, auth *platform.AuthContext, f ReportFilter) ([]ContractorReportRow, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil && len(f.SubdivisionIDs) == 0 {
		f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	}
	return s.repo.ContractorsReport(ctx, f)
}

func (s *Service) EngineersReport(ctx context.Context, auth *platform.AuthContext, f ReportFilter) ([]EngineerReportRow, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil && len(f.SubdivisionIDs) == 0 {
		f.SubdivisionIDs = []int64{*auth.SubdivisionID}
	}
	rows, err := s.repo.EngineersReport(ctx, f)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range rows {
		if u := s.resolveUserCached(ctx, rows[i].EngineerID, cache); u != nil {
			rows[i].EngineerName = u.FullName
		} else {
			rows[i].EngineerName = fmt.Sprintf("Инженер #%d", rows[i].EngineerID)
		}
	}
	return rows, nil
}

func (s *Service) TrafficSourcesReport(ctx context.Context, f ReportFilter) (*TrafficSourceReport, error) {
	return s.repo.TrafficSourcesReport(ctx, f)
}
