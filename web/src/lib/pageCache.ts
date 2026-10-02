import type { PublicPage } from '@/api/types'

const key = (slug: string) => `homedeck:public:${slug}`

export function readCachedPage(slug: string): PublicPage | null {
  try {
    const raw = localStorage.getItem(key(slug))
    return raw ? (JSON.parse(raw) as PublicPage) : null
  } catch {
    return null
  }
}

export function writeCachedPage(slug: string, data: PublicPage) {
  const services = data.services.map(({ status: _, ...s }) => s)
  try {
    localStorage.setItem(key(slug), JSON.stringify({ ...data, services }))
  } catch {
    return
  }
}

export function dropCachedPage(slug: string) {
  try {
    localStorage.removeItem(key(slug))
  } catch {
    return
  }
}
