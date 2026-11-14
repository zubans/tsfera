export class ApiError extends Error {
  status: number
  code?: string
  fields?: Record<string, string>

  constructor(message: string, status: number, code?: string, fields?: Record<string, string>) {
    super(message)
    this.status = status
    this.code = code
    this.fields = fields
  }
}

export async function requestJSON<T>(url: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers ?? {})
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const resp = await fetch(url, {
    credentials: 'include',
    ...init,
    headers
  })

  if (resp.status === 204) {
    return undefined as unknown as T
  }

  const text = await resp.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = text
    }
  }

  if (!resp.ok) {
    if (parsed && typeof parsed === 'object' && 'error' in parsed) {
      const e = parsed as { error: string; code?: string; fields?: Record<string, string> }
      throw new ApiError(e.error, resp.status, e.code, e.fields)
    }
    throw new ApiError(typeof parsed === 'string' && parsed ? parsed : `Ошибка HTTP ${resp.status}`, resp.status)
  }

  return parsed as T
}

export function parseSSEBuffer<T>(buffer: string): { events: T[]; rest: string } {
  const events: T[] = []
  let rest = buffer
  while (true) {
    const idx = rest.indexOf('\n\n')
    if (idx === -1) break
    const frame = rest.slice(0, idx)
    rest = rest.slice(idx + 2)
    for (const rawLine of frame.split('\n')) {
      const line = rawLine.trim()
      if (!line.startsWith('data:')) continue
      const payload = line.slice(5).trim()
      if (!payload || payload === '[DONE]') continue
      try {
        events.push(JSON.parse(payload) as T)
      } catch {
        // Игнорируем некорректный фрагмент без падения всего потока.
      }
    }
  }
  return { events, rest }
}

export async function requestSSE<T>(
  url: string,
  body: unknown,
  onEvent: (event: T) => void,
  signal?: AbortSignal
): Promise<void> {
  const resp = await fetch(url, {
    method: 'POST',
    credentials: 'include',
    signal,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream'
    },
    body: JSON.stringify(body)
  })
  if (!resp.ok) {
    const errText = await resp.text()
    try {
      const parsed = JSON.parse(errText) as { error?: string }
      throw new ApiError(parsed.error || `HTTP ${resp.status}`, resp.status)
    } catch (e) {
      if (e instanceof ApiError) throw e
      throw new ApiError(errText || `HTTP ${resp.status}`, resp.status)
    }
  }
  if (!resp.body) return
  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    const parsed = parseSSEBuffer<T>(buf)
    buf = parsed.rest
    for (const ev of parsed.events) {
      onEvent(ev)
    }
  }
  if (buf.trim()) {
    const tail = parseSSEBuffer<T>(buf + '\n\n')
    for (const ev of tail.events) onEvent(ev)
  }
}

export interface UploadedFile {
  id: number
  original_name: string
  mime_type: string
  size_bytes: number
  url: string
}

export async function uploadCRMFile(file: File): Promise<UploadedFile> {
  const fd = new FormData()
  fd.append('file', file)
  return requestJSON<UploadedFile>('/api/v1/files', {
    method: 'POST',
    body: fd
  })
}
