<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAlert,
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
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { taskStatusLabel } from '@/shared/utils/format'
import { formatDateParts, taskStatusTagType, taskTypeLabel, taskTypeTagType } from './crmLabels'
import { useAuthStore } from '@/stores/auth'
import type { CRMTask, Contractor, Subdivision, UserBrief, CRMEnums } from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const runningPool = ref(false)
const errorMsg = ref('')
const jobSummary = ref('')

const tasks = ref<CRMTask[]>([])
const searchQuery = ref('')
const statusFilter = ref<number | null>(null)
const typeFilter = ref<string | null>(null)
const assigneeFilter = ref<number | null>(null)
const subdivisionFilter = ref<number | null>(null)
const onlyUrgent = ref(false)

const workers = ref<UserBrief[]>([])
const subdivisions = ref<Subdivision[]>([])
const contractors = ref<Contractor[]>([])
const searchingContractors = ref(false)

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
  task_statuses: [
    { id: 1, label: 'Новая' },
    { id: 2, label: 'В работе' },
    { id: 3, label: 'Выполнена' },
    { id: 4, label: 'Недозвон' },
    { id: 5, label: 'Провалена' },
    { id: 6, label: 'Отменена' }
  ],
  task_results: [
    { id: 1, key: 'answered', label: 'Дозвонились / Выполнено' },
    { id: 2, key: 'no_answer', label: 'Недозвон' },
    { id: 3, key: 'application_created', label: 'Создана заявка' },
    { id: 4, key: 'refused', label: 'Отказ' }
  ]
})

const taskModalOpen = ref(false)
const editTaskId = ref<number | null>(null)
const savingTask = ref(false)
const taskForm = ref({
  contractor_id: null as number | null,
  task_type: 'call',
  title: '',
  description: '',
  status: 1,
  priority: 1,
  assigned_user_id: null as number | null,
  plan_datetime_ms: Date.now() + 3600000 as number | null,
  is_urgent: false,
  note: ''
})

const executeModalOpen = ref(false)
const executeTaskId = ref<number | null>(null)
const executeTaskTitle = ref('')
const savingExecute = ref(false)
const executeForm = ref({
  task_result: 'answered',
  note: '',
  new_plan_datetime_ms: Date.now() + 4 * 3600000 as number | null
})

const taskResultOptions = computed(() =>
  (enums.value.task_results ?? []).map((r) => ({ label: r.label, value: r.key }))
)

const hasActiveFilters = computed(
  () =>
    Boolean(searchQuery.value.trim()) ||
    statusFilter.value !== null ||
    typeFilter.value !== null ||
    assigneeFilter.value !== null ||
    subdivisionFilter.value !== null ||
    onlyUrgent.value
)

const filteredTasks = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return tasks.value
  return tasks.value.filter(
    (t) =>
      String(t.id).includes(q) ||
      (t.title || '').toLowerCase().includes(q) ||
      (t.contractor_name || '').toLowerCase().includes(q) ||
      (t.assigned_name || '').toLowerCase().includes(q)
  )
})

const urgentCount = computed(() => tasks.value.filter((t) => t.is_urgent && isTaskActive(t.status)).length)
const activeCount = computed(() => tasks.value.filter((t) => isTaskActive(t.status)).length)

function isTaskActive(status: number): boolean {
  return status === 1 || status === 2 || status === 4
}

function isTaskOverdue(t: CRMTask): boolean {
  if (!isTaskActive(t.status) || t.missing_deadline || !t.plan_datetime_to) return false
  const ms = new Date(t.plan_datetime_to).getTime()
  return !Number.isNaN(ms) && ms < Date.now()
}

function buildTasksQS(): string {
  const params = new URLSearchParams({ limit: '200' })
  if (searchQuery.value.trim()) params.set('q', searchQuery.value.trim())
  if (onlyUrgent.value) params.set('urgent', 'true')
  if (statusFilter.value !== null) params.set('status', String(statusFilter.value))
  if (typeFilter.value) params.set('task_type', typeFilter.value)
  if (assigneeFilter.value !== null) params.set('assigned_user_id', String(assigneeFilter.value))
  if (subdivisionFilter.value !== null) params.set('subdivision_id', String(subdivisionFilter.value))
  return params.toString()
}

async function loadTasks() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await requestJSON<CRMTask[]>(`/api/v1/crm/tasks?${buildTasksQS()}`)
    tasks.value = Array.isArray(res) ? res : []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить задачи'
  } finally {
    loading.value = false
  }
}

async function loadMeta() {
  const [eRes, wRes, sRes, cRes] = await Promise.allSettled([
    requestJSON<CRMEnums>('/api/v1/crm/enums'),
    requestJSON<UserBrief[]>('/api/v1/crm/workers'),
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
    requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100')
  ])
  if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
  if (wRes.status === 'fulfilled' && Array.isArray(wRes.value)) workers.value = wRes.value
  if (sRes.status === 'fulfilled' && Array.isArray(sRes.value)) subdivisions.value = sRes.value
  if (cRes.status === 'fulfilled') contractors.value = cRes.value?.items ?? []
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
  searchQuery.value = ''
  statusFilter.value = null
  typeFilter.value = null
  assigneeFilter.value = null
  subdivisionFilter.value = null
  onlyUrgent.value = false
  void loadTasks()
}

function openTaskModal(t?: CRMTask) {
  editTaskId.value = t?.id ?? null
  const deadlineMs = t && !t.missing_deadline && t.plan_datetime_to ? new Date(t.plan_datetime_to).getTime() : null
  taskForm.value = t
    ? {
        contractor_id: t.contractor_id ?? null,
        task_type: t.task_type,
        title: t.title,
        description: t.description,
        status: t.status,
        priority: t.priority,
        assigned_user_id: t.assigned_user_id ?? null,
        plan_datetime_ms: deadlineMs && !Number.isNaN(deadlineMs) ? deadlineMs : null,
        is_urgent: t.is_urgent,
        note: t.note
      }
    : {
        contractor_id: null,
        task_type: 'call',
        title: '',
        description: '',
        status: 1,
        priority: 1,
        assigned_user_id: null,
        plan_datetime_ms: Date.now() + 3600000,
        is_urgent: false,
        note: ''
      }
  taskModalOpen.value = true
}

async function saveTask() {
  if (!taskForm.value.title.trim()) {
    errorMsg.value = 'Укажите заголовок задачи'
    return
  }
  savingTask.value = true
  errorMsg.value = ''
  try {
    const payload = {
      contractor_id: taskForm.value.contractor_id,
      task_type: taskForm.value.task_type,
      title: taskForm.value.title.trim(),
      description: taskForm.value.description,
      status: taskForm.value.status,
      priority: taskForm.value.priority,
      assigned_user_id: taskForm.value.assigned_user_id,
      plan_datetime_to: taskForm.value.plan_datetime_ms
        ? new Date(taskForm.value.plan_datetime_ms).toISOString()
        : new Date(Date.now() + 3600000).toISOString(),
      is_urgent: taskForm.value.is_urgent,
      note: taskForm.value.note
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
    await loadTasks()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения задачи'
  } finally {
    savingTask.value = false
  }
}

function openExecuteTaskModal(t: CRMTask) {
  executeTaskId.value = t.id
  executeTaskTitle.value = t.title
  executeForm.value = {
    task_result: t.task_result || 'answered',
    note: '',
    new_plan_datetime_ms: Date.now() + 4 * 3600000
  }
  executeModalOpen.value = true
}

async function submitExecuteTask() {
  if (!executeTaskId.value) return
  savingExecute.value = true
  errorMsg.value = ''
  try {
    const body: Record<string, unknown> = {
      task_result: executeForm.value.task_result,
      note: executeForm.value.note
    }
    if (executeForm.value.task_result === 'no_answer' && executeForm.value.new_plan_datetime_ms) {
      body.new_plan_datetime_to = new Date(executeForm.value.new_plan_datetime_ms).toISOString()
    }
    await requestJSON(`/api/v1/crm/tasks/${executeTaskId.value}/execute`, {
      method: 'POST',
      body: JSON.stringify(body)
    })
    executeModalOpen.value = false
    await loadTasks()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка выполнения задачи'
  } finally {
    savingExecute.value = false
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
    jobSummary.value = `Создано квартальных: ${res.quarterly_created}, пролонгаций: ${res.prolongation_created}, распределено по пулу: ${res.pool_assigned}, просрочено: ${res.overdue_flagged}`
    await loadTasks()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка запуска планировщика задач'
  } finally {
    runningPool.value = false
  }
}

const columns = computed<DataTableColumns<CRMTask>>(() => [
  {
    title: 'ID',
    key: 'id',
    width: 82,
    render: (r) => h('span', { class: 'cell-id' }, `#${r.id}`)
  },
  {
    title: 'Задача',
    key: 'title',
    minWidth: 230,
    render: (r) =>
      h('div', { class: 'task-cell' }, [
        h('div', { class: 'task-badges' }, [
          r.is_urgent ? h(NTag, { type: 'error', size: 'tiny' }, { default: () => 'Срочно' }) : null,
          r.prev_task_id ? h(NTag, { type: 'warning', size: 'tiny' }, { default: () => `Повтор #${r.prev_task_id}` }) : null
        ]),
        h('div', { class: 'task-title' }, r.title || 'Без заголовка'),
        r.note ? h('div', { class: 'cell-muted-line' }, r.note) : null
      ])
  },
  {
    title: 'Тип',
    key: 'task_type',
    width: 170,
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: taskTypeTagType(r.task_type), bordered: false },
        { default: () => taskTypeLabel(r.task_type, enums.value) }
      )
  },
  {
    title: 'Контрагент',
    key: 'contractor_name',
    minWidth: 190,
    ellipsis: { tooltip: true },
    render: (r) => r.contractor_name || h('span', { class: 'cell-muted' }, '—')
  },
  {
    title: 'Исполнитель',
    key: 'assigned_name',
    width: 185,
    ellipsis: { tooltip: true },
    render: (r) => r.assigned_name || h('span', { class: 'cell-muted' }, 'В пуле (не назначен)')
  },
  {
    title: 'Дедлайн',
    key: 'plan_datetime_to',
    width: 120,
    render: (r) => {
      if (r.missing_deadline) return h('span', { class: 'cell-muted' }, 'Не указан')
      const parts = formatDateParts(r.plan_datetime_to)
      if (!parts) return '—'
      const overdue = isTaskOverdue(r)
      return h('div', { class: overdue ? 'dt-cell dt-overdue' : 'dt-cell' }, [
        h('span', parts.date),
        h('span', { class: 'dt-time' }, parts.time)
      ])
    }
  },
  {
    title: 'Статус',
    key: 'status',
    width: 120,
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: taskStatusTagType(r.status), bordered: false },
        { default: () => taskStatusLabel(r.status) }
      )
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 165,
    fixed: 'right',
    render: (r) =>
      h(NSpace, { size: 6, wrap: false }, () => [
        auth.hasPerm('crm.task.execute') && isTaskActive(r.status)
          ? h(
              NButton,
              { size: 'tiny', type: 'primary', secondary: true, onClick: () => openExecuteTaskModal(r) },
              { default: () => 'Выполнить' }
            )
          : null,
        auth.hasPerm('crm.task.update')
          ? h(NButton, { size: 'tiny', onClick: () => openTaskModal(r) }, { default: () => 'Изм.' })
          : null
      ])
  }
])

onMounted(async () => {
  if (route.query.urgent === '1' || route.query.urgent === 'true') {
    onlyUrgent.value = true
  }
  await Promise.all([loadMeta(), loadTasks()])
})
</script>

<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">Задачи</h2>
        <div class="page-meta">
          <span>Показано: <b>{{ filteredTasks.length }}</b></span>
          <span class="meta-sep">· Активных: <b>{{ activeCount }}</b></span>
          <span v-if="urgentCount > 0" class="meta-danger">· Срочных: <b>{{ urgentCount }}</b></span>
        </div>
      </div>
      <NSpace :size="8" wrap>
        <NButton
          v-if="auth.hasPerm('crm.pool.manage') || auth.hasPerm('crm.pool.view')"
          size="small"
          tertiary
          @click="router.push({ path: '/crm/settings', query: { tab: 'pool' } })"
        >
          Правила пула
        </NButton>
        <NButton
          v-if="auth.hasPerm('crm.pool.manage')"
          size="small"
          secondary
          :loading="runningPool"
          @click="runPoolJobs"
        >
          Запустить планировщик
        </NButton>
        <NButton v-if="auth.hasPerm('crm.task.create')" type="primary" size="small" @click="openTaskModal()">
          + Новая задача
        </NButton>
      </NSpace>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>
    <NAlert v-if="jobSummary" type="success" closable @close="jobSummary = ''">{{ jobSummary }}</NAlert>

    <NCard size="small">
      <div class="filter-row">
        <NInput
          v-model:value="searchQuery"
          clearable
          placeholder="Поиск по заголовку, ID, контрагенту..."
          style="min-width: 220px; flex: 1 1 240px"
          @keyup.enter="loadTasks"
          @clear="loadTasks"
        />
        <NSelect
          v-model:value="statusFilter"
          clearable
          placeholder="Статус"
          style="width: 150px"
          :options="enums.task_statuses.map((s) => ({ label: s.label, value: s.id }))"
          @update:value="loadTasks"
        />
        <NSelect
          v-model:value="typeFilter"
          clearable
          placeholder="Тип задачи"
          style="width: 205px"
          :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))"
          @update:value="loadTasks"
        />
        <NSelect
          v-model:value="assigneeFilter"
          clearable
          filterable
          placeholder="Исполнитель"
          style="width: 200px"
          :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
          @update:value="loadTasks"
        />
        <NSelect
          v-model:value="subdivisionFilter"
          clearable
          placeholder="Филиал"
          style="width: 155px"
          :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
          @update:value="loadTasks"
        />
        <NButton
          size="medium"
          :type="onlyUrgent ? 'error' : 'default'"
          :secondary="!onlyUrgent"
          @click="
            () => {
              onlyUrgent = !onlyUrgent
              loadTasks()
            }
          "
        >
          Только срочные
        </NButton>
        <NButton v-if="hasActiveFilters" size="medium" tertiary @click="resetFilters">Сбросить</NButton>
      </div>
    </NCard>

    <NCard size="small" :content-style="{ padding: 0 }">
      <NDataTable
        :columns="columns"
        :data="filteredTasks"
        :loading="loading"
        :row-key="(r: CRMTask) => r.id"
        :scroll-x="1180"
        :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
        size="small"
      />
    </NCard>

    <NModal
      v-model:show="taskModalOpen"
      preset="card"
      :title="editTaskId ? 'Редактировать задачу' : 'Новая задача'"
      style="width: 580px; max-width: 96vw"
    >
      <NForm label-placement="top" size="small">
        <NFormItem label="Заголовок *">
          <NInput v-model:value="taskForm.title" placeholder="Например: Позвонить по продлению договора ТО" />
        </NFormItem>
        <NFormItem label="Контрагент">
          <NSelect
            v-model:value="taskForm.contractor_id"
            filterable
            remote
            clearable
            :loading="searchingContractors"
            placeholder="Выберите или найдите контрагента (необязательно)"
            :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
            @search="searchContractorsRemote"
          />
        </NFormItem>
        <div class="form-grid-2">
          <NFormItem label="Тип задачи">
            <NSelect
              v-model:value="taskForm.task_type"
              :options="enums.task_types.map((t) => ({ label: t.label, value: t.id }))"
            />
          </NFormItem>
          <NFormItem label="Статус">
            <NSelect
              v-model:value="taskForm.status"
              :options="enums.task_statuses.map((s) => ({ label: s.label, value: s.id }))"
            />
          </NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Исполнитель">
            <NSelect
              v-model:value="taskForm.assigned_user_id"
              filterable
              clearable
              placeholder="Не назначен (попадёт в пул)"
              :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
            />
          </NFormItem>
          <NFormItem label="Дедлайн">
            <NDatePicker
              v-model:value="taskForm.plan_datetime_ms"
              type="datetime"
              clearable
              format="dd.MM.yyyy HH:mm"
              style="width: 100%"
            />
          </NFormItem>
        </div>
        <NFormItem label="Срочная задача">
          <NSwitch v-model:value="taskForm.is_urgent" />
        </NFormItem>
        <NFormItem label="Описание / Примечание">
          <NInput
            v-model:value="taskForm.note"
            type="textarea"
            :rows="2"
            placeholder="При статусе «Недозвон» задача закроется и создаст повторную через 2 ч"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="taskModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingTask" @click="saveTask">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="executeModalOpen" preset="card" title="Выполнение задачи" style="width: 480px; max-width: 96vw">
      <div v-if="executeTaskTitle" class="modal-subtitle">{{ executeTaskTitle }}</div>
      <NForm label-placement="top" size="small">
        <NFormItem label="Результат звонка / задачи">
          <NSelect v-model:value="executeForm.task_result" :options="taskResultOptions" />
        </NFormItem>
        <NFormItem v-if="executeForm.task_result === 'no_answer'" label="Срок повторного звонка">
          <NDatePicker
            v-model:value="executeForm.new_plan_datetime_ms"
            type="datetime"
            format="dd.MM.yyyy HH:mm"
            style="width: 100%"
          />
        </NFormItem>
        <NFormItem label="Комментарий к результату">
          <NInput v-model:value="executeForm.note" type="textarea" :rows="3" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="executeModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingExecute" @click="submitExecuteTask">Зафиксировать результат</NButton>
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
.meta-sep {
  margin-left: 6px;
}
.meta-danger {
  color: #dc2626;
  margin-left: 6px;
}
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.cell-id {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  color: #64748b;
  font-size: 12px;
}
.task-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.task-badges {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.task-title {
  font-weight: 500;
  color: #0f172a;
  word-break: break-word;
}
.cell-muted {
  color: #94a3b8;
}
.cell-muted-line {
  font-size: 12px;
  color: #64748b;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
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
.dt-overdue {
  color: #dc2626;
  font-weight: 600;
}
.dt-overdue .dt-time {
  color: #ef4444;
}
.modal-subtitle {
  font-size: 13px;
  color: #475569;
  margin-bottom: 12px;
  padding: 8px 10px;
  background: #f8fafc;
  border-radius: 6px;
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
