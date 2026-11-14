export async function downloadFile(url: string, fallbackFilename: string): Promise<void> {
  const resp = await fetch(url, { credentials: 'include' })
  if (!resp.ok) {
    throw new Error(`Ошибка экспорта (${resp.status})`)
  }
  const blob = await resp.blob()
  const disp = resp.headers.get('content-disposition') || ''
  const match = disp.match(/filename="?([^"]+)"?/i)
  const filename = match?.[1] || fallbackFilename

  const objectUrl = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = objectUrl
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(objectUrl)
}

export function openPrintTab(url: string): void {
  window.open(url, '_blank', 'noopener,noreferrer')
}
