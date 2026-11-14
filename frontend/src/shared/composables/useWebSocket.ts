import { ref, onUnmounted } from 'vue'

export interface WSMessage<T = unknown> {
  type: string
  payload: T
}

export function useCrmWebSocket(onEvent: (msg: WSMessage) => void) {
  const connected = ref(false)
  let ws: WebSocket | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let closedManually = false

  const connect = () => {
    closedManually = false
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}/ws/crm`
    try {
      ws = new WebSocket(url)
    } catch {
      return
    }
    ws.onopen = () => {
      connected.value = true
    }
    ws.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(String(ev.data)) as WSMessage
        onEvent(parsed)
      } catch {
        // Игнорируем некорректный WS-пакет.
      }
    }
    ws.onclose = () => {
      connected.value = false
      if (!closedManually) {
        timer = setTimeout(connect, 3000)
      }
    }
  }

  const disconnect = () => {
    closedManually = true
    if (timer) clearTimeout(timer)
    if (ws) {
      ws.close()
      ws = null
    }
    connected.value = false
  }

  onUnmounted(disconnect)

  return { connected, connect, disconnect }
}
