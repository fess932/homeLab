import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ServiceIcon from './ServiceIcon.vue'

describe('ServiceIcon: иконка сайта', () => {
  it('перебирает адреса иконки и падает на глобус', async () => {
    const w = mount(ServiceIcon, { props: { icon: 'favicon', url: 'http://nas.lan:5000/ui/' } })
    expect(w.find('img').attributes('src')).toBe('http://nas.lan:5000/favicon.ico')
    await w.find('img').trigger('error')
    expect(w.find('img').attributes('src')).toBe('http://nas.lan:5000/apple-touch-icon.png')
    await w.find('img').trigger('error')
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('svg').exists()).toBe(true)
  })
})
