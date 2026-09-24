import type { RangeName } from '@/api/types'

export const rangeNames: RangeName[] = ['1h', '6h', '24h', '7d', '30d']

export const rangeSeconds: Record<RangeName, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 86400,
  '30d': 30 * 86400,
}

export const RETENTION_SECONDS = rangeSeconds['30d']

export interface TimeWindow {
  range: RangeName | 'custom'
  start?: Date
  end?: Date
}

export function resolveWindow(w: TimeWindow, now = new Date()): { start: Date; end: Date } {
  if (w.range === 'custom' && w.start && w.end) return { start: w.start, end: w.end }
  const seconds = rangeSeconds[w.range === 'custom' ? '24h' : w.range]
  return { start: new Date(now.getTime() - seconds * 1000), end: now }
}

export function pointsForWidth(px: number): number {
  return Math.min(1000, Math.max(10, Math.round(px / 3)))
}

export function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function fromLocalInput(s: string): Date | null {
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? null : d
}
