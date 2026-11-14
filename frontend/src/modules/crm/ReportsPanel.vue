<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { ref, computed, watch, onMounted } from 'vue'
import {
  NSpace,
  NInput,
  NSelect,
  NButton,
  NCard,
  NDataTable,
  NStatistic,
  NGrid,
  NGi,
  NAlert,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { downloadFile } from '@/shared/utils/download'
import { formatDateTime, formatMoneyRUB } from '@/shared/utils/format'
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
  'amount-acts': 'sum_executor', 'expired-tasks': 'expired_tasks', 'sum-by-subdivision': 'sum_subdivision',
  'amount-spare-parts': 'amount_spare_parts', 'call-conversion': 'call_conversion', contractors: 'contractors', engineers: 'engineers', 'traffic-sources': 'traffic_sources'
}
const reportOptions = computed(() => allReportOptions.filter((option) => auth.hasPerm('crm.report.view') || auth.hasPerm(`crm.report.${reportPermission[option.value]}.view`)))
const activeReport = ref<ReportKey>('amount-acts')
const range = ref('')
const subdivisionIds = ref<number[]>([])
const userIds = ref<number[]>([])
const paymentMethods = ref<string[]>([])
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

function buildQueryString(): string {
  const params = new URLSearchParams()
  if (range.value.trim()) params.set('range', range.value.trim())
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
  if (range.value.trim()) params.set('range', range.value.trim())
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

const actsColumns: DataTableColumns<AmountActsReportRow> = [
  { title: 'Инженер', key: 'engineer_name' },
  { title: 'Филиал', key: 'subdivision_name', render: (r) => r.subdivision_name || '—' },
  { title: 'Актов', key: 'certificates_count' },
  {
    title: 'План. сумма',
    key: 'planned_sum_total',
    render: (r) => formatMoneyRUB(r.planned_sum_total || 0)
  },
  { title: 'Начислено', key: 'total_amount', render: (r) => formatMoneyRUB(r.total_amount) },
  { title: 'Оплачено', key: 'paid_amount', render: (r) => formatMoneyRUB(r.paid_amount) },
  { title: 'Долг', key: 'debt_amount', render: (r) => formatMoneyRUB(r.debt_amount) },
  {
    title: 'По договорам',
    key: 'contracts_amount',
    render: (r) => formatMoneyRUB(r.contracts_amount || 0)
  }
]

const subdivisionSumColumns = computed<DataTableColumns<SubdivisionSumReportRow>>(() => {
  const base: DataTableColumns<SubdivisionSumReportRow> = [
    { title: 'Подразделение', key: 'subdivision_name' },
    {
      title: 'Плановая сумма',
      key: 'planned_sum_total',
      render: (r) => formatMoneyRUB(r.planned_sum_total)
    },
    { title: 'Факт. сумма', key: 'sum_total', render: (r) => formatMoneyRUB(r.sum_total) }
  ]
  for (const po of props.enums.payment_options ?? []) {
    base.push({
      title: po.label,
      key: `po_${po.key}`,
      render: (r) => formatMoneyRUB(r.by_payment_option?.[po.key] || 0)
    })
  }
  return base
})

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
    { title: 'Категория обращения', key: 'label' },
    { title: 'Всего звонков', key: 'total' }
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
      render: (r) => `${r.results?.[res.id] || 0} (${(r.percents?.[res.id] || 0).toFixed(1)}%)`
    })
  }
  base.push({
    title: 'Прочее',
    key: 'other_count',
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
  <div>
    <NAlert v-if="errorMsg" type="error" closable style="margin-bottom: 12px" @close="errorMsg = ''">
      {{ errorMsg }}
    </NAlert>

    <NSpace style="margin-bottom: 14px" wrap align="center">
      <NSelect v-model:value="activeReport" :options="reportOptions" style="width: 310px" />
      <NInput
        v-model:value="range"
        placeholder="01.11.2026 - 30.11.2026 (опц.)"
        style="width: 230px"
      />
      <NSelect
        v-model:value="subdivisionIds"
        multiple
        clearable
        placeholder="Филиалы"
        style="width: 220px"
        :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
      />
      <NSelect
        v-model:value="userIds"
        multiple
        clearable
        placeholder="Сотрудники / инженеры"
        style="width: 240px"
        :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
      />
      <NSelect
        v-model:value="paymentMethods"
        multiple
        clearable
        placeholder="Способы оплаты"
        style="width: 210px"
        :options="paymentMethodOptions"
      />
      <NButton type="primary" secondary :loading="loading" @click="loadActiveReport">
        Сформировать
      </NButton>
    </NSpace>

    <NCard
      v-if="activeReport === 'amount-acts'"
      title="Выручка по актам ТО и договорам по инженерам"
      size="small"
    >
      <NDataTable :data="actsRows" :loading="loading" :row-key="(r) => r.engineer_id" :columns="actsColumns" />
    </NCard>

    <NCard
      v-else-if="activeReport === 'sum-by-subdivision'"
      title="Суммы по подразделениям и способам оплаты"
      size="small"
    >
      <NDataTable
        :data="subdivisionSums"
        :loading="loading"
        :row-key="(r) => r.subdivision_id"
        :columns="subdivisionSumColumns"
      />
    </NCard>

    <NCard
      v-else-if="activeReport === 'engineers'"
      title="Расширенный отчёт по инженерам"
      size="small"
    >
      <NDataTable
        :data="engineerRows"
        :loading="loading"
        :row-key="(r) => r.engineer_id"
        :columns="[
          { title: 'Инженер', key: 'engineer_name' },
          { title: 'Актов', key: 'certificates_count' },
          { title: 'Нулевых', key: 'zero_sum_count' },
          { title: 'Доля 0 ₽', key: 'zero_percent', render: (r: EngineerReportRow) => `${r.zero_percent.toFixed(1)}%` },
          { title: 'ТО/рем. актов', key: 'type_job_cert_count' },
          { title: 'Сумма ТО/рем.', key: 'type_job_fact_sum', render: (r: EngineerReportRow) => formatMoneyRUB(r.type_job_fact_sum) },
          { title: 'План', key: 'planned_sum', render: (r: EngineerReportRow) => formatMoneyRUB(r.planned_sum) },
          { title: 'Факт', key: 'fact_sum', render: (r: EngineerReportRow) => formatMoneyRUB(r.fact_sum) }
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
        :row-key="(r) => r.contractor_id"
        :columns="[
          { title: 'ID', key: 'contractor_id', width: 70 },
          { title: 'Контрагент', key: 'name' },
          { title: 'Телефоны', key: 'phones', render: (r: ContractorReportRow) => (r.phones ?? []).join(', ') || '—' },
          { title: 'Адреса', key: 'addresses', render: (r: ContractorReportRow) => (r.addresses ?? []).join('; ') || '—' },
          { title: 'Актов', key: 'certificates_count', width: 80 },
          { title: 'Выручка', key: 'fact_sum', render: (r: ContractorReportRow) => formatMoneyRUB(r.fact_sum) },
          { title: 'Запчасти', key: 'cost_of_parts', render: (r: ContractorReportRow) => formatMoneyRUB(r.cost_of_parts) }
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
        :row-key="(r) => `${r.type_of_appeal}:${r.label}`"
        :columns="callConversionColumns"
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
        :row-key="(r) => `${r.traffic_source_id ?? 0}:${r.name}`"
        :columns="[
          { title: 'Источник', key: 'name' },
          { title: 'Звонки', key: 'calls' },
          {
            title: 'Заявки',
            key: 'applications',
            render: (r: TrafficSourceReportRow) => `${r.applications} (${r.application_conversion.toFixed(1)}%)`
          },
          {
            title: 'Закрыто заявок',
            key: 'closed_applications',
            render: (r: TrafficSourceReportRow) =>
              `${r.closed_applications} (${r.closed_application_conversion.toFixed(1)}%)`
          },
          {
            title: 'Выручка',
            key: 'sum',
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
        :row-key="(r) => r.task_id"
        :columns="[
          { title: 'ID', key: 'task_id', width: 70 },
          { title: 'Задача', key: 'title' },
          { title: 'Тип', key: 'task_type' },
          { title: 'Контрагент', key: 'contractor_name' },
          { title: 'Ответственный', key: 'assigned_name' },
          {
            title: 'Дедлайн',
            key: 'plan_datetime_to',
            render: (r: ExpiredTaskReportRow) => formatDateTime(r.plan_datetime_to)
          },
          { title: 'Просрочка (мин)', key: 'overdue_minutes' }
        ]"
      />
    </NCard>
  </div>
</template>
