<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCheckbox,
  NDatePicker,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  useMessage
} from 'naive-ui'
import { ApiError, requestJSON } from '@/shared/api/http'
import { formatMoneyRUB } from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type { DailyPlan, PlanFactReport, Subdivision, TimesheetEntry, TimesheetKind, UserBrief } from '@/shared/types/domain'

const auth = useAuthStore()
const message = useMessage()

const canViewTimesheet = computed(
  () => auth.hasPerm('crm.timesheet.view') || auth.hasPerm('crm.timesheet.manage') || auth.hasPerm('crm.calendar.view')
)
const canManageTimesheet = computed(() => auth.hasPerm('crm.timesheet.manage'))
const canViewPlan = computed(
  () => auth.hasPerm('crm.plan.view') || auth.hasPerm('crm.plan.manage') || auth.hasPerm('crm.plan.view_own')
)
const canManagePlan = computed(() => auth.hasPerm('crm.plan.manage'))
const canManageSettings = computed(() => auth.hasPerm('crm.setting.manage'))

const tab = ref(canViewTimesheet.value ? 'timesheet' : 'plan')
const month = ref(startOfMonth(new Date()).getTime())
const subdivisionId = ref<number | null>(auth.user?.subdivision_id ?? null)
const engineers = ref<UserBrief[]>([])
const subdivisions = ref<Subdivision[]>([])
const entries = ref<TimesheetEntry[]>([])
const loading = ref(false)
const errorMsg = ref('')

const KIND_LABELS: Record<TimesheetKind, string> = {
  work: 'Смена',
  duty: 'Дежурство',
  vacation: 'Отпуск',
  sick: 'Больничный',
  unpaid: 'Б/с',
  reserve: 'Резерв'
}
const kindOptions = (Object.keys(KIND_LABELS) as TimesheetKind[]).map((k) => ({ label: KIND_LABELS[k], value: k }))

function startOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1)
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

function isoDate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function hhmm(d: Date): string {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const monthStart = computed(() => startOfMonth(new Date(month.value)))
const monthEnd = computed(() => new Date(monthStart.value.getFullYear(), monthStart.value.getMonth() + 1, 0))
const days = computed(() => {
  const out: Date[] = []
  for (let d = 1; d <= monthEnd.value.getDate(); d++) {
    out.push(new Date(monthStart.value.getFullYear(), monthStart.value.getMonth(), d))
  }
  return out
})

const subdivisionOptions = computed(() => subdivisions.value.map((s) => ({ label: s.name, value: s.id })))
const engineerOptions = computed(() => engineers.value.map((e) => ({ label: e.full_name || e.username, value: e.id })))

const rows = computed(() => {
  const ids = new Set(entries.value.map((e) => e.user_id))
  return engineers.value
    .filter((e) => !subdivisionId.value || e.subdivision_id === subdivisionId.value || ids.has(e.id))
    .sort((a, b) => (a.full_name || a.username).localeCompare(b.full_name || b.username, 'ru'))
})

function entriesFor(userId: number, day: Date): TimesheetEntry[] {
  const from = day.getTime()
  const to = from + 24 * 3600 * 1000
  return entries.value.filter((e) => {
    if (e.user_id !== userId) return false
    const s = new Date(e.starts_at).getTime()
    const f = new Date(e.ends_at).getTime()
    // Смена относится к дню начала, отсутствие — ко всем дням, которые покрывает.
    if (e.kind === 'work' || e.kind === 'duty') return s >= from && s < to
    return s < to && f > from
  })
}

function cellLabel(e: TimesheetEntry): string {
  const s = new Date(e.starts_at)
  const f = new Date(e.ends_at)
  switch (e.kind) {
    case 'work':
      return f.getTime() - s.getTime() >= 20 * 3600 * 1000 ? `${s.getHours()}→${f.getHours()}` : `${s.getHours()}–${f.getHours()}`
    case 'duty':
      return 'Д'
    case 'vacation':
      return 'О'
    case 'sick':
      return 'Б'
    case 'unpaid':
      return 'б/с'
    case 'reserve':
      return 'Р'
  }
  return ''
}

async function loadMeta() {
  const [eng, subs] = await Promise.allSettled([
    requestJSON<UserBrief[]>('/api/v1/crm/workers?engineer=true'),
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions')
  ])
  if (eng.status === 'fulfilled') engineers.value = eng.value ?? []
  if (subs.status === 'fulfilled') subdivisions.value = subs.value ?? []
}

async function loadTimesheet() {
  if (!canViewTimesheet.value) return
  loading.value = true
  errorMsg.value = ''
  try {
    const params = new URLSearchParams({ from: isoDate(monthStart.value), to: isoDate(monthEnd.value) })
    if (subdivisionId.value) params.set('subdivision_id', String(subdivisionId.value))
    entries.value = (await requestJSON<TimesheetEntry[]>(`/api/v1/crm/timesheet?${params}`)) ?? []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить табель'
  } finally {
    loading.value = false
  }
}

// --- Редактирование дня ---
const dayModal = ref<{ userId: number; day: Date } | null>(null)
const entryForm = ref({ id: 0, kind: 'work' as TimesheetKind, from: '', to: '', comment: '', subdivision_id: null as number | null })

function localInput(d: Date): string {
  return `${isoDate(d)}T${hhmm(d)}`
}

function openDay(userId: number, day: Date) {
  dayModal.value = { userId, day }
  resetEntryForm(day)
}

function resetEntryForm(day: Date) {
  const s = new Date(day)
  s.setHours(8, 0, 0, 0)
  const f = new Date(day)
  f.setHours(17, 0, 0, 0)
  entryForm.value = { id: 0, kind: 'work', from: localInput(s), to: localInput(f), comment: '', subdivision_id: null }
}

function editEntry(e: TimesheetEntry) {
  entryForm.value = {
    id: e.id,
    kind: e.kind,
    from: localInput(new Date(e.starts_at)),
    to: localInput(new Date(e.ends_at)),
    comment: e.comment,
    subdivision_id: e.subdivision_id ?? null
  }
}

const dayEntries = computed(() => (dayModal.value ? entriesFor(dayModal.value.userId, dayModal.value.day) : []))
const dayTitle = computed(() => {
  if (!dayModal.value) return ''
  const eng = engineers.value.find((e) => e.id === dayModal.value?.userId)
  return `${eng?.full_name || eng?.username || ''}, ${dayModal.value.day.toLocaleDateString('ru-RU')}`
})

async function saveEntry() {
  if (!dayModal.value) return
  const body = {
    user_id: dayModal.value.userId,
    kind: entryForm.value.kind,
    starts_at: new Date(entryForm.value.from).toISOString(),
    ends_at: new Date(entryForm.value.to).toISOString(),
    comment: entryForm.value.comment,
    subdivision_id: entryForm.value.subdivision_id
  }
  try {
    if (entryForm.value.id) {
      await requestJSON(`/api/v1/crm/timesheet/${entryForm.value.id}`, { method: 'PUT', body: JSON.stringify(body) })
    } else {
      await requestJSON('/api/v1/crm/timesheet', { method: 'POST', body: JSON.stringify(body) })
    }
    resetEntryForm(dayModal.value.day)
    await loadTimesheet()
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : 'Не удалось сохранить запись табеля')
  }
}

async function deleteEntry(id: number) {
  try {
    await requestJSON(`/api/v1/crm/timesheet/${id}`, { method: 'DELETE' })
    await loadTimesheet()
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : 'Не удалось удалить запись')
  }
}

// --- Заполнение по графику ---
const genOpen = ref(false)
const genForm = ref({
  user_id: null as number | null,
  pattern: '5/2',
  kind: 'work' as TimesheetKind,
  cycle_start: null as number | null,
  period: null as [number, number] | null,
  time_from: '08:00',
  time_to: '17:00',
  subdivision_id: null as number | null,
  replace: true
})

function openGenerate() {
  genForm.value.period = [monthStart.value.getTime(), monthEnd.value.getTime()]
  genForm.value.cycle_start = monthStart.value.getTime()
  genOpen.value = true
}

async function runGenerate() {
  const f = genForm.value
  if (!f.user_id || !f.period) {
    message.warning('Выберите сотрудника и период')
    return
  }
  try {
    const res = await requestJSON<{ created: number }>('/api/v1/crm/timesheet/generate', {
      method: 'POST',
      body: JSON.stringify({
        user_id: f.user_id,
        pattern: f.pattern,
        kind: f.kind,
        cycle_start: f.cycle_start ? isoDate(new Date(f.cycle_start)) : '',
        date_from: isoDate(new Date(f.period[0])),
        date_to: isoDate(new Date(f.period[1])),
        time_from: f.time_from,
        time_to: f.time_to,
        subdivision_id: f.subdivision_id,
        replace: f.replace
      })
    })
    message.success(`Создано смен: ${res.created}`)
    genOpen.value = false
    await loadTimesheet()
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : 'Не удалось заполнить табель')
  }
}

// --- План/факт ---
const report = ref<PlanFactReport | null>(null)
const plans = ref<DailyPlan[]>([])
const planForm = ref({ subdivision_id: null as number | null, amount: 0, effective_from: Date.now() })

function subdivisionName(id?: number | null): string {
  return subdivisions.value.find((s) => s.id === id)?.name ?? (id ? `#${id}` : '—')
}

async function loadPlanFact() {
  if (!canViewPlan.value) return
  loading.value = true
  try {
    const params = new URLSearchParams({ from: isoDate(monthStart.value), to: isoDate(monthEnd.value) })
    if (subdivisionId.value) params.set('subdivision_id', String(subdivisionId.value))
    const [rep, pl] = await Promise.all([
      requestJSON<PlanFactReport>(`/api/v1/crm/reports/plan-fact?${params}`),
      requestJSON<DailyPlan[]>('/api/v1/crm/daily-plans')
    ])
    report.value = rep
    plans.value = pl ?? []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить план/факт'
  } finally {
    loading.value = false
  }
}

async function savePlan() {
  if (!planForm.value.subdivision_id) {
    message.warning('Выберите подразделение')
    return
  }
  try {
    await requestJSON('/api/v1/crm/daily-plans', {
      method: 'POST',
      body: JSON.stringify({
        subdivision_id: planForm.value.subdivision_id,
        amount: Number(planForm.value.amount || 0),
        effective_from: isoDate(new Date(planForm.value.effective_from))
      })
    })
    await loadPlanFact()
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : 'Не удалось сохранить план')
  }
}

async function deletePlan(id: number) {
  await requestJSON(`/api/v1/crm/daily-plans/${id}`, { method: 'DELETE' })
  await loadPlanFact()
}

function percentType(p: number | null): 'success' | 'warning' | 'error' | 'default' {
  if (p === null) return 'default'
  if (p >= 100) return 'success'
  if (p >= 70) return 'warning'
  return 'error'
}

// --- Настройки расписания ---
const settings = ref({
  default_work_from: '08:00',
  default_work_to: '17:00',
  min_free_minutes: 60,
  min_shown_minutes: 15,
  grid_from: '07:00',
  grid_to: '23:00',
  service_user_ids: [] as number[]
})

async function loadSettings() {
  if (!canManageSettings.value) return
  try {
    const s = await requestJSON<{ data: Partial<typeof settings.value> }>('/api/v1/crm/app-settings/calendar_load')
    if (s?.data) settings.value = { ...settings.value, ...s.data }
  } catch {
    // Настройка ещё не сохранялась — остаются значения по умолчанию.
  }
}

async function saveSettings() {
  try {
    await requestJSON('/api/v1/crm/app-settings/calendar_load', {
      method: 'PUT',
      body: JSON.stringify({ name: 'Расписание инженеров', data: settings.value })
    })
    message.success('Настройки расписания сохранены')
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : 'Не удалось сохранить настройки')
  }
}

function reload() {
  if (tab.value === 'timesheet') void loadTimesheet()
  if (tab.value === 'plan') void loadPlanFact()
}

watch([month, subdivisionId, tab], reload)

onMounted(async () => {
  await loadMeta()
  await loadSettings()
  reload()
})
</script>

<template>
  <div class="ts-page">
    <div class="toolbar">
      <NSpace align="center">
        <NDatePicker v-model:value="month" type="month" format="LLLL yyyy" :clearable="false" style="width: 170px" />
        <NSelect
          v-if="auth.hasPerm('crm.calendar.select_subdivision') || auth.hasPerm('crm.plan.view')"
          v-model:value="subdivisionId"
          clearable
          placeholder="Все подразделения"
          style="width: 220px"
          :options="subdivisionOptions"
        />
      </NSpace>
      <NButton v-if="tab === 'timesheet' && canManageTimesheet" type="primary" @click="openGenerate">Заполнить по графику</NButton>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NTabs v-model:value="tab" type="line">
      <NTabPane v-if="canViewTimesheet" name="timesheet" tab="Табель">
        <NSpin :show="loading">
          <div class="legend">
            <span>8–17 — смена</span><span>9→9 — сутки</span><span>Д — дежурство</span><span>О — отпуск</span>
            <span>Б — больничный</span><span>б/с — без содержания</span><span>Р — резерв</span>
          </div>
          <div class="ts-scroll">
            <table class="ts-table">
              <thead>
                <tr>
                  <th class="name">Сотрудник</th>
                  <th v-for="d in days" :key="d.getDate()" :class="{ weekend: d.getDay() === 0 || d.getDay() === 6 }">
                    {{ d.getDate() }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="eng in rows" :key="eng.id">
                  <td class="name">{{ eng.full_name || eng.username }}</td>
                  <td
                    v-for="d in days"
                    :key="d.getDate()"
                    class="cell"
                    :class="{ weekend: d.getDay() === 0 || d.getDay() === 6, editable: canManageTimesheet }"
                    @click="canManageTimesheet && openDay(eng.id, d)"
                  >
                    <span
                      v-for="e in entriesFor(eng.id, d)"
                      :key="e.id"
                      class="chip"
                      :class="`k-${e.kind}`"
                      :title="`${KIND_LABELS[e.kind]}${e.comment ? ': ' + e.comment : ''}${e.subdivision_id ? ' · ' + subdivisionName(e.subdivision_id) : ''}`"
                    >
                      {{ cellLabel(e) }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </NSpin>
      </NTabPane>

      <NTabPane v-if="canViewPlan" name="plan" tab="План / факт">
        <NSpin :show="loading">
          <div v-if="report" class="pf-summary">
            <div><span class="muted">План</span><strong>{{ formatMoneyRUB(report.plan_amount) }}</strong></div>
            <div><span class="muted">Факт (оплачено)</span><strong>{{ formatMoneyRUB(report.fact_amount) }}</strong></div>
            <div>
              <span class="muted">Выполнение</span>
              <strong>{{ report.percent === null ? '—' : `${report.percent}%` }}</strong>
            </div>
          </div>
          <table v-if="report" class="pf-table">
            <thead>
              <tr>
                <th>Инженер</th>
                <th>Подразделение</th>
                <th>Дней в плане</th>
                <th>План</th>
                <th>Факт</th>
                <th>%</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in report.rows" :key="r.user_id">
                <td>
                  {{ r.full_name }}
                  <NTag v-if="r.no_timesheet" size="tiny" :bordered="false">нет табеля</NTag>
                </td>
                <td>{{ subdivisionName(r.subdivision_id) }}</td>
                <td>{{ r.plan_days }}</td>
                <td>{{ formatMoneyRUB(r.plan_amount) }}</td>
                <td>{{ formatMoneyRUB(r.fact_amount) }}</td>
                <td>
                  <NTag size="small" :type="percentType(r.percent)">{{ r.percent === null ? '—' : `${r.percent}%` }}</NTag>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="muted note">
            План — дневной план города смены × рабочие смены по табелю; отпуска и больничные вычитаются, б/с — нет.
            Факт — оплаченная доля инженера в актах по дате работ.
          </div>

          <div v-if="canManagePlan" class="plans">
            <h3>Дневной план по подразделениям</h3>
            <NSpace align="center">
              <NSelect v-model:value="planForm.subdivision_id" placeholder="Подразделение" style="width: 200px" :options="subdivisionOptions" />
              <NInputNumber v-model:value="planForm.amount" :min="0" :step="1000" style="width: 160px" placeholder="Сумма в день" />
              <NDatePicker v-model:value="planForm.effective_from" type="date" format="dd.MM.yyyy" style="width: 150px" />
              <NButton type="primary" @click="savePlan">Установить</NButton>
            </NSpace>
            <table class="pf-table" style="margin-top: 10px">
              <thead>
                <tr><th>Подразделение</th><th>Действует с</th><th>План в день</th><th /></tr>
              </thead>
              <tbody>
                <tr v-for="p in plans" :key="p.id">
                  <td>{{ subdivisionName(p.subdivision_id) }}</td>
                  <td>{{ new Date(p.effective_from).toLocaleDateString('ru-RU', { timeZone: 'UTC' }) }}</td>
                  <td>{{ formatMoneyRUB(p.amount) }}</td>
                  <td><NButton size="tiny" quaternary type="error" @click="deletePlan(p.id)">Удалить</NButton></td>
                </tr>
              </tbody>
            </table>
          </div>
        </NSpin>
      </NTabPane>

      <NTabPane v-if="canManageSettings" name="settings" tab="Настройки расписания">
        <NForm label-placement="left" label-width="260" style="max-width: 640px">
          <NFormItem label="Рабочее время без табеля">
            <NSpace>
              <NInput v-model:value="settings.default_work_from" style="width: 90px" placeholder="08:00" />
              <NInput v-model:value="settings.default_work_to" style="width: 90px" placeholder="17:00" />
            </NSpace>
          </NFormItem>
          <NFormItem label="Сетка расписания">
            <NSpace>
              <NInput v-model:value="settings.grid_from" style="width: 90px" placeholder="07:00" />
              <NInput v-model:value="settings.grid_to" style="width: 90px" placeholder="23:00" />
            </NSpace>
          </NFormItem>
          <NFormItem label="Явно выделять окна от, мин">
            <NInputNumber v-model:value="settings.min_free_minutes" :min="15" :step="15" style="width: 120px" />
          </NFormItem>
          <NFormItem label="Не показывать разрывы короче, мин">
            <NInputNumber v-model:value="settings.min_shown_minutes" :min="5" :step="5" style="width: 120px" />
          </NFormItem>
          <NFormItem label="Служебные учётки («Офис»)">
            <NSelect v-model:value="settings.service_user_ids" multiple filterable :options="engineerOptions" />
          </NFormItem>
          <NButton type="primary" @click="saveSettings">Сохранить</NButton>
        </NForm>
      </NTabPane>
    </NTabs>

    <NModal :show="dayModal !== null" preset="card" :title="dayTitle" style="width: 560px; max-width: 96vw" @update:show="(v: boolean) => !v && (dayModal = null)">
      <div v-for="e in dayEntries" :key="e.id" class="day-entry">
        <span class="chip" :class="`k-${e.kind}`">{{ KIND_LABELS[e.kind] }}</span>
        <span>{{ new Date(e.starts_at).toLocaleString('ru-RU') }} — {{ new Date(e.ends_at).toLocaleString('ru-RU') }}</span>
        <span v-if="e.comment" class="muted">{{ e.comment }}</span>
        <NSpace :size="4">
          <NButton size="tiny" @click="editEntry(e)">Изменить</NButton>
          <NButton size="tiny" type="error" quaternary @click="deleteEntry(e.id)">Удалить</NButton>
        </NSpace>
      </div>
      <NForm label-placement="top" style="margin-top: 12px">
        <div class="form-grid">
          <NFormItem label="Вид">
            <NSelect v-model:value="entryForm.kind" :options="kindOptions" />
          </NFormItem>
          <NFormItem v-if="entryForm.kind === 'work' || entryForm.kind === 'duty'" label="Город смены">
            <NSelect v-model:value="entryForm.subdivision_id" clearable placeholder="Своё подразделение" :options="subdivisionOptions" />
          </NFormItem>
          <NFormItem label="Начало">
            <input v-model="entryForm.from" type="datetime-local" class="native-input" />
          </NFormItem>
          <NFormItem label="Окончание">
            <input v-model="entryForm.to" type="datetime-local" class="native-input" />
          </NFormItem>
        </div>
        <NFormItem :label="entryForm.kind === 'reserve' ? 'Причина (обязательно)' : 'Комментарий'">
          <NInput v-model:value="entryForm.comment" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton v-if="entryForm.id && dayModal" @click="resetEntryForm(dayModal.day)">Новая запись</NButton>
          <NButton type="primary" @click="saveEntry">{{ entryForm.id ? 'Сохранить' : 'Добавить' }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="genOpen" preset="card" title="Заполнить табель по графику" style="width: 560px; max-width: 96vw">
      <NForm label-placement="top">
        <div class="form-grid">
          <NFormItem label="Сотрудник">
            <NSelect v-model:value="genForm.user_id" filterable :options="engineerOptions" />
          </NFormItem>
          <NFormItem label="График">
            <NSelect
              v-model:value="genForm.pattern"
              :options="[
                { label: '5/2 (пн–пт)', value: '5/2' },
                { label: '2/2', value: '2/2' },
                { label: '3/3', value: '3/3' },
                { label: '1/1 (сутки через сутки)', value: '1/1' }
              ]"
            />
          </NFormItem>
          <NFormItem label="Вид смены">
            <NSelect v-model:value="genForm.kind" :options="kindOptions.filter((o) => o.value === 'work' || o.value === 'duty')" />
          </NFormItem>
          <NFormItem v-if="genForm.pattern !== '5/2'" label="Первый рабочий день цикла">
            <NDatePicker v-model:value="genForm.cycle_start" type="date" format="dd.MM.yyyy" />
          </NFormItem>
          <NFormItem label="Начало смены">
            <NInput v-model:value="genForm.time_from" placeholder="08:00" />
          </NFormItem>
          <NFormItem label="Конец смены (не позже начала — до след. дня)">
            <NInput v-model:value="genForm.time_to" placeholder="17:00" />
          </NFormItem>
          <NFormItem label="Город смены">
            <NSelect v-model:value="genForm.subdivision_id" clearable placeholder="Своё подразделение" :options="subdivisionOptions" />
          </NFormItem>
        </div>
        <NFormItem label="Период">
          <NDatePicker v-model:value="genForm.period" type="daterange" format="dd.MM.yyyy" />
        </NFormItem>
        <NCheckbox v-model:checked="genForm.replace">Заменить прежние смены периода (отпуска и больничные сохраняются)</NCheckbox>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="genOpen = false">Отмена</NButton>
          <NButton type="primary" @click="runGenerate">Заполнить</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.ts-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.toolbar {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  font-size: 12px;
  color: #64748b;
  margin-bottom: 8px;
}
.ts-scroll {
  overflow-x: auto;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
}
.ts-table {
  border-collapse: collapse;
  font-size: 12px;
  width: 100%;
}
.ts-table th,
.ts-table td {
  border: 1px solid #f1f5f9;
  padding: 3px;
  text-align: center;
  min-width: 34px;
}
.ts-table .name {
  position: sticky;
  left: 0;
  background: #fff;
  text-align: left;
  min-width: 180px;
  white-space: nowrap;
  z-index: 1;
}
.ts-table .weekend {
  background: #f8fafc;
}
.cell.editable {
  cursor: pointer;
}
.cell.editable:hover {
  background: #eff6ff;
}
.chip {
  display: inline-block;
  border-radius: 4px;
  padding: 0 3px;
  margin: 1px;
  font-size: 11px;
  white-space: nowrap;
}
.k-work { background: #dbeafe; color: #1e3a8a; }
.k-duty { background: #fef3c7; color: #92400e; }
.k-vacation { background: #e2e8f0; color: #334155; }
.k-sick { background: #fee2e2; color: #991b1b; }
.k-unpaid { background: #f1f5f9; color: #475569; }
.k-reserve { background: #ede9fe; color: #5b21b6; }
.pf-summary {
  display: flex;
  gap: 24px;
  margin-bottom: 12px;
}
.pf-summary div {
  display: flex;
  flex-direction: column;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 10px 16px;
}
.pf-summary strong {
  font-size: 18px;
}
.pf-table {
  width: 100%;
  border-collapse: collapse;
  background: #fff;
  font-size: 13px;
}
.pf-table th,
.pf-table td {
  border-bottom: 1px solid #f1f5f9;
  padding: 6px 8px;
  text-align: left;
}
.muted {
  color: #64748b;
  font-size: 12px;
}
.note {
  margin-top: 8px;
}
.plans {
  margin-top: 20px;
}
.day-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 6px 0;
  border-bottom: 1px solid #f1f5f9;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 12px;
}
.native-input {
  width: 100%;
  height: 34px;
  border: 1px solid #e0e0e6;
  border-radius: 3px;
  padding: 0 8px;
  font: inherit;
}
</style>
