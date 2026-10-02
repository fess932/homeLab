import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import PublicView from './PublicView.vue'
import type { PublicPage } from '@/api/types'

const KEY = 'homedeck:public:links'

// Фикстура: публичная страница с одной ссылкой на сервис с проверкой.
const fresh: PublicPage = {
  title: 'Дом',
  logo_asset_id: null,
  presets: [],
  services: [
    {
      id: 'svc_nas',
      name: 'NAS',
      description: '',
      url: 'http://nas.lan:5000',
      icon: 'builtin:hard-drive',
      tags: [],
      open_mode: 'new_tab',
      source_id: null,
      revision: 1,
      check_id: 'chk_1',
      status: { state: 'up', duration_ms: 12 },
    },
  ],
  page: {
    id: 'pg_links',
    title: 'Ссылки',
    slug: 'links',
    order: 0,
    revision: 1,
    public: true,
    theme: { mode: 'system', accent: '#22c3e6', background_asset_id: null, density: 'comfortable', columns: 12 },
    groups: [{ id: 'grp_1', title: 'Ссылки' }],
    widgets: [
      {
        id: 'wgt_a',
        group_id: 'grp_1',
        type: 'link',
        config: { service_id: 'svc_nas', show_status: true, show_latency: false, metric: null },
        layout: { lg: { x: 0, y: 0, w: 3, h: 1 } },
      },
    ],
  },
}

let answer: () => Promise<Response>

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

async function open(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/public/:slug?', component: PublicView }],
  })
  await router.push(path)
  await router.isReady()
  return mount(PublicView, { global: { plugins: [router] }, attachTo: document.body })
}

beforeEach(() => {
  localStorage.clear()
  answer = async () => json(fresh)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'innerWidth', { value: 1440, configurable: true })
  vi.stubGlobal('fetch', vi.fn(() => answer()))
})

afterEach(() => vi.unstubAllGlobals())

describe('PublicView', () => {
  it('страница из кеша видна до ответа сервера', async () => {
    localStorage.setItem(KEY, JSON.stringify(fresh))
    // Сервер не отвечает вовсе: всё, что на экране, взято из кеша.
    answer = () => new Promise<Response>(() => {})
    const w = await open('/public/links')
    expect(w.text()).toContain('NAS')
    expect(w.text()).not.toContain('Загрузка')
    w.unmount()
  })

  it('ответ сервера сохраняется в кеш без статусов сервисов', async () => {
    const w = await open('/public/links')
    await flushPromises()
    expect(w.text()).toContain('NAS')
    const cached = JSON.parse(localStorage.getItem(KEY) ?? 'null') as PublicPage
    expect(cached.page.slug).toBe('links')
    // Статус устаревает за секунды, поэтому в кеш не попадает.
    expect(cached.services[0]).not.toHaveProperty('status')
    w.unmount()
  })

  it('при недоступном сервере остаётся страница из кеша', async () => {
    localStorage.setItem(KEY, JSON.stringify(fresh))
    answer = async () => {
      throw new TypeError('network down')
    }
    const w = await open('/public/links')
    await flushPromises()
    expect(w.text()).toContain('NAS')
    expect(localStorage.getItem(KEY)).not.toBeNull()
    w.unmount()
  })

  it('закрытая страница исчезает и из кеша', async () => {
    localStorage.setItem(KEY, JSON.stringify(fresh))
    answer = async () => json({ code: 'not_found', message: 'не найдено' }, 404)
    const w = await open('/public/links')
    await flushPromises()
    expect(w.text()).not.toContain('NAS')
    expect(w.text()).toContain('Страница не найдена')
    expect(localStorage.getItem(KEY)).toBeNull()
    w.unmount()
  })

  it('?bare скрывает шапку', async () => {
    const withHead = await open('/public/links')
    await flushPromises()
    expect(withHead.find('.pub-head').exists()).toBe(true)
    withHead.unmount()

    const bare = await open('/public/links?bare')
    await flushPromises()
    expect(bare.find('.pub-head').exists()).toBe(false)
    expect(bare.text()).toContain('NAS')
    bare.unmount()
  })
})
