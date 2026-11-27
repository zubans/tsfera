<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import SoftPhonePanel from './SoftPhonePanel.vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NLayout,
  NLayoutSider,
  NLayoutHeader,
  NLayoutContent,
  NMenu,
  NBadge,
  NButton,
  NPopover,
  NList,
  NListItem,
  NTag,
  NModal,
  NForm,
  NFormItem,
  NSelect,
  NSwitch,
  NDivider,
  NInput,
  NSpace,
  NAlert,
  type MenuOption
} from 'naive-ui'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import { useCrmWebSocket } from '@/shared/composables/useWebSocket'
import { requestJSON, ApiError } from '@/shared/api/http'
import { formatPhoneRU, formatDateTime } from '@/shared/utils/format'
import type {
  SearchResult,
  Contractor,
  CRMEnums,
  CallRecord,
  TrafficSource
} from '@/shared/types/domain'

const auth = useAuthStore()
const realtime = useRealtimeStore()
const router = useRouter()
const route = useRoute()

const collapsed = ref(false)
const searchOpen = ref(false)
const searchQuery = ref('')
const searchResults = ref<SearchResult[]>([])
const searching = ref(false)
const sipPanelOpen = ref(false)
const callCardOpen = ref(false)
const callCardSaving = ref(false)
const callCardError = ref('')
const callTransferTarget = ref('')
const callContractors = ref<Contractor[]>([])
const callTrafficSources = ref<TrafficSource[]>([])
const callAppealOptions = ref<{ label: string; value: string }[]>([
  { label: 'Договор ТО', value: 'to_contract' },
  { label: 'Сервисные / ремонтные работы', value: 'service_work' },
  { label: 'Продажа оборудования', value: 'sale_equipment' },
  { label: 'Магазин запчастей', value: 'parts_store' },
  { label: 'Уточнить информацию', value: 'specify' },
  { label: 'Спам', value: 'spam_call' },
  { label: 'Прочее', value: 'other_appeal' }
])
const callerTypeOptions = ref<{ label: string; value: number }[]>([
  { label: 'Клиент', value: 1 },
  { label: 'Поставщик', value: 2 },
  { label: 'Партнёр', value: 3 },
  { label: 'Спам', value: 4 },
  { label: 'Прочее', value: 5 }
])
const callResultOptions = ref<{ label: string; value: number }[]>([
  { label: 'Оформлен договор', value: 1 },
  { label: 'Продлён договор', value: 2 },
  { label: 'Оформлена заявка', value: 3 },
  { label: 'Консультация', value: 4 },
  { label: 'Перевод в другой отдел', value: 5 },
  { label: 'Нецелевой звонок', value: 6 },
  { label: 'Перезвонить', value: 7 },
  { label: 'Нет ответа', value: 8 },
  { label: 'Сброшен', value: 9 },
  { label: 'Отказ', value: 10 }
])
const callCardForm = ref({
  contractor_id: null as number | null,
  traffic_source_id: null as number | null,
  caller_type: 1 as number,
  call_result: 0 as number,
  type_of_appeal: '',
  comment: '',
  detach_from_others: true
})

const isCallCardClientOrSupplier = computed(
  () => callCardForm.value.caller_type === 1 || callCardForm.value.caller_type === 2
)

async function openCallCardForCall(call: CallRecord, contractor?: Contractor | null) {
  callCardError.value = ''
  callTransferTarget.value = ''
  callCardForm.value = {
    contractor_id: contractor?.id ?? call.contractor_id ?? null,
    traffic_source_id: call.traffic_source_id ?? null,
    caller_type: call.caller_type || 1,
    call_result: call.call_result || 0,
    type_of_appeal: call.type_of_appeal || '',
    comment: call.comment || '',
    detach_from_others: true
  }
  const [ctrRes, enumRes, tsRes] = await Promise.allSettled([
    requestJSON<{ items: Contractor[] }>('/api/v1/crm/contractors?limit=100'),
    requestJSON<CRMEnums>('/api/v1/crm/enums'),
    requestJSON<TrafficSource[]>('/api/v1/crm/traffic-sources?active=true')
  ])
  if (ctrRes.status === 'fulfilled' && ctrRes.value?.items) {
    callContractors.value = ctrRes.value.items
  }
  if (tsRes.status === 'fulfilled' && Array.isArray(tsRes.value)) {
    callTrafficSources.value = tsRes.value
  }
  if (enumRes.status === 'fulfilled' && enumRes.value) {
    if (enumRes.value.call_types_of_appeal?.length) {
      callAppealOptions.value = enumRes.value.call_types_of_appeal
        .filter((o) => !o.legacy || o.key === call.type_of_appeal)
        .map((o) => ({ label: o.label, value: o.key }))
    }
    if (enumRes.value.caller_types?.length) {
      callerTypeOptions.value = enumRes.value.caller_types.map((o) => ({ label: o.label, value: o.id }))
    }
    if (enumRes.value.call_results?.length) {
      callResultOptions.value = enumRes.value.call_results.map((o) => ({ label: o.label, value: o.id }))
    }
  }
  callCardOpen.value = true
}

async function openCallCardModal() {
  const popup = realtime.incomingCall
  if (!popup) return
  await openCallCardForCall(popup.call, popup.contractor ?? null)
}

async function openMissedCallCard(callId: number) {
  try {
    const call = await requestJSON<CallRecord>(`/api/v1/crm/calls/${callId}`)
    realtime.incomingCall = {
      call,
      contractor: call.contractor_id
        ? { id: call.contractor_id, name: call.contractor_name || '', type: 1, note: '' }
        : null
    }
    await openCallCardForCall(call, realtime.incomingCall.contractor)
  } catch {
    // Ошибка открытия пропущенного звонка не ломает UI.
  }
}

async function saveCallCard() {
  const popup = realtime.incomingCall
  if (!popup) return
  callCardSaving.value = true
  callCardError.value = ''
  try {
    const updated = await requestJSON<CallRecord>(`/api/v1/crm/calls/${popup.call.id}`, {
      method: 'PUT',
      body: JSON.stringify(callCardForm.value)
    })
    const matched = callContractors.value.find((c) => c.id === updated.contractor_id) ?? null
    realtime.incomingCall = {
      ...popup,
      call: updated,
      contractor: matched || popup.contractor
    }
    realtime.missCalls = realtime.missCalls.filter((m) => m.call_id !== updated.id)
    callCardOpen.value = false
  } catch (err) {
    callCardError.value = err instanceof ApiError ? err.message : 'Не удалось сохранить карточку звонка'
  } finally {
    callCardSaving.value = false
  }
}

async function transferIncomingCall() {
  const popup = realtime.incomingCall
  if (!popup || !callTransferTarget.value.trim()) return
  callCardError.value = ''
  try {
    await requestJSON('/api/v1/crm/calls/transfer', {
      method: 'POST',
      body: JSON.stringify({
        external_call_id: popup.call.external_call_id,
        target_phone: callTransferTarget.value.trim()
      })
    })
    callTransferTarget.value = ''
  } catch (err) {
    callCardError.value = err instanceof ApiError ? err.message : 'Ошибка перевода звонка'
  }
}

const { connected, connect, disconnect } = useCrmWebSocket((ev) => realtime.handleWsEvent(ev))

onMounted(async () => {
  await realtime.loadInitial()
  connect()
})

const menuOptions = computed<MenuOption[]>(() => {
  const opts: MenuOption[] = []
  if (auth.hasPerm('crm.application.view')) {
    opts.push({ label: 'Календарь заявок', key: '/crm/calendar' })
    opts.push({ label: 'Заявки и Акты ТО', key: '/crm/applications' })
  }
  if (auth.hasPerm('crm.contractor.view')) {
    opts.push({ label: 'Контрагенты и Адреса', key: '/crm/contractors' })
  }
  if (
    ['crm.timesheet.view', 'crm.timesheet.manage', 'crm.plan.view', 'crm.plan.view_own', 'crm.plan.manage'].some((p) =>
      auth.hasPerm(p)
    )
  ) {
    opts.push({ label: 'Табель и план', key: '/crm/timesheet' })
  }
  if (auth.hasPerm('crm.contract.view')) {
    opts.push({ label: 'Договоры ТО', key: '/crm/contracts' })
  }
  if (auth.hasPerm('crm.task.view')) {
    opts.push({ label: 'Задачи', key: '/crm/tasks' })
  }
  if (auth.hasPerm('crm.call.view')) {
    opts.push({ label: 'Звонки', key: '/crm/calls' })
  }
  if (
    [
      'crm.report.view',
      'crm.report.sum_executor.view',
      'crm.report.expired_tasks.view',
      'crm.report.sum_subdivision.view',
      'crm.report.amount_spare_parts.view',
      'crm.report.call_conversion.view',
      'crm.report.contractors.view',
      'crm.report.engineers.view',
      'crm.report.traffic_sources.view'
    ].some((p) => auth.hasPerm(p))
  ) {
    opts.push({ label: 'Отчёты', key: '/crm/reports' })
  }
  if (
    [
      'crm.ref.manage',
      'crm.equipment.view',
      'crm.subdivision.view',
      'crm.subdivision.manage',
      'crm.sip.view',
      'crm.sip.manage',
      'crm.pool.view',
      'crm.pool.manage',
      'crm.banner.view',
      'crm.banner.manage',
      'crm.analytic.view',
      'crm.analytic.manage',
      'crm.traffic_source.manage'
    ].some((p) => auth.hasPerm(p))
  ) {
    opts.push({ label: 'Справочники CRM', key: '/crm/settings' })
  }
  if (auth.hasPerm('docs.view')) {
    opts.push({ label: 'Документация и Регламенты', key: '/docs' })
  }
  if (auth.hasPerm('chat.use')) {
    opts.push({ label: 'Чат с ИИ-ассистентом', key: '/chat' })
  }
  if (auth.hasPerm('ai.search') || auth.hasPerm('ai.compose') || auth.hasPerm('ai.admin')) {
    opts.push({ label: 'ИИ: Поиск, Генерация, Модели', key: '/ai' })
  }
  if (auth.hasPerm('admin.user.view')) {
    opts.push({ label: 'Пользователи и RBAC', key: '/admin/users' })
  }
  return opts
})

async function runGlobalSearch() {
  if (!searchQuery.value.trim()) return
  searching.value = true
  try {
    searchResults.value = await requestJSON<SearchResult[]>(
      `/api/v1/ai/search?q=${encodeURIComponent(searchQuery.value.trim())}&limit=10`
    )
  } finally {
    searching.value = false
  }
}

function openSearchResult(item: SearchResult) {
  searchOpen.value = false
  if (item.path.startsWith('/docs/')) {
    const id = item.path.split('/').pop()
    void router.push({ path: '/docs', query: { docId: id, anchor: item.anchor || undefined } })
    return
  }
  if (item.path.startsWith('/crm/contractors/')) {
    const id = item.path.split('/').pop()
    void router.push({ path: '/crm/contractors', query: { contractorId: id } })
    return
  }
  void router.push(item.path)
}

async function handleLogout() {
  disconnect()
  await auth.logout()
  await router.push('/login')
}
</script>

<template>
  <NLayout has-sider style="min-height: 100vh">
    <NLayoutSider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="245"
      show-trigger
      v-model:collapsed="collapsed"
      style="background: #0f172a; color: #f8fafc"
    >
      <div class="brand-box" @click="router.push('/crm/calendar')">
        <span class="brand-dot" />
        <span v-if="!collapsed" class="brand-title">Теплосфера</span>
      </div>
      <NMenu
        inverted
        :collapsed="collapsed"
        :value="route.path"
        :options="menuOptions"
        @update:value="(key) => router.push(String(key))"
      />
    </NLayoutSider>

    <NLayout>
      <NLayoutHeader bordered class="app-header">
        <div class="header-left">
          <NButton size="small" secondary @click="searchOpen = true">
            Поиск по базе и CRM
          </NButton>
          <NTag :type="connected ? 'success' : 'warning'" size="small" round>
            {{ connected ? 'WS онлайн' : 'WS переподключение' }}
          </NTag>
          <NButton
            v-if="realtime.softPhone.enabled"
            size="small"
            type="info"
            secondary
            @click="sipPanelOpen = true"
          >
            SIP: {{ realtime.softPhone.login }}
          </NButton>
        </div>

        <NSpace align="center" :size="12">
          <NPopover trigger="click" placement="bottom-end" style="width: 360px">
            <template #trigger>
              <NBadge :value="realtime.missCallsCount" :max="99">
                <NButton size="small" :type="realtime.missCallsCount > 0 ? 'warning' : 'default'" secondary>
                  Пропущенные
                </NButton>
              </NBadge>
            </template>
            <div style="max-height: 340px; overflow: auto">
              <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px">
                <strong>Пул пропущенных звонков</strong>
                <NButton text size="tiny" type="primary" @click="router.push({ path: '/crm/calls', query: { tab: 'missed' } })">Журнал</NButton>
              </div>
              <NList v-if="realtime.missCalls.length" size="small" bordered>
                <NListItem v-for="m in realtime.missCalls" :key="m.id">
                  <div>
                    <div style="font-weight: 600">{{ formatPhoneRU(m.caller_phone) }}</div>
                    <div style="font-size: 12px; color: #64748b">
                      {{ m.contractor_name || 'Неизвестный номер' }} · {{ formatDateTime(m.started_at) }}
                    </div>
                    <NSpace :size="6" style="margin-top: 6px">
                      <NButton size="tiny" type="primary" secondary @click="openMissedCallCard(m.call_id)">Карточка</NButton>
                      <NButton size="tiny" tertiary @click="realtime.markMissCallViewed(m.call_id)">Просмотрено</NButton>
                    </NSpace>
                  </div>
                </NListItem>
              </NList>
              <div v-else style="color: #64748b; font-size: 13px">Необработанных пропущенных нет</div>
            </div>
          </NPopover>

          <NPopover trigger="click" placement="bottom-end" style="width: 340px">
            <template #trigger>
              <NBadge :value="realtime.urgentCount" :max="99">
                <NButton size="small" :type="realtime.urgentCount > 0 ? 'error' : 'default'" secondary>
                  Срочные задачи
                </NButton>
              </NBadge>
            </template>
            <div style="max-height: 320px; overflow: auto">
              <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px">
                <strong>Истекающий SLA по задачам</strong>
                <NButton text size="tiny" type="primary" @click="router.push({ path: '/crm/tasks', query: { urgent: '1' } })">Все задачи</NButton>
              </div>
              <NList v-if="realtime.urgentTasks.length" size="small" bordered>
                <NListItem v-for="t in realtime.urgentTasks" :key="t.id">
                  <div>
                    <div style="font-weight: 600">{{ t.title }}</div>
                    <div style="font-size: 12px; color: #64748b">
                      {{ t.contractor_name || 'Без контрагента' }} · до {{ formatDateTime(t.plan_datetime_to) }}
                    </div>
                  </div>
                </NListItem>
              </NList>
              <div v-else style="color: #64748b; font-size: 13px">Просроченных задач нет</div>
            </div>
          </NPopover>

          <NPopover trigger="click" placement="bottom-end" style="width: 360px">
            <template #trigger>
              <NBadge :value="realtime.unreadCount" :max="99">
                <NButton size="small" secondary>Уведомления</NButton>
              </NBadge>
            </template>
            <div style="max-height: 360px; overflow: auto">
              <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px">
                <strong>Уведомления</strong>
                <NButton text size="tiny" type="primary" @click="realtime.markAllRead">Прочитать все</NButton>
              </div>
              <NList v-if="realtime.notifications.length" size="small" bordered>
                <NListItem
                  v-for="n in realtime.notifications"
                  :key="n.id"
                  :style="{ opacity: n.is_read ? 0.6 : 1, cursor: 'pointer' }"
                  @click="realtime.markRead(n.id)"
                >
                  <div>
                    <div style="font-weight: 600">{{ n.title }}</div>
                    <div style="font-size: 12px">{{ n.message }}</div>
                  </div>
                </NListItem>
              </NList>
              <div v-else style="color: #64748b; font-size: 13px">Новых уведомлений нет</div>
            </div>
          </NPopover>

          <span style="font-size: 13px; color: #334155">
            {{ auth.user?.full_name || auth.user?.username }}
          </span>
          <NButton size="small" tertiary @click="handleLogout">Выйти</NButton>
        </NSpace>
      </NLayoutHeader>

      <NLayoutContent content-style="padding: 16px; background: #f8fafc; min-height: calc(100vh - 56px)">
        <NAlert
          v-if="realtime.incomingCall"
          :type="realtime.incomingCall.call.status === 'ended' ? 'default' : 'info'"
          closable
          title="Входящий / активный звонок МТС ВАТС"
          style="margin-bottom: 12px"
          @close="realtime.dismissCallPopup"
        >
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap">
            <div>
              <strong>{{ formatPhoneRU(realtime.incomingCall.call.caller_phone) }}</strong>
              <span v-if="realtime.incomingCall.contractor">
                — Контрагент: <b>{{ realtime.incomingCall.contractor.name }}</b>
              </span>
              <span v-else> — Новый номер (не найден в базе)</span>
              <NTag size="tiny" style="margin-left: 8px">{{ realtime.incomingCall.call.status }}</NTag>
            </div>
            <NSpace>
              <NButton
                v-if="auth.hasPerm('crm.call.update')"
                size="small"
                type="info"
                secondary
                @click="openCallCardModal"
              >
                Карточка звонка
              </NButton>
              <NButton
                v-if="realtime.incomingCall.contractor"
                size="small"
                type="primary"
                @click="router.push({ path: '/crm/contractors', query: { contractorId: realtime.incomingCall.contractor.id } })"
              >
                Открыть карточку
              </NButton>
              <NButton
                size="small"
                secondary
                @click="
                  router.push({
                    path: '/crm/calendar',
                    query: realtime.incomingCall.contractor
                      ? { createContractorId: String(realtime.incomingCall.contractor.id) }
                      : undefined
                  })
                "
              >
                Создать заявку
              </NButton>
            </NSpace>
          </div>
        </NAlert>

        <slot />
      </NLayoutContent>
    </NLayout>
  </NLayout>

  <NModal v-model:show="searchOpen" preset="card" title="Гибридный ИИ-поиск (Документация + CRM)" style="width: 680px">
    <NSpace vertical>
      <div style="display: flex; gap: 8px">
        <NInput
          v-model:value="searchQuery"
          placeholder="Например: ошибка E01 Baxi, договор ТО, Иванов..."
          @keyup.enter="runGlobalSearch"
        />
        <NButton type="primary" :loading="searching" @click="runGlobalSearch">Найти</NButton>
      </div>
      <NList v-if="searchResults.length" bordered hoverable clickable>
        <NListItem v-for="(item, idx) in searchResults" :key="idx" @click="openSearchResult(item)">
          <div>
            <NSpace align="center" :size="8">
              <NTag size="tiny" type="info">{{ item.source_type }}</NTag>
              <strong>{{ item.title }}</strong>
            </NSpace>
            <div style="font-size: 12px; color: #475569; margin-top: 4px">{{ item.snippet }}</div>
          </div>
        </NListItem>
      </NList>
    </NSpace>
  </NModal>

  <SoftPhonePanel v-model:show="sipPanelOpen" />

  <NModal v-model:show="callCardOpen" preset="card" title="Карточка звонка МТС ВАТС" style="width: 520px">
    <template v-if="realtime.incomingCall">
      <NAlert v-if="callCardError" type="error" style="margin-bottom: 10px">{{ callCardError }}</NAlert>
      <div style="margin-bottom: 12px; font-size: 13px">
        <div><b>Звонок:</b> {{ formatPhoneRU(realtime.incomingCall.call.caller_phone) }} → {{ formatPhoneRU(realtime.incomingCall.call.callee_phone) }}</div>
        <div><b>Статус:</b> {{ realtime.incomingCall.call.status }} · <b>Начало:</b> {{ formatDateTime(realtime.incomingCall.call.started_at) }}</div>
      </div>
      <NForm label-placement="top">
        <NFormItem label="Кто звонит">
          <NSelect v-model:value="callCardForm.caller_type" :options="callerTypeOptions" />
        </NFormItem>
        <NFormItem :label="isCallCardClientOrSupplier ? 'Контрагент *' : 'Контрагент'">
          <NSelect
            v-model:value="callCardForm.contractor_id"
            filterable
            clearable
            :options="callContractors.map((c) => ({ label: c.name, value: c.id }))"
          />
        </NFormItem>
        <NFormItem label="Отвязать номер от других контрагентов">
          <NSwitch v-model:value="callCardForm.detach_from_others" />
        </NFormItem>
        <NFormItem :label="isCallCardClientOrSupplier ? 'Источник трафика *' : 'Источник трафика'">
          <NSelect
            v-model:value="callCardForm.traffic_source_id"
            clearable
            filterable
            :options="callTrafficSources.map((ts) => ({ label: ts.name, value: ts.id }))"
          />
        </NFormItem>
        <NFormItem :label="isCallCardClientOrSupplier ? 'Тип обращения *' : 'Тип обращения'">
          <NSelect
            v-model:value="callCardForm.type_of_appeal"
            clearable
            :options="callAppealOptions"
          />
        </NFormItem>
        <NFormItem :label="isCallCardClientOrSupplier ? 'Результат звонка *' : 'Результат звонка'">
          <NSelect
            v-model:value="callCardForm.call_result"
            clearable
            :options="callResultOptions"
          />
        </NFormItem>
        <NFormItem label="Комментарий по звонку">
          <NInput v-model:value="callCardForm.comment" type="textarea" :rows="2" />
        </NFormItem>
      </NForm>
      <NDivider>Слепой перевод (BlindTransfer)</NDivider>
      <div style="display: flex; gap: 8px; margin-bottom: 12px">
        <NInput v-model:value="callTransferTarget" placeholder="Номер (+79991234567)" />
        <NButton secondary @click="transferIncomingCall">Перевести</NButton>
      </div>
      <NSpace justify="end">
        <NButton @click="callCardOpen = false">Закрыть</NButton>
        <NButton type="primary" :loading="callCardSaving" @click="saveCallCard">Сохранить карточку</NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.brand-box {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  cursor: pointer;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.brand-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #f97316;
  flex-shrink: 0;
}
.brand-title {
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.3px;
}
.app-header {
  min-height: 56px;
  height: auto;
  padding: 8px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  background: #ffffff;
}
.header-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
</style>
