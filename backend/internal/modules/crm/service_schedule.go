package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// CalendarLoadSettingsKey — ключ глобальной настройки расписания инженеров.
const CalendarLoadSettingsKey = "calendar_load"

// CalendarLoadSettings — параметры расчёта загрузки. Хранятся в глобальных настройках CRM.
type CalendarLoadSettings struct {
	DefaultWorkFrom string  `json:"default_work_from"` // окно по умолчанию для инженеров без табеля
	DefaultWorkTo   string  `json:"default_work_to"`
	MinFreeMinutes  int     `json:"min_free_minutes"`  // окно не короче порога выделяется явно
	MinShownMinutes int     `json:"min_shown_minutes"` // более короткие разрывы не показываются
	GridFrom        string  `json:"grid_from"`         // границы сетки расписания
	GridTo          string  `json:"grid_to"`
	ServiceUserIDs  []int64 `json:"service_user_ids"` // служебные учётки («Офис»): без загрузки и плана
}

func defaultCalendarLoadSettings() CalendarLoadSettings {
	return CalendarLoadSettings{
		DefaultWorkFrom: "08:00",
		DefaultWorkTo:   "17:00",
		MinFreeMinutes:  60,
		MinShownMinutes: 15,
		GridFrom:        "07:00",
		GridTo:          "23:00",
		ServiceUserIDs:  []int64{},
	}
}

func (s *Service) calendarLoadSettings(ctx context.Context) CalendarLoadSettings {
	cfg := defaultCalendarLoadSettings()
	setting, err := s.repo.GetAppSetting(ctx, CalendarLoadSettingsKey)
	if err != nil || setting == nil || len(setting.Data) == 0 {
		return cfg
	}
	var stored CalendarLoadSettings
	if err := json.Unmarshal(setting.Data, &stored); err != nil {
		return cfg
	}
	if _, ok := parseClock(stored.DefaultWorkFrom); ok {
		cfg.DefaultWorkFrom = stored.DefaultWorkFrom
	}
	if _, ok := parseClock(stored.DefaultWorkTo); ok {
		cfg.DefaultWorkTo = stored.DefaultWorkTo
	}
	if _, ok := parseClock(stored.GridFrom); ok {
		cfg.GridFrom = stored.GridFrom
	}
	if _, ok := parseClock(stored.GridTo); ok {
		cfg.GridTo = stored.GridTo
	}
	if stored.MinFreeMinutes > 0 {
		cfg.MinFreeMinutes = stored.MinFreeMinutes
	}
	if stored.MinShownMinutes > 0 {
		cfg.MinShownMinutes = stored.MinShownMinutes
	}
	if stored.ServiceUserIDs != nil {
		cfg.ServiceUserIDs = stored.ServiceUserIDs
	}
	return cfg
}

// parseClock разбирает время суток «ЧЧ:ММ»; «24:00» допустимо как конец суток.
func parseClock(raw string) (time.Duration, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(raw))
	if err != nil {
		if strings.TrimSpace(raw) == "24:00" {
			return 24 * time.Hour, true
		}
		return 0, false
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, true
}

// ParseLocalDate разбирает дату «ГГГГ-ММ-ДД» как начало суток по московскому времени.
func (s *Service) ParseLocalDate(raw string) (time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), s.moscowLoc())
	if err != nil {
		return time.Time{}, platform.NewValidation("Дата должна быть в формате ГГГГ-ММ-ДД", map[string]string{"date": "Неверный формат"})
	}
	return d, nil
}

func (s *Service) startOfLocalDay(t time.Time) time.Time {
	l := t.In(s.moscowLoc())
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, s.moscowLoc())
}

// --- Табель ---

// ListTimesheet возвращает записи табеля за период. Без права просмотра табеля — только свои.
func (s *Service) ListTimesheet(ctx context.Context, auth *platform.AuthContext, from, to time.Time, subdivisionID, userID *int64) ([]TimesheetEntry, error) {
	if !to.After(from) {
		return nil, platform.NewValidation("Конец периода должен быть позже начала", nil)
	}
	f := TimesheetFilter{From: from, To: to}
	if auth != nil && !auth.HasPermission("crm.timesheet.view") && !auth.HasPermission("crm.timesheet.manage") {
		f.UserIDs = []int64{auth.UserID}
	} else if userID != nil {
		f.UserIDs = []int64{*userID}
	}
	entries, err := s.repo.ListTimesheetEntries(ctx, f)
	if err != nil {
		return nil, err
	}
	if subdivisionID == nil || s.users == nil {
		return entries, nil
	}
	members := map[int64]bool{}
	if workers, err := s.users.ListActiveWorkers(ctx, false, false, subdivisionID); err == nil {
		for _, w := range workers {
			members[w.ID] = true
		}
	}
	out := make([]TimesheetEntry, 0, len(entries))
	for _, e := range entries {
		if (e.SubdivisionID != nil && *e.SubdivisionID == *subdivisionID) || (e.SubdivisionID == nil && members[e.UserID]) {
			out = append(out, e)
		}
	}
	return out, nil
}

func validateTimesheetEntry(e *TimesheetEntry) error {
	e.Comment = strings.TrimSpace(e.Comment)
	if e.UserID <= 0 {
		return platform.NewValidation("Укажите сотрудника", map[string]string{"user_id": "Обязательное поле"})
	}
	if !e.Kind.Valid() {
		return platform.NewValidation("Неизвестный вид записи табеля", map[string]string{"kind": "Неверное значение"})
	}
	if !e.EndsAt.After(e.StartsAt) {
		return platform.NewValidation("Окончание должно быть позже начала", map[string]string{"ends_at": "Неверный интервал"})
	}
	if e.Kind.IsShift() && e.EndsAt.Sub(e.StartsAt) > 48*time.Hour {
		return platform.NewValidation("Смена не может быть длиннее 48 часов", map[string]string{"ends_at": "Слишком длинная смена"})
	}
	if e.Kind == TimesheetReserve && e.Comment == "" {
		return platform.NewValidation("Для резерва укажите причину", map[string]string{"comment": "Обязательное поле"})
	}
	return nil
}

func (s *Service) CreateTimesheetEntry(ctx context.Context, auth *platform.AuthContext, in TimesheetEntry) (*TimesheetEntry, error) {
	if err := validateTimesheetEntry(&in); err != nil {
		return nil, err
	}
	if auth != nil {
		in.CreatedBy = auth.UserID
	}
	if err := s.repo.CreateTimesheetEntry(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "timesheet", in.ID, "create", in)
	s.broadcastScheduleUpdate()
	return &in, nil
}

func (s *Service) UpdateTimesheetEntry(ctx context.Context, auth *platform.AuthContext, id int64, in TimesheetEntry) (*TimesheetEntry, error) {
	existing, err := s.repo.GetTimesheetEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Запись табеля не найдена")
	}
	in.ID = id
	if err := validateTimesheetEntry(&in); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateTimesheetEntry(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "timesheet", id, "update", in)
	s.broadcastScheduleUpdate()
	return s.repo.GetTimesheetEntry(ctx, id)
}

func (s *Service) DeleteTimesheetEntry(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.DeleteTimesheetEntry(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "timesheet", id, "delete", nil)
	s.broadcastScheduleUpdate()
	return nil
}

// GenerateTimesheetInput — заполнение смен по шаблону графика.
type GenerateTimesheetInput struct {
	UserID        int64         `json:"user_id"`
	SubdivisionID *int64        `json:"subdivision_id"`
	Pattern       ShiftPattern  `json:"pattern"`
	Kind          TimesheetKind `json:"kind"`
	CycleStart    string        `json:"cycle_start"` // первый рабочий день цикла для сменных графиков
	DateFrom      string        `json:"date_from"`
	DateTo        string        `json:"date_to"` // включительно
	TimeFrom      string        `json:"time_from"`
	TimeTo        string        `json:"time_to"` // не позже начала — смена до следующего дня
	Replace       bool          `json:"replace"` // снять прежние смены периода
}

// GenerateTimesheet создаёт смены по шаблону и возвращает их число.
func (s *Service) GenerateTimesheet(ctx context.Context, auth *platform.AuthContext, in GenerateTimesheetInput) (int, error) {
	if in.UserID <= 0 {
		return 0, platform.NewValidation("Укажите сотрудника", map[string]string{"user_id": "Обязательное поле"})
	}
	if in.Kind == "" {
		in.Kind = TimesheetWork
	}
	if !in.Kind.IsShift() {
		return 0, platform.NewValidation("По шаблону заполняются только рабочие и дежурные смены", map[string]string{"kind": "Неверное значение"})
	}
	from, err := s.ParseLocalDate(in.DateFrom)
	if err != nil {
		return 0, err
	}
	toIncl, err := s.ParseLocalDate(in.DateTo)
	if err != nil {
		return 0, err
	}
	to := toIncl.AddDate(0, 0, 1)
	if !to.After(from) || to.Sub(from) > 93*24*time.Hour {
		return 0, platform.NewValidation("Период — от одного дня до трёх месяцев", map[string]string{"date_to": "Неверный период"})
	}
	anchor := from
	if strings.TrimSpace(in.CycleStart) != "" {
		if anchor, err = s.ParseLocalDate(in.CycleStart); err != nil {
			return 0, err
		}
	}
	startClock, ok1 := parseClock(in.TimeFrom)
	endClock, ok2 := parseClock(in.TimeTo)
	if !ok1 || !ok2 {
		return 0, platform.NewValidation("Время смены — в формате ЧЧ:ММ", map[string]string{"time_from": "Неверный формат"})
	}
	shifts, ok := GenerateShifts(in.Pattern, anchor, from, to, startClock, endClock, s.moscowLoc())
	if !ok {
		return 0, platform.NewValidation("Неизвестный шаблон графика", map[string]string{"pattern": "Допустимо 5/2, 2/2, 3/3, 1/1"})
	}
	if in.Replace {
		if err := s.repo.DeleteTimesheetShifts(ctx, in.UserID, from, to); err != nil {
			return 0, err
		}
	}
	var createdBy int64
	if auth != nil {
		createdBy = auth.UserID
	}
	for _, sh := range shifts {
		e := TimesheetEntry{UserID: in.UserID, SubdivisionID: in.SubdivisionID, Kind: in.Kind, StartsAt: sh.From, EndsAt: sh.To, CreatedBy: createdBy}
		if err := s.repo.CreateTimesheetEntry(ctx, &e); err != nil {
			return 0, err
		}
	}
	s.logChange(ctx, auth, "timesheet", in.UserID, "generate", in)
	s.broadcastScheduleUpdate()
	return len(shifts), nil
}

func (s *Service) broadcastScheduleUpdate() {
	if s.hub != nil {
		s.hub.BroadcastAll(WSEvent{Type: "calendar:updated", Payload: map[string]any{"timesheet": true}})
	}
}

// --- Загрузка инженеров ---

// EngineerDayLoad — колонка инженера в расписании на день.
type EngineerDayLoad struct {
	UserID            int64   `json:"user_id"`
	FullName          string  `json:"full_name"`
	Color             string  `json:"color"`
	SubdivisionID     *int64  `json:"subdivision_id,omitempty"`
	IsService         bool    `json:"is_service"`
	ApplicationsCount int     `json:"applications_count"`
	CompletedCount    int     `json:"completed_count"`
	PlannedSum        float64 `json:"planned_sum"`
	DayLoad
}

// CalendarLoadResult — расписание подразделения на день.
type CalendarLoadResult struct {
	Date      string            `json:"date"`
	GridFrom  string            `json:"grid_from"`
	GridTo    string            `json:"grid_to"`
	MinFree   int               `json:"min_free_minutes"`
	Engineers []EngineerDayLoad `json:"engineers"`
	Totals    struct {
		Applications int     `json:"applications"`
		Engineers    int     `json:"engineers"`
		PlannedSum   float64 `json:"planned_sum"`
	} `json:"totals"`
}

func (s *Service) scopeCalendar(auth *platform.AuthContext, engineerID, subdivisionID *int64) (*int64, *int64, error) {
	if auth == nil {
		return engineerID, subdivisionID, nil
	}
	if !auth.HasPermission("crm.calendar.select_user") {
		uid := auth.UserID
		engineerID = &uid
	}
	if !auth.HasPermission("crm.calendar.select_subdivision") {
		if auth.SubdivisionID == nil {
			return nil, nil, platform.NewForbidden("Не назначено подразделение календаря")
		}
		subID := *auth.SubdivisionID
		subdivisionID = &subID
	}
	return engineerID, subdivisionID, nil
}

// CalendarLoad считает загрузку и свободные окна инженеров подразделения на день.
// В колонки попадают инженеры подразделения и те, кто в этот день работает в нём по табелю;
// инженер, у которого все смены дня в другом городе, показывается там.
func (s *Service) CalendarLoad(ctx context.Context, auth *platform.AuthContext, day time.Time, engineerID, subdivisionID *int64) (*CalendarLoadResult, error) {
	engineerID, subdivisionID, err := s.scopeCalendar(auth, engineerID, subdivisionID)
	if err != nil {
		return nil, err
	}
	if s.users == nil {
		return nil, platform.NewNotConfigured("Справочник сотрудников недоступен")
	}
	cfg := s.calendarLoadSettings(ctx)
	dayIv := Interval{From: day, To: day.AddDate(0, 0, 1)}

	workers, err := s.users.ListActiveWorkers(ctx, true, false, nil)
	if err != nil {
		return nil, err
	}
	byID := map[int64]UserBrief{}
	for _, w := range workers {
		byID[w.ID] = w
	}
	entries, err := s.repo.ListTimesheetEntries(ctx, TimesheetFilter{From: dayIv.From, To: dayIv.To})
	if err != nil {
		return nil, err
	}
	entriesByUser := map[int64][]TimesheetEntry{}
	for _, e := range entries {
		entriesByUser[e.UserID] = append(entriesByUser[e.UserID], e)
	}
	// Окно по умолчанию — только для тех, кого ещё не ведут в табеле: если записи есть в соседние недели,
	// пустой день — выходной, а не «график неизвестен».
	nearby, err := s.repo.ListTimesheetEntries(ctx, TimesheetFilter{From: dayIv.From.AddDate(0, 0, -35), To: dayIv.To.AddDate(0, 0, 35)})
	if err != nil {
		return nil, err
	}
	tracked := map[int64]bool{}
	for _, e := range nearby {
		tracked[e.UserID] = true
	}

	var ids []int64
	for _, w := range workers {
		if engineerID != nil && w.ID != *engineerID {
			continue
		}
		if subdivisionID != nil && !worksInSubdivision(w, entriesByUser[w.ID], *subdivisionID) {
			continue
		}
		ids = append(ids, w.ID)
	}
	out := &CalendarLoadResult{
		Date:      day.Format("2006-01-02"),
		GridFrom:  cfg.GridFrom,
		GridTo:    cfg.GridTo,
		MinFree:   cfg.MinFreeMinutes,
		Engineers: []EngineerDayLoad{},
	}
	if len(ids) == 0 {
		return out, nil
	}
	busy, err := s.repo.ListBusyIntervals(ctx, ids, dayIv.From, dayIv.To)
	if err != nil {
		return nil, err
	}
	busyByUser := map[int64][]BusyInterval{}
	for _, b := range busy {
		busyByUser[b.UserID] = append(busyByUser[b.UserID], b)
	}
	var defaultWork []Interval
	if from, ok := parseClock(cfg.DefaultWorkFrom); ok {
		if to, ok := parseClock(cfg.DefaultWorkTo); ok && to > from {
			defaultWork = []Interval{{From: day.Add(from), To: day.Add(to)}}
		}
	}
	appIDs := map[int64]bool{}
	for _, id := range ids {
		w := byID[id]
		var ivs []Interval
		row := EngineerDayLoad{UserID: id, FullName: w.FullName, Color: w.Color, SubdivisionID: w.SubdivisionID, IsService: slices.Contains(cfg.ServiceUserIDs, id)}
		if row.FullName == "" {
			row.FullName = w.Username
		}
		for _, b := range busyByUser[id] {
			ivs = append(ivs, Interval{From: b.From, To: b.To})
			// Заявка относится к дню, в который начинается: суточная смена не дублирует счётчики.
			if !b.From.Before(dayIv.From) {
				row.ApplicationsCount++
				if b.IsComplete {
					row.CompletedCount++
				}
				if b.IsMain {
					row.PlannedSum += b.PlannedSum
				}
				if !row.IsService {
					appIDs[b.ApplicationID] = true
				}
			}
		}
		userDefault := defaultWork
		if tracked[id] {
			userDefault = nil
		}
		row.DayLoad = ComputeDayLoad(DayLoadInput{
			Day:         dayIv,
			Entries:     entriesByUser[id],
			Busy:        ivs,
			DefaultWork: userDefault,
			MinFree:     time.Duration(cfg.MinFreeMinutes) * time.Minute,
			MinShown:    time.Duration(cfg.MinShownMinutes) * time.Minute,
		})
		if row.IsService {
			// Служебная учётка — напоминания менеджерам: не показываем загрузку и свободные окна.
			row.LoadPercent = nil
			row.Free = []FreeSlot{}
		} else {
			out.Totals.Engineers++
			out.Totals.PlannedSum += row.PlannedSum
		}
		out.Engineers = append(out.Engineers, row)
	}
	out.Totals.Applications = len(appIDs)
	return out, nil
}

// worksInSubdivision решает, показывать ли инженера в расписании подразделения на день.
func worksInSubdivision(w UserBrief, entries []TimesheetEntry, subdivisionID int64) bool {
	home := w.SubdivisionID != nil && *w.SubdivisionID == subdivisionID
	hasShift, shiftHere := false, false
	for _, e := range entries {
		if !e.Kind.IsShift() {
			continue
		}
		hasShift = true
		sub := w.SubdivisionID
		if e.SubdivisionID != nil {
			sub = e.SubdivisionID
		}
		if sub != nil && *sub == subdivisionID {
			shiftHere = true
		}
	}
	if hasShift {
		return shiftHere
	}
	return home
}

// SlotWarning — предупреждение при постановке заявки: заявку можно сохранить после подтверждения.
type SlotWarning struct {
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`
	Kind     string `json:"kind"` // outside_schedule, duty, absence
	Message  string `json:"message"`
}

// SlotCheckResult — результат проверки времени заявки.
type SlotCheckResult struct {
	Conflict string        `json:"conflict,omitempty"` // пересечение с другой заявкой: сохранить нельзя
	Warnings []SlotWarning `json:"warnings"`
}

// SlotCheckInput — проверяемое время заявки.
type SlotCheckInput struct {
	ApplicationID *int64    `json:"application_id"`
	EngineerID    int64     `json:"engineer_id"`
	ExecutorIDs   []int64   `json:"executor_ids"`
	From          time.Time `json:"plan_datetime_from"`
	To            time.Time `json:"plan_datetime_to"`
	IsDuty        bool      `json:"is_duty"`
}

// CheckApplicationSlot проверяет пересечения (запрет) и график инженеров (предупреждения).
func (s *Service) CheckApplicationSlot(ctx context.Context, in SlotCheckInput) (*SlotCheckResult, error) {
	if in.EngineerID <= 0 {
		return nil, platform.NewValidation("Укажите инженера", map[string]string{"engineer_id": "Обязательное поле"})
	}
	if !in.To.After(in.From) {
		return nil, platform.NewValidation("Время окончания должно быть позже начала", map[string]string{"plan_datetime_to": "Неверный интервал"})
	}
	res := &SlotCheckResult{Warnings: []SlotWarning{}}
	users := []int64{in.EngineerID}
	for _, id := range in.ExecutorIDs {
		if id > 0 && !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	if !in.IsDuty {
		for _, uid := range users {
			overlap, err := s.repo.HasEngineerTimeOverlap(ctx, uid, in.From, in.To, in.ApplicationID)
			if err != nil {
				return nil, err
			}
			if overlap {
				res.Conflict = fmt.Sprintf("У сотрудника %s уже есть заявка в это время", s.userName(ctx, uid))
				return res, nil
			}
		}
	}
	entries, err := s.repo.ListTimesheetEntries(ctx, TimesheetFilter{From: in.From.Add(-48 * time.Hour), To: in.To.Add(48 * time.Hour), UserIDs: users})
	if err != nil {
		return nil, err
	}
	slot := Interval{From: in.From, To: in.To}
	for _, uid := range users {
		var mine []TimesheetEntry
		for _, e := range entries {
			if e.UserID == uid {
				mine = append(mine, e)
			}
		}
		res.Warnings = append(res.Warnings, s.slotWarnings(ctx, uid, mine, slot)...)
	}
	return res, nil
}

func (s *Service) slotWarnings(ctx context.Context, uid int64, entries []TimesheetEntry, slot Interval) []SlotWarning {
	name := s.userName(ctx, uid)
	loc := s.moscowLoc()
	var out []SlotWarning
	var work, duty []Interval
	for _, e := range entries {
		iv := Interval{From: e.StartsAt, To: e.EndsAt}
		switch {
		case e.Kind == TimesheetWork:
			work = append(work, iv)
		case e.Kind == TimesheetDuty:
			duty = append(duty, iv)
		default:
			if len(intersectIntervals([]Interval{slot}, []Interval{iv})) == 0 {
				continue
			}
			msg := fmt.Sprintf("%s: %s %s–%s", name, timesheetKindLabel(e.Kind), iv.From.In(loc).Format("02.01 15:04"), iv.To.In(loc).Format("02.01 15:04"))
			if e.Comment != "" {
				msg += " (" + e.Comment + ")"
			}
			out = append(out, SlotWarning{UserID: uid, UserName: name, Kind: "absence", Message: msg})
		}
	}
	if len(intersectIntervals([]Interval{slot}, duty)) > 0 {
		out = append(out, SlotWarning{UserID: uid, UserName: name, Kind: "duty", Message: name + " в это время дежурит — обычную заявку ставят только по согласованию с ним"})
	}
	// Без табеля график неизвестен — не предупреждаем.
	if len(work)+len(duty) > 0 && len(subtractIntervals([]Interval{slot}, append(work, duty...))) > 0 {
		out = append(out, SlotWarning{UserID: uid, UserName: name, Kind: "outside_schedule", Message: name + ": заявка выходит за рабочее время по табелю"})
	}
	return out
}

func timesheetKindLabel(k TimesheetKind) string {
	switch k {
	case TimesheetWork:
		return "смена"
	case TimesheetDuty:
		return "дежурство"
	case TimesheetVacation:
		return "отпуск"
	case TimesheetSick:
		return "больничный"
	case TimesheetUnpaid:
		return "б/с"
	case TimesheetReserve:
		return "резерв"
	}
	return string(k)
}

func (s *Service) userName(ctx context.Context, uid int64) string {
	if s.users != nil {
		if u, err := s.users.GetUserBrief(ctx, uid); err == nil && u != nil && u.FullName != "" {
			return u.FullName
		}
	}
	return fmt.Sprintf("#%d", uid)
}

// RescheduleApplication переносит заявку на другое время и/или к другому инженеру (перетаскивание в расписании).
func (s *Service) RescheduleApplication(ctx context.Context, auth *platform.AuthContext, id, engineerID int64, from, to time.Time) (*Application, error) {
	existing, err := s.repo.GetApplication(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.Deleted {
		return nil, platform.NewNotFound("Заявка не найдена")
	}
	if engineerID <= 0 {
		engineerID = existing.EngineerID
	}
	moved := *existing
	if engineerID != existing.EngineerID {
		// Новый основной инженер не может остаться соисполнителем, прежний становится соисполнителем не автоматически.
		moved.ExecutorIDs = slices.DeleteFunc(slices.Clone(existing.ExecutorIDs), func(x int64) bool { return x == engineerID })
	}
	moved.EngineerID = engineerID
	moved.PlanDatetimeFrom = from
	moved.PlanDatetimeTo = to
	if err := s.validateApplicationSlot(ctx, &moved, &id); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateApplication(ctx, &moved); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "application", id, "reschedule", map[string]any{"engineer_id": engineerID, "from": from, "to": to})
	if engineerID != existing.EngineerID {
		timeStr := from.In(s.moscowLoc()).Format("02.01 15:04")
		s.pushNotification(ctx, engineerID, "application", "Новая заявка #"+fmt.Sprint(id), "Назначена заявка на "+timeStr, "application", id)
	}
	s.broadcastCalendarUpdate(id)
	return s.GetApplication(ctx, auth, id)
}

// --- Дневной план и план/факт ---

func (s *Service) ListDailyPlans(ctx context.Context) ([]DailyPlan, error) {
	return s.repo.ListDailyPlans(ctx)
}

func (s *Service) SaveDailyPlan(ctx context.Context, auth *platform.AuthContext, subdivisionID int64, amount float64, effectiveFrom string) (*DailyPlan, error) {
	if subdivisionID <= 0 {
		return nil, platform.NewValidation("Укажите подразделение", map[string]string{"subdivision_id": "Обязательное поле"})
	}
	if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, platform.NewValidation("План не может быть отрицательным", map[string]string{"amount": "Неверное значение"})
	}
	day, err := s.ParseLocalDate(effectiveFrom)
	if err != nil {
		return nil, err
	}
	p := &DailyPlan{SubdivisionID: subdivisionID, Amount: math.Round(amount*100) / 100, EffectiveFrom: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)}
	if auth != nil {
		p.CreatedBy = auth.UserID
	}
	if err := s.repo.SaveDailyPlan(ctx, p); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "daily_plan", p.ID, "save", p)
	return p, nil
}

func (s *Service) DeleteDailyPlan(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.DeleteDailyPlan(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "daily_plan", id, "delete", nil)
	return nil
}

// PlanFactRow — план и факт инженера за период.
type PlanFactRow struct {
	UserID        int64    `json:"user_id"`
	FullName      string   `json:"full_name"`
	SubdivisionID *int64   `json:"subdivision_id,omitempty"`
	PlanDays      int      `json:"plan_days"`
	PlanAmount    float64  `json:"plan_amount"`
	FactAmount    float64  `json:"fact_amount"`
	Percent       *float64 `json:"percent"`
	NoTimesheet   bool     `json:"no_timesheet"`
}

// PlanFactReport — отчёт план/факт по инженерам и итог подразделения.
type PlanFactReport struct {
	From       string        `json:"from"`
	To         string        `json:"to"`
	Rows       []PlanFactRow `json:"rows"`
	PlanAmount float64       `json:"plan_amount"`
	FactAmount float64       `json:"fact_amount"`
	Percent    *float64      `json:"percent"`
}

func percentOf(fact, plan float64) *float64 {
	if plan <= 0 {
		return nil
	}
	p := math.Round(fact/plan*1000) / 10
	return &p
}

// PlanFact считает план/факт за период [from, to). План — дневной план города смены на каждый
// рабочий день по табелю (без отпусков и больничных); факт — оплаченная доля инженера в актах по дате работ.
// План подразделения — сумма планов его инженеров. Инженер без права видеть всех получает только свою строку.
func (s *Service) PlanFact(ctx context.Context, auth *platform.AuthContext, from, to time.Time, subdivisionID *int64) (*PlanFactReport, error) {
	if !to.After(from) {
		return nil, platform.NewValidation("Конец периода должен быть позже начала", nil)
	}
	if s.users == nil {
		return nil, platform.NewNotConfigured("Справочник сотрудников недоступен")
	}
	ownOnly := auth != nil && !auth.HasPermission("crm.plan.view") && !auth.HasPermission("crm.plan.manage")
	cfg := s.calendarLoadSettings(ctx)
	workers, err := s.users.ListActiveWorkers(ctx, true, false, nil)
	if err != nil {
		return nil, err
	}
	entries, err := s.repo.ListTimesheetEntries(ctx, TimesheetFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	entriesByUser := map[int64][]TimesheetEntry{}
	for _, e := range entries {
		entriesByUser[e.UserID] = append(entriesByUser[e.UserID], e)
	}
	plans, err := s.repo.ListDailyPlans(ctx)
	if err != nil {
		return nil, err
	}
	loc := s.moscowLoc()
	var rows []PlanFactRow
	var ids []int64
	for _, w := range workers {
		if slices.Contains(cfg.ServiceUserIDs, w.ID) || (ownOnly && w.ID != auth.UserID) {
			continue
		}
		days := CountPlanDays(entriesByUser[w.ID], from, to, loc)
		row := PlanFactRow{UserID: w.ID, FullName: w.FullName, SubdivisionID: w.SubdivisionID, NoTimesheet: len(entriesByUser[w.ID]) == 0}
		inScope := subdivisionID == nil || (w.SubdivisionID != nil && *w.SubdivisionID == *subdivisionID)
		for _, d := range days {
			sub := w.SubdivisionID
			if d.SubdivisionID != nil {
				sub = d.SubdivisionID
			}
			if sub == nil {
				continue
			}
			if subdivisionID != nil && *sub != *subdivisionID {
				continue
			}
			inScope = true
			row.PlanDays++
			row.PlanAmount += PlanAmountOn(plans, *sub, time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), 0, 0, 0, 0, time.UTC))
		}
		if !inScope {
			continue
		}
		rows = append(rows, row)
		ids = append(ids, w.ID)
	}
	if len(ids) > 0 {
		// Отчёт по актам фильтрует дату работ включительно, поэтому конец периода — последний день.
		facts, err := s.repo.AmountActsReportFiltered(ctx, ReportFilter{From: from, To: to.Add(-time.Nanosecond), UserIDs: ids})
		if err != nil {
			return nil, err
		}
		factBy := map[int64]float64{}
		for _, f := range facts {
			factBy[f.EngineerID] += f.PaidAmount
		}
		for i := range rows {
			rows[i].FactAmount = math.Round(factBy[rows[i].UserID]*100) / 100
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].FullName < rows[j].FullName })
	rep := &PlanFactReport{From: from.Format("2006-01-02"), To: to.AddDate(0, 0, -1).Format("2006-01-02"), Rows: rows}
	if rep.Rows == nil {
		rep.Rows = []PlanFactRow{}
	}
	for i := range rep.Rows {
		r := &rep.Rows[i]
		r.PlanAmount = math.Round(r.PlanAmount*100) / 100
		r.Percent = percentOf(r.FactAmount, r.PlanAmount)
		rep.PlanAmount += r.PlanAmount
		rep.FactAmount += r.FactAmount
	}
	rep.Percent = percentOf(rep.FactAmount, rep.PlanAmount)
	return rep, nil
}
