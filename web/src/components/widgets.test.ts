import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import ErrorBoundary from './ui/ErrorBoundary.vue'
import StatusBadge from './ui/StatusBadge.vue'
import WidgetNote from './widgets/WidgetNote.vue'
import type { Widget } from '@/api/types'

describe('ErrorBoundary', () => {
  it('isolates a failing widget and keeps siblings rendered', async () => {
    // ошибка рендера одного виджета не должна ломать страницу
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const Broken = defineComponent({
      setup() {
        throw new Error('kaput')
      },
      render: () => null,
    })
    const Page = defineComponent({
      render: () =>
        h('div', [h(ErrorBoundary, () => h(Broken)), h(ErrorBoundary, () => h('p', { class: 'ok' }, 'fine'))]),
    })
    const wrapper = mount(Page)
    // состояние ошибки выставляется в onErrorCaptured и применяется на следующем тике
    await nextTick()
    expect(wrapper.find('[role="alert"]').text()).toContain('Виджет не удалось отобразить')
    expect(wrapper.find('[role="alert"]').text()).toContain('kaput')
    expect(wrapper.find('.ok').text()).toBe('fine')
    spy.mockRestore()
  })
})

describe('StatusBadge', () => {
  it('renders text label with pending counter', () => {
    const w = mount(StatusBadge, { props: { status: { state: 'up', pending: 'down', streak: 1 } } })
    expect(w.text()).toBe('доступен (1 из 3 ошибок)')
    expect(w.classes()).toContain('ok')
  })

  it('defaults to unknown without status', () => {
    const w = mount(StatusBadge)
    expect(w.text()).toBe('нет данных')
    expect(w.classes()).toContain('muted')
  })
})

describe('WidgetNote', () => {
  const note = (markdown: string): Widget<'note'> => ({
    id: 'w1',
    group_id: 'g1',
    type: 'note',
    config: { markdown },
    layout: {},
  })

  it('renders markdown but escapes raw HTML', () => {
    const w = mount(WidgetNote, { props: { widget: note('**bold** <script>alert(1)</script><img src=x onerror=alert(1)>') } })
    expect(w.find('strong').text()).toBe('bold')
    expect(w.find('script').exists()).toBe(false)
    expect(w.find('img').exists()).toBe(false)
    expect(w.html()).toContain('&lt;script&gt;')
  })

  it('opens links in new tab without opener and drops javascript: urls', () => {
    const w = mount(WidgetNote, { props: { widget: note('[ok](https://example.org) [bad](javascript:alert(1))') } })
    const links = w.findAll('a')
    expect(links).toHaveLength(1)
    expect(links[0]!.attributes('rel')).toBe('noopener noreferrer')
    expect(links[0]!.attributes('target')).toBe('_blank')
  })

  it('does not render markdown images', () => {
    const w = mount(WidgetNote, { props: { widget: note('![x](http://tracker.example/p.png)') } })
    expect(w.find('img').exists()).toBe(false)
  })
})
