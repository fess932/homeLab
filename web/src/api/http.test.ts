import { describe, expect, it, vi } from 'vitest'
import { ApiError, createHttp } from './http'
import { createApi } from './index'

// Минимальный фейковый fetch: запоминает запросы и отдаёт заранее заданный ответ.
function fakeFetch(respond: (url: string, init: RequestInit) => Response) {
  const calls: { url: string; init: RequestInit }[] = []
  const fn = vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ url, init })
    return respond(url, init)
  })
  return { fn: fn as unknown as typeof fetch, calls }
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

const headers = (init: RequestInit) => init.headers as Record<string, string>

describe('http client', () => {
  it('sends CSRF token only on state-changing requests', async () => {
    const { fn, calls } = fakeFetch(() => json({ ok: true }))
    const http = createHttp({ fetch: fn })
    http.setCsrf('tok123')

    await http.request('/api/v1/pages')
    await http.request('/api/v1/pages', { method: 'POST', body: { a: 1 } })
    await http.request('/api/v1/pages/x', { method: 'DELETE' })

    expect(headers(calls[0]!.init)['X-CSRF-Token']).toBeUndefined()
    expect(headers(calls[1]!.init)['X-CSRF-Token']).toBe('tok123')
    expect(headers(calls[2]!.init)['X-CSRF-Token']).toBe('tok123')
    // куки сессии должны уходить на тот же origin
    expect(calls[0]!.init.credentials).toBe('same-origin')
  })

  it('sets quoted If-Match from revision', async () => {
    const { fn, calls } = fakeFetch(() => json({}))
    const http = createHttp({ fetch: fn })
    await http.request('/api/v1/pages/p1', { method: 'PUT', body: {}, revision: 7 })
    expect(headers(calls[0]!.init)['If-Match']).toBe('"7"')
  })

  it('serializes JSON bodies but leaves FormData to the browser', async () => {
    const { fn, calls } = fakeFetch(() => json({}))
    const http = createHttp({ fetch: fn })
    await http.request('/a', { method: 'POST', body: { x: 1 } })
    const fd = new FormData()
    fd.append('file', new Blob(['x']), 'a.png')
    await http.request('/b', { method: 'POST', body: fd })

    expect(headers(calls[0]!.init)['Content-Type']).toBe('application/json')
    expect(calls[0]!.init.body).toBe('{"x":1}')
    // для multipart boundary выставляет сам fetch
    expect(headers(calls[1]!.init)['Content-Type']).toBeUndefined()
    expect(calls[1]!.init.body).toBe(fd)
  })

  it('parses API error body into ApiError', async () => {
    const { fn } = fakeFetch(() =>
      json({ code: 'validation', message: 'bad', details: { slug: 'занят', n: 1 }, request_id: 'r-1' }, 422),
    )
    const http = createHttp({ fetch: fn })
    const err = await http.request('/x', { method: 'POST', body: {} }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    const e = err as ApiError
    expect(e.status).toBe(422)
    expect(e.code).toBe('validation')
    expect(e.requestId).toBe('r-1')
    // нестроковые details не попадают в ошибки полей
    expect(e.fieldErrors()).toEqual({ slug: 'занят' })
  })

  it('falls back when error body is not JSON', async () => {
    const { fn } = fakeFetch(() => new Response('oops', { status: 502, statusText: 'Bad Gateway' }))
    const http = createHttp({ fetch: fn })
    const e = (await http.request('/x').catch((x: unknown) => x)) as ApiError
    expect(e.status).toBe(502)
    expect(e.code).toBe('internal')
  })

  it('calls unauthorized handler on 401 except for anonymous requests', async () => {
    const onUnauthorized = vi.fn()
    const { fn } = fakeFetch(() => json({ code: 'unauthorized', message: 'no', request_id: 'r' }, 401))
    const http = createHttp({ fetch: fn, onUnauthorized })

    await http.request('/api/v1/session', { anonymous: true }).catch(() => undefined)
    expect(onUnauthorized).not.toHaveBeenCalled()

    await http.request('/api/v1/pages').catch(() => undefined)
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('returns undefined for 204 and text for non-JSON', async () => {
    const { fn } = fakeFetch((url) =>
      url === '/empty' ? new Response(null, { status: 204 }) : new Response('a: 1', { headers: { 'Content-Type': 'application/yaml' } }),
    )
    const http = createHttp({ fetch: fn })
    expect(await http.request('/empty', { method: 'DELETE' })).toBeUndefined()
    expect(await http.request('/yaml')).toBe('a: 1')
  })

  it('passes abort signal through', async () => {
    const { fn, calls } = fakeFetch(() => json({}))
    const http = createHttp({ fetch: fn })
    const c = new AbortController()
    await http.request('/x', { signal: c.signal })
    expect(calls[0]!.init.signal).toBe(c.signal)
  })
})

describe('api wrapper', () => {
  it('stores CSRF token from login and uses it afterwards', async () => {
    const { fn, calls } = fakeFetch((url) =>
      url === '/api/v1/login' ? json({ user: { id: 'u1', username: 'admin' }, csrf_token: 'c-1' }) : json({}),
    )
    const api = createApi(createHttp({ fetch: fn }))
    await api.login('admin', 'secret-password')
    await api.pages.save('p1', { title: 'a', slug: 'a', theme: {} as never, groups: [], widgets: [] }, 3)

    const put = calls[1]!
    expect(put.url).toBe('/api/v1/pages/p1')
    expect(headers(put.init)['X-CSRF-Token']).toBe('c-1')
    expect(headers(put.init)['If-Match']).toBe('"3"')
  })

  it('builds widget data query string without empty params', async () => {
    const { fn, calls } = fakeFetch(() => json({}))
    const api = createApi(createHttp({ fetch: fn }))
    await api.widgetData('wgt_1', { range: '24h', points: 200, start: undefined })
    expect(calls[0]!.url).toBe('/api/v1/widgets/wgt_1/data?range=24h&points=200')
  })
})
