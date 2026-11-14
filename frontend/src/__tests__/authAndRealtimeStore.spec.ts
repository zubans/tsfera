import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
  })

  it('fetches current user and evaluates RBAC & superadmin permissions', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          user: { id: 1, username: 'ivan', full_name: 'Иван', roles: ['dispatcher'], active: true },
          permissions: ['crm.application.view', 'docs.view']
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    )

    const auth = useAuthStore()
    const ok = await auth.fetchMe()
    expect(ok).toBe(true)
    expect(auth.initialized).toBe(true)
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.hasPerm('crm.application.view')).toBe(true)
    expect(auth.hasPerm('admin.user.delete')).toBe(false)

    // Superadmin bypasses explicit permissions list
    auth.user!.roles = ['superadmin']
    expect(auth.hasPerm('admin.user.delete')).toBe(true)
  })

  it('handles 401 on fetchMe cleanly and logs out', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('unauthorized', { status: 401 }))
    const auth = useAuthStore()
    const ok = await auth.fetchMe()
    expect(ok).toBe(false)
    expect(auth.initialized).toBe(true)
    expect(auth.isAuthenticated).toBe(false)
    expect(auth.hasPerm('docs.view')).toBe(false)
  })
})

describe('realtime store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
  })

  it('handles notification:new, task:urgent, call:connect and calendar:updated WS events', () => {
    const rt = useRealtimeStore()

    rt.handleWsEvent({
      type: 'notification:new',
      payload: {
        id: 11,
        kind: 'info',
        title: 'Новая заявка',
        message: 'Выезд в 14:00',
        entity_type: 'application',
        entity_id: 5,
        is_read: false,
        created_at: '2026-11-13T10:00:00Z'
      }
    })
    expect(rt.unreadCount).toBe(1)

    rt.handleWsEvent({
      type: 'task:urgent',
      payload: {
        id: 22,
        task_type: 'call',
        title: 'Срочный перезвон',
        description: '',
        status: 1,
        priority: 3,
        plan_datetime_to: '2026-11-13T11:00:00Z',
        is_urgent: true,
        note: ''
      }
    })
    expect(rt.urgentCount).toBe(1)

    rt.handleWsEvent({
      type: 'call:connect',
      payload: {
        call: { id: 33, external_call_id: 'call-33', status: 'active', caller_phone: '9991234567' },
        contractor: { id: 7, name: 'ООО ГазСервис' }
      }
    })
    expect(rt.incomingCall?.contractor?.name).toBe('ООО ГазСервис')

    rt.handleWsEvent({
      type: 'call:ended',
      payload: {
        call: { id: 33, external_call_id: 'call-33', status: 'ended', caller_phone: '9991234567' }
      }
    })
    expect(rt.incomingCall?.call.status).toBe('ended')

    rt.handleWsEvent({ type: 'calendar:updated', payload: { id: 5 } })
    expect(rt.calendarRevision).toBe(1)

    rt.dismissCallPopup()
    expect(rt.incomingCall).toBeNull()
  })

  it('restores active current call on loadInitial', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/calls/current')) {
        return new Response(
          JSON.stringify([
            {
              id: 88,
              external_call_id: 'ext-88',
              status: 'active',
              direction: 'incoming',
              caller_phone: '9995554433',
              callee_phone: '8001002030',
              contractor_id: 12,
              contractor_name: 'ИП Сидоров',
              duration_sec: 15,
              started_at: '2026-11-13T10:00:00Z'
            }
          ]),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        )
      }
      if (url.includes('/api/v1/crm/softphone/config')) {
        return new Response(JSON.stringify({ enabled: false }), { status: 200 })
      }
      return new Response(JSON.stringify([]), { status: 200 })
    })
    const rt = useRealtimeStore()
    await rt.loadInitial()
    expect(rt.incomingCall?.call.external_call_id).toBe('ext-88')
    expect(rt.incomingCall?.contractor?.name).toBe('ИП Сидоров')
  })

  it('marks notifications read via API', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    )
    const rt = useRealtimeStore()
    rt.notifications = [
      {
        id: 1,
        kind: 'info',
        title: 'T1',
        message: 'M1',
        entity_type: 'task',
        entity_id: 1,
        is_read: false,
        created_at: '2026-11-13T10:00:00Z'
      },
      {
        id: 2,
        kind: 'info',
        title: 'T2',
        message: 'M2',
        entity_type: 'task',
        entity_id: 2,
        is_read: false,
        created_at: '2026-11-13T10:00:00Z'
      }
    ]
    await rt.markRead(1)
    expect(rt.unreadCount).toBe(1)
    await rt.markAllRead()
    expect(rt.unreadCount).toBe(0)
  })

  it('loads unviewed miss calls on miss_call:new WS event and marks them viewed', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/miss-calls?only_unviewed=true')) {
        return new Response(
          JSON.stringify([
            {
              id: 101,
              call_id: 501,
              user_id: 1,
              is_viewed: false,
              caller_phone: '9998887766',
              callee_phone: '8001002030',
              started_at: '2026-11-14T09:00:00Z',
              contractor_id: 7,
              contractor_name: 'ООО ТеплоДом',
              created_at: '2026-11-14T09:00:05Z'
            }
          ]),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        )
      }
      if (url.includes('/api/v1/crm/miss-calls/501/viewed')) {
        return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
      }
      if (url.includes('/api/v1/crm/softphone/config')) {
        return new Response(JSON.stringify({ enabled: false }), { status: 200 })
      }
      return new Response(JSON.stringify([]), { status: 200 })
    })

    const rt = useRealtimeStore()
    rt.handleWsEvent({ type: 'miss_call:new', payload: { call_id: 501 } })
    await vi.waitFor(() => {
      expect(rt.missCallsCount).toBe(1)
    })
    expect(rt.missCalls[0].contractor_name).toBe('ООО ТеплоДом')

    await rt.markMissCallViewed(501)
    expect(rt.missCallsCount).toBe(0)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/crm/miss-calls/501/viewed',
      expect.objectContaining({ method: 'POST' })
    )
  })
})
