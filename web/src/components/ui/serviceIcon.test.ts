import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ServiceIcon from './ServiceIcon.vue'

describe('ServiceIcon', () => {
  it('загруженная иконка — картинка с адреса панели, «favicon» до сохранения — глобус', () => {
    const asset = mount(ServiceIcon, { props: { icon: 'asset:ast_abcdefghijkmnp' } })
    expect(asset.find('img').attributes('src')).toBe('/assets/ast_abcdefghijkmnp')
    const pending = mount(ServiceIcon, { props: { icon: 'favicon' } })
    expect(pending.find('img').exists()).toBe(false)
    expect(pending.find('svg').exists()).toBe(true)
  })
})
