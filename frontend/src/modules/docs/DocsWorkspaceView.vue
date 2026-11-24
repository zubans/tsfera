<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NLayout,
  NLayoutSider,
  NLayoutContent,
  NSpace,
  NInput,
  NSelect,
  NButton,
  NTag,
  NAlert,
  NCard,
  NTabs,
  NTabPane,
  NModal,
  NForm,
  NFormItem,
  NDivider,
  NSpin
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { renderMarkdown } from '@/shared/utils/markdown'
import { formatDateTime } from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import type {
  DocDocument,
  DocComment,
  DocRevision,
  DocRevisionDetail,
  SpaceSummary
} from '@/shared/types/domain'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const spaces = ref<SpaceSummary[]>([])
const documents = ref<DocDocument[]>([])
const selectedSpace = ref('')
const selectedDocType = ref('')
const searchQ = ref('')
const loadingList = ref(false)

const currentDoc = ref<DocDocument | null>(null)
const comments = ref<DocComment[]>([])
const revisions = ref<DocRevision[]>([])
const previewRev = ref<DocRevisionDetail | null>(null)

const editing = ref(false)
const editForm = ref({
  title: '',
  space: 'general',
  doc_type: 'article',
  status: 'published',
  visibility: 'internal',
  tagsText: '',
  content: '',
  diff_summary: '',
  expected_version: 0
})

const conflictError = ref('')
const errorMsg = ref('')
const saving = ref(false)

const newCommentQuote = ref('')
const newCommentBody = ref('')
const linkForm = ref({ entity_type: 'contractor', entity_id: '', note: '' })

const createModalOpen = ref(false)
const createForm = ref({
  space: 'general',
  doc_type: 'sop',
  title: '',
  visibility: 'internal',
  tagsText: '',
  content: '# Заголовок\n\nТекст регламента...'
})

const IMPORT_EXTENSIONS = ['.docx', '.xlsx', '.pdf']
const importModalOpen = ref(false)
const importSpace = ref('general')
const importDocType = ref('sop')
const importTitle = ref('')
const importFile = ref<File | null>(null)
const importFileInput = ref<HTMLInputElement | null>(null)
const importError = ref('')
const importDragOver = ref(false)
const workspaceDragDepth = ref(0)
const workspaceDragOver = computed(() => workspaceDragDepth.value > 0)
const uploading = ref(false)

const renderedHtml = computed(() => renderMarkdown(currentDoc.value?.content || ''))

const lightboxOpen = ref(false)
const lightboxSrc = ref('')
const lightboxTitle = ref('')
const lightboxZoom = ref(100)
const lightboxFit = ref(true)

function openImageLightbox(src: string, title = 'Изображение') {
  lightboxSrc.value = src
  lightboxTitle.value = title || 'Изображение'
  lightboxZoom.value = 100
  lightboxFit.value = true
  lightboxOpen.value = true
}

function zoomInLightbox() {
  lightboxFit.value = false
  lightboxZoom.value = Math.min(400, lightboxZoom.value + 25)
}

function zoomOutLightbox() {
  lightboxFit.value = false
  lightboxZoom.value = Math.max(50, lightboxZoom.value - 25)
}

function setLightboxActualSize() {
  lightboxFit.value = false
  lightboxZoom.value = 100
}

function fitLightboxToScreen() {
  lightboxFit.value = true
  lightboxZoom.value = 100
}

function onLightboxWheel(ev: WheelEvent) {
  if (!ev.ctrlKey && !ev.metaKey) return
  ev.preventDefault()
  if (ev.deltaY < 0) {
    zoomInLightbox()
  } else {
    zoomOutLightbox()
  }
}

function onMarkdownClick(ev: MouseEvent) {
  const target = ev.target as HTMLElement | null
  if (!target || target.tagName !== 'IMG') return
  const img = target as HTMLImageElement
  const src = img.getAttribute('src') || ''
  if (!src) return
  openImageLightbox(src, img.getAttribute('alt') || 'Схема')
}

const docTypeOptions = [
  { label: 'Все типы', value: '' },
  { label: 'Статья (article)', value: 'article' },
  { label: 'Регламент / СОП (sop)', value: 'sop' },
  { label: 'Инструкция к котлу (equipment_manual)', value: 'equipment_manual' },
  { label: 'Чек-лист (checklist)', value: 'checklist' },
  { label: 'ADR', value: 'adr' },
  { label: 'Заметка (note)', value: 'note' }
]

async function loadCatalog() {
  loadingList.value = true
  try {
    const params = new URLSearchParams()
    if (selectedSpace.value) params.set('space', selectedSpace.value)
    if (selectedDocType.value) params.set('doc_type', selectedDocType.value)
    if (searchQ.value.trim()) params.set('q', searchQ.value.trim())
    const [spRes, docRes] = await Promise.allSettled([
      requestJSON<SpaceSummary[]>('/api/v1/docs/spaces'),
      requestJSON<DocDocument[]>(`/api/v1/docs/documents?${params}`)
    ])
    if (spRes.status === 'fulfilled') spaces.value = spRes.value ?? []
    if (docRes.status === 'fulfilled') documents.value = docRes.value ?? []
  } finally {
    loadingList.value = false
  }
}

async function openDocument(id: number, anchor?: string) {
  errorMsg.value = ''
  conflictError.value = ''
  editing.value = false
  previewRev.value = null
  try {
    const [doc, cmts, revs] = await Promise.all([
      requestJSON<DocDocument>(`/api/v1/docs/documents/${id}`),
      requestJSON<DocComment[]>(`/api/v1/docs/documents/${id}/comments`),
      requestJSON<DocRevision[]>(`/api/v1/docs/documents/${id}/revisions`)
    ])
    currentDoc.value = doc
    comments.value = cmts ?? []
    revisions.value = revs ?? []
    if (anchor) {
      setTimeout(() => {
        document.getElementById(anchor)?.scrollIntoView({ behavior: 'smooth' })
      }, 60)
    }
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Не удалось загрузить документ'
  }
}

function startEditing() {
  if (!currentDoc.value) return
  conflictError.value = ''
  editForm.value = {
    title: currentDoc.value.title,
    space: currentDoc.value.space,
    doc_type: currentDoc.value.doc_type,
    status: currentDoc.value.status,
    visibility: currentDoc.value.visibility,
    tagsText: (currentDoc.value.tags ?? []).join(', '),
    content: currentDoc.value.content,
    diff_summary: '',
    expected_version: currentDoc.value.version
  }
  editing.value = true
}

async function saveEdits() {
  if (!currentDoc.value) return
  saving.value = true
  conflictError.value = ''
  errorMsg.value = ''
  try {
    const tags = editForm.value.tagsText
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const updated = await requestJSON<DocDocument>(`/api/v1/docs/documents/${currentDoc.value.id}`, {
      method: 'PUT',
      body: JSON.stringify({
        space: editForm.value.space,
        doc_type: editForm.value.doc_type,
        title: editForm.value.title,
        content: editForm.value.content,
        status: editForm.value.status,
        visibility: editForm.value.visibility,
        tags,
        diff_summary: editForm.value.diff_summary,
        expected_version: editForm.value.expected_version
      })
    })
    currentDoc.value = updated
    editing.value = false
    await Promise.all([loadCatalog(), openDocument(updated.id)])
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      conflictError.value = err.message || 'Конфликт версий: документ был изменён другим пользователем.'
    } else {
      errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка сохранения документа'
    }
  } finally {
    saving.value = false
  }
}

async function createDocument() {
  saving.value = true
  errorMsg.value = ''
  try {
    const created = await requestJSON<DocDocument>('/api/v1/docs/documents', {
      method: 'POST',
      body: JSON.stringify({
        space: createForm.value.space,
        doc_type: createForm.value.doc_type,
        title: createForm.value.title,
        visibility: createForm.value.visibility,
        tags: createForm.value.tagsText
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
        content: createForm.value.content,
        status: 'published'
      })
    })
    createModalOpen.value = false
    await loadCatalog()
    await openDocument(created.id)
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : 'Ошибка создания документа'
  } finally {
    saving.value = false
  }
}

async function deleteCurrentDocument() {
  if (!currentDoc.value) return
  await requestJSON(`/api/v1/docs/documents/${currentDoc.value.id}`, { method: 'DELETE' })
  currentDoc.value = null
  await loadCatalog()
}

function captureSelectionQuote() {
  const sel = window.getSelection()?.toString().trim()
  if (sel) newCommentQuote.value = sel.slice(0, 300)
}

async function addComment() {
  if (!currentDoc.value || !newCommentBody.value.trim()) return
  await requestJSON(`/api/v1/docs/documents/${currentDoc.value.id}/comments`, {
    method: 'POST',
    body: JSON.stringify({
      quote: newCommentQuote.value.trim(),
      body: newCommentBody.value.trim()
    })
  })
  newCommentQuote.value = ''
  newCommentBody.value = ''
  comments.value = await requestJSON<DocComment[]>(`/api/v1/docs/documents/${currentDoc.value.id}/comments`)
}

async function toggleCommentResolved(c: DocComment) {
  if (!currentDoc.value) return
  await requestJSON(`/api/v1/docs/documents/${currentDoc.value.id}/comments/${c.id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ resolved: !c.resolved })
  })
  comments.value = await requestJSON<DocComment[]>(`/api/v1/docs/documents/${currentDoc.value.id}/comments`)
}

async function viewRevision(revId: number) {
  if (!currentDoc.value) return
  previewRev.value = await requestJSON<DocRevisionDetail>(
    `/api/v1/docs/documents/${currentDoc.value.id}/revisions/${revId}`
  )
}

async function restoreRevision(revId: number) {
  if (!currentDoc.value) return
  const doc = await requestJSON<DocDocument>(
    `/api/v1/docs/documents/${currentDoc.value.id}/revisions/${revId}/restore`,
    { method: 'POST' }
  )
  await openDocument(doc.id)
}

async function addEntityLink() {
  if (!currentDoc.value || !Number(linkForm.value.entity_id)) return
  await requestJSON(`/api/v1/docs/documents/${currentDoc.value.id}/links`, {
    method: 'POST',
    body: JSON.stringify({
      entity_type: linkForm.value.entity_type,
      entity_id: Number(linkForm.value.entity_id),
      note: linkForm.value.note
    })
  })
  linkForm.value.entity_id = ''
  linkForm.value.note = ''
  await openDocument(currentDoc.value.id)
}

async function removeEntityLink(linkId: number) {
  if (!currentDoc.value) return
  await requestJSON(`/api/v1/docs/links/${linkId}`, { method: 'DELETE' })
  await openDocument(currentDoc.value.id)
}

async function uploadAttachment(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !currentDoc.value) return
  const fd = new FormData()
  fd.append('file', file)
  await requestJSON(`/api/v1/docs/documents/${currentDoc.value.id}/attachments`, {
    method: 'POST',
    body: fd
  })
  input.value = ''
  await openDocument(currentDoc.value.id)
}

function selectImportFile(file: File | null) {
  importError.value = ''
  if (!file) {
    importFile.value = null
    return
  }
  const lower = file.name.toLowerCase()
  if (!IMPORT_EXTENSIONS.some((ext) => lower.endsWith(ext))) {
    importFile.value = null
    importError.value = 'Неподдерживаемый формат файла: выберите .docx, .xlsx или .pdf'
    return
  }
  importFile.value = file
}

function clearImportFile() {
  importFile.value = null
  importError.value = ''
  if (importFileInput.value) importFileInput.value.value = ''
}

function onImportInputChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  selectImportFile(input.files?.[0] ?? null)
}

function onModalDrop(ev: DragEvent) {
  importDragOver.value = false
  selectImportFile(ev.dataTransfer?.files?.[0] ?? null)
}

function hasDraggedFiles(ev: DragEvent): boolean {
  return Boolean(ev.dataTransfer?.types && Array.from(ev.dataTransfer.types).includes('Files'))
}

function onWorkspaceDragEnter(ev: DragEvent) {
  if (!auth.hasPerm('docs.create') || importModalOpen.value || !hasDraggedFiles(ev)) return
  workspaceDragDepth.value++
}

function onWorkspaceDragOver(ev: DragEvent) {
  if (!auth.hasPerm('docs.create') || importModalOpen.value || !hasDraggedFiles(ev)) return
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'copy'
}

function onWorkspaceDragLeave(ev: DragEvent) {
  if (!hasDraggedFiles(ev)) return
  workspaceDragDepth.value = Math.max(0, workspaceDragDepth.value - 1)
}

function onWorkspaceDrop(ev: DragEvent) {
  workspaceDragDepth.value = 0
  if (!auth.hasPerm('docs.create') || importModalOpen.value) return
  const file = ev.dataTransfer?.files?.[0] ?? null
  if (!file) return
  if (selectedSpace.value) importSpace.value = selectedSpace.value
  importModalOpen.value = true
  selectImportFile(file)
}

async function submitImportFile() {
  if (!importFile.value) return
  uploading.value = true
  importError.value = ''
  try {
    const fd = new FormData()
    fd.append('file', importFile.value)
    fd.append('space', importSpace.value)
    fd.append('doc_type', importDocType.value)
    if (importTitle.value.trim()) fd.append('title', importTitle.value.trim())
    const doc = await requestJSON<DocDocument>('/api/v1/docs/import', {
      method: 'POST',
      body: fd
    })
    importModalOpen.value = false
    clearImportFile()
    importTitle.value = ''
    await loadCatalog()
    await openDocument(doc.id)
  } catch (err) {
    importError.value = err instanceof ApiError ? err.message : 'Ошибка импорта файла'
  } finally {
    uploading.value = false
  }
}

async function discussWithAI() {
  if (!currentDoc.value) return
  const conv = await requestJSON<{ id: number }>('/api/v1/chat/conversations', {
    method: 'POST',
    body: JSON.stringify({
      title: `Документ: ${currentDoc.value.title}`,
      mode: 'rag',
      entity_type: 'document',
      entity_id: currentDoc.value.id
    })
  })
  await router.push({ path: '/chat', query: { convId: String(conv.id) } })
}

function scrollToAnchor(anchor: string) {
  document.getElementById(anchor)?.scrollIntoView({ behavior: 'smooth' })
}

watch(
  () => route.query.docId,
  (id) => {
    if (id) void openDocument(Number(id), route.query.anchor ? String(route.query.anchor) : undefined)
  }
)

onMounted(async () => {
  await loadCatalog()
  if (route.query.docId) {
    await openDocument(
      Number(route.query.docId),
      route.query.anchor ? String(route.query.anchor) : undefined
    )
  } else if (documents.value.length > 0) {
    await openDocument(documents.value[0].id)
  }
})
</script>

<template>
  <div
    class="docs-drop-host"
    @dragenter.prevent="onWorkspaceDragEnter"
    @dragover.prevent="onWorkspaceDragOver"
    @dragleave="onWorkspaceDragLeave"
    @drop.prevent="onWorkspaceDrop"
  >
    <div v-if="workspaceDragOver" class="workspace-drop-overlay">
      <div class="workspace-drop-card">
        Перетащите файл <strong>.docx</strong>, <strong>.xlsx</strong> или <strong>.pdf</strong> для импорта в базу знаний
      </div>
    </div>

    <NLayout has-sider class="docs-shell">
    <NLayoutSider bordered :width="300" style="background: #ffffff; padding: 12px">
      <NSpace vertical :size="10">
        <NSpace justify="space-between">
          <strong>База знаний</strong>
          <NSpace :size="6">
            <NButton
              v-if="auth.hasPerm('docs.create')"
              size="tiny"
              type="primary"
              @click="createModalOpen = true"
            >
              + Док
            </NButton>
            <NButton
              v-if="auth.hasPerm('docs.create')"
              size="tiny"
              secondary
              title="Импорт из .docx, .xlsx или .pdf"
              @click="
                () => {
                  importError = ''
                  importModalOpen = true
                }
              "
            >
              Импорт
            </NButton>
          </NSpace>
        </NSpace>

        <NInput
          v-model:value="searchQ"
          placeholder="Поиск документов..."
          clearable
          @keyup.enter="loadCatalog"
        />
        <NSelect
          :value="selectedSpace"
          placeholder="Пространство"
          :options="[
            { label: 'Все пространства', value: '' },
            ...spaces.map((s) => ({ label: `${s.space} (${s.count})`, value: s.space }))
          ]"
          @update:value="
            (v) => {
              selectedSpace = String(v)
              loadCatalog()
            }
          "
        />
        <NSelect
          :value="selectedDocType"
          :options="docTypeOptions"
          @update:value="
            (v) => {
              selectedDocType = String(v)
              loadCatalog()
            }
          "
        />

        <NSpin :show="loadingList">
          <div class="doc-list">
            <div
              v-for="doc in documents"
              :key="doc.id"
              class="doc-list-item"
              :class="{ active: currentDoc?.id === doc.id }"
              @click="openDocument(doc.id)"
            >
              <div class="doc-title">{{ doc.title }}</div>
              <div class="doc-meta">{{ doc.space }} · {{ doc.doc_type }} · v{{ doc.version }}</div>
            </div>
            <div v-if="documents.length === 0" class="muted">Нет документов</div>
          </div>
        </NSpin>
      </NSpace>
    </NLayoutSider>

    <NLayoutContent style="padding: 16px; background: #f8fafc">
      <NAlert v-if="errorMsg" type="error" closable style="margin-bottom: 12px" @close="errorMsg = ''">
        {{ errorMsg }}
      </NAlert>
      <NAlert v-if="conflictError" type="warning" title="Конфликт версий (HTTP 409)" style="margin-bottom: 12px">
        {{ conflictError }}
        <NButton
          size="tiny"
          type="warning"
          style="margin-left: 8px"
          @click="currentDoc && openDocument(currentDoc.id)"
        >
          Загрузить актуальную версию
        </NButton>
      </NAlert>

      <div v-if="currentDoc" class="doc-workspace">
        <div class="doc-main">
          <div class="doc-header">
            <div>
              <h2 style="margin: 0 0 6px">{{ currentDoc.title }}</h2>
              <NSpace :size="6">
                <NTag size="small" type="info">{{ currentDoc.space }}</NTag>
                <NTag size="small">{{ currentDoc.doc_type }}</NTag>
                <NTag size="small">v{{ currentDoc.version }}</NTag>
                <NTag size="small" type="warning">{{ currentDoc.visibility }}</NTag>
                <NTag v-for="t in currentDoc.tags" :key="t" size="tiny">#{{ t }}</NTag>
              </NSpace>
            </div>
            <NSpace>
              <NButton v-if="!editing && auth.hasPerm('docs.update')" secondary @click="startEditing">
                Редактировать
              </NButton>
              <a
                :href="`/api/v1/docs/context.md?document_id=${currentDoc.id}`"
                target="_blank"
                style="text-decoration: none"
              >
                <NButton secondary>context.md</NButton>
              </a>
              <NButton
                v-if="auth.hasPerm('chat.use')"
                type="info"
                secondary
                @click="discussWithAI"
              >
                Обсудить с ИИ
              </NButton>
              <NButton
                v-if="auth.hasPerm('docs.delete')"
                type="error"
                ghost
                @click="deleteCurrentDocument"
              >
                Удалить
              </NButton>
            </NSpace>
          </div>

          <NCard v-if="!editing" style="margin-top: 12px">
            <div v-if="currentDoc.outline?.length" class="outline-box">
              <strong>Оглавление:</strong>
              <span
                v-for="item in currentDoc.outline"
                :key="item.anchor"
                class="outline-link"
                :style="{ paddingLeft: `${(item.level - 1) * 10}px` }"
                @click="scrollToAnchor(item.anchor)"
              >
                {{ item.title }}
              </span>
            </div>
            <div
              class="markdown-body"
              @mouseup="captureSelectionQuote"
              @click="onMarkdownClick"
              v-html="renderedHtml"
            />
          </NCard>

          <NCard v-else title="Редактор Markdown" style="margin-top: 12px">
            <NForm label-placement="top">
              <div class="edit-row">
                <NFormItem label="Заголовок">
                  <NInput v-model:value="editForm.title" />
                </NFormItem>
                <NFormItem label="Пространство">
                  <NInput v-model:value="editForm.space" />
                </NFormItem>
                <NFormItem label="Видимость">
                  <NSelect
                    v-model:value="editForm.visibility"
                    :options="[
                      { label: 'internal', value: 'internal' },
                      { label: 'engineers', value: 'engineers' },
                      { label: 'dispatchers', value: 'dispatchers' },
                      { label: 'admins', value: 'admins' },
                      { label: 'public', value: 'public' }
                    ]"
                  />
                </NFormItem>
              </div>
              <NFormItem label="Теги (через запятую)">
                <NInput v-model:value="editForm.tagsText" />
              </NFormItem>
              <NFormItem label="Markdown">
                <NInput v-model:value="editForm.content" type="textarea" :rows="16" />
              </NFormItem>
              <NFormItem label="Комментарий к ревизии">
                <NInput v-model:value="editForm.diff_summary" placeholder="Что изменено..." />
              </NFormItem>
              <NSpace>
                <NButton type="primary" :loading="saving" @click="saveEdits">
                  Сохранить (expected v{{ editForm.expected_version }})
                </NButton>
                <NButton @click="editing = false">Отмена</NButton>
              </NSpace>
            </NForm>
          </NCard>
        </div>

        <div class="doc-sidebar">
          <NCard size="small">
            <NTabs type="line" size="small">
              <NTabPane name="comments" :tab="`Комментарии (${comments.length})`">
                <NSpace vertical :size="8">
                  <NInput
                    v-model:value="newCommentQuote"
                    size="small"
                    placeholder="Цитата (выделите текст в документе)"
                  />
                  <NInput
                    v-model:value="newCommentBody"
                    type="textarea"
                    :rows="2"
                    placeholder="Комментарий..."
                  />
                  <NButton size="small" type="primary" @click="addComment">Добавить</NButton>
                  <NDivider style="margin: 6px 0" />
                  <NCard v-for="c in comments" :key="c.id" size="small">
                    <div class="comment-head">
                      <strong>{{ c.author_name }}</strong>
                      <NTag :type="c.resolved ? 'success' : 'warning'" size="tiny">
                        {{ c.resolved ? 'Решено' : 'Открыт' }}
                      </NTag>
                    </div>
                    <blockquote v-if="c.quote" class="quote-box">{{ c.quote }}</blockquote>
                    <div>{{ c.body }}</div>
                    <NButton size="tiny" text @click="toggleCommentResolved(c)">
                      {{ c.resolved ? 'Открыть снова' : 'Отметить решённым' }}
                    </NButton>
                  </NCard>
                </NSpace>
              </NTabPane>

              <NTabPane name="links" :tab="`CRM и файлы (${(currentDoc.entity_links?.length || 0) + (currentDoc.attachments?.length || 0)})`">
                <NSpace vertical :size="10">
                  <strong>Связи с CRM:</strong>
                  <div v-for="lnk in currentDoc.entity_links" :key="lnk.id" class="link-row">
                    <span>
                      <NTag size="tiny">{{ lnk.entity_type }} #{{ lnk.entity_id }}</NTag>
                      {{ lnk.entity_title || lnk.note }}
                    </span>
                    <NButton size="tiny" text type="error" @click="removeEntityLink(lnk.id)">×</NButton>
                  </div>
                  <NSpace>
                    <NSelect
                      v-model:value="linkForm.entity_type"
                      size="small"
                      style="width: 120px"
                      :options="[
                        { label: 'Контрагент', value: 'contractor' },
                        { label: 'Заявка', value: 'application' },
                        { label: 'Акт ТО', value: 'certificate' },
                        { label: 'Оборудование', value: 'equipment' }
                      ]"
                    />
                    <NInput v-model:value="linkForm.entity_id" size="small" placeholder="ID" style="width: 70px" />
                    <NButton size="small" @click="addEntityLink">Привязать</NButton>
                  </NSpace>

                  <NDivider style="margin: 6px 0" />
                  <strong>Вложения:</strong>
                  <input type="file" @change="uploadAttachment" />
                  <div v-for="att in currentDoc.attachments" :key="att.id" class="link-row">
                    <NSpace :size="6" align="center">
                      <a
                        v-if="att.kind === 'schematic'"
                        :href="`/api/v1/docs/attachments/${att.id}/download?inline=1`"
                        @click.prevent="
                          openImageLightbox(
                            `/api/v1/docs/attachments/${att.id}/download?inline=1`,
                            att.page_number ? `${att.filename} (стр. ${att.page_number})` : att.filename
                          )
                        "
                      >
                        {{ att.filename }}
                      </a>
                      <a
                        v-else
                        :href="`/api/v1/docs/attachments/${att.id}/download`"
                        target="_blank"
                      >
                        {{ att.filename }}
                      </a>
                      <NTag v-if="att.kind === 'schematic'" size="tiny" type="info">
                        {{ att.page_number ? `Схема · стр. ${att.page_number}` : 'Схема' }}
                      </NTag>
                      <NTag v-if="att.analysis_status === 'pending'" size="tiny" type="warning">
                        Распознаётся…
                      </NTag>
                      <NTag v-else-if="att.analysis_status === 'done'" size="tiny" type="success">
                        В поиске
                      </NTag>
                      <NTag
                        v-else-if="att.analysis_status === 'failed'"
                        size="tiny"
                        type="error"
                        :title="att.analysis_error || 'Ошибка анализа схемы'"
                      >
                        Ошибка анализа
                      </NTag>
                    </NSpace>
                    <span class="muted">{{ Math.round(att.size_bytes / 1024) }} КБ</span>
                  </div>
                </NSpace>
              </NTabPane>

              <NTabPane name="revisions" :tab="`История (${revisions.length})`">
                <NSpace vertical :size="8">
                  <NCard v-for="r in revisions" :key="r.id" size="small">
                    <NSpace justify="space-between">
                      <strong>v{{ r.version }}</strong>
                      <NSpace :size="4">
                        <NTag v-if="r.compressed" size="tiny">gzip</NTag>
                        <NButton size="tiny" @click="viewRevision(r.id)">Просмотр</NButton>
                        <NButton size="tiny" type="warning" @click="restoreRevision(r.id)">
                          Откатить
                        </NButton>
                      </NSpace>
                    </NSpace>
                    <div class="muted">{{ formatDateTime(r.created_at) }} · {{ r.diff_summary || '—' }}</div>
                  </NCard>
                  <NCard v-if="previewRev" size="small" :title="`Предпросмотр v${previewRev.version}`">
                    <pre class="rev-preview">{{ previewRev.content }}</pre>
                  </NCard>
                </NSpace>
              </NTabPane>
            </NTabs>
          </NCard>
        </div>
      </div>
    </NLayoutContent>

    <NModal v-model:show="createModalOpen" preset="card" title="Новый документ" style="width: 600px">
      <NForm label-placement="top">
        <NFormItem label="Заголовок">
          <NInput v-model:value="createForm.title" />
        </NFormItem>
        <div class="edit-row">
          <NFormItem label="Пространство">
            <NInput v-model:value="createForm.space" />
          </NFormItem>
          <NFormItem label="Тип">
            <NSelect
              v-model:value="createForm.doc_type"
              :options="docTypeOptions.filter((o) => o.value !== '')"
            />
          </NFormItem>
          <NFormItem label="Видимость">
            <NSelect
              v-model:value="createForm.visibility"
              :options="[
                { label: 'internal', value: 'internal' },
                { label: 'engineers', value: 'engineers' },
                { label: 'dispatchers', value: 'dispatchers' },
                { label: 'admins', value: 'admins' }
              ]"
            />
          </NFormItem>
        </div>
        <NFormItem label="Содержимое Markdown">
          <NInput v-model:value="createForm.content" type="textarea" :rows="10" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="createModalOpen = false">Отмена</NButton>
          <NButton type="primary" :loading="saving" @click="createDocument">Создать</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="importModalOpen" preset="card" title="Импорт .docx, .xlsx или .pdf в Markdown" style="width: 460px">
      <NAlert v-if="importError" type="error" closable style="margin-bottom: 12px" @close="importError = ''">
        {{ importError }}
      </NAlert>
      <NForm label-placement="top">
        <NFormItem label="Пространство">
          <NInput v-model:value="importSpace" />
        </NFormItem>
        <NFormItem label="Тип документа">
          <NSelect
            v-model:value="importDocType"
            :options="docTypeOptions.filter((o) => o.value !== '')"
          />
        </NFormItem>
        <NFormItem label="Заголовок (необязательно)">
          <NInput v-model:value="importTitle" />
        </NFormItem>
        <NFormItem label="Файл .docx / .xlsx / .pdf">
          <div
            class="import-dropzone"
            :class="{ 'is-dragover': importDragOver, 'has-file': Boolean(importFile) }"
            @click="importFileInput?.click()"
            @dragenter.prevent="importDragOver = true"
            @dragover.prevent="importDragOver = true"
            @dragleave.prevent="importDragOver = false"
            @drop.prevent="onModalDrop"
          >
            <input
              ref="importFileInput"
              type="file"
              accept=".docx,.xlsx,.pdf"
              class="import-file-hidden"
              @change="onImportInputChange"
            />
            <template v-if="importFile">
              <div class="dropzone-file">
                <strong>{{ importFile.name }}</strong>
                <span class="muted">{{ Math.max(1, Math.round(importFile.size / 1024)) }} КБ</span>
              </div>
              <NButton size="tiny" text type="error" @click.stop="clearImportFile">
                Убрать файл
              </NButton>
            </template>
            <template v-else>
              <div class="dropzone-title">Перетащите файл сюда или нажмите для выбора</div>
              <div class="muted">Поддерживаются .docx, .xlsx и .pdf (до 50 МБ)</div>
            </template>
          </div>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="importModalOpen = false">Отмена</NButton>
          <NButton type="primary" :disabled="!importFile" :loading="uploading" @click="submitImportFile">
            Импортировать
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="lightboxOpen"
      preset="card"
      :title="lightboxTitle"
      style="width: 92vw; max-width: 1400px"
      :content-style="{ padding: '12px' }"
    >
      <template #header-extra>
        <NSpace :size="6" align="center" style="margin-right: 12px">
          <NButton size="tiny" :disabled="!lightboxFit && lightboxZoom <= 50" @click="zoomOutLightbox">−</NButton>
          <NButton size="tiny" :type="lightboxFit ? 'primary' : 'default'" @click="fitLightboxToScreen">
            По экрану
          </NButton>
          <NButton size="tiny" :type="!lightboxFit && lightboxZoom === 100 ? 'primary' : 'default'" @click="setLightboxActualSize">
            {{ lightboxFit ? '100%' : `${lightboxZoom}%` }}
          </NButton>
          <NButton size="tiny" :disabled="!lightboxFit && lightboxZoom >= 400" @click="zoomInLightbox">+</NButton>
          <NButton
            size="tiny"
            tag="a"
            :href="lightboxSrc"
            target="_blank"
            rel="noopener noreferrer"
          >
            Оригинал ↗
          </NButton>
        </NSpace>
      </template>
      <div class="lightbox-viewport" @wheel="onLightboxWheel">
        <img
          :src="lightboxSrc"
          :alt="lightboxTitle"
          class="lightbox-img"
          :class="{ 'is-fit': lightboxFit }"
          :style="lightboxFit ? undefined : { width: `${lightboxZoom}%` }"
          @click="lightboxFit ? setLightboxActualSize() : fitLightboxToScreen()"
        />
      </div>
    </NModal>
  </NLayout>
  </div>
</template>

<style scoped>
.docs-drop-host {
  position: relative;
}
.workspace-drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 40;
  background: rgba(37, 99, 235, 0.09);
  border: 2px dashed #2563eb;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}
.workspace-drop-card {
  background: #ffffff;
  color: #1e293b;
  padding: 16px 24px;
  border-radius: 10px;
  box-shadow: 0 10px 25px rgba(15, 23, 42, 0.12);
  font-size: 14px;
}
.import-dropzone {
  width: 100%;
  border: 2px dashed #cbd5e1;
  border-radius: 8px;
  padding: 20px 16px;
  text-align: center;
  cursor: pointer;
  background: #f8fafc;
  transition: border-color 0.15s ease, background-color 0.15s ease;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
}
.import-dropzone:hover,
.import-dropzone.is-dragover {
  border-color: #2563eb;
  background: #eff6ff;
}
.import-dropzone.has-file {
  border-style: solid;
  border-color: #93c5fd;
  background: #f0f9ff;
}
.import-file-hidden {
  display: none;
}
.dropzone-title {
  font-size: 13px;
  font-weight: 500;
  color: #1e293b;
}
.dropzone-file {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  word-break: break-all;
}
.docs-shell {
  min-height: calc(100vh - 90px);
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  overflow: hidden;
}
.doc-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.doc-list-item {
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid #e2e8f0;
  cursor: pointer;
}
.doc-list-item.active {
  background: #eff6ff;
  border-color: #3b82f6;
}
.doc-title {
  font-weight: 600;
  font-size: 13px;
}
.doc-meta,
.muted {
  font-size: 12px;
  color: #64748b;
}
.doc-workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 16px;
}
.doc-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.outline-box {
  background: #f8fafc;
  padding: 8px 12px;
  border-radius: 6px;
  margin-bottom: 12px;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.outline-link {
  font-size: 13px;
  color: #2563eb;
  cursor: pointer;
}
.markdown-body {
  overflow-x: auto;
}
.markdown-body :deep(img) {
  display: block;
  max-width: 100%;
  height: auto;
  max-height: 75vh;
  object-fit: contain;
  margin: 10px 0;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  background: #ffffff;
  cursor: zoom-in;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.markdown-body :deep(img:hover) {
  border-color: #3b82f6;
  box-shadow: 0 4px 14px rgba(37, 99, 235, 0.14);
}
.lightbox-viewport {
  height: 78vh;
  overflow: auto;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 12px;
}
.lightbox-img {
  display: block;
  height: auto;
  max-width: none;
  background: #ffffff;
  box-shadow: 0 2px 12px rgba(15, 23, 42, 0.1);
  cursor: zoom-out;
}
.lightbox-img.is-fit {
  max-width: 100%;
  max-height: 100%;
  width: auto;
  object-fit: contain;
  cursor: zoom-in;
  margin: auto;
}
.markdown-body :deep(pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 10px;
  border-radius: 6px;
  overflow-x: auto;
}
.markdown-body :deep(table) {
  width: 100%;
  border-collapse: collapse;
}
.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid #cbd5e1;
  padding: 6px 8px;
}
.edit-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.comment-head {
  display: flex;
  justify-content: space-between;
  margin-bottom: 4px;
}
.quote-box {
  margin: 4px 0;
  padding-left: 8px;
  border-left: 3px solid #94a3b8;
  color: #475569;
  font-size: 12px;
}
.link-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}
.rev-preview {
  white-space: pre-wrap;
  font-size: 11px;
  max-height: 220px;
  overflow: auto;
}
@media (max-width: 1100px) {
  .doc-workspace {
    grid-template-columns: 1fr;
  }
}
</style>
