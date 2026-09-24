import { computed, reactive } from 'vue'
import { api, type Check, type Device, type PageSummary, type QueryPreset, type Service, type Session, type Settings, type Source } from '@/api'

interface State {
  session: Session | null
  settings: Settings | null
  pages: PageSummary[]
  services: Service[]
  presets: QueryPreset[]
  sources: Source[]
  checks: Check[]
  devices: Device[]
  loaded: { services: boolean; presets: boolean; sources: boolean; pages: boolean; checks: boolean; devices: boolean }
}

export const store = reactive<State>({
  session: null,
  settings: null,
  pages: [],
  services: [],
  presets: [],
  sources: [],
  checks: [],
  devices: [],
  loaded: { services: false, presets: false, sources: false, pages: false, checks: false, devices: false },
})

export const servicesById = computed(() => new Map(store.services.map((s) => [s.id, s])))
export const presetsById = computed(() => new Map(store.presets.map((p) => [p.id, p])))
export const checksById = computed(() => new Map(store.checks.map((c) => [c.id, c])))
export const sourcesById = computed(() => new Map(store.sources.map((s) => [s.id, s])))

export async function loadSession(): Promise<Session | null> {
  try {
    store.session = await api.session()
  } catch {
    store.session = null
  }
  return store.session
}

export async function loadSettings() {
  store.settings = await api.settings.get()
  return store.settings
}

export async function loadPages() {
  store.pages = (await api.pages.list()).sort((a, b) => a.order - b.order)
  store.loaded.pages = true
  return store.pages
}

/** Сохраняет порядок страниц: order каждой — её индекс в списке. Возвращает id изменённых. */
export async function reorderPages(list: PageSummary[]): Promise<string[]> {
  const changed: string[] = []
  try {
    for (const [order, summary] of list.entries()) {
      if (summary.order === order) continue
      const full = await api.pages.get(summary.id)
      await api.pages.save(full.id, { title: full.title, slug: full.slug, order, theme: full.theme, groups: full.groups, widgets: full.widgets }, full.revision)
      changed.push(full.id)
    }
  } finally {
    await loadPages()
  }
  return changed
}

export async function loadServices() {
  store.services = await api.services.list()
  store.loaded.services = true
  return store.services
}

export async function loadPresets() {
  store.presets = await api.presets.list()
  store.loaded.presets = true
  return store.presets
}

export async function loadSources() {
  store.sources = await api.sources.list()
  store.loaded.sources = true
  return store.sources
}

export async function loadChecks() {
  store.checks = await api.checks.list()
  store.loaded.checks = true
  return store.checks
}

export async function loadDevices() {
  store.devices = await api.devices.list()
  store.loaded.devices = true
  return store.devices
}

export async function ensureCatalog() {
  const tasks: Promise<unknown>[] = []
  if (!store.loaded.services) tasks.push(loadServices())
  if (!store.loaded.presets) tasks.push(loadPresets())
  if (!store.loaded.sources) tasks.push(loadSources())
  if (!store.loaded.checks) tasks.push(loadChecks())
  if (!store.loaded.devices) tasks.push(loadDevices())
  await Promise.all(tasks)
}

export function resetStore() {
  store.session = null
  store.settings = null
  store.pages = []
  store.services = []
  store.presets = []
  store.sources = []
  store.checks = []
  store.devices = []
  store.loaded = { services: false, presets: false, sources: false, pages: false, checks: false, devices: false }
}
