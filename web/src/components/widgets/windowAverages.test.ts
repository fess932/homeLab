import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import WindowAverages from './WindowAverages.vue'

describe('WindowAverages', () => {
  it('показывает среднее за сутки и вердикт по норме', () => {
    const w = mount(WindowAverages, {
      props: {
        unit: 'ugm3',
        averages: [
          { window: '24h', value: 18.2, norm: { value: 15, color: 'warn', window: '24h' }, exceeded: true },
          { window: '7d', value: 9, norm: { value: 15, color: 'warn', window: '7d' }, exceeded: false },
        ],
      },
    })
    const items = w.findAll('li')
    expect(items[0]!.text()).toMatch(/^за сутки 18.* — выше нормы 15/)
    expect(items[0]!.classes()).toContain('warn')
    expect(items[1]!.text()).toMatch(/за неделю 9.* — в норме \(до 15/)
    expect(items[1]!.classes()).toContain('ok')
  })
})
