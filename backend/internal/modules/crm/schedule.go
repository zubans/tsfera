package crm

import (
	"math"
	"sort"
	"time"
)

// TimesheetKind — вид записи табеля: смена (рабочая или дежурная) либо отсутствие.
type TimesheetKind string

const (
	TimesheetWork     TimesheetKind = "work"     // рабочая смена
	TimesheetDuty     TimesheetKind = "duty"     // дежурство: отдельная смена, обычные заявки только по согласованию
	TimesheetVacation TimesheetKind = "vacation" // отпуск, вычитается из плана
	TimesheetSick     TimesheetKind = "sick"     // больничный, вычитается из плана
	TimesheetUnpaid   TimesheetKind = "unpaid"   // без сохранения, из плана не вычитается
	TimesheetReserve  TimesheetKind = "reserve"  // резерв на часть дня с обязательной причиной
)

func (k TimesheetKind) Valid() bool {
	switch k {
	case TimesheetWork, TimesheetDuty, TimesheetVacation, TimesheetSick, TimesheetUnpaid, TimesheetReserve:
		return true
	}
	return false
}

// IsShift сообщает, что запись добавляет рабочее время, а не забирает его.
func (k TimesheetKind) IsShift() bool {
	return k == TimesheetWork || k == TimesheetDuty
}

// ReducesPlan — отсутствия, на которые уменьшается план (б/с и резерв план не уменьшают).
func (k TimesheetKind) ReducesPlan() bool {
	return k == TimesheetVacation || k == TimesheetSick
}

// TimesheetEntry — запись табеля инженера.
type TimesheetEntry struct {
	ID            int64         `json:"id"`
	UserID        int64         `json:"user_id"`
	SubdivisionID *int64        `json:"subdivision_id,omitempty"`
	Kind          TimesheetKind `json:"kind"`
	StartsAt      time.Time     `json:"starts_at"`
	EndsAt        time.Time     `json:"ends_at"`
	Comment       string        `json:"comment"`
	CreatedBy     int64         `json:"created_by,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// DailyPlan — дневной финансовый план инженера в подразделении (городе), действующий с даты.
type DailyPlan struct {
	ID            int64     `json:"id"`
	SubdivisionID int64     `json:"subdivision_id"`
	Amount        float64   `json:"amount"`
	EffectiveFrom time.Time `json:"effective_from"`
	CreatedBy     int64     `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Interval — полуоткрытый интервал времени [From, To).
type Interval struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (iv Interval) Duration() time.Duration { return iv.To.Sub(iv.From) }

func (iv Interval) clip(bounds Interval) (Interval, bool) {
	from, to := iv.From, iv.To
	if from.Before(bounds.From) {
		from = bounds.From
	}
	if to.After(bounds.To) {
		to = bounds.To
	}
	if !to.After(from) {
		return Interval{}, false
	}
	return Interval{From: from, To: to}, true
}

// mergeIntervals склеивает пересекающиеся и смежные интервалы.
func mergeIntervals(in []Interval) []Interval {
	items := make([]Interval, 0, len(in))
	for _, iv := range in {
		if iv.To.After(iv.From) {
			items = append(items, iv)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].From.Before(items[j].From) })
	var out []Interval
	for _, iv := range items {
		if n := len(out); n > 0 && !iv.From.After(out[n-1].To) {
			if iv.To.After(out[n-1].To) {
				out[n-1].To = iv.To
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// subtractIntervals возвращает части base, не покрытые cut.
func subtractIntervals(base, cut []Interval) []Interval {
	base = mergeIntervals(base)
	cut = mergeIntervals(cut)
	var out []Interval
	for _, b := range base {
		cur := b.From
		for _, c := range cut {
			if !c.To.After(cur) || !c.From.Before(b.To) {
				continue
			}
			if c.From.After(cur) {
				out = append(out, Interval{From: cur, To: c.From})
			}
			if c.To.After(cur) {
				cur = c.To
			}
			if !cur.Before(b.To) {
				break
			}
		}
		if cur.Before(b.To) {
			out = append(out, Interval{From: cur, To: b.To})
		}
	}
	return out
}

// intersectIntervals возвращает общие части двух наборов интервалов.
func intersectIntervals(a, b []Interval) []Interval {
	a = mergeIntervals(a)
	b = mergeIntervals(b)
	var out []Interval
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		from := a[i].From
		if b[j].From.After(from) {
			from = b[j].From
		}
		to := a[i].To
		if b[j].To.Before(to) {
			to = b[j].To
		}
		if to.After(from) {
			out = append(out, Interval{From: from, To: to})
		}
		if a[i].To.Before(b[j].To) {
			i++
		} else {
			j++
		}
	}
	return out
}

func totalDuration(in []Interval) time.Duration {
	var d time.Duration
	for _, iv := range mergeIntervals(in) {
		d += iv.Duration()
	}
	return d
}

// FreeSlot — свободное окно инженера. Short — окно короче порога: показывается бледно.
type FreeSlot struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Short bool      `json:"short"`
}

// DayLoadInput — всё, что нужно для расчёта загрузки одного инженера за один день.
type DayLoadInput struct {
	Day         Interval
	Entries     []TimesheetEntry
	Busy        []Interval
	DefaultWork []Interval // рабочее окно по умолчанию, если в табеле на этот день ничего нет
	MinFree     time.Duration
	MinShown    time.Duration
}

// DayLoad — результат расчёта загрузки инженера за день.
type DayLoad struct {
	Work           []Interval `json:"work"`
	Duty           []Interval `json:"duty"`
	Absences       []Absence  `json:"absences"`
	Busy           []Interval `json:"busy"`
	Free           []FreeSlot `json:"free"`
	WorkMinutes    int        `json:"work_minutes"`
	BusyMinutes    int        `json:"busy_minutes"`
	LoadPercent    *int       `json:"load_percent"`
	OnDuty         bool       `json:"on_duty"`
	ScheduleSource string     `json:"schedule_source"` // timesheet — из табеля, default — окно по умолчанию
}

// Absence — отсутствие в пределах дня.
type Absence struct {
	Kind    TimesheetKind `json:"kind"`
	From    time.Time     `json:"from"`
	To      time.Time     `json:"to"`
	Comment string        `json:"comment"`
}

// ComputeDayLoad считает рабочее время, занятость и свободные окна инженера.
// Свободными считаются только части рабочих смен: дежурство помечается, но окна не даёт,
// потому что дежурному обычные заявки ставят лишь по согласованию.
func ComputeDayLoad(in DayLoadInput) DayLoad {
	var work, duty, absent []Interval
	out := DayLoad{ScheduleSource: "timesheet"}
	hasShift := false
	for _, e := range in.Entries {
		iv, ok := Interval{From: e.StartsAt, To: e.EndsAt}.clip(in.Day)
		if !ok {
			continue
		}
		switch {
		case e.Kind == TimesheetWork:
			work = append(work, iv)
			hasShift = true
		case e.Kind == TimesheetDuty:
			duty = append(duty, iv)
			hasShift = true
		default:
			absent = append(absent, iv)
			out.Absences = append(out.Absences, Absence{Kind: e.Kind, From: iv.From, To: iv.To, Comment: e.Comment})
		}
	}
	if !hasShift && len(out.Absences) == 0 && len(in.DefaultWork) > 0 {
		for _, iv := range in.DefaultWork {
			if c, ok := iv.clip(in.Day); ok {
				work = append(work, c)
			}
		}
		out.ScheduleSource = "default"
	}
	duty = mergeIntervals(duty)
	work = subtractIntervals(mergeIntervals(work), duty)
	available := subtractIntervals(work, absent)

	var busy []Interval
	for _, b := range in.Busy {
		if c, ok := b.clip(in.Day); ok {
			busy = append(busy, c)
		}
	}
	busy = mergeIntervals(busy)

	out.Work = work
	out.Duty = duty
	out.OnDuty = len(duty) > 0
	out.Busy = busy
	workDur := totalDuration(available)
	busyInWork := totalDuration(intersectIntervals(available, busy))
	out.WorkMinutes = int(workDur / time.Minute)
	out.BusyMinutes = int(totalDuration(busy) / time.Minute)
	if workDur > 0 {
		pct := int((busyInWork*100 + workDur/2) / workDur)
		out.LoadPercent = &pct
	}
	for _, iv := range subtractIntervals(available, busy) {
		if iv.Duration() < in.MinShown {
			continue
		}
		out.Free = append(out.Free, FreeSlot{From: iv.From, To: iv.To, Short: iv.Duration() < in.MinFree})
	}
	if out.Work == nil {
		out.Work = []Interval{}
	}
	if out.Duty == nil {
		out.Duty = []Interval{}
	}
	if out.Absences == nil {
		out.Absences = []Absence{}
	}
	if out.Busy == nil {
		out.Busy = []Interval{}
	}
	if out.Free == nil {
		out.Free = []FreeSlot{}
	}
	return out
}

// PlanDay — рабочий день инженера, на который начисляется дневной план.
type PlanDay struct {
	Date          time.Time
	SubdivisionID *int64
}

// CountPlanDays возвращает дни плана: каждая рабочая смена — один день по дате начала
// (суточная смена тоже один день). Смены, пересекающиеся с отпуском или больничным,
// не считаются; б/с и резерв план не уменьшают. Дежурство в план не входит.
func CountPlanDays(entries []TimesheetEntry, from, to time.Time, loc *time.Location) []PlanDay {
	var reducing []Interval
	for _, e := range entries {
		if e.Kind.ReducesPlan() {
			reducing = append(reducing, Interval{From: e.StartsAt, To: e.EndsAt})
		}
	}
	seen := map[string]bool{}
	var out []PlanDay
	for _, e := range entries {
		if e.Kind != TimesheetWork {
			continue
		}
		start := e.StartsAt.In(loc)
		day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		if day.Before(from) || !day.Before(to) {
			continue
		}
		if len(intersectIntervals([]Interval{{From: e.StartsAt, To: e.EndsAt}}, reducing)) > 0 {
			continue
		}
		key := day.Format("2006-01-02")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, PlanDay{Date: day, SubdivisionID: e.SubdivisionID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// PlanAmountOn возвращает дневной план подразделения, действующий на дату (последний с effective_from <= day).
func PlanAmountOn(plans []DailyPlan, subdivisionID int64, day time.Time) float64 {
	var best *DailyPlan
	for i := range plans {
		p := &plans[i]
		if p.SubdivisionID != subdivisionID || p.EffectiveFrom.After(day) {
			continue
		}
		if best == nil || p.EffectiveFrom.After(best.EffectiveFrom) {
			best = p
		}
	}
	if best == nil {
		return 0
	}
	return best.Amount
}

// ShiftPattern — шаблон графика для заполнения табеля.
type ShiftPattern string

const (
	Pattern52 ShiftPattern = "5/2"
	Pattern22 ShiftPattern = "2/2"
	Pattern33 ShiftPattern = "3/3"
	Pattern11 ShiftPattern = "1/1"
)

// patternWorks сообщает, рабочий ли день по шаблону. Для сменных графиков отсчёт идёт от anchor —
// первого рабочего дня цикла.
func patternWorks(p ShiftPattern, day, anchor time.Time) (bool, bool) {
	if p == Pattern52 {
		wd := day.Weekday()
		return wd != time.Saturday && wd != time.Sunday, true
	}
	var on, off int
	switch p {
	case Pattern22:
		on, off = 2, 2
	case Pattern33:
		on, off = 3, 3
	case Pattern11:
		on, off = 1, 1
	default:
		return false, false
	}
	days := int(math.Round(day.Sub(anchor).Hours() / 24))
	cycle := on + off
	pos := ((days % cycle) + cycle) % cycle
	return pos < on, true
}

// GenerateShifts строит смены по шаблону на период [from, to) по местному времени.
// Если конец смены не позже начала, смена заканчивается на следующий день (суточные графики).
func GenerateShifts(p ShiftPattern, anchor, from, to time.Time, startClock, endClock time.Duration, loc *time.Location) ([]Interval, bool) {
	if _, ok := patternWorks(p, from, anchor); !ok {
		return nil, false
	}
	var out []Interval
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		works, _ := patternWorks(p, day, anchor)
		if !works {
			continue
		}
		base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		start := base.Add(startClock)
		end := base.Add(endClock)
		if !end.After(start) {
			end = end.AddDate(0, 0, 1)
		}
		out = append(out, Interval{From: start, To: end})
	}
	return out, true
}
