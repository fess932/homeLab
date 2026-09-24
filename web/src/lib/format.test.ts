import { describe, expect, it } from 'vitest'
import { formatAgo, formatBytes, formatDuration, formatValue, lastValue, thresholdColor } from './format'

// Intl в ru-RU использует неразрывные пробелы — нормализуем для сравнения.
const n = (s: string) => s.replace(/[  ]/g, ' ')

describe('formatValue', () => {
  it('renders null, NaN and Infinity as "нет данных", never as zero', () => {
    expect(formatValue(null)).toBe('нет данных')
    expect(formatValue(undefined, 'percent')).toBe('нет данных')
    expect(formatValue(NaN, 'bytes')).toBe('нет данных')
    expect(formatValue(Infinity)).toBe('нет данных')
    expect(formatValue(-Infinity, 'seconds')).toBe('нет данных')
  })

  it('keeps real zero as zero', () => {
    expect(formatValue(0, 'percent')).toBe('0 %')
    expect(formatValue(0, 'count')).toBe('0')
  })

  it('formats percent and fraction', () => {
    expect(n(formatValue(42.345, 'percent'))).toBe('42,3 %')
    expect(n(formatValue(0.5, 'percent_unit'))).toBe('50 %')
  })

  it('formats bytes with binary prefixes', () => {
    expect(formatBytes(512)).toBe('512 Б')
    expect(n(formatBytes(1536))).toBe('1,5 КиБ')
    expect(n(formatValue(3 * 1024 ** 3, 'bytes'))).toBe('3 ГиБ')
    expect(n(formatValue(2048, 'bytes_per_second'))).toBe('2 КиБ/с')
  })

  it('formats durations', () => {
    expect(n(formatValue(0.0123, 'seconds'))).toBe('12 мс')
    expect(n(formatValue(250, 'milliseconds'))).toBe('250 мс')
    expect(formatDuration(3 * 86400 + 5 * 3600)).toBe('3 д 5 ч')
    expect(formatDuration(3700)).toBe('1 ч 1 мин')
  })

  it('formats bool and celsius', () => {
    expect(formatValue(1, 'bool')).toBe('да')
    expect(formatValue(0, 'bool')).toBe('нет')
    expect(n(formatValue(21.55, 'celsius'))).toBe('21,6 °C')
  })

  it('respects explicit decimals', () => {
    expect(n(formatValue(3.14159, '', 3))).toBe('3,142')
  })
})

describe('formatAgo', () => {
  it('handles missing timestamps', () => {
    expect(formatAgo(null)).toBe('никогда')
    expect(formatAgo('')).toBe('никогда')
  })

  it('formats relative time from unix seconds and ISO', () => {
    const now = Date.UTC(2026, 0, 1, 12, 0, 0)
    expect(formatAgo(now / 1000 - 2, now)).toBe('только что')
    expect(formatAgo(new Date(now - 90_000).toISOString(), now)).toBe('1 мин 30 с назад')
  })
})

describe('thresholdColor', () => {
  const th = [
    { value: 90, color: 'crit' as const },
    { value: 0, color: 'ok' as const },
    { value: 75, color: 'warn' as const },
  ]

  it('picks the highest reached threshold regardless of order', () => {
    expect(thresholdColor(10, th)).toBe('ok')
    expect(thresholdColor(80, th)).toBe('warn')
    expect(thresholdColor(95, th)).toBe('crit')
  })

  it('supports lower thresholds and ignores window norms', () => {
    const humidity = [
      { value: 20, color: 'crit' as const, below: true },
      { value: 30, color: 'warn' as const, below: true },
      { value: 60, color: 'warn' as const },
      { value: 70, color: 'crit' as const },
    ]
    expect(thresholdColor(45, humidity)).toBeNull()
    expect(thresholdColor(25, humidity)).toBe('warn')
    expect(thresholdColor(15, humidity)).toBe('crit')
    expect(thresholdColor(75, humidity)).toBe('crit')
    expect(thresholdColor(20, [{ value: 15, color: 'warn', window: '24h' }])).toBeNull()
  })

  it('returns null without data or thresholds', () => {
    expect(thresholdColor(null, th)).toBeNull()
    expect(thresholdColor(50, [])).toBeNull()
  })
})

describe('lastValue', () => {
  it('returns last finite value and its measurement time', () => {
    expect(lastValue([1, 2, null, null], 1000, 15)).toEqual({ value: 2, time: 1015 })
    expect(lastValue([null, null], 1000, 15)).toEqual({ value: null, time: null })
  })
})

describe('autoDecimals', () => {
  it('сохраняет значащие цифры у малых значений, чтобы ось CPU не превращалась в нули', () => {
    expect(formatValue(0.0042, 'count')).toBe('0,0042')
    expect(formatValue(0.25, 'count')).toBe('0,25')
    expect(formatValue(12, 'count')).toBe('12')
    expect(formatValue(0, 'count')).toBe('0')
  })
})
