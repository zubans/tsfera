<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRoute } from 'vue-router'
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDatePicker,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NTag,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { downloadFile, openPrintTab } from '@/shared/utils/download'
import {
  formatDateOnly,
  formatDateRangeParam,
  formatMoneyRUB,
  documentStatusLabel,
  toDateInput,
  dateInputToRFC3339
} from '@/shared/utils/format'
import { documentStatusTagType, isSuspiciousFutureYear } from './crmLabels'
import { useAuthStore } from '@/stores/auth'
import type {
  MaintenanceContract,
  ContractEquipment,
  Contractor,
  ContractorAddress,
  Equipment,
  CRMEnums
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()

const loading = ref(false)
const errorMsg = ref('')

const contracts = ref<MaintenanceContract[]>([])
const searchQuery = ref('')
const statusFilter = ref<number | null>(null)
const prolongationFilter = ref<'all' | 'prolongation'>('all')
const dateRange = ref<[number, number] | null>(null)

const contractors = ref<Contractor[]>([])
const searchingContractors = ref(false)
const equipmentsCatalog = ref<Equipment[]>([])
const modalAddresses = ref<ContractorAddress[]>([])

const enums = ref<CRMEnums>({
  work_types: [],
  document_statuses: [
    { id: 1, label: 'Активна' },
    { id: 2, label: 'Проведена' },
    { id: 3, label: 'Провалена' },
    { id: 4, label: 'Отменена' }
  ],
  task_types: [],
  task_statuses: [],
  contract_categories: [
    { id: 1, key: 'standard', label: 'Стандарт' },
    { id: 2, key: 'plus', label: 'Плюс' },
    { id: 3, key: 'all_inclusive', label: 'Всё включено' },
    { id: 4, key: 'economy', label: 'Эконом' }
  ]
})

const contractModalOpen = ref(false)
const editContractId = ref<number | null>(null)
const savingContract = ref(false)
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
const savingRenew = ref(false)
const renewForm = ref({
  number: '',
  category: 'standard',
  price: 0,
  valid_from: '',
  valid_to: '',
  note: ''
})

const contractCategoryOptions = computed(() =>
  (enums.value.contract_categories ?? []).map((c) => ({ label: c.label, value: c.key }))
)

const statusOptions = computed(() =>
  (enums.value.document_statuses ?? []).map((s) => ({ label: s.label, value: s.id }))
)

const hasActiveFilters = computed(
  () =>
    Boolean(searchQuery.value.trim()) ||
    statusFilter.value !== null ||
    prolongationFilter.value !== 'all' ||
    dateRange.value !== null
)

// Клиентский фолбэк-фильтр гарантирует мгновенный поиск даже без задержки сети.
const filteredContracts = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return contracts.value
  return contracts.value.filter(
    (c) =>
      (c.number || '').toLowerCase().includes(q) ||
      (c.contractor_name || '').toLowerCase().includes(q) ||
      (c.address_text || '').toLowerCase().includes(q)
  )
})

const prolongationCount = computed(() => contracts.value.filter((c) => c.needs_prolongation).length)

function contractCategoryLabel(key?: string): string {
  return enums.value.contract_categories?.find((c) => c.key === key)?.label || key || '—'
}

function buildContractsQS(extra?: Record<string, string>): string {
  const params = new URLSearchParams({ limit: '200' })
  if (searchQuery.value.trim()) params.set('q', searchQuery.value.trim())
  if (prolongationFilter.value === 'prolongation') params.set('only_needing_prolongation', 'true')
  if (statusFilter.value !== null) params.set('status', String(statusFilter.value))
  const rangeStr = formatDateRangeParam(dateRange.value)
  if (rangeStr) params.set('range', rangeStr)
  if (extra) {
    for (const [k, v] of Object.entries(extra)) params.set(k, v)
  }
  return params.toString()
}

async function loadContracts() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await requestJSON<MaintenanceContract[]>(`/api/v1/crm/contracts?${buildContractsQS()}`)
    contracts.value = Array.isArray(res) ? res : []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить договоры ТО'
  } finally {
    loading.value = false
  }
}

async function loadMeta() {
  const [eRes, ctrRes, eqRes] = await Promise.allSettled([
    requestJSON<CRMEnums>('/api/v1/crm/enums'),
    requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100'),
    requestJSON<Equipment[]>('/api/v1/crm/equipments?limit=200')
  ])
  if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
  if (ctrRes.status === 'fulfilled') contractors.value = ctrRes.value?.items ?? []
  if (eqRes.status === 'fulfilled' && Array.isArray(eqRes.value)) equipmentsCatalog.value = eqRes.value
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

async function loadContractorAddresses(contractorId: number | null) {
  if (!contractorId) {
    modalAddresses.value = []
    return
  }
  try {
    modalAddresses.value =
      (await requestJSON<ContractorAddress[]>(
        `/api/v1/crm/addresses?contractor_id=${contractorId}&limit=50`
      )) ?? []
  } catch {
    modalAddresses.value = []
  }
}

function resetFilters() {
  searchQuery.value = ''
  statusFilter.value = null
  prolongationFilter.value = 'all'
  dateRange.value = null
  void loadContracts()
}

async function openContractModal(c?: MaintenanceContract, prefill?: { contractorId?: number; addressId?: number }) {
  editContractId.value = c?.id ?? null
  const cid = c?.contractor_id ?? prefill?.contractorId ?? null
  if (cid && !contractors.value.some((item) => item.id === cid)) {
    try {
      const found = await requestJSON<Contractor>(`/api/v1/crm/contractors/${cid}`)
      if (found) contractors.value = [found, ...contractors.value]
    } catch {
      // Если контрагент недоступен, оставляем текущий список.
    }
  }
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
        address_id: prefill?.addressId ?? (cid ? modalAddresses.value[0]?.id ?? null : null),
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

async function handleContractorChange(val: number | null) {
  contractForm.value.address_id = null
  await loadContractorAddresses(val)
  if (modalAddresses.value.length === 1) {
    contractForm.value.address_id = modalAddresses.value[0]!.id
  }
}

async function saveContract() {
  if (!contractForm.value.contractor_id || !contractForm.value.address_id) {
    errorMsg.value = 'Выберите контрагента и адрес объекта'
    return
  }
  savingContract.value = true
  errorMsg.value = ''
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
    await loadContracts()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения договора'
  } finally {
    savingContract.value = false
  }
}

function openRenewModal(c: MaintenanceContract) {
  renewContractId.value = c.id
  const oldTo = new Date(c.valid_to)
  const baseValid = Number.isNaN(oldTo.getTime()) || isSuspiciousFutureYear(c.valid_to) ? new Date() : oldTo
  const nextFrom = new Date(baseValid.getTime() + 86400000)
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
  savingRenew.value = true
  errorMsg.value = ''
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
    await loadContracts()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка пролонгации договора'
  } finally {
    savingRenew.value = false
  }
}

async function runExport(format: 'xlsx' | 'csv') {
  try {
    await downloadFile(
      `/api/v1/crm/contracts/export?${buildContractsQS({ format })}`,
      `contracts-${new Date().toISOString().slice(0, 10)}.${format}`
    )
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : 'Не удалось выгрузить файл'
  }
}

const columns = computed<DataTableColumns<MaintenanceContract>>(() => [
  {
    title: '№ договора',
    key: 'number',
    width: 130,
    render: (r) => h('span', { class: 'cell-strong' }, r.number || `#${r.id}`)
  },
  {
    title: 'Тариф',
    key: 'category',
    width: 135,
    render: (r) => h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => contractCategoryLabel(r.category) })
  },
  {
    title: 'Контрагент',
    key: 'contractor_name',
    minWidth: 190,
    ellipsis: { tooltip: true }
  },
  {
    title: 'Адрес объекта',
    key: 'address_text',
    minWidth: 230,
    ellipsis: { tooltip: true }
  },
  {
    title: 'Сумма',
    key: 'price',
    width: 125,
    align: 'right',
    render: (r) => h('span', { class: 'cell-num' }, formatMoneyRUB(r.price))
  },
  {
    title: 'Срок действия',
    key: 'valid_to',
    width: 195,
    render: (r) => {
      const badDate = isSuspiciousFutureYear(r.valid_to) || isSuspiciousFutureYear(r.valid_from)
      return h('div', { class: 'cell-stack' }, [
        h('span', { class: 'cell-nowrap' }, `${formatDateOnly(r.valid_from)} — ${formatDateOnly(r.valid_to)}`),
        badDate
          ? h(NTag, { size: 'tiny', type: 'warning', bordered: false }, { default: () => 'Проверьте год' })
          : null
      ])
    }
  },
  {
    title: 'Статус',
    key: 'status',
    width: 145,
    render: (r) =>
      h(NSpace, { size: 4, wrap: false }, () => [
        h(
          NTag,
          { size: 'small', type: documentStatusTagType(r.status), bordered: false },
          { default: () => documentStatusLabel(r.status) }
        ),
        r.needs_prolongation
          ? h(NTag, { size: 'tiny', type: 'warning' }, { default: () => 'Продлить' })
          : null
      ])
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 215,
    fixed: 'right',
    render: (r) =>
      h(NSpace, { size: 6, wrap: false }, () => [
        auth.hasPerm('crm.contract.update')
          ? h(NButton, { size: 'tiny', onClick: () => void openContractModal(r) }, { default: () => 'Изм.' })
          : null,
        auth.hasPerm('crm.contract.create')
          ? h(
              NButton,
              { size: 'tiny', type: 'primary', secondary: true, onClick: () => openRenewModal(r) },
              { default: () => 'Продлить' }
            )
          : null,
        h(
          NButton,
          { size: 'tiny', secondary: true, onClick: () => openPrintTab(`/api/v1/crm/contracts/${r.id}/print`) },
          { default: () => 'Печать' }
        )
      ])
  }
])

onMounted(async () => {
  await Promise.all([loadMeta(), loadContracts()])
  if (route.query.contractorId && auth.hasPerm('crm.contract.create')) {
    await openContractModal(undefined, {
      contractorId: Number(route.query.contractorId),
      addressId: route.query.addressId ? Number(route.query.addressId) : undefined
    })
  }
})
</script>

<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">Договоры ТО</h2>
        <div class="page-meta">
          <span>Показано: <b>{{ filteredContracts.length }}</b></span>
          <span v-if="prolongationCount > 0" class="meta-warn">· Требуют продления: <b>{{ prolongationCount }}</b></span>
        </div>
      </div>
      <NSpace :size="8" wrap>
        <NButton v-if="auth.hasPerm('crm.contract.export')" size="small" secondary @click="runExport('xlsx')">
          Экспорт XLSX
        </NButton>
        <NButton v-if="auth.hasPerm('crm.contract.export')" size="small" secondary @click="runExport('csv')">
          Экспорт CSV
        </NButton>
        <NButton v-if="auth.hasPerm('crm.contract.create')" type="primary" size="small" @click="openContractModal()">
          + Новый договор
        </NButton>
      </NSpace>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NCard size="small" class="filter-card">
      <div class="filter-row">
        <NInput
          v-model:value="searchQuery"
          clearable
          placeholder="Поиск по № договора, контрагенту, адресу..."
          style="min-width: 240px; flex: 1 1 260px"
          @keyup.enter="loadContracts"
          @clear="loadContracts"
        />
        <NSelect
          v-model:value="prolongationFilter"
          style="width: 215px"
          :options="[
            { label: 'Все договоры', value: 'all' },
            { label: 'Требуют пролонгации', value: 'prolongation' }
          ]"
          @update:value="loadContracts"
        />
        <NSelect
          v-model:value="statusFilter"
          clearable
          placeholder="Статус"
          style="width: 155px"
          :options="statusOptions"
          @update:value="loadContracts"
        />
        <NDatePicker
          v-model:value="dateRange"
          type="daterange"
          clearable
          format="dd.MM.yyyy"
          start-placeholder="Действует с"
          end-placeholder="по"
          style="width: 260px"
          @update:value="loadContracts"
        />
        <NButton size="medium" type="primary" secondary @click="loadContracts">Найти</NButton>
        <NButton v-if="hasActiveFilters" size="medium" tertiary @click="resetFilters">Сбросить</NButton>
      </div>
    </NCard>

    <NCard size="small" :content-style="{ padding: 0 }">
      <NDataTable
        :columns="columns"
        :data="filteredContracts"
        :loading="loading"
        :row-key="(r: MaintenanceContract) => r.id"
        :scroll-x="1200"
        :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
        size="small"
      />
    </NCard>

    <NModal
      v-model:show="contractModalOpen"
      preset="card"
      :title="editContractId ? 'Редактировать договор ТО' : 'Новый договор ТО'"
      style="width: 600px; max-width: 96vw"
    >
      <NForm label-placement="top" size="small">
        <NFormItem label="Контрагент *">
          <NSelect
            v-model:value="contractForm.contractor_id"
            filterable
            remote
            clearable
            :loading="searchingContractors"
            placeholder="Начните вводить ФИО или название..."
            :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
            @search="searchContractorsRemote"
            @update:value="handleContractorChange"
          />
        </NFormItem>
        <NFormItem label="Адрес объекта *">
          <NSelect
            v-model:value="contractForm.address_id"
            clearable
            :disabled="!contractForm.contractor_id"
            :placeholder="contractForm.contractor_id ? 'Выберите адрес' : 'Сначала выберите контрагента'"
            :options="modalAddresses.map((a) => ({ label: a.address, value: a.id }))"
          />
        </NFormItem>
        <div class="form-grid-2">
          <NFormItem label="Номер договора">
            <NInput v-model:value="contractForm.number" placeholder="Например: 125-26" />
          </NFormItem>
          <NFormItem label="Стоимость (₽)">
            <NInputNumber v-model:value="contractForm.price" :min="0" style="width: 100%" />
          </NFormItem>
        </div>
        <div class="form-grid-3">
          <NFormItem label="Подписан">
            <NDatePicker
              v-model:formatted-value="contractForm.signed_at"
              type="date"
              value-format="yyyy-MM-dd"
              format="dd.MM.yyyy"
              style="width: 100%"
            />
          </NFormItem>
          <NFormItem label="Действует с">
            <NDatePicker
              v-model:formatted-value="contractForm.valid_from"
              type="date"
              value-format="yyyy-MM-dd"
              format="dd.MM.yyyy"
              style="width: 100%"
            />
          </NFormItem>
          <NFormItem label="Действует по">
            <NDatePicker
              v-model:formatted-value="contractForm.valid_to"
              type="date"
              value-format="yyyy-MM-dd"
              format="dd.MM.yyyy"
              style="width: 100%"
            />
          </NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Категория (тариф)">
            <NSelect v-model:value="contractForm.category" :options="contractCategoryOptions" />
          </NFormItem>
          <NFormItem label="Статус">
            <NSelect v-model:value="contractForm.status" :options="statusOptions" />
          </NFormItem>
        </div>
        <NFormItem label="Оборудование по договору">
          <div class="eq-list">
            <div v-for="(eq, idx) in contractForm.equipments" :key="idx" class="eq-row">
              <NSelect
                v-model:value="eq.equipment_id"
                filterable
                placeholder="Оборудование"
                :options="equipmentsCatalog.map((e) => ({ label: e.name, value: e.id }))"
                style="flex: 1"
              />
              <NInput v-model:value="eq.serial_number" placeholder="Серийный №" style="width: 170px" />
              <NButton size="small" type="error" ghost @click="contractForm.equipments.splice(idx, 1)">×</NButton>
            </div>
            <NButton
              size="tiny"
              dashed
              @click="
                contractForm.equipments.push({
                  equipment_id: equipmentsCatalog[0]?.id ?? 0,
                  serial_number: ''
                })
              "
            >
              + Добавить котёл / оборудование
            </NButton>
          </div>
        </NFormItem>
        <NFormItem label="Примечание">
          <NInput v-model:value="contractForm.note" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="contractModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingContract" @click="saveContract">Сохранить</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="renewModalOpen" preset="card" title="Пролонгация договора ТО" style="width: 520px; max-width: 96vw">
      <NForm label-placement="top" size="small">
        <div class="form-grid-2">
          <NFormItem label="Новый номер">
            <NInput v-model:value="renewForm.number" />
          </NFormItem>
          <NFormItem label="Тариф">
            <NSelect v-model:value="renewForm.category" :options="contractCategoryOptions" />
          </NFormItem>
        </div>
        <div class="form-grid-2">
          <NFormItem label="Действует с">
            <NDatePicker
              v-model:formatted-value="renewForm.valid_from"
              type="date"
              value-format="yyyy-MM-dd"
              format="dd.MM.yyyy"
              style="width: 100%"
            />
          </NFormItem>
          <NFormItem label="Действует по">
            <NDatePicker
              v-model:formatted-value="renewForm.valid_to"
              type="date"
              value-format="yyyy-MM-dd"
              format="dd.MM.yyyy"
              style="width: 100%"
            />
          </NFormItem>
        </div>
        <NFormItem label="Сумма (₽)">
          <NInputNumber v-model:value="renewForm.price" :min="0" style="width: 100%" />
        </NFormItem>
        <NFormItem label="Примечание">
          <NInput v-model:value="renewForm.note" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="renewModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingRenew" @click="submitRenewContract">Продлить договор</NButton>
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
.meta-warn {
  color: #b45309;
  margin-left: 6px;
}
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.cell-strong {
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
}
.cell-num {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.cell-nowrap {
  white-space: nowrap;
}
.cell-stack {
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: flex-start;
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
.eq-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}
.eq-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
@media (max-width: 640px) {
  .form-grid-2,
  .form-grid-3 {
    grid-template-columns: 1fr;
  }
}
</style>
