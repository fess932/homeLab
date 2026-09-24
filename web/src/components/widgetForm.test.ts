import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WidgetForm from './WidgetForm.vue'
import { api } from '@/api'
import type { Widget } from '@/api/types'
import { resetStore } from '@/stores/app'

const widget: Widget<'link'> = {
  id: 'new_1',
  group_id: 'grp_1',
  type: 'link',
  config: { service_id: '', show_status: true, show_latency: false, metric: null },
  layout: {},
}

let posts: { url: string; body: Record<string, unknown> }[] = []

beforeEach(() => {
  posts = []
  resetStore()
  api.http.setCsrf('csrf-x')
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } })
      if (init.method === 'POST') {
        const body = JSON.parse(init.body as string) as Record<string, unknown>
        posts.push({ url, body })
        return json({ ...body, id: url.endsWith('services') ? 'svc_new' : 'chk_new', revision: 1 }, 201)
      }
      return json([])
    }),
  )
})

afterEach(() => vi.unstubAllGlobals())

describe('WidgetForm: ссылка', () => {
  it('своя ссылка создаёт сервис и проверку раз в 5 минут', async () => {
    const w = mount(WidgetForm, { props: { widget, groups: [{ id: 'grp_1', title: 'Сервисы' }] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain('Своя ссылка')
    await w.find('input[inputmode="url"]').setValue('nas.lan:5000')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posts).toEqual([
      { url: '/api/v1/services', body: expect.objectContaining({ name: 'nas.lan:5000', url: 'http://nas.lan:5000', icon: 'favicon' }) },
      { url: '/api/v1/checks', body: expect.objectContaining({ service_id: 'svc_new', target: 'http://nas.lan:5000', interval_s: 300 }) },
    ])
    const saved = w.emitted('save')?.[0]?.[0] as Widget<'link'>
    expect(saved.config.service_id).toBe('svc_new')
    w.unmount()
  })

  it('неверный адрес и ссылка без проверки', async () => {
    const w = mount(WidgetForm, { props: { widget, groups: [] }, attachTo: document.body })
    await flushPromises()
    await w.find('input[inputmode="url"]').setValue('ftp://x')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posts).toEqual([])
    expect(w.emitted('save')).toBeUndefined()

    // Без проверки — простая ссылка в самом виджете, сервис не создаётся.
    await w.find('input[inputmode="url"]').setValue('https://router.lan')
    const ping = w.findAll('input[type="checkbox"]').find((c) => c.element.parentElement?.textContent?.includes('раз в 5 минут'))!
    await ping.setValue(false)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posts).toEqual([])
    const saved = w.emitted('save')?.[0]?.[0] as Widget<'link'>
    expect(saved.config).toMatchObject({ service_id: '', url: 'https://router.lan', title: '' })
    w.unmount()
  })
})
