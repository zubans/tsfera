<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { useAuthStore } from '@/stores/auth'
import {
  NSpace,
  NCard,
  NDataTable,
  NButton,
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
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, uploadCRMFile, ApiError } from '@/shared/api/http'
import type {
  TrafficSource,
  AnalyticQuestion,
  AnalyticQuestionOption,
  Banner,
  CRMEnums
} from '@/shared/types/domain'

const auth = useAuthStore()
function can(resource: string, action: string): boolean {
  const group = resource === 'analytic_question' ? 'analytic' : resource
  return auth.hasPerm(`crm.${group}.manage`) || auth.hasPerm(`crm.${resource}.${action}`)
}
const errorMsg = ref('')
const okMsg = ref('')

const trafficSources = ref<TrafficSource[]>([])
const questions = ref<AnalyticQuestion[]>([])
const banners = ref<Banner[]>([])
const tsTypeOptions = ref<{ label: string; value: number }[]>([
  { label: 'Поиск / Карты', value: 1 },
  { label: 'Доски объявлений', value: 2 },
  { label: 'Рекомендации', value: 3 },
  { label: 'Телефония', value: 4 }
])

const engineerBannersCfg = ref<{ before_create: number[]; after_create: number[] }>({
  before_create: [],
  after_create: []
})

// --- Модалки ---
const tsModalOpen = ref(false)
const editTsId = ref<number | null>(null)
const tsForm = ref({ name: '', traffic_source_type_id: 1, sort: 100, active: true })

const qModalOpen = ref(false)
const editQId = ref<number | null>(null)
const qForm = ref({
  name: '',
  analytic_document_type_id: 1,
  sort: 100,
  active: true,
  options: [] as AnalyticQuestionOption[]
})

const bannerModalOpen = ref(false)
const editBannerId = ref<number | null>(null)
const bannerForm = ref({
  name: '',
  text: '',
  file_id: null as number | null,
  file_url: '',
  active: true
})
const uploadingBanner = ref(false)

async function loadCatalogs() {
  const [tsRes, qRes, bRes, sRes, enumRes] = await Promise.allSettled([
    requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources'),
    requestJSON<AnalyticQuestion[]>('/api/v1/crm/analytic-questions'),
    requestJSON<Banner[]>('/api/v1/crm/banners'),
    requestJSON<{ key: string; data: { before_create?: number[]; after_create?: number[] } }>(
      '/api/v1/crm/app-settings/app.banners_for_engineer'
    ),
    requestJSON<CRMEnums>('/api/v1/crm/enums')
  ])
  if (tsRes.status === 'fulfilled' && Array.isArray(tsRes.value)) trafficSources.value = tsRes.value
  if (qRes.status === 'fulfilled' && Array.isArray(qRes.value)) questions.value = qRes.value
  if (bRes.status === 'fulfilled' && Array.isArray(bRes.value)) banners.value = bRes.value
  if (sRes.status === 'fulfilled' && sRes.value?.data) {
    engineerBannersCfg.value = {
      before_create: sRes.value.data.before_create ?? [],
      after_create: sRes.value.data.after_create ?? []
    }
  }
  if (enumRes.status === 'fulfilled' && enumRes.value?.traffic_source_types?.length) {
    tsTypeOptions.value = enumRes.value.traffic_source_types.map((o) => ({ label: o.label, value: o.id }))
  }
}

onMounted(loadCatalogs)

function tsTypeLabel(id: number): string {
  return tsTypeOptions.value.find((o) => o.value === id)?.label ?? String(id)
}

function openCreateTs() {
  editTsId.value = null
  tsForm.value = { name: '', traffic_source_type_id: 1, sort: 100, active: true }
  tsModalOpen.value = true
}

function openEditTs(ts: TrafficSource) {
  editTsId.value = ts.id
  tsForm.value = {
    name: ts.name,
    traffic_source_type_id: ts.traffic_source_type_id,
    sort: ts.sort,
    active: ts.active
  }
  tsModalOpen.value = true
}

async function saveTs() {
  errorMsg.value = ''
  try {
    if (editTsId.value) {
      await requestJSON(`/api/v1/crm/traffic-sources/${editTsId.value}`, {
        method: 'PUT',
        body: JSON.stringify(tsForm.value)
      })
    } else {
      await requestJSON('/api/v1/crm/traffic-sources', {
        method: 'POST',
        body: JSON.stringify(tsForm.value)
      })
    }
    tsModalOpen.value = false
    await loadCatalogs()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка сохранения источника трафика'
  }
}

async function deleteTs(id: number) {
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/traffic-sources/${id}`, { method: 'DELETE' })
    await loadCatalogs()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Не удалось удалить источник'
  }
}

function openCreateQuestion() {
  editQId.value = null
  qForm.value = {
    name: '',
    analytic_document_type_id: 1,
    sort: 100,
    active: true,
    options: [{ id: 1, name: '', sort: 1 }]
  }
  qModalOpen.value = true
}

function openEditQuestion(q: AnalyticQuestion) {
  editQId.value = q.id
  qForm.value = {
    name: q.name,
    analytic_document_type_id: q.analytic_document_type_id || 1,
    sort: q.sort,
    active: q.active,
    options: (q.options || []).map((o) => ({ ...o }))
  }
  qModalOpen.value = true
}

function addQuestionOption() {
  const nextId = qForm.value.options.reduce((mx, o) => Math.max(mx, o.id || 0), 0) + 1
  qForm.value.options.push({ id: nextId, name: '', sort: (qForm.value.options.length + 1) * 10 })
}

async function saveQuestion() {
  errorMsg.value = ''
  try {
    const payload = {
      ...qForm.value,
      options: qForm.value.options.filter((o) => o.name.trim())
    }
    if (editQId.value) {
      await requestJSON(`/api/v1/crm/analytic-questions/${editQId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/crm/analytic-questions', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    qModalOpen.value = false
    await loadCatalogs()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка сохранения вопроса аналитики'
  }
}

function openCreateBanner() {
  editBannerId.value = null
  bannerForm.value = { name: '', text: '', file_id: null, file_url: '', active: true }
  bannerModalOpen.value = true
}

function openEditBanner(b: Banner) {
  editBannerId.value = b.id
  bannerForm.value = {
    name: b.name,
    text: b.text || '',
    file_id: b.file_id ?? null,
    file_url: b.file_url || '',
    active: b.active
  }
  bannerModalOpen.value = true
}

async function onBannerFileChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) return
  uploadingBanner.value = true
  errorMsg.value = ''
  try {
    const uploaded = await uploadCRMFile(f)
    bannerForm.value.file_id = uploaded.id
    bannerForm.value.file_url = uploaded.url
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка загрузки файла баннера'
  } finally {
    uploadingBanner.value = false
    input.value = ''
  }
}

async function saveBanner() {
  errorMsg.value = ''
  try {
    if (editBannerId.value) {
      await requestJSON(`/api/v1/crm/banners/${editBannerId.value}`, {
        method: 'PUT',
        body: JSON.stringify(bannerForm.value)
      })
    } else {
      await requestJSON('/api/v1/crm/banners', {
        method: 'POST',
        body: JSON.stringify(bannerForm.value)
      })
    }
    bannerModalOpen.value = false
    await loadCatalogs()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка сохранения баннера'
  }
}

async function deleteBanner(id: number) {
  errorMsg.value = ''
  try {
    await requestJSON(`/api/v1/crm/banners/${id}`, { method: 'DELETE' })
    await loadCatalogs()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Не удалось удалить баннер'
  }
}

async function saveEngineerBannersConfig() {
  errorMsg.value = ''
  okMsg.value = ''
  try {
    await requestJSON('/api/v1/crm/app-settings/app.banners_for_engineer', {
      method: 'PUT',
      body: JSON.stringify({
        name: 'Баннеры для инженера (до и после создания акта)',
        data: engineerBannersCfg.value
      })
    })
    okMsg.value = 'Настройки баннеров инженера сохранены'
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка сохранения настроек баннеров'
  }
}

const tsColumns: DataTableColumns<TrafficSource> = [
  { title: 'ID', key: 'id', width: 60 },
  { title: 'Название', key: 'name' },
  { title: 'Канал', key: 'traffic_source_type_id', render: (r) => tsTypeLabel(r.traffic_source_type_id) },
  { title: 'Сорт.', key: 'sort', width: 70 },
  {
    title: 'Статус',
    key: 'active',
    width: 100,
    render: (r) => h(NTag, { size: 'small', type: r.active ? 'success' : 'default' }, () => (r.active ? 'Активен' : 'Архив'))
  },
  {
    title: 'Действия',
    key: 'act',
    width: 160,
    render: (r) =>
      h(NSpace, { size: 6 }, () => [
        can('traffic_source', 'update') ? h(NButton, { size: 'tiny', onClick: () => openEditTs(r) }, () => 'Изм.') : null,
        can('traffic_source', 'delete') ? h(NButton, { size: 'tiny', type: 'error', ghost: true, onClick: () => void deleteTs(r.id) }, () => 'Уд.') : null
      ])
  }
]

const questionColumns: DataTableColumns<AnalyticQuestion> = [
  { title: 'ID', key: 'id', width: 60 },
  { title: 'Вопрос', key: 'name' },
  {
    title: 'Варианты ответов',
    key: 'options',
    render: (r) => (r.options || []).map((o) => o.name).join(', ')
  },
  {
    title: 'Активен',
    key: 'active',
    width: 90,
    render: (r) => (r.active ? 'Да' : 'Нет')
  },
  {
    title: '',
    key: 'act',
    width: 90,
    render: (r) => can('analytic_question', 'update') ? h(NButton, { size: 'tiny', onClick: () => openEditQuestion(r) }, () => 'Изм.') : null
  }
]

const bannerColumns: DataTableColumns<Banner> = [
  { title: 'ID', key: 'id', width: 60 },
  { title: 'Заголовок', key: 'name' },
  { title: 'Текст', key: 'text' },
  {
    title: 'Файл',
    key: 'file_url',
    width: 110,
    render: (b) =>
      b.file_url
        ? h('a', { href: b.file_url, target: '_blank', style: 'color:#2563eb' }, 'Открыть')
        : '—'
  },
  {
    title: 'Действия',
    key: 'act',
    width: 150,
    render: (b) =>
      h(NSpace, { size: 6 }, () => [
        can('banner', 'update') ? h(NButton, { size: 'tiny', onClick: () => openEditBanner(b) }, () => 'Изм.') : null,
        can('banner', 'delete') ? h(NButton, { size: 'tiny', type: 'error', ghost: true, onClick: () => void deleteBanner(b.id) }, () => 'Уд.') : null
      ])
  }
]
</script>

<template>
  <NSpace vertical :size="14">
    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>
    <NAlert v-if="okMsg" type="success" closable @close="okMsg = ''">{{ okMsg }}</NAlert>

    <NCard title="Источники трафика" size="small">
      <template #header-extra>
        <NButton v-if="can('traffic_source', 'create')" size="small" type="primary" @click="openCreateTs">+ Источник</NButton>
      </template>
      <NDataTable :columns="tsColumns" :data="trafficSources" size="small" :pagination="{ pageSize: 8 }" />
    </NCard>

    <NCard title="Вопросы контроля качества (Аналитика актов)" size="small">
      <template #header-extra>
        <NButton v-if="can('analytic_question', 'create')" size="small" type="primary" @click="openCreateQuestion">+ Вопрос</NButton>
      </template>
      <NDataTable :columns="questionColumns" :data="questions" size="small" :pagination="{ pageSize: 8 }" />
    </NCard>

    <NCard title="Баннеры (памятки до/после создания акта)" size="small">
      <template #header-extra>
        <NButton v-if="can('banner', 'create')" size="small" type="primary" @click="openCreateBanner">+ Баннер</NButton>
      </template>
      <NDataTable :columns="bannerColumns" :data="banners" size="small" :pagination="{ pageSize: 8 }" />
      <NDivider>Подборки баннеров для инженеров (`app.banners_for_engineer`)</NDivider>
      <NSpace v-if="can('setting', 'update')" vertical :size="8">
        <NForm label-placement="left" label-width="230" size="small">
          <NFormItem label="Показывать ДО создания акта">
            <NSelect
              v-model:value="engineerBannersCfg.before_create"
              multiple
              filterable
              :options="banners.map((b) => ({ label: b.name, value: b.id }))"
            />
          </NFormItem>
          <NFormItem label="Показывать ПОСЛЕ создания акта">
            <NSelect
              v-model:value="engineerBannersCfg.after_create"
              multiple
              filterable
              :options="banners.map((b) => ({ label: b.name, value: b.id }))"
            />
          </NFormItem>
          <NButton size="small" type="primary" @click="saveEngineerBannersConfig">Сохранить настройки баннеров</NButton>
        </NForm>
      </NSpace>
    </NCard>

    <NModal v-model:show="tsModalOpen" preset="card" :title="editTsId ? 'Редактирование источника трафика' : 'Новый источник трафика'" style="width: 440px">
      <NForm label-placement="top">
        <NFormItem label="Название *"><NInput v-model:value="tsForm.name" /></NFormItem>
        <NFormItem label="Тип канала"><NSelect v-model:value="tsForm.traffic_source_type_id" :options="tsTypeOptions" /></NFormItem>
        <NFormItem label="Сортировка"><NInputNumber v-model:value="tsForm.sort" style="width: 100%" /></NFormItem>
        <NFormItem label="Активен"><NSwitch v-model:value="tsForm.active" /></NFormItem>
        <NSpace justify="end">
          <NButton @click="tsModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveTs">Сохранить</NButton>
        </NSpace>
      </NForm>
    </NModal>

    <NModal v-model:show="qModalOpen" preset="card" :title="editQId ? 'Редактирование вопроса' : 'Новый вопрос аналитики'" style="width: 560px">
      <NForm label-placement="top">
        <NFormItem label="Текст вопроса *"><NInput v-model:value="qForm.name" /></NFormItem>
        <NFormItem label="Сортировка / Активен">
          <NSpace align="center">
            <NInputNumber v-model:value="qForm.sort" style="width: 140px" />
            <NSwitch v-model:value="qForm.active" />
          </NSpace>
        </NFormItem>
        <NFormItem label="Варианты ответов">
          <NSpace vertical style="width: 100%">
            <div v-for="(opt, idx) in qForm.options" :key="opt.id || idx" style="display: flex; gap: 6px">
              <NInput v-model:value="opt.name" placeholder="Текст ответа" />
              <NButton size="small" type="error" ghost @click="qForm.options.splice(idx, 1)">×</NButton>
            </div>
            <NButton size="tiny" dashed @click="addQuestionOption">+ Добавить вариант</NButton>
          </NSpace>
        </NFormItem>
        <NSpace justify="end">
          <NButton @click="qModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveQuestion">Сохранить</NButton>
        </NSpace>
      </NForm>
    </NModal>

    <NModal v-model:show="bannerModalOpen" preset="card" :title="editBannerId ? 'Редактирование баннера' : 'Новый баннер'" style="width: 500px">
      <NForm label-placement="top">
        <NFormItem label="Заголовок *"><NInput v-model:value="bannerForm.name" /></NFormItem>
        <NFormItem label="Текст памятки"><NInput v-model:value="bannerForm.text" type="textarea" :rows="3" /></NFormItem>
        <NFormItem label="Изображение">
          <NSpace vertical style="width: 100%">
            <input type="file" accept="image/*" @change="onBannerFileChange" :disabled="uploadingBanner" />
            <div v-if="bannerForm.file_url" style="font-size: 12px">
              Загружено: <a :href="bannerForm.file_url" target="_blank">{{ bannerForm.file_url }}</a>
            </div>
          </NSpace>
        </NFormItem>
        <NFormItem label="Активен"><NSwitch v-model:value="bannerForm.active" /></NFormItem>
        <NSpace justify="end">
          <NButton @click="bannerModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="uploadingBanner" @click="saveBanner">Сохранить</NButton>
        </NSpace>
      </NForm>
    </NModal>
  </NSpace>
</template>
