<script setup lang="ts">
import { ref, computed, watch, onMounted, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NSpace,
  NInput,
  NButton,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NModal,
  NForm,
  NFormItem,
  NSelect,
  NCheckboxGroup,
  NCheckbox,
  NPopover,
  NTag,
  NAlert,
  NDivider,
  NCard,
  NTabs,
  NTabPane,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { formatPhoneRU, formatDateTime } from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type {
  Contractor,
  ContractorAddress,
  SimilarContractorMatch,
  Subdivision,
  Equipment,
  RefItem,
  ChangeLogItem,
  DocDocument,
  TrafficSource,
  LegalEntity,
  DaDataAddressSuggestion
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const searchQuery = ref('')
const legalFilter = ref<'all' | 'legal' | 'individual'>('all')
const items = ref<Contractor[]>([])
const subdivisions = ref<Subdivision[]>([])
const trafficSources = ref<TrafficSource[]>([])
const equipmentTypes = ref<RefItem[]>([])
const manufacturers = ref<RefItem[]>([])
const similarMatches = ref<SimilarContractorMatch[]>([])
const addressSuggestions = ref<DaDataAddressSuggestion[]>([])
const subscriptionsInfo = ref<{ active_subscriptions?: unknown[]; active_contracts?: number } | null>(null)
const addrSubCheck = ref<Record<number, { has_active_subscription?: boolean; has_active_contract?: boolean }>>({})
const total = ref(0)
const loading = ref(false)
const errorMsg = ref('')

const emptyLegalEntity = (): LegalEntity => ({
  name: '',
  director_name: '',
  actual_address: '',
  legal_address: '',
  bank_name: '',
  inn: '',
  kpp: '',
  bik: '',
  checking_account: '',
  corresponding_account: '',
  okpo: '',
  ogrn: ''
})

const visibleColumns = ref<string[]>(['type', 'phones', 'addresses', 'note'])

const selected = ref<Contractor | null>(null)
const drawerOpen = ref(false)
const linkedDocs = ref<DocDocument[]>([])
const changeLogs = ref<ChangeLogItem[]>([])
const equipmentsCatalog = ref<Equipment[]>([])

const contractorModalOpen = ref(false)
const editContractorId = ref<number | null>(null)
const savingContractor = ref(false)
const modalError = ref('')
const contractorForm = ref({
  name: '',
  last_name: '',
  first_name: '',
  second_name: '',
  subdivision_id: null as number | null,
  traffic_source_id: null as number | null,
  type: 1,
  legal_entity: emptyLegalEntity(),
  note: '',
  phones: [{ phone: '', note: '' }],
  emails: [{ email: '' }]
})

const addressForm = ref({
  id: null as number | null,
  address: '',
  city: '',
  street: '',
  house: '',
  apartment: '',
  comment: ''
})
const addressModalOpen = ref(false)

const attachForm = ref({
  address_id: 0,
  equipment_id: null as number | null,
  serial_number: '',
  note: ''
})
const attachModalOpen = ref(false)

const newEqForm = ref({ type: 'Котёл', model: '', manufacturer: '' })
const creatingEq = ref(false)

async function loadColumnSettings() {
  try {
    const saved = await requestJSON<{ columns?: string[] }>('/api/v1/crm/settings/contractors_columns')
    if (saved?.columns && Array.isArray(saved.columns) && saved.columns.length > 0) {
      visibleColumns.value = saved.columns
    }
  } catch {
    // Используем набор колонок по умолчанию.
  }
}

async function saveColumnSettings(cols: (string | number)[]) {
  visibleColumns.value = cols.map(String)
  try {
    await requestJSON('/api/v1/crm/settings/contractors_columns', {
      method: 'PUT',
      body: JSON.stringify({ columns: visibleColumns.value })
    })
  } catch {
    // Не блокируем интерфейс при ошибке сохранения настроек.
  }
}

async function loadContractors() {
  loading.value = true
  errorMsg.value = ''
  try {
    const q = encodeURIComponent(searchQuery.value.trim())
    const extra =
      legalFilter.value === 'legal'
        ? '&only_legal=true'
        : legalFilter.value === 'individual'
          ? '&only_individuals=true'
          : ''
    const res = await requestJSON<{ items: Contractor[]; total: number }>(
      `/api/v1/crm/contractors?q=${q}&limit=50${extra}`
    )
    items.value = res.items ?? []
    total.value = res.total ?? 0
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка загрузки контрагентов'
  } finally {
    loading.value = false
  }
}

async function openContractor(id: number) {
  errorMsg.value = ''
  subscriptionsInfo.value = null
  addrSubCheck.value = {}
  try {
    const [detail, docsRes, logsRes, eqRes, subInfoRes] = await Promise.allSettled([
      requestJSON<Contractor>(`/api/v1/crm/contractors/${id}`),
      requestJSON<DocDocument[]>(`/api/v1/docs/by-entity/contractor/${id}`),
      requestJSON<ChangeLogItem[]>(`/api/v1/crm/change-logs/contractor/${id}?limit=30`),
      requestJSON<Equipment[]>('/api/v1/crm/equipments?limit=100'),
      requestJSON<{ active_subscriptions?: unknown[]; active_contracts?: number }>(
        `/api/v1/crm/contractors/${id}/subscriptions`
      )
    ])
    if (detail.status === 'fulfilled') selected.value = detail.value
    linkedDocs.value = docsRes.status === 'fulfilled' ? docsRes.value ?? [] : []
    changeLogs.value = logsRes.status === 'fulfilled' ? logsRes.value ?? [] : []
    equipmentsCatalog.value = eqRes.status === 'fulfilled' ? eqRes.value ?? [] : []
    if (subInfoRes.status === 'fulfilled') subscriptionsInfo.value = subInfoRes.value ?? null
    drawerOpen.value = true
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка открытия карточки'
  }
}

async function checkAddressSubscription(addrId: number) {
  try {
    const res = await requestJSON<{ has_active_subscription?: boolean; has_active_contract?: boolean }>(
      `/api/v1/crm/addresses/${addrId}/check-subscription`
    )
    addrSubCheck.value = { ...addrSubCheck.value, [addrId]: res }
  } catch {
    // Игнорируем сбой проверки подписки
  }
}

async function suggestDaDataAddresses() {
  const q = addressForm.value.address.trim()
  if (q.length < 3) {
    addressSuggestions.value = []
    return
  }
  try {
    const items = await requestJSON<DaDataAddressSuggestion[]>(
      `/api/v1/crm/addresses/suggest?query=${encodeURIComponent(q)}&count=7`
    )
    addressSuggestions.value = items ?? []
  } catch {
    addressSuggestions.value = []
  }
}

function applyDaDataSuggestion(s: DaDataAddressSuggestion) {
  addressForm.value.address = s.value
  if (s.city) addressForm.value.city = s.city
  if (s.street) addressForm.value.street = s.street
  if (s.house) addressForm.value.house = s.house
  if (s.flat) addressForm.value.apartment = s.flat
  addressSuggestions.value = []
}

async function loadRefs() {
  const [subRes, typeRes, mfgRes, tsRes] = await Promise.allSettled([
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
    requestJSON<RefItem[]>('/api/v1/crm/refs/equipment_type'),
    requestJSON<RefItem[]>('/api/v1/crm/refs/manufacturer'),
    requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources?active=true')
  ])
  if (subRes.status === 'fulfilled') subdivisions.value = subRes.value ?? []
  if (typeRes.status === 'fulfilled') equipmentTypes.value = typeRes.value ?? []
  if (mfgRes.status === 'fulfilled') manufacturers.value = mfgRes.value ?? []
  if (tsRes.status === 'fulfilled') trafficSources.value = tsRes.value ?? []
}

async function checkSimilarContacts() {
  const phones = contractorForm.value.phones.map((p) => p.phone.trim()).filter(Boolean)
  const emails = contractorForm.value.emails.map((e) => e.email.trim()).filter(Boolean)
  if (phones.length === 0 && emails.length === 0) {
    similarMatches.value = []
    return
  }
  try {
    const res = await requestJSON<{ matches: SimilarContractorMatch[] }>(
      '/api/v1/crm/contractors/check-similar',
      {
        method: 'POST',
        body: JSON.stringify({
          exclude_id: editContractorId.value ?? undefined,
          phones,
          emails
        })
      }
    )
    similarMatches.value = res.matches ?? []
  } catch {
    similarMatches.value = []
  }
}

function openCreateContractor() {
  editContractorId.value = null
  modalError.value = ''
  similarMatches.value = []
  contractorForm.value = {
    name: '',
    last_name: '',
    first_name: '',
    second_name: '',
    subdivision_id: auth.user?.subdivision_id ?? null,
    traffic_source_id: null,
    type: 1,
    legal_entity: emptyLegalEntity(),
    note: '',
    phones: [{ phone: '', note: 'Основной' }],
    emails: [{ email: '' }]
  }
  contractorModalOpen.value = true
}

function openEditContractor(c: Contractor) {
  editContractorId.value = c.id
  modalError.value = ''
  similarMatches.value = []
  contractorForm.value = {
    name: c.name,
    last_name: c.last_name || '',
    first_name: c.first_name || '',
    second_name: c.second_name || '',
    subdivision_id: c.subdivision_id ?? null,
    traffic_source_id: c.traffic_source_id ?? null,
    type: c.is_legal_entity ? 2 : c.type || 1,
    legal_entity: c.legal_entity ? { ...emptyLegalEntity(), ...c.legal_entity } : emptyLegalEntity(),
    note: c.note,
    phones: c.phones?.length
      ? c.phones.map((p) => ({ phone: formatPhoneRU(p.phone), note: p.note }))
      : [{ phone: '', note: '' }],
    emails: c.emails?.length ? c.emails.map((e) => ({ email: e.email })) : [{ email: '' }]
  }
  contractorModalOpen.value = true
}

async function saveContractor() {
  savingContractor.value = true
  modalError.value = ''
  try {
    const isLegal = contractorForm.value.type === 2
    const payload = {
      name: contractorForm.value.name,
      last_name: contractorForm.value.last_name,
      first_name: contractorForm.value.first_name,
      second_name: contractorForm.value.second_name,
      subdivision_id: contractorForm.value.subdivision_id,
      traffic_source_id: contractorForm.value.traffic_source_id,
      type: contractorForm.value.type,
      is_legal_entity: isLegal,
      legal_entity: isLegal ? contractorForm.value.legal_entity : null,
      note: contractorForm.value.note,
      phones: contractorForm.value.phones.filter((p) => p.phone.trim()),
      emails: contractorForm.value.emails.filter((e) => e.email.trim())
    }
    let saved: Contractor
    if (editContractorId.value) {
      saved = await requestJSON<Contractor>(`/api/v1/crm/contractors/${editContractorId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      saved = await requestJSON<Contractor>('/api/v1/crm/contractors', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    contractorModalOpen.value = false
    await loadContractors()
    if (selected.value?.id === saved.id) {
      await openContractor(saved.id)
    }
  } catch (err) {
    modalError.value = err instanceof ApiError ? err.message : 'Ошибка сохранения контрагента'
  } finally {
    savingContractor.value = false
  }
}

async function deleteContractor(id: number) {
  try {
    await requestJSON(`/api/v1/crm/contractors/${id}`, { method: 'DELETE' })
    drawerOpen.value = false
    selected.value = null
    await loadContractors()
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось удалить контрагента'
  }
}

function openAddressModal(addr?: ContractorAddress) {
  addressForm.value = addr
    ? {
        id: addr.id,
        address: addr.address,
        city: addr.city || '',
        street: addr.street || '',
        house: addr.house || '',
        apartment: addr.apartment || '',
        comment: addr.comment || ''
      }
    : { id: null, address: '', city: '', street: '', house: '', apartment: '', comment: '' }
  addressModalOpen.value = true
}

async function saveAddress() {
  if (!selected.value) return
  try {
    const payload = { ...addressForm.value, contractor_id: selected.value.id }
    if (addressForm.value.id) {
      await requestJSON(`/api/v1/crm/addresses/${addressForm.value.id}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/addresses', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    addressModalOpen.value = false
    await openContractor(selected.value.id)
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения адреса'
  }
}

async function deleteAddress(addrId: number) {
  if (!selected.value) return
  try {
    await requestJSON(`/api/v1/crm/addresses/${addrId}`, { method: 'DELETE' })
    await openContractor(selected.value.id)
  } catch (err) {
    errorMsg.value =
      err instanceof ApiError ? err.message : 'Адрес используется в заявках или договорах'
  }
}

function openAttachEquipment(addressId: number) {
  attachForm.value = {
    address_id: addressId,
    equipment_id: equipmentsCatalog.value[0]?.id ?? null,
    serial_number: '',
    note: ''
  }
  attachModalOpen.value = true
}

async function createEquipmentInCatalog() {
  if (!newEqForm.value.model.trim()) return
  creatingEq.value = true
  try {
    const created = await requestJSON<Equipment>('/api/v1/crm/equipments', {
      method: 'POST',
      body: JSON.stringify(newEqForm.value)
    })
    equipmentsCatalog.value = [created, ...equipmentsCatalog.value]
    attachForm.value.equipment_id = created.id
    newEqForm.value = { type: 'Котёл', model: '', manufacturer: '' }
  } finally {
    creatingEq.value = false
  }
}

async function attachEquipment() {
  if (!selected.value || !attachForm.value.equipment_id) return
  await requestJSON('/api/v1/crm/address-equipments', {
    method: 'POST',
    body: JSON.stringify(attachForm.value)
  })
  attachModalOpen.value = false
  await openContractor(selected.value.id)
}

async function detachEquipment(id: number) {
  if (!selected.value) return
  await requestJSON(`/api/v1/crm/address-equipments/${id}`, { method: 'DELETE' })
  await openContractor(selected.value.id)
}

async function discussWithAI(c: Contractor) {
  const conv = await requestJSON<{ id: number }>('/api/v1/chat/conversations', {
    method: 'POST',
    body: JSON.stringify({
      title: `Контрагент: ${c.name}`,
      mode: 'agent',
      entity_type: 'contractor',
      entity_id: c.id
    })
  })
  await router.push({ path: '/chat', query: { convId: String(conv.id) } })
}

const columns = computed<DataTableColumns<Contractor>>(() => {
  const cols: DataTableColumns<Contractor> = [
    {
      title: 'Контрагент',
      key: 'name',
      render: (row) => h('strong', row.name)
    }
  ]
  cols.push({ title: 'Категория клиента', key: 'client_category_id', render: (row) => ({ 1: 'Проблемный', 2: 'Беспроблемный', 3: 'Внимательный', 4: 'VIP', 5: 'Юридическое лицо', 6: 'Сотрудник', 7: 'Спящий' } as Record<number, string>)[row.client_category_id ?? 0] || '—' })
  if (visibleColumns.value.includes('type')) {
    cols.push({
      title: 'Тип',
      key: 'type',
      width: 120,
      render: (row) =>
        h(NTag, { size: 'small' }, { default: () => (row.type === 2 ? 'Юр. лицо' : 'Физ. лицо') })
    })
  }
  if (visibleColumns.value.includes('phones')) {
    cols.push({
      title: 'Телефоны',
      key: 'phones',
      render: (row) => (row.phones ?? []).map((p) => formatPhoneRU(p.phone)).join(', ') || '—'
    })
  }
  if (visibleColumns.value.includes('addresses')) {
    cols.push({
      title: 'Адреса',
      key: 'addresses',
      render: (row) => (row.addresses ?? []).map((a) => a.address).join('; ') || '—'
    })
  }
  if (visibleColumns.value.includes('note')) {
    cols.push({
      title: 'Примечание',
      key: 'note',
      ellipsis: { tooltip: true }
    })
  }
  cols.push({
    title: '',
    key: 'actions',
    width: 110,
    render: (row) =>
      h(
        NButton,
        { size: 'tiny', onClick: () => void openContractor(row.id) },
        { default: () => 'Открыть' }
      )
  })
  return cols
})

watch(
  () => route.query.contractorId,
  (cid) => {
    if (cid) void openContractor(Number(cid))
  }
)

onMounted(async () => {
  await Promise.all([loadColumnSettings(), loadContractors(), loadRefs()])
  if (route.query.contractorId) {
    await openContractor(Number(route.query.contractorId))
  }
})
</script>

<template>
  <div class="contractors-page">
    <div class="toolbar">
      <NSpace>
        <NInput
          v-model:value="searchQuery"
          placeholder="Имя, ИНН, телефон, адрес или S/N..."
          style="width: 300px"
          clearable
          @keyup.enter="loadContractors"
        />
        <NSelect
          v-model:value="legalFilter"
          style="width: 170px"
          :options="[
            { label: 'Все лица', value: 'all' },
            { label: 'Только юр. лица', value: 'legal' },
            { label: 'Только физ. лица', value: 'individual' }
          ]"
          @update:value="loadContractors"
        />
        <NButton @click="loadContractors">Найти</NButton>
        <NPopover trigger="click" placement="bottom">
          <template #trigger>
            <NButton secondary>Колонки таблицы</NButton>
          </template>
          <NCheckboxGroup :value="visibleColumns" @update:value="saveColumnSettings">
            <NSpace vertical>
              <NCheckbox value="type">Тип лица</NCheckbox>
              <NCheckbox value="phones">Телефоны</NCheckbox>
              <NCheckbox value="addresses">Адреса</NCheckbox>
              <NCheckbox value="note">Примечание</NCheckbox>
            </NSpace>
          </NCheckboxGroup>
        </NPopover>
      </NSpace>

      <NButton
        v-if="auth.hasPerm('crm.contractor.create')"
        type="primary"
        @click="openCreateContractor"
      >
        + Новый контрагент
      </NButton>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NDataTable :columns="columns" :data="items" :loading="loading" :row-key="(r) => r.id" />
    <div class="footer-total">Найдено записей: {{ total }}</div>

    <NDrawer v-model:show="drawerOpen" :width="560">
      <NDrawerContent v-if="selected" :title="selected.name" closable>
        <NSpace justify="space-between" style="margin-bottom: 12px">
          <NSpace wrap>
            <NButton
              v-if="auth.hasPerm('crm.contractor.update')"
              size="small"
              secondary
              @click="openEditContractor(selected)"
            >
              Редактировать
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.application.create')"
              size="small"
              type="primary"
              secondary
              @click="
                router.push({
                  path: '/crm/calendar',
                  query: {
                    createContractorId: String(selected.id),
                    addressId: selected.addresses?.[0]?.id ? String(selected.addresses[0].id) : undefined
                  }
                })
              "
            >
              + Заявка
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.contract.create')"
              size="small"
              secondary
              @click="
                router.push({
                  path: '/crm/contracts',
                  query: {
                    contractorId: String(selected.id),
                    addressId: selected.addresses?.[0]?.id ? String(selected.addresses[0].id) : undefined
                  }
                })
              "
            >
              + Договор ТО
            </NButton>
            <NButton
              v-if="auth.hasPerm('crm.certificate.create')"
              size="small"
              secondary
              @click="
                router.push({
                  path: '/crm/applications',
                  query: {
                    contractorId: String(selected.id),
                    addressId: selected.addresses?.[0]?.id ? String(selected.addresses[0].id) : undefined,
                    openCreateCert: '1'
                  }
                })
              "
            >
              + Акт ТО
            </NButton>
            <NButton
              v-if="auth.hasPerm('chat.use')"
              size="small"
              type="info"
              secondary
              @click="discussWithAI(selected)"
            >
              Спросить ИИ о клиенте
            </NButton>
          </NSpace>
          <NButton
            v-if="auth.hasPerm('crm.contractor.delete')"
            size="small"
            type="error"
            ghost
            @click="deleteContractor(selected.id)"
          >
            Удалить
          </NButton>
        </NSpace>

        <NTabs type="line">
          <NTabPane name="info" tab="Контакты и адреса">
            <NSpace vertical :size="12">
              <div v-if="selected.note"><strong>Примечание:</strong> {{ selected.note }}</div>
              <div>
                <strong>Телефоны:</strong>
                <div v-for="(p, idx) in selected.phones" :key="idx">
                  {{ formatPhoneRU(p.phone) }} <span v-if="p.note" class="muted">({{ p.note }})</span>
                </div>
              </div>
              <div v-if="selected.emails?.length">
                <strong>Email:</strong> {{ selected.emails.map((e) => e.email).join(', ') }}
              </div>
              <div v-if="subscriptionsInfo">
                <NTag
                  size="small"
                  :type="(subscriptionsInfo.active_subscriptions?.length || 0) > 0 ? 'success' : 'default'"
                >
                  CloudPayments подписок: {{ subscriptionsInfo.active_subscriptions?.length || 0 }}
                </NTag>
                <NTag size="small" style="margin-left: 6px" type="info">
                  Активных договоров: {{ subscriptionsInfo.active_contracts ?? 0 }}
                </NTag>
              </div>
              <NCard v-if="selected.is_legal_entity && selected.legal_entity" size="small" title="Реквизиты юридического лица">
                <div style="font-size: 12px; display: grid; grid-template-columns: 1fr 1fr; gap: 4px">
                  <div><b>Наименование:</b> {{ selected.legal_entity.name }}</div>
                  <div><b>Руководитель:</b> {{ selected.legal_entity.director_name || '—' }}</div>
                  <div><b>ИНН / КПП:</b> {{ selected.legal_entity.inn || '—' }} / {{ selected.legal_entity.kpp || '—' }}</div>
                  <div><b>ОГРН / ОКПО:</b> {{ selected.legal_entity.ogrn || '—' }} / {{ selected.legal_entity.okpo || '—' }}</div>
                  <div><b>Банк / БИК:</b> {{ selected.legal_entity.bank_name || '—' }} ({{ selected.legal_entity.bik || '—' }})</div>
                  <div><b>Р/с:</b> {{ selected.legal_entity.checking_account || '—' }}</div>
                  <div><b>Юр. адрес:</b> {{ selected.legal_entity.legal_address || '—' }}</div>
                  <div><b>Факт. адрес:</b> {{ selected.legal_entity.actual_address || '—' }}</div>
                </div>
              </NCard>

              <NDivider />

              <NSpace justify="space-between" align="center">
                <strong>Адреса и оборудование</strong>
                <NButton
                  v-if="auth.hasPerm('crm.contractor.update')"
                  size="tiny"
                  type="primary"
                  @click="openAddressModal()"
                >
                  + Адрес
                </NButton>
              </NSpace>

              <NCard v-for="addr in selected.addresses" :key="addr.id" size="small">
                <template #header>{{ addr.address }}</template>
                <template #header-extra>
                  <NSpace :size="4">
                    <NButton size="tiny" secondary @click="checkAddressSubscription(addr.id)">Подписка</NButton>
                    <NButton size="tiny" @click="openAttachEquipment(addr.id)">+ Оборудование</NButton>
                    <NButton size="tiny" @click="openAddressModal(addr)">Изм.</NButton>
                    <NButton size="tiny" type="error" ghost @click="deleteAddress(addr.id)">Уд.</NButton>
                  </NSpace>
                </template>
                <div v-if="addrSubCheck[addr.id]" style="margin-bottom: 4px">
                  <NTag
                    size="tiny"
                    :type="addrSubCheck[addr.id].has_active_subscription || addrSubCheck[addr.id].has_active_contract ? 'success' : 'warning'"
                  >
                    {{
                      addrSubCheck[addr.id].has_active_subscription
                        ? 'Подписка CloudPayments активна'
                        : addrSubCheck[addr.id].has_active_contract
                          ? 'Действует договор ТО'
                          : 'Активная подписка/договор не найдены'
                    }}
                  </NTag>
                </div>
                <div v-if="addr.comment" class="muted">{{ addr.comment }}</div>
                <div v-for="ae in addr.equipments" :key="ae.id" class="eq-row">
                  <span>{{ ae.equipment_name }} · S/N: {{ ae.serial_number || '—' }}</span>
                  <NButton size="tiny" text type="error" @click="detachEquipment(ae.id)">
                    Убрать
                  </NButton>
                </div>
              </NCard>
            </NSpace>
          </NTabPane>

          <NTabPane name="docs" :tab="`Документы (${linkedDocs.length})`">
            <NSpace vertical>
              <NCard
                v-for="doc in linkedDocs"
                :key="doc.id"
                size="small"
                hoverable
                style="cursor: pointer"
                @click="router.push({ path: '/docs', query: { docId: String(doc.id) } })"
              >
                <strong>{{ doc.title }}</strong>
                <div class="muted">{{ doc.space }} · {{ doc.doc_type }} · v{{ doc.version }}</div>
              </NCard>
              <div v-if="linkedDocs.length === 0" class="muted">
                Нет привязанных регламентов или заметок.
              </div>
            </NSpace>
          </NTabPane>

          <NTabPane name="history" :tab="`История (${changeLogs.length})`">
            <NSpace vertical>
              <NCard v-for="log in changeLogs" :key="log.id" size="small">
                <div>
                  <NTag size="tiny">{{ log.action }}</NTag>
                  <span style="margin-left: 8px">{{ log.user_name || `User #${log.user_id}` }}</span>
                  <span class="muted" style="margin-left: 8px">{{ formatDateTime(log.created_at) }}</span>
                </div>
                <pre class="diff-json">{{ JSON.stringify(log.changes, null, 2) }}</pre>
              </NCard>
            </NSpace>
          </NTabPane>
        </NTabs>
      </NDrawerContent>
    </NDrawer>

    <NModal
      v-model:show="contractorModalOpen"
      preset="card"
      :title="editContractorId ? 'Редактировать контрагента' : 'Новый контрагент'"
      style="width: 600px; max-width: 96vw"
    >
      <NAlert v-if="modalError" type="error" style="margin-bottom: 10px">{{ modalError }}</NAlert>
      <NAlert v-if="similarMatches.length" type="warning" title="Найдены похожие контрагенты" style="margin-bottom: 10px">
        <div v-for="(m, idx) in similarMatches" :key="idx" style="display: flex; justify-content: space-between; gap: 8px; margin-top: 4px">
          <span><b>{{ m.contractor_name }}</b> ({{ m.matched_on }}: {{ m.contact_value }})</span>
          <NButton
            size="tiny"
            text
            type="primary"
            @click="
              () => {
                contractorModalOpen = false
                openContractor(m.contractor_id)
              }
            "
          >
            Открыть
          </NButton>
        </div>
      </NAlert>
      <NForm label-placement="top">
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 10px">
          <NFormItem label="Тип">
            <NSelect
              v-model:value="contractorForm.type"
              :options="[
                { label: 'Физическое лицо', value: 1 },
                { label: 'Юридическое лицо', value: 2 }
              ]"
            />
          </NFormItem>
          <NFormItem label="Подразделение">
            <NSelect
              v-model:value="contractorForm.subdivision_id"
              clearable
              :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
            />
          </NFormItem>
        </div>
        <div v-if="contractorForm.type === 1" style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 8px">
          <NFormItem label="Фамилия">
            <NInput v-model:value="contractorForm.last_name" placeholder="Иванов" />
          </NFormItem>
          <NFormItem label="Имя">
            <NInput v-model:value="contractorForm.first_name" placeholder="Иван" />
          </NFormItem>
          <NFormItem label="Отчество">
            <NInput v-model:value="contractorForm.second_name" placeholder="Иванович" />
          </NFormItem>
        </div>
        <NFormItem :label="contractorForm.type === 2 ? 'Название компании' : 'Отображаемое ФИО (если пусто — соберётся автоматически)'">
          <NInput v-model:value="contractorForm.name" />
        </NFormItem>
        <NFormItem label="Источник трафика">
          <NSelect
            v-model:value="contractorForm.traffic_source_id"
            clearable
            filterable
            :options="trafficSources.map((t) => ({ label: t.name, value: t.id }))"
          />
        </NFormItem>
        <NCard v-if="contractorForm.type === 2" size="small" title="Реквизиты юридического лица" style="margin-bottom: 12px">
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 8px">
            <NInput v-model:value="contractorForm.legal_entity.name" placeholder="Полное наименование ООО/ИП" />
            <NInput v-model:value="contractorForm.legal_entity.director_name" placeholder="Руководитель" />
            <NInput v-model:value="contractorForm.legal_entity.inn" placeholder="ИНН" />
            <NInput v-model:value="contractorForm.legal_entity.kpp" placeholder="КПП" />
            <NInput v-model:value="contractorForm.legal_entity.ogrn" placeholder="ОГРН" />
            <NInput v-model:value="contractorForm.legal_entity.okpo" placeholder="ОКПО" />
            <NInput v-model:value="contractorForm.legal_entity.bank_name" placeholder="Банк" />
            <NInput v-model:value="contractorForm.legal_entity.bik" placeholder="БИК" />
            <NInput v-model:value="contractorForm.legal_entity.checking_account" placeholder="Расчётный счёт" />
            <NInput v-model:value="contractorForm.legal_entity.corresponding_account" placeholder="Корр. счёт" />
            <NInput v-model:value="contractorForm.legal_entity.legal_address" placeholder="Юридический адрес" />
            <NInput v-model:value="contractorForm.legal_entity.actual_address" placeholder="Фактический адрес" />
          </div>
        </NCard>
        <NFormItem label="Телефоны (нормализуются к 10 цифрам РФ)">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(p, idx) in contractorForm.phones" :key="idx">
              <NInput
                v-model:value="p.phone"
                placeholder="+7 (999) 123-45-67"
                style="width: 210px"
                @blur="checkSimilarContacts"
              />
              <NInput v-model:value="p.note" placeholder="Примечание" style="width: 180px" />
              <NButton size="small" @click="contractorForm.phones.splice(idx, 1)">×</NButton>
            </NSpace>
            <NSpace>
              <NButton size="tiny" @click="contractorForm.phones.push({ phone: '', note: '' })">
                + Добавить телефон
              </NButton>
              <NButton size="tiny" secondary @click="checkSimilarContacts">
                Проверить дубликаты
              </NButton>
            </NSpace>
          </NSpace>
        </NFormItem>
        <NFormItem label="Email">
          <NSpace vertical style="width: 100%">
            <NSpace v-for="(em, idx) in contractorForm.emails" :key="idx">
              <NInput
                v-model:value="em.email"
                placeholder="client@example.ru"
                style="width: 398px"
                @blur="checkSimilarContacts"
              />
              <NButton size="small" @click="contractorForm.emails.splice(idx, 1)">×</NButton>
            </NSpace>
            <NButton size="tiny" @click="contractorForm.emails.push({ email: '' })">
              + Добавить Email
            </NButton>
          </NSpace>
        </NFormItem>
        <NFormItem label="Примечание">
          <NInput v-model:value="contractorForm.note" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="contractorModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="savingContractor" @click="saveContractor">
            Сохранить
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="addressModalOpen" preset="card" title="Адрес объекта" style="width: 480px">
      <NForm label-placement="top">
        <NFormItem label="Полный адрес (подсказки DaData + автосборка)">
          <div style="width: 100%">
            <div style="display: flex; gap: 6px">
              <NInput
                v-model:value="addressForm.address"
                placeholder="г. Воронеж, ул. Мира, д. 10"
                @update:value="suggestDaDataAddresses"
              />
              <NButton size="small" secondary @click="suggestDaDataAddresses">DaData</NButton>
            </div>
            <div
              v-if="addressSuggestions.length > 0"
              style="border: 1px solid #e2e8f0; border-radius: 6px; margin-top: 4px; max-height: 140px; overflow: auto; background: #fff"
            >
              <div
                v-for="(sug, i) in addressSuggestions"
                :key="i"
                style="padding: 6px 8px; cursor: pointer; font-size: 12px; border-bottom: 1px solid #f1f5f9"
                @click="applyDaDataSuggestion(sug)"
              >
                {{ sug.value }}
              </div>
            </div>
          </div>
        </NFormItem>
        <NFormItem label="Город / Улица / Дом">
          <NSpace>
            <NInput v-model:value="addressForm.city" placeholder="Город" style="width: 130px" />
            <NInput v-model:value="addressForm.street" placeholder="Улица" style="width: 160px" />
            <NInput v-model:value="addressForm.house" placeholder="Дом" style="width: 80px" />
          </NSpace>
        </NFormItem>
        <NFormItem label="Комментарий (домофон, код ворот)">
          <NInput v-model:value="addressForm.comment" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="addressModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveAddress">Сохранить адрес</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="attachModalOpen" preset="card" title="Привязать оборудование" style="width: 500px">
      <NForm label-placement="top">
        <NFormItem label="Модель из справочника">
          <NSelect
            v-model:value="attachForm.equipment_id"
            filterable
            :options="equipmentsCatalog.map((e) => ({ label: e.name, value: e.id }))"
          />
        </NFormItem>
        <NFormItem label="Серийный номер">
          <NInput v-model:value="attachForm.serial_number" />
        </NFormItem>
        <NFormItem label="Примечание">
          <NInput v-model:value="attachForm.note" />
        </NFormItem>
        <NDivider>Быстро добавить в справочник</NDivider>
        <NSpace>
          <NSelect
            v-if="equipmentTypes.length"
            v-model:value="newEqForm.type"
            filterable
            :options="equipmentTypes.map((t) => ({ label: t.name, value: t.name }))"
            style="width: 130px"
          />
          <NInput v-else v-model:value="newEqForm.type" placeholder="Тип" style="width: 100px" />
          <NSelect
            v-if="manufacturers.length"
            v-model:value="newEqForm.manufacturer"
            filterable
            clearable
            placeholder="Бренд"
            :options="manufacturers.map((m) => ({ label: m.name, value: m.name }))"
            style="width: 130px"
          />
          <NInput v-else v-model:value="newEqForm.manufacturer" placeholder="Бренд" style="width: 110px" />
          <NInput v-model:value="newEqForm.model" placeholder="Модель" style="width: 130px" />
          <NButton size="small" :loading="creatingEq" @click="createEquipmentInCatalog">Добавить</NButton>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="attachModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="attachEquipment">Привязать</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.contractors-page {
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
.footer-total {
  font-size: 12px;
  color: #64748b;
}
.muted {
  color: #64748b;
  font-size: 12px;
}
.eq-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 6px;
  font-size: 13px;
}
.diff-json {
  font-size: 11px;
  background: #f8fafc;
  padding: 6px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 6px 0 0;
}
</style>
