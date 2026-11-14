package crm

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// --- Tasks & Task Pool ---

func (s *Service) ListTasks(ctx context.Context, auth *platform.AuthContext, f TaskFilter) ([]Task, error) {
	if auth != nil {
		if !hasFullContractorAccess(auth) {
			uid := auth.UserID
			f.AssignedUserID = &uid
		} else if !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
			subID := *auth.SubdivisionID
			f.SubdivisionID = &subID
		}
	}
	items, err := s.repo.ListTasks(ctx, f)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range items {
		if items[i].AssignedUserID != nil {
			if u := s.resolveUserCached(ctx, *items[i].AssignedUserID, cache); u != nil {
				items[i].AssignedName = u.FullName
			}
		}
	}
	return items, nil
}

func (s *Service) GetTask(ctx context.Context, id int64) (*Task, error) {
	t, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, platform.NewNotFound("Задача не найдена")
	}
	if t.AssignedUserID != nil && s.users != nil {
		if u, _ := s.users.GetUserBrief(ctx, *t.AssignedUserID); u != nil {
			t.AssignedName = u.FullName
		}
	}
	return t, nil
}

func (s *Service) CreateTask(ctx context.Context, auth *platform.AuthContext, in Task) (*Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, platform.NewValidation("Укажите заголовок задачи", map[string]string{"title": "Обязательное поле"})
	}
	if in.TaskType == "" {
		in.TaskType = TaskTypeCall
	}
	if in.Status == 0 {
		in.Status = TaskStatusNew
	}
	if in.Priority == 0 {
		in.Priority = 10
	}
	if in.DecisionTimeSec <= 0 {
		in.DecisionTimeSec = 3600
	}
	if in.PlanDatetimeTo.IsZero() {
		in.PlanDatetimeTo = s.now().Add(24 * time.Hour)
	}
	if auth != nil {
		in.CreatedBy = auth.UserID
	}
	if err := s.repo.CreateTask(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "task", in.ID, "create", in)
	if in.AssignedUserID != nil {
		s.notifyTaskAssigned(ctx, &in)
	}
	return s.GetTask(ctx, in.ID)
}

func (s *Service) UpdateTask(ctx context.Context, auth *platform.AuthContext, id int64, in Task) (*Task, error) {
	existing, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Задача не найдена")
	}
	prevStatus := existing.Status
	prevAssignee := existing.AssignedUserID

	if strings.TrimSpace(in.Title) != "" {
		existing.Title = strings.TrimSpace(in.Title)
	}
	if in.TaskType != "" {
		existing.TaskType = in.TaskType
	}
	if in.Status != 0 {
		existing.Status = in.Status
	}
	if in.Priority != 0 {
		existing.Priority = in.Priority
	}
	if !in.PlanDatetimeTo.IsZero() {
		existing.PlanDatetimeTo = in.PlanDatetimeTo
	}
	if in.DecisionTimeSec > 0 {
		existing.DecisionTimeSec = in.DecisionTimeSec
	}
	existing.Description = in.Description
	existing.Note = in.Note
	existing.AssignedUserID = in.AssignedUserID
	existing.SubdivisionID = in.SubdivisionID
	existing.IsUrgent = in.IsUrgent
	if in.TaskResult != "" {
		existing.TaskResult = in.TaskResult
	}
	if in.ExecutionDatetime != nil {
		existing.ExecutionDatetime = in.ExecutionDatetime
	} else if existing.Status >= TaskStatusDone && prevStatus < TaskStatusDone && existing.ExecutionDatetime == nil {
		now := s.now()
		existing.ExecutionDatetime = &now
	}

	if err := s.repo.UpdateTask(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "task", id, "update", existing)

	if existing.AssignedUserID != nil && (prevAssignee == nil || *prevAssignee != *existing.AssignedUserID) {
		s.notifyTaskAssigned(ctx, existing)
	}

	// Недозвон автоматически порождает отложенную повторную задачу (+4 часа), чтобы клиент не потерялся.
	if prevStatus != TaskStatusNoAnswer && existing.Status == TaskStatusNoAnswer {
		s.spawnNoAnswerFollowup(ctx, auth, existing, s.now().Add(4*time.Hour))
	}

	return s.GetTask(ctx, id)
}

type ExecuteTaskInput struct {
	TaskResult        string     `json:"task_result"`
	Note              string     `json:"note"`
	NewPlanDatetimeTo *time.Time `json:"new_plan_datetime_to,omitempty"`
}

// ExecuteTask фиксирует результат выполнения задачи (Дозвонился / Не дозвонился / Заявка оформлена / Отказ) и при недозвоне создаёт перенесённую задачу.
func (s *Service) ExecuteTask(ctx context.Context, auth *platform.AuthContext, id int64, in ExecuteTaskInput) (*Task, error) {
	existing, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Задача не найдена")
	}
	res := strings.TrimSpace(in.TaskResult)
	switch res {
	case TaskResultAnswered, TaskResultApplicationCreated:
		existing.Status = TaskStatusDone
	case TaskResultNoAnswer:
		existing.Status = TaskStatusNoAnswer
	case TaskResultRefused:
		existing.Status = TaskStatusFailed
	default:
		return nil, platform.NewValidation("Укажите корректный результат выполнения задачи", map[string]string{"task_result": "Допустимые значения: answered, no_answer, application_created, refused"})
	}
	now := s.now()
	existing.TaskResult = res
	existing.ExecutionDatetime = &now
	if note := strings.TrimSpace(in.Note); note != "" {
		existing.Note = note
	}
	if err := s.repo.UpdateTask(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "task", id, "execute", in)

	if res == TaskResultNoAnswer {
		nextTime := now.Add(4 * time.Hour)
		if in.NewPlanDatetimeTo != nil && !in.NewPlanDatetimeTo.IsZero() {
			nextTime = *in.NewPlanDatetimeTo
		}
		s.spawnNoAnswerFollowup(ctx, auth, existing, nextTime)
	}
	return s.GetTask(ctx, id)
}

func (s *Service) spawnNoAnswerFollowup(ctx context.Context, auth *platform.AuthContext, src *Task, planTo time.Time) {
	prevID := src.ID
	var createdBy int64 = src.CreatedBy
	if auth != nil {
		createdBy = auth.UserID
	}
	followup := Task{
		ContractorID:    src.ContractorID,
		AddressID:       src.AddressID,
		ApplicationID:   src.ApplicationID,
		ContractID:      src.ContractID,
		PrevTaskID:      &prevID,
		TaskType:        TaskTypeNoAnswer,
		Title:           "Повторный звонок (недозвон): " + src.Title,
		Description:     src.Description,
		Status:          TaskStatusNew,
		Priority:        src.Priority + 5,
		AssignedUserID:  src.AssignedUserID,
		SubdivisionID:   src.SubdivisionID,
		PlanDatetimeTo:  planTo,
		DecisionTimeSec: src.DecisionTimeSec,
		CreatedBy:       createdBy,
	}
	if err := s.repo.CreateTask(ctx, &followup); err == nil {
		s.logChange(ctx, auth, "task", followup.ID, "auto_no_answer_followup", map[string]any{"source_task_id": src.ID, "prev_task_id": prevID})
	}
}

func (s *Service) DeleteTask(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteTask(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "task", id, "delete", nil)
	return nil
}

func (s *Service) ListTaskPoolSettings(ctx context.Context) ([]TaskPoolSetting, error) {
	return s.repo.ListTaskPoolSettings(ctx)
}

func (s *Service) SaveTaskPoolSetting(ctx context.Context, auth *platform.AuthContext, in TaskPoolSetting) (*TaskPoolSetting, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, platform.NewValidation("Укажите название правила пула", nil)
	}
	if in.TimeFrom == "" {
		in.TimeFrom = "08:00"
	}
	if in.TimeTo == "" {
		in.TimeTo = "20:00"
	}
	if err := s.repo.SaveTaskPoolSetting(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "task_pool_setting", in.ID, "save", in)
	return &in, nil
}

// CreateQuarterlyCallTasks создаёт задачи квартального обзвона по активным договорам ТО (каждые 90 дней при остатке > 89 дней).
func (s *Service) CreateQuarterlyCallTasks(ctx context.Context) (int, error) {
	now := s.now()
	contracts, err := s.repo.ListContractsNeedingQuarterlyTask(ctx, now)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, c := range contracts {
		cid := c.ContractorID
		aid := c.AddressID
		ctid := c.ID
		task := &Task{
			ContractorID:    &cid,
			AddressID:       &aid,
			ContractID:      &ctid,
			TaskType:        TaskTypeQuarterlyCall,
			Title:           fmt.Sprintf("Ежеквартальный обзвон по договору №%s", c.Number),
			Description:     fmt.Sprintf("Контрагент: %s | Адрес: %s", c.ContractorName, c.AddressText),
			Status:          TaskStatusNew,
			Priority:        20,
			PlanDatetimeTo:  now.Add(24 * time.Hour),
			DecisionTimeSec: 3600,
		}
		if err := s.repo.CreateTask(ctx, task); err == nil {
			created++
		}
	}
	return created, nil
}

// CreateProlongationTasks создаёт задачи пролонгации за <= 21 день до окончания договора при отсутствии нового договора или заявки.
func (s *Service) CreateProlongationTasks(ctx context.Context) (int, error) {
	now := s.now()
	contracts, err := s.repo.ListContractsNeedingProlongationTask(ctx, now)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, c := range contracts {
		cid := c.ContractorID
		aid := c.AddressID
		ctid := c.ID
		task := &Task{
			ContractorID:    &cid,
			AddressID:       &aid,
			ContractID:      &ctid,
			TaskType:        TaskTypeProlongation,
			Title:           fmt.Sprintf("Пролонгация договора ТО №%s (до %s)", c.Number, c.ValidTo.Format("02.01.2006")),
			Description:     fmt.Sprintf("Контрагент: %s | Адрес: %s", c.ContractorName, c.AddressText),
			Status:          TaskStatusNew,
			Priority:        30,
			PlanDatetimeTo:  now.Add(48 * time.Hour),
			DecisionTimeSec: 7200,
		}
		if err := s.repo.CreateTask(ctx, task); err == nil {
			created++
		}
	}
	return created, nil
}

// DistributeTaskPool распределяет неназначенные задачи по круговой очереди сотрудников согласно правилам PoolSetting.
func (s *Service) DistributeTaskPool(ctx context.Context) (int, error) {
	now := s.now()
	localNow := now.In(s.moscowLoc())
	settings, err := s.repo.ListTaskPoolSettings(ctx)
	if err != nil {
		return 0, err
	}
	var activeSettings []TaskPoolSetting
	for _, st := range settings {
		if st.Active && isWithinHHMMWindow(localNow, st.TimeFrom, st.TimeTo) {
			activeSettings = append(activeSettings, st)
		}
	}
	if len(activeSettings) == 0 {
		return 0, nil
	}

	tasks, err := s.repo.ListUnassignedActiveTasks(ctx, 100)
	if err != nil || len(tasks) == 0 {
		return 0, err
	}

	candidates, err := s.loadPoolCandidates(ctx)
	if err != nil || len(candidates) == 0 {
		return 0, err
	}

	assignedCount := 0
	for i := range tasks {
		task := &tasks[i]
		setting := matchPoolSetting(activeSettings, task.TaskType)
		if setting == nil {
			continue
		}
		uid := s.pickNextPoolUser(ctx, candidates, setting, task)
		if uid == 0 {
			continue
		}
		task.AssignedUserID = &uid
		task.Status = TaskStatusInProgress
		if err := s.repo.UpdateTask(ctx, task); err != nil {
			continue
		}
		_ = s.repo.TouchQueueUser(ctx, uid, now)
		moveUserToBack(&candidates, uid)
		s.notifyTaskAssigned(ctx, task)
		assignedCount++
	}
	return assignedCount, nil
}

// CheckOverdueTasks помечает задачи, у которых наступило окно SLA (plan_datetime_to - decision_time), срочными и уведомляет сотрудников.
func (s *Service) CheckOverdueTasks(ctx context.Context) (int, error) {
	now := s.now()
	tasks, err := s.repo.ListOverdueActiveTasks(ctx, now)
	if err != nil {
		return 0, err
	}
	updated := 0
	for i := range tasks {
		t := &tasks[i]
		if !t.IsUrgent {
			t.IsUrgent = true
			if err := s.repo.UpdateTask(ctx, t); err == nil {
				updated++
			}
		}
		if t.AssignedUserID != nil && s.hub != nil {
			s.hub.BroadcastToUsers([]int64{*t.AssignedUserID}, WSEvent{
				Type:    "task:urgent",
				Payload: t,
			})
		}
	}
	return updated, nil
}

func (s *Service) loadPoolCandidates(ctx context.Context) ([]UserBrief, error) {
	queue, err := s.repo.ListQueueUsers(ctx)
	if err != nil {
		return nil, err
	}
	if len(queue) > 0 && s.users != nil {
		out := make([]UserBrief, 0, len(queue))
		for _, q := range queue {
			u, err := s.users.GetUserBrief(ctx, q.UserID)
			if err == nil && u != nil && u.Active {
				out = append(out, *u)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if s.users == nil {
		return nil, nil
	}
	return s.users.ListActiveWorkers(ctx, false, false, nil)
}

func matchPoolSetting(settings []TaskPoolSetting, tt TaskType) *TaskPoolSetting {
	for i := range settings {
		if len(settings[i].TaskTypes) == 0 || slices.Contains(settings[i].TaskTypes, tt) {
			return &settings[i]
		}
	}
	return nil
}

func (s *Service) pickNextPoolUser(ctx context.Context, candidates []UserBrief, st *TaskPoolSetting, task *Task) int64 {
	for _, u := range candidates {
		roleOK := (st.ForDispatcher && u.IsDispatcher) || (st.ForEngineer && u.IsEngineer) || (!st.ForDispatcher && !st.ForEngineer)
		if !roleOK {
			continue
		}
		if st.IsOnline && s.hub != nil && !s.hub.IsUserOnline(u.ID) {
			continue
		}
		if st.WithSubdivision && task.SubdivisionID != nil {
			if u.SubdivisionID == nil || *u.SubdivisionID != *task.SubdivisionID {
				continue
			}
		}
		if st.TasksMaxCount > 0 {
			activeCnt, err := s.repo.CountUserActivePoolTasks(ctx, u.ID, false)
			if err == nil && activeCnt >= st.TasksMaxCount {
				continue
			}
		}
		if st.TransferredTasksMaxCount > 0 {
			trCnt, err := s.repo.CountUserActivePoolTasks(ctx, u.ID, true)
			if err == nil && trCnt >= st.TransferredTasksMaxCount {
				continue
			}
		}
		return u.ID
	}
	return 0
}

func moveUserToBack(candidates *[]UserBrief, uid int64) {
	list := *candidates
	for i := range list {
		if list[i].ID == uid {
			picked := list[i]
			*candidates = append(append(list[:i], list[i+1:]...), picked)
			return
		}
	}
}

func isWithinHHMMWindow(t time.Time, fromStr, toStr string) bool {
	fromMin := parseHHMM(fromStr)
	toMin := parseHHMM(toStr)
	curMin := t.Hour()*60 + t.Minute()
	if fromMin <= toMin {
		return curMin >= fromMin && curMin <= toMin
	}
	// Ночная смена через полночь (например 22:00 .. 06:00).
	return curMin >= fromMin || curMin <= toMin
}

func parseHHMM(hhmm string) int {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) != 2 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return h*60 + m
}

func (s *Service) notifyTaskAssigned(ctx context.Context, task *Task) {
	if task.AssignedUserID == nil {
		return
	}
	s.pushNotification(ctx, *task.AssignedUserID, "task", "Назначена задача #"+strconv.FormatInt(task.ID, 10), task.Title, "task", task.ID)
	if s.hub != nil {
		s.hub.BroadcastToUsers([]int64{*task.AssignedUserID}, WSEvent{
			Type:    "task:assigned",
			Payload: task,
		})
	}
}

// --- Telephony (MTS VPBX) ---

type MTSVPBXPayload struct {
	CallID             string `json:"callId"`
	AbonentID          string `json:"abonentId"`
	State              string `json:"state"`
	StartTime          int64  `json:"startTime"`
	AnswerTime         int64  `json:"answerTime"`
	EndTime            int64  `json:"endTime"`
	CallDirection      string `json:"callDirection"`
	RemotePartyAddress string `json:"remotePartyAddress"`
	ExtTrackingID      string `json:"extTrackingId"`
}

type MTSWebhookInput struct {
	EventType      string          `json:"eventType"`
	AbonentID      string          `json:"abonentId"`
	Payload        *MTSVPBXPayload `json:"payload"`
	ExternalCallID string          `json:"external_call_id"`
	CallID         string          `json:"call_id"`
	ExtTrackingID  string          `json:"ext_tracking_id"`
	Direction      string          `json:"direction"`
	State          string          `json:"state"`
	Status         string          `json:"status"`
	CallerPhone    string          `json:"caller_phone"`
	CalleePhone    string          `json:"callee_phone"`
	From           string          `json:"from"`
	To             string          `json:"to"`
	SipLogin       string          `json:"sip_login"`
	RecordURL      string          `json:"record_url"`
	DurationSec    int             `json:"duration_sec"`
}

type UpdateCallCardInput struct {
	ContractorID     *int64 `json:"contractor_id"`
	TrafficSourceID  *int64 `json:"traffic_source_id"`
	CallerType       int    `json:"caller_type"`
	CallResult       int    `json:"call_result"`
	TypeOfAppeal     string `json:"type_of_appeal"`
	Comment          string `json:"comment"`
	DetachFromOthers bool   `json:"detach_from_others"`
}

func (s *Service) ListCalls(ctx context.Context, f CallFilter) ([]CallRecord, error) {
	return s.repo.ListCalls(ctx, f)
}

func (s *Service) GetCallCard(ctx context.Context, auth *platform.AuthContext, id int64) (*CallRecord, error) {
	rec, err := s.repo.GetCall(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, platform.NewNotFound("Звонок не найден")
	}
	var uid *int64
	if auth != nil {
		idCopy := auth.UserID
		uid = &idCopy
	}
	_ = s.repo.MarkMissCallViewed(ctx, id, uid)
	return rec, nil
}

func stripTelURI(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 4 && strings.EqualFold(s[:4], "tel:") {
		s = s[4:]
	}
	if at := strings.IndexByte(s, '@'); at > 0 {
		s = s[:at]
	}
	return strings.TrimSpace(s)
}

func (s *Service) HandleMTSWebhook(ctx context.Context, headerToken string, rawBody []byte) (*CallRecord, error) {
	if s.cfg.MTSWebhookSecret != "" {
		if subtle.ConstantTimeCompare([]byte(headerToken), []byte(s.cfg.MTSWebhookSecret)) != 1 {
			return nil, platform.NewUnauthorized("Неверный токен вебхука МТС ВАТС")
		}
	}
	var in MTSWebhookInput
	if err := json.Unmarshal(rawBody, &in); err != nil {
		return nil, platform.NewValidation("Некорректный JSON вебхука МТС", nil)
	}
	if strings.EqualFold(strings.TrimSpace(in.EventType), "CHECK_ALIVE") {
		return nil, nil
	}

	var payloadCallID, payloadAbonent, payloadState, payloadDir, payloadRemote, payloadTrack string
	var startMs, answerMs, endMs int64
	if in.Payload != nil {
		payloadCallID = in.Payload.CallID
		payloadAbonent = in.Payload.AbonentID
		payloadState = in.Payload.State
		payloadDir = in.Payload.CallDirection
		payloadRemote = stripTelURI(in.Payload.RemotePartyAddress)
		payloadTrack = in.Payload.ExtTrackingID
		startMs = in.Payload.StartTime
		answerMs = in.Payload.AnswerTime
		endMs = in.Payload.EndTime
	}

	extID := firstNonEmpty(in.ExternalCallID, in.CallID, payloadCallID)
	if extID == "" {
		return nil, platform.NewValidation("Отсутствует call_id/external_call_id", nil)
	}
	abonentID := firstNonEmpty(in.AbonentID, payloadAbonent)
	extTrackingID := firstNonEmpty(in.ExtTrackingID, payloadTrack)

	direction := strings.TrimSpace(in.Direction)
	if direction == "" && payloadDir != "" {
		if strings.EqualFold(payloadDir, "Originator") || strings.EqualFold(payloadDir, "Click-to-Dial") {
			direction = "outbound"
		} else {
			direction = "inbound"
		}
	}
	if direction == "" {
		direction = "inbound"
	}

	caller := firstNonEmpty(in.CallerPhone, in.From)
	callee := firstNonEmpty(in.CalleePhone, in.To)
	if payloadRemote != "" {
		if direction == "outbound" && callee == "" {
			callee = payloadRemote
		} else if direction != "outbound" && caller == "" {
			caller = payloadRemote
		}
	}
	if abonentPhone := stripTelURI(abonentID); abonentPhone != "" {
		if direction == "outbound" && caller == "" {
			caller = abonentPhone
		} else if direction != "outbound" && callee == "" {
			callee = abonentPhone
		}
	}
	if p10, err := platform.ParsePhone10(caller); err == nil {
		caller = p10
	}
	if p10, err := platform.ParsePhone10(callee); err == nil {
		callee = p10
	}

	evUpper := strings.ToUpper(strings.TrimSpace(in.EventType))
	status := firstNonEmpty(in.Status, in.State)
	if status == "" {
		switch evUpper {
		case "CALL_RECEIVED", "CALL_ORIGINATED":
			status = "ringing"
		case "CALL_ANSWERED":
			status = "answered"
		case "CALL_RELEASED":
			status = "ended"
		default:
			if strings.EqualFold(payloadState, "Active") {
				status = "answered"
			} else if strings.EqualFold(payloadState, "Released") {
				status = "ended"
			} else if payloadState != "" {
				status = strings.ToLower(payloadState)
			} else {
				status = "ringing"
			}
		}
	}

	var existing *CallRecord
	if prev, err := s.repo.GetCallByExternalID(ctx, extID); err == nil && prev != nil {
		existing = prev
	}

	var userID *int64
	if in.SipLogin != "" {
		if sip, err := s.repo.GetSipAccountByLogin(ctx, in.SipLogin); err == nil && sip != nil && sip.UserID != nil {
			userID = sip.UserID
		}
	}
	if userID == nil && abonentID != "" {
		if sip, err := s.repo.GetSipAccountByAbonentID(ctx, abonentID); err == nil && sip != nil && sip.UserID != nil {
			userID = sip.UserID
		}
	}
	if userID == nil && existing != nil {
		userID = existing.UserID
	}

	if caller == "" && existing != nil {
		caller = existing.CallerPhone
	}
	if callee == "" && existing != nil {
		callee = existing.CalleePhone
	}

	lookupPhone := caller
	if direction == "outbound" {
		lookupPhone = callee
	}
	var contractor *Contractor
	var contractorID *int64
	if len(lookupPhone) == 10 {
		if c, err := s.repo.FindUniqueContractorByPhone(ctx, lookupPhone); err == nil && c != nil {
			contractor = c
			contractorID = &c.ID
		}
	}
	if contractorID == nil && existing != nil && existing.ContractorID != nil {
		contractorID = existing.ContractorID
		if c, err := s.repo.GetContractor(ctx, *existing.ContractorID); err == nil && c != nil {
			contractor = c
		}
	}

	now := s.now()
	startedAt := now
	if startMs > 0 {
		startedAt = time.UnixMilli(startMs).UTC()
	} else if existing != nil && !existing.StartedAt.IsZero() {
		startedAt = existing.StartedAt
	}

	var answerTime *time.Time
	if answerMs > 0 {
		t := time.UnixMilli(answerMs).UTC()
		answerTime = &t
	} else if evUpper == "CALL_ANSWERED" {
		answerTime = &now
	} else if existing != nil {
		answerTime = existing.AnswerTime
	}

	var endedAt *time.Time
	if endMs > 0 {
		t := time.UnixMilli(endMs).UTC()
		endedAt = &t
	} else if status == "ended" || status == "missed" || status == "completed" {
		endedAt = &now
	}
	if status == "ended" && answerTime == nil && (existing == nil || existing.AnswerTime == nil) && evUpper == "CALL_RELEASED" {
		status = "missed"
	}

	durationSec := in.DurationSec
	if durationSec == 0 && endedAt != nil {
		refStart := startedAt
		if answerTime != nil {
			refStart = *answerTime
		}
		if diff := int(endedAt.Sub(refStart).Seconds()); diff > 0 {
			durationSec = diff
		}
	}

	rec := &CallRecord{
		ExternalCallID: extID,
		AbonentID:      abonentID,
		ExtTrackingID:  extTrackingID,
		Direction:      direction,
		Status:         status,
		CallerPhone:    caller,
		CalleePhone:    callee,
		SipLogin:       in.SipLogin,
		UserID:         userID,
		ContractorID:   contractorID,
		RecordURL:      in.RecordURL,
		DurationSec:    durationSec,
		StartedAt:      startedAt,
		AnswerTime:     answerTime,
		EndedAt:        endedAt,
		RawPayload:     json.RawMessage(rawBody),
	}
	if contractor != nil {
		rec.ContractorName = contractor.Name
	}

	if err := s.repo.UpsertCallByExternalID(ctx, rec); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"call":       rec,
		"contractor": contractor,
		"event_type": evUpper,
	}
	var targetUsers []int64
	if userID != nil {
		targetUsers = []int64{*userID}
	} else if s.hub != nil {
		targetUsers = s.hub.OnlineUserIDs()
	}

	if evUpper == "CALL_RELEASED" || status == "ended" || status == "missed" {
		if status == "missed" && (direction == "inbound" || direction == "in") {
			poolUID := int64(0)
			if userID != nil {
				poolUID = *userID
			}
			_ = s.repo.AddToMissCallPool(ctx, rec.ID, poolUID)
			if s.hub != nil && len(targetUsers) > 0 {
				s.hub.BroadcastToUsers(targetUsers, WSEvent{
					Type:    "miss_call:new",
					Payload: payload,
				})
			}
		}
		if s.hub != nil && len(targetUsers) > 0 {
			s.hub.BroadcastToUsers(targetUsers, WSEvent{
				Type:    "call:ended",
				Payload: payload,
			})
		}
	} else {
		if s.bus != nil {
			s.bus.Publish(ctx, platform.Event{
				Topic:   "crm.call.incoming",
				UserIDs: targetUsers,
				Payload: payload,
			})
		}
		if s.hub != nil && len(targetUsers) > 0 {
			s.hub.BroadcastToUsers(targetUsers, WSEvent{
				Type:    "call:connect",
				Payload: payload,
			})
		}
	}
	return rec, nil
}

func isValidCallAppeal(appeal string) bool {
	switch appeal {
	case "", CallAppealGeneralQuestion, CallAppealServiceRequest, CallAppealBuyParts,
		CallAppealToContract, CallAppealServiceWork, CallAppealSaleEquipment,
		CallAppealPartsStore, CallAppealSpecify, CallAppealSpamCall, CallAppealOther:
		return true
	default:
		return false
	}
}

// UpdateCallCard обновляет карточку звонка, выполняет условную валидацию для клиентских/поставщицких звонков и отмечает пропущенный звонок просмотренным.
func (s *Service) UpdateCallCard(ctx context.Context, auth *platform.AuthContext, callID int64, in UpdateCallCardInput) (*CallRecord, error) {
	rec, err := s.repo.GetCall(ctx, callID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, platform.NewNotFound("Звонок не найден")
	}
	if in.CallerType < 0 || in.CallerType > CallerTypeOther {
		return nil, platform.NewValidation("Неизвестный тип звонящего", map[string]string{"caller_type": "Неверное значение"})
	}
	if in.CallResult < 0 || in.CallResult > CallResultRefusal {
		return nil, platform.NewValidation("Неизвестный результат звонка", map[string]string{"call_result": "Неверное значение"})
	}
	appeal := strings.TrimSpace(in.TypeOfAppeal)
	if !isValidCallAppeal(appeal) {
		return nil, platform.NewValidation("Неизвестный тип обращения", map[string]string{"type_of_appeal": "Неверное значение"})
	}
	if in.CallerType == CallerTypeClient || in.CallerType == CallerTypeSupplier {
		fields := map[string]string{}
		if appeal == "" {
			fields["type_of_appeal"] = "Укажите тип обращения"
		}
		if in.ContractorID == nil || *in.ContractorID <= 0 {
			fields["contractor_id"] = "Выберите контрагента"
		}
		if in.TrafficSourceID == nil || *in.TrafficSourceID <= 0 {
			fields["traffic_source_id"] = "Укажите источник трафика"
		}
		if in.CallResult <= 0 {
			fields["call_result"] = "Укажите результат звонка"
		}
		if len(fields) > 0 {
			return nil, platform.NewValidation("Заполните обязательные поля карточки звонка", fields)
		}
	}

	rec.ContractorID = in.ContractorID
	rec.TrafficSourceID = in.TrafficSourceID
	rec.CallerType = in.CallerType
	rec.CallResult = in.CallResult
	rec.TypeOfAppeal = appeal
	rec.Comment = strings.TrimSpace(in.Comment)
	if err := s.repo.UpdateCallCard(ctx, rec); err != nil {
		return nil, err
	}
	if rec.ContractorID != nil && *rec.ContractorID > 0 && in.DetachFromOthers {
		externalPhone := rec.CallerPhone
		if rec.Direction == "outbound" {
			externalPhone = rec.CalleePhone
		}
		if p10, err := platform.ParsePhone10(externalPhone); err == nil {
			_ = s.repo.DetachPhoneFromOtherContractors(ctx, p10, *rec.ContractorID)
		}
	}
	var uid *int64
	if auth != nil {
		idCopy := auth.UserID
		uid = &idCopy
	}
	_ = s.repo.MarkMissCallViewed(ctx, callID, uid)
	s.logChange(ctx, auth, "call", callID, "update_card", in)
	return s.repo.GetCall(ctx, callID)
}

// ListMissCallPool возвращает пропущенные звонки из пула диспетчера.
func (s *Service) ListMissCallPool(ctx context.Context, auth *platform.AuthContext, onlyUnviewed bool, limit int) ([]MissCallPoolItem, error) {
	var uid *int64
	if auth != nil && !hasFullContractorAccess(auth) {
		idCopy := auth.UserID
		uid = &idCopy
	}
	return s.repo.ListMissCallPool(ctx, uid, onlyUnviewed, limit)
}

// MarkMissCallViewed помечает пропущенный звонок просмотренным.
func (s *Service) MarkMissCallViewed(ctx context.Context, auth *platform.AuthContext, callID int64) error {
	var uid *int64
	if auth != nil && !hasFullContractorAccess(auth) {
		idCopy := auth.UserID
		uid = &idCopy
	}
	return s.repo.MarkMissCallViewed(ctx, callID, uid)
}

// SyncMissedCallPool добирает пропущенные входящие звонки за последние 2 дня в пул и уведомляет онлайн-диспетчеров.
func (s *Service) SyncMissedCallPool(ctx context.Context) (int, error) {
	since := s.now().Add(-48 * time.Hour)
	calls, err := s.repo.ListMissedCallsToSync(ctx, since)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, c := range calls {
		uid := int64(0)
		if c.UserID != nil {
			uid = *c.UserID
		}
		if err := s.repo.AddToMissCallPool(ctx, c.ID, uid); err != nil {
			continue
		}
		added++
		if s.hub != nil {
			targets := s.hub.OnlineUserIDs()
			if uid > 0 {
				targets = []int64{uid}
			}
			if len(targets) > 0 {
				s.hub.BroadcastToUsers(targets, WSEvent{
					Type:    "miss_call:new",
					Payload: map[string]any{"call": c},
				})
			}
		}
	}
	return added, nil
}

// BlindTransfer отправляет команду перевода звонка на внешний номер через МТС ВАТС API.
func (s *Service) BlindTransfer(ctx context.Context, auth *platform.AuthContext, externalCallID, targetPhone string) error {
	if s.cfg.MTSBaseURL == "" || s.cfg.MTSAPIToken == "" {
		return platform.NewNotConfigured("Интеграция МТС ВАТС (MTS_BASE_URL / MTS_API_TOKEN) не настроена")
	}
	cleanTarget, err := platform.ParsePhone10(targetPhone)
	if err != nil {
		return err
	}

	abonentID := ""
	if existing, err := s.repo.GetCallByExternalID(ctx, externalCallID); err == nil && existing != nil {
		abonentID = strings.TrimSpace(existing.AbonentID)
	}
	if abonentID == "" && auth != nil {
		if sip, err := s.repo.GetSipAccountByUser(ctx, auth.UserID); err == nil && sip != nil {
			abonentID = strings.TrimSpace(sip.AbonentID)
		}
	}

	baseURL := strings.TrimRight(s.cfg.MTSBaseURL, "/")
	var req *http.Request
	if abonentID != "" {
		endpoint := fmt.Sprintf("%s/user/%s/calls/%s/BlindTransfer?address=8%s",
			baseURL, url.PathEscape(abonentID), url.PathEscape(externalCallID), cleanTarget)
		req, err = http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-AUTH-TOKEN", s.cfg.MTSAPIToken)
	} else {
		body, _ := json.Marshal(map[string]string{
			"call_id": externalCallID,
			"target":  "7" + cleanTarget,
		})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/calls/transfer", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.cfg.MTSAPIToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка запроса к МТС ВАТС: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return platform.NewRuleError(fmt.Sprintf("МТС ВАТС отклонила перевод звонка (HTTP %d)", resp.StatusCode))
	}
	s.logChange(ctx, auth, "call", 0, "blind_transfer", map[string]string{"external_call_id": externalCallID, "target": cleanTarget})
	return nil
}

// GetCallRecordMP3 скачивает MP3-запись разговора из МТС ВАТС по ext_tracking_id.
func (s *Service) GetCallRecordMP3(ctx context.Context, callID int64) (string, []byte, error) {
	rec, err := s.repo.GetCall(ctx, callID)
	if err != nil {
		return "", nil, err
	}
	if rec == nil {
		return "", nil, platform.NewNotFound("Звонок не найден")
	}
	if s.cfg.MTSBaseURL == "" || s.cfg.MTSAPIToken == "" {
		return "", nil, platform.NewNotConfigured("Интеграция МТС ВАТС (MTS_BASE_URL / MTS_API_TOKEN) не настроена")
	}
	trackID := strings.TrimSpace(rec.ExtTrackingID)
	if trackID == "" {
		return "", nil, platform.NewValidation("У звонка отсутствует ext_tracking_id для загрузки записи", nil)
	}
	endpoint := fmt.Sprintf("%s/callRecording/mp3/%s", strings.TrimRight(s.cfg.MTSBaseURL, "/"), url.PathEscape(trackID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("X-AUTH-TOKEN", s.cfg.MTSAPIToken)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("ошибка получения записи МТС ВАТС: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", nil, platform.NewRuleError(fmt.Sprintf("МТС ВАТС не отдала запись разговора (HTTP %d)", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return "", nil, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	return ct, data, nil
}

// GetCurrentCall запрашивает текущие активные вызовы абонента в МТС ВАТС или возвращает локально незавершённые вызовы.
func (s *Service) GetCurrentCall(ctx context.Context, auth *platform.AuthContext) ([]CallRecord, error) {
	if auth == nil {
		return nil, platform.NewUnauthorized("Требуется авторизация")
	}
	if s.cfg.MTSBaseURL != "" && s.cfg.MTSAPIToken != "" {
		if sip, err := s.repo.GetSipAccountByUser(ctx, auth.UserID); err == nil && sip != nil && strings.TrimSpace(sip.AbonentID) != "" {
			endpoint := fmt.Sprintf("%s/user/%s/calls", strings.TrimRight(s.cfg.MTSBaseURL, "/"), url.PathEscape(strings.TrimSpace(sip.AbonentID)))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err == nil {
				req.Header.Set("X-AUTH-TOKEN", s.cfg.MTSAPIToken)
				if resp, err := s.httpClient.Do(req); err == nil {
					defer resp.Body.Close()
					if resp.StatusCode < 300 {
						var mtsCalls []MTSVPBXPayload
						if err := json.NewDecoder(resp.Body).Decode(&mtsCalls); err == nil && len(mtsCalls) > 0 {
							out := make([]CallRecord, 0, len(mtsCalls))
							for _, mc := range mtsCalls {
								remote := stripTelURI(mc.RemotePartyAddress)
								if p10, err := platform.ParsePhone10(remote); err == nil {
									remote = p10
								}
								rec := CallRecord{
									ExternalCallID: mc.CallID,
									AbonentID:      sip.AbonentID,
									ExtTrackingID:  mc.ExtTrackingID,
									Status:         strings.ToLower(firstNonEmpty(mc.State, "active")),
									CallerPhone:    remote,
									UserID:         &auth.UserID,
									StartedAt:      time.UnixMilli(mc.StartTime).UTC(),
								}
								if existing, err := s.repo.GetCallByExternalID(ctx, mc.CallID); err == nil && existing != nil {
									rec = *existing
								} else if len(remote) == 10 {
									if c, err := s.repo.FindUniqueContractorByPhone(ctx, remote); err == nil && c != nil {
										rec.ContractorID = &c.ID
										rec.ContractorName = c.Name
									}
								}
								out = append(out, rec)
							}
							return out, nil
						}
					}
				}
			}
		}
	}
	all, err := s.repo.ListCalls(ctx, CallFilter{UserID: &auth.UserID, Limit: 10})
	if err != nil {
		return nil, err
	}
	var active []CallRecord
	for _, c := range all {
		if c.EndedAt == nil && c.Status != "ended" && c.Status != "missed" && c.Status != "completed" {
			active = append(active, c)
		}
	}
	return active, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
