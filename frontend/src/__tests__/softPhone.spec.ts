import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSoftPhone } from '@/shared/composables/useSoftPhone'

const mock = vi.hoisted(() => ({ clients: [] as any[] }))
vi.mock('jssip', () => ({
  WebSocketInterface: class { constructor(public url: string) {} },
  UA: class {
    handlers: Record<string, (...args: any[]) => void> = {}
    start = vi.fn()
    stop = vi.fn()
    call = vi.fn()
    constructor(public config: unknown) { mock.clients.push(this) }
    on(event: string, handler: (...args: any[]) => void): void { this.handlers[event] = handler }
  }
}))

const config = { enabled: true, ws_url: 'wss://sip.test/ws', domain: 'sip.test', login: '101', password: 'secret' }
function fakeSession() {
  const handlers: Record<string, (...args: any[]) => void> = {}
  return { remote_identity: { uri: { user: '102' } }, handlers,
    on: (event: string, fn: (...args: any[]) => void) => { handlers[event] = fn },
    answer: vi.fn(), terminate: vi.fn() }
}

describe('SIP telephone lifecycle', () => {
  beforeEach(() => { mock.clients.length = 0 })
  it('registers and calls only after registration, with audio only', () => {
    const phone = useSoftPhone()
    phone.connect(config)
    const client = mock.clients[0]
    phone.call('102')
    expect(client.call).not.toHaveBeenCalled()
    client.handlers.registered()
    phone.call('102')
    expect(client.call).toHaveBeenCalledWith('sip:102@sip.test', { mediaConstraints: { audio: true, video: false } })
    phone.call('evil@outside.test')
    expect(client.call).toHaveBeenCalledTimes(1)
    phone.disconnect()
    expect(client.stop).toHaveBeenCalled()
    expect(phone.registered.value).toBe(false)
  })
  it('answers incoming calls, rejects a second call and releases the session', () => {
    const phone = useSoftPhone()
    phone.connect(config)
    const client = mock.clients[0]
    client.handlers.registered()
    const session = fakeSession()
    client.handlers.newRTCSession({ session, originator: 'remote' })
    expect(phone.incoming.value).toBe(true)
    phone.answer()
    expect(session.answer).toHaveBeenCalledWith({ mediaConstraints: { audio: true, video: false } })
    const second = fakeSession()
    client.handlers.newRTCSession({ session: second, originator: 'remote' })
    expect(second.terminate).toHaveBeenCalledWith({ status_code: 486, reason_phrase: 'Busy Here' })
    session.handlers.confirmed()
    expect(phone.incoming.value).toBe(false)
    phone.hangup()
    expect(session.terminate).toHaveBeenCalled()
    session.handlers.ended()
    expect(phone.session.value).toBeNull()
  })
  it('does not register incomplete credentials', () => {
    const phone = useSoftPhone()
    phone.connect({ enabled: true })
    expect(mock.clients).toHaveLength(0)
    expect(phone.error.value).not.toBe('')
  })
})
