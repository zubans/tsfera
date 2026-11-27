<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAlert,
  NBadge,
  NButton,
  NCard,
  NDataTable,
  NDatePicker,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { downloadFile } from '@/shared/utils/download'
import {
  formatDateRangeParam,
  formatDateTime,
  formatDurationSec,
  formatPhoneRU,
  isRussianPhone
} from '@/shared/utils/format'
import {
  callAppealLabel,
  callDirectionMeta,
  callResultLabel,
  callStatusMeta,
  formatDateParts
} from './crmLabels'
import { useAuthStore } from '@/stores/auth'
import type {
  CallRecord,
  MissCallPoolItem,
  TrafficSource,
  Subdivision,
  SipAccount,
  SipUserSelectOption,
  Contractor,
  CRMEnums
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const activeTab = ref<string>('calls')
const loading = ref(false)
const loadingMissed = ref(false)
const errorMsg = ref('')

const calls = ref<CallRecord[]>([])
const currentCalls = ref<CallRecord[]>([])
const missCalls = ref<MissCallPoolItem[]>([])
const missCallsOnlyUnviewed = ref(true)
const missSearch = ref('')

const phoneFilter = ref('')
const directionFilter = ref<string | null>(null)
const statusFilter = ref<string | null>(null)
const callerTypeFilter = ref<number | null>(null)
const callResultFilter = ref<number | null>(null)
const appealFilter = ref<string | null>(null)
const trafficSourceFilter = ref<number | null>(null)
const subdivisionFilter = ref<number | null>(null)
const dateRange = ref<[number, number] | null>(null)

const trafficSources = ref<TrafficSource[]>([])
const subdivisions = ref<Subdivision[]>([])
const sipAccounts = ref<SipAccount[]>([])
const sipUserSelect = ref<SipUserSelectOption[]>([])
const contractors = ref<Contractor[]>([])
const searchingContractors = ref(false)

const playingCall = ref<CallRecord | null>(null)

const enums = ref<CRMEnums>({
  work_types: [],
  document_statuses: [],
  task_types: [],
  task_statuses: [],
  call_types_of_appeal: [
    { id: 9, key: 'to_contract', label: 'Договор ТО' },
    { id: 10, key: 'service_work', label: 'Сервисные / ремонтные работы' },
    { id: 11, key: 'sale_equipment', label: 'Продажа оборудования' },
    { id: 12, key: 'parts_store', label: 'Магазин запчастей' },
    { id: 13, key: 'specify', label: 'Уточнить информацию' },
    { id: 14, key: 'spam_call', label: 'Спам' },
    { id: 15, key: 'other_appeal', label: 'Прочее' }
  ]
})

const callCardModalOpen = ref(false)
const editCall = ref<CallRecord | null>(null)
const savingCard = ref(false)
const transcribingCallId = ref<number | null>(null)
const callCardForm = ref({
  contractor_id: null as number | null,
  type_of_appeal: '',
  caller_type: 1,
  call_result: 1,
  traffic_source_id: null as number | null,
  comment: '',
  detach_from_others: true
})

const transferModalOpen = ref(false)
const transferForm = ref({ external_call_id: '', target_phone: '' })

// Сопоставляем внутренние ID линий ВАТС с именами сотрудников/линий.
const vatsLineMap = computed(() => {
  const map = new Map<string, string>()
  for (const su of sipUserSelect.value) {
    const key = (su.abonent_id || '').replace(/\D/g, '')
    if (key && su.full_name) map.set(key, su.full_name)
  }
  for (const sip of sipAccounts.value) {
    const key = (sip.abonent_id || '').replace(/\D/g, '')
    if (key && !map.has(key) && sip.name) {
      map.set(key, sip.name.replace(/\s+/g, ' ').trim())
    }
  }
  return map
})

function formatEndpoint(raw?: string | null): { main: string; sub?: string } {
  const value = (raw || '').trim()
  if (!value) return { main: '—' }
  if (isRussianPhone(value)) {
    return { main: formatPhoneRU(value) }
  }
  const digits = value.replace(/\D/g, '')
  const owner = digits ? vatsLineMap.value.get(digits) : undefined
  if (digits.length >= 6) {
    return { main: `ВАТС ${digits}`, sub: owner }
  }
  return { main: value, sub: owner }
}

const directionFilterOptions = [
  { label: 'Входящие', value: 'in,inbound,incoming' },
  { label: 'Исходящие', value: 'out,outbound,outgoing' },
  { label: 'Внутренние', value: 'internal' }
]

const statusFilterOptions = [
  { label: 'Отвечен / Завершён', value: 'completed,ended,answered' },
  { label: 'Пропущен', value: 'missed' },
  { label: 'На линии / Звонит', value: 'new,ringing,connected' },
  { label: 'Не состоялся', value: 'failed' }
]

const callAppealOptions = computed(() =>
  (enums.value.call_types_of_appeal ?? [])
    .filter((o) => !o.legacy || o.key.startsWith('import_'))
    .map((o) => ({ label: o.label, value: o.key }))
)
const allCallAppealOptions = computed(() =>
  (enums.value.call_types_of_appeal ?? []).map((o) => ({ label: o.label, value: o.key }))
)
const callerTypeOptions = computed(() =>
  (enums.value.caller_types ?? []).map((o) => ({ label: o.label, value: o.id }))
)
const callResultOptions = computed(() =>
  (enums.value.call_results ?? []).map((o) => ({ label: o.label, value: o.id }))
)

const hasActiveFilters = computed(
  () =>
    Boolean(phoneFilter.value.trim()) ||
    directionFilter.value !== null ||
    statusFilter.value !== null ||
    callerTypeFilter.value !== null ||
    callResultFilter.value !== null ||
    appealFilter.value !== null ||
    trafficSourceFilter.value !== null ||
    subdivisionFilter.value !== null ||
    dateRange.value !== null
)

const unviewedMissCount = computed(() => missCalls.value.filter((m) => !m.is_viewed).length)

const filteredMissCalls = computed(() => {
  const q = missSearch.value.trim().toLowerCase()
  if (!q) return missCalls.value
  return missCalls.value.filter(
    (m) =>
      (m.caller_phone || '').toLowerCase().includes(q) ||
      (m.callee_phone || '').toLowerCase().includes(q) ||
      (m.contractor_name || '').toLowerCase().includes(q)
  )
})

function hasPlayableRecording(r: CallRecord): boolean {
  const normStatus = (r.status || '').toLowerCase()
  if (normStatus === 'missed' || normStatus === 'failed') return false
  if (r.duration_sec <= 0) return false
  return Boolean(r.ext_tracking_id || r.external_call_id?.startsWith('mango:'))
}

function buildCallsQS(extra?: Record<string, string>): string {
  const params = new URLSearchParams({ limit: '200' })
  if (phoneFilter.value.trim()) params.set('phone', phoneFilter.value.trim())
  if (directionFilter.value) params.set('directions', directionFilter.value)
  if (statusFilter.value) params.set('statuses', statusFilter.value)
  if (callerTypeFilter.value !== null) params.set('caller_type', String(callerTypeFilter.value))
  if (callResultFilter.value !== null) params.set('call_result', String(callResultFilter.value))
  if (appealFilter.value) params.set('type_of_appeal', appealFilter.value)
  if (trafficSourceFilter.value !== null) params.set('traffic_source_id', String(trafficSourceFilter.value))
  if (subdivisionFilter.value !== null) params.set('subdivision_id', String(subdivisionFilter.value))
  const rangeStr = formatDateRangeParam(dateRange.value)
  if (rangeStr) params.set('range', rangeStr)
  if (extra) {
    for (const [k, v] of Object.entries(extra)) params.set(k, v)
  }
  return params.toString()
}

async function loadCalls() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [callRes, curRes] = await Promise.allSettled([
      requestJSON<CallRecord[]>(`/api/v1/crm/calls?${buildCallsQS()}`),
      requestJSON<CallRecord[]>('/api/v1/crm/calls/current')
    ])
    if (callRes.status === 'fulfilled') calls.value = Array.isArray(callRes.value) ? callRes.value : []
    if (curRes.status === 'fulfilled') currentCalls.value = Array.isArray(curRes.value) ? curRes.value : []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить журнал звонков'
  } finally {
    loading.value = false
  }
}

async function loadMissCalls() {
  loadingMissed.value = true
  try {
    const res = await requestJSON<MissCallPoolItem[]>(
      `/api/v1/crm/miss-calls?only_unviewed=${missCallsOnlyUnviewed.value ? 'true' : 'false'}&limit=200`
    )
    missCalls.value = Array.isArray(res) ? res : []
  } catch {
    missCalls.value = []
  } finally {
    loadingMissed.value = false
  }
}

async function loadMeta() {
  const [tsRes, subRes, sipRes, sipSelRes, ctrRes, eRes] = await Promise.allSettled([
    requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources?active=true'),
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
    requestJSON<SipAccount[]>('/api/v1/crm/sip-accounts'),
    requestJSON<SipUserSelectOption[]>('/api/v1/crm/sip-accounts/select-with-user-name'),
    requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100'),
    requestJSON<CRMEnums>('/api/v1/crm/enums')
  ])
  if (tsRes.status === 'fulfilled' && Array.isArray(tsRes.value)) trafficSources.value = tsRes.value
  if (subRes.status === 'fulfilled' && Array.isArray(subRes.value)) subdivisions.value = subRes.value
  if (sipRes.status === 'fulfilled' && Array.isArray(sipRes.value)) sipAccounts.value = sipRes.value
  if (sipSelRes.status === 'fulfilled' && Array.isArray(sipSelRes.value)) sipUserSelect.value = sipSelRes.value
  if (ctrRes.status === 'fulfilled') contractors.value = ctrRes.value?.items ?? []
  if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
}

async function searchContractorsRemote(query: string) {
  searchingContractors.value = true
  try {
    const res = await requestJSON<{ items: Contractor[] }>(
      `/api/v1/crm/contractors?q=${encodeURIComponent(query.trim())}&limit=50`
    )
    contractors.value = res?.items ?? []
  } finally {
    searchingContractors.value = false
  }
}

function resetFilters() {
  phoneFilter.value = ''
  directionFilter.value = null
  statusFilter.value = null
  callerTypeFilter.value = null
  callResultFilter.value = null
  appealFilter.value = null
  trafficSourceFilter.value = null
  subdivisionFilter.value = null
  dateRange.value = null
  void loadCalls()
}

async function markMissCallViewed(callId: number) {
  try {
    await requestJSON(`/api/v1/crm/miss-calls/${callId}/viewed`, { method: 'POST' })
    await loadMissCalls()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось отметить звонок'
  }
}

async function openMissedCallCard(callId: number) {
  try {
    const call = await requestJSON<CallRecord>(`/api/v1/crm/calls/${callId}`)
    if (call) openCallCard(call)
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось открыть карточку звонка'
  }
}

function openCallCard(call: CallRecord) {
  editCall.value = call
  callCardForm.value = {
    contractor_id: call.contractor_id ?? null,
    type_of_appeal: call.type_of_appeal || '',
    caller_type: call.caller_type ?? 1,
    call_result: call.call_result ?? 1,
    traffic_source_id: call.traffic_source_id ?? null,
    comment: call.comment || '',
    detach_from_others: true
  }
  callCardModalOpen.value = true
}

async function saveCallCard() {
  if (!editCall.value) return
  savingCard.value = true
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/calls/${editCall.value.id}`, {
      method: 'PUT',
      body: JSON.stringify(callCardForm.value)
    })
    callCardModalOpen.value = false
    await loadCalls()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения карточки звонка'
  } finally {
    savingCard.value = false
  }
}

async function sendTransfer() {
  try {
    await requestJSON('/api/v1/crm/calls/transfer', {
      method: 'POST',
      body: JSON.stringify(transferForm.value)
    })
    transferModalOpen.value = false
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка перевода звонка'
  }
}

async function transcribeCallRecord(call: CallRecord) {
  transcribingCallId.value = call.id
  errorMsg.value = ''
  try {
    const updated = await requestJSON<CallRecord>(`/api/v1/crm/calls/${call.id}/transcribe`, {
      method: 'POST'
    })
    if (editCall.value?.id === call.id) editCall.value = updated
    if (playingCall.value?.id === call.id) playingCall.value = updated
    await loadCalls()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка расшифровки записи звонка'
  } finally {
    transcribingCallId.value = null
  }
}

async function runExport(format: 'xlsx' | 'csv') {
  try {
    await downloadFile(
      `/api/v1/crm/calls/export?${buildCallsQS({ format })}`,
      `calls-${new Date().toISOString().slice(0, 10)}.${format}`
    )
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : 'Не удалось выгрузить файл'
  }
}

const callColumns = computed<DataTableColumns<CallRecord>>(() => [
  {
    title: 'Дата / время',
    key: 'started_at',
    width: 112,
    render: (r) => {
      const parts = formatDateParts(r.started_at)
      if (!parts) return '—'
      return h('div', { class: 'dt-cell' }, [
        h('span', parts.date),
        h('span', { class: 'dt-time' }, parts.time)
      ])
    }
  },
  {
    title: 'Направление / Статус',
    key: 'status',
    width: 150,
    render: (r) => {
      const dir = callDirectionMeta(r.direction)
      const st = callStatusMeta(r.status)
      return h('div', { class: 'stack-cell' }, [
        h(NTag, { size: 'tiny', type: dir.type, bordered: false }, { default: () => dir.short }),
        h(NTag, { size: 'tiny', type: st.type, bordered: false }, { default: () => st.label })
      ])
    }
  },
  {
    title: 'Абонент → Линия',
    key: 'caller_phone',
    width: 215,
    render: (r) => {
      const from = formatEndpoint(r.caller_phone)
      const to = formatEndpoint(r.callee_phone)
      return h('div', { class: 'phone-cell' }, [
        h('div', { class: 'phone-main' }, from.main),
        h(
          'div',
          { class: 'phone-sub' },
          to.sub ? `→ ${to.main} (${to.sub})` : `→ ${to.main}`
        )
      ])
    }
  },
  {
    title: 'Контрагент',
    key: 'contractor_name',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (r) => r.contractor_name || h('span', { class: 'cell-muted' }, 'Не определён')
  },
  {
    title: 'Обращение и итог',
    key: 'type_of_appeal',
    minWidth: 205,
    render: (r) => {
      const appeal = r.type_of_appeal ? callAppealLabel(r.type_of_appeal, enums.value) : ''
      const res = callResultLabel(r.call_result, enums.value)
      const src = r.traffic_source_name || ''
      const subParts = [res, src].filter(Boolean).join(' · ')
      if (!appeal && !subParts) return h('span', { class: 'cell-muted' }, '—')
      return h('div', { class: 'stack-cell' }, [
        appeal ? h('span', { class: 'cell-strong' }, appeal) : null,
        subParts ? h('span', { class: 'cell-muted-sm' }, subParts) : null
      ])
    }
  },
  {
    title: 'Длит.',
    key: 'duration_sec',
    width: 78,
    align: 'right',
    render: (r) => h('span', { class: 'cell-num' }, formatDurationSec(r.duration_sec))
  },
  {
    title: 'Запись',
    key: 'recording',
    width: 132,
    render: (r) => {
      if (!hasPlayableRecording(r)) {
        return h('span', { class: 'cell-muted' }, '—')
      }
      const isActive = playingCall.value?.id === r.id
      return h(
        NButton,
        {
          size: 'tiny',
          type: isActive ? 'primary' : 'default',
          secondary: !isActive,
          onClick: () => {
            playingCall.value = isActive ? null : r
          }
        },
        { default: () => (isActive ? '■ Скрыть' : '▶ Слушать') }
      )
    }
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 180,
    fixed: 'right',
    render: (r) =>
      auth.hasPerm('crm.call.update')
        ? h(NSpace, { size: 6, wrap: false }, () => [
            h(
              NButton,
              { size: 'tiny', secondary: true, onClick: () => openCallCard(r) },
              { default: () => 'Карточка' }
            ),
            hasPlayableRecording(r)
              ? h(
                  NButton,
                  {
                    size: 'tiny',
                    type: r.transcript_status === 'done' ? 'success' : 'info',
                    secondary: true,
                    loading: transcribingCallId.value === r.id,
                    onClick: () => void transcribeCallRecord(r)
                  },
                  { default: () => (r.transcript_status === 'done' ? 'Текст ✓' : 'STT') }
                )
              : null,
            ['new', 'ringing', 'connected'].includes((r.status || '').toLowerCase())
              ? h(
                  NButton,
                  {
                    size: 'tiny',
                    onClick: () => {
                      transferForm.value = { external_call_id: r.external_call_id, target_phone: '' }
                      transferModalOpen.value = true
                    }
                  },
                  { default: () => 'Перевод' }
                )
              : null
          ])
        : '—'
  }
])

const missColumns = computed<DataTableColumns<MissCallPoolItem>>(() => [
  {
    title: 'Дата / время',
    key: 'started_at',
    width: 130,
    render: (r) => {
      const parts = formatDateParts(r.started_at)
      if (!parts) return '—'
      return h('div', { class: 'dt-cell' }, [
        h('span', parts.date),
        h('span', { class: 'dt-time' }, parts.time)
      ])
    }
  },
  {
    title: 'Кто звонил',
    key: 'caller_phone',
    width: 185,
    render: (r) => h('span', { class: 'phone-main' }, formatEndpoint(r.caller_phone).main)
  },
  {
    title: 'Линия',
    key: 'callee_phone',
    width: 220,
    render: (r) => {
      const ep = formatEndpoint(r.callee_phone)
      return h('div', { class: 'stack-cell' }, [
        h('span', ep.main),
        ep.sub ? h('span', { class: 'cell-muted-sm' }, ep.sub) : null
      ])
    }
  },
  {
    title: 'Контрагент',
    key: 'contractor_name',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (r) => r.contractor_name || h('span', { class: 'cell-muted' }, 'Неизвестный номер')
  },
  {
    title: 'Статус',
    key: 'is_viewed',
    width: 120,
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: r.is_viewed ? 'success' : 'warning', bordered: false },
        { default: () => (r.is_viewed ? 'Обработан' : 'Новый') }
      )
  },
  {
    title: 'Действия',
    key: 'act',
    width: 210,
    fixed: 'right',
    render: (r) =>
      h(NSpace, { size: 6, wrap: false }, () => [
        auth.hasPerm('crm.call.update')
          ? h(
              NButton,
              { size: 'tiny', secondary: true, onClick: () => void openMissedCallCard(r.call_id) },
              { default: () => 'Карточка' }
            )
          : null,
        !r.is_viewed && auth.hasPerm('crm.call.update')
          ? h(
              NButton,
              {
                size: 'tiny',
                type: 'primary',
                secondary: true,
                onClick: () => void markMissCallViewed(r.call_id)
              },
              { default: () => 'Просмотрено' }
            )
          : null
      ])
  }
])

onMounted(async () => {
  if (route.query.tab === 'missed') {
    activeTab.value = 'missed'
  }
  await Promise.all([loadMeta(), loadCalls(), loadMissCalls()])
})
</script>

<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">Звонки</h2>
        <div class="page-meta">
          Журнал телефонии и необработанные пропущенные вызовы
        </div>
      </div>
      <NSpace :size="8" wrap>
        <NButton
          v-if="auth.hasPerm('crm.sip.view') || auth.hasPerm('crm.sip.manage') || auth.hasPerm('crm.subdivision.view')"
          size="small"
          tertiary
          @click="router.push({ path: '/crm/settings', query: { tab: 'sip' } })"
        >
          SIP и подразделения
        </NButton>
        <NButton v-if="auth.hasPerm('crm.call.export')" size="small" secondary @click="runExport('xlsx')">
          Экспорт XLSX
        </NButton>
        <NButton v-if="auth.hasPerm('crm.call.export')" size="small" secondary @click="runExport('csv')">
          Экспорт CSV
        </NButton>
      </NSpace>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NAlert v-if="currentCalls.length" type="info">
      Активных звонков в линии: {{ currentCalls.length }} ({{ currentCalls.map((c) => formatEndpoint(c.caller_phone).main).join(', ') }})
    </NAlert>

    <NCard v-if="playingCall" size="small" class="player-bar">
      <div class="player-bar-inner">
        <div class="player-meta">
          <strong>Запись звонка: {{ formatEndpoint(playingCall.caller_phone).main }}</strong>
          <span class="cell-muted-sm">
            {{ formatDateTime(playingCall.started_at) }} · {{ formatDurationSec(playingCall.duration_sec) }}
            <template v-if="playingCall.contractor_name"> · {{ playingCall.contractor_name }}</template>
          </span>
        </div>
        <audio
          :key="playingCall.id"
          controls
          autoplay
          :src="`/api/v1/crm/calls/${playingCall.id}/recording`"
          class="shared-audio"
        />
        <NSpace :size="6">
          <NButton size="small" secondary @click="openCallCard(playingCall)">Карточка</NButton>
          <NButton size="small" tertiary @click="playingCall = null">Закрыть</NButton>
        </NSpace>
      </div>
      <div v-if="playingCall.transcript_text" class="player-transcript">
        <b>Расшифровка:</b> {{ playingCall.transcript_text }}
      </div>
    </NCard>

    <NTabs v-model:value="activeTab" type="line">
      <NTabPane name="calls" :tab="`Журнал звонков (${calls.length})`">
        <NCard size="small" style="margin-bottom: 12px">
          <div class="filter-row">
            <NInput
              v-model:value="phoneFilter"
              clearable
              placeholder="Поиск по номеру телефона..."
              style="width: 210px"
              @keyup.enter="loadCalls"
              @clear="loadCalls"
            />
            <NDatePicker
              v-model:value="dateRange"
              type="daterange"
              clearable
              format="dd.MM.yyyy"
              start-placeholder="Период с"
              end-placeholder="по"
              style="width: 250px"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="directionFilter"
              clearable
              placeholder="Направление"
              style="width: 145px"
              :options="directionFilterOptions"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="statusFilter"
              clearable
              placeholder="Статус"
              style="width: 175px"
              :options="statusFilterOptions"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="appealFilter"
              clearable
              filterable
              placeholder="Обращение"
              style="width: 185px"
              :options="allCallAppealOptions"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="callResultFilter"
              clearable
              filterable
              placeholder="Итог звонка"
              style="width: 170px"
              :options="callResultOptions"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="callerTypeFilter"
              clearable
              placeholder="Тип абонента"
              style="width: 150px"
              :options="callerTypeOptions"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="trafficSourceFilter"
              clearable
              filterable
              placeholder="Источник"
              style="width: 165px"
              :options="trafficSources.map((s) => ({ label: s.name, value: s.id }))"
              @update:value="loadCalls"
            />
            <NSelect
              v-model:value="subdivisionFilter"
              clearable
              placeholder="Филиал"
              style="width: 150px"
              :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
              @update:value="loadCalls"
            />
            <NButton type="primary" secondary @click="loadCalls">Применить</NButton>
            <NButton v-if="hasActiveFilters" tertiary @click="resetFilters">Сбросить</NButton>
          </div>
        </NCard>

        <NCard size="small" :content-style="{ padding: 0 }">
          <NDataTable
            :columns="callColumns"
            :data="calls"
            :loading="loading"
            :row-key="(r: CallRecord) => r.id"
            :scroll-x="1220"
            :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
            size="small"
          />
        </NCard>
      </NTabPane>

      <NTabPane name="missed">
        <template #tab>
          <NSpace align="center" :size="6">
            <span>Пропущенные</span>
            <NBadge v-if="unviewedMissCount > 0" :value="unviewedMissCount" :max="99" type="warning" />
          </NSpace>
        </template>

        <NCard size="small" style="margin-bottom: 12px">
          <div class="filter-row">
            <NInput
              v-model:value="missSearch"
              clearable
              placeholder="Поиск по номеру или контрагенту..."
              style="min-width: 240px; flex: 1 1 260px"
            />
            <NSpace align="center" :size="8">
              <NSwitch v-model:value="missCallsOnlyUnviewed" @update:value="loadMissCalls" />
              <span class="switch-label">Только необработанные</span>
            </NSpace>
          </div>
        </NCard>

        <NCard size="small" :content-style="{ padding: 0 }">
          <NDataTable
            :columns="missColumns"
            :data="filteredMissCalls"
            :loading="loadingMissed"
            :row-key="(r: MissCallPoolItem) => r.id"
            :scroll-x="1050"
            :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
            size="small"
          />
        </NCard>
      </NTabPane>
    </NTabs>

    <NModal v-model:show="callCardModalOpen" preset="card" title="Карточка звонка" style="width: 560px; max-width: 96vw">
      <div v-if="editCall" class="call-modal-meta">
        <span><b>{{ formatEndpoint(editCall.caller_phone).main }}</b> → {{ formatEndpoint(editCall.callee_phone).main }}</span>
        <span class="cell-muted-sm">{{ formatDateTime(editCall.started_at) }} · {{ formatDurationSec(editCall.duration_sec) }}</span>
      </div>
      <NForm label-placement="top" size="small">
        <NFormItem label="Контрагент">
          <NSelect
            v-model:value="callCardForm.contractor_id"
            filterable
            remote
            clearable
            :loading="searchingContractors"
            placeholder="Выберите или найдите контрагента"
            :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
            @search="searchContractorsRemote"
          />
        </NFormItem>
        <NFormItem label="Отвязать номер от других контрагентов">
          <NSwitch v-model:value="callCardForm.detach_from_others" />
        </NFormItem>
        <div class="form-grid-2">
          <NFormItem label="Тип абонента">
            <NSelect v-model:value="callCardForm.caller_type" :options="callerTypeOptions" />
          </NFormItem>
          <NFormItem label="Итог звонка">
            <NSelect v-model:value="callCardForm.call_result" :options="callResultOptions" />
          </NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Тип обращения">
            <NSelect v-model:value="callCardForm.type_of_appeal" clearable :options="callAppealOptions" />
          </NFormItem>
          <NFormItem label="Источник трафика">
            <NSelect
              v-model:value="callCardForm.traffic_source_id"
              filterable
              clearable
              :options="trafficSources.map((s) => ({ label: s.name, value: s.id }))"
            />
          </NFormItem>
        </div>
        <NFormItem label="Комментарий">
          <NInput v-model:value="callCardForm.comment" type="textarea" :rows="2" />
        </NFormItem>
        <NFormItem v-if="editCall && hasPlayableRecording(editCall)" label="Расшифровка звонка (STT)">
          <NSpace vertical style="width: 100%">
            <NSpace align="center">
              <NButton
                size="small"
                type="info"
                secondary
                :loading="transcribingCallId === editCall.id"
                @click="transcribeCallRecord(editCall)"
              >
                {{ editCall.transcript_status === 'done' ? 'Расшифровать заново' : 'Расшифровать запись' }}
              </NButton>
              <NTag v-if="editCall.transcript_status" size="small" :type="editCall.transcript_status === 'done' ? 'success' : 'error'">
                {{ editCall.transcript_status === 'done' ? 'Готово' : 'Ошибка' }}
              </NTag>
            </NSpace>
            <NAlert v-if="editCall.transcript_error" type="error">{{ editCall.transcript_error }}</NAlert>
            <NInput
              v-if="editCall.transcript_text"
              :value="editCall.transcript_text"
              type="textarea"
              :rows="4"
              readonly
            />
          </NSpace>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="callCardModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingCard" @click="saveCallCard">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="transferModalOpen" preset="card" title="Перевести звонок" style="width: 380px; max-width: 96vw">
      <NForm label-placement="top" size="small">
        <NFormItem label="Целевой номер">
          <NInput v-model:value="transferForm.target_phone" placeholder="+79991234567" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="transferModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="sendTransfer">Перевести</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.crm-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
}
.page-meta {
  font-size: 13px;
  color: #64748b;
  margin-top: 2px;
}
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.switch-label {
  font-size: 13px;
  color: #334155;
}
.player-bar {
  background: #f0f9ff;
  border-color: #bae6fd;
}
.player-bar-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.player-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.shared-audio {
  height: 34px;
  min-width: 260px;
  flex: 1 1 280px;
  max-width: 440px;
}
.player-transcript {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #bae6fd;
  font-size: 13px;
  color: #1e293b;
}
.dt-cell {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.dt-time {
  font-size: 12px;
  color: #64748b;
}
.stack-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: flex-start;
}
.phone-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.phone-main {
  font-weight: 600;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.phone-sub {
  font-size: 12px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 205px;
}
.cell-strong {
  font-weight: 500;
  color: #0f172a;
}
.cell-muted {
  color: #94a3b8;
}
.cell-muted-sm {
  font-size: 12px;
  color: #64748b;
}
.cell-num {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.call-modal-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 10px;
  margin-bottom: 12px;
  background: #f8fafc;
  border-radius: 6px;
  font-size: 13px;
}
.form-grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 640px) {
  .form-grid-2 {
    grid-template-columns: 1fr;
  }
}
</style>
