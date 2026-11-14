import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { parseSSEBuffer, requestSSE, ApiError } from '@/shared/api/http'
import { useCrmWebSocket, type WSMessage } from '@/shared/composables/useWebSocket'

describe('SSE buffer and streaming client', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('parses chunked SSE frames and skips malformed json and [DONE]', () => {
    const chunk1 = 'data: {"type":"step","step":{"name":"search"}}\n\ndata: {broken\n\ndata: [DONE]\n\ndata: {"type":"del'
    const first = parseSSEBuffer<{ type: string; delta?: string }>(chunk1)
    expect(first.events).toHaveLength(1)
    expect(first.events[0].type).toBe('step')
    expect(first.rest).toBe('data: {"type":"del')

    const second = parseSSEBuffer<{ type: string; delta?: string }>(
      first.rest + 'ta","delta":"Привет"}\n\n'
    )
    expect(second.events).toHaveLength(1)
    expect(second.events[0]).toEqual({ type: 'delta', delta: 'Привет' })
    expect(second.rest).toBe('')
  })

  it('streams events via requestSSE from ReadableStream', async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('data: {"type":"delta","delta":"Часть 1 "}\n\n'))
        controller.enqueue(encoder.encode('data: {"type":"delta","delta":"Часть 2"}\n\n'))
        controller.enqueue(encoder.encode('data: {"type":"done"}\n\n'))
        controller.close()
      }
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(stream, { status: 200 }))

    const events: { type: string; delta?: string }[] = []
    await requestSSE('/api/v1/ai/compose', { title: 'Test' }, (ev) => events.push(ev as any))
    expect(events.map((e) => e.delta || e.type)).toEqual(['Часть 1 ', 'Часть 2', 'done'])
  })

  it('throws ApiError on non-OK SSE response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'Модель недоступна' }), { status: 403 })
    )
    await expect(requestSSE('/api/v1/ai/compose', {}, () => {})).rejects.toMatchObject({
      status: 403,
      message: 'Модель недоступна'
    } satisfies Partial<ApiError>)
  })
})

describe('useCrmWebSocket', () => {
  it('connects, forwards parsed events, and closes cleanly on disconnect', () => {
    const instances: any[] = []
    class FakeWS {
      onopen: (() => void) | null = null
      onmessage: ((ev: { data: string }) => void) | null = null
      onclose: (() => void) | null = null
      url: string
      closed = false
      constructor(url: string) {
        this.url = url
        instances.push(this)
      }
      close() {
        this.closed = true
        this.onclose?.()
      }
    }
    vi.stubGlobal('WebSocket', FakeWS as any)

    const received: WSMessage[] = []
    let handle!: ReturnType<typeof useCrmWebSocket>
    const Comp = defineComponent({
      setup() {
        handle = useCrmWebSocket((m) => received.push(m))
        return () => null
      }
    })
    const wrapper = mount(Comp)

    handle.connect()
    expect(instances).toHaveLength(1)
    expect(instances[0].url).toContain('/ws/crm')

    instances[0].onopen?.()
    expect(handle.connected.value).toBe(true)

    instances[0].onmessage?.({ data: JSON.stringify({ type: 'calendar:updated', payload: { id: 9 } }) })
    expect(received).toEqual([{ type: 'calendar:updated', payload: { id: 9 } }])

    handle.disconnect()
    expect(instances[0].closed).toBe(true)
    expect(handle.connected.value).toBe(false)
    wrapper.unmount()
  })
})
