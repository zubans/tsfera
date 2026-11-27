<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { ref, computed, watch, onMounted, h } from 'vue'
import {
  NSpace,
  NDatePicker,
  NSelect,
  NButton,
  NCard,
  NDataTable,
  NStatistic,
  NGrid,
  NGi,
  NAlert,
  NSwitch,
  type DataTableColumns,
  type DataTableCreateSummary
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { downloadFile } from '@/shared/utils/download'
import { formatDateRangeParam, formatDateTime, formatMoneyRUB } from '@/shared/utils/format'
import { exportRowsToCsv, isOfficePlaceholderName, taskTypeLabel } from './crmLabels'
import type {
  Subdivision,
  UserBrief,
  CRMEnums,
  AmountActsReportRow,
  ExpiredTaskReportRow,
  SubdivisionSumReportRow,
  AmountSparePartsReport,
  CallConversionReport,
  CallConversionRow,
  ContractorReportRow,
  EngineerReportRow,
  TrafficSourceReport,
  TrafficSourceReportRow
} from '@/shared/types/domain'

const props = defineProps<{
  subdivisions: Subdivision[]
  workers: UserBrief[]
  enums: CRMEnums
}>()

type ReportKey =
  | 'amount-acts'
  | 'expired-tasks'
  | 'sum-by-subdivision'
  | 'amount-spare-parts'
  | 'call-conversion'
  | 'contractors'
  | 'engineers'
  | 'traffic-sources'

const allReportOptions: { label: string; value: ReportKey }[] = [
  { label: 'Выручка по актам и договорам (инженеры)', value: 'amount-acts' },
  { label: 'Суммы по филиалам и способам оплаты', value: 'sum-by-subdivision' },
  { label: 'Расширенный отчёт по инженерам', value: 'engineers' },
  { label: 'Отчёт по контрагентам (с выгрузкой)', value: 'contractors' },
  { label: 'Стоимость запчастей по актам', value: 'amount-spare-parts' },
  { label: 'Конверсия звонков', value: 'call-conversion' },
  { label: 'Эффективность источников трафика', value: 'traffic-sources' },
  { label: 'Просроченные задачи', value: 'expired-tasks' }
]

const auth = useAuthStore()
const reportPermission: Record<ReportKey, string> = {
  'amount-acts': 'sum_executor',
  'expired-tasks': 'expired_tasks',
  'sum-by-subdivision': 'sum_subdivision',
  'amount-spare-parts': 'amount_spare_parts',
  'call-conversion': 'call_conversion',
  contractors: 'contractors',
  engineers: 'engineers',
  'traffic-sources': 'traffic_sources'
}
const reportOptions = computed(() =>
  allReportOptions.filter(
    (option) =>
      auth.hasPerm('crm.report.view') || auth.hasPerm(`crm.report.${reportPermission[option.value]}.view`)
  )
)

const activeReport = ref<ReportKey>('amount-acts')
const dateRange = ref<[number, number] | null>(null)
const subdivisionIds = ref<number[]>([])
const userIds = ref<number[]>([])
const paymentMethods = ref<string[]>([])
const hideOfficeAccounts = ref(false)
const loading = ref(false)
const errorMsg = ref('')

const actsRows = ref<AmountActsReportRow[]>([])
const expiredRows = ref<ExpiredTaskReportRow[]>([])
const subdivisionSums = ref<SubdivisionSumReportRow[]>([])
const sparePartsReport = ref<AmountSparePartsReport | null>(null)
const callConversion = ref<CallConversionReport | null>(null)
const contractorRows = ref<ContractorReportRow[]>([])
const engineerRows = ref<EngineerReportRow[]>([])
const trafficSourcesReport = ref<TrafficSourceReport | null>(null)

const paymentMethodOptions = computed(() =>
  (props.enums.payment_options ?? []).map((p) => ({ label: p.label, value: p.key }))
)

const visibleActsRows = computed(() =>
  hideOfficeAccounts.value
    ? actsRows.value.filter((r) => !isOfficePlaceholderName(r.engineer_name))
    : actsRows.value
)

const visibleEngineerRows = computed(() =>
  hideOfficeAccounts.value
    ? engineerRows.value.filter((r) => !isOfficePlaceholderName(r.engineer_name))
    : engineerRows.value
)

function buildQueryString(): string {
  const params = new URLSearchParams()
  const rangeStr = formatDateRangeParam(dateRange.value)
  if (rangeStr) params.set('range', rangeStr)
  if (subdivisionIds.value.length) params.set('subdivision_ids', subdivisionIds.value.join(','))
  if (userIds.value.length) params.set('user_ids', userIds.value.join(','))
  if (paymentMethods.value.length) params.set('payment_methods', paymentMethods.value.join(','))
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

async function loadActiveReport() {
  if (!reportOptions.value.some((option) => option.value === activeReport.value)) {
    if (!reportOptions.value.length) return
    activeReport.value = reportOptions.value[0]!.value
  }
  loading.value = true
  errorMsg.value = ''
  const qs = buildQueryString()
  try {
    switch (activeReport.value) {
      case 'amount-acts': {
        const res = await requestJSON<AmountActsReportRow[]>(`/api/v1/crm/reports/amount-acts${qs}`)
        actsRows.value = Array.isArray(res) ? res : []
        break
      }
      case 'expired-tasks': {
        const res = await requestJSON<ExpiredTaskReportRow[]>(`/api/v1/crm/reports/expired-tasks${qs}`)
        expiredRows.value = Array.isArray(res) ? res : []
        break
      }
      case 'sum-by-subdivision': {
        const res = await requestJSON<SubdivisionSumReportRow[]>(
          `/api/v1/crm/reports/sum-by-subdivision${qs}`
        )
        subdivisionSums.value = Array.isArray(res) ? res : []
        break
      }
      case 'amount-spare-parts': {
        const res = await requestJSON<AmountSparePartsReport>(
          `/api/v1/crm/reports/amount-spare-parts${qs}`
        )
        sparePartsReport.value = res && !Array.isArray(res) ? res : null
        break
      }
      case 'call-conversion': {
        const res = await requestJSON<CallConversionReport>(`/api/v1/crm/reports/call-conversion${qs}`)
        callConversion.value = res && !Array.isArray(res) ? res : null
        break
      }
      case 'contractors': {
        const res = await requestJSON<ContractorReportRow[]>(`/api/v1/crm/reports/contractors${qs}`)
        contractorRows.value = Array.isArray(res) ? res : []
        break
      }
      case 'engineers': {
        const res = await requestJSON<EngineerReportRow[]>(`/api/v1/crm/reports/engineers${qs}`)
        engineerRows.value = Array.isArray(res) ? res : []
        break
      }
      case 'traffic-sources': {
        const res = await requestJSON<TrafficSourceReport>(`/api/v1/crm/reports/traffic-sources${qs}`)
        trafficSourcesReport.value = res && !Array.isArray(res) ? res : null
        break
      }
    }
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка загрузки отчёта'
  } finally {
    loading.value = false
  }
}

async function exportContractors(format: 'xlsx' | 'csv') {
  const params = new URLSearchParams()
  const rangeStr = formatDateRangeParam(dateRange.value)
  if (rangeStr) params.set('range', rangeStr)
  if (subdivisionIds.value.length) params.set('subdivision_ids', subdivisionIds.value.join(','))
  if (userIds.value.length) params.set('user_ids', userIds.value.join(','))
  if (paymentMethods.value.length) params.set('payment_methods', paymentMethods.value.join(','))
  params.set('format', format)
  try {
    await downloadFile(
      `/api/v1/crm/reports/contractors/export?${params.toString()}`,
      `contractors-report.${format}`
    )
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : 'Не удалось выгрузить отчёт'
  }
}

function exportActsCsv() {
  exportRowsToCsv(
    `amount-acts-${new Date().toISOString().slice(0, 10)}.csv`,
    ['Инженер', 'Филиал', 'Актов', 'План. сумма', 'Начислено', 'Оплачено', 'Долг', 'По договорам'],
    visibleActsRows.value.map((r) => [
      r.engineer_name,
      r.subdivision_name || '',
      r.certificates_count,
      r.planned_sum_total || 0,
      r.total_amount,
      r.paid_amount,
      r.debt_amount,
      r.contracts_amount || 0
    ])
  )
}

function exportEngineersCsv() {
  exportRowsToCsv(
    `engineers-${new Date().toISOString().slice(0, 10)}.csv`,
    ['Инженер', 'Актов', 'Нулевых', 'Доля 0 ₽ (%)', 'ТО/рем. актов', 'Сумма ТО/рем.', 'План', 'Факт'],
    visibleEngineerRows.value.map((r) => [
      r.engineer_name,
      r.certificates_count,
      r.zero_sum_count,
      r.zero_percent.toFixed(1),
      r.type_job_cert_count,
      r.type_job_fact_sum,
      r.planned_sum,
      r.fact_sum
    ])
  )
}

const actsColumns: DataTableColumns<AmountActsReportRow> = [
  { title: 'Инженер', key: 'engineer_name', minWidth: 210 },
  { title: 'Филиал', key: 'subdivision_name', width: 150, render: (r) => r.subdivision_name || '—' },
  { title: 'Актов', key: 'certificates_count', width: 80, align: 'right' },
  {
    title: 'План. сумма',
    key: 'planned_sum_total',
    width: 130,
    align: 'right',
    render: (r) => formatMoneyRUB(r.planned_sum_total || 0)
  },
  { title: 'Начислено', key: 'total_amount', width: 130, align: 'right', render: (r) => formatMoneyRUB(r.total_amount) },
  { title: 'Оплачено', key: 'paid_amount', width: 120, align: 'right', render: (r) => formatMoneyRUB(r.paid_amount) },
  { title: 'Долг', key: 'debt_amount', width: 130, align: 'right', render: (r) => formatMoneyRUB(r.debt_amount) },
  {
    title: 'По договорам',
    key: 'contracts_amount',
    width: 135,
    align: 'right',
    render: (r) => formatMoneyRUB(r.contracts_amount || 0)
  }
]

const actsSummary: DataTableCreateSummary = (pageData) => {
  const rows = pageData as unknown as AmountActsReportRow[]
  const sum = (fn: (r: AmountActsReportRow) => number) => rows.reduce((acc, r) => acc + fn(r), 0)
  return {
    engineer_name: { value: h('strong', `Итого (${rows.length})`) },
    subdivision_name: { value: '' },
    certificates_count: { value: h('strong', String(sum((r) => r.certificates_count || 0))) },
    planned_sum_total: { value: h('strong', formatMoneyRUB(sum((r) => r.planned_sum_total || 0))) },
    total_amount: { value: h('strong', formatMoneyRUB(sum((r) => r.total_amount || 0))) },
    paid_amount: { value: h('strong', formatMoneyRUB(sum((r) => r.paid_amount || 0))) },
    debt_amount: { value: h('strong', formatMoneyRUB(sum((r) => r.debt_amount || 0))) },
    contracts_amount: { value: h('strong', formatMoneyRUB(sum((r) => r.contracts_amount || 0))) }
  }
}

const subdivisionSumColumns = computed<DataTableColumns<SubdivisionSumReportRow>>(() => {
  const base: DataTableColumns<SubdivisionSumReportRow> = [
    { title: 'Подразделение', key: 'subdivision_name', minWidth: 180 },
    {
      title: 'Плановая сумма',
      key: 'planned_sum_total',
      width: 145,
      align: 'right',
      render: (r) => formatMoneyRUB(r.planned_sum_total)
    },
    {
      title: 'Факт. сумма',
      key: 'sum_total',
      width: 145,
      align: 'right',
      render: (r) => formatMoneyRUB(r.sum_total)
    }
  ]
  for (const po of props.enums.payment_options ?? []) {
    base.push({
      title: po.label,
      key: `po_${po.key}`,
      width: 135,
      align: 'right',
      render: (r) => formatMoneyRUB(r.by_payment_option?.[po.key] || 0)
    })
  }
  return base
})

const subdivisionSummary: DataTableCreateSummary = (pageData) => {
  const rows = pageData as unknown as SubdivisionSumReportRow[]
  const out: Record<string, { value: ReturnType<typeof h> }> = {
    subdivision_name: { value: h('strong', 'Итого') },
    planned_sum_total: {
      value: h('strong', formatMoneyRUB(rows.reduce((a, r) => a + (r.planned_sum_total || 0), 0)))
    },
    sum_total: {
      value: h('strong', formatMoneyRUB(rows.reduce((a, r) => a + (r.sum_total || 0), 0)))
    }
  }
  for (const po of props.enums.payment_options ?? []) {
    out[`po_${po.key}`] = {
      value: h('strong', formatMoneyRUB(rows.reduce((a, r) => a + (r.by_payment_option?.[po.key] || 0), 0)))
    }
  }
  return out
}

const engineersSummary: DataTableCreateSummary = (pageData) => {
  const rows = pageData as unknown as EngineerReportRow[]
  const sum = (fn: (r: EngineerReportRow) => number) => rows.reduce((a, r) => a + fn(r), 0)
  const totalCerts = sum((r) => r.certificates_count || 0)
  const totalZero = sum((r) => r.zero_sum_count || 0)
  const zeroPct = totalCerts > 0 ? ((totalZero / totalCerts) * 100).toFixed(1) : '0.0'
  return {
    engineer_name: { value: h('strong', `Итого (${rows.length})`) },
    certificates_count: { value: h('strong', String(totalCerts)) },
    zero_sum_count: { value: h('strong', String(totalZero)) },
    zero_percent: { value: h('strong', `${zeroPct}%`) },
    type_job_cert_count: { value: h('strong', String(sum((r) => r.type_job_cert_count || 0))) },
    type_job_fact_sum: { value: h('strong', formatMoneyRUB(sum((r) => r.type_job_fact_sum || 0))) },
    planned_sum: { value: h('strong', formatMoneyRUB(sum((r) => r.planned_sum || 0))) },
    fact_sum: { value: h('strong', formatMoneyRUB(sum((r) => r.fact_sum || 0))) }
  }
}

const callConversionRows = computed<CallConversionRow[]>(() => {
  if (!callConversion.value) return []
  const list = [...(callConversion.value.rows ?? [])]
  if (callConversion.value.target_row) {
    list.push({ ...callConversion.value.target_row, label: 'Целевые обращения' })
  }
  if (callConversion.value.summary_row) {
    list.push({ ...callConversion.value.summary_row, label: 'Итого по всем' })
  }
  return list
})

const callConversionColumns = computed<DataTableColumns<CallConversionRow>>(() => {
  const base: DataTableColumns<CallConversionRow> = [
    { title: 'Категория обращения', key: 'label', minWidth: 200 },
    { title: 'Всего звонков', key: 'total', width: 120, align: 'right' }
  ]
  const results = props.enums.call_results?.length
    ? props.enums.call_results
    : [
        { id: 1, key: 'application', label: 'Заявка' },
        { id: 2, key: 'consultation', label: 'Консультация' },
        { id: 3, key: 'refusal', label: 'Отказ' }
      ]
  for (const res of results) {
    base.push({
      title: res.label,
      key: `res_${res.id}`,
      width: 130,
      align: 'right',
      render: (r) => `${r.results?.[res.id] || 0} (${(r.percents?.[res.id] || 0).toFixed(1)}%)`
    })
  }
  base.push({
    title: 'Прочее',
    key: 'other_count',
    width: 120,
    align: 'right',
    render: (r) => `${r.other_count} (${(r.other_percent || 0).toFixed(1)}%)`
  })
  return base
})

const trafficSourceRows = computed<TrafficSourceReportRow[]>(() => {
  if (!trafficSourcesReport.value) return []
  const list = [...(trafficSourcesReport.value.rows ?? [])]
  if (trafficSourcesReport.value.summary_row) {
    list.push({ ...trafficSourcesReport.value.summary_row, name: 'Итого' })
  }
  return list
})

watch(activeReport, () => {
  void loadActiveReport()
})

onMounted(() => {
  void loadActiveReport()
})
</script>

<template>
  <div class="reports-panel">
    <NAlert v-if="errorMsg" type="error" closable style="margin-bottom: 12px" @close="errorMsg = ''">
      {{ errorMsg }}
    </NAlert>

    <NCard size="small" style="margin-bottom: 12px">
      <div class="filter-row">
        <NSelect
          v-model:value="activeReport"
          :options="reportOptions"
          style="min-width: 340px; flex: 1 1 360px; max-width: 440px"
        />
        <NDatePicker
          v-model:value="dateRange"
          type="daterange"
          clearable
          format="dd.MM.yyyy"
          start-placeholder="Период с"
          end-placeholder="по"
          style="width: 255px"
        />
        <NSelect
          v-model:value="subdivisionIds"
          multiple
          clearable
          placeholder="Филиалы"
          style="width: 205px"
          :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
        />
        <NSelect
          v-model:value="userIds"
          multiple
          clearable
          filterable
          placeholder="Сотрудники / инженеры"
          style="width: 230px"
          :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
        />
        <NSelect
          v-model:value="paymentMethods"
          multiple
          clearable
          placeholder="Способы оплаты"
          style="width: 195px"
          :options="paymentMethodOptions"
        />
        <NButton type="primary" :loading="loading" @click="loadActiveReport">
          Сформировать
        </NButton>
      </div>
    </NCard>

    <NCard
      v-if="activeReport === 'amount-acts'"
      title="Выручка по актам ТО и договорам по инженерам"
      size="small"
    >
      <template #header-extra>
        <NSpace align="center" :size="10">
          <NSpace align="center" :size="6">
            <NSwitch v-model:value="hideOfficeAccounts" size="small" />
            <span class="switch-label">Скрыть офисные учётки</span>
          </NSpace>
          <NButton size="small" secondary @click="exportActsCsv">Экспорт CSV</NButton>
        </NSpace>
      </template>
      <NDataTable
        :data="visibleActsRows"
        :loading="loading"
        :row-key="(r: AmountActsReportRow) => r.engineer_id"
        :columns="actsColumns"
        :summary="actsSummary"
        :scroll-x="1050"
        size="small"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'sum-by-subdivision'"
      title="Суммы по подразделениям и способам оплаты"
      size="small"
    >
      <NDataTable
        :data="subdivisionSums"
        :loading="loading"
        :row-key="(r: SubdivisionSumReportRow) => r.subdivision_id"
        :columns="subdivisionSumColumns"
        :summary="subdivisionSummary"
        :scroll-x="860"
        size="small"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'engineers'"
      title="Расширенный отчёт по инженерам"
      size="small"
    >
      <template #header-extra>
        <NSpace align="center" :size="10">
          <NSpace align="center" :size="6">
            <NSwitch v-model:value="hideOfficeAccounts" size="small" />
            <span class="switch-label">Скрыть офисные учётки</span>
          </NSpace>
          <NButton size="small" secondary @click="exportEngineersCsv">Экспорт CSV</NButton>
        </NSpace>
      </template>
      <NDataTable
        :data="visibleEngineerRows"
        :loading="loading"
        :row-key="(r: EngineerReportRow) => r.engineer_id"
        :summary="engineersSummary"
        :scroll-x="980"
        size="small"
        :columns="[
          { title: 'Инженер', key: 'engineer_name', minWidth: 210 },
          { title: 'Актов', key: 'certificates_count', width: 80, align: 'right' },
          { title: 'Нулевых', key: 'zero_sum_count', width: 90, align: 'right' },
          { title: 'Доля 0 ₽', key: 'zero_percent', width: 95, align: 'right', render: (r: EngineerReportRow) => `${r.zero_percent.toFixed(1)}%` },
          { title: 'ТО/рем. актов', key: 'type_job_cert_count', width: 115, align: 'right' },
          { title: 'Сумма ТО/рем.', key: 'type_job_fact_sum', width: 135, align: 'right', render: (r: EngineerReportRow) => formatMoneyRUB(r.type_job_fact_sum) },
          { title: 'План', key: 'planned_sum', width: 130, align: 'right', render: (r: EngineerReportRow) => formatMoneyRUB(r.planned_sum) },
          { title: 'Факт', key: 'fact_sum', width: 130, align: 'right', render: (r: EngineerReportRow) => formatMoneyRUB(r.fact_sum) }
        ]"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'contractors'"
      title="Отчёт по контрагентам"
      size="small"
    >
      <template #header-extra>
        <NSpace>
          <NButton size="small" secondary @click="exportContractors('xlsx')">Экспорт XLSX</NButton>
          <NButton size="small" secondary @click="exportContractors('csv')">Экспорт CSV</NButton>
        </NSpace>
      </template>
      <NDataTable
        :data="contractorRows"
        :loading="loading"
        :row-key="(r: ContractorReportRow) => r.contractor_id"
        :scroll-x="1050"
        :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
        size="small"
        :columns="[
          { title: 'ID', key: 'contractor_id', width: 75 },
          { title: 'Контрагент', key: 'name', minWidth: 190 },
          { title: 'Телефоны', key: 'phones', minWidth: 160, render: (r: ContractorReportRow) => (r.phones ?? []).join(', ') || '—' },
          { title: 'Адреса', key: 'addresses', minWidth: 220, ellipsis: { tooltip: true }, render: (r: ContractorReportRow) => (r.addresses ?? []).join('; ') || '—' },
          { title: 'Актов', key: 'certificates_count', width: 80, align: 'right' },
          { title: 'Выручка', key: 'fact_sum', width: 130, align: 'right', render: (r: ContractorReportRow) => formatMoneyRUB(r.fact_sum) },
          { title: 'Запчасти', key: 'cost_of_parts', width: 125, align: 'right', render: (r: ContractorReportRow) => formatMoneyRUB(r.cost_of_parts) }
        ]"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'amount-spare-parts'"
      title="Стоимость запчастей по выполненным актам"
      size="small"
    >
      <NGrid :cols="2" :x-gap="16" v-if="sparePartsReport">
        <NGi>
          <NStatistic
            label="Итого стоимость запчастей"
            :value="formatMoneyRUB(sparePartsReport.total_cost_of_parts)"
          />
        </NGi>
        <NGi>
          <NStatistic label="Количество актов" :value="sparePartsReport.certificates_count" />
        </NGi>
      </NGrid>
    </NCard>

    <NCard v-else-if="activeReport === 'call-conversion'" title="Конверсия звонков" size="small">
      <NDataTable
        :data="callConversionRows"
        :loading="loading"
        :row-key="(r: CallConversionRow) => `${r.type_of_appeal}:${r.label}`"
        :columns="callConversionColumns"
        :scroll-x="960"
        size="small"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'traffic-sources'"
      title="Эффективность источников трафика"
      size="small"
    >
      <NDataTable
        :data="trafficSourceRows"
        :loading="loading"
        :row-key="(r: TrafficSourceReportRow) => `${r.traffic_source_id ?? 0}:${r.name}`"
        :scroll-x="880"
        size="small"
        :columns="[
          { title: 'Источник', key: 'name', minWidth: 200 },
          { title: 'Звонки', key: 'calls', width: 95, align: 'right' },
          {
            title: 'Заявки',
            key: 'applications',
            width: 145,
            align: 'right',
            render: (r: TrafficSourceReportRow) => `${r.applications} (${r.application_conversion.toFixed(1)}%)`
          },
          {
            title: 'Закрыто заявок',
            key: 'closed_applications',
            width: 155,
            align: 'right',
            render: (r: TrafficSourceReportRow) =>
              `${r.closed_applications} (${r.closed_application_conversion.toFixed(1)}%)`
          },
          {
            title: 'Выручка',
            key: 'sum',
            width: 185,
            align: 'right',
            render: (r: TrafficSourceReportRow) =>
              `${formatMoneyRUB(r.sum)} (${r.sum_conversion.toFixed(1)}%)`
          }
        ]"
      />
    </NCard>

    <NCard v-else-if="activeReport === 'expired-tasks'" title="Просроченные задачи" size="small">
      <NDataTable
        :data="expiredRows"
        :loading="loading"
        :row-key="(r: ExpiredTaskReportRow) => r.task_id"
        :scroll-x="980"
        :pagination="{ pageSize: 25, showSizePicker: true, pageSizes: [25, 50, 100] }"
        size="small"
        :columns="[
          { title: 'ID', key: 'task_id', width: 80 },
          { title: 'Задача', key: 'title', minWidth: 220 },
          { title: 'Тип', key: 'task_type', width: 165, render: (r: ExpiredTaskReportRow) => taskTypeLabel(r.task_type, enums) },
          { title: 'Контрагент', key: 'contractor_name', minWidth: 180, ellipsis: { tooltip: true } },
          { title: 'Ответственный', key: 'assigned_name', width: 180, ellipsis: { tooltip: true } },
          {
            title: 'Дедлайн',
            key: 'plan_datetime_to',
            width: 150,
            render: (r: ExpiredTaskReportRow) => formatDateTime(r.plan_datetime_to)
          },
          { title: 'Просрочка (мин)', key: 'overdue_minutes', width: 135, align: 'right' }
        ]"
      />
    </NCard>
  </div>
</template>

<style scoped>
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.switch-label {
  font-size: 12px;
  color: #475569;
}
</style>
