<script setup lang="ts">
import BusinessDataDetails from './BusinessDataDetails.vue'
import { ref, computed, watch, onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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
  NTag,
  NAlert,
  NDivider,
  NCard,
  NCheckbox,
  NRate,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { downloadFile, openPrintTab } from '@/shared/utils/download'
import {
  formatDateOnly,
  formatDateTime,
  formatMoneyRUB,
  documentStatusLabel,
  toDateInput,
  dateInputToRFC3339
} from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type {
  Application,
  Certificate,
  CertificateExecutor,
  MaintenanceContract,
  Contractor,
  ContractorAddress,
  UserBrief,
  RefItem,
  CRMEnums,
  Analytic,
  AnalyticQuestion,
  Banner
} from '@/shared/types/domain'

const commentApp = ref<Application | null>(null)
const commentText = ref('')
const commentOpen = ref(false)
async function saveOnlyComment() {
  if (!commentApp.value) return
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/applications/${commentApp.value.id}/comment`, { method: 'PATCH', body: JSON.stringify({ comment: commentText.value }) })
    commentApp.value.comment = commentText.value
    commentOpen.value = false
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : 'Не удалось сохранить комментарий'
  }
}
const importedDetails = ref<Application | Certificate | null>(null)
const importedOpen = ref(false)
function showImportedDetails(record: Application | Certificate) {
  importedDetails.value = record
  importedOpen.value = true
}
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const activeTab = ref<'applications' | 'certificates' | 'analytics'>('applications')
const appOrderBy = ref<string>('')
const applications = ref<Application[]>([])
const certificates = ref<Certificate[]>([])
const analytics = ref<Analytic[]>([])
const analyticQuestions = ref<AnalyticQuestion[]>([])
const banners = ref<{ before_create: Banner[]; after_create: Banner[] }>({
  before_create: [],
  after_create: []
})
const afterCreateBanners = ref<Banner[]>([])
const contracts = ref<MaintenanceContract[]>([])
const contractors = ref<Contractor[]>([])
const engineers = ref<UserBrief[]>([])
const specialNotePresets = ref<RefItem[]>([])
const modalAddresses = ref<ContractorAddress[]>([])
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

const loading = ref(false)
const errorMsg = ref('')

const certModalOpen = ref(false)
const editCertId = ref<number | null>(null)
const savingCert = ref(false)
const certError = ref('')

const analyticModalOpen = ref(false)
const analyticCertId = ref<number | null>(null)
const editAnalyticId = ref<number | null>(null)
const savingAnalytic = ref(false)
const analyticError = ref('')
const analyticForm = ref({
  number: '',
  rating: 5,
  comment: '',
  status: 1,
  create_task: false,
  answers: {} as Record<number, number[]>
})

function emptyCertForm() {
  return {
    application_id: null as number | null,
    maintenance_contract_id: null as number | null,
    without_maintenance_contract: false,
    contractor_id: null as number | null,
    address_id: null as number | null,
    engineer_id: null as number | null,
    work_date: toDateInput(new Date().toISOString()),
    total_amount: 0,
    paid_amount: 0,
    cost_of_parts: 0,
    receipt_number: '',
    payment_method: 'cash',
    work_types: ['ТО'] as string[],
    spare_parts: [] as { name: string; quantity: number; price: number }[],
    executors: [] as CertificateExecutor[],
    results: {
      smoke_channel_vacuum: '',
      gas_pressure_in_the_pipeline: '',
      burner_gas_pressure: '',
      working_pressure: '',
      heating_expansion_tank_pressure: '',
      DHW_expansion_tank_pressure: '',
      DHW_flow: '',
      partial_heat_setting: '',
      voltage: '',
      type_of_coolant: '',
      coolant_parameters: '',
      contract_eligible: undefined as boolean | undefined,
      has_air_conditioner: undefined as boolean | undefined
    },
    special_notes: '',
    other_note: '',
    recommendation: '',
    comment: ''
  }
}

const certForm = ref(emptyCertForm())

const triStateBoolOptions = [
  { label: 'Да', value: 'yes' },
  { label: 'Нет', value: 'no' }
]

const executorsSum = computed(() =>
  certForm.value.executors.reduce((acc, ex) => acc + Number(ex.sum || 0), 0)
)

const appSortOptions = [
  { label: 'По умолчанию (сначала новые)', value: '' },
  { label: 'Сначала ранние по плану', value: 'plan_asc' },
  { label: 'По плановой сумме (убыв.)', value: 'planned_sum_desc' }
]

const analyticStatusOptions = [
  { label: 'Активна', value: 1 },
  { label: 'Закрыта', value: 2 }
]

const contractorContractOptions = computed(() =>
  contracts.value
    .filter((c) => !certForm.value.contractor_id || c.contractor_id === certForm.value.contractor_id)
    .map((c) => ({ label: `Договор №${c.number} (${c.address_text || c.valid_to.slice(0, 10)})`, value: c.id }))
)

const paymentOptions = computed(() => {
  if (enums.value.payment_options?.length) {
    return enums.value.payment_options.map((p) => ({ label: p.label, value: p.key }))
  }
  return [
    { label: 'Наличные', value: 'cash' },
    { label: 'Терминал / Карта', value: 'card' },
    { label: 'Безналичный расчёт', value: 'bank_transfer' },
    { label: 'Онлайн / СБП', value: 'online' }
  ]
})

async function loadApplications() {
  const qs = appOrderBy.value ? `&order_by=${encodeURIComponent(appOrderBy.value)}` : ''
  const data = await requestJSON<Application[]>(`/api/v1/crm/applications?limit=100${qs}`)
  applications.value = Array.isArray(data) ? data : []
}

async function loadData() {
  loading.value = true
  errorMsg.value = ''
  try {
    const appUrl = appOrderBy.value
      ? `/api/v1/crm/applications?limit=100&order_by=${encodeURIComponent(appOrderBy.value)}`
      : '/api/v1/crm/applications?limit=100'
    const [
      appsRes,
      certsRes,
      ctrRes,
      engRes,
      enumRes,
      contractRes,
      noteRes,
      bannersRes,
      aqRes,
      analyticsRes
    ] = await Promise.allSettled([
      requestJSON<Application[]>(appUrl),
      requestJSON<Certificate[]>('/api/v1/crm/certificates?limit=100'),
      requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100'),
      requestJSON<UserBrief[]>('/api/v1/crm/workers?engineer=true'),
      requestJSON<CRMEnums>('/api/v1/crm/enums'),
      requestJSON<MaintenanceContract[]>('/api/v1/crm/contracts?limit=100'),
      requestJSON<RefItem[]>('/api/v1/crm/refs/special_note'),
      requestJSON<{ before_create?: Banner[]; after_create?: Banner[] }>('/api/v1/crm/banners/for-engineer'),
      requestJSON<AnalyticQuestion[]>('/api/v1/crm/analytic-questions?active=true'),
      requestJSON<Analytic[]>('/api/v1/crm/analytics?limit=100')
    ])
    if (appsRes.status === 'fulfilled') applications.value = appsRes.value ?? []
    if (certsRes.status === 'fulfilled') certificates.value = certsRes.value ?? []
    if (ctrRes.status === 'fulfilled') contractors.value = ctrRes.value?.items ?? []
    if (engRes.status === 'fulfilled') engineers.value = engRes.value ?? []
    if (enumRes.status === 'fulfilled' && enumRes.value) enums.value = enumRes.value
    if (contractRes.status === 'fulfilled') contracts.value = contractRes.value ?? []
    if (noteRes.status === 'fulfilled') specialNotePresets.value = noteRes.value ?? []
    if (bannersRes.status === 'fulfilled' && bannersRes.value && !Array.isArray(bannersRes.value)) {
      banners.value = {
        before_create: bannersRes.value.before_create ?? [],
        after_create: bannersRes.value.after_create ?? []
      }
    }
    if (aqRes.status === 'fulfilled' && Array.isArray(aqRes.value)) {
      analyticQuestions.value = aqRes.value
    }
    if (analyticsRes.status === 'fulfilled' && Array.isArray(analyticsRes.value)) {
      analytics.value = analyticsRes.value
    }
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка загрузки данных'
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

async function openCreateCert(prefill?: {
  application_id?: number
  contractor_id?: number
  address_id?: number
  engineer_id?: number
}) {
  editCertId.value = null
  certError.value = ''
  const form = emptyCertForm()
  if (prefill) {
    form.application_id = prefill.application_id ?? null
    form.contractor_id = prefill.contractor_id ?? contractors.value[0]?.id ?? null
    form.address_id = prefill.address_id ?? null
    form.engineer_id = prefill.engineer_id ?? engineers.value[0]?.id ?? null
  } else {
    form.contractor_id = contractors.value[0]?.id ?? null
    form.engineer_id = engineers.value[0]?.id ?? null
  }
  certForm.value = form
  await loadContractorAddresses(form.contractor_id)
  if (!certForm.value.address_id && modalAddresses.value.length > 0) {
    certForm.value.address_id = modalAddresses.value[0].id
  }
  activeTab.value = 'certificates'
  certModalOpen.value = true
}

async function openEditCert(cert: Certificate) {
  editCertId.value = cert.id
  certError.value = ''
  await loadContractorAddresses(cert.contractor_id)
  certForm.value = {
    application_id: cert.application_id ?? null,
    maintenance_contract_id: cert.maintenance_contract_id ?? null,
    without_maintenance_contract: Boolean(cert.without_maintenance_contract),
    contractor_id: cert.contractor_id,
    address_id: cert.address_id,
    engineer_id: cert.engineer_id,
    work_date: toDateInput(cert.work_date),
    total_amount: cert.total_amount,
    paid_amount: cert.paid_amount,
    cost_of_parts: cert.cost_of_parts ?? 0,
    receipt_number: cert.receipt_number || '',
    payment_method: cert.payment_method || 'cash',
    work_types: [...(cert.work_types ?? [])],
    spare_parts: (cert.spare_parts ?? []).map((p) => ({ ...p })),
    executors: (cert.executors ?? []).map((e) => ({ ...e })),
    results: {
      smoke_channel_vacuum: cert.results?.smoke_channel_vacuum || '',
      gas_pressure_in_the_pipeline: cert.results?.gas_pressure_in_the_pipeline || '',
      burner_gas_pressure: cert.results?.burner_gas_pressure || '',
      working_pressure: cert.results?.working_pressure || '',
      heating_expansion_tank_pressure: cert.results?.heating_expansion_tank_pressure || '',
      DHW_expansion_tank_pressure: cert.results?.DHW_expansion_tank_pressure || '',
      DHW_flow: cert.results?.DHW_flow || '',
      partial_heat_setting: cert.results?.partial_heat_setting || '',
      voltage: cert.results?.voltage || '',
      type_of_coolant: cert.results?.type_of_coolant || '',
      coolant_parameters: cert.results?.coolant_parameters || '',
      contract_eligible: cert.results?.contract_eligible,
      has_air_conditioner: cert.results?.has_air_conditioner
    },
    special_notes: cert.special_notes,
    other_note: cert.other_note,
    recommendation: cert.recommendation,
    comment: cert.comment
  }
  certModalOpen.value = true
}

function addCertExecutor() {
  const used = new Set(certForm.value.executors.map((e) => e.user_id))
  const nextEng =
    (certForm.value.engineer_id && !used.has(certForm.value.engineer_id)
      ? certForm.value.engineer_id
      : engineers.value.find((e) => !used.has(e.id))?.id) ||
    certForm.value.engineer_id ||
    0
  const rem = Math.max(0, Number(certForm.value.total_amount || 0) - executorsSum.value)
  certForm.value.executors.push({ user_id: nextEng, sum: rem })
}

function splitExecutorsEvenly() {
  const list = certForm.value.executors.filter((e) => e.user_id > 0)
  if (list.length === 0) return
  const total = Number(certForm.value.total_amount || 0)
  const share = Math.floor((total / list.length) * 100) / 100
  let assigned = 0
  list.forEach((ex, i) => {
    if (i === list.length - 1) {
      ex.sum = Math.round((total - assigned) * 100) / 100
    } else {
      ex.sum = share
      assigned += share
    }
  })
}

async function saveCert() {
  if (!certForm.value.contractor_id || !certForm.value.address_id || !certForm.value.engineer_id) {
    certError.value = 'Укажите контрагента, адрес и инженера'
    return
  }
  const cleanExecutors = certForm.value.executors.filter((e) => e.user_id > 0)
  if (cleanExecutors.length > 0) {
    const diff = Math.abs(
      cleanExecutors.reduce((s, e) => s + Number(e.sum || 0), 0) - Number(certForm.value.total_amount || 0)
    )
    if (diff > 0.01) {
      certError.value = `Сумма по соисполнителям (${executorsSum.value} ₽) должна совпадать с итоговой суммой акта (${certForm.value.total_amount} ₽)`
      return
    }
  }
  savingCert.value = true
  certError.value = ''
  try {
    const isCreate = !editCertId.value
    const payload = {
      ...certForm.value,
      executors: cleanExecutors,
      maintenance_contract_id: certForm.value.without_maintenance_contract
        ? null
        : certForm.value.maintenance_contract_id,
      work_date: dateInputToRFC3339(certForm.value.work_date)
    }
    if (editCertId.value) {
      await requestJSON(`/api/v1/crm/certificates/${editCertId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/certificates', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    certModalOpen.value = false
    if (isCreate && banners.value.after_create.length > 0) {
      afterCreateBanners.value = banners.value.after_create
    }
    await loadData()
  } catch (err) {
    certError.value = err instanceof ApiError ? err.message : 'Ошибка сохранения акта'
  } finally {
    savingCert.value = false
  }
}

async function openAnalyticForCert(cert: Certificate) {
  analyticError.value = ''
  analyticCertId.value = cert.id
  editAnalyticId.value = cert.analytic_id ?? null
  const answersMap: Record<number, number[]> = {}
  if (cert.analytic_id) {
    try {
      const existing = await requestJSON<Analytic>(`/api/v1/crm/analytics/${cert.analytic_id}`)
      for (const ans of existing.answers ?? []) {
        answersMap[ans.question_id] = [...(ans.option_ids ?? [])]
      }
      analyticForm.value = {
        number: existing.number || '',
        rating: existing.rating || 5,
        comment: existing.comment || '',
        status: existing.status || 1,
        create_task: false,
        answers: answersMap
      }
    } catch {
      analyticForm.value = {
        number: '',
        rating: cert.analytic_rating || 5,
        comment: '',
        status: 1,
        create_task: false,
        answers: {}
      }
    }
  } else {
    analyticForm.value = {
      number: `АН-${cert.id}`,
      rating: 5,
      comment: '',
      status: 1,
      create_task: false,
      answers: {}
    }
  }
  analyticModalOpen.value = true
}

function openAnalyticRatingModal(a: Analytic) {
  analyticError.value = ''
  analyticCertId.value = a.certificate_id
  editAnalyticId.value = a.id
  const answersMap: Record<number, number[]> = {}
  for (const ans of a.answers ?? []) {
    answersMap[ans.question_id] = [...(ans.option_ids ?? [])]
  }
  analyticForm.value = {
    number: a.number || '',
    rating: a.rating || 5,
    comment: a.comment || '',
    status: a.status || 1,
    create_task: false,
    answers: answersMap
  }
  analyticModalOpen.value = true
}

function toggleAnalyticOption(questionId: number, optionId: number, checked: boolean) {
  const cur = new Set(analyticForm.value.answers[questionId] ?? [])
  if (checked) cur.add(optionId)
  else cur.delete(optionId)
  analyticForm.value.answers[questionId] = Array.from(cur)
}

async function saveAnalytic() {
  if (!analyticCertId.value) return
  savingAnalytic.value = true
  analyticError.value = ''
  try {
    const answers = Object.entries(analyticForm.value.answers)
      .filter(([, ids]) => ids.length > 0)
      .map(([qid, option_ids]) => ({ question_id: Number(qid), option_ids }))

    await requestJSON(`/api/v1/crm/analytics/by-certificate/${analyticCertId.value}`, {
      method: 'POST',
      body: JSON.stringify({
        number: analyticForm.value.number,
        rating: analyticForm.value.rating,
        comment: analyticForm.value.comment,
        status: analyticForm.value.status,
        create_task: analyticForm.value.create_task,
        answers
      })
    })
    analyticModalOpen.value = false
    await loadData()
  } catch (err) {
    analyticError.value = err instanceof ApiError ? err.message : 'Ошибка сохранения анкеты качества'
  } finally {
    savingAnalytic.value = false
  }
}

async function quickSetAnalyticRating(a: Analytic, rating: number) {
  try {
    await requestJSON(`/api/v1/crm/analytics/${a.id}/rating`, {
      method: 'PATCH',
      body: JSON.stringify({ rating, comment: a.comment || '', create_task: rating > 0 && rating <= 3 })
    })
    await loadData()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось обновить оценку'
  }
}

async function runExport(url: string, fallbackName: string) {
  try {
    await downloadFile(url, fallbackName)
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : 'Не удалось выгрузить файл'
  }
}

async function toggleCompleteApplication(app: Application) {
  await requestJSON(`/api/v1/crm/applications/${app.id}/complete`, {
    method: 'POST',
    body: JSON.stringify({ is_complete: !app.is_complete })
  })
  await loadData()
}

async function deleteCert(id: number) {
  await requestJSON(`/api/v1/crm/certificates/${id}`, { method: 'DELETE' })
  await loadData()
}

async function exportCSV() {
  await runExport('/api/v1/crm/certificates/export?format=csv', `certificates-${new Date().toISOString().slice(0, 10)}.csv`)
}

async function askAIAboutCert(cert: Certificate) {
  const conv = await requestJSON<{ id: number }>('/api/v1/chat/conversations', {
    method: 'POST',
    body: JSON.stringify({
      title: `Акт ТО #${cert.id}`,
      mode: 'agent',
      entity_type: 'certificate',
      entity_id: cert.id
    })
  })
  await router.push({ path: '/chat', query: { convId: String(conv.id) } })
}

const appColumns: DataTableColumns<Application> = [
  { title: 'Комментарий', key: 'comment_action', render: (r) => auth.hasPerm('crm.application.update_comment') ? h(NButton, { size: 'tiny', onClick: () => { commentApp.value = r; commentText.value = r.comment; commentOpen.value = true } }, { default: () => 'Изменить комментарий' }) : r.comment },
  { title: 'Номер', key: 'number', render: (r) => r.number ?? r.id },
  { title: 'Связи и данные', key: 'details', render: (r) => h(NButton, { size: 'tiny', onClick: () => showImportedDetails(r) }, { default: () => r.incomplete ? 'Неполная запись' : 'Подробнее' }) },
  { title: 'ID', key: 'id', width: 65 },
  {
    title: 'Плановый слот',
    key: 'plan_datetime_from',
    render: (r) => {
      const slot = formatDateTime(r.plan_datetime_from)
      if (r.work_window_from && r.work_window_to) {
        return `${slot} (окно ${r.work_window_from.slice(11, 16)}–${r.work_window_to.slice(11, 16)})`
      }
      return slot
    }
  },
  { title: 'Контрагент', key: 'contractor_name' },
  { title: 'Адрес', key: 'address_text', ellipsis: { tooltip: true } },
  {
    title: 'Исполнители',
    key: 'engineer_name',
    render: (r) =>
      r.executor_names?.length
        ? `${r.engineer_name || ''} (+${r.executor_names.join(', ')})`
        : r.engineer_name || '—'
  },
  {
    title: 'План / Оценка',
    key: 'planned_sum',
    render: (r) => {
      const parts: string[] = []
      if (r.planned_sum && r.planned_sum > 0) parts.push(formatMoneyRUB(r.planned_sum))
      if (r.analytic_rating || r.rating) parts.push(`★ ${r.analytic_rating || r.rating}/5`)
      return parts.join(' · ') || '—'
    }
  },
  {
    title: 'Статус',
    key: 'status',
    render: (r) => h(NTag, { size: 'small' }, { default: () => documentStatusLabel(r.status) })
  },
  {
    title: 'Работы',
    key: 'work_types',
    render: (r) => (r.work_types ?? []).join(', ') || '—'
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 220,
    render: (r) =>
      h(NSpace, { size: 6 }, () => [
        auth.hasPerm('crm.certificate.create') &&
          h(
            NButton,
            {
              size: 'tiny',
              type: 'primary',
              secondary: true,
              onClick: () =>
                void openCreateCert({
                  application_id: r.id,
                  contractor_id: r.contractor_id,
                  address_id: r.address_id,
                  engineer_id: r.engineer_id
                })
            },
            { default: () => '+ Акт ТО' }
          ),
        auth.hasPerm('crm.application.close') &&
          h(
            NButton,
            {
              size: 'tiny',
              type: r.is_complete ? 'warning' : 'success',
              secondary: true,
              onClick: () => void toggleCompleteApplication(r)
            },
            { default: () => (r.is_complete ? 'Открыть' : 'Завершить') }
          )
      ])
  }
]

const certColumns: DataTableColumns<Certificate> = [
  { title: 'Номер', key: 'number', render: (r) => r.number ?? r.id },
  { title: 'Связи и данные', key: 'details', render: (r) => h(NButton, { size: 'tiny', onClick: () => showImportedDetails(r) }, { default: () => 'Подробнее' }) },
  { title: 'ID', key: 'id', width: 65 },
  { title: 'Дата работ', key: 'work_date', render: (r) => formatDateOnly(r.work_date) },
  { title: 'Контрагент', key: 'contractor_name' },
  {
    title: 'Инженер',
    key: 'engineer_name',
    render: (r) =>
      r.executors && r.executors.length > 0
        ? `${r.engineer_name || ''} (${r.executors.map((e) => `${e.full_name || `#${e.user_id}`}: ${formatMoneyRUB(e.sum)}`).join(', ')})`
        : r.engineer_name || '—'
  },
  { title: 'Сумма', key: 'total_amount', render: (r) => formatMoneyRUB(r.total_amount) },
  { title: 'Оплачено', key: 'paid_amount', render: (r) => formatMoneyRUB(r.paid_amount) },
  {
    title: 'Работы',
    key: 'work_types',
    render: (r) => (r.work_types ?? []).join(', ') || '—'
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 310,
    render: (r) =>
      h(NSpace, { size: 4 }, () => [
        h(
          NButton,
          { size: 'tiny', onClick: () => void openEditCert(r) },
          { default: () => 'Изм.' }
        ),
        h(
          NButton,
          {
            size: 'tiny',
            secondary: true,
            onClick: () => openPrintTab(`/api/v1/crm/certificates/${r.id}/print`)
          },
          { default: () => 'Печать' }
        ),
        (auth.hasPerm('crm.analytic.manage') || auth.hasPerm('crm.analytic.create') || auth.hasPerm('crm.analytic.update')) &&
          h(
            NButton,
            {
              size: 'tiny',
              type: r.analytic_id ? 'success' : 'warning',
              secondary: true,
              onClick: () => void openAnalyticForCert(r)
            },
            { default: () => (r.analytic_rating ? `★ ${r.analytic_rating}/5` : 'Анкета') }
          ),
        auth.hasPerm('chat.use') &&
          h(
            NButton,
            { size: 'tiny', type: 'info', secondary: true, onClick: () => void askAIAboutCert(r) },
            { default: () => 'ИИ' }
          ),
        auth.hasPerm('crm.certificate.delete') &&
          h(
            NButton,
            { size: 'tiny', type: 'error', ghost: true, onClick: () => void deleteCert(r.id) },
            { default: () => 'Уд.' }
          )
      ])
  }
]

const analyticColumns: DataTableColumns<Analytic> = [
  { title: 'ID', key: 'id', width: 65 },
  { title: 'Номер', key: 'number', width: 100 },
  { title: 'Акт', key: 'certificate_id', width: 75, render: (r) => `#${r.certificate_id}` },
  { title: 'Контрагент', key: 'contractor_name', render: (r) => r.contractor_name || '—' },
  { title: 'Инженер', key: 'engineer_name', render: (r) => r.engineer_name || '—' },
  {
    title: 'Оценка',
    key: 'rating',
    width: 170,
    render: (r) =>
      (auth.hasPerm('crm.analytic.manage') || auth.hasPerm('crm.analytic.create') || auth.hasPerm('crm.analytic.update'))
        ? h(NRate, {
            value: r.rating,
            count: 5,
            size: 'small',
            onUpdateValue: (val: number) => void quickSetAnalyticRating(r, val)
          })
        : `★ ${r.rating}/5`
  },
  { title: 'Комментарий', key: 'comment', ellipsis: { tooltip: true } },
  {
    title: 'Действия',
    key: 'actions',
    width: 110,
    render: (r) =>
      h(
        NButton,
        { size: 'tiny', secondary: true, onClick: () => openAnalyticRatingModal(r) },
        { default: () => 'Анкета' }
      )
  }
]

watch(appOrderBy, () => {
  void loadApplications()
})

watch(
  () => certForm.value.contractor_id,
  (cid) => {
    if (certModalOpen.value && cid) {
      void loadContractorAddresses(cid)
    }
  }
)

onMounted(async () => {
  const qTab = String(route.query.tab || '')
  if (qTab === 'certificates' || qTab === 'analytics' || qTab === 'applications') {
    activeTab.value = qTab
  }
  await loadData()
  if (route.query.createCertForApp || route.query.openCreateCert) {
    await openCreateCert({
      application_id: route.query.createCertForApp ? Number(route.query.createCertForApp) : undefined,
      contractor_id: route.query.contractorId ? Number(route.query.contractorId) : undefined,
      address_id: route.query.addressId ? Number(route.query.addressId) : undefined,
      engineer_id: route.query.engineerId ? Number(route.query.engineerId) : undefined
    })
  }
})
</script>

<template>
  <div class="apps-acts-page">
    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NAlert
      v-if="afterCreateBanners.length > 0"
      type="info"
      closable
      style="margin-bottom: 12px"
      @close="afterCreateBanners = []"
    >
      <div v-for="b in afterCreateBanners" :key="b.id" class="banner-box">
        <img v-if="b.file_url" :src="b.file_url" alt="" class="banner-img" />
        <div>
          <strong>{{ b.name }}</strong>
          <div v-if="b.text">{{ b.text }}</div>
        </div>
      </div>
    </NAlert>

    <NTabs v-model:value="activeTab" type="line">
      <NTabPane name="applications" tab="Заявки">
        <NSpace justify="space-between" align="center" style="margin-bottom: 12px">
          <NSelect
            v-model:value="appOrderBy"
            :options="appSortOptions"
            style="width: 280px"
          />
          <NSpace>
            <NButton
              secondary
              @click="
                void runExport(
                  '/api/v1/crm/applications/export?format=xlsx',
                  `applications-${new Date().toISOString().slice(0, 10)}.xlsx`
                )
              "
            >
              Экспорт XLSX
            </NButton>
            <NButton
              secondary
              @click="
                void runExport(
                  '/api/v1/crm/applications/export?format=csv',
                  `applications-${new Date().toISOString().slice(0, 10)}.csv`
                )
              "
            >
              Экспорт CSV
            </NButton>
          </NSpace>
        </NSpace>
        <NDataTable :columns="appColumns" :data="applications" :loading="loading" :row-key="(r) => r.id" />
      </NTabPane>

      <NTabPane name="certificates" tab="Акты выполненных работ">
        <NSpace justify="space-between" style="margin-bottom: 12px">
          <NSpace>
            <NButton v-if="auth.hasPerm('crm.certificate.export')"
              secondary
              @click="
                void runExport(
                  '/api/v1/crm/certificates/export?format=xlsx',
                  `certificates-${new Date().toISOString().slice(0, 10)}.xlsx`
                )
              "
            >
              Экспорт XLSX
            </NButton>
            <NButton v-if="auth.hasPerm('crm.certificate.export')" secondary @click="exportCSV">Экспорт в CSV</NButton>
          </NSpace>
          <NButton
            v-if="auth.hasPerm('crm.certificate.create')"
            type="primary"
            @click="openCreateCert()"
          >
            + Новый акт ТО
          </NButton>
        </NSpace>
        <NDataTable :columns="certColumns" :data="certificates" :loading="loading" :row-key="(r) => r.id" />
      </NTabPane>

      <NTabPane name="analytics" tab="Аналитика (Контроль качества)">
        <NSpace justify="end" style="margin-bottom: 12px">
          <NButton
            secondary
            @click="
              void runExport(
                '/api/v1/crm/analytics/export?format=xlsx',
                `analytics-${new Date().toISOString().slice(0, 10)}.xlsx`
              )
            "
          >
            Экспорт XLSX
          </NButton>
          <NButton
            secondary
            @click="
              void runExport(
                '/api/v1/crm/analytics/export?format=csv',
                `analytics-${new Date().toISOString().slice(0, 10)}.csv`
              )
            "
          >
            Экспорт CSV
          </NButton>
        </NSpace>
        <NDataTable :columns="analyticColumns" :data="analytics" :loading="loading" :row-key="(r) => r.id" />
      </NTabPane>
    </NTabs>

    <NModal v-model:show="commentOpen" preset="card" title="Комментарий заявки" style="width: 600px; max-width: 96vw">
      <NInput v-model:value="commentText" type="textarea" />
      <NButton type="primary" @click="saveOnlyComment">Сохранить комментарий</NButton>
    </NModal>
    <NModal v-model:show="importedOpen" preset="card" title="Данные документа" style="width: 900px; max-width: 96vw">
      <BusinessDataDetails v-if="importedDetails" :record="importedDetails" />
    </NModal>
    <NModal
      v-model:show="certModalOpen"
      preset="card"
      :title="editCertId ? `Акт ТО #${editCertId}` : 'Новый акт выполненных работ'"
      style="width: 820px; max-width: 96vw"
    >
      <div v-if="!editCertId && banners.before_create.length > 0" style="margin-bottom: 12px">
        <NCard v-for="b in banners.before_create" :key="b.id" size="small" style="margin-bottom: 8px">
          <div class="banner-box">
            <img v-if="b.file_url" :src="b.file_url" alt="" class="banner-img" />
            <div>
              <strong>{{ b.name }}</strong>
              <div v-if="b.text">{{ b.text }}</div>
            </div>
          </div>
        </NCard>
      </div>
      <NAlert v-if="certError" type="error" style="margin-bottom: 10px">{{ certError }}</NAlert>
      <NForm label-placement="top">
        <div class="grid-3">
          <NFormItem label="Контрагент">
            <NSelect
              v-model:value="certForm.contractor_id"
              filterable
              :options="contractors.map((c) => ({ label: c.name, value: c.id }))"
            />
          </NFormItem>
          <NFormItem label="Адрес">
            <NSelect
              v-model:value="certForm.address_id"
              filterable
              :options="modalAddresses.map((a) => ({ label: a.address, value: a.id }))"
            />
          </NFormItem>
          <NFormItem label="Инженер">
            <NSelect
              v-model:value="certForm.engineer_id"
              filterable
              :options="engineers.map((e) => ({ label: e.full_name || e.username, value: e.id }))"
            />
          </NFormItem>
        </div>

        <div class="grid-3">
          <NFormItem label="Договор ТО">
            <NSelect
              v-model:value="certForm.maintenance_contract_id"
              filterable
              clearable
              :disabled="certForm.without_maintenance_contract"
              :options="contractorContractOptions"
            />
          </NFormItem>
          <NFormItem label="Номер квитанции">
            <NInput v-model:value="certForm.receipt_number" placeholder="КВ-000123" />
          </NFormItem>
          <NFormItem label="Без договора ТО">
            <NSwitch v-model:value="certForm.without_maintenance_contract" />
          </NFormItem>
        </div>

        <div class="grid-4">
          <NFormItem label="Дата работ">
            <input v-model="certForm.work_date" type="date" class="native-dt" />
          </NFormItem>
          <NFormItem label="Итого (₽)">
            <NInputNumber v-model:value="certForm.total_amount" :min="0" style="width: 100%" />
          </NFormItem>
          <NFormItem label="Оплачено (₽)">
            <NInputNumber v-model:value="certForm.paid_amount" :min="0" style="width: 100%" />
          </NFormItem>
          <NFormItem label="Стоимость запчастей (₽)">
            <NInputNumber v-model:value="certForm.cost_of_parts" :min="0" style="width: 100%" />
          </NFormItem>
        </div>

        <div class="grid-2">
          <NFormItem label="Способ оплаты">
            <NSelect
              v-model:value="certForm.payment_method"
              :options="paymentOptions"
            />
          </NFormItem>
          <NFormItem label="Виды выполненных работ">
            <NSelect
              v-model:value="certForm.work_types"
              multiple
              :options="enums.work_types.map((w) => ({ label: w, value: w }))"
            />
          </NFormItem>
        </div>

        <NDivider>Контрольные замеры оборудования (9 параметров)</NDivider>
        <div class="grid-3">
          <NFormItem label="Разрежение в дым. канале">
            <NInput v-model:value="certForm.results.smoke_channel_vacuum" placeholder="Па" />
          </NFormItem>
          <NFormItem label="Давление газа в газопроводе">
            <NInput v-model:value="certForm.results.gas_pressure_in_the_pipeline" placeholder="мбар" />
          </NFormItem>
          <NFormItem label="Давление газа на горелке">
            <NInput v-model:value="certForm.results.burner_gas_pressure" placeholder="мбар" />
          </NFormItem>
          <NFormItem label="Рабочее давление СО">
            <NInput v-model:value="certForm.results.working_pressure" placeholder="бар" />
          </NFormItem>
          <NFormItem label="Давление РБ отопления">
            <NInput v-model:value="certForm.results.heating_expansion_tank_pressure" placeholder="бар" />
          </NFormItem>
          <NFormItem label="Давление РБ ГВС">
            <NInput v-model:value="certForm.results.DHW_expansion_tank_pressure" placeholder="бар" />
          </NFormItem>
          <NFormItem label="Расход ГВС">
            <NInput v-model:value="certForm.results.DHW_flow" placeholder="л/мин" />
          </NFormItem>
          <NFormItem label="Настройка част. мощности">
            <NInput v-model:value="certForm.results.partial_heat_setting" placeholder="кВт / %" />
          </NFormItem>
          <NFormItem label="Напряжение сети">
            <NInput v-model:value="certForm.results.voltage" placeholder="В" />
          </NFormItem>
          <NFormItem label="Тип теплоносителя">
            <NInput v-model:value="certForm.results.type_of_coolant" placeholder="Вода / Антифриз" />
          </NFormItem>
          <NFormItem label="Параметры теплоносителя">
            <NInput v-model:value="certForm.results.coolant_parameters" placeholder="pH / концентрация" />
          </NFormItem>
          <NFormItem label="Подходит под договор ТО">
            <NSelect
              :value="
                certForm.results.contract_eligible === true
                  ? 'yes'
                  : certForm.results.contract_eligible === false
                    ? 'no'
                    : null
              "
              clearable
              placeholder="Не указано"
              :options="triStateBoolOptions"
              @update:value="(v) => (certForm.results.contract_eligible = v === 'yes' ? true : v === 'no' ? false : undefined)"
            />
          </NFormItem>
          <NFormItem label="Есть кондиционер">
            <NSelect
              :value="
                certForm.results.has_air_conditioner === true
                  ? 'yes'
                  : certForm.results.has_air_conditioner === false
                    ? 'no'
                    : null
              "
              clearable
              placeholder="Не указано"
              :options="triStateBoolOptions"
              @update:value="(v) => (certForm.results.has_air_conditioner = v === 'yes' ? true : v === 'no' ? false : undefined)"
            />
          </NFormItem>
        </div>

        <NDivider>
          Соисполнители ({{ formatMoneyRUB(executorsSum) }} из {{ formatMoneyRUB(certForm.total_amount) }})
        </NDivider>
        <NSpace vertical style="margin-bottom: 12px">
          <NSpace v-for="(ex, idx) in certForm.executors" :key="idx" align="center">
            <NSelect
              v-model:value="ex.user_id"
              filterable
              style="width: 280px"
              :options="engineers.map((e) => ({ label: e.full_name || e.username, value: e.id }))"
            />
            <NInputNumber v-model:value="ex.sum" :min="0" style="width: 160px" />
            <NButton size="small" @click="certForm.executors.splice(idx, 1)">×</NButton>
          </NSpace>
          <NSpace>
            <NButton size="tiny" @click="addCertExecutor">+ Добавить соисполнителя</NButton>
            <NButton
              v-if="certForm.executors.length > 0"
              size="tiny"
              secondary
              @click="splitExecutorsEvenly"
            >
              Распределить поровну
            </NButton>
          </NSpace>
        </NSpace>

        <NDivider>Запчасти и расходники</NDivider>
        <NSpace vertical style="margin-bottom: 12px">
          <NSpace v-for="(sp, idx) in certForm.spare_parts" :key="idx">
            <NInput v-model:value="sp.name" placeholder="Наименование" style="width: 280px" />
            <NInputNumber v-model:value="sp.quantity" :min="1" style="width: 110px" />
            <NInputNumber v-model:value="sp.price" :min="0" style="width: 130px" />
            <NButton size="small" @click="certForm.spare_parts.splice(idx, 1)">×</NButton>
          </NSpace>
          <NButton
            size="tiny"
            @click="certForm.spare_parts.push({ name: '', quantity: 1, price: 0 })"
          >
            + Добавить запчасть
          </NButton>
        </NSpace>

        <div class="grid-2">
          <NFormItem label="Особые отметки (устанавливает notes_viewed=false в заявке)">
            <NSpace vertical style="width: 100%">
              <NSpace v-if="specialNotePresets.length" :size="4" wrap>
                <NButton
                  v-for="preset in specialNotePresets"
                  :key="preset.id"
                  size="tiny"
                  secondary
                  @click="
                    certForm.special_notes = certForm.special_notes
                      ? `${certForm.special_notes}; ${preset.name}`
                      : preset.name
                  "
                >
                  + {{ preset.name }}
                </NButton>
              </NSpace>
              <NInput v-model:value="certForm.special_notes" type="textarea" :rows="2" />
            </NSpace>
          </NFormItem>
          <NFormItem label="Рекомендации клиенту">
            <NInput v-model:value="certForm.recommendation" type="textarea" :rows="2" />
          </NFormItem>
        </div>
        <div class="grid-2">
          <NFormItem label="Прочие заметки">
            <NInput v-model:value="certForm.other_note" type="textarea" :rows="2" />
          </NFormItem>
          <NFormItem label="Комментарий к акту">
            <NInput v-model:value="certForm.comment" type="textarea" :rows="2" />
          </NFormItem>
        </div>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="certModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingCert" @click="saveCert">Сохранить акт</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="analyticModalOpen"
      preset="card"
      :title="`Анкета контроля качества · Акт #${analyticCertId ?? ''}`"
      style="width: 640px; max-width: 96vw"
    >
      <NAlert v-if="analyticError" type="error" style="margin-bottom: 10px">{{ analyticError }}</NAlert>
      <NForm label-placement="top">
        <div class="grid-3">
          <NFormItem label="Номер анкеты">
            <NInput v-model:value="analyticForm.number" placeholder="АН-100" />
          </NFormItem>
          <NFormItem label="Оценка (1–5)">
            <NRate v-model:value="analyticForm.rating" :count="5" />
          </NFormItem>
          <NFormItem label="Статус">
            <NSelect v-model:value="analyticForm.status" :options="analyticStatusOptions" />
          </NFormItem>
        </div>
        <NFormItem label="Комментарий">
          <NInput v-model:value="analyticForm.comment" type="textarea" :rows="2" />
        </NFormItem>
        <NFormItem>
          <NCheckbox v-model:checked="analyticForm.create_task">
            Создать задачу контроля качества в пул (при низкой оценке)
          </NCheckbox>
        </NFormItem>
        <template v-if="analyticQuestions.length > 0">
          <NDivider>Вопросы анкеты</NDivider>
          <div v-for="q in analyticQuestions" :key="q.id" class="aq-block">
            <div class="aq-title">{{ q.name }}</div>
            <NSpace wrap>
              <NCheckbox
                v-for="opt in q.options"
                :key="opt.id"
                :checked="(analyticForm.answers[q.id] ?? []).includes(opt.id)"
                @update:checked="(v) => toggleAnalyticOption(q.id, opt.id, v)"
              >
                {{ opt.name }}
              </NCheckbox>
            </NSpace>
          </div>
        </template>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="analyticModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingAnalytic" @click="saveAnalytic">Сохранить анкету</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.banner-box {
  display: flex;
  align-items: center;
  gap: 12px;
}
.banner-img {
  max-width: 120px;
  max-height: 70px;
  border-radius: 6px;
  object-fit: cover;
}
.aq-block {
  margin-bottom: 12px;
}
.aq-title {
  font-weight: 600;
  margin-bottom: 6px;
}
.grid-2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.grid-3 {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.grid-4 {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}
.native-dt {
  width: 100%;
  padding: 5px 8px;
  border: 1px solid #d1d5db;
  border-radius: 4px;
}
</style>
