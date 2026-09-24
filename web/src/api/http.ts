import type { ApiErrorBody } from './types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: Record<string, unknown>
  readonly requestId: string

  constructor(status: number, body: Partial<ApiErrorBody>) {
    super(body.message || `HTTP ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.code = body.code || 'internal'
    this.details = body.details ?? {}
    this.requestId = body.request_id ?? ''
  }

  fieldErrors(): Record<string, string> {
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(this.details)) {
      if (typeof v === 'string') out[k] = v
    }
    return out
  }
}

export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

export type Method = 'GET' | 'POST' | 'PUT' | 'DELETE'

export interface RequestOptions {
  method?: Method
  body?: unknown
  revision?: number
  signal?: AbortSignal
  anonymous?: boolean
  raw?: boolean
}

export interface ClientDeps {
  fetch?: typeof fetch
  onUnauthorized?: () => void
}

const unsafe = new Set<Method>(['POST', 'PUT', 'DELETE'])

export function createHttp(deps: ClientDeps = {}) {
  let csrf = ''
  let onUnauthorized = deps.onUnauthorized

  const doFetch = (input: string, init: RequestInit) => (deps.fetch ?? globalThis.fetch)(input, init)

  async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const method = opts.method ?? 'GET'
    const headers: Record<string, string> = { Accept: 'application/json' }
    let body: BodyInit | undefined
    if (opts.body instanceof FormData) {
      body = opts.body
    } else if (opts.body !== undefined) {
      headers['Content-Type'] = 'application/json'
      body = JSON.stringify(opts.body)
    }
    if (unsafe.has(method) && csrf) headers['X-CSRF-Token'] = csrf
    if (opts.revision !== undefined) headers['If-Match'] = `"${opts.revision}"`

    const res = await doFetch(path, {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: opts.signal,
    })

    if (!res.ok) {
      let parsed: Partial<ApiErrorBody> = {}
      try {
        parsed = (await res.json()) as ApiErrorBody
      } catch {
        parsed = { code: res.status === 401 ? 'unauthorized' : 'internal', message: res.statusText }
      }
      const err = new ApiError(res.status, parsed)
      if (res.status === 401 && !opts.anonymous) onUnauthorized?.()
      throw err
    }
    if (opts.raw) return res as unknown as T
    if (res.status === 204) return undefined as T
    const type = res.headers.get('Content-Type') ?? ''
    if (type.includes('application/json')) return (await res.json()) as T
    return (await res.text()) as unknown as T
  }

  return {
    request,
    setCsrf(token: string) {
      csrf = token
    },
    csrf: () => csrf,
    setUnauthorizedHandler(fn: () => void) {
      onUnauthorized = fn
    },
  }
}

export type Http = ReturnType<typeof createHttp>
