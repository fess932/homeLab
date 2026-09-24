import { describe, expect, it } from 'vitest'
import {
  breakpointFor,
  clampRect,
  collides,
  columnsFor,
  compact,
  findSpot,
  moveItem,
  nudge,
  resolveLayout,
  type Item,
  type LayoutSource,
} from './grid'

const noOverlap = (items: Item[]) => {
  for (let i = 0; i < items.length; i++)
    for (let j = i + 1; j < items.length; j++) expect(collides(items[i]!, items[j]!), `${items[i]!.id} vs ${items[j]!.id}`).toBe(false)
}

describe('breakpoints', () => {
  it('maps width to breakpoint', () => {
    expect(breakpointFor(1440)).toBe('lg')
    expect(breakpointFor(800)).toBe('md')
    expect(breakpointFor(360)).toBe('sm')
  })

  it('derives columns: lg from theme, md ≤ 6, sm = 1', () => {
    expect(columnsFor('lg', 12)).toBe(12)
    expect(columnsFor('md', 12)).toBe(6)
    expect(columnsFor('md', 4)).toBe(4)
    expect(columnsFor('sm', 12)).toBe(1)
  })
})

describe('clampRect', () => {
  it('keeps rect inside columns and above minimum size', () => {
    expect(clampRect({ x: 10, y: -2, w: 5, h: 0 }, 12, { w: 2, h: 1 })).toEqual({ x: 7, y: 0, w: 5, h: 1 })
    expect(clampRect({ x: 0, y: 0, w: 1, h: 1 }, 12, { w: 3, h: 2 })).toEqual({ x: 0, y: 0, w: 3, h: 2 })
  })

  it('never exceeds column count even if minimum is larger', () => {
    // на телефоне одна колонка — минимальная ширина виджета схлопывается до 1
    expect(clampRect({ x: 3, y: 0, w: 6, h: 2 }, 1, { w: 3, h: 2 })).toEqual({ x: 0, y: 0, w: 1, h: 2 })
  })
})

describe('compact', () => {
  it('moves items up into free space and preserves input order', () => {
    const out = compact([
      { id: 'a', x: 0, y: 5, w: 2, h: 1 },
      { id: 'b', x: 0, y: 9, w: 2, h: 2 },
    ])
    expect(out.map((i) => i.id)).toEqual(['a', 'b'])
    expect(out[0]).toMatchObject({ y: 0 })
    expect(out[1]).toMatchObject({ y: 1 })
  })

  it('resolves overlaps by pushing down', () => {
    const out = compact([
      { id: 'a', x: 0, y: 0, w: 4, h: 2 },
      { id: 'b', x: 2, y: 1, w: 4, h: 1 },
    ])
    noOverlap(out)
    expect(out[1]).toMatchObject({ y: 2 })
  })
})

describe('moveItem', () => {
  const base: Item[] = [
    { id: 'a', x: 0, y: 0, w: 3, h: 1 },
    { id: 'b', x: 3, y: 0, w: 3, h: 1 },
    { id: 'c', x: 0, y: 1, w: 6, h: 2 },
  ]

  it('pushes colliding items down and keeps layout free of overlaps', () => {
    const out = moveItem(base, 'a', { x: 3, y: 0, w: 3, h: 1 }, 12)
    noOverlap(out)
    expect(out.find((i) => i.id === 'a')).toMatchObject({ x: 3, y: 0 })
    expect(out.find((i) => i.id === 'b')!.y).toBeGreaterThan(0)
  })

  it('clamps target to grid bounds and minimum size', () => {
    const out = moveItem(base, 'a', { x: 20, y: 0, w: 1, h: 1 }, 12, { w: 2, h: 1 })
    expect(out.find((i) => i.id === 'a')).toMatchObject({ x: 10, w: 2 })
  })

  it('swaps vertically when nudged down past the item below', () => {
    const items: Item[] = [
      { id: 'top', x: 0, y: 0, w: 4, h: 1 },
      { id: 'bottom', x: 0, y: 1, w: 4, h: 1 },
    ]
    const out = moveItem(items, 'top', nudge(items[0]!, 'down'), 4)
    expect(out.find((i) => i.id === 'bottom')!.y).toBe(0)
    expect(out.find((i) => i.id === 'top')!.y).toBe(1)
  })

  it('swaps vertically when nudged up', () => {
    const items: Item[] = [
      { id: 'top', x: 0, y: 0, w: 4, h: 2 },
      { id: 'bottom', x: 0, y: 2, w: 4, h: 1 },
    ]
    const out = moveItem(items, 'bottom', nudge(items[1]!, 'up'), 4)
    expect(out.find((i) => i.id === 'bottom')!.y).toBe(0)
    expect(out.find((i) => i.id === 'top')!.y).toBe(1)
  })

  it('returns input for unknown id', () => {
    expect(moveItem(base, 'zzz', { x: 0, y: 0, w: 1, h: 1 }, 12)).toBe(base)
  })
})

describe('findSpot', () => {
  it('finds first free position scanning rows left to right', () => {
    const spot = findSpot([{ x: 0, y: 0, w: 6, h: 1 }], { w: 6, h: 1 }, 12)
    expect(spot).toEqual({ x: 6, y: 0, w: 6, h: 1 })
    const next = findSpot([{ x: 0, y: 0, w: 12, h: 2 }], { w: 3, h: 1 }, 12)
    expect(next).toEqual({ x: 0, y: 2, w: 3, h: 1 })
  })
})

describe('resolveLayout', () => {
  const src = (id: string, layout: LayoutSource['layout']): LayoutSource => ({
    id,
    layout,
    min: { w: 2, h: 1 },
    size: { w: 3, h: 1 },
  })

  it('uses stored breakpoint layout when present', () => {
    const out = resolveLayout([src('a', { lg: { x: 4, y: 0, w: 4, h: 2 } })], 'lg', 12)
    expect(out[0]).toEqual({ id: 'a', x: 4, y: 0, w: 4, h: 2 })
  })

  it('auto-places items without layout next to existing ones', () => {
    const out = resolveLayout([src('a', { lg: { x: 0, y: 0, w: 6, h: 1 } }), src('b', {})], 'lg', 12)
    expect(out[1]).toEqual({ id: 'b', x: 6, y: 0, w: 3, h: 1 })
  })

  it('derives phone layout from larger breakpoint as a single column in reading order', () => {
    const out = resolveLayout(
      [
        src('right', { lg: { x: 6, y: 0, w: 6, h: 2 } }),
        src('left', { lg: { x: 0, y: 0, w: 6, h: 1 } }),
        src('below', { lg: { x: 0, y: 3, w: 12, h: 1 } }),
      ],
      'sm',
      1,
    )
    const byId = Object.fromEntries(out.map((i) => [i.id, i]))
    expect(out.every((i) => i.x === 0 && i.w === 1)).toBe(true)
    expect(byId.left!.y).toBeLessThan(byId.right!.y)
    expect(byId.right!.y).toBeLessThan(byId.below!.y)
    // высота берётся из большего breakpoint
    expect(byId.right!.h).toBe(2)
    noOverlap(out)
  })

  it('clamps stored layout when theme columns shrink', () => {
    const out = resolveLayout([src('a', { lg: { x: 8, y: 0, w: 4, h: 1 } })], 'lg', 6)
    expect(out[0]).toMatchObject({ x: 2, w: 4 })
  })
})
