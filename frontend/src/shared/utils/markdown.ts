import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'
import katex from 'katex'
import markdownKatex from '@vscode/markdown-it-katex'

const md = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true
})

md.use(markdownKatex, { katex, throwOnError: false, trust: false, output: 'html', maxExpand: 1000 })

export function slugifyAnchor(heading: string): string {
  return heading
    .toLowerCase()
    .trim()
    .replace(/[^\p{L}\p{N}]+/gu, '-')
    .replace(/^-+|-+$/g, '')
}

// Добавляем якоря id к заголовкам для плавной навигации по оглавлению.
const defaultHeadingOpen =
  md.renderer.rules.heading_open ||
  ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))

md.renderer.rules.heading_open = (tokens, idx, options, env, self) => {
  const inline = tokens[idx + 1]
  if (inline && inline.content) {
    tokens[idx].attrSet('id', slugifyAnchor(inline.content))
  }
  return defaultHeadingOpen(tokens, idx, options, env, self)
}

export function renderMarkdown(raw: string): string {
  if (!raw) return ''
  const html = md.render(raw)
  return DOMPurify.sanitize(`<div>${html}</div>`, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed']
  })
}

export function extractOutlineClient(raw: string): { level: number; title: string; anchor: string }[] {
  const out: { level: number; title: string; anchor: string }[] = []
  let inFence = false
  for (const line of raw.split('\n')) {
    const t = line.trim()
    if (t.startsWith('```')) {
      inFence = !inFence
      continue
    }
    if (inFence) continue
    const match = t.match(/^(#{1,4})\s+(.+)$/)
    if (match) {
      out.push({
        level: match[1].length,
        title: match[2].trim(),
        anchor: slugifyAnchor(match[2])
      })
    }
  }
  return out
}
