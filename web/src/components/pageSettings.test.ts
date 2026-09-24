import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PageSettingsForm from './PageSettingsForm.vue'
import type { PageInput } from '@/api/types'

const page: PageInput = {
  title: 'Дом',
  slug: 'home',
  order: 0,
  theme: { mode: 'system', accent: '#3b82f6', background_asset_id: null, density: 'comfortable', columns: 12 },
  groups: [],
  widgets: [],
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('[]', { headers: { 'Content-Type': 'application/json' } })))
})

afterEach(() => vi.unstubAllGlobals())

describe('PageSettingsForm', () => {
  it('отметка «Публичная» показывает ссылку по адресу страницы и сохраняется со страницей', async () => {
    const w = mount(PageSettingsForm, { props: { page }, attachTo: document.body })
    await flushPromises()
    expect(w.find('.link-row').exists()).toBe(false)

    await w.find('input[type="checkbox"]').setValue(true)
    const link = () => (w.find('.link-row input').element as HTMLInputElement).value
    expect(link()).toBe(`${location.origin}/public/home`)
    await w.find('input[aria-describedby="slug-hint"]').setValue('dacha')
    expect(link()).toBe(`${location.origin}/public/dacha`)

    await w.find('form').trigger('submit')
    expect(w.emitted('save')?.[0]?.[0]).toMatchObject({ slug: 'dacha', public: true })
    w.unmount()
  })

  it('публичная страница открывается с отмеченной галочкой', async () => {
    const w = mount(PageSettingsForm, { props: { page: { ...page, public: true } }, attachTo: document.body })
    await flushPromises()
    expect((w.find('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    w.unmount()
  })
})
