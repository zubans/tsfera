<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRoute } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, uploadCRMFile, ApiError } from '@/shared/api/http'
import { formatPhoneRU } from '@/shared/utils/format'
import { paymentOptionLabel, taskTypeLabel } from './crmLabels'
import CrmCatalogsPanel from './CrmCatalogsPanel.vue'
import { useAuthStore } from '@/stores/auth'
import type {
  RefItem,
  Subdivision,
  SubdivisionPhone,
  SubdivisionPaymentOption,
  SipAccount,
  SipUserSelectOption,
  TaskPoolSetting,
  UserBrief,
  CRMEnums
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()

const activeTab = ref<string>('refs')
const errorMsg = ref('')
const okMsg = ref('')

// --- Справочники ---
const activeRefKind = ref<'manufacturer' | 'equipment_type' | 'work_type' | 'special_note'>('manufacturer')
const refItems = ref<RefItem[]>([])
const loadingRefs = ref(false)
const refSearch = ref('')
const onlyShortRefs = ref(false)
const newRefName = ref('')
const editRefModalOpen = ref(false)
const editRefItem = ref<{ id: number; name: string } | null>(null)

// --- Подразделения ---
const subdivisions = ref<Subdivision[]>([])
const loadingSubs = ref(false)
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

// --- SIP ---
const sipAccounts = ref<SipAccount[]>([])
const sipUserSelect = ref<SipUserSelectOption[]>([])
const sipSearch = ref('')
const loadingSip = ref(false)
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

// --- Пул задач ---
const poolSettings = ref<TaskPoolSetting[]>([])
const loadingPool = ref(false)
const runningPool = ref(false)
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

const workers = ref<UserBrief[]>([])
const enums = ref<CRMEnums>({
  work_types: [],
  document_statuses: [],
  task_types: [
    { id: 'call', label: 'Звонок' },
    { id: 'quarterly_call', label: 'Ежеквартальный обзвон' },
    { id: 'prolongation', label: 'Пролонгация договора' },
    { id: 'no_answer', label: 'Недозвон' },
    { id: 'service', label: 'ТО / Сервис' },
    { id: 'other', label: 'Прочее' }
  ],
  task_statuses: [],
  payment_options: [
    { id: 1, key: 'cash', label: 'Наличные' },
    { id: 2, key: 'card', label: 'Терминал' },
    { id: 3, key: 'bank_transfer', label: 'Безналичный расчёт' },
    { id: 4, key: 'online', label: 'Онлайн / СБП' }
  ]
})

const paymentOptionsSelect = computed(() =>
  (enums.value.payment_options ?? []).map((p) => ({ label: p.label, value: p.id, key: p.key }))
)

function workerNameById(id?: number | null): string {
  if (!id) return '—'
  const w = workers.value.find((u) => u.id === id)
  return w ? w.full_name || w.username : `Сотрудник #${id}`
}

function subdivisionNameById(id?: number | null): string {
  if (!id) return '—'
  const s = subdivisions.value.find((sub) => sub.id === id)
  return s ? s.name : `#${id}`
}

const filteredRefs = computed(() => {
  const q = refSearch.value.trim().toLowerCase()
  return refItems.value.filter((item) => {
    if (onlyShortRefs.value && item.name.trim().length > 2) return false
    if (!q) return true
    return item.name.toLowerCase().includes(q) || String(item.id).includes(q)
  })
})

const filteredSipAccounts = computed(() => {
  const q = sipSearch.value.trim().toLowerCase()
  if (!q) return sipAccounts.value
  return sipAccounts.value.filter((s) => {
    const worker = workerNameById(s.user_id).toLowerCase()
    return (
      (s.name || '').toLowerCase().includes(q) ||
      (s.abonent_id || '').toLowerCase().includes(q) ||
      (s.login || '').toLowerCase().includes(q) ||
      worker.includes(q)
    )
  })
})

const uniqueSipUserBindings = computed(() => {
  const seen = new Set<string>()
  return sipUserSelect.value.filter((su) => {
    const key = `${su.abonent_id || su.name}::${su.user_id ?? su.full_name}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
})

async function loadRefs() {
  loadingRefs.value = true
  try {
    const res = await requestJSON<RefItem[]>(`/api/v1/crm/refs/${activeRefKind.value}`)
    refItems.value = Array.isArray(res) ? res : []
  } catch {
    refItems.value = []
  } finally {
    loadingRefs.value = false
  }
}

async function createRefItem() {
  if (!newRefName.value.trim()) return
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/refs/${activeRefKind.value}`, {
      method: 'POST',
      body: JSON.stringify({ name: newRefName.value.trim() })
    })
    newRefName.value = ''
    await loadRefs()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось добавить запись'
  }
}

function openEditRefModal(item: RefItem) {
  editRefItem.value = { id: item.id, name: item.name }
  editRefModalOpen.value = true
}

async function saveRefItem() {
  if (!editRefItem.value || !editRefItem.value.name.trim()) return
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/refs/${activeRefKind.value}/${editRefItem.value.id}`, {
      method: 'PUT',
      body: JSON.stringify({ name: editRefItem.value.name.trim() })
    })
    editRefModalOpen.value = false
    await loadRefs()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось обновить запись'
  }
}

async function deleteRefItem(id: number) {
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/refs/${activeRefKind.value}/${id}`, { method: 'DELETE' })
    await loadRefs()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось удалить запись'
  }
}

async function loadSubdivisions() {
  loadingSubs.value = true
  try {
    const res = await requestJSON<Subdivision[]>('/api/v1/crm/subdivisions')
    subdivisions.value = Array.isArray(res) ? res : []
  } catch {
    subdivisions.value = []
  } finally {
    loadingSubs.value = false
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
  errorMsg.value = ''
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
  try {
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
    await loadSubdivisions()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось сохранить подразделение'
  }
}

async function deleteSubdivision(id: number) {
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/subdivisions/${id}`, { method: 'DELETE' })
    await loadSubdivisions()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось удалить подразделение'
  }
}

async function loadSip() {
  loadingSip.value = true
  try {
    const [sipRes, selRes] = await Promise.allSettled([
      requestJSON<SipAccount[]>('/api/v1/crm/sip-accounts'),
      requestJSON<SipUserSelectOption[]>('/api/v1/crm/sip-accounts/select-with-user-name')
    ])
    if (sipRes.status === 'fulfilled' && Array.isArray(sipRes.value)) sipAccounts.value = sipRes.value
    if (selRes.status === 'fulfilled' && Array.isArray(selRes.value)) sipUserSelect.value = selRes.value
  } finally {
    loadingSip.value = false
  }
}

function openSipModal(sip?: SipAccount) {
  sipForm.value = sip
    ? {
        id: sip.id,
        name: (sip.name || '').replace(/\s+/g, ' ').trim(),
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
  errorMsg.value = ''
  try {
    await requestJSON('/api/v1/crm/sip-accounts', {
      method: 'POST',
      body: JSON.stringify(sipForm.value)
    })
    sipModalOpen.value = false
    await loadSip()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения SIP-аккаунта'
  }
}

async function loadPool() {
  loadingPool.value = true
  try {
    const res = await requestJSON<TaskPoolSetting[]>('/api/v1/crm/pool-settings')
    poolSettings.value = Array.isArray(res) ? res : []
  } catch {
    poolSettings.value = []
  } finally {
    loadingPool.value = false
  }
}

function openPoolModal(ps?: TaskPoolSetting) {
  poolForm.value = ps
    ? {
        ...ps,
        task_types: [...(ps.task_types ?? [])]
      }
    : {
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
      }
  poolModalOpen.value = true
}

async function savePoolSetting() {
  errorMsg.value = ''
  try {
    await requestJSON('/api/v1/crm/pool-settings', {
      method: 'POST',
      body: JSON.stringify(poolForm.value)
    })
    poolModalOpen.value = false
    await loadPool()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения правила пула'
  }
}

async function runPoolJobs() {
  runningPool.value = true
  errorMsg.value = ''
  try {
    const res = await requestJSON<{
      quarterly_created: number
      prolongation_created: number
      pool_assigned: number
      overdue_flagged: number
    }>('/api/v1/crm/pool-settings/run', { method: 'POST' })
    okMsg.value = `Создано квартальных: ${res.quarterly_created}, пролонгаций: ${res.prolongation_created}, распределено по пулу: ${res.pool_assigned}, просрочено: ${res.overdue_flagged}`
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка запуска планировщика'
  } finally {
    runningPool.value = false
  }
}

const refColumns = computed<DataTableColumns<RefItem>>(() => [
  { title: 'ID', key: 'id', width: 80 },
  { title: 'Название', key: 'name' },
  {
    title: 'Действия',
    key: 'actions',
    width: 170,
    render: (r) =>
      h(NSpace, { size: 6, wrap: false }, () => [
        auth.hasPerm('crm.ref.manage') || auth.hasPerm(`crm.ref.${activeRefKind.value}.update`)
          ? h(NButton, { size: 'tiny', onClick: () => openEditRefModal(r) }, { default: () => 'Изм.' })
          : null,
        auth.hasPerm('crm.ref.manage') || auth.hasPerm(`crm.ref.${activeRefKind.value}.delete`)
          ? h(
              NPopconfirm,
              {
                positiveText: 'Удалить',
                negativeText: 'Отмена',
                onPositiveClick: () => void deleteRefItem(r.id)
              },
              {
                trigger: () => h(NButton, { size: 'tiny', type: 'error', ghost: true }, { default: () => 'Удалить' }),
                default: () => `Удалить «${r.name}»?`
              }
            )
          : null
      ])
  }
])

const sipColumns = computed<DataTableColumns<SipAccount>>(() => [
  {
    title: 'Название линии',
    key: 'name',
    minWidth: 180,
    render: (r) => (r.name || r.login || `Линия #${r.id}`).replace(/\s+/g, ' ').trim()
  },
  {
    title: 'ID абонента ВАТС',
    key: 'abonent_id',
    width: 155,
    render: (r) => r.abonent_id || '—'
  },
  {
    title: 'Сотрудник',
    key: 'user_id',
    minWidth: 190,
    render: (r) => workerNameById(r.user_id)
  },
  {
    title: 'Филиал',
    key: 'subdivision_id',
    width: 150,
    render: (r) => subdivisionNameById(r.subdivision_id)
  },
  {
    title: 'Статус',
    key: 'active',
    width: 105,
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: r.active ? 'success' : 'default', bordered: false },
        { default: () => (r.active ? 'Активен' : 'Отключён') }
      )
  },
  {
    title: 'Действия',
    key: 'act',
    width: 100,
    render: (r) =>
      auth.hasPerm('crm.sip.manage') || auth.hasPerm('crm.sip.update')
        ? h(NButton, { size: 'tiny', onClick: () => openSipModal(r) }, { default: () => 'Изм.' })
        : '—'
  }
])

const poolColumns = computed<DataTableColumns<TaskPoolSetting>>(() => [
  { title: 'Правило', key: 'name', minWidth: 200 },
  {
    title: 'Типы задач',
    key: 'task_types',
    minWidth: 220,
    render: (r) =>
      h(NSpace, { size: 4, wrap: true }, () =>
        (r.task_types?.length ? r.task_types : ['любые']).map((tt) =>
          h(
            NTag,
            { size: 'small', type: 'info', bordered: false },
            { default: () => (tt === 'любые' ? 'Любые' : taskTypeLabel(tt, enums.value)) }
          )
        )
      )
  },
  {
    title: 'Окно',
    key: 'window',
    width: 120,
    render: (r) => `${r.time_from} – ${r.time_to}`
  },
  { title: 'Приоритет', key: 'priority', width: 95, align: 'right' },
  { title: 'Макс. задач', key: 'tasks_max_count', width: 105, align: 'right' },
  {
    title: 'Кому',
    key: 'roles',
    width: 165,
    render: (r) =>
      [r.for_dispatcher ? 'Диспетчеры' : '', r.for_engineer ? 'Инженеры' : '', r.with_subdivision ? 'По филиалу' : '']
        .filter(Boolean)
        .join(', ') || '—'
  },
  {
    title: 'Статус',
    key: 'active',
    width: 105,
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: r.active ? 'success' : 'default', bordered: false },
        { default: () => (r.active ? 'Активно' : 'Пауза') }
      )
  },
  {
    title: 'Действия',
    key: 'act',
    width: 100,
    render: (r) =>
      auth.hasPerm('crm.pool.manage') || auth.hasPerm('crm.pool.update')
        ? h(NButton, { size: 'tiny', onClick: () => openPoolModal(r) }, { default: () => 'Изм.' })
        : '—'
  }
])

onMounted(async () => {
  if (typeof route.query.tab === 'string' && route.query.tab) {
    activeTab.value = route.query.tab
  }
  const [wRes, eRes] = await Promise.allSettled([
    requestJSON<UserBrief[]>('/api/v1/crm/workers'),
    requestJSON<CRMEnums>('/api/v1/crm/enums')
  ])
  if (wRes.status === 'fulfilled' && Array.isArray(wRes.value)) workers.value = wRes.value
  if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
  await Promise.all([loadRefs(), loadSubdivisions(), loadSip(), loadPool()])
})
</script>

<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">Справочники и настройки CRM</h2>
        <div class="page-meta">
          Оборудование, подразделения, SIP-линии, правила пула и каталоги CRM
        </div>
      </div>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>
    <NAlert v-if="okMsg" type="success" closable @close="okMsg = ''">{{ okMsg }}</NAlert>

    <NTabs v-model:value="activeTab" type="line">
      <NTabPane name="refs" :tab="`Справочники (${refItems.length})`">
        <NCard size="small" style="margin-bottom: 12px">
          <div class="filter-row">
            <NSelect
              v-model:value="activeRefKind"
              style="width: 300px"
              :options="[
                { label: 'Производители оборудования', value: 'manufacturer' },
                { label: 'Типы оборудования', value: 'equipment_type' },
                { label: 'Виды работ', value: 'work_type' },
                { label: 'Шаблоны особых отметок', value: 'special_note' }
              ]"
              @update:value="loadRefs"
            />
            <NInput
              v-model:value="refSearch"
              clearable
              placeholder="Поиск по названию или ID..."
              style="min-width: 220px; flex: 1 1 240px"
            />
            <NSpace align="center" :size="6">
              <NSwitch v-model:value="onlyShortRefs" size="small" />
              <span class="switch-label">Короткие (≤ 2 симв.)</span>
            </NSpace>
            <div v-if="auth.hasPerm('crm.ref.manage') || auth.hasPerm(`crm.ref.${activeRefKind}.create`)" class="add-inline">
              <NInput
                v-model:value="newRefName"
                placeholder="Новое значение..."
                style="width: 220px"
                @keyup.enter="createRefItem"
              />
              <NButton type="primary" @click="createRefItem">Добавить</NButton>
            </div>
          </div>
          <div class="sub-counter">Показано: {{ filteredRefs.length }} из {{ refItems.length }}</div>
        </NCard>

        <NCard size="small" :content-style="{ padding: 0 }">
          <NDataTable
            :columns="refColumns"
            :data="filteredRefs"
            :loading="loadingRefs"
            :row-key="(r: RefItem) => r.id"
            :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
            size="small"
          />
        </NCard>
      </NTabPane>

      <NTabPane name="subdivisions" :tab="`Подразделения (${subdivisions.length})`">
        <div class="section-bar">
          <span class="sub-counter">Филиалы, контакты, руководители и способы оплаты</span>
          <NButton
            v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.create')"
            type="primary"
            size="small"
            @click="openSubdivisionModal()"
          >
            + Подразделение
          </NButton>
        </div>

        <div class="sub-grid">
          <NCard v-for="s in subdivisions" :key="s.id" size="small" class="sub-card">
            <template #header>
              <span class="sub-name">{{ s.name }}</span>
            </template>
            <template #header-extra>
              <NSpace :size="4">
                <NButton
                  v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.update')"
                  size="tiny"
                  @click="openSubdivisionModal(s)"
                >
                  Изм.
                </NButton>
                <NPopconfirm
                  v-if="auth.hasPerm('crm.subdivision.manage') || auth.hasPerm('crm.subdivision.delete')"
                  positive-text="Удалить"
                  negative-text="Отмена"
                  @positive-click="deleteSubdivision(s.id)"
                >
                  <template #trigger>
                    <NButton size="tiny" type="error" ghost>Уд.</NButton>
                  </template>
                  Удалить подразделение «{{ s.name }}»?
                </NPopconfirm>
              </NSpace>
            </template>

            <div class="sub-meta">{{ s.address || 'Адрес не указан' }}</div>
            <div v-if="s.supervisor_user_id" class="sub-meta">
              Руководитель: <b>{{ workerNameById(s.supervisor_user_id) }}</b>
            </div>

            <div v-if="s.phones?.length" class="sub-phones">
              <div v-for="(p, idx) in s.phones" :key="idx" class="sub-phone-row">
                <span class="cell-num">{{ formatPhoneRU(p.phone) }}</span>
                <NSpace :size="4">
                  <NTag v-if="p.is_main" size="tiny" type="info" :bordered="false">Основной</NTag>
                  <NTag v-if="p.is_duty" size="tiny" type="warning" :bordered="false">Дежурный</NTag>
                </NSpace>
              </div>
            </div>

            <div v-if="s.payment_options?.length" class="sub-payments">
              <NSpace :size="4" wrap>
                <NTag
                  v-for="(po, pidx) in s.payment_options"
                  :key="pidx"
                  size="tiny"
                  :type="po.is_main ? 'success' : 'default'"
                  :bordered="false"
                >
                  {{ paymentOptionLabel(po, enums) }}<template v-if="po.file_url"> · QR</template>
                </NTag>
              </NSpace>
            </div>
          </NCard>
        </div>
      </NTabPane>

      <NTabPane name="sip" :tab="`SIP и ВАТС (${sipAccounts.length})`">
        <div class="section-bar">
          <NInput
            v-model:value="sipSearch"
            clearable
            placeholder="Поиск по линии, ID ВАТС или сотруднику..."
            style="max-width: 340px"
          />
          <NButton
            v-if="auth.hasPerm('crm.sip.manage') || auth.hasPerm('crm.sip.create')"
            type="primary"
            size="small"
            @click="openSipModal()"
          >
            + SIP-аккаунт
          </NButton>
        </div>

        <NCard size="small" title="SIP-аккаунты и линии ВАТС" :content-style="{ padding: 0 }" style="margin-bottom: 14px">
          <NDataTable
            :columns="sipColumns"
            :data="filteredSipAccounts"
            :loading="loadingSip"
            :row-key="(r: SipAccount) => r.id"
            :scroll-x="860"
            :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50] }"
            size="small"
          />
        </NCard>

        <NCard v-if="uniqueSipUserBindings.length" size="small" :title="`Привязка абонентов ВАТС (${uniqueSipUserBindings.length})`" :content-style="{ padding: 0 }">
          <NDataTable
            :data="uniqueSipUserBindings"
            :row-key="(r: SipUserSelectOption) => `${r.id}-${r.abonent_id}`"
            :pagination="{ pageSize: 15 }"
            size="small"
            :columns="[
              { title: 'Абонент ВАТС', key: 'abonent_id', width: 170, render: (r: SipUserSelectOption) => r.abonent_id || r.name },
              { title: 'Линия', key: 'name', render: (r: SipUserSelectOption) => (r.name || '—').replace(/\s+/g, ' ').trim() },
              { title: 'Сотрудник', key: 'full_name', render: (r: SipUserSelectOption) => r.full_name || `#${r.user_id}` }
            ]"
          />
        </NCard>
      </NTabPane>

      <NTabPane name="pool" :tab="`Пул задач (${poolSettings.length})`">
        <div class="section-bar">
          <span class="sub-counter">Правила автоматического распределения задач по диспетчерам и инженерам</span>
          <NSpace :size="8">
            <NButton
              v-if="auth.hasPerm('crm.pool.manage')"
              size="small"
              secondary
              :loading="runningPool"
              @click="runPoolJobs"
            >
              Запустить планировщик
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.pool.manage') || auth.hasPerm('crm.pool.update')"
              type="primary"
              size="small"
              @click="openPoolModal()"
            >
              + Правило пула
            </NButton>
          </NSpace>
        </div>

        <NCard size="small" :content-style="{ padding: 0 }">
          <NDataTable
            :columns="poolColumns"
            :data="poolSettings"
            :loading="loadingPool"
            :row-key="(r: TaskPoolSetting) => r.id"
            :scroll-x="980"
            size="small"
          />
        </NCard>
      </NTabPane>

      <NTabPane name="catalogs" tab="Источники, Анкеты и Баннеры">
        <CrmCatalogsPanel />
      </NTabPane>
    </NTabs>

    <NModal v-model:show="editRefModalOpen" preset="card" title="Редактировать запись справочника" style="width: 440px; max-width: 96vw">
      <NForm v-if="editRefItem" label-placement="top" size="small">
        <NFormItem label="Название *">
          <NInput v-model:value="editRefItem.name" @keyup.enter="saveRefItem" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="editRefModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveRefItem">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="subModalOpen"
      preset="card"
      :title="editSubId ? 'Редактировать подразделение' : 'Новое подразделение'"
      style="width: 620px; max-width: 96vw"
    >
      <NForm label-placement="top" size="small">
        <NFormItem label="Название *"><NInput v-model:value="subForm.name" /></NFormItem>
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
            <NButton size="tiny" dashed @click="subForm.phones.push({ phone: '', is_main: false, is_duty: false })">
              + Телефон
            </NButton>
          </NSpace>
        </NFormItem>
        <NFormItem label="Способы оплаты и QR-коды">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(po, idx) in subForm.payment_options" :key="idx" align="center" wrap>
              <NSelect v-model:value="po.payment_option_id" style="width: 195px" :options="paymentOptionsSelect" />
              <NCheckbox v-model:checked="po.is_main">Основной</NCheckbox>
              <label class="qr-upload">
                <input type="file" accept="image/*" style="display: none" @change="(e) => handleSubQrUpload(idx, e)" />
                {{ uploadingSubQrIdx === idx ? 'Загрузка...' : po.file_url ? 'Заменить QR' : 'Загрузить QR' }}
              </label>
              <a v-if="po.file_url" :href="po.file_url" target="_blank" style="font-size: 12px">Открыть QR</a>
              <NButton size="small" @click="subForm.payment_options.splice(idx, 1)">×</NButton>
            </NSpace>
            <NButton size="tiny" dashed @click="addSubPaymentOption">+ Способ оплаты</NButton>
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

    <NModal v-model:show="sipModalOpen" preset="card" title="SIP-аккаунт / линия ВАТС" style="width: 560px; max-width: 96vw">
      <NForm label-placement="top" size="small">
        <div class="form-grid-2">
          <NFormItem label="Название линии"><NInput v-model:value="sipForm.name" /></NFormItem>
          <NFormItem label="ID абонента / внутр. номер ВАТС"><NInput v-model:value="sipForm.abonent_id" /></NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Сотрудник">
            <NSelect
              v-model:value="sipForm.user_id"
              filterable
              clearable
              placeholder="Не привязан"
              :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
            />
          </NFormItem>
          <NFormItem label="Подразделение">
            <NSelect
              v-model:value="sipForm.subdivision_id"
              clearable
              placeholder="Не указано"
              :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
            />
          </NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="SIP-логин (опц.)"><NInput v-model:value="sipForm.login" /></NFormItem>
          <NFormItem label="SIP-пароль (опц.)"><NInput v-model:value="sipForm.password" type="password" /></NFormItem>
        </div>
        <div class="form-grid-3">
          <NFormItem label="Домен (опц.)"><NInput v-model:value="sipForm.domain" /></NFormItem>
          <NFormItem label="WSS-сервер (опц.)"><NInput v-model:value="sipForm.server" /></NFormItem>
          <NFormItem label="Порт"><NInputNumber v-model:value="sipForm.port" style="width: 100%" /></NFormItem>
        </div>
        <NFormItem label="Активен"><NSwitch v-model:value="sipForm.active" /></NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="sipModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveSipAccount">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="poolModalOpen" preset="card" :title="poolForm.id ? 'Редактировать правило пула' : 'Новое правило пула'" style="width: 540px; max-width: 96vw">
      <NForm label-placement="top" size="small">
        <NFormItem label="Название *"><NInput v-model:value="poolForm.name" /></NFormItem>
        <NFormItem label="Типы задач">
          <NSelect
            v-model:value="poolForm.task_types"
            multiple
            :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))"
          />
        </NFormItem>
        <div class="form-grid-2">
          <NFormItem label="Окно с"><NInput v-model:value="poolForm.time_from" placeholder="08:00" /></NFormItem>
          <NFormItem label="по"><NInput v-model:value="poolForm.time_to" placeholder="20:00" /></NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Приоритет"><NInputNumber v-model:value="poolForm.priority" style="width: 100%" /></NFormItem>
          <NFormItem label="Макс. активных задач"><NInputNumber v-model:value="poolForm.tasks_max_count" style="width: 100%" /></NFormItem>
        </div>
        <NSpace :size="14" wrap>
          <NCheckbox v-model:checked="poolForm.for_dispatcher">Диспетчерам</NCheckbox>
          <NCheckbox v-model:checked="poolForm.for_engineer">Инженерам</NCheckbox>
          <NCheckbox v-model:checked="poolForm.with_subdivision">По филиалу</NCheckbox>
          <NCheckbox v-model:checked="poolForm.active">Активно</NCheckbox>
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
.add-inline {
  display: flex;
  gap: 8px;
  align-items: center;
}
.switch-label {
  font-size: 13px;
  color: #334155;
}
.sub-counter {
  font-size: 12px;
  color: #64748b;
  margin-top: 8px;
}
.section-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.sub-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 12px;
}
.sub-card {
  height: 100%;
}
.sub-name {
  font-weight: 600;
}
.sub-meta {
  font-size: 12px;
  color: #64748b;
  margin-bottom: 4px;
}
.sub-phones {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #f1f5f9;
}
.sub-phone-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
}
.sub-payments {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #f1f5f9;
}
.cell-num {
  font-variant-numeric: tabular-nums;
}
.qr-upload {
  cursor: pointer;
  font-size: 12px;
  color: #2563eb;
}
.form-grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.form-grid-3 {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 640px) {
  .form-grid-2,
  .form-grid-3 {
    grid-template-columns: 1fr;
  }
}
</style>
