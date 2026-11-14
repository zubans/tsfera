export function formatPhoneRU(raw: string): string {
  const digits = raw.replace(/\D/g, '')
  const ten = digits.length >= 10 ? digits.slice(-10) : digits
  if (ten.length !== 10) return raw
  return `+7 (${ten.slice(0, 3)}) ${ten.slice(3, 6)}-${ten.slice(6, 8)}-${ten.slice(8, 10)}`
}

export function formatMoneyRUB(value: number): string {
  return new Intl.NumberFormat('ru-RU', {
    style: 'currency',
    currency: 'RUB',
    maximumFractionDigits: 2
  }).format(value || 0)
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

export function formatDateOnly(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('ru-RU')
}

export function documentStatusLabel(status: number): string {
  switch (status) {
    case 1:
      return 'Активна'
    case 2:
      return 'Проведена'
    case 3:
      return 'Провалена'
    case 4:
      return 'Отменена'
    default:
      return 'Неизвестно'
  }
}

export function taskStatusLabel(status: number): string {
  switch (status) {
    case 1:
      return 'Новая'
    case 2:
      return 'В работе'
    case 3:
      return 'Выполнена'
    case 4:
      return 'Недозвон'
    case 5:
      return 'Провалена'
    case 6:
      return 'Отменена'
    default:
      return '—'
  }
}

export function toDateInput(iso: string | null | undefined): string {
  if (!iso) return new Date().toISOString().slice(0, 10)
  return iso.slice(0, 10)
}

export function dateInputToRFC3339(yyyyMmDd: string): string {
  const clean = (yyyyMmDd || '').slice(0, 10)
  if (!clean) return new Date().toISOString()
  return `${clean}T00:00:00Z`
}

export function toDatetimeLocal(iso: string | null | undefined): string {
  const d = iso ? new Date(iso) : new Date()
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function datetimeLocalToRFC3339(localVal: string): string {
  if (!localVal) return new Date().toISOString()
  const d = new Date(localVal)
  if (Number.isNaN(d.getTime())) return new Date().toISOString()
  return d.toISOString()
}
