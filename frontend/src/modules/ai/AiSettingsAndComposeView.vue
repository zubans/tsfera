<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import {
  NTabs,
  NTabPane,
  NInput,
  NButton,
  NSpace,
  NCard,
  NDataTable,
  NSelect,
  NSwitch,
  NModal,
  NForm,
  NFormItem,
  NTag,
  NAlert,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, requestSSE, ApiError } from '@/shared/api/http'
import { renderMarkdown } from '@/shared/utils/markdown'
import { formatDateTime } from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type {
  SearchResult,
  AIProvider,
  AIModel,
  AIUsageLog,
  ModelAccessPolicy,
  UserBrief
} from '@/shared/types/domain'

const auth = useAuthStore()
const router = useRouter()
const errorMsg = ref('')
const statusMsg = ref('')

// 1. Поиск
const searchQ = ref('')
const searchResults = ref<SearchResult[]>([])
const searching = ref(false)
const reindexing = ref(false)

// 2. Compose Studio
const composeForm = ref({
  title: '',
  goal: '',
  space: 'general',
  doc_type: 'sop',
  model_id: null as number | null,
  extra_notes: ''
})
const composeDraft = ref('')
const composing = ref(false)
const renderedDraft = computed(() => renderMarkdown(composeDraft.value))

// 3. Провайдеры и модели
const providers = ref<AIProvider[]>([])
const models = ref<AIModel[]>([])
const usageLogs = ref<AIUsageLog[]>([])
const accessPolicies = ref<ModelAccessPolicy[]>([])
const workers = ref<UserBrief[]>([])

const providerForm = ref({
  name: '',
  kind: 'openai_compat',
  base_url: 'https://api.openai.com',
  api_key: '',
  active: true,
  is_default: false
})

const modelForm = ref({
  provider_id: null as number | null,
  name: '',
  model_id: '',
  capability: 'chat',
  context_window: 32768,
  is_default: false,
  open_to_all: true,
  active: true
})

const accessModalOpen = ref(false)
const accessTargetModel = ref<AIModel | null>(null)
const accessOpenToAll = ref(true)
const accessUserIds = ref<number[]>([])

async function runSearch() {
  if (!searchQ.value.trim()) return
  searching.value = true
  try {
    searchResults.value =
      (await requestJSON<SearchResult[]>(
        `/api/v1/ai/search?q=${encodeURIComponent(searchQ.value.trim())}&limit=20`
      )) ?? []
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка поиска'
  } finally {
    searching.value = false
  }
}

async function runReindex() {
  reindexing.value = true
  try {
    const res = await requestJSON<{ status: string; total_chunks: number }>('/api/v1/ai/reindex', {
      method: 'POST'
    })
    statusMsg.value = `Индекс перестроен: ${res.total_chunks} фрагментов`
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка переиндексации'
  } finally {
    reindexing.value = false
  }
}

function openResult(item: SearchResult) {
  if (item.path.startsWith('/docs/')) {
    void router.push({
      path: '/docs',
      query: { docId: item.path.split('/').pop(), anchor: item.anchor || undefined }
    })
    return
  }
  if (item.path.startsWith('/crm/contractors/')) {
    void router.push({ path: '/crm/contractors', query: { contractorId: item.path.split('/').pop() } })
    return
  }
  void router.push(item.path)
}

async function runCompose() {
  if (!composeForm.value.title.trim() || !composeForm.value.goal.trim()) {
    errorMsg.value = 'Укажите название документа и цель генерации'
    return
  }
  composeDraft.value = ''
  composing.value = true
  errorMsg.value = ''
  try {
    await requestSSE<{ type: string; delta?: string; error?: string }>(
      '/api/v1/ai/compose',
      {
        title: composeForm.value.title,
        goal: composeForm.value.goal,
        space: composeForm.value.space,
        doc_type: composeForm.value.doc_type,
        model_id: composeForm.value.model_id ?? 0,
        extra_notes: composeForm.value.extra_notes
      },
      (ev) => {
        if (ev.type === 'delta' && ev.delta) composeDraft.value += ev.delta
        if (ev.type === 'error' && ev.error) errorMsg.value = ev.error
      }
    )
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка генерации'
  } finally {
    composing.value = false
  }
}

async function saveDraftToDocs() {
  if (!composeDraft.value.trim()) return
  const doc = await requestJSON<{ id: number }>('/api/v1/docs/documents', {
    method: 'POST',
    body: JSON.stringify({
      space: composeForm.value.space || 'general',
      doc_type: composeForm.value.doc_type || 'sop',
      title: composeForm.value.title || 'Черновик ИИ',
      content: composeDraft.value,
      status: 'published',
      visibility: 'internal'
    })
  })
  await router.push({ path: '/docs', query: { docId: String(doc.id) } })
}

async function loadAdminAI() {
  const [mRes, pRes, uRes, aRes, wRes] = await Promise.allSettled([
    requestJSON<AIModel[]>('/api/v1/ai/models'),
    auth.hasPerm('ai.admin') ? requestJSON<AIProvider[]>('/api/v1/ai/providers') : Promise.resolve([]),
    auth.hasPerm('ai.admin') ? requestJSON<AIUsageLog[]>('/api/v1/ai/usage?limit=50') : Promise.resolve([]),
    auth.hasPerm('admin.model_access.manage')
      ? requestJSON<ModelAccessPolicy[]>('/api/v1/admin/model-access')
      : Promise.resolve([]),
    requestJSON<UserBrief[]>('/api/v1/crm/workers')
  ])
  if (mRes.status === 'fulfilled') models.value = mRes.value ?? []
  if (pRes.status === 'fulfilled') providers.value = pRes.value ?? []
  if (uRes.status === 'fulfilled') usageLogs.value = uRes.value ?? []
  if (aRes.status === 'fulfilled') accessPolicies.value = aRes.value ?? []
  if (wRes.status === 'fulfilled') workers.value = wRes.value ?? []

  if (!modelForm.value.provider_id && providers.value.length > 0) {
    modelForm.value.provider_id = providers.value[0].id
  }
}

async function saveProvider() {
  await requestJSON('/api/v1/ai/providers', {
    method: 'POST',
    body: JSON.stringify(providerForm.value)
  })
  providerForm.value = {
    name: '',
    kind: 'openai_compat',
    base_url: 'https://api.openai.com',
    api_key: '',
    active: true,
    is_default: false
  }
  await loadAdminAI()
}

async function saveModel() {
  if (!modelForm.value.provider_id || !modelForm.value.model_id.trim()) return
  await requestJSON('/api/v1/ai/models', {
    method: 'POST',
    body: JSON.stringify(modelForm.value)
  })
  modelForm.value.model_id = ''
  modelForm.value.name = ''
  await loadAdminAI()
}

function openAccessModal(m: AIModel) {
  accessTargetModel.value = m
  const pol = accessPolicies.value.find((p) => p.model_id === m.id)
  accessOpenToAll.value = pol?.open_to_all ?? m.open_to_all
  accessUserIds.value = [...(pol?.user_ids ?? [])]
  accessModalOpen.value = true
}

async function saveModelAccess() {
  if (!accessTargetModel.value) return
  await requestJSON(`/api/v1/admin/model-access/${accessTargetModel.value.id}`, {
    method: 'PUT',
    body: JSON.stringify({
      open_to_all: accessOpenToAll.value,
      user_ids: accessUserIds.value
    })
  })
  accessModalOpen.value = false
  await loadAdminAI()
}

const modelColumns: DataTableColumns<AIModel> = [
  { title: 'Название', key: 'name' },
  { title: 'Model ID', key: 'model_id' },
  { title: 'Провайдер', key: 'provider_name' },
  { title: 'Тип', key: 'capability' },
  {
    title: 'Доступ',
    key: 'open_to_all',
    render: (r) =>
      h(
        NTag,
        { size: 'small', type: r.open_to_all ? 'success' : 'warning' },
        { default: () => (r.open_to_all ? 'Открыта всем' : 'По политике') }
      )
  },
  {
    title: 'Действия',
    key: 'actions',
    width: 190,
    render: (r) =>
      h(NSpace, { size: 6 }, () => [
        auth.hasPerm('admin.model_access.manage') &&
          h(NButton, { size: 'tiny', onClick: () => openAccessModal(r) }, { default: () => 'Доступ' }),
        h(
          NButton,
          {
            size: 'tiny',
            type: 'error',
            ghost: true,
            onClick: async () => {
              await requestJSON(`/api/v1/ai/models/${r.id}`, { method: 'DELETE' })
              await loadAdminAI()
            }
          },
          { default: () => 'Удалить' }
        )
      ])
  }
]

onMounted(() => {
  void loadAdminAI()
})
</script>

<template>
  <div class="ai-page">
    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>
    <NAlert v-if="statusMsg" type="success" closable @close="statusMsg = ''">{{ statusMsg }}</NAlert>

    <NTabs type="line">
      <NTabPane name="search" tab="Гибридный поиск (BM25 + Vector)">
        <NSpace style="margin-bottom: 12px">
          <NInput
            v-model:value="searchQ"
            placeholder="Код ошибки котла, телефон, регламент..."
            style="width: 380px"
            @keyup.enter="runSearch"
          />
          <NButton type="primary" :loading="searching" @click="runSearch">Найти</NButton>
          <NButton
            v-if="auth.hasPerm('ai.admin')"
            secondary
            :loading="reindexing"
            @click="runReindex"
          >
            Переиндексировать КБ и CRM
          </NButton>
        </NSpace>

        <NSpace vertical>
          <NCard
            v-for="item in searchResults"
            :key="`${item.source_type}-${item.source_id}-${item.anchor}`"
            size="small"
            hoverable
            style="cursor: pointer"
            @click="openResult(item)"
          >
            <NSpace justify="space-between">
              <strong>{{ item.title }}</strong>
              <NTag size="tiny">{{ item.source_type }} · score {{ item.score.toFixed(3) }}</NTag>
            </NSpace>
            <div style="margin-top: 6px; color: #334155">{{ item.snippet }}</div>
          </NCard>
        </NSpace>
      </NTabPane>

      <NTabPane v-if="auth.hasPerm('ai.compose')" name="compose" tab="Compose (Генерация документов)">
        <div class="compose-grid">
          <NCard title="Параметры документа" size="small">
            <NForm label-placement="top">
              <NFormItem label="Заголовок">
                <NInput v-model:value="composeForm.title" placeholder="Регламент ТО газового котла..." />
              </NFormItem>
              <NFormItem label="Задача для ИИ">
                <NInput
                  v-model:value="composeForm.goal"
                  type="textarea"
                  :rows="3"
                  placeholder="Составить пошаговый чек-лист диагностики и контрольных замеров..."
                />
              </NFormItem>
              <NSpace>
                <NFormItem label="Пространство" style="width: 150px">
                  <NInput v-model:value="composeForm.space" />
                </NFormItem>
                <NFormItem label="Тип" style="width: 170px">
                  <NSelect
                    v-model:value="composeForm.doc_type"
                    :options="[
                      { label: 'СОП / Регламент', value: 'sop' },
                      { label: 'Инструкция котла', value: 'equipment_manual' },
                      { label: 'Чек-лист', value: 'checklist' },
                      { label: 'Статья', value: 'article' }
                    ]"
                  />
                </NFormItem>
              </NSpace>
              <NFormItem label="Модель LLM">
                <NSelect
                  v-model:value="composeForm.model_id"
                  clearable
                  placeholder="По умолчанию"
                  :options="
                    models
                      .filter((m) => m.capability === 'chat')
                      .map((m) => ({ label: m.name || m.model_id, value: m.id }))
                  "
                />
              </NFormItem>
              <NSpace>
                <NButton type="primary" :loading="composing" @click="runCompose">Сгенерировать</NButton>
                <NButton
                  v-if="composeDraft && auth.hasPerm('docs.create')"
                  type="success"
                  secondary
                  @click="saveDraftToDocs"
                >
                  Сохранить в Документацию
                </NButton>
              </NSpace>
            </NForm>
          </NCard>

          <NCard title="Предпросмотр (SSE поток)" size="small">
            <div v-if="composeDraft" class="markdown-body" v-html="renderedDraft" />
            <div v-else style="color: #94a3b8">Заполните цель и нажмите «Сгенерировать»</div>
          </NCard>
        </div>
      </NTabPane>

      <NTabPane v-if="auth.hasPerm('ai.admin')" name="admin" tab="Провайдеры и Модели">
        <NCard title="Добавить / обновить провайдера" size="small" style="margin-bottom: 12px">
          <NSpace>
            <NInput v-model:value="providerForm.name" placeholder="Название" style="width: 150px" />
            <NSelect
              v-model:value="providerForm.kind"
              style="width: 160px"
              :options="[
                { label: 'OpenAI', value: 'openai' },
                { label: 'OpenAI Compat', value: 'openai_compat' },
                { label: 'Anthropic', value: 'anthropic' },
                { label: 'Gemini', value: 'gemini' },
                { label: 'Ollama (Local)', value: 'ollama' }
              ]"
            />
            <NInput v-model:value="providerForm.base_url" placeholder="Base URL" style="width: 240px" />
            <NInput v-model:value="providerForm.api_key" type="password" placeholder="API Key" style="width: 180px" />
            <NButton type="primary" @click="saveProvider">Сохранить провайдера</NButton>
          </NSpace>
          <NSpace style="margin-top: 8px">
            <NTag v-for="p in providers" :key="p.id">
              {{ p.name }} ({{ p.kind }}) {{ p.has_api_key ? '🔑' : '' }}
            </NTag>
          </NSpace>
        </NCard>

        <NCard title="Добавить модель" size="small" style="margin-bottom: 12px">
          <NSpace align="center">
            <NSelect
              v-model:value="modelForm.provider_id"
              style="width: 180px"
              placeholder="Провайдер"
              :options="providers.map((p) => ({ label: p.name, value: p.id }))"
            />
            <NInput v-model:value="modelForm.name" placeholder="Отображаемое имя" style="width: 160px" />
            <NInput v-model:value="modelForm.model_id" placeholder="gpt-4o / qwen2.5..." style="width: 180px" />
            <NSelect
              v-model:value="modelForm.capability"
              style="width: 140px"
              :options="[
                { label: 'Чат / Агент', value: 'chat' },
                { label: 'Эмбеддинги', value: 'embedding' },
                { label: 'Распознавание речи (STT)', value: 'stt' }
              ]"
            />
            <NButton type="primary" @click="saveModel">Добавить модель</NButton>
          </NSpace>
        </NCard>

        <NDataTable :columns="modelColumns" :data="models" :row-key="(r) => r.id" />
      </NTabPane>

      <NTabPane v-if="auth.hasPerm('ai.admin')" name="usage" tab="Журнал использования">
        <NDataTable
          :data="usageLogs"
          :row-key="(r) => r.id"
          :columns="[
            { title: 'Время', key: 'created_at', render: (r: AIUsageLog) => formatDateTime(r.created_at) },
            { title: 'Модель', key: 'model_name' },
            { title: 'Операция', key: 'operation' },
            { title: 'Токены (in/out)', key: 'tokens', render: (r: AIUsageLog) => `${r.prompt_tokens} / ${r.completion_tokens}` },
            { title: 'Latency (мс)', key: 'latency_ms' },
            { title: 'Статус', key: 'status' }
          ]"
        />
      </NTabPane>
    </NTabs>

    <NModal v-model:show="accessModalOpen" preset="card" title="Политика доступа к модели" style="width: 460px">
      <NForm label-placement="top">
        <NFormItem label="Доступна всем авторизованным">
          <NSwitch v-model:value="accessOpenToAll" />
        </NFormItem>
        <NFormItem v-if="!accessOpenToAll" label="Разрешённые пользователи">
          <NSelect
            v-model:value="accessUserIds"
            multiple
            filterable
            :options="workers.map((w) => ({ label: w.full_name || w.username, value: w.id }))"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="accessModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveModelAccess">Сохранить доступ</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.ai-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.compose-grid {
  display: grid;
  grid-template-columns: 400px 1fr;
  gap: 14px;
}
.markdown-body :deep(pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 10px;
  border-radius: 6px;
}
@media (max-width: 1000px) {
  .compose-grid {
    grid-template-columns: 1fr;
  }
}
</style>
