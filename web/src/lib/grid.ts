import type { Breakpoint, Rect } from '@/api/types'

export interface Item extends Rect {
  id: string
}

export interface Size {
  w: number
  h: number
}

export const breakpoints: Breakpoint[] = ['lg', 'md', 'sm']

export const breakpointMinWidth: Record<Breakpoint, number> = { lg: 1024, md: 640, sm: 0 }

export function breakpointFor(width: number): Breakpoint {
  if (width >= breakpointMinWidth.lg) return 'lg'
  if (width >= breakpointMinWidth.md) return 'md'
  return 'sm'
}

export function columnsFor(bp: Breakpoint, lgColumns: number): number {
  if (bp === 'lg') return lgColumns
  if (bp === 'md') return Math.min(6, lgColumns)
  return 1
}

export function collides(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
}

export function clampRect(r: Rect, cols: number, min: Size = { w: 1, h: 1 }): Rect {
  const minW = Math.min(Math.max(1, min.w), cols)
  const w = Math.min(Math.max(Math.round(r.w), minW), cols)
  const h = Math.max(Math.round(r.h), Math.max(1, min.h))
  const x = Math.min(Math.max(0, Math.round(r.x)), cols - w)
  const y = Math.max(0, Math.round(r.y))
  return { x, y, w, h }
}

function byPosition(a: Item, b: Item): number {
  return a.y - b.y || a.x - b.x
}

export function compact(items: Item[]): Item[] {
  const placed: Item[] = []
  for (const it of [...items].sort(byPosition)) {
    const next = { ...it }
    while (next.y > 0 && !placed.some((p) => collides({ ...next, y: next.y - 1 }, p))) next.y--
    while (placed.some((p) => collides(next, p))) next.y++
    placed.push(next)
  }
  return order(items, placed)
}

function order(original: Item[], result: Item[]): Item[] {
  const map = new Map(result.map((r) => [r.id, r]))
  return original.map((o) => map.get(o.id)!)
}

export function moveItem(items: Item[], id: string, target: Rect, cols: number, min?: Size): Item[] {
  const current = items.find((i) => i.id === id)
  if (!current) return items
  const moved: Item = { id, ...clampRect(target, cols, min) }
  const fixed = new Map<string, Item>([[id, moved]])
  const rest = items.filter((i) => i.id !== id).map((i) => ({ ...i }))
  rest.sort(byPosition)
  if (moved.y > current.y) {
    for (const it of rest) {
      if (!collides(it, moved)) continue
      const above = { ...it, y: moved.y - it.h }
      if (above.y >= 0 && !rest.some((o) => o !== it && collides(above, o))) it.y = above.y
    }
  }
  for (const it of rest) {
    let guard = 0
    while ([...fixed.values()].some((f) => collides(it, f)) && guard++ < 10_000) {
      const blocker = [...fixed.values()].find((f) => collides(it, f))!
      it.y = blocker.y + blocker.h
    }
    fixed.set(it.id, it)
  }
  return compact(order(items, [...fixed.values()]))
}

export function findSpot(items: Rect[], size: Size, cols: number): Rect {
  const w = Math.min(size.w, cols)
  for (let y = 0; ; y++) {
    for (let x = 0; x + w <= cols; x++) {
      const r = { x, y, w, h: size.h }
      if (!items.some((i) => collides(r, i))) return r
    }
  }
}

export function bottom(items: Rect[]): number {
  return items.reduce((m, i) => Math.max(m, i.y + i.h), 0)
}

export interface LayoutSource {
  id: string
  layout: Partial<Record<Breakpoint, Rect>>
  min: Size
  size: Size
}

export function resolveLayout(sources: LayoutSource[], bp: Breakpoint, cols: number): Item[] {
  const own: Item[] = []
  const missing: LayoutSource[] = []
  for (const s of sources) {
    const r = s.layout[bp]
    if (r) own.push({ id: s.id, ...clampRect(r, cols, s.min) })
    else missing.push(s)
  }
  const resolved = compact(own)
  const fromLarger = (s: LayoutSource): Rect | undefined => {
    const idx = breakpoints.indexOf(bp)
    for (let i = idx - 1; i >= 0; i--) {
      const r = s.layout[breakpoints[i]!]
      if (r) return r
    }
    return undefined
  }
  const ordered = [...missing].sort((a, b) => {
    const ra = fromLarger(a)
    const rb = fromLarger(b)
    if (ra && rb) return ra.y - rb.y || ra.x - rb.x
    if (ra) return -1
    if (rb) return 1
    return 0
  })
  for (const s of ordered) {
    const ref = fromLarger(s)
    const size = ref ? { w: Math.min(ref.w, cols), h: ref.h } : s.size
    const clamped = clampRect({ x: 0, y: 0, ...size }, cols, s.min)
    const spot = cols === 1 ? { x: 0, y: bottom(resolved), w: 1, h: clamped.h } : findSpot(resolved, clamped, cols)
    resolved.push({ id: s.id, ...spot })
  }
  return order(
    sources.map((s) => ({ id: s.id, x: 0, y: 0, w: 1, h: 1 })),
    resolved,
  )
}

export type Direction = 'left' | 'right' | 'up' | 'down'

export function nudge(r: Rect, dir: Direction): Rect {
  switch (dir) {
    case 'left':
      return { ...r, x: r.x - 1 }
    case 'right':
      return { ...r, x: r.x + 1 }
    case 'up':
      return { ...r, y: Math.max(0, r.y - 1) }
    case 'down':
      return { ...r, y: r.y + r.h }
  }
}
