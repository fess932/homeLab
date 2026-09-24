import { describe, expect, it } from 'vitest'
import type { ProbeState } from '@/api/types'
import { pendingLabel, probeLabel, probeTone, sourceLabel, statusText } from './status'

describe('probe states', () => {
  it('has a text label for every state', () => {
    const expected: Record<ProbeState, string> = {
      unknown: 'нет данных',
      up: 'доступен',
      down: 'недоступен',
      stale: 'устарело',
      disabled: 'выключено',
    }
    for (const [state, label] of Object.entries(expected)) expect(probeLabel(state as ProbeState)).toBe(label)
  })

  it('maps states to tones so status is not conveyed by color alone', () => {
    expect(probeTone.up).toBe('ok')
    expect(probeTone.down).toBe('bad')
    expect(probeTone.stale).toBe('warn')
    expect(probeTone.unknown).toBe('muted')
  })

  it('labels source states', () => {
    expect(sourceLabel('down')).toBe('ошибка')
    expect(sourceLabel('pending')).toBe('ожидание')
  })
})

describe('pending counter', () => {
  it('shows progress toward down threshold while keeping previous state', () => {
    const s = { state: 'up' as const, pending: 'down' as const, streak: 2 }
    expect(pendingLabel(s)).toBe('2 из 3 ошибок')
    expect(statusText(s)).toBe('доступен (2 из 3 ошибок)')
  })

  it('shows progress toward up threshold', () => {
    expect(pendingLabel({ state: 'down', pending: 'up', streak: 1 })).toBe('1 из 2 успехов')
  })

  it('is empty without pending transition', () => {
    expect(pendingLabel({ state: 'up' })).toBe('')
    expect(statusText(undefined)).toBe('нет данных')
  })
})
