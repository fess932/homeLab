import { t } from '@/i18n'

export type Errors = Record<string, string>

export function isHttpUrl(s: string): boolean {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && !!u.hostname
  } catch {
    return false
  }
}

export function isHostPort(s: string): boolean {
  const m = /^(\[[0-9a-fA-F:.]+\]|[A-Za-z0-9.-]+):(\d{1,5})$/.exec(s.trim())
  if (!m) return false
  const port = Number(m[2])
  return port > 0 && port < 65536
}

export const slugPattern = /^[a-z0-9][a-z0-9-]{0,39}$/

const reservedLabels = new Set(['source_id', 'job', 'instance', 'service_id'])

export function isLabelKey(k: string): boolean {
  return /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(k) && !k.startsWith('__') && !reservedLabels.has(k)
}

export function intervalErrors(interval: number, timeout: number, prefix = ''): Errors {
  const e: Errors = {}
  if (!Number.isInteger(interval) || interval < 5 || interval > 3600)
    e[`${prefix}interval_s`] = t('validation.range', { min: 5, max: 3600 })
  if (!Number.isInteger(timeout) || timeout < 1) e[`${prefix}timeout_s`] = t('validation.range', { min: 1, max: interval - 1 })
  else if (timeout >= interval) e[`${prefix}timeout_s`] = t('validation.timeout')
  return e
}

export function parseTags(s: string): string[] {
  return [...new Set(s.split(',').map((x) => x.trim()).filter(Boolean))]
}
