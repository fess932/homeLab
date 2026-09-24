import type { CheckStatus, ProbeState, SourceState } from '@/api/types'
import { t } from '@/i18n'

export const DOWN_THRESHOLD = 3
export const UP_THRESHOLD = 2

export type Tone = 'ok' | 'bad' | 'warn' | 'muted'

export const probeTone: Record<ProbeState, Tone> = {
  up: 'ok',
  down: 'bad',
  stale: 'warn',
  unknown: 'muted',
  disabled: 'muted',
}

export const sourceTone: Record<SourceState, Tone> = {
  up: 'ok',
  down: 'bad',
  pending: 'muted',
  disabled: 'muted',
}

export function probeLabel(state: ProbeState): string {
  return t(`status.states.${state}`)
}

export function sourceLabel(state: SourceState): string {
  return t(`status.source.${state}`)
}

export function pendingLabel(status: CheckStatus | undefined): string {
  if (!status?.pending || !status.streak) return ''
  if (status.pending === 'down') return t('status.pendingDown', { n: Math.min(status.streak, DOWN_THRESHOLD) })
  return t('status.pendingUp', { n: Math.min(status.streak, UP_THRESHOLD) })
}

export function statusText(status: CheckStatus | undefined): string {
  const state = status?.state ?? 'unknown'
  const pending = pendingLabel(status)
  return pending ? `${probeLabel(state)} (${pending})` : probeLabel(state)
}
