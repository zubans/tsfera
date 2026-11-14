<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NSpace,
  NButton,
  NSelect,
  NRadioGroup,
  NRadioButton,
  NTag,
  NBadge,
  NRate,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NCheckbox,
  NAlert,
  NDrawer,
  NDrawerContent,
  NDivider,
  NSpin
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import {
  formatPhoneRU,
  formatMoneyRUB,
  formatDateTime,
  toDatetimeLocal,
  datetimeLocalToRFC3339,
  documentStatusLabel
} from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import type {
  Application,
  CalendarEvent,
  UserBrief,
  Subdivision,
  Contractor,
  ContractorAddress,
  RefItem,
  CRMEnums,
  TrafficSource
} from '@/shared/types/domain'

const auth = useAuthStore()
const realtime = useRealtimeStore()
const route = useRoute()
const router = useRouter()

const viewMode = ref<'week' | 'day' | 'list'>('week')
const anchorDate = ref(startOfWeek(new Date()))
const selectedEngineerId = ref<number | null>(null)
const selectedSubdivisionId = ref<number | null>(null)
const onlyMyCalendar = ref(false)
const onlyDuty = ref(false)
const loading = ref(false)
const errorMsg = ref('')

const events = ref<CalendarEvent[]>([])
const engineers = ref<UserBrief[]>([])
const subdivisions = ref<Subdivision[]>([])
const contractors = ref<Contractor[]>([])
const trafficSources = ref<TrafficSource[]>([])
const specialNotePresets = ref<RefItem[]>([])
const modalAddresses = ref<ContractorAddress[]>([])
const hiddenEngineerIds = ref<number[]>([])
const engineerOrder = ref<number[]>([])
const settingsModalOpen = ref(false)
const draggingEventId = ref<number | null>(null)
const enums = ref<CRMEnums>({
  work_types: ['ТО', 'Ремонт', 'Пусконаладка', 'Диагностика', 'Монтаж', 'Аварийный выезд', 'Прочее'],
  document_statuses: [
    { id: 1, label: 'Активна' },
    { id: 2, label: 'Проведена' },
    { id: 3, label: 'Провалена' },
    { id: 4, label: 'Отменена' }
  ],
  task_types: [],
  task_statuses: []
})

const selectedEvent = ref<CalendarEvent | null>(null)
const drawerOpen = ref(false)

const modalOpen = ref(false)
const editId = ref<number | null>(null)
const saving = ref(false)
const modalError = ref('')
const ratingComment = ref('')
const completeComment = ref('')

const appForm = ref({
  contractor_id: null as number | null,
  address_id: null as number | null,
  engineer_id: null as number | null,
  executor_ids: [] as number[],
  traffic_source_id: null as number | null,
  planned_sum: 0,
  work_window_from: '',
  work_window_to: '',
  plan_from: '',
  plan_to: '',
  status: 1,
  is_duty: false,
  work_types: ['ТО'] as string[],
  price_note: '',
  comment: '',
  special_notes: ''
})

function startOfWeek(d: Date): Date {
  const copy = new Date(d)
  copy.setHours(0, 0, 0, 0)
  const day = copy.getDay()
  const diff = day === 0 ? -6 : 1 - day
  copy.setDate(copy.getDate() + diff)
  return copy
}

function addDays(d: Date, n: number): Date {
  const copy = new Date(d)
  copy.setDate(copy.getDate() + n)
  return copy
}

function toISODate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

const rangeDays = computed(() => {
  const count = viewMode.value === 'day' ? 1 : 7
  return Array.from({ length: count }, (_, idx) => addDays(anchorDate.value, idx))
})

const orderedEngineers = computed(() => {
  const rank = new Map(engineerOrder.value.map((id, idx) => [id, idx]))
  return [...engineers.value].sort((a, b) => {
    const ra = rank.has(a.id) ? (rank.get(a.id) ?? 9999) : 9999
    const rb = rank.has(b.id) ? (rank.get(b.id) ?? 9999) : 9999
    if (ra !== rb) return ra - rb
    return (a.full_name || a.username).localeCompare(b.full_name || b.username, 'ru')
  })
})

const filteredEvents = computed(() =>
  events.value.filter((ev) => {
    if (onlyDuty.value && !ev.is_duty) return false
    if (hiddenEngineerIds.value.includes(ev.engineer_id) && !selectedEngineerId.value) return false
    return true
  })
)

const engineerOptions = computed(() => [
  { label: 'Все инженеры', value: 0 },
  ...orderedEngineers.value.map((e) => ({ label: e.full_name || e.username, value: e.id }))
])

const subdivisionOptions = computed(() => [
  { label: 'Все подразделения', value: 0 },
  ...subdivisions.value.map((s) => ({ label: s.name, value: s.id }))
])

const contractorOptions = computed(() =>
  contractors.value.map((c) => ({ label: c.name, value: c.id }))
)

const addressOptions = computed(() =>
  modalAddresses.value.map((a) => ({ label: a.address, value: a.id }))
)

const workTypeOptions = computed(() =>
  enums.value.work_types.map((w) => ({ label: w, value: w }))
)

function eventsForDay(day: Date): CalendarEvent[] {
  const key = toISODate(day)
  return filteredEvents.value
    .filter((ev) => ev.start.slice(0, 10) === key)
    .sort((a, b) => a.start.localeCompare(b.start))
}

function formatTimeRange(start: string, end: string): string {
  const s = new Date(start)
  const e = new Date(end)
  const fmt = (d: Date) =>
    Number.isNaN(d.getTime())
      ? '--:--'
      : d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  return `${fmt(s)}–${fmt(e)}`
}

function formatExecutorNames(ids?: number[]): string {
  if (!ids || ids.length === 0) return ''
  return ids
    .map((id) => engineers.value.find((e) => e.id === id)?.full_name || `#${id}`)
    .join(', ')
}

function statusTagType(status: number): 'info' | 'success' | 'error' | 'warning' {
  if (status === 2) return 'success'
  if (status === 3) return 'error'
  if (status === 4) return 'warning'
  return 'info'
}

async function loadMeta() {
  const [engRes, subRes, enumRes, ctrRes, noteRes, cfgRes, tsRes] = await Promise.allSettled([
    requestJSON<UserBrief[]>('/api/v1/crm/workers?engineer=true'),
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
    requestJSON<CRMEnums>('/api/v1/crm/enums'),
    requestJSON<{ items: Contractor[]; total: number }>('/api/v1/crm/contractors?limit=100'),
    requestJSON<RefItem[]>('/api/v1/crm/refs/special_note'),
    requestJSON<{
      view_mode?: 'week' | 'day' | 'list'
      hidden_engineer_ids?: number[]
      engineer_order?: number[]
      only_duty?: boolean
    }>('/api/v1/crm/settings/calendar_settings'),
    requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources?active=true')
  ])
  if (engRes.status === 'fulfilled') engineers.value = engRes.value ?? []
  if (subRes.status === 'fulfilled') subdivisions.value = subRes.value ?? []
  if (enumRes.status === 'fulfilled' && enumRes.value) enums.value = enumRes.value
  if (ctrRes.status === 'fulfilled' && ctrRes.value) contractors.value = ctrRes.value.items ?? []
  if (noteRes.status === 'fulfilled') specialNotePresets.value = noteRes.value ?? []
  if (tsRes.status === 'fulfilled' && Array.isArray(tsRes.value)) trafficSources.value = tsRes.value
  if (cfgRes.status === 'fulfilled' && cfgRes.value) {
    if (cfgRes.value.view_mode === 'day' || cfgRes.value.view_mode === 'week' || cfgRes.value.view_mode === 'list') {
      viewMode.value = cfgRes.value.view_mode
    }
    if (Array.isArray(cfgRes.value.hidden_engineer_ids)) {
      hiddenEngineerIds.value = cfgRes.value.hidden_engineer_ids
    }
    if (Array.isArray(cfgRes.value.engineer_order)) {
      engineerOrder.value = cfgRes.value.engineer_order
    }
    if (typeof cfgRes.value.only_duty === 'boolean') {
      onlyDuty.value = cfgRes.value.only_duty
    }
  }
}

async function saveCalendarSettings() {
  try {
    await requestJSON('/api/v1/crm/settings/calendar_settings', {
      method: 'PUT',
      body: JSON.stringify({
        view_mode: viewMode.value,
        hidden_engineer_ids: hiddenEngineerIds.value,
        engineer_order: engineerOrder.value,
        only_duty: onlyDuty.value
      })
    })
    settingsModalOpen.value = false
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось сохранить настройки календаря'
  }
}

function toggleHiddenEngineer(id: number, visible: boolean) {
  if (visible) {
    hiddenEngineerIds.value = hiddenEngineerIds.value.filter((x) => x !== id)
  } else if (!hiddenEngineerIds.value.includes(id)) {
    hiddenEngineerIds.value = [...hiddenEngineerIds.value, id]
  }
}

function moveEngineerOrder(id: number, dir: -1 | 1) {
  const list = orderedEngineers.value.map((e) => e.id)
  const idx = list.indexOf(id)
  const target = idx + dir
  if (idx < 0 || target < 0 || target >= list.length) return
  ;[list[idx], list[target]] = [list[target], list[idx]]
  engineerOrder.value = list
}

async function loadCalendar() {
  loading.value = true
  errorMsg.value = ''
  try {
    const from = toISODate(anchorDate.value)
    const to = toISODate(addDays(anchorDate.value, viewMode.value === 'day' ? 1 : 7))
    const params = new URLSearchParams({ from, to })
    if (selectedEngineerId.value && selectedEngineerId.value > 0) {
      params.set('engineer_id', String(selectedEngineerId.value))
    } else if (onlyMyCalendar.value && auth.user?.id) {
      params.set('engineer_id', String(auth.user.id))
    }
    if (selectedSubdivisionId.value && selectedSubdivisionId.value > 0) {
      params.set('subdivision_id', String(selectedSubdivisionId.value))
    }
    const list = await requestJSON<CalendarEvent[]>(`/api/v1/crm/applications/calendar?${params}`)
    events.value = list ?? []
    if (selectedEvent.value) {
      const updated = events.value.find((e) => e.id === selectedEvent.value?.id)
      if (updated) selectedEvent.value = updated
    }
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить календарь'
  } finally {
    loading.value = false
  }
}

async function loadContractorAddresses(contractorId: number | null) {
  if (!contractorId) {
    modalAddresses.value = []
    return
  }
  const items = await requestJSON<ContractorAddress[]>(
    `/api/v1/crm/addresses?contractor_id=${contractorId}&limit=50`
  )
  modalAddresses.value = items ?? []
}

function shiftRange(dir: number) {
  const step = viewMode.value === 'day' ? 1 : 7
  anchorDate.value = addDays(anchorDate.value, dir * step)
}

function resetToday() {
  anchorDate.value = viewMode.value === 'day' ? new Date() : startOfWeek(new Date())
  if (viewMode.value === 'day') {
    anchorDate.value.setHours(0, 0, 0, 0)
  }
}

function openDetails(ev: CalendarEvent) {
  selectedEvent.value = ev
  ratingComment.value = ''
  completeComment.value = ''
  drawerOpen.value = true
}

async function openCreateModal(prefill?: { day?: Date; contractorId?: number; addressId?: number }) {
  editId.value = null
  modalError.value = ''
  const start = prefill?.day ? new Date(prefill.day) : new Date()
  if (prefill?.day) {
    start.setHours(9, 0, 0, 0)
  } else {
    start.setMinutes(0, 0, 0)
  }
  const end = new Date(start.getTime() + 2 * 60 * 60 * 1000)
  appForm.value = {
    contractor_id: prefill?.contractorId ?? contractors.value[0]?.id ?? null,
    address_id: prefill?.addressId ?? null,
    engineer_id: selectedEngineerId.value || orderedEngineers.value[0]?.id || null,
    executor_ids: [],
    traffic_source_id: null,
    planned_sum: 0,
    work_window_from: '',
    work_window_to: '',
    plan_from: toDatetimeLocal(start.toISOString()),
    plan_to: toDatetimeLocal(end.toISOString()),
    status: 1,
    is_duty: false,
    work_types: ['ТО'],
    price_note: '',
    comment: '',
    special_notes: ''
  }
  await loadContractorAddresses(appForm.value.contractor_id)
  if (!appForm.value.address_id && modalAddresses.value.length > 0) {
    appForm.value.address_id = modalAddresses.value[0].id
  }
  modalOpen.value = true
}

async function openEditModal(ev: CalendarEvent) {
  editId.value = ev.id
  modalError.value = ''
  await loadContractorAddresses(ev.contractor_id)
  try {
    const full = await requestJSON<Application>(`/api/v1/crm/applications/${ev.id}`)
    appForm.value = {
      contractor_id: full.contractor_id,
      address_id: full.address_id,
      engineer_id: full.engineer_id,
      executor_ids: [...(full.executor_ids ?? [])],
      traffic_source_id: full.traffic_source_id ?? null,
      planned_sum: full.planned_sum ?? 0,
      work_window_from: full.work_window_from ? toDatetimeLocal(full.work_window_from) : '',
      work_window_to: full.work_window_to ? toDatetimeLocal(full.work_window_to) : '',
      plan_from: toDatetimeLocal(full.plan_datetime_from),
      plan_to: toDatetimeLocal(full.plan_datetime_to),
      status: full.status,
      is_duty: full.is_duty,
      work_types: [...(full.work_types ?? [])],
      price_note: full.price_note || '',
      comment: full.comment || '',
      special_notes: full.special_notes || ''
    }
  } catch {
    appForm.value = {
      contractor_id: ev.contractor_id,
      address_id: ev.address_id,
      engineer_id: ev.engineer_id,
      executor_ids: [...(ev.executor_ids ?? [])],
      traffic_source_id: ev.traffic_source_id ?? null,
      planned_sum: ev.planned_sum ?? 0,
      work_window_from: ev.work_window_from ? toDatetimeLocal(ev.work_window_from) : '',
      work_window_to: ev.work_window_to ? toDatetimeLocal(ev.work_window_to) : '',
      plan_from: toDatetimeLocal(ev.start),
      plan_to: toDatetimeLocal(ev.end),
      status: ev.status,
      is_duty: ev.is_duty,
      work_types: [...(ev.work_types ?? [])],
      price_note: '',
      comment: '',
      special_notes: ev.special_notes
    }
  }
  modalOpen.value = true
}

function onDragStartEvent(ev: CalendarEvent) {
  draggingEventId.value = ev.id
}

async function onDropToDay(targetDay: Date) {
  const id = draggingEventId.value
  draggingEventId.value = null
  if (!id || !auth.hasPerm('crm.application.update')) return
  const ev = events.value.find((e) => e.id === id)
  if (!ev) return
  const oldStart = new Date(ev.start)
  const oldEnd = new Date(ev.end)
  if (Number.isNaN(oldStart.getTime()) || Number.isNaN(oldEnd.getTime())) return
  const durationMs = Math.max(oldEnd.getTime() - oldStart.getTime(), 30 * 60 * 1000)
  const nextStart = new Date(targetDay)
  nextStart.setHours(oldStart.getHours(), oldStart.getMinutes(), 0, 0)
  const nextEnd = new Date(nextStart.getTime() + durationMs)
  try {
    const existing = await requestJSON<Application>(`/api/v1/crm/applications/${ev.id}`)
    await requestJSON(`/api/v1/crm/applications/${ev.id}`, {
      method: 'PUT',
      body: JSON.stringify({
        contractor_id: existing.contractor_id,
        address_id: existing.address_id,
        engineer_id: existing.engineer_id,
        plan_datetime_from: nextStart.toISOString(),
        plan_datetime_to: nextEnd.toISOString(),
        status: existing.status,
        is_duty: existing.is_duty,
        work_types: existing.work_types ?? [],
        executor_ids: existing.executor_ids ?? [],
        traffic_source_id: existing.traffic_source_id ?? null,
        planned_sum: existing.planned_sum ?? 0,
        work_window_from: existing.work_window_from ?? null,
        work_window_to: existing.work_window_to ?? null,
        price_note: existing.price_note || '',
        comment: existing.comment || '',
        special_notes: existing.special_notes || ''
      })
    })
    await loadCalendar()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось перенести заявку'
  }
}

async function toggleCompleteApp(ev: CalendarEvent, complete: boolean) {
  try {
    await requestJSON(`/api/v1/crm/applications/${ev.id}/complete`, {
      method: 'POST',
      body: JSON.stringify({ is_complete: complete, comment: completeComment.value })
    })
    ev.is_complete = complete
    if (complete) ev.status = 2
    await loadCalendar()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось обновить статус завершения'
  }
}

async function deleteApplication(ev: CalendarEvent) {
  try {
    await requestJSON(`/api/v1/crm/applications/${ev.id}`, { method: 'DELETE' })
    drawerOpen.value = false
    selectedEvent.value = null
    await loadCalendar()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось удалить заявку'
  }
}

async function saveApplication() {
  if (!appForm.value.contractor_id || !appForm.value.address_id || !appForm.value.engineer_id) {
    modalError.value = 'Выберите контрагента, адрес и инженера'
    return
  }
  saving.value = true
  modalError.value = ''
  try {
    const payload = {
      contractor_id: appForm.value.contractor_id,
      address_id: appForm.value.address_id,
      engineer_id: appForm.value.engineer_id,
      executor_ids: appForm.value.executor_ids,
      traffic_source_id: appForm.value.traffic_source_id,
      planned_sum: Number(appForm.value.planned_sum || 0),
      work_window_from: appForm.value.work_window_from ? datetimeLocalToRFC3339(appForm.value.work_window_from) : null,
      work_window_to: appForm.value.work_window_to ? datetimeLocalToRFC3339(appForm.value.work_window_to) : null,
      plan_datetime_from: datetimeLocalToRFC3339(appForm.value.plan_from),
      plan_datetime_to: datetimeLocalToRFC3339(appForm.value.plan_to),
      status: appForm.value.status,
      is_duty: appForm.value.is_duty,
      work_types: appForm.value.work_types,
      price_note: appForm.value.price_note,
      comment: appForm.value.comment,
      special_notes: appForm.value.special_notes
    }
    if (editId.value) {
      await requestJSON(`/api/v1/crm/applications/${editId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/applications', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    modalOpen.value = false
    await loadCalendar()
  } catch (err) {
    modalError.value = err instanceof ApiError ? err.message : 'Ошибка сохранения заявки'
  } finally {
    saving.value = false
  }
}

async function markNotesViewed(ev: CalendarEvent) {
  await requestJSON(`/api/v1/crm/applications/${ev.id}/notes-viewed`, { method: 'POST' })
  ev.notes_viewed = true
}

async function rateApp(ev: CalendarEvent, rating: number) {
  await requestJSON(`/api/v1/crm/applications/${ev.id}/rating`, {
    method: 'PATCH',
    body: JSON.stringify({ rating, comment: ratingComment.value })
  })
  ev.rating = rating
}

function createActFromEvent(ev: CalendarEvent) {
  void router.push({
    path: '/crm/applications',
    query: {
      createCertForApp: String(ev.id),
      contractorId: String(ev.contractor_id),
      addressId: String(ev.address_id),
      engineerId: String(ev.engineer_id)
    }
  })
}

async function discussWithAI(ev: CalendarEvent) {
  const conv = await requestJSON<{ id: number }>('/api/v1/chat/conversations', {
    method: 'POST',
    body: JSON.stringify({
      title: `Заявка #${ev.id}: ${ev.contractor_name}`,
      mode: 'agent',
      entity_type: 'application',
      entity_id: ev.id
    })
  })
  await router.push({ path: '/chat', query: { convId: String(conv.id) } })
}

watch([anchorDate, viewMode, selectedEngineerId, selectedSubdivisionId, onlyMyCalendar], () => {
  void loadCalendar()
})

watch(
  () => realtime.calendarRevision,
  () => {
    void loadCalendar()
  }
)

watch(
  () => appForm.value.contractor_id,
  (cid) => {
    if (modalOpen.value && cid) {
      void loadContractorAddresses(cid)
    }
  }
)

onMounted(async () => {
  await loadMeta()
  await loadCalendar()
  if (route.query.createContractorId && auth.hasPerm('crm.application.create')) {
    await openCreateModal({
      contractorId: Number(route.query.createContractorId),
      addressId: route.query.addressId ? Number(route.query.addressId) : undefined
    })
  }
})
</script>

<template>
  <div class="calendar-page">
    <div class="toolbar">
      <NSpace align="center">
        <NButton @click="shiftRange(-1)">← Назад</NButton>
        <NButton @click="resetToday">Сегодня</NButton>
        <NButton @click="shiftRange(1)">Вперёд →</NButton>
        <NRadioGroup v-model:value="viewMode" size="small">
          <NRadioButton value="week">Неделя</NRadioButton>
          <NRadioButton value="day">День</NRadioButton>
          <NRadioButton value="list">Список</NRadioButton>
        </NRadioGroup>
        <NSelect
          v-if="auth.hasPerm('crm.calendar.select_user')"
          style="width: 210px"
          :value="selectedEngineerId ?? 0"
          :options="engineerOptions"
          @update:value="(v) => (selectedEngineerId = Number(v) === 0 ? null : Number(v))"
        />
        <NSelect
          v-if="auth.hasPerm('crm.calendar.select_subdivision') && subdivisions.length > 0"
          style="width: 200px"
          :value="selectedSubdivisionId ?? 0"
          :options="subdivisionOptions"
          @update:value="(v) => (selectedSubdivisionId = Number(v) === 0 ? null : Number(v))"
        />
        <NButton :type="onlyMyCalendar ? 'primary' : 'default'" secondary @click="onlyMyCalendar = !onlyMyCalendar">
          Мой календарь
        </NButton>
        <NButton :type="onlyDuty ? 'warning' : 'default'" secondary @click="onlyDuty = !onlyDuty">
          Дежурные
        </NButton>
        <NButton secondary @click="settingsModalOpen = true">Настройки</NButton>
      </NSpace>
      <NButton
        v-if="auth.hasPerm('crm.application.create')"
        type="primary"
        @click="openCreateModal()"
      >
        + Новая заявка
      </NButton>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">
      {{ errorMsg }}
    </NAlert>

    <NSpin :show="loading">
      <div v-if="viewMode !== 'list'" class="calendar-grid" :class="{ single: viewMode === 'day' }">
        <div
          v-for="day in rangeDays"
          :key="toISODate(day)"
          class="day-column"
          @dragover.prevent
          @drop.prevent="onDropToDay(day)"
        >
          <div class="day-header">
            <span>{{ day.toLocaleDateString('ru-RU', { weekday: 'short', day: '2-digit', month: 'short' }) }}</span>
            <NSpace :size="4" align="center">
              <NButton
                v-if="auth.hasPerm('crm.application.create')"
                size="tiny"
                quaternary
                title="Создать заявку на этот день"
                @click="openCreateModal({ day })"
              >
                +
              </NButton>
              <NTag size="tiny">{{ eventsForDay(day).length }}</NTag>
            </NSpace>
          </div>
          <div class="day-cards">
            <div
              v-for="ev in eventsForDay(day)"
              :key="ev.id"
              class="cal-card"
              :draggable="auth.hasPerm('crm.application.update')"
              :style="{ borderLeftColor: ev.color || '#2563eb' }"
              @dragstart="onDragStartEvent(ev)"
              @click="openDetails(ev)"
            >
              <div class="card-top">
                <strong>{{ formatTimeRange(ev.start, ev.end) }}</strong>
                <NSpace :size="4">
                  <NTag v-if="ev.is_duty" type="warning" size="tiny">Дежурная</NTag>
                  <NTag :type="statusTagType(ev.status)" size="tiny">
                    {{ documentStatusLabel(ev.status) }}
                  </NTag>
                </NSpace>
              </div>
              <div class="card-title">{{ ev.contractor_name }}</div>
              <div class="card-sub">{{ ev.address_text }}</div>
              <div class="card-sub muted">
                Инженер: {{ ev.engineer_name }}
                <template v-if="ev.executor_ids?.length"> (+{{ formatExecutorNames(ev.executor_ids) }})</template>
              </div>
              <div v-if="ev.work_types?.length" class="card-tags">
                <NTag v-for="wt in ev.work_types" :key="wt" size="tiny" :bordered="false">
                  {{ wt }}
                </NTag>
              </div>
              <div v-if="ev.special_notes && !ev.notes_viewed" class="unread-badge">
                <NBadge dot type="error" /> Особые заметки не просмотрены
              </div>
              <div class="card-footer">
                <span v-if="ev.acts_total_amount > 0">Акты: {{ formatMoneyRUB(ev.acts_total_amount) }}</span>
                <span v-if="ev.planned_sum && ev.planned_sum > 0">План: {{ formatMoneyRUB(ev.planned_sum) }}</span>
                <span v-if="ev.analytic_rating || ev.rating">★ {{ ev.analytic_rating || ev.rating }}/5</span>
              </div>
            </div>
            <div v-if="eventsForDay(day).length === 0" class="empty-day">Нет заявок</div>
          </div>
        </div>
      </div>

      <div v-else class="list-view">
        <div
          v-for="ev in filteredEvents"
          :key="ev.id"
          class="list-row"
          :style="{ borderLeftColor: ev.color || '#2563eb' }"
          @click="openDetails(ev)"
        >
          <div>
            <strong>#{{ ev.id }} · {{ formatDateTime(ev.start) }}</strong>
            <div>{{ ev.contractor_name }} — {{ ev.address_text }}</div>
            <div class="muted">Инженер: {{ ev.engineer_name }}</div>
          </div>
          <NSpace align="center">
            <NTag v-if="ev.is_duty" type="warning" size="small">Дежурная</NTag>
            <NTag :type="statusTagType(ev.status)" size="small">
              {{ documentStatusLabel(ev.status) }}
            </NTag>
            <span>{{ formatMoneyRUB(ev.acts_total_amount) }}</span>
          </NSpace>
        </div>
      </div>
    </NSpin>

    <NDrawer v-model:show="drawerOpen" :width="440">
      <NDrawerContent v-if="selectedEvent" :title="`Заявка #${selectedEvent.id}`" closable>
        <NSpace vertical :size="12">
          <div>
            <strong>Контрагент:</strong> {{ selectedEvent.contractor_name }}
            <div><strong>Адрес:</strong> {{ selectedEvent.address_text }}</div>
            <div><strong>Инженер:</strong> {{ selectedEvent.engineer_name }}</div>
            <div><strong>Время:</strong> {{ formatDateTime(selectedEvent.start) }} — {{ formatDateTime(selectedEvent.end) }}</div>
            <div><strong>Статус:</strong> {{ documentStatusLabel(selectedEvent.status) }}</div>
          </div>

          <div v-if="selectedEvent.phones?.length">
            <strong>Телефоны:</strong>
            <div v-for="p in selectedEvent.phones" :key="p">{{ formatPhoneRU(p) }}</div>
          </div>

          <div v-if="selectedEvent.equipments?.length">
            <strong>Оборудование:</strong>
            <div v-for="(eq, idx) in selectedEvent.equipments" :key="idx">
              {{ eq }}
              <span v-if="selectedEvent.serial_numbers?.[idx]" class="muted">
                (S/N {{ selectedEvent.serial_numbers[idx] }})
              </span>
            </div>
          </div>

          <NAlert
            v-if="selectedEvent.special_notes"
            :type="selectedEvent.notes_viewed ? 'info' : 'warning'"
            title="Особые примечания"
          >
            <div>{{ selectedEvent.special_notes }}</div>
            <NButton
              v-if="!selectedEvent.notes_viewed"
              size="tiny"
              type="warning"
              style="margin-top: 8px"
              @click="markNotesViewed(selectedEvent)"
            >
              Отметить прочитанным
            </NButton>
          </NAlert>

          <div>
            <strong>Оценка качества:</strong>
            <NSpace align="center" style="margin-top: 6px">
              <NRate
                :disabled="!auth.hasPerm('crm.application.rating')"
                :value="selectedEvent.rating ?? 0"
                @update:value="(v) => selectedEvent && rateApp(selectedEvent, v)"
              />
            </NSpace>
            <NInput
              v-model:value="ratingComment"
              size="small"
              placeholder="Комментарий к оценке..."
              style="margin-top: 6px"
            />
          </div>

          <NDivider />

          <NSpace wrap>
            <NButton
              v-if="auth.hasPerm('crm.application.update')"
              secondary
              @click="openEditModal(selectedEvent)"
            >
              Редактировать / Перенести
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.application.close')"
              :type="selectedEvent.is_complete ? 'warning' : 'success'"
              secondary
              @click="toggleCompleteApp(selectedEvent, !selectedEvent.is_complete)"
            >
              {{ selectedEvent.is_complete ? 'Снять отметку выполнения' : 'Завершить заявку' }}
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.certificate.create')"
              type="primary"
              @click="createActFromEvent(selectedEvent)"
            >
              Создать акт ТО
            </NButton>
            <NButton
              v-if="auth.hasPerm('chat.use')"
              type="info"
              secondary
              @click="discussWithAI(selectedEvent)"
            >
              Обсудить с ИИ
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.application.delete')"
              type="error"
              ghost
              @click="deleteApplication(selectedEvent)"
            >
              Удалить
            </NButton>
          </NSpace>
        </NSpace>
      </NDrawerContent>
    </NDrawer>

    <NModal v-model:show="settingsModalOpen" preset="card" title="Настройки отображения календаря" style="width: 480px">
      <NSpace vertical>
        <div class="muted">Порядок и видимость инженеров сохраняются в профиле (calendar_settings)</div>
        <div
          v-for="(eng, idx) in orderedEngineers"
          :key="eng.id"
          style="display: flex; align-items: center; justify-content: space-between; padding: 6px 0; border-bottom: 1px solid #f1f5f9"
        >
          <NCheckbox
            :checked="!hiddenEngineerIds.includes(eng.id)"
            @update:checked="(v) => toggleHiddenEngineer(eng.id, v)"
          >
            {{ eng.full_name || eng.username }}
          </NCheckbox>
          <NSpace :size="4">
            <NButton size="tiny" :disabled="idx === 0" @click="moveEngineerOrder(eng.id, -1)">↑</NButton>
            <NButton size="tiny" :disabled="idx === orderedEngineers.length - 1" @click="moveEngineerOrder(eng.id, 1)">↓</NButton>
          </NSpace>
        </div>
      </NSpace>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="settingsModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveCalendarSettings">Сохранить настройки</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="modalOpen"
      preset="card"
      :title="editId ? `Редактировать заявку #${editId}` : 'Новая заявка'"
      style="width: 620px; max-width: 96vw"
    >
      <NAlert v-if="modalError" type="error" style="margin-bottom: 12px">{{ modalError }}</NAlert>
      <NForm label-placement="top">
        <NFormItem label="Контрагент">
          <NSelect v-model:value="appForm.contractor_id" filterable :options="contractorOptions" />
        </NFormItem>
        <NFormItem label="Адрес объекта">
          <NSelect v-model:value="appForm.address_id" filterable :options="addressOptions" />
        </NFormItem>
        <NFormItem label="Инженер">
          <NSelect
            v-model:value="appForm.engineer_id"
            filterable
            :options="engineers.map((e) => ({ label: e.full_name || e.username, value: e.id }))"
          />
        </NFormItem>
        <NFormItem label="Соисполнители">
          <NSelect
            v-model:value="appForm.executor_ids"
            multiple
            filterable
            clearable
            placeholder="Выберите соисполнителей"
            :options="engineers.map((e) => ({ label: e.full_name || e.username, value: e.id }))"
          />
        </NFormItem>
        <div class="two-col">
          <NFormItem label="Начало слота">
            <input v-model="appForm.plan_from" type="datetime-local" class="native-dt" />
          </NFormItem>
          <NFormItem label="Конец слота">
            <input v-model="appForm.plan_to" type="datetime-local" class="native-dt" />
          </NFormItem>
        </div>
        <div class="two-col">
          <NFormItem label="Окно прибытия с">
            <input v-model="appForm.work_window_from" type="datetime-local" class="native-dt" />
          </NFormItem>
          <NFormItem label="Окно прибытия по">
            <input v-model="appForm.work_window_to" type="datetime-local" class="native-dt" />
          </NFormItem>
        </div>
        <div class="two-col">
          <NFormItem label="Статус">
            <NSelect
              v-model:value="appForm.status"
              :options="enums.document_statuses.map((s) => ({ label: s.label, value: s.id }))"
            />
          </NFormItem>
          <NFormItem label="Дежурный слот (разрешить наложение)">
            <NSwitch v-model:value="appForm.is_duty" />
          </NFormItem>
        </div>
        <NFormItem label="Виды работ">
          <NSelect v-model:value="appForm.work_types" multiple :options="workTypeOptions" />
        </NFormItem>
        <NFormItem label="Особые примечания для инженера">
          <NSpace vertical style="width: 100%">
            <NSpace v-if="specialNotePresets.length" :size="4" wrap>
              <NButton
                v-for="preset in specialNotePresets"
                :key="preset.id"
                size="tiny"
                secondary
                @click="
                  appForm.special_notes = appForm.special_notes
                    ? `${appForm.special_notes}; ${preset.name}`
                    : preset.name
                "
              >
                + {{ preset.name }}
              </NButton>
            </NSpace>
            <NInput v-model:value="appForm.special_notes" type="textarea" :rows="2" />
          </NSpace>
        </NFormItem>
        <div class="two-col">
          <NFormItem label="Ориентир по цене">
            <NInput v-model:value="appForm.price_note" placeholder="Например: от 3500 ₽" />
          </NFormItem>
          <NFormItem label="Комментарий диспетчера">
            <NInput v-model:value="appForm.comment" placeholder="Комментарий по выезду" />
          </NFormItem>
        </div>
        <div class="two-col">
          <NFormItem label="Плановая сумма (₽)">
            <NInputNumber v-model:value="appForm.planned_sum" :min="0" :step="500" style="width: 100%" />
          </NFormItem>
          <NFormItem label="Источник трафика">
            <NSelect
              v-model:value="appForm.traffic_source_id"
              filterable
              clearable
              placeholder="Не выбран"
              :options="trafficSources.map((s) => ({ label: s.name, value: s.id }))"
            />
          </NFormItem>
        </div>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="modalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="saving" @click="saveApplication">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.calendar-page {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 10px;
}
.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(170px, 1fr));
  gap: 10px;
  overflow-x: auto;
}
.calendar-grid.single {
  grid-template-columns: 1fr;
}
.day-column {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 10px;
  min-height: 360px;
}
.day-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  margin-bottom: 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid #f1f5f9;
}
.day-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.cal-card {
  border: 1px solid #e2e8f0;
  border-left: 5px solid #2563eb;
  border-radius: 8px;
  padding: 8px;
  background: #f8fafc;
  cursor: pointer;
  transition: transform 0.1s ease, box-shadow 0.1s ease;
}
.cal-card:hover {
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.08);
}
.card-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  margin-bottom: 4px;
}
.card-title {
  font-weight: 600;
  font-size: 13px;
}
.card-sub {
  font-size: 12px;
  color: #334155;
}
.muted {
  color: #64748b;
}
.card-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}
.unread-badge {
  margin-top: 6px;
  font-size: 11px;
  color: #b91c1c;
  display: flex;
  align-items: center;
  gap: 6px;
}
.card-footer {
  display: flex;
  justify-content: space-between;
  margin-top: 6px;
  font-size: 11px;
  color: #475569;
}
.empty-day {
  text-align: center;
  color: #94a3b8;
  font-size: 12px;
  padding: 20px 0;
}
.list-view {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.list-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-left: 5px solid #2563eb;
  border-radius: 8px;
  padding: 12px;
  cursor: pointer;
}
.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.native-dt {
  width: 100%;
  padding: 6px 10px;
  border: 1px solid #d1d5db;
  border-radius: 4px;
}
</style>
