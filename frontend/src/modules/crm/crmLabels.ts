import type { CRMEnums, SubdivisionPaymentOption } from '@/shared/types/domain'

export type TagType = 'default' | 'primary' | 'info' | 'success' | 'warning' | 'error'

const TASK_TYPE_FALLBACK: Record<string, string> = {
  call: 'Звонок',
  quarterly_call: 'Квартальный обзвон',
  prolongation: 'Пролонгация ТО',
  no_answer: 'Недозвон',
  service: 'ТО / Сервис',
  app_limit: 'Лимит заявок',
  call_feedback: 'Обратная связь',
  other: 'Прочее'
}

const CALL_APPEAL_FALLBACK: Record<string, string> = {
  to_contract: 'Договор ТО',
  service_work: 'Сервисные / ремонтные работы',
  sale_equipment: 'Продажа оборудования',
  parts_store: 'Магазин запчастей',
  specify: 'Уточнить информацию',
  spam_call: 'Спам',
  other_appeal: 'Прочее',
  repair_application: 'Заявка на ремонт/ТО',
  consultation: 'Консультация',
  callback: 'Повторный звонок',
  spam: 'Спам',
  general_question: 'Общий вопрос',
  service_request: 'Заявка на обслуживание',
  buy_parts: 'Купить запчасти',
  import_1: 'Заявка на обслуживание (новый)',
  import_2: 'Заявка на обслуживание (действ.)',
  import_3: 'Купить запчасти',
  import_4: 'Отказ / отмена заявки',
  import_5: 'Спам',
  import_6: 'Уточнения по заявке / договору',
  import_7: 'Вопрос в ГК',
  import_8: 'Отзыв клиента',
  import_9: 'Договор',
  import_10: 'Услуга',
  import_11: 'Акция',
  import_12: 'Магазин',
  import_13: 'Уточнение',
  import_14: 'Обратная связь',
  import_15: 'Обзвон по договору'
}

const PAYMENT_OPTION_FALLBACK: Record<string, string> = {
  '1': 'Наличные',
  '2': 'Терминал',
  '3': 'Безналичный расчёт',
  '4': 'Онлайн / СБП',
  cash: 'Наличные',
  card: 'Терминал',
  bank_transfer: 'Безналичный расчёт',
  online: 'Онлайн / СБП'
}

export function documentStatusTagType(status: number): TagType {
  switch (status) {
    case 1:
      return 'success'
    case 2:
      return 'info'
    case 3:
      return 'error'
    default:
      return 'default'
  }
}

export function taskStatusTagType(status: number): TagType {
  switch (status) {
    case 1:
      return 'info'
    case 2:
      return 'primary'
    case 3:
      return 'success'
    case 4:
      return 'warning'
    case 5:
      return 'error'
    default:
      return 'default'
  }
}

export function taskTypeLabel(taskType?: string | null, enums?: CRMEnums): string {
  if (!taskType) return '—'
  const fromEnum = enums?.task_types?.find((t) => t.id === taskType)?.label
  if (fromEnum) return fromEnum
  return TASK_TYPE_FALLBACK[taskType] || taskType
}

export function taskTypeTagType(taskType?: string | null): TagType {
  switch (taskType) {
    case 'prolongation':
      return 'warning'
    case 'quarterly_call':
      return 'info'
    case 'no_answer':
      return 'error'
    case 'service':
      return 'success'
    default:
      return 'default'
  }
}

export function callDirectionMeta(direction?: string | null): { label: string; short: string; type: TagType } {
  const norm = (direction || '').toLowerCase()
  if (norm === 'in' || norm === 'inbound' || norm === 'incoming') {
    return { label: 'Входящий', short: '↙ Вход.', type: 'success' }
  }
  if (norm === 'out' || norm === 'outbound' || norm === 'outgoing') {
    return { label: 'Исходящий', short: '↗ Исх.', type: 'info' }
  }
  if (norm === 'internal') {
    return { label: 'Внутренний', short: '⇄ Внутр.', type: 'default' }
  }
  return { label: direction || '—', short: direction || '—', type: 'default' }
}

export function callStatusMeta(status?: string | null): { label: string; type: TagType } {
  const norm = (status || '').toLowerCase()
  switch (norm) {
    case 'completed':
    case 'ended':
      return { label: 'Завершён', type: 'success' }
    case 'answered':
      return { label: 'Отвечен', type: 'success' }
    case 'missed':
      return { label: 'Пропущен', type: 'error' }
    case 'connected':
      return { label: 'Разговор', type: 'info' }
    case 'ringing':
    case 'new':
      return { label: 'Звонит', type: 'info' }
    case 'failed':
      return { label: 'Не состоялся', type: 'warning' }
    default:
      return { label: status || '—', type: 'default' }
  }
}

export function callAppealLabel(key?: string | null, enums?: CRMEnums): string {
  if (!key) return '—'
  const found = enums?.call_types_of_appeal?.find((o) => o.key === key)?.label
  if (found) return found.replace(/\s*\(перенесено\)\s*$/i, '')
  return CALL_APPEAL_FALLBACK[key] || key
}

export function callResultLabel(id?: number | null, enums?: CRMEnums): string {
  if (!id) return ''
  const found = enums?.call_results?.find((o) => o.id === id)?.label
  if (found) return found.replace(/\s*\(перенесено\)\s*$/i, '')
  return ''
}

export function paymentOptionLabel(po: SubdivisionPaymentOption, enums?: CRMEnums): string {
  const byId = enums?.payment_options?.find((p) => p.id === po.payment_option_id)?.label
  if (byId) return byId
  if (po.key) {
    const byKey = enums?.payment_options?.find((p) => p.key === po.key)?.label
    if (byKey) return byKey
    if (PAYMENT_OPTION_FALLBACK[po.key]) return PAYMENT_OPTION_FALLBACK[po.key]
  }
  return PAYMENT_OPTION_FALLBACK[String(po.payment_option_id)] || `Способ #${po.payment_option_id}`
}

export function formatDateParts(iso?: string | null): { date: string; time: string } | null {
  if (!iso) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  const pad = (n: number) => String(n).padStart(2, '0')
  return {
    date: `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}`,
    time: `${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
}

export function isSuspiciousFutureYear(iso?: string | null): boolean {
  if (!iso) return false
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return false
  return d.getFullYear() > new Date().getFullYear() + 5
}

export function isOfficePlaceholderName(name?: string | null): boolean {
  const norm = (name || '').toLowerCase()
  return /(^|\s)(офис|запасной|тестов|командирован|ремонт плат)/i.test(norm)
}

export function exportRowsToCsv(filename: string, headers: string[], rows: (string | number)[][]): void {
  const escapeCell = (v: string | number) => {
    const s = String(v ?? '')
    return /[;"\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const lines = [headers.map(escapeCell).join(';'), ...rows.map((r) => r.map(escapeCell).join(';'))]
  const blob = new Blob(['\uFEFF' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
