import { inject, provide, ref, watch, type InjectionKey, type Ref } from 'vue'
import { api, type Check, type QueryPreset, type RangeName, type Service, type Widget, type WidgetData } from '@/api'
import { chartLimiter } from '@/lib/queue'
import { rangeSeconds } from '@/lib/time'
import { metricOf } from '@/lib/widgets'
import { usePolling } from '@/composables/polling'

export type WidgetMode = 'view' | 'edit' | 'public'

export interface DataRequest {
  range?: RangeName
  points?: number
}

export interface WidgetContext {
  mode: WidgetMode
  service(id: string): Service | undefined
  preset(id: string): QueryPreset | undefined
  check(id: string | null): Check | undefined
  load(w: Widget, req: DataRequest, signal: AbortSignal): Promise<WidgetData>
}

const key: InjectionKey<WidgetContext> = Symbol('widget-context')

export function provideWidgetContext(ctx: WidgetContext) {
  provide(key, ctx)
}

export function useWidgetContext(): WidgetContext {
  const ctx = inject(key)
  if (!ctx) throw new Error('widget context missing')
  return ctx
}

export async function loadDirect(
  w: Widget,
  req: DataRequest,
  preset: (id: string) => QueryPreset | undefined,
  signal: AbortSignal,
): Promise<WidgetData> {
  const metric = metricOf(w)
  if (!metric?.preset_id) return {}
  const p = preset(metric.preset_id)
  const thresholds = p?.thresholds ?? []
  if (req.range) {
    const end = new Date()
    const start = new Date(end.getTime() - rangeSeconds[req.range] * 1000)
    const range = await api.queryRange(
      { preset_id: metric.preset_id, vars: metric.vars, start: start.toISOString(), end: end.toISOString(), points: req.points },
      signal,
    )
    return { range, thresholds }
  }
  const instant = await api.query({ preset_id: metric.preset_id, vars: metric.vars }, signal)
  return { instant, thresholds }
}

export function createContext(
  mode: WidgetMode | (() => WidgetMode),
  services: () => Map<string, Service>,
  presets: () => Map<string, QueryPreset>,
  checks: () => Map<string, Check> = () => new Map(),
  publicSlug: () => string = () => '',
): WidgetContext {
  const preset = (id: string) => presets().get(id)
  const currentMode = typeof mode === 'function' ? mode : () => mode
  return {
    get mode() {
      return currentMode()
    },
    service: (id) => services().get(id),
    preset,
    check: (id) => (id ? checks().get(id) : undefined),
    load(w, req, signal) {
      const mode = currentMode()
      if (mode === 'public') return api.public.widgetData(publicSlug(), w.id, req, signal)
      if (mode === 'view' && !w.id.startsWith('new_')) return api.widgetData(w.id, req, signal)
      return loadDirect(w, req, preset, signal)
    },
  }
}

export function useWidgetData(widget: Ref<Widget>, req: Ref<DataRequest>, enabled: Ref<boolean> = ref(true)) {
  const ctx = useWidgetContext()
  const data = ref<WidgetData | null>(null)
  const error = ref<unknown>(null)
  const poll = usePolling(async (signal) => {
    if (!enabled.value) {
      data.value = null
      return
    }
    try {
      data.value = await chartLimiter.run(() => ctx.load(widget.value, req.value, signal), signal)
      error.value = null
    } catch (e) {
      if (signal.aborted) return
      error.value = e
    }
  }, [])
  watch(
    () => [JSON.stringify(widget.value.config), JSON.stringify(req.value), enabled.value],
    () => void poll.run(),
  )
  return { data, error, loading: poll.loading }
}
