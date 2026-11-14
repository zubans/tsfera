import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { requestJSON } from '@/shared/api/http'
import type { CRMTask, Contractor, NotificationItem, CallRecord, MissCallPoolItem } from '@/shared/types/domain'
import type { WSMessage } from '@/shared/composables/useWebSocket'

export interface IncomingCallPopup {
  call: CallRecord
  contractor?: Contractor | null
  event_type?: string
}

export interface SoftPhoneConfig {
  enabled: boolean
  name?: string
  domain?: string
  password?: string
  login?: string
  abonent_id?: string
  protocol?: string
  server?: string
  port?: number
  ws_url?: string
}

export const useRealtimeStore = defineStore('realtime', () => {
  const notifications = ref<NotificationItem[]>([])
  const urgentTasks = ref<CRMTask[]>([])
  const missCalls = ref<MissCallPoolItem[]>([])
  const incomingCall = ref<IncomingCallPopup | null>(null)
  const softPhone = ref<SoftPhoneConfig>({ enabled: false })
  const calendarRevision = ref(0)

  const unreadCount = computed(() => notifications.value.filter((n) => !n.is_read).length)
  const urgentCount = computed(() => urgentTasks.value.length)
  const missCallsCount = computed(() => missCalls.value.filter((m) => !m.is_viewed).length)

  async function loadInitial(): Promise<void> {
    const [notifs, tasks, sip, activeCalls, missed] = await Promise.allSettled([
      requestJSON<NotificationItem[]>('/api/v1/crm/notifications?limit=30'),
      requestJSON<CRMTask[]>('/api/v1/crm/tasks?urgent=true&limit=20'),
      requestJSON<SoftPhoneConfig>('/api/v1/crm/softphone/config'),
      requestJSON<CallRecord[]>('/api/v1/crm/calls/current'),
      requestJSON<MissCallPoolItem[]>('/api/v1/crm/miss-calls?only_unviewed=true&limit=50')
    ])
    if (notifs.status === 'fulfilled') notifications.value = notifs.value ?? []
    if (tasks.status === 'fulfilled') urgentTasks.value = tasks.value ?? []
    if (sip.status === 'fulfilled') softPhone.value = sip.value ?? { enabled: false }
    if (missed.status === 'fulfilled') missCalls.value = missed.value ?? []
    if (activeCalls.status === 'fulfilled' && activeCalls.value?.length) {
      const c = activeCalls.value[0]
      incomingCall.value = {
        call: c,
        contractor: c.contractor_id
          ? { id: c.contractor_id, name: c.contractor_name || '', type: 1, note: '' }
          : null
      }
    }
  }

  async function markMissCallViewed(callId: number): Promise<void> {
    await requestJSON(`/api/v1/crm/miss-calls/${callId}/viewed`, { method: 'POST' })
    missCalls.value = missCalls.value.filter((m) => m.call_id !== callId)
  }

  async function markRead(id: number): Promise<void> {
    await requestJSON(`/api/v1/crm/notifications/${id}/read`, { method: 'POST' })
    const item = notifications.value.find((n) => n.id === id)
    if (item) item.is_read = true
  }

  async function markAllRead(): Promise<void> {
    await requestJSON('/api/v1/crm/notifications/read-all', { method: 'POST' })
    for (const n of notifications.value) n.is_read = true
  }

  function dismissCallPopup(): void {
    incomingCall.value = null
  }

  function handleWsEvent(msg: WSMessage): void {
    switch (msg.type) {
      case 'notification:new': {
        const n = msg.payload as NotificationItem
        notifications.value = [n, ...notifications.value.filter((x) => x.id !== n.id)].slice(0, 50)
        break
      }
      case 'task:urgent': {
        const t = msg.payload as CRMTask
        urgentTasks.value = [t, ...urgentTasks.value.filter((x) => x.id !== t.id)]
        break
      }
      case 'task:assigned': {
        void loadInitial()
        break
      }
      case 'call:connect': {
        incomingCall.value = msg.payload as IncomingCallPopup
        break
      }
      case 'call:ended': {
        const ended = msg.payload as IncomingCallPopup
        if (
          incomingCall.value &&
          (!ended.call?.external_call_id ||
            incomingCall.value.call.external_call_id === ended.call.external_call_id)
        ) {
          incomingCall.value = {
            ...incomingCall.value,
            call: ended.call ?? { ...incomingCall.value.call, status: 'ended' },
            event_type: ended.event_type
          }
        }
        break
      }
      case 'calendar:updated': {
        calendarRevision.value++
        break
      }
      case 'miss_call:new': {
        void loadInitial()
        break
      }
    }
  }

  return {
    notifications,
    urgentTasks,
    missCalls,
    incomingCall,
    softPhone,
    calendarRevision,
    unreadCount,
    urgentCount,
    missCallsCount,
    loadInitial,
    markMissCallViewed,
    markRead,
    markAllRead,
    dismissCallPopup,
    handleWsEvent
  }
})
