import { ru } from './ru'

type Leaves<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string
    ? `${P}${K}`
    : T[K] extends readonly string[]
      ? never
      : Leaves<T[K], `${P}${K}.`>
}[keyof T & string]

export type MessageKey = Leaves<typeof ru>

export const messages = ru

export function t(key: MessageKey | (string & {}), params?: Record<string, string | number>): string {
  let node: unknown = messages
  for (const part of key.split('.')) {
    if (node && typeof node === 'object' && part in node) node = (node as Record<string, unknown>)[part]
    else return key
  }
  if (typeof node !== 'string') return key
  if (!params) return node
  return node.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m))
}

export function errorText(err: unknown): string {
  if (err && typeof err === 'object' && 'code' in err && 'message' in err) {
    const e = err as { code: string; message: string }
    const known = t(`errors.${e.code}`)
    return e.message && e.message !== known ? e.message : known
  }
  if (err instanceof TypeError) return t('errors.network')
  if (err instanceof Error) return err.message
  return t('app.unexpected')
}
