import type { Breakpoint, PageInput, Rect, Theme, Widget, WidgetConfigMap, WidgetType } from '@/api/types'
import type { LayoutSource, Size } from './grid'

export const widgetTypes: WidgetType[] = ['link', 'links', 'clock', 'note', 'number', 'chart', 'status']

export const minSize: Record<WidgetType, Size> = {
  link: { w: 2, h: 1 },
  links: { w: 2, h: 2 },
  clock: { w: 2, h: 1 },
  note: { w: 2, h: 1 },
  number: { w: 2, h: 1 },
  chart: { w: 3, h: 2 },
  status: { w: 2, h: 1 },
}

export const defaultSize: Record<WidgetType, Size> = {
  link: { w: 3, h: 1 },
  links: { w: 3, h: 3 },
  clock: { w: 3, h: 1 },
  note: { w: 4, h: 2 },
  number: { w: 2, h: 1 },
  chart: { w: 6, h: 3 },
  status: { w: 3, h: 1 },
}

export function defaultConfig<T extends WidgetType>(type: T): WidgetConfigMap[T] {
  const configs: WidgetConfigMap = {
    link: { service_id: '', show_status: true, show_latency: false, metric: null },
    links: { title: '', service_ids: [] },
    clock: { timezone: '', hour12: false, show_date: true, show_seconds: false },
    note: { markdown: '' },
    number: { title: '', metric: { preset_id: '', vars: {} }, decimals: 1 },
    chart: { title: '', metric: { preset_id: '', vars: {} }, range: '24h', stacked: false },
    status: { service_id: '' },
  }
  return configs[type]
}

export const defaultTheme = (): Theme => ({
  mode: 'system',
  accent: '#22c3e6',
  background_asset_id: null,
  density: 'comfortable',
  columns: 12,
})

let seq = 0
export function newId(): string {
  seq++
  return `new_${Date.now().toString(36)}${seq.toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

export function blankPage(title: string, slug: string, groupTitle: string): PageInput {
  return { title, slug, theme: defaultTheme(), groups: [{ id: newId(), title: groupTitle }], widgets: [] }
}

export function layoutSources(widgets: Widget[]): LayoutSource[] {
  return widgets.map((w) => ({ id: w.id, layout: w.layout, min: minSize[w.type], size: defaultSize[w.type] }))
}

export function setRect(w: Widget, bp: Breakpoint, r: Rect): Widget {
  return { ...w, layout: { ...w.layout, [bp]: r } }
}

export function referencedServices(w: Widget): string[] {
  switch (w.type) {
    case 'link':
    case 'status':
      return [(w.config as WidgetConfigMap['link']).service_id].filter(Boolean)
    case 'links':
      return (w.config as WidgetConfigMap['links']).service_ids
    default:
      return []
  }
}

export function metricOf(w: Widget): { preset_id: string; vars: Record<string, string> } | null {
  if (w.type === 'number' || w.type === 'chart') return (w.config as WidgetConfigMap['chart']).metric
  if (w.type === 'link') return (w.config as WidgetConfigMap['link']).metric
  return null
}

export function isWidget<T extends WidgetType>(w: Widget, type: T): w is Widget<T> {
  return w.type === type
}

export function slugify(s: string): string {
  const map: Record<string, string> = {
    а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z', и: 'i', й: 'y', к: 'k', л: 'l',
    м: 'm', н: 'n', о: 'o', п: 'p', р: 'r', с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'c', ч: 'ch', ш: 'sh',
    щ: 'sch', ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
  }
  const out = s
    .toLowerCase()
    .split('')
    .map((c) => map[c] ?? c)
    .join('')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
  return out || 'page'
}
