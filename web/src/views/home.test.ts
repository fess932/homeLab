import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import HomeView from './HomeView.vue'
import { api } from '@/api'
import type { Page, Service } from '@/api/types'
import { resetStore, store } from '@/stores/app'

// Фикстуры: одна страница с одной группой и двумя виджетами.
const service: Service = {
  id: 'svc_nas',
  name: 'NAS',
  description: 'Synology',
  url: 'http://nas.lan:5000',
  icon: 'builtin:hard-drive',
  tags: ['storage'],
  open_mode: 'new_tab',
  source_id: null,
  revision: 1,
  check_id: 'chk_1',
  status: { state: 'up', duration_ms: 12 },
}

const page: Page = {
  id: 'pg_home',
  title: 'Дом',
  slug: 'home',
  order: 0,
  revision: 5,
  theme: { mode: 'system', accent: '#3b82f6', background_asset_id: null, density: 'comfortable', columns: 12 },
  groups: [{ id: 'grp_1', title: 'Хранилище' }],
  widgets: [
    {
      id: 'wgt_a',
      group_id: 'grp_1',
      type: 'link',
      config: { service_id: 'svc_nas', show_status: true, show_latency: true, metric: null },
      layout: { lg: { x: 0, y: 0, w: 3, h: 1 } },
    },
    {
      id: 'wgt_b',
      group_id: 'grp_1',
      type: 'note',
      config: { markdown: 'Привет' },
      layout: { lg: { x: 3, y: 0, w: 4, h: 2 } },
    },
  ],
}

type Call = { url: string; method: string; body: unknown; headers: Record<string, string> }
let calls: Call[] = []
let putStatus = 200

function respond(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  calls = []
  putStatus = 200
  resetStore()
  store.session = { user: { id: 'u1', username: 'admin' }, csrf_token: 'csrf-x' }
  api.http.setCsrf('csrf-x')
  // jsdom не знает про ResizeObserver и layout — делаем широкий экран
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'innerWidth', { value: 1440, configurable: true })
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const method = init.method ?? 'GET'
      calls.push({
        url,
        method,
        body: typeof init.body === 'string' ? JSON.parse(init.body) : init.body,
        headers: (init.headers ?? {}) as Record<string, string>,
      })
      if (url === '/api/v1/pages' && method === 'GET') return respond([{ id: page.id, title: page.title, slug: page.slug, order: 0, revision: page.revision }])
      if (url.startsWith('/api/v1/pages/') && method === 'GET') return respond(page)
      if (url.startsWith('/api/v1/pages/') && method === 'PUT') {
        if (putStatus !== 200) return respond({ code: 'conflict', message: 'conflict', request_id: 'r1' }, putStatus)
        return respond({ ...page, ...(JSON.parse(init.body as string) as object), revision: page.revision + 1 })
      }
      if (url === '/api/v1/settings') return respond({ title: 'Мой дом', logo_asset_id: null, start_page_id: 'pg_home', public_page_id: null, revision: 1 })
      if (url === '/api/v1/services') return respond([service])
      if (url === '/api/v1/checks')
        return respond([{ id: 'chk_1', service_id: 'svc_nas', kind: 'http', target: 'http://nas.lan:5000/health', expected_status: '200-399', interval_s: 30, timeout_s: 5, enabled: true, ca_pem: '', revision: 1 }])
      if (url === '/api/v1/presets' || url === '/api/v1/sources') return respond([])
      return respond({ code: 'not_found', message: 'nf', request_id: 'x' }, 404)
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountHome() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView },
      { path: '/p/:slug', component: HomeView },
    ],
  })
  await router.push('/')
  const wrapper = mount(HomeView, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('HomeView', () => {
  it('renders start page with service card, status text and note', async () => {
    const w = await mountHome()
    expect(w.text()).toContain('Хранилище')
    const link = w.find('a.w-link')
    expect(link.attributes('href')).toBe('http://nas.lan:5000')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.text()).toContain('доступен')
    // статус объясняет, какой проверкой он определён
    expect(link.find('.badge').attributes('title')).toContain('http://nas.lan:5000/health')
    expect(w.find('.w-note').text()).toBe('Привет')
    w.unmount()
  })

  it('filters by tag in search', async () => {
    const w = await mountHome()
    await w.find('input[type="search"]').setValue('stor')
    expect(w.findAll('.results a.w-link')).toHaveLength(1)
    await w.find('input[type="search"]').setValue('zzz')
    expect(w.text()).toContain('Ничего не найдено')
    w.unmount()
  })

  it('edits a draft, moves widget with buttons and saves atomically with If-Match', async () => {
    const w = await mountHome()
    await w.findAll('button').find((b) => b.text().includes('Редактировать'))!.trigger('click')
    expect(w.text()).toContain('Редактирование страницы')

    // вертикальная гравитация не даёт опустить виджет в пустоту, поэтому двигаем вправо
    await w.find('[aria-label="Вправо"]').trigger('click')
    expect(w.text()).toContain('Черновик')

    await w.findAll('button').find((b) => b.text().includes('Сохранить страницу'))!.trigger('click')
    await flushPromises()

    const put = calls.find((c) => c.method === 'PUT')!
    expect(put.url).toBe('/api/v1/pages/pg_home')
    expect(put.headers['If-Match']).toBe('"5"')
    expect(put.headers['X-CSRF-Token']).toBe('csrf-x')
    const body = put.body as Page
    // весь layout уходит одним запросом: первый виджет сдвинут, соседний вытеснен вниз
    expect(body.widgets.find((x) => x.id === 'wgt_a')!.layout.lg).toMatchObject({ x: 1, y: 0 })
    expect(body.widgets.find((x) => x.id === 'wgt_b')!.layout.lg!.y).toBeGreaterThan(0)
    expect(w.text()).not.toContain('Редактирование страницы')
    w.unmount()
  })

  it('cancel restores original layout without requests', async () => {
    const w = await mountHome()
    await w.findAll('button').find((b) => b.text().includes('Редактировать'))!.trigger('click')
    await w.find('[aria-label="Шире"]').trigger('click')
    await w.findAll('button').find((b) => b.text().includes('Отменить изменения'))!.trigger('click')
    expect(calls.some((c) => c.method === 'PUT')).toBe(false)
    expect(w.text()).not.toContain('Черновик')
    w.unmount()
  })

  it('shows conflict message on 409 and keeps the draft', async () => {
    putStatus = 409
    const w = await mountHome()
    await w.findAll('button').find((b) => b.text().includes('Редактировать'))!.trigger('click')
    await w.find('[aria-label="Шире"]').trigger('click')
    await w.findAll('button').find((b) => b.text().includes('Сохранить страницу'))!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Страница изменена в другом месте')
    expect(w.text()).toContain('Редактирование страницы')
    w.unmount()
  })
})
