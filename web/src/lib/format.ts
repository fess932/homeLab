import type { Unit } from '@/api/types'
import { messages, t } from '@/i18n'

const nf = (decimals: number) =>
  new Intl.NumberFormat('ru-RU', { maximumFractionDigits: decimals, minimumFractionDigits: 0 })

export function isValue(v: number | null | undefined): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

export function autoDecimals(v: number, base: number): number {
  const a = Math.abs(v)
  if (a === 0 || a >= 1) return base
  return Math.min(6, Math.max(base, Math.ceil(-Math.log10(a)) + 1))
}

export function formatNumber(v: number, decimals = 2): string {
  return nf(decimals).format(v)
}

export function formatBytes(v: number, decimals = 1): string {
  const units = messages.units.bytes
  let i = 0
  let n = Math.abs(v)
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${v < 0 ? '-' : ''}${formatNumber(n, i === 0 ? 0 : decimals)} ${units[i]}`
}

export function formatDuration(seconds: number): string {
  const u = messages.units.duration
  const s = Math.abs(seconds)
  if (s < 1e-3) return `${formatNumber(seconds * 1e6, 0)} ${messages.units.us}`
  if (s < 1) return `${formatNumber(seconds * 1e3, s < 0.01 ? 2 : 0)} ${messages.units.ms}`
  if (s < 60) return `${formatNumber(seconds, s < 10 ? 2 : 1)} ${messages.units.s}`
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  const parts: string[] = []
  if (d) parts.push(`${d} ${u.d}`)
  if (h) parts.push(`${h} ${u.h}`)
  if (m && !d) parts.push(`${m} ${u.m}`)
  if (sec && !d && !h) parts.push(`${sec} ${u.s}`)
  return parts.join(' ')
}

export function formatValue(v: number | null | undefined, unit: Unit = '', decimals?: number): string {
  if (!isValue(v)) return messages.units.noData
  switch (unit) {
    case 'percent':
      return `${formatNumber(v, decimals ?? 1)} %`
    case 'percent_unit':
      return `${formatNumber(v * 100, decimals ?? 1)} %`
    case 'bytes':
      return formatBytes(v, decimals ?? 1)
    case 'bytes_per_second':
      return `${formatBytes(v, decimals ?? 1)}${messages.units.perSecond}`
    case 'seconds':
      return formatDuration(v)
    case 'milliseconds':
      return formatDuration(v / 1000)
    case 'per_second':
      return `${formatNumber(v, decimals ?? autoDecimals(v, 2))}${messages.units.perSecond}`
    case 'count':
      return formatNumber(v, decimals ?? autoDecimals(v, 0))
    case 'celsius':
      return `${formatNumber(v, decimals ?? 1)} ${messages.units.celsius}`
    case 'bool':
      return v !== 0 ? messages.units.boolTrue : messages.units.boolFalse
    case 'ppm':
      return `${formatNumber(v, decimals ?? 0)} ${messages.units.ppm}`
    case 'ugm3':
      return `${formatNumber(v, decimals ?? 0)} ${messages.units.ugm3}`
    case 'mgm3':
      return `${formatNumber(v, decimals ?? 3)} ${messages.units.mgm3}`
    default:
      return formatNumber(v, decimals ?? autoDecimals(v, 2))
  }
}

export function formatAxis(v: number, unit: Unit): string {
  if (unit === 'bool') return v ? '1' : '0'
  return formatValue(v, unit, unit === 'bytes' || unit === 'bytes_per_second' ? 0 : undefined)
}

export function formatTime(iso: string | number | null | undefined): string {
  if (iso === null || iso === undefined || iso === '') return t('time.never')
  const d = typeof iso === 'number' ? new Date(iso * 1000) : new Date(iso)
  if (Number.isNaN(d.getTime())) return t('time.never')
  return d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'medium' })
}

export function formatAgo(iso: string | number | null | undefined, now = Date.now()): string {
  if (iso === null || iso === undefined || iso === '') return t('time.never')
  const ms = typeof iso === 'number' ? iso * 1000 : new Date(iso).getTime()
  if (Number.isNaN(ms)) return t('time.never')
  const diff = Math.max(0, (now - ms) / 1000)
  if (diff < 5) return t('time.justNow')
  return t('time.ago', { v: formatDuration(Math.round(diff)) })
}

const severity = { ok: 0, warn: 1, crit: 2 } as const

/** Цвет текущего значения: самый строгий из достигнутых порогов (нормы за окно не учитываются). */
export function thresholdColor(
  v: number | null | undefined,
  thresholds: { value: number; color: 'ok' | 'warn' | 'crit'; below?: boolean; window?: string }[] | undefined,
): 'ok' | 'warn' | 'crit' | null {
  if (!isValue(v) || !thresholds?.length) return null
  let result: 'ok' | 'warn' | 'crit' | null = null
  for (const th of thresholds) {
    if (th.window) continue
    const hit = th.below ? v <= th.value : v >= th.value
    if (hit && (result === null || severity[th.color] > severity[result])) result = th.color
  }
  return result
}

export function lastValue(values: (number | null)[], start: number, step: number): { value: number | null; time: number | null } {
  for (let i = values.length - 1; i >= 0; i--) {
    const v = values[i]
    if (isValue(v)) return { value: v, time: start + i * step }
  }
  return { value: null, time: null }
}
