<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRoute } from 'vue-router'
import {
  NTabs,
  NTabPane,
  NDataTable,
  NButton,
  NSpace,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSwitch,
  NCheckbox,
  NTag,
  NAlert,
  NCard,
  NDivider,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, uploadCRMFile, ApiError } from '@/shared/api/http'
import { downloadFile, openPrintTab } from '@/shared/utils/download'
import ReportsPanel from './ReportsPanel.vue'
import {
  formatDateOnly,
  formatDateTime,
  formatMoneyRUB,
  formatPhoneRU,
  documentStatusLabel,
  taskStatusLabel,
  toDateInput,
  dateInputToRFC3339,
  toDatetimeLocal,
  datetimeLocalToRFC3339
} from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type {
  MaintenanceContract,
  ContractEquipment,
  CRMTask,
  TaskPoolSetting,
  CallRecord,
  MissCallPoolItem,
  TrafficSource,
  Subdivision,
  SubdivisionPhone,
  SubdivisionPaymentOption,
  SipAccount,
  SipUserSelectOption,
  Contractor,
  ContractorAddress,
  Equipment,
  RefItem,
  UserBrief,
  CRMEnums
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()
const activeTab = ref<string>('contracts')
const loading = ref(false)
const errorMsg = ref('')
const jobSummary = ref('')

const contracts = ref<MaintenanceContract[]>([])
const contractOnlyProlongation = ref(false)
const contractRange = ref('')

const tasks = ref<CRMTask[]>([])
const taskStatusFilter = ref<number | null>(null)
const taskTypeFilter = ref<string | null>(null)
const onlyUrgentTasks = ref(false)

const poolSettings = ref<TaskPoolSetting[]>([])
const calls = ref<CallRecord[]>([])
const currentCalls = ref<CallRecord[]>([])
const missCalls = ref<MissCallPoolItem[]>([])
const missCallsOnlyUnviewed = ref(true)
const trafficSources = ref<TrafficSource[]>([])

const callDirectionFilter = ref<string | null>(null)
const callStatusFilter = ref<string | null>(null)
const callCallerTypeFilter = ref<number | null>(null)
const callResultFilter = ref<number | null>(null)
const callAppealFilter = ref<string | null>(null)
const callTrafficSourceFilter = ref<number | null>(null)
const callSubdivisionFilter = ref<number | null>(null)
const callRangeFilter = ref('')

const subdivisions = ref<Subdivision[]>([])
const sipAccounts = ref<SipAccount[]>([])
const sipUserSelect = ref<SipUserSelectOption[]>([])
const contractors = ref<Contractor[]>([])
const equipmentsCatalog = ref<Equipment[]>([])
const workers = ref<UserBrief[]>([])
const modalAddresses = ref<ContractorAddress[]>([])
const activeRefKind = ref<'manufacturer' | 'equipment_type' | 'work_type' | 'special_note'>('manufacturer')
const refItems = ref<RefItem[]>([])
const newRefName = ref('')
const enums = ref<CRMEnums>({
  work_types: [],
  document_statuses: [
    { id: 1, label: 'Активна' },
    { id: 2, label: 'Проведена' },
    { id: 3, label: 'Провалена' },
    { id: 4, label: 'Отменена' }
  ],
  task_types: [
    { id: 'call', label: 'Звонок' },
    { id: 'quarterly_call', label: 'Ежеквартальный обзвон' },
    { id: 'prolongation', label: 'Пролонгация договора' },
    { id: 'no_answer', label: 'Недозвон' },
    { id: 'service', label: 'ТО / Сервис' },
    { id: 'other', label: 'Прочее' }
  ],
  task_statuses: [
    { id: 1, label: 'Новая' },
    { id: 2, label: 'В работе' },
    { id: 3, label: 'Выполнена' },
    { id: 4, label: 'Недозвон' },
    { id: 5, label: 'Провалена' },
    { id: 6, label: 'Отменена' }
  ],
  contract_categories: [
    { id: 1, key: 'standard', label: 'Стандарт' },
    { id: 2, key: 'plus', label: 'Плюс' },
    { id: 3, key: 'all_inclusive', label: 'Всё включено' },
    { id: 4, key: 'economy', label: 'Эконом' }
  ],
  task_results: [
    { id: 1, key: 'answered', label: 'Дозвонились / Выполнено' },
    { id: 2, key: 'no_answer', label: 'Недозвон' },
    { id: 3, key: 'application_created', label: 'Создана заявка' },
    { id: 4, key: 'refused', label: 'Отказ' }
  ],
  call_types_of_appeal: [
    { id: 1, key: 'repair_application', label: 'Заявка на ремонт/ТО' },
    { id: 2, key: 'consultation', label: 'Консультация' },
    { id: 3, key: 'callback', label: 'Повторный звонок' },
    { id: 4, key: 'spam', label: 'Спам / ошибочный номер' }
  ]
})

const contractModalOpen = ref(false)
const editContractId = ref<number | null>(null)
const contractForm = ref({
  contractor_id: null as number | null,
  address_id: null as number | null,
  application_id: null as number | null,
  number: '',
  category: 'standard',
  price: 0,
  signed_at: toDateInput(new Date().toISOString()),
  valid_from: toDateInput(new Date().toISOString()),
  valid_to: toDateInput(new Date(Date.now() + 365 * 86400000).toISOString()),
  status: 1,
  note: '',
  equipments: [] as ContractEquipment[]
})

const renewModalOpen = ref(false)
const renewContractId = ref<number | null>(null)
const renewForm = ref({
  number: '',
  category: 'standard',
  price: 0,
  valid_from: '',
  valid_to: '',
  note: ''
})

const taskModalOpen = ref(false)
const editTaskId = ref<number | null>(null)
const taskForm = ref({
  contractor_id: null as number | null,
  task_type: 'call',
  title: '',
  description: '',
  status: 1,
  priority: 1,
  assigned_user_id: null as number | null,
  plan_datetime_to: toDatetimeLocal(new Date(Date.now() + 3600000).toISOString()),
  is_urgent: false,
  note: ''
})

const executeModalOpen = ref(false)
const executeTaskId = ref<number | null>(null)
const executeForm = ref({
  task_result: 'answered',
  note: '',
  new_plan_datetime_to: toDatetimeLocal(new Date(Date.now() + 4 * 3600000).toISOString())
})

const callCardModalOpen = ref(false)
const editCall = ref<CallRecord | null>(null)
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
const subModalOpen = ref(false)
const editSubId = ref<number | null>(null)
const uploadingSubQrIdx = ref<number | null>(null)
const subForm = ref<{
  name: string
  address: string
  supervisor_user_id: number | null
  phones: SubdivisionPhone[]
  payment_options: SubdivisionPaymentOption[]
}>({
  name: '',
  address: '',
  supervisor_user_id: null,
  phones: [{ phone: '', is_main: true, is_duty: false }],
  payment_options: []
})
const sipModalOpen = ref(false)
const sipForm = ref({
  id: 0,
  name: '',
  domain: '',
  login: '',
  password: '',
  abonent_id: '',
  protocol: 'wss',
  server: '',
  port: 8089,
  subdivision_id: null as number | null,
  user_id: null as number | null,
  active: true
})

const poolModalOpen = ref(false)
const poolForm = ref<TaskPoolSetting>({
  id: 0,
  name: '',
  task_types: ['call', 'quarterly_call'],
  priority: 10,
  time_from: '08:00',
  time_to: '20:00',
  is_online: true,
  with_subdivision: false,
  for_dispatcher: true,
  for_engineer: false,
  tasks_max_count: 5,
  transferred_tasks_max_count: 3,
  active: true
})

const contractCategoryOptions = computed(() =>
  (enums.value.contract_categories ?? []).map((c) => ({ label: c.label, value: c.key }))
)
const taskResultOptions = computed(() =>
  (enums.value.task_results ?? []).map((r) => ({ label: r.label, value: r.key }))
)
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
const callDirectionOptions = computed(() =>
  (enums.value.call_directions ?? [
    { id: 1, key: 'incoming', label: 'Входящий' },
    { id: 2, key: 'outgoing', label: 'Исходящий' }
  ]).map((o) => ({ label: o.label, value: o.key }))
)
const callStatusOptions = computed(() =>
  (enums.value.call_statuses ?? [
    { id: 1, key: 'ringing', label: 'Звонит' },
    { id: 2, key: 'answered', label: 'Отвечен' },
    { id: 3, key: 'missed', label: 'Пропущен' },
    { id: 4, key: 'ended', label: 'Завершён' }
  ]).map((o) => ({ label: o.label, value: o.key }))
)
const paymentOptionsSelect = computed(() =>
  (enums.value.payment_options ?? [
    { id: 1, key: 'cash', label: 'Наличные' },
    { id: 2, key: 'card', label: 'Терминал' },
    { id: 3, key: 'bank_transfer', label: 'Безналичный расчёт' },
    { id: 4, key: 'online', label: 'Онлайн / СБП' }
  ]).map((p) => ({ label: p.label, value: p.id, key: p.key }))
)
function workerNameById(id?: number | null): string {
  if (!id) return '—'
  const w = workers.value.find((u) => u.id === id)
  return w ? w.full_name || w.username : `#${id}`
}
function buildContractsQS(extra?: Record<string, string>): string {
  const params = new URLSearchParams({ limit: '100' })
  if (contractOnlyProlongation.value) params.set('only_needing_prolongation', 'true')
  if (contractRange.value.trim()) params.set('range', contractRange.value.trim())
  if (extra) {
    for (const [k, v] of Object.entries(extra)) params.set(k, v)
  }
  return params.toString()
}
function buildTasksQS(): string {
  const params = new URLSearchParams({ limit: '100' })
  if (onlyUrgentTasks.value) params.set('urgent', 'true')
  if (taskStatusFilter.value !== null) params.set('status', String(taskStatusFilter.value))
  if (taskTypeFilter.value) params.set('task_type', taskTypeFilter.value)
  return params.toString()
}
function buildCallsQS(extra?: Record<string, string>): string {
  const params = new URLSearchParams({ limit: '100' })
  if (callDirectionFilter.value) params.set('direction', callDirectionFilter.value)
  if (callStatusFilter.value) params.set('status', callStatusFilter.value)
  if (callCallerTypeFilter.value !== null) params.set('caller_type', String(callCallerTypeFilter.value))
  if (callResultFilter.value !== null) params.set('call_result', String(callResultFilter.value))
  if (callAppealFilter.value) params.set('type_of_appeal', callAppealFilter.value)
  if (callTrafficSourceFilter.value !== null) {
    params.set('traffic_source_id', String(callTrafficSourceFilter.value))
  }
  if (callSubdivisionFilter.value !== null) {
    params.set('subdivision_id', String(callSubdivisionFilter.value))
  }
  if (callRangeFilter.value.trim()) params.set('range', callRangeFilter.value.trim())
  if (extra) {
    for (const [k, v] of Object.entries(extra)) params.set(k, v)
  }
  return params.toString()
}
function contractCategoryLabel(key?: string): string {
  return enums.value.contract_categories?.find((c) => c.key === key)?.label || key || '—'
}

async function loadRefs() {
  try {
    refItems.value = (await requestJSON<RefItem[]>(`/api/v1/crm/refs/${activeRefKind.value}`)) ?? []
  } catch {
    refItems.value = []
  }
}

async function createRefItem() {
  if (!newRefName.value.trim()) return
  await requestJSON(`/api/v1/crm/refs/${activeRefKind.value}`, {
    method: 'POST',
    body: JSON.stringify({ name: newRefName.value.trim() })
  })
  newRefName.value = ''
  await loadRefs()
}

async function deleteRefItem(id: number) {
  await requestJSON(`/api/v1/crm/refs/${activeRefKind.value}/${id}`, { method: 'DELETE' })
  await loadRefs()
}

async function loadMissCalls() {
  try {
    const res = await requestJSON<MissCallPoolItem[]>(
      `/api/v1/crm/miss-calls?only_unviewed=${missCallsOnlyUnviewed.value ? 'true' : 'false'}`
    )
    missCalls.value = Array.isArray(res) ? res : []
  } catch {
    missCalls.value = []
  }
}

async function markMissCallViewedInTab(callId: number) {
  await requestJSON(`/api/v1/crm/miss-calls/${callId}/viewed`, { method: 'POST' })
  await loadMissCalls()
}

async function loadAll() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [
      cRes,
      tRes,
      pRes,
      callRes,
      curCallsRes,
      missRes,
      subRes,
      sipRes,
      sipSelRes,
      tsRes,
      ctrRes,
      eqRes,
      wRes,
      eRes
    ] = await Promise.allSettled([
      requestJSON<MaintenanceContract[]>(`/api/v1/crm/contracts?${buildContractsQS()}`),
      requestJSON<CRMTask[]>(`/api/v1/crm/tasks?${buildTasksQS()}`),
      requestJSON<TaskPoolSetting[]>('/api/v1/crm/pool-settings'),
      requestJSON<CallRecord[]>(`/api/v1/crm/calls?${buildCallsQS()}`),
      requestJSON<CallRecord[]>('/api/v1/crm/calls/current'),
      requestJSON<MissCallPoolItem[]>(
        `/api/v1/crm/miss-calls?only_unviewed=${missCallsOnlyUnviewed.value ? 'true' : 'false'}`
      ),
      requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
      requestJSON<SipAccount[]>('/api/v1/crm/sip-accounts'),
      requestJSON<SipUserSelectOption[]>('/api/v1/crm/sip-accounts/select-with-user-name'),
      requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources?active=true'),
      requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100'),
      requestJSON<Equipment[]>('/api/v1/crm/equipments?limit=100'),
      requestJSON<UserBrief[]>('/api/v1/crm/workers'),
      requestJSON<CRMEnums>('/api/v1/crm/enums')
    ])
    if (cRes.status === 'fulfilled') contracts.value = cRes.value ?? []
    if (tRes.status === 'fulfilled') tasks.value = tRes.value ?? []
    if (pRes.status === 'fulfilled') poolSettings.value = pRes.value ?? []
    if (callRes.status === 'fulfilled') calls.value = callRes.value ?? []
    if (curCallsRes.status === 'fulfilled') currentCalls.value = curCallsRes.value ?? []
    if (missRes.status === 'fulfilled' && Array.isArray(missRes.value)) missCalls.value = missRes.value
    if (subRes.status === 'fulfilled') subdivisions.value = subRes.value ?? []
    if (sipRes.status === 'fulfilled') sipAccounts.value = sipRes.value ?? []
    if (sipSelRes.status === 'fulfilled' && Array.isArray(sipSelRes.value)) sipUserSelect.value = sipSelRes.value
    if (tsRes.status === 'fulfilled' && Array.isArray(tsRes.value)) trafficSources.value = tsRes.value
    if (ctrRes.status === 'fulfilled') contractors.value = ctrRes.value?.items ?? []
    if (eqRes.status === 'fulfilled') equipmentsCatalog.value = eqRes.value ?? []
    if (wRes.status === 'fulfilled') workers.value = wRes.value ?? []
    if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
    await loadRefs()
  } finally {
    loading.value = false
  }
}

async function loadContractorAddresses(contractorId: number | null) {
  if (!contractorId) {
    modalAddresses.value = []
    return
  }
  modalAddresses.value =
    (await requestJSON<ContractorAddress[]>(
      `/api/v1/crm/addresses?contractor_id=${contractorId}&limit=50`
    )) ?? []
}

async function openContractModal(c?: MaintenanceContract, prefill?: { contractorId?: number; addressId?: number }) {
  editContractId.value = c?.id ?? null
  const cid = c?.contractor_id ?? prefill?.contractorId ?? contractors.value[0]?.id ?? null
  await loadContractorAddresses(cid)
  contractForm.value = c
    ? {
        contractor_id: c.contractor_id,
        address_id: c.address_id,
        application_id: c.application_id ?? null,
        number: c.number,
        category: c.category || 'standard',
        price: c.price,
        signed_at: toDateInput(c.signed_at),
        valid_from: toDateInput(c.valid_from),
        valid_to: toDateInput(c.valid_to),
        status: c.status,
        note: c.note,
        equipments: (c.equipments ?? []).map((e) => ({ ...e }))
      }
    : {
        contractor_id: cid,
        address_id: prefill?.addressId ?? modalAddresses.value[0]?.id ?? null,
        application_id: null,
        number: '',
        category: 'standard',
        price: 0,
        signed_at: toDateInput(new Date().toISOString()),
        valid_from: toDateInput(new Date().toISOString()),
        valid_to: toDateInput(new Date(Date.now() + 365 * 86400000).toISOString()),
        status: 1,
        note: '',
        equipments: []
      }
  contractModalOpen.value = true
}

async function saveContract() {
  try {
    const payload = {
      ...contractForm.value,
      signed_at: dateInputToRFC3339(contractForm.value.signed_at),
      valid_from: dateInputToRFC3339(contractForm.value.valid_from),
      valid_to: dateInputToRFC3339(contractForm.value.valid_to)
    }
    if (editContractId.value) {
      await requestJSON(`/api/v1/crm/contracts/${editContractId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/contracts', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    contractModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения договора'
  }
}

function openRenewModal(c: MaintenanceContract) {
  renewContractId.value = c.id
  const oldTo = new Date(c.valid_to)
  const nextFrom = new Date(oldTo.getTime() + 86400000)
  const nextTo = new Date(nextFrom)
  nextTo.setFullYear(nextTo.getFullYear() + 1)
  nextTo.setDate(nextTo.getDate() - 1)
  renewForm.value = {
    number: `${c.number}-П`,
    category: c.category || 'standard',
    price: c.price,
    valid_from: toDateInput(nextFrom.toISOString()),
    valid_to: toDateInput(nextTo.toISOString()),
    note: ''
  }
  renewModalOpen.value = true
}

async function submitRenewContract() {
  if (!renewContractId.value) return
  try {
    await requestJSON(`/api/v1/crm/contracts/${renewContractId.value}/renew`, {
      method: 'POST',
      body: JSON.stringify({
        number: renewForm.value.number.trim(),
        category: renewForm.value.category,
        price: Number(renewForm.value.price || 0),
        valid_from: renewForm.value.valid_from ? dateInputToRFC3339(renewForm.value.valid_from) : undefined,
        valid_to: renewForm.value.valid_to ? dateInputToRFC3339(renewForm.value.valid_to) : undefined,
        note: renewForm.value.note
      })
    })
    renewModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка пролонгации договора'
  }
}

async function runExport(url: string, fallbackName: string) {
  try {
    await downloadFile(url, fallbackName)
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : 'Не удалось выгрузить файл'
  }
}

function openTaskModal(t?: CRMTask) {
  editTaskId.value = t?.id ?? null
  taskForm.value = t
    ? {
        contractor_id: t.contractor_id ?? null,
        task_type: t.task_type,
        title: t.title,
        description: t.description,
        status: t.status,
        priority: t.priority,
        assigned_user_id: t.assigned_user_id ?? null,
        plan_datetime_to: t.missing_deadline ? '' : toDatetimeLocal(t.plan_datetime_to),
        is_urgent: t.is_urgent,
        note: t.note
      }
    : {
        contractor_id: contractors.value[0]?.id ?? null,
        task_type: 'call',
        title: '',
        description: '',
        status: 1,
        priority: 1,
        assigned_user_id: workers.value[0]?.id ?? null,
        plan_datetime_to: toDatetimeLocal(new Date(Date.now() + 3600000).toISOString()),
        is_urgent: false,
        note: ''
      }
  taskModalOpen.value = true
}

async function saveTask() {
  try {
    const payload = {
      ...taskForm.value,
      plan_datetime_to: datetimeLocalToRFC3339(taskForm.value.plan_datetime_to)
    }
    if (editTaskId.value) {
      await requestJSON(`/api/v1/crm/tasks/${editTaskId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/tasks', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    taskModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения задачи'
  }
}

async function runPoolJobs() {
  const res = await requestJSON<{
    quarterly_created: number
    prolongation_created: number
    pool_assigned: number
    overdue_flagged: number
  }>('/api/v1/crm/pool-settings/run', { method: 'POST' })
  jobSummary.value = `Создано квартальных: ${res.quarterly_created}, пролонгаций: ${res.prolongation_created}, распределено по пулу: ${res.pool_assigned}, просрочено: ${res.overdue_flagged}`
  await loadAll()
}

function openExecuteTaskModal(t: CRMTask) {
  executeTaskId.value = t.id
  executeForm.value = {
    task_result: t.task_result || 'answered',
    note: '',
    new_plan_datetime_to: toDatetimeLocal(new Date(Date.now() + 4 * 3600000).toISOString())
  }
  executeModalOpen.value = true
}

async function submitExecuteTask() {
  if (!executeTaskId.value) return
  try {
    const body: Record<string, unknown> = {
      task_result: executeForm.value.task_result,
      note: executeForm.value.note
    }
    if (executeForm.value.task_result === 'no_answer' && executeForm.value.new_plan_datetime_to) {
      body.new_plan_datetime_to = datetimeLocalToRFC3339(executeForm.value.new_plan_datetime_to)
    }
    await requestJSON(`/api/v1/crm/tasks/${executeTaskId.value}/execute`, {
      method: 'POST',
      body: JSON.stringify(body)
    })
    executeModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка выполнения задачи'
  }
}

async function savePoolSetting() {
  try {
    await requestJSON('/api/v1/crm/pool-settings', {
      method: 'POST',
      body: JSON.stringify(poolForm.value)
    })
    poolModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения правила пула'
  }
}

function openSubdivisionModal(sub?: Subdivision) {
  editSubId.value = sub?.id ?? null
  subForm.value = sub
    ? {
        name: sub.name,
        address: sub.address,
        supervisor_user_id: sub.supervisor_user_id ?? null,
        phones: sub.phones?.length
          ? sub.phones.map((p) => ({ ...p, is_main: Boolean(p.is_main) }))
          : [{ phone: '', is_main: true, is_duty: false }],
        payment_options: (sub.payment_options ?? []).map((po) => ({ ...po }))
      }
    : {
        name: '',
        address: '',
        supervisor_user_id: null,
        phones: [{ phone: '', is_main: true, is_duty: false }],
        payment_options: []
      }
  subModalOpen.value = true
}

function addSubPaymentOption() {
  const firstPo = paymentOptionsSelect.value[0]
  subForm.value.payment_options.push({
    payment_option_id: firstPo?.value ?? 1,
    key: firstPo?.key ?? 'cash',
    file_id: null,
    file_url: '',
    is_main: subForm.value.payment_options.length === 0
  })
}

async function handleSubQrUpload(idx: number, evt: Event) {
  const input = evt.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploadingSubQrIdx.value = idx
  try {
    const uploaded = await uploadCRMFile(file)
    subForm.value.payment_options[idx].file_id = uploaded.id
    subForm.value.payment_options[idx].file_url = uploaded.url
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить QR-файл'
  } finally {
    uploadingSubQrIdx.value = null
    input.value = ''
  }
}

async function saveSubdivision() {
  if (!subForm.value.name.trim()) return
  const payload = {
    name: subForm.value.name.trim(),
    address: subForm.value.address.trim(),
    supervisor_user_id: subForm.value.supervisor_user_id,
    phones: subForm.value.phones.filter((p) => p.phone.trim()),
    payment_options: subForm.value.payment_options.map((po) => ({
      ...po,
      key: paymentOptionsSelect.value.find((o) => o.value === po.payment_option_id)?.key || po.key || ''
    }))
  }
  if (editSubId.value) {
    await requestJSON(`/api/v1/crm/subdivisions/${editSubId.value}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    })
  } else {
    await requestJSON('/api/v1/crm/subdivisions', {
      method: 'POST',
      body: JSON.stringify(payload)
    })
  }
  subModalOpen.value = false
  await loadAll()
}

async function deleteSubdivision(id: number) {
  try {
    await requestJSON(`/api/v1/crm/subdivisions/${id}`, { method: 'DELETE' })
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось удалить подразделение'
  }
}

function openSipModal(sip?: SipAccount) {
  sipForm.value = sip
    ? {
        id: sip.id,
        name: sip.name,
        domain: sip.domain,
        login: sip.login,
        password: '',
        abonent_id: sip.abonent_id || '',
        protocol: sip.protocol || 'wss',
        server: sip.server,
        port: sip.port || 8089,
        subdivision_id: sip.subdivision_id ?? null,
        user_id: sip.user_id ?? null,
        active: sip.active
      }
    : {
        id: 0,
        name: '',
        domain: '',
        login: '',
        password: '',
        abonent_id: '',
        protocol: 'wss',
        server: '',
        port: 8089,
        subdivision_id: null,
        user_id: null,
        active: true
      }
  sipModalOpen.value = true
}

async function saveSipAccount() {
  try {
    await requestJSON('/api/v1/crm/sip-accounts', {
      method: 'POST',
      body: JSON.stringify(sipForm.value)
    })
    sipModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения SIP-аккаунта'
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
  try {
    await requestJSON(`/api/v1/crm/calls/${editCall.value.id}`, {
      method: 'PUT',
      body: JSON.stringify(callCardForm.value)
    })
    callCardModalOpen.value = false
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения карточки звонка'
  }
}

async function sendTransfer() {
  await requestJSON('/api/v1/crm/calls/transfer', {
    method: 'POST',
    body: JSON.stringify(transferForm.value)
  })
  transferModalOpen.value = false
}

async function transcribeCallRecord(call: CallRecord) {
  transcribingCallId.value = call.id
  errorMsg.value = ''
  try {
    const updated = await requestJSON<CallRecord>(`/api/v1/crm/calls/${call.id}/transcribe`, {
      method: 'POST'
    })
    if (editCall.value?.id === call.id) {
      editCall.value = updated
    }
    await loadAll()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка расшифровки записи звонка'
  } finally {
    transcribingCallId.value = null
  }
}

const contractColumns: DataTableColumns<MaintenanceContract> = [
  { title: '№ договора', key: 'number' },
  {
    title: 'Тариф',
    key: 'category',
    render: (r) => h(NTag, { size: 'small', type: 'info' }, { default: () => contractCategoryLabel(r.category) })
  },
  { title: 'Контрагент', key: 'contractor_name' },
  { title: 'Адрес', key: 'address_text', ellipsis: { tooltip: true } },
  { title: 'Сумма', key: 'price', render: (r) => formatMoneyRUB(r.price) },
  {
    title: 'Срок действия',
    key: 'valid_to',
    render: (r) => `${formatDateOnly(r.valid_from)} — ${formatDateOnly(r.valid_to)}`
  },
  {
    title: 'Статус',
    key: 'status',
    render: (r) =>
      h(NSpace, { size: 4 }, () => [
        h(NTag, { size: 'small' }, { default: () => documentStatusLabel(r.status) }),
        r.needs_prolongation
          ? h(NTag, { size: 'tiny', type: 'warning' }, { default: () => 'Продление' })
          : null
      ])
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 230,
    render: (r) =>
      h(NSpace, { size: 4 }, () => [
        h(NButton, { size: 'tiny', onClick: () => void openContractModal(r) }, { default: () => 'Изм.' }),
        auth.hasPerm('crm.contract.create') &&
          h(
            NButton,
            { size: 'tiny', type: 'primary', secondary: true, onClick: () => openRenewModal(r) },
            { default: () => 'Продлить' }
          ),
        h(
          NButton,
          { size: 'tiny', secondary: true, onClick: () => openPrintTab(`/api/v1/crm/contracts/${r.id}/print`) },
          { default: () => 'Печать' }
        )
      ])
  }
]

const taskColumns: DataTableColumns<CRMTask> = [
  { title: 'ID', key: 'id', width: 60 },
  {
    title: 'Задача',
    key: 'title',
    render: (r) =>
      h(NSpace, { size: 6, align: 'center' }, () => [
        r.is_urgent ? h(NTag, { type: 'error', size: 'tiny' }, { default: () => 'Срочно' }) : null,
        r.prev_task_id ? h(NTag, { type: 'warning', size: 'tiny' }, { default: () => `Повтор #${r.prev_task_id}` }) : null,
        h('span', r.title)
      ])
  },
  { title: 'Тип', key: 'task_type' },
  { title: 'Контрагент', key: 'contractor_name' },
  { title: 'Исполнитель', key: 'assigned_name' },
  { title: 'Дедлайн', key: 'plan_datetime_to', render: (r) => r.missing_deadline ? 'Не указан' : formatDateTime(r.plan_datetime_to) },
  {
    title: 'Статус',
    key: 'status',
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: r.status === 4 ? 'warning' : r.status === 3 ? 'success' : 'default' },
        { default: () => taskStatusLabel(r.status) }
      )
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 170,
    render: (r) =>
      h(NSpace, { size: 6 }, () => [
        auth.hasPerm('crm.task.execute') &&
          h(
            NButton,
            { size: 'tiny', type: 'primary', secondary: true, onClick: () => openExecuteTaskModal(r) },
            { default: () => 'Выполнить' }
          ),
        auth.hasPerm('crm.task.update') && h(NButton, { size: 'tiny', onClick: () => openTaskModal(r) }, { default: () => 'Изм.' })
      ])
  }
]

const callColumns: DataTableColumns<CallRecord> = [
  { title: 'Время', key: 'started_at', render: (r) => formatDateTime(r.started_at) },
  { title: 'Напр.', key: 'direction', width: 85 },
  { title: 'Статус', key: 'status', width: 85 },
  { title: 'Кто звонил', key: 'caller_phone', render: (r) => formatPhoneRU(r.caller_phone) },
  { title: 'Куда', key: 'callee_phone', render: (r) => formatPhoneRU(r.callee_phone) },
  { title: 'Контрагент', key: 'contractor_name' },
  { title: 'Источник', key: 'traffic_source_name', render: (r) => r.traffic_source_name || '—' },
  { title: 'Обращение', key: 'type_of_appeal', ellipsis: { tooltip: true } },
  {
    title: 'Итог',
    key: 'call_result',
    render: (r) =>
      callResultOptions.value.find((o) => o.value === r.call_result)?.label || '—'
  },
  { title: 'Длит. (с)', key: 'duration_sec', width: 75 },
  {
    title: 'Запись',
    key: 'recording',
    width: 225,
    render: (r) =>
      r.ext_tracking_id
        ? h('audio', {
            controls: true,
            preload: 'none',
            src: `/api/v1/crm/calls/${r.id}/recording`,
            style: 'height: 28px; width: 210px'
          })
        : '—'
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 185,
    render: (r) =>
      auth.hasPerm('crm.call.update')
        ? h(NSpace, { size: 6 }, () => [
            h(
              NButton,
              { size: 'tiny', secondary: true, onClick: () => openCallCard(r) },
              { default: () => 'Карточка' }
            ),
            h(
              NButton,
              {
                size: 'tiny',
                onClick: () => {
                  transferForm.value = { external_call_id: r.external_call_id, target_phone: '' }
                  transferModalOpen.value = true
                }
              },
              { default: () => 'Перевод' }
            ),
            r.ext_tracking_id || r.external_call_id?.startsWith('mango:')
              ? h(
                  NButton,
                  {
                    size: 'tiny',
                    type: r.transcript_status === 'done' ? 'success' : 'info',
                    secondary: true,
                    loading: transcribingCallId.value === r.id,
                    onClick: () => void transcribeCallRecord(r)
                  },
                  { default: () => (r.transcript_status === 'done' ? 'Текст ✓' : 'Расшифр.') }
                )
              : null
          ])
        : '—'
  }
]

onMounted(async () => {
  if (typeof route.query.tab === 'string' && route.query.tab) {
    activeTab.value = route.query.tab
  }
  await loadAll()
  if (route.query.contractorId && activeTab.value === 'contracts' && auth.hasPerm('crm.contract.create')) {
    await openContractModal(undefined, {
      contractorId: Number(route.query.contractorId),
      addressId: route.query.addressId ? Number(route.query.addressId) : undefined
    })
  }
})
</script>

<template>
  <div class="ops-page">
    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>
    <NAlert v-if="jobSummary" type="success" closable @close="jobSummary = ''">{{ jobSummary }}</NAlert>

    <NTabs v-model:value="activeTab" type="line">
      <NTabPane name="contracts" tab="Договоры ТО">
        <NSpace justify="space-between" align="center" style="margin-bottom: 12px" wrap>
          <NSpace align="center" wrap>
            <NSwitch v-model:value="contractOnlyProlongation" @update:value="loadAll">
              <template #checked>Требуют пролонгации</template>
              <template #unchecked>Все договоры</template>
            </NSwitch>
            <NInput
              v-model:value="contractRange"
              placeholder="01.11.2026 - 30.11.2026"
              style="width: 220px"
              @keyup.enter="loadAll"
            />
            <NButton size="small" secondary @click="loadAll">Применить</NButton>
          </NSpace>
          <NSpace>
            <NButton v-if="auth.hasPerm('crm.contract.export')"
              secondary
              @click="
                void runExport(
                  `/api/v1/crm/contracts/export?${buildContractsQS({ format: 'xlsx' })}`,
                  `contracts-${new Date().toISOString().slice(0, 10)}.xlsx`
                )
              "
            >
              Экспорт XLSX
            </NButton>
            <NButton v-if="auth.hasPerm('crm.contract.export')"
              secondary
              @click="
                void runExport(
                  `/api/v1/crm/contracts/export?${buildContractsQS({ format: 'csv' })}`,
                  `contracts-${new Date().toISOString().slice(0, 10)}.csv`
                )
              "
            >
              Экспорт CSV
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.contract.create')"
              type="primary"
              @click="openContractModal()"
            >
              + Новый договор
            </NButton>
          </NSpace>
        </NSpace>
        <NDataTable :columns="contractColumns" :data="contracts" :loading="loading" :row-key="(r) => r.id" />
      </NTabPane>

      <NTabPane name="tasks" tab="Задачи и Пул">
        <NSpace justify="space-between" style="margin-bottom: 12px" wrap>
          <NSpace align="center" wrap>
            <NSelect
              v-model:value="taskStatusFilter"
              clearable
              placeholder="Статус задачи"
              style="width: 170px"
              :options="enums.task_statuses.map((s) => ({ label: s.label, value: s.id }))"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="taskTypeFilter"
              clearable
              placeholder="Тип задачи"
              style="width: 210px"
              :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))"
              @update:value="loadAll"
            />
            <NButton
              size="small"
              :type="onlyUrgentTasks ? 'error' : 'default'"
              @click="
                () => {
                  onlyUrgentTasks = !onlyUrgentTasks
                  loadAll()
                }
              "
            >
              Только срочные
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.pool.manage')"
              size="small"
              secondary
              @click="runPoolJobs"
            >
              Запустить планировщик задач и пул
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.pool.manage') || auth.hasPerm('crm.pool.update')"
              size="small"
              secondary
              @click="poolModalOpen = true"
            >
              + Правило пула
            </NButton>
          </NSpace>
          <NButton
            v-if="auth.hasPerm('crm.task.create')"
            type="primary"
            @click="openTaskModal()"
          >
            + Новая задача
          </NButton>
        </NSpace>
        <NDataTable :columns="taskColumns" :data="tasks" :loading="loading" :row-key="(r) => r.id" />

        <NDivider>Правила распределения пула ({{ poolSettings.length }})</NDivider>
        <NSpace>
          <NCard v-for="ps in poolSettings" :key="ps.id" size="small" style="min-width: 240px">
            <strong>{{ ps.name }}</strong>
            <div>Приоритет: {{ ps.priority }} · Макс. активных: {{ ps.tasks_max_count }}</div>
            <div>Типы: {{ (ps.task_types ?? []).join(', ') || 'любые' }}</div>
          </NCard>
        </NSpace>
      </NTabPane>

      <NTabPane name="telephony" tab="Звонки и Подразделения">
        <NAlert v-if="currentCalls.length" type="info" style="margin-bottom: 10px">
          Активных звонков в линии: {{ currentCalls.length }} ({{ currentCalls.map((c) => formatPhoneRU(c.caller_phone)).join(', ') }})
        </NAlert>
        <NSpace justify="space-between" align="center" style="margin-bottom: 10px" wrap>
          <NSpace wrap align="center">
            <NSelect
              v-model:value="callDirectionFilter"
              clearable
              placeholder="Направление"
              style="width: 140px"
              :options="callDirectionOptions"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callStatusFilter"
              clearable
              placeholder="Статус"
              style="width: 135px"
              :options="callStatusOptions"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callCallerTypeFilter"
              clearable
              placeholder="Тип абонента"
              style="width: 150px"
              :options="callerTypeOptions"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callResultFilter"
              clearable
              placeholder="Итог звонка"
              style="width: 155px"
              :options="callResultOptions"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callAppealFilter"
              clearable
              placeholder="Обращение"
              style="width: 165px"
              :options="allCallAppealOptions"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callTrafficSourceFilter"
              clearable
              placeholder="Источник"
              style="width: 155px"
              :options="trafficSources.map((s) => ({ label: s.name, value: s.id }))"
              @update:value="loadAll"
            />
            <NSelect
              v-model:value="callSubdivisionFilter"
              clearable
              placeholder="Филиал"
              style="width: 150px"
              :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
              @update:value="loadAll"
            />
            <NInput
              v-model:value="callRangeFilter"
              placeholder="01.11.2026 - 30.11.2026"
              style="width: 205px"
              @keyup.enter="loadAll"
            />
            <NButton size="small" secondary @click="loadAll">Фильтр</NButton>
          </NSpace>
          <NSpace>
            <NButton v-if="auth.hasPerm('crm.call.export')"
              size="small"
              secondary
              @click="
                void runExport(
                  `/api/v1/crm/calls/export?${buildCallsQS({ format: 'xlsx' })}`,
                  `calls-${new Date().toISOString().slice(0, 10)}.xlsx`
                )
              "
            >
              Экспорт XLSX
            </NButton>
            <NButton v-if="auth.hasPerm('crm.call.export')"
              size="small"
              secondary
              @click="
                void runExport(
                  `/api/v1/crm/calls/export?${buildCallsQS({ format: 'csv' })}`,
                  `calls-${new Date().toISOString().slice(0, 10)}.csv`
                )
              "
            >
              Экспорт CSV
            </NButton>
          </NSpace>
        </NSpace>
        <NDataTable :columns="callColumns" :data="calls" :loading="loading" :row-key="(r) => r.id" />

        <NDivider>Пул пропущенных звонков ({{ missCalls.length }})</NDivider>
        <NSpace style="margin-bottom: 8px" align="center">
          <NSwitch v-model:value="missCallsOnlyUnviewed" @update:value="loadMissCalls">
            <template #checked>Только необработанные</template>
            <template #unchecked>Все пропущенные</template>
          </NSwitch>
        </NSpace>
        <NDataTable
          :data="missCalls"
          :row-key="(r: MissCallPoolItem) => r.id"
          :columns="[
            { title: 'Время', key: 'started_at', render: (r: MissCallPoolItem) => formatDateTime(r.started_at) },
            { title: 'Кто звонил', key: 'caller_phone', render: (r: MissCallPoolItem) => formatPhoneRU(r.caller_phone) },
            { title: 'Линия', key: 'callee_phone', render: (r: MissCallPoolItem) => formatPhoneRU(r.callee_phone) },
            { title: 'Контрагент', key: 'contractor_name', render: (r: MissCallPoolItem) => r.contractor_name || 'Неизвестный' },
            {
              title: 'Статус',
              key: 'is_viewed',
              render: (r: MissCallPoolItem) =>
                h(NTag, { size: 'tiny', type: r.is_viewed ? 'success' : 'warning' }, { default: () => (r.is_viewed ? 'Просмотрено' : 'Новый') })
            },
            {
              title: '',
              key: 'act',
              width: 130,
              render: (r: MissCallPoolItem) =>
                !r.is_viewed
                  ? h(NButton, { size: 'tiny', type: 'primary', secondary: true, onClick: () => void markMissCallViewedInTab(r.call_id) }, { default: () => 'Просмотрено' })
                  : '—'
            }
          ]"
        />

        <NDivider>Подразделения ({{ subdivisions.length }})</NDivider>
        <NSpace justify="space-between" style="margin-bottom: 10px">
          <NSpace wrap>
            <NCard v-for="s in subdivisions" :key="s.id" size="small" style="min-width: 260px">
              <div style="display: flex; justify-content: space-between; gap: 8px; align-items: center">
                <strong>{{ s.name }}</strong>
                <NSpace :size="4">
                  <NButton v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.update')" size="tiny" @click="openSubdivisionModal(s)">Изм.</NButton>
                  <NButton v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.delete')" size="tiny" type="error" ghost @click="deleteSubdivision(s.id)">Уд.</NButton>
                </NSpace>
              </div>
              <div style="font-size: 12px; color: #64748b">{{ s.address || 'Без адреса' }}</div>
              <div v-if="s.supervisor_user_id" style="font-size: 12px; color: #475569">
                Руководитель: {{ workerNameById(s.supervisor_user_id) }}
              </div>
              <div v-for="(p, idx) in s.phones || []" :key="idx" style="font-size: 12px">
                {{ formatPhoneRU(p.phone) }}
                <NTag v-if="p.is_main" size="tiny" type="info">Основной</NTag>
                <NTag v-if="p.is_duty" size="tiny" type="warning">Дежурный</NTag>
              </div>
              <div v-if="s.payment_options?.length" style="margin-top: 4px">
                <NSpace :size="4" wrap>
                  <NTag
                    v-for="(po, pidx) in s.payment_options"
                    :key="pidx"
                    size="tiny"
                    :type="po.is_main ? 'success' : 'default'"
                  >
                    {{ po.key || `#${po.payment_option_id}` }}<template v-if="po.file_url"> · QR</template>
                  </NTag>
                </NSpace>
              </div>
            </NCard>
          </NSpace>
          <NButton v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.create')" type="primary" size="small" @click="openSubdivisionModal()">
            + Подразделение
          </NButton>
        </NSpace>

        <NDivider>SIP-аккаунты сотрудников ({{ sipAccounts.length }})</NDivider>
        <NSpace justify="space-between" style="margin-bottom: 10px">
          <NSpace wrap>
            <NTag v-for="sip in sipAccounts" :key="sip.id" size="large" @click="(auth.hasPerm('crm.sip.manage') || auth.hasPerm('crm.sip.update')) && openSipModal(sip)" style="cursor: pointer">
              {{ sip.name || sip.abonent_id || sip.login }}
              <template v-if="sip.abonent_id"> · ВАТС {{ sip.abonent_id }}</template>
              <template v-if="sip.user_id"> · {{ workerNameById(sip.user_id) }}</template>
            </NTag>
            <NTag v-for="su in sipUserSelect" :key="`sel-${su.id}`" size="small" type="info">
              Абонент {{ su.abonent_id || su.name }} -> {{ su.full_name || `#${su.user_id}` }}
            </NTag>
          </NSpace>
          <NButton v-if="auth.hasPerm('crm.sip.manage') || auth.hasPerm('crm.sip.create')" size="small" secondary @click="openSipModal()">
            + SIP-аккаунт
          </NButton>
        </NSpace>
      </NTabPane>

      <NTabPane name="refs" tab="Справочники">
        <NSpace justify="space-between" style="margin-bottom: 12px">
          <NSpace>
            <NSelect
              v-model:value="activeRefKind"
              style="width: 240px"
              :options="[
                { label: 'Производители оборудования', value: 'manufacturer' },
                { label: 'Типы оборудования', value: 'equipment_type' },
                { label: 'Виды работ', value: 'work_type' },
                { label: 'Шаблоны особых отметок', value: 'special_note' }
              ]"
              @update:value="loadRefs"
            />
          </NSpace>
          <NSpace v-if="auth.hasPerm('crm.ref.manage') || auth.hasPerm(`crm.ref.${activeRefKind}.create`)">
            <NInput v-model:value="newRefName" placeholder="Новое значение..." style="width: 240px" @keyup.enter="createRefItem" />
            <NButton type="primary" @click="createRefItem">Добавить</NButton>
          </NSpace>
        </NSpace>
        <NDataTable
          :data="refItems"
          :row-key="(r: RefItem) => r.id"
          :columns="[
            { title: 'ID', key: 'id', width: 70 },
            { title: 'Название', key: 'name' },
            {
              title: '',
              key: 'del',
              width: 90,
              render: (r: RefItem) =>
                (auth.hasPerm('crm.ref.manage') || auth.hasPerm(`crm.ref.${activeRefKind}.delete`))
                  ? h(NButton, { size: 'tiny', type: 'error', ghost: true, onClick: () => void deleteRefItem(r.id) }, { default: () => 'Удалить' })
                  : '—'
            }
          ]"
        />
      </NTabPane>

      <NTabPane name="reports" tab="Отчёты">
        <ReportsPanel :subdivisions="subdivisions" :workers="workers" :enums="enums" />
      </NTabPane>
    </NTabs>

    <NModal
      v-model:show="contractModalOpen"
      preset="card"
      :title="editContractId ? 'Редактировать договор' : 'Новый договор ТО'"
      style="width: 540px"
    >
      <NForm label-placement="top">
        <NFormItem label="Контрагент">
          <NSelect
            v-model:value="contractForm.contractor_id"
            filterable
            :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
            @update:value="(v) => loadContractorAddresses(Number(v))"
          />
        </NFormItem>
        <NFormItem label="Адрес">
          <NSelect
            v-model:value="contractForm.address_id"
            :options="modalAddresses.map((a) => ({ label: a.address, value: a.id }))"
          />
        </NFormItem>
        <NSpace>
          <NFormItem label="Номер договора" style="width: 240px">
            <NInput v-model:value="contractForm.number" />
          </NFormItem>
          <NFormItem label="Стоимость (₽)" style="width: 220px">
            <NInputNumber v-model:value="contractForm.price" :min="0" />
          </NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="Подписан">
            <input v-model="contractForm.signed_at" type="date" class="native-dt" />
          </NFormItem>
          <NFormItem label="Действует с">
            <input v-model="contractForm.valid_from" type="date" class="native-dt" />
          </NFormItem>
          <NFormItem label="Действует по">
            <input v-model="contractForm.valid_to" type="date" class="native-dt" />
          </NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="Категория (тариф)" style="width: 240px">
            <NSelect v-model:value="contractForm.category" :options="contractCategoryOptions" />
          </NFormItem>
          <NFormItem label="Статус" style="width: 220px">
            <NSelect
              v-model:value="contractForm.status"
              :options="enums.document_statuses.map((s) => ({ label: s.label, value: s.id }))"
            />
          </NFormItem>
        </NSpace>
        <NFormItem label="Оборудование по договору">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(eq, idx) in contractForm.equipments" :key="idx">
              <NSelect
                v-model:value="eq.equipment_id"
                filterable
                :options="equipmentsCatalog.map((e) => ({ label: e.name, value: e.id }))"
                style="width: 250px"
              />
              <NInput v-model:value="eq.serial_number" placeholder="Серийный №" style="width: 170px" />
              <NButton size="small" @click="contractForm.equipments.splice(idx, 1)">×</NButton>
            </NSpace>
            <NButton
              size="tiny"
              @click="
                contractForm.equipments.push({
                  equipment_id: equipmentsCatalog[0]?.id ?? 0,
                  serial_number: ''
                })
              "
            >
              + Добавить котёл/оборудование
            </NButton>
          </NSpace>
        </NFormItem>
        <NFormItem label="Примечание">
          <NInput v-model:value="contractForm.note" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="contractModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveContract">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="taskModalOpen"
      preset="card"
      :title="editTaskId ? 'Редактировать задачу' : 'Новая задача'"
      style="width: 540px"
    >
      <NForm label-placement="top">
        <NFormItem label="Заголовок">
          <NInput v-model:value="taskForm.title" />
        </NFormItem>
        <NSpace>
          <NFormItem label="Тип задачи" style="width: 240px">
            <NSelect
              v-model:value="taskForm.task_type"
              :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))"
            />
          </NFormItem>
          <NFormItem label="Статус" style="width: 220px">
            <NSelect
              v-model:value="taskForm.status"
              :options="enums.task_statuses.map((s) => ({ label: s.label, value: s.id }))"
            />
          </NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="Исполнитель" style="width: 240px">
            <NSelect
              v-model:value="taskForm.assigned_user_id"
              clearable
              :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
            />
          </NFormItem>
          <NFormItem label="Дедлайн">
            <input v-model="taskForm.plan_datetime_to" type="datetime-local" class="native-dt" />
          </NFormItem>
        </NSpace>
        <NFormItem label="Срочная задача">
          <NSwitch v-model:value="taskForm.is_urgent" />
        </NFormItem>
        <NFormItem label="Описание / Примечание (при статусе Недозвон закроется и создаст повторную через 2 ч)">
          <NInput v-model:value="taskForm.note" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="taskModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveTask">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="transferModalOpen" preset="card" title="Перевести звонок" style="width: 380px">
      <NForm label-placement="top">
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

    <NModal v-model:show="executeModalOpen" preset="card" title="Выполнение задачи" style="width: 480px">
      <NForm label-placement="top">
        <NFormItem label="Результат звонка / задачи">
          <NSelect v-model:value="executeForm.task_result" :options="taskResultOptions" />
        </NFormItem>
        <NFormItem v-if="executeForm.task_result === 'no_answer'" label="Срок повторного звонка">
          <input v-model="executeForm.new_plan_datetime_to" type="datetime-local" class="native-dt" />
        </NFormItem>
        <NFormItem label="Комментарий к результату">
          <NInput v-model:value="executeForm.note" type="textarea" :rows="3" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="executeModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="submitExecuteTask">Зафиксировать результат</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="renewModalOpen" preset="card" title="Пролонгация договора ТО" style="width: 500px">
      <NForm label-placement="top">
        <div class="two-col">
          <NFormItem label="Новый номер"><NInput v-model:value="renewForm.number" /></NFormItem>
          <NFormItem label="Тариф">
            <NSelect v-model:value="renewForm.category" :options="contractCategoryOptions" />
          </NFormItem>
        </div>
        <div class="two-col">
          <NFormItem label="Действует с">
            <input v-model="renewForm.valid_from" type="date" class="native-dt" />
          </NFormItem>
          <NFormItem label="Действует по">
            <input v-model="renewForm.valid_to" type="date" class="native-dt" />
          </NFormItem>
        </div>
        <NFormItem label="Сумма (₽)">
          <NInputNumber v-model:value="renewForm.price" :min="0" style="width: 100%" />
        </NFormItem>
        <NFormItem label="Примечание"><NInput v-model:value="renewForm.note" type="textarea" :rows="2" /></NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="renewModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="submitRenewContract">Продлить договор</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="callCardModalOpen" preset="card" title="Карточка звонка" style="width: 520px">
      <NForm label-placement="top">
        <NFormItem label="Контрагент">
          <NSelect
            v-model:value="callCardForm.contractor_id"
            filterable
            clearable
            :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
          />
        </NFormItem>
        <NFormItem label="Отвязать номер от других контрагентов">
          <NSwitch v-model:value="callCardForm.detach_from_others" />
        </NFormItem>
        <div class="two-col">
          <NFormItem label="Тип абонента">
            <NSelect v-model:value="callCardForm.caller_type" :options="callerTypeOptions" />
          </NFormItem>
          <NFormItem label="Итог звонка">
            <NSelect v-model:value="callCardForm.call_result" :options="callResultOptions" />
          </NFormItem>
        </div>
        <div class="two-col">
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
        <NFormItem v-if="editCall" label="Расшифровка звонка (STT)">
          <NSpace vertical style="width: 100%">
            <NSpace align="center">
              <NButton
                v-if="editCall.ext_tracking_id || editCall.external_call_id?.startsWith('mango:')"
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
          <NButton type="primary" @click="saveCallCard">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="subModalOpen" preset="card" :title="editSubId ? 'Редактировать подразделение' : 'Новое подразделение'" style="width: 620px; max-width: 96vw">
      <NForm label-placement="top">
        <NFormItem label="Название"><NInput v-model:value="subForm.name" /></NFormItem>
        <NFormItem label="Адрес"><NInput v-model:value="subForm.address" /></NFormItem>
        <NFormItem label="Руководитель подразделения">
          <NSelect
            v-model:value="subForm.supervisor_user_id"
            filterable
            clearable
            placeholder="Выберите сотрудника"
            :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
          />
        </NFormItem>
        <NFormItem label="Телефоны филиала">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(p, idx) in subForm.phones" :key="idx" align="center">
              <NInput v-model:value="p.phone" placeholder="+7 (999) 123-45-67" style="width: 210px" />
              <NCheckbox v-model:checked="p.is_main">Основной</NCheckbox>
              <NCheckbox v-model:checked="p.is_duty">Дежурный</NCheckbox>
              <NButton size="small" @click="subForm.phones.splice(idx, 1)">×</NButton>
            </NSpace>
            <NButton size="tiny" @click="subForm.phones.push({ phone: '', is_main: false, is_duty: false })">+ Телефон</NButton>
          </NSpace>
        </NFormItem>
        <NFormItem label="Способы оплаты и QR-коды">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(po, idx) in subForm.payment_options" :key="idx" align="center" wrap>
              <NSelect
                v-model:value="po.payment_option_id"
                style="width: 185px"
                :options="paymentOptionsSelect"
              />
              <NCheckbox v-model:checked="po.is_main">Основной</NCheckbox>
              <label style="cursor: pointer; font-size: 12px; color: #2563eb">
                <input
                  type="file"
                  accept="image/*"
                  style="display: none"
                  @change="(e) => handleSubQrUpload(idx, e)"
                />
                {{ uploadingSubQrIdx === idx ? 'Загрузка...' : po.file_url ? 'Заменить QR' : 'Загрузить QR' }}
              </label>
              <a v-if="po.file_url" :href="po.file_url" target="_blank" style="font-size: 12px">Открыть QR</a>
              <NButton size="small" @click="subForm.payment_options.splice(idx, 1)">×</NButton>
            </NSpace>
            <NButton size="tiny" @click="addSubPaymentOption">+ Способ оплаты</NButton>
          </NSpace>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="subModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveSubdivision">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="sipModalOpen" preset="card" title="SIP-аккаунт / WebRTC" style="width: 520px">
      <NForm label-placement="top">
        <NSpace>
          <NFormItem label="Название линии" style="width: 220px"><NInput v-model:value="sipForm.name" /></NFormItem>
          <NFormItem label="ID абонента / внутренний номер MANGO" style="width: 240px"><NInput v-model:value="sipForm.abonent_id" /></NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="Сотрудник" style="width: 230px">
            <NSelect v-model:value="sipForm.user_id" filterable clearable :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))" />
          </NFormItem>
          <NFormItem label="Подразделение" style="width: 230px">
            <NSelect v-model:value="sipForm.subdivision_id" clearable :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))" />
          </NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="SIP-логин (опц.)" style="width: 220px"><NInput v-model:value="sipForm.login" /></NFormItem>
          <NFormItem label="SIP-пароль (опц.)" style="width: 240px"><NInput v-model:value="sipForm.password" type="password" /></NFormItem>
        </NSpace>
        <NSpace>
          <NFormItem label="Домен (опц.)" style="width: 220px"><NInput v-model:value="sipForm.domain" /></NFormItem>
          <NFormItem label="WSS-сервер (опц.)" style="width: 160px"><NInput v-model:value="sipForm.server" /></NFormItem>
          <NFormItem label="Порт" style="width: 75px"><NInputNumber v-model:value="sipForm.port" /></NFormItem>
        </NSpace>
        <NFormItem label="Активен"><NSwitch v-model:value="sipForm.active" /></NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="sipModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveSipAccount">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="poolModalOpen" preset="card" title="Правило распределения пула" style="width: 500px">
      <NForm label-placement="top">
        <NFormItem label="Название"><NInput v-model:value="poolForm.name" /></NFormItem>
        <NFormItem label="Типы задач">
          <NSelect v-model:value="poolForm.task_types" multiple :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))" />
        </NFormItem>
        <NSpace>
          <NFormItem label="Окно с" style="width: 110px"><NInput v-model:value="poolForm.time_from" /></NFormItem>
          <NFormItem label="по" style="width: 110px"><NInput v-model:value="poolForm.time_to" /></NFormItem>
          <NFormItem label="Приоритет" style="width: 110px"><NInputNumber v-model:value="poolForm.priority" /></NFormItem>
          <NFormItem label="Макс. задач" style="width: 110px"><NInputNumber v-model:value="poolForm.tasks_max_count" /></NFormItem>
        </NSpace>
        <NSpace>
          <NCheckbox v-model:checked="poolForm.for_dispatcher">Диспетчерам</NCheckbox>
          <NCheckbox v-model:checked="poolForm.for_engineer">Инженерам</NCheckbox>
          <NCheckbox v-model:checked="poolForm.with_subdivision">По филиалу</NCheckbox>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="poolModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="savePoolSetting">Сохранить правило</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.native-dt {
  width: 100%;
  padding: 5px 8px;
  border: 1px solid #d1d5db;
  border-radius: 4px;
}
</style>
