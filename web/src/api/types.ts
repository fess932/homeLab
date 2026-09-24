export interface ApiErrorBody {
  code: string
  message: string
  details?: Record<string, unknown>
  request_id: string
}

export interface Session {
  user: { id: string; username: string }
  csrf_token: string
}

export interface Settings {
  title: string
  logo_asset_id: string | null
  start_page_id: string | null
  revision?: number
  restart_required?: string[]
  runtime?: {
    listen: string
    data_dir: string
    retention: string
    version: string
    tsdb_version: string
  }
}

export type ThemeMode = 'system' | 'light' | 'dark'
export type Density = 'comfortable' | 'compact'

export interface Theme {
  mode: ThemeMode
  accent: string
  background_asset_id: string | null
  density: Density
  columns: 4 | 6 | 8 | 12
}

export type Breakpoint = 'lg' | 'md' | 'sm'

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export interface Group {
  id: string
  title: string
  collapsed?: boolean
}

export type WidgetType = 'link' | 'links' | 'clock' | 'note' | 'number' | 'chart' | 'status'

export interface MetricRef {
  preset_id: string
  vars: Record<string, string>
}

export interface LinkConfig {
  service_id: string
  /** Простая ссылка без сервиса и проверки: используется, когда service_id пуст. */
  url?: string
  title?: string
  /** Иконка простой ссылки: «favicon» при сохранении сервер заменяет файлом с сайта. */
  icon?: string
  show_status: boolean
  show_latency: boolean
  metric: MetricRef | null
}
export interface LinksConfig {
  title: string
  service_ids: string[]
}
export interface ClockConfig {
  timezone: string
  hour12: boolean
  show_date: boolean
  show_seconds: boolean
}
export interface NoteConfig {
  markdown: string
}
export interface NumberConfig {
  title: string
  metric: MetricRef
  decimals: number
}
export interface ChartConfig {
  title: string
  metric: MetricRef
  range: RangeName
  stacked: boolean
}
export interface StatusConfig {
  service_id: string
}

export interface WidgetConfigMap {
  link: LinkConfig
  links: LinksConfig
  clock: ClockConfig
  note: NoteConfig
  number: NumberConfig
  chart: ChartConfig
  status: StatusConfig
}

export interface Widget<T extends WidgetType = WidgetType> {
  id: string
  group_id: string
  type: T
  config: WidgetConfigMap[T]
  layout: Partial<Record<Breakpoint, Rect>>
}

export interface PageSummary {
  id: string
  title: string
  slug: string
  order: number
  public?: boolean
  revision: number
}

export interface PageInput {
  title: string
  slug: string
  order?: number
  /** Страница целиком открывается без входа по адресу /public/<slug>. */
  public?: boolean
  theme: Theme
  groups: Group[]
  widgets: Widget[]
}

export interface Page extends PageInput {
  id: string
  revision: number
  updated_at?: string
}

export type OpenMode = 'same_tab' | 'new_tab'

export interface ServiceInput {
  name: string
  description: string
  url: string
  icon: string
  tags: string[]
  open_mode: OpenMode
  source_id: string | null
}

export type ProbeState = 'unknown' | 'up' | 'down' | 'stale' | 'disabled'

export interface CheckStatus {
  state: ProbeState
  pending?: 'up' | 'down' | null
  streak?: number
  last_run?: string | null
  last_success?: string | null
  duration_ms?: number | null
  http_status?: number | null
  error?: string
}

export interface Service extends ServiceInput {
  id: string
  revision: number
  check_id: string | null
  status?: CheckStatus
}

export type CheckKind = 'http' | 'tcp'

export interface CheckInput {
  service_id: string
  kind: CheckKind
  target: string
  expected_status: string
  interval_s: number
  timeout_s: number
  enabled: boolean
  ca_pem: string
}

export interface Check extends CheckInput {
  id: string
  revision: number
  status?: CheckStatus
}

export type SourceKind = 'prometheus' | 'node_exporter' | 'cadvisor'

export interface SourceInput {
  name: string
  kind: SourceKind
  url: string
  interval_s: number
  timeout_s: number
  labels: Record<string, string>
  secret_id: string | null
  tls: { ca_pem: string; server_name: string }
  enabled: boolean
}

export type SourceState = 'pending' | 'up' | 'down' | 'disabled'

export interface SourceStatus {
  state: SourceState
  last_attempt?: string | null
  last_success?: string | null
  duration_ms?: number | null
  samples?: number | null
  error?: string
}

export interface Source extends SourceInput {
  id: string
  system?: boolean
  revision: number
  status?: SourceStatus
}

export type SourceErrorKind =
  | ''
  | 'dns'
  | 'connect'
  | 'tls'
  | 'timeout'
  | 'http_status'
  | 'parse'
  | 'too_large'
  | 'too_many_samples'
  | 'forbidden_address'

export interface SourceTestResult {
  ok: boolean
  http_status: number | null
  duration_ms: number
  samples: number
  bytes: number
  error_kind: SourceErrorKind
  error: string
}

/** account:<драйвер> — подключённый облачный аккаунт драйвера. */
export type SecretKind = 'basic' | 'bearer' | 'key' | `account:${string}`

export interface SecretInput {
  name: string
  kind: SecretKind
  username?: string
  password?: string
  token?: string
  key?: string
}

export interface Secret {
  id: string
  name: string
  kind: SecretKind
  mask: string
  used_by: string[]
}

/** Тип устройства — имя драйвера из реестра drivers на сервере. */
export type DeviceKind = string

export interface DeviceInput {
  name: string
  kind: DeviceKind
  address: string
  interval_s: number
  timeout_s: number
  labels: Record<string, string>
  secret_id: string | null
  enabled: boolean
  /** Настройки драйвера; их форма описана в web/src/drivers/<драйвер>. */
  config: Record<string, unknown>
}

export interface DriverInfo {
  kind: string
  title: string
  secret_kinds: SecretKind[]
  secret_required: boolean
  discover: boolean
  accounts: boolean
}

export interface Candidate {
  name: string
  address: string
  config: Record<string, unknown>
  product_id?: string
  ref?: string
  account_id?: string
  has_key: boolean
  in_network: boolean
  online?: boolean
  note?: string
}

export interface DiscoverResponse {
  candidates: Candidate[]
  subnets: string[]
  warnings: string[]
  accounts: { id: string; name: string }[]
  added: Record<string, string>
}

export interface DriverLogin {
  id: string
  qr: string
  hint: string
  expires: string
}

export interface Reading {
  key: string
  unit: Unit
  value: number
  state?: string
}

export type DeviceErrorKind = '' | 'dns' | 'connect' | 'timeout' | 'http_status' | 'forbidden_address' | 'tls' | 'auth' | 'protocol' | 'parse' | 'cloud'

export interface DeviceStatus {
  state: 'pending' | 'up' | 'down' | 'disabled'
  last_attempt?: string | null
  last_success?: string | null
  duration_ms?: number | null
  error?: string
  error_kind?: DeviceErrorKind
  protocol?: string
  readings: Reading[]
}

export interface Device extends DeviceInput {
  id: string
  revision: number
  status: DeviceStatus
}

export type ThresholdColor = 'ok' | 'warn' | 'crit'
export interface Threshold {
  value: number
  color: ThresholdColor
  /** Порог снизу: окрашивается значение не выше value. */
  below?: boolean
  /** Норма для среднего за окно («24h» — суточная), а не для текущего значения. */
  window?: RangeName
}

export interface WindowAverage {
  window: RangeName
  value: number
  norm: Threshold
  exceeded: boolean
}

export type Unit =
  | ''
  | 'percent'
  | 'percent_unit'
  | 'bytes'
  | 'bytes_per_second'
  | 'seconds'
  | 'milliseconds'
  | 'count'
  | 'per_second'
  | 'celsius'
  | 'bool'
  | 'ppm'
  | 'ugm3'
  | 'mgm3'

export interface QueryPresetInput {
  title: string
  expression: string
  unit: Unit
  legend: string
  thresholds: Threshold[]
  min_step_s?: number
}

export type PresetCategory = 'homedeck' | 'tsdb' | 'node' | 'container' | 'probe' | 'device' | 'custom'

export interface QueryPreset extends QueryPresetInput {
  id: string
  builtin: boolean
  category: PresetCategory
  vars: string[]
  revision: number
}

export type RangeName = '1h' | '6h' | '24h' | '7d' | '30d'

export interface InstantQuery {
  query?: string
  preset_id?: string
  vars?: Record<string, string>
  time?: string
}

export interface RangeQuery {
  query?: string
  preset_id?: string
  vars?: Record<string, string>
  start: string
  end: string
  points?: number
}

export interface Series {
  labels: Record<string, string>
  name: string
  values: (number | null)[]
}

export interface RangeResult {
  start: number
  step: number
  unit: Unit
  series: Series[]
  warnings?: string[]
}

export interface InstantSample {
  labels: Record<string, string>
  name: string
  value: number | null
  time: number
}

export interface InstantResult {
  unit: Unit
  samples: InstantSample[]
  warnings?: string[]
}

export interface WidgetData {
  range?: RangeResult
  instant?: InstantResult
  status?: CheckStatus
  thresholds?: Threshold[]
  averages?: WindowAverage[]
}

export type TsdbState = 'starting' | 'running' | 'restarting' | 'stopped' | 'failed'
export type DiskLevel = 'ok' | 'warning' | 'critical'

export interface Status {
  now: string
  tsdb: { state: TsdbState; restarts: number; error: string; since: string; series: number | null; samples: number | null }
  config: { desired_revision: number; applied_revision: number; error: string }
  disk: {
    total_bytes: number
    free_bytes: number
    metrics_bytes: number
    app_bytes: number
    level: DiskLevel
  }
  services: { total: number; up: number; down: number; stale: number; unknown: number; disabled: number }
  sources: { total: number; up: number; down: number; pending: number; disabled: number }
}

export interface Asset {
  id: string
  media_type: string
  size: number
  width: number
  height: number
  checksum: string
  url: string
  created_at: string
}

export type ImportFormat = 'homedeck' | 'homer'

export interface ImportChange {
  entity: 'page' | 'service' | 'check' | 'source' | 'preset' | 'asset' | 'settings'
  action: 'create' | 'replace' | 'delete'
  name: string
}

export interface ImportPreview {
  token: string
  format: ImportFormat
  applied: boolean
  revision_id: number | null
  changes: ImportChange[]
  warnings: { path: string; message: string }[]
}

export interface ConfigRevision {
  id: number
  schema_version: number
  reason: string
  created_at: string
}

export interface PublicPage {
  title: string
  logo_asset_id: string | null
  page: Page
  services: Service[]
  presets: QueryPreset[]
}

export interface Readiness {
  sqlite?: string
  tsdb?: string
  config?: string
}
