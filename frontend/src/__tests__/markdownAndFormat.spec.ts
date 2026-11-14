import { describe, it, expect } from 'vitest'
import { renderMarkdown, extractOutlineClient, slugifyAnchor } from '@/shared/utils/markdown'
import {
  formatPhoneRU,
  formatMoneyRUB,
  documentStatusLabel,
  taskStatusLabel,
  toDateInput,
  dateInputToRFC3339
} from '@/shared/utils/format'

describe('markdown & outline helpers', () => {
  it('renders inline and block mathematics without trusted HTML commands', () => {
    const html = renderMarkdown('$x^2$\n\n$$\n\\frac{1}{2}\n$$')
    expect(html).toContain('class="katex"')
    expect(html).toContain('katex-display')
    expect(renderMarkdown('$\\href{javascript:alert(1)}{x}$')).not.toContain('href="javascript:')
  })

  it('slugifies Cyrillic and Latin headings and adds id anchors', () => {
    expect(slugifyAnchor('Регламент ТО Котла Baxi')).toBe('регламент-то-котла-baxi')
    const html = renderMarkdown('# Регламент ТО\n\nТекст с **выделением**')
    expect(html).toContain('<h1 id="регламент-то">Регламент ТО</h1>')
    expect(html).toContain('<strong>выделением</strong>')
  })

  it('sanitizes dangerous script tags and javascript links', () => {
    const html = renderMarkdown(
      '<script>alert(1)</script><img src=x onerror="alert(2)">\n[xss](javascript:alert(1))\n# Заголовок'
    )
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<a ')
    expect(html).not.toContain('href="javascript:')
    expect(html).toContain('<h1 id="заголовок">Заголовок</h1>')
  })

  it('extracts outline ignoring headers inside fenced code blocks', () => {
    const md = [
      '# Раздел 1',
      '```bash',
      '# комментарий внутри кода',
      '```',
      '## Подраздел 1.1',
      '### Шаг А'
    ].join('\n')
    expect(extractOutlineClient(md)).toEqual([
      { level: 1, title: 'Раздел 1', anchor: 'раздел-1' },
      { level: 2, title: 'Подраздел 1.1', anchor: 'подраздел-1-1' },
      { level: 3, title: 'Шаг А', anchor: 'шаг-а' }
    ])
  })
})

describe('format helpers', () => {
  it('formats 10-digit and 11-digit Russian phone numbers uniformly', () => {
    expect(formatPhoneRU('9991234567')).toBe('+7 (999) 123-45-67')
    expect(formatPhoneRU('89991234567')).toBe('+7 (999) 123-45-67')
    expect(formatPhoneRU('123')).toBe('123')
  })

  it('formats RUB currency and status labels', () => {
    expect(formatMoneyRUB(4500)).toContain('4')
    expect(documentStatusLabel(1)).toBe('Активна')
    expect(documentStatusLabel(2)).toBe('Проведена')
    expect(taskStatusLabel(4)).toBe('Недозвон')
  })

  it('converts between YYYY-MM-DD and RFC3339 date strings', () => {
    expect(toDateInput('2026-11-13T12:30:00Z')).toBe('2026-11-13')
    expect(dateInputToRFC3339('2026-11-13')).toBe('2026-11-13T00:00:00Z')
  })
})
