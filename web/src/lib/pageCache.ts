import type { Page, PublicPage } from '@/api/types'
import { metricOf, referencedServices } from './widgets'

const key = (slug: string) => `homedeck:public:${slug}`

export function readCachedPage(slug: string): PublicPage | null {
  try {
    const raw = localStorage.getItem(key(slug))
    return raw ? (JSON.parse(raw) as PublicPage) : null
  } catch {
    return null
  }
}

export function withoutPrivate(page: Page): Page {
  const hidden = new Set(page.groups.filter((g) => g.private).map((g) => g.id))
  if (!hidden.size) return page
  return {
    ...page,
    groups: page.groups.filter((g) => !hidden.has(g.id)),
    widgets: page.widgets.filter((w) => !hidden.has(w.group_id)),
  }
}

export function writeCachedPage(slug: string, data: PublicPage) {
  const page = withoutPrivate(data.page)
  const used = new Set(page.widgets.flatMap(referencedServices))
  const services = data.services.filter((s) => used.has(s.id)).map(({ status: _, ...s }) => s)
  const metrics = new Set(page.widgets.map((w) => metricOf(w)?.preset_id))
  const presets = data.presets.filter((p) => metrics.has(p.id))
  try {
    localStorage.setItem(key(slug), JSON.stringify({ ...data, authenticated: false, page, services, presets }))
  } catch {
    return
  }
}

export function dropCachedPage(slug: string) {
  try {
    localStorage.removeItem(key(slug))
  } catch {
    return
  }
}
