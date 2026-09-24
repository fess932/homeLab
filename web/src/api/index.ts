import { createHttp, type Http } from './http'
import type {
  Asset,
  Check,
  CheckInput,
  ConfigRevision,
  Device,
  DeviceInput,
  DeviceStatus,
  DiscoverResponse,
  DriverInfo,
  DriverLogin,
  ImportFormat,
  ImportPreview,
  InstantQuery,
  InstantResult,
  Page,
  PageInput,
  PageSummary,
  PublicPage,
  QueryPreset,
  QueryPresetInput,
  RangeName,
  RangeQuery,
  RangeResult,
  Secret,
  SecretInput,
  Service,
  ServiceInput,
  Session,
  Settings,
  Source,
  SourceInput,
  SourceTestResult,
  Status,
  WidgetData,
} from './types'

export * from './http'
export type * from './types'

const enc = encodeURIComponent

export function createApi(http: Http) {
  const r = http.request
  const crud = <T, I>(base: string) => ({
    list: (signal?: AbortSignal) => r<T[]>(base, { signal }),
    get: (id: string, signal?: AbortSignal) => r<T>(`${base}/${enc(id)}`, { signal }),
    create: (input: I) => r<T>(base, { method: 'POST', body: input }),
    update: (id: string, input: I, revision: number) =>
      r<T>(`${base}/${enc(id)}`, { method: 'PUT', body: input, revision }),
    remove: (id: string) => r<void>(`${base}/${enc(id)}`, { method: 'DELETE' }),
  })

  const remember = async (p: Promise<Session>) => {
    const s = await p
    http.setCsrf(s.csrf_token)
    return s
  }

  return {
    http,
    setupRequired: () => r<{ required: boolean }>('/api/v1/setup', { anonymous: true }),
    setup: (body: { token: string; username: string; password: string; title: string }) =>
      remember(r<Session>('/api/v1/setup', { method: 'POST', body, anonymous: true })),
    login: (username: string, password: string) =>
      remember(r<Session>('/api/v1/login', { method: 'POST', body: { username, password }, anonymous: true })),
    logout: async () => {
      await r<void>('/api/v1/logout', { method: 'POST', anonymous: true })
      http.setCsrf('')
    },
    session: () => remember(r<Session>('/api/v1/session', { anonymous: true })),
    changePassword: (current: string, next: string) =>
      r<void>('/api/v1/password', { method: 'PUT', body: { current, next } }),

    settings: {
      get: () => r<Settings>('/api/v1/settings'),
      update: (s: Settings, revision: number) => r<Settings>('/api/v1/settings', { method: 'PUT', body: s, revision }),
    },

    pages: {
      list: () => r<PageSummary[]>('/api/v1/pages'),
      get: (idOrSlug: string, signal?: AbortSignal) => r<Page>(`/api/v1/pages/${enc(idOrSlug)}`, { signal }),
      create: (p: PageInput) => r<Page>('/api/v1/pages', { method: 'POST', body: p }),
      save: (id: string, p: PageInput, revision: number) =>
        r<Page>(`/api/v1/pages/${enc(id)}`, { method: 'PUT', body: p, revision }),
      remove: (id: string) => r<void>(`/api/v1/pages/${enc(id)}`, { method: 'DELETE' }),
    },

    services: crud<Service, ServiceInput>('/api/v1/services'),
    checks: crud<Check, CheckInput>('/api/v1/checks'),
    sources: {
      ...crud<Source, SourceInput>('/api/v1/sources'),
      test: (id: string) => r<SourceTestResult>(`/api/v1/sources/${enc(id)}/test`, { method: 'POST' }),
      testDraft: (input: SourceInput) => r<SourceTestResult>('/api/v1/sources/test', { method: 'POST', body: input }),
    },
    devices: {
      ...crud<Device, DeviceInput>('/api/v1/devices'),
      /** Создание с учётными данными в том же запросе: сервер сохранит их вместе с устройством. */
      createWith: (input: DeviceInput & { secret?: SecretInput }) => r<Device>('/api/v1/devices', { method: 'POST', body: input }),
      updateWith: (id: string, input: DeviceInput & { secret?: SecretInput }, revision: number) =>
        r<Device>(`/api/v1/devices/${enc(id)}`, { method: 'PUT', body: input, revision }),
      test: (input: DeviceInput & { secret?: SecretInput }) =>
        r<DeviceStatus>('/api/v1/devices/test', { method: 'POST', body: input }),
    },
    drivers: {
      list: () => r<DriverInfo[]>('/api/v1/drivers'),
      discover: (kind: string, subnet = '') =>
        r<DiscoverResponse>(`/api/v1/drivers/${enc(kind)}/discover`, { method: 'POST', body: { subnet } }),
      login: (kind: string, params: Record<string, string>) =>
        r<DriverLogin>(`/api/v1/drivers/${enc(kind)}/login`, { method: 'POST', body: params }),
      checkLogin: (kind: string, id: string) =>
        r<{ state: 'pending' | 'done'; account?: Secret }>(`/api/v1/drivers/${enc(kind)}/login/${enc(id)}`),
      adopt: (kind: string, body: { account_id: string; ref: string; name: string; address: string }) =>
        r<Device>(`/api/v1/drivers/${enc(kind)}/adopt`, { method: 'POST', body }),
    },
    secrets: {
      list: () => r<Secret[]>('/api/v1/secrets'),
      create: (s: SecretInput) => r<Secret>('/api/v1/secrets', { method: 'POST', body: s }),
      update: (id: string, s: SecretInput) => r<Secret>(`/api/v1/secrets/${enc(id)}`, { method: 'PUT', body: s }),
      remove: (id: string) => r<void>(`/api/v1/secrets/${enc(id)}`, { method: 'DELETE' }),
    },
    presets: crud<QueryPreset, QueryPresetInput>('/api/v1/presets'),

    status: (signal?: AbortSignal) => r<Status>('/api/v1/status', { signal }),
    query: (q: InstantQuery, signal?: AbortSignal) =>
      r<InstantResult>('/api/v1/metrics/query', { method: 'POST', body: q, signal }),
    queryRange: (q: RangeQuery, signal?: AbortSignal) =>
      r<RangeResult>('/api/v1/metrics/query-range', { method: 'POST', body: q, signal }),
    widgetData: (
      id: string,
      params: { range?: RangeName; start?: string; end?: string; points?: number },
      signal?: AbortSignal,
    ) => r<WidgetData>(`/api/v1/widgets/${enc(id)}/data${qs(params)}`, { signal }),

    assets: {
      list: () => r<Asset[]>('/api/v1/assets'),
      upload: (file: File) => {
        const fd = new FormData()
        fd.append('file', file)
        return r<Asset>('/api/v1/assets', { method: 'POST', body: fd })
      },
      remove: (id: string) => r<void>(`/api/v1/assets/${enc(id)}`, { method: 'DELETE' }),
    },

    importPreview: (format: ImportFormat, file: File, assets?: File) => {
      const fd = new FormData()
      fd.append('format', format)
      fd.append('file', file)
      if (assets) fd.append('assets', assets)
      return r<ImportPreview>('/api/v1/import/preview', { method: 'POST', body: fd })
    },
    importApply: (token: string) => r<ImportPreview>('/api/v1/import/apply', { method: 'POST', body: { token } }),
    exportUrl: '/api/v1/export',
    revisions: () => r<ConfigRevision[]>('/api/v1/revisions'),

    public: {
      page: () => r<PublicPage>('/api/v1/public', { anonymous: true }),
      widgetData: (id: string, params: { range?: RangeName; points?: number }, signal?: AbortSignal) =>
        r<WidgetData>(`/api/v1/public/widgets/${enc(id)}/data${qs(params)}`, { signal, anonymous: true }),
    },
  }
}

function qs(params: Record<string, string | number | undefined>): string {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') u.set(k, String(v))
  const s = u.toString()
  return s ? `?${s}` : ''
}

export type Api = ReturnType<typeof createApi>

export const api = createApi(createHttp())

export function assetUrl(id: string | null | undefined): string | null {
  return id ? `/assets/${enc(id)}` : null
}
