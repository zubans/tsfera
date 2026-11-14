<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NLayout,
  NLayoutSider,
  NLayoutContent,
  NSpace,
  NButton,
  NInput,
  NSelect,
  NTag,
  NCard,
  NCollapse,
  NCollapseItem,
  NModal,
  NAlert
} from 'naive-ui'
import { requestJSON, requestSSE, ApiError } from '@/shared/api/http'
import { renderMarkdown } from '@/shared/utils/markdown'
import { formatDateTime } from '@/shared/utils/format'
import type {
  Conversation,
  ChatMessage,
  ToolStep,
  SourceRef,
  AIModel,
  ConversationShare
} from '@/shared/types/domain'

const route = useRoute()
const router = useRouter()

const isSharedMode = computed(() => Boolean(route.params.token))
const conversations = ref<Conversation[]>([])
const activeConv = ref<Conversation | null>(null)
const models = ref<AIModel[]>([])

const selectedMode = ref<'agent' | 'rag'>('agent')
const selectedModelId = ref<number | null>(null)
const promptText = ref('')
const streaming = ref(false)
const streamingText = ref('')
const streamingSteps = ref<ToolStep[]>([])
const streamingSources = ref<SourceRef[]>([])
const errorMsg = ref('')

const shareModalOpen = ref(false)
const shareData = ref<ConversationShare | null>(null)
const shareOrigin = typeof window !== 'undefined' ? window.location.origin : ''
let abortCtrl: AbortController | null = null

async function loadWorkspace() {
  if (route.params.token) {
    activeConv.value = await requestJSON<Conversation>(
      `/api/v1/chat/shared/${encodeURIComponent(String(route.params.token))}`
    )
    return
  }
  const [convsRes, modRes] = await Promise.allSettled([
    requestJSON<Conversation[]>('/api/v1/chat/conversations'),
    requestJSON<AIModel[]>('/api/v1/ai/models?capability=chat')
  ])
  if (convsRes.status === 'fulfilled') conversations.value = convsRes.value ?? []
  if (modRes.status === 'fulfilled') models.value = modRes.value ?? []

  if (!selectedModelId.value && models.value.length > 0) {
    const def = models.value.find((m) => m.is_default) || models.value[0]
    selectedModelId.value = def.id
  }
  if (route.query.convId) {
    await openConversation(Number(route.query.convId))
  } else if (!activeConv.value && conversations.value.length > 0) {
    await openConversation(conversations.value[0].id)
  }
}

async function openConversation(id: number) {
  const conv = await requestJSON<Conversation>(`/api/v1/chat/conversations/${id}`)
  activeConv.value = conv
  selectedMode.value = conv.mode || 'agent'
  if (conv.model_id) selectedModelId.value = conv.model_id
}

async function createConversation() {
  const conv = await requestJSON<Conversation>('/api/v1/chat/conversations', {
    method: 'POST',
    body: JSON.stringify({
      title: 'Новый диалог',
      mode: selectedMode.value,
      model_id: selectedModelId.value ?? 0
    })
  })
  conversations.value = [conv, ...conversations.value]
  await openConversation(conv.id)
}

async function deleteConversation(id: number) {
  await requestJSON(`/api/v1/chat/conversations/${id}`, { method: 'DELETE' })
  conversations.value = conversations.value.filter((c) => c.id !== id)
  if (activeConv.value?.id === id) {
    activeConv.value = null
    if (conversations.value.length > 0) await openConversation(conversations.value[0].id)
  }
}

async function sendMessage() {
  if (!promptText.value.trim() || streaming.value) return
  if (!activeConv.value) {
    await createConversation()
  }
  if (!activeConv.value) return

  const content = promptText.value.trim()
  promptText.value = ''
  errorMsg.value = ''
  streaming.value = true
  streamingText.value = ''
  streamingSteps.value = []
  streamingSources.value = []
  abortCtrl = new AbortController()

  try {
    await requestSSE<{
      type: string
      message?: ChatMessage
      step?: ToolStep
      delta?: string
      sources?: SourceRef[]
      error?: string
    }>(
      `/api/v1/chat/conversations/${activeConv.value.id}/messages?stream=true`,
      {
        content,
        mode: selectedMode.value,
        model_id: selectedModelId.value ?? 0
      },
      (ev) => {
        if (ev.type === 'user' && ev.message) {
          activeConv.value!.messages = [...(activeConv.value!.messages ?? []), ev.message]
        } else if (ev.type === 'step' && ev.step) {
          streamingSteps.value = [...streamingSteps.value, ev.step]
        } else if (ev.type === 'delta' && ev.delta) {
          streamingText.value += ev.delta
        } else if (ev.type === 'sources' && ev.sources) {
          streamingSources.value = ev.sources
        } else if (ev.type === 'done' && ev.message) {
          activeConv.value!.messages = [...(activeConv.value!.messages ?? []), ev.message]
          streamingText.value = ''
          streamingSteps.value = []
          streamingSources.value = []
        } else if (ev.type === 'error' && ev.error) {
          errorMsg.value = ev.error
        }
      },
      abortCtrl.signal
    )
    await openConversation(activeConv.value.id)
  } catch (err) {
    if ((err as Error)?.name !== 'AbortError') {
      errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка чата с ИИ'
    }
  } finally {
    streaming.value = false
    abortCtrl = null
  }
}

function stopStreaming() {
  abortCtrl?.abort()
  streaming.value = false
}

async function createShareLink() {
  if (!activeConv.value) return
  shareData.value = await requestJSON<ConversationShare>(
    `/api/v1/chat/conversations/${activeConv.value.id}/share`,
    { method: 'POST', body: JSON.stringify({ ttl_hours: 168 }) }
  )
  shareModalOpen.value = true
}

async function revokeShareLink() {
  if (!activeConv.value) return
  await requestJSON(`/api/v1/chat/conversations/${activeConv.value.id}/share`, {
    method: 'DELETE'
  })
  shareData.value = null
  shareModalOpen.value = false
}

function openSource(src: SourceRef) {
  if (src.path?.startsWith('/docs/')) {
    void router.push({
      path: '/docs',
      query: { docId: src.path.split('/').pop(), anchor: src.anchor || undefined }
    })
    return
  }
  if (src.path?.startsWith('/crm/contractors/')) {
    void router.push({ path: '/crm/contractors', query: { contractorId: src.path.split('/').pop() } })
    return
  }
  if (src.path) void router.push(src.path)
}

function openBoundEntity(conv: Conversation) {
  if (conv.entity_type === 'contractor' && conv.entity_id) {
    void router.push({ path: '/crm/contractors', query: { contractorId: String(conv.entity_id) } })
  } else if (conv.entity_type === 'document' && conv.entity_id) {
    void router.push({ path: '/docs', query: { docId: String(conv.entity_id) } })
  } else if (conv.entity_type === 'application') {
    void router.push('/crm/calendar')
  }
}

watch(
  () => route.query.convId,
  (id) => {
    if (id && !isSharedMode.value) void openConversation(Number(id))
  }
)

onMounted(() => {
  void loadWorkspace()
})
</script>

<template>
  <NLayout has-sider class="chat-shell">
    <NLayoutSider v-if="!isSharedMode" bordered :width="280" style="background: #fff; padding: 12px">
      <NSpace vertical :size="10">
        <NButton type="primary" block @click="createConversation">+ Новый диалог</NButton>
        <div class="conv-list">
          <div
            v-for="conv in conversations"
            :key="conv.id"
            class="conv-item"
            :class="{ active: activeConv?.id === conv.id }"
            @click="openConversation(conv.id)"
          >
            <div class="conv-title">{{ conv.title || `Диалог #${conv.id}` }}</div>
            <div class="conv-meta">
              <NTag size="tiny">{{ conv.mode }}</NTag>
              <span>{{ formatDateTime(conv.updated_at) }}</span>
              <NButton
                size="tiny"
                text
                type="error"
                @click.stop="deleteConversation(conv.id)"
              >
                ×
              </NButton>
            </div>
          </div>
        </div>
      </NSpace>
    </NLayoutSider>

    <NLayoutContent style="padding: 16px; display: flex; flex-direction: column; gap: 12px; background: #f8fafc">
      <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

      <div v-if="activeConv" class="chat-header">
        <NSpace align="center">
          <strong>{{ activeConv.title }}</strong>
          <NTag
            v-if="activeConv.entity_type && activeConv.entity_id"
            type="info"
            size="small"
            style="cursor: pointer"
            @click="openBoundEntity(activeConv)"
          >
            Контекст: {{ activeConv.entity_type }} #{{ activeConv.entity_id }}
          </NTag>
        </NSpace>
        <NSpace v-if="!isSharedMode" align="center">
          <NSelect
            v-model:value="selectedMode"
            size="small"
            style="width: 210px"
            :options="[
              { label: 'Агент (CRM + КБ инструменты)', value: 'agent' },
              { label: 'RAG (только по базе знаний)', value: 'rag' }
            ]"
          />
          <NSelect
            v-model:value="selectedModelId"
            size="small"
            style="width: 190px"
            placeholder="Модель"
            :options="models.map((m) => ({ label: m.name || m.model_id, value: m.id }))"
          />
          <NButton size="small" secondary @click="createShareLink">Поделиться</NButton>
        </NSpace>
      </div>

      <div class="messages-scroll">
        <template v-if="activeConv?.messages?.length">
          <div
            v-for="msg in activeConv.messages"
            :key="msg.id"
            class="msg-bubble"
            :class="msg.role"
          >
            <div class="msg-role">{{ msg.role === 'user' ? 'Вы' : 'Ассистент' }}</div>
            <NCollapse v-if="msg.steps?.length" style="margin-bottom: 8px">
              <NCollapseItem :title="`Вызовы инструментов (${msg.steps.length})`" name="steps">
                <NCard v-for="(st, i) in msg.steps" :key="i" size="small" style="margin-bottom: 6px">
                  <strong>{{ st.name }}</strong> ({{ st.duration_ms }} мс)
                  <div v-if="st.summary">{{ st.summary }}</div>
                  <div v-if="st.error" style="color: #dc2626">{{ st.error }}</div>
                </NCard>
              </NCollapseItem>
            </NCollapse>

            <div class="markdown-body" v-html="renderMarkdown(msg.content)" />

            <NSpace v-if="msg.sources?.length" :size="6" style="margin-top: 8px">
              <NTag
                v-for="(s, idx) in msg.sources"
                :key="idx"
                size="small"
                type="info"
                style="cursor: pointer"
                @click="openSource(s)"
              >
                📄 {{ s.title }}
              </NTag>
            </NSpace>
          </div>
        </template>

        <div v-if="streaming" class="msg-bubble assistant">
          <div class="msg-role">Ассистент (печатает...)</div>
          <div v-for="(st, i) in streamingSteps" :key="i" class="step-inline">
            ⚙ {{ st.name }} — {{ st.summary }} ({{ st.duration_ms }} мс)
          </div>
          <div class="markdown-body" v-html="renderMarkdown(streamingText || '...')" />
        </div>
      </div>

      <div v-if="!isSharedMode" class="composer-bar">
        <NInput
          v-model:value="promptText"
          type="textarea"
          :rows="2"
          placeholder="Спросите про клиента, заявку, ошибку котла или регламент..."
          @keydown.ctrl.enter.prevent="sendMessage"
          @keydown.meta.enter.prevent="sendMessage"
        />
        <NSpace vertical>
          <NButton v-if="!streaming" type="primary" @click="sendMessage">Отправить</NButton>
          <NButton v-else type="error" @click="stopStreaming">Остановить</NButton>
        </NSpace>
      </div>
    </NLayoutContent>

    <NModal v-model:show="shareModalOpen" preset="card" title="Публичная ссылка на диалог" style="width: 520px">
      <div v-if="shareData">
        <div>Ссылка активна только для просмотра:</div>
        <NInput
          readonly
          :value="`${shareOrigin}/chat/shared/${shareData.share_token}`"
          style="margin: 10px 0"
        />
        <NButton type="error" ghost size="small" @click="revokeShareLink">Отозвать ссылку</NButton>
      </div>
    </NModal>
  </NLayout>
</template>

<style scoped>
.chat-shell {
  height: calc(100vh - 96px);
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  overflow: hidden;
}
.conv-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.conv-item {
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid #e2e8f0;
  cursor: pointer;
}
.conv-item.active {
  background: #eff6ff;
  border-color: #3b82f6;
}
.conv-title {
  font-weight: 600;
  font-size: 13px;
}
.conv-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  color: #64748b;
  margin-top: 4px;
}
.chat-header {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  background: #fff;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid #e2e8f0;
}
.messages-scroll {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.msg-bubble {
  max-width: 85%;
  padding: 12px 14px;
  border-radius: 10px;
  background: #fff;
  border: 1px solid #e2e8f0;
}
.msg-bubble.user {
  align-self: flex-end;
  background: #eff6ff;
  border-color: #bfdbfe;
}
.msg-role {
  font-size: 11px;
  font-weight: 600;
  color: #64748b;
  margin-bottom: 4px;
}
.step-inline {
  font-size: 12px;
  color: #475569;
  margin-bottom: 4px;
}
.composer-bar {
  display: grid;
  grid-template-columns: 1fr 120px;
  gap: 10px;
}
.markdown-body :deep(pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 8px;
  border-radius: 6px;
}
</style>
