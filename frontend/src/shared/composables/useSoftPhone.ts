import { ref, shallowRef } from 'vue'
import { UA, WebSocketInterface } from 'jssip'
import type { RTCSessionEvent } from 'jssip/lib/UA'
import type { RTCSession } from 'jssip/lib/RTCSession'
import type { SoftPhoneConfig } from '@/stores/realtime'

export function useSoftPhone() {
  const registered = ref(false)
  const status = ref('Отключён')
  const error = ref('')
  const incoming = ref(false)
  const peer = ref('')
  const session = shallowRef<RTCSession | null>(null)
  const remoteAudio = shallowRef<HTMLAudioElement | null>(null)
  let ua: UA | null = null
  let domain = ''

  function releaseCall(): void {
    session.value = null
    incoming.value = false
    peer.value = ''
    if (remoteAudio.value) remoteAudio.value.srcObject = null
  }

  function attachConnection(connection: RTCPeerConnection): void {
    connection.addEventListener('track', (event) => {
      if (!remoteAudio.value) return
      remoteAudio.value.srcObject = event.streams[0] ?? new MediaStream([event.track])
      void remoteAudio.value.play().catch(() => {
        error.value = 'Нажмите воспроизведение в аудиоплеере для включения звука'
      })
    })
  }

  function disconnect(): void {
    const previous = ua
    ua = null
    previous?.stop()
    releaseCall()
    registered.value = false
    status.value = 'Отключён'
  }

  function connect(config: SoftPhoneConfig): void {
    disconnect()
    error.value = ''
    if (!config.enabled || !config.ws_url || !config.login || !config.domain || !config.password) {
      error.value = 'SIP-учётная запись настроена не полностью'
      return
    }
    try {
      domain = config.domain
      const client = new UA({
        sockets: [new WebSocketInterface(config.ws_url)],
        uri: `sip:${config.login}@${domain}`,
        password: config.password,
        display_name: config.name,
        register: true
      })
      ua = client
      status.value = 'Подключение'
      client.on('registered', () => {
        if (ua !== client) return
        registered.value = true
        status.value = 'Линия готова'
      })
      client.on('registrationFailed', () => {
        if (ua !== client) return
        registered.value = false
        status.value = 'Ошибка регистрации'
        error.value = 'Проверьте SIP-учётную запись и доступность сервера'
      })
      client.on('disconnected', () => {
        if (ua !== client) return
        registered.value = false
        status.value = 'Соединение потеряно'
      })
      client.on('unregistered', () => { if (ua === client) registered.value = false })
      client.on('newRTCSession', ({ session: next, originator }: RTCSessionEvent) => {
        if (ua !== client || session.value) {
          next.terminate({ status_code: 486, reason_phrase: 'Busy Here' })
          return
        }
        session.value = next
        incoming.value = originator === 'remote'
        peer.value = next.remote_identity.uri.user
        status.value = incoming.value ? 'Входящий звонок' : 'Вызов'
        next.on('peerconnection', ({ peerconnection }: { peerconnection: RTCPeerConnection }) => attachConnection(peerconnection))
        if (next.connection) attachConnection(next.connection)
        next.on('confirmed', () => {
          if (session.value !== next) return
          incoming.value = false
          status.value = 'Разговор'
        })
        const finish = (): void => {
          if (session.value !== next) return
          releaseCall()
          status.value = registered.value ? 'Линия готова' : 'Отключён'
        }
        next.on('ended', finish)
        next.on('failed', () => {
          if (session.value !== next) return
          error.value = 'Звонок завершён с ошибкой. Проверьте доступ к микрофону и соединение'
          finish()
        })
      })
      client.start()
    } catch {
      disconnect()
      error.value = 'Не удалось подключить SIP-линию'
    }
  }

  function call(number: string): void {
    const target = number.trim()
    if (!ua || !registered.value || session.value) return
    if (!/^\+?[0-9*#]+$/.test(target)) {
      error.value = 'Введите телефонный или внутренний номер'
      return
    }
    error.value = ''
    try { ua.call(`sip:${target}@${domain}`, { mediaConstraints: { audio: true, video: false } }) }
    catch { error.value = 'Не удалось начать звонок' }
  }

  function answer(): void {
    if (!incoming.value || !session.value) return
    error.value = ''
    try { session.value.answer({ mediaConstraints: { audio: true, video: false } }) }
    catch { error.value = 'Не удалось ответить на звонок' }
  }

  function hangup(): void {
    try { session.value?.terminate() }
    catch { error.value = 'Не удалось завершить звонок' }
  }

  return { registered, status, error, incoming, peer, session, remoteAudio, connect, disconnect, call, answer, hangup }
}
