import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PageSettingsForm from './PageSettingsForm.vue'
import { api } from '@/api'
import type { PageInput } from '@/api/types'
import { resetStore, store } from '@/stores/app'

const page: PageInput = {
  title: 'Дом',
  slug: 'home',
  order: 0,
  theme: { mode: 'system', accent: '#3b82f6', background_asset_id: null, density: 'comfortable', columns: 12 },
  groups: [],
  widgets: [],
}

let puts: unknown[] = []

beforeEach(() => {
  puts = []
  resetStore()
  store.pages = [
    { id: 'pg_home', title: 'Дом', slug: 'home', order: 0, revision: 1 },
    { id: 'pg_2', title: 'Дом 2', slug: 'home-2', order: 1, revision: 1 },
  ]
  store.settings = { title: 'HomeDeck', logo_asset_id: null, start_page_id: null, public_page_id: 'pg_2', revision: 3 }
  api.http.setCsrf('csrf-x')
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { 'Content-Type': 'application/json' } })
      if (url === '/api/v1/settings' && init.method === 'PUT') {
        const body = JSON.parse(init.body as string) as object
        puts.push(body)
        return json({ ...body, revision: 4 })
      }
      if (url === '/api/v1/settings') return json(store.settings)
      return json([])
    }),
  )
})

afterEach(() => vi.unstubAllGlobals())

describe('PageSettingsForm', () => {
  it('публикует страницу целиком и показывает ссылку', async () => {
    const w = mount(PageSettingsForm, { props: { page, pageId: 'pg_home' }, attachTo: document.body })
    await flushPromises()
    const box = w.find('input[type="checkbox"]')
    expect((box.element as HTMLInputElement).checked).toBe(false)
    expect(w.text()).not.toContain('Публичная ссылка')

    await box.setValue(true)
    expect(w.text()).toContain('Публичная ссылка')
    expect(w.text()).toContain('Сейчас публичная «Дом 2»')
    expect((w.find('.link-row input').element as HTMLInputElement).value).toBe(`${location.origin}/public`)

    await w.find('form').trigger('submit')
    await flushPromises()
    expect(puts).toEqual([expect.objectContaining({ public_page_id: 'pg_home' })])
    expect(store.settings?.public_page_id).toBe('pg_home')
    expect(w.emitted('save')).toHaveLength(1)
    w.unmount()
  })

  it('без изменения отметки настройки не трогает', async () => {
    const w = mount(PageSettingsForm, { props: { page, pageId: 'pg_2' }, attachTo: document.body })
    await flushPromises()
    expect((w.find('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(puts).toEqual([])
    expect(w.emitted('save')).toHaveLength(1)
    w.unmount()
  })
})
