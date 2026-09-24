<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  GripVertical,
  MoveDiagonal2,
  Pencil,
  Trash2,
} from 'lucide-vue-next'
import type { Breakpoint, Widget } from '@/api'
import WidgetHost from '@/components/widgets/WidgetHost.vue'
import { t } from '@/i18n'
import { clampRect, moveItem, nudge, resolveLayout, type Direction, type Item } from '@/lib/grid'
import { layoutSources, minSize } from '@/lib/widgets'

const props = defineProps<{
  widgets: Widget[]
  bp: Breakpoint
  cols: number
  editing?: boolean
}>()

const emit = defineEmits<{
  layout: [items: Item[]]
  edit: [id: string]
  remove: [id: string]
}>()

const container = ref<HTMLElement | null>(null)
const selected = ref<string | null>(null)
const preview = ref<Item[] | null>(null)
const announce = ref('')

const resolved = computed(() => resolveLayout(layoutSources(props.widgets), props.bp, props.cols))
const items = computed(() => preview.value ?? resolved.value)
const byId = computed(() => new Map(items.value.map((i) => [i.id, i])))
const widgetMin = (id: string) => {
  const w = props.widgets.find((x) => x.id === id)
  return w ? minSize[w.type] : { w: 1, h: 1 }
}

const rows = computed(() => items.value.reduce((m, i) => Math.max(m, i.y + i.h), 0))

function cellStyle(id: string) {
  const r = byId.value.get(id)
  if (!r) return {}
  return { gridColumn: `${r.x + 1} / span ${r.w}`, gridRow: `${r.y + 1} / span ${r.h}` }
}

function commit(next: Item[], id: string) {
  emit('layout', next)
  const r = next.find((i) => i.id === id)
  if (r) announce.value = t('editor.widgetMoved', { x: r.x + 1, y: r.y + 1, w: r.w, h: r.h })
}

function move(id: string, dir: Direction) {
  const r = byId.value.get(id)
  if (!r) return
  commit(moveItem(items.value, id, nudge(r, dir), props.cols, widgetMin(id)), id)
}

function resize(id: string, dw: number, dh: number) {
  const r = byId.value.get(id)
  if (!r) return
  const target = clampRect({ ...r, w: r.w + dw, h: r.h + dh }, props.cols, widgetMin(id))
  if (target.x + target.w > props.cols) target.x = props.cols - target.w
  commit(moveItem(items.value, id, target, props.cols, widgetMin(id)), id)
}

function onKey(e: KeyboardEvent, id: string) {
  if (!props.editing || e.target !== e.currentTarget) return
  const dirs: Record<string, Direction> = { ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down' }
  const dir = dirs[e.key]
  if (!dir) return
  e.preventDefault()
  if (e.shiftKey) {
    const dw = dir === 'left' ? -1 : dir === 'right' ? 1 : 0
    const dh = dir === 'up' ? -1 : dir === 'down' ? 1 : 0
    resize(id, dw, dh)
  } else move(id, dir)
}

interface DragState {
  id: string
  mode: 'move' | 'resize'
  startX: number
  startY: number
  origin: Item
  base: Item[]
  cellW: number
  cellH: number
}

let drag: DragState | null = null

function metrics() {
  const el = container.value!
  const style = getComputedStyle(el)
  const gap = parseFloat(style.columnGap) || 0
  const rowGap = parseFloat(style.rowGap) || 0
  const rowH = parseFloat(style.gridAutoRows) || 76
  const colW = (el.clientWidth - gap * (props.cols - 1)) / props.cols
  return { cellW: colW + gap, cellH: rowH + rowGap }
}

function startDrag(e: PointerEvent, id: string, mode: DragState['mode']) {
  if (!props.editing || e.button !== 0) return
  const origin = byId.value.get(id)
  if (!origin || !container.value) return
  e.preventDefault()
  selected.value = id
  const { cellW, cellH } = metrics()
  drag = { id, mode, startX: e.clientX, startY: e.clientY, origin, base: items.value, cellW, cellH }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onDrag(e: PointerEvent) {
  if (!drag) return
  const dx = Math.round((e.clientX - drag.startX) / drag.cellW)
  const dy = Math.round((e.clientY - drag.startY) / drag.cellH)
  const o = drag.origin
  const target = drag.mode === 'move' ? { ...o, x: o.x + dx, y: o.y + dy } : { ...o, w: o.w + dx, h: o.h + dy }
  preview.value = moveItem(drag.base, drag.id, target, props.cols, widgetMin(drag.id))
}

function endDrag(e: PointerEvent) {
  if (!drag) return
  const id = drag.id
  ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
  drag = null
  if (preview.value) commit(preview.value, id)
  preview.value = null
}
</script>

<template>
  <div
    ref="container"
    class="grid"
    :class="{ editing }"
    :style="{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, '--rows': rows }"
  >
    <div
      v-for="w in widgets"
      :key="w.id"
      class="cell"
      :class="{ selected: editing && selected === w.id, dragging: preview && selected === w.id }"
      :style="cellStyle(w.id)"
      :tabindex="editing ? 0 : undefined"
      :aria-label="editing ? t(`widgets.types.${w.type}`) : undefined"
      @focusin="selected = w.id"
      @keydown="onKey($event, w.id)"
    >
      <WidgetHost :widget="w" />
      <div v-if="editing" class="tools" role="toolbar" :aria-label="t(`widgets.types.${w.type}`)">
        <button
          type="button"
          class="btn small icon grip"
          :aria-label="t('editor.drag')"
          :title="t('editor.drag')"
          @pointerdown="startDrag($event, w.id, 'move')"
          @pointermove="onDrag"
          @pointerup="endDrag"
          @pointercancel="endDrag"
        >
          <GripVertical :size="16" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon" :aria-label="t('editor.moveLeft')" @click="move(w.id, 'left')">
          <ArrowLeft :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon" :aria-label="t('editor.moveUp')" @click="move(w.id, 'up')">
          <ArrowUp :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon" :aria-label="t('editor.moveDown')" @click="move(w.id, 'down')">
          <ArrowDown :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon" :aria-label="t('editor.moveRight')" @click="move(w.id, 'right')">
          <ArrowRight :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small" :aria-label="t('editor.narrower')" @click="resize(w.id, -1, 0)">W−</button>
        <button type="button" class="btn small" :aria-label="t('editor.wider')" @click="resize(w.id, 1, 0)">W+</button>
        <button type="button" class="btn small" :aria-label="t('editor.shorter')" @click="resize(w.id, 0, -1)">H−</button>
        <button type="button" class="btn small" :aria-label="t('editor.taller')" @click="resize(w.id, 0, 1)">H+</button>
        <button type="button" class="btn small icon" :aria-label="t('editor.widgetSettings')" @click="emit('edit', w.id)">
          <Pencil :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon danger" :aria-label="t('editor.deleteWidget')" @click="emit('remove', w.id)">
          <Trash2 :size="14" aria-hidden="true" />
        </button>
      </div>
      <button
        v-if="editing"
        type="button"
        class="resize"
        :aria-label="t('editor.resize')"
        :title="t('editor.resize')"
        tabindex="-1"
        @pointerdown="startDrag($event, w.id, 'resize')"
        @pointermove="onDrag"
        @pointerup="endDrag"
        @pointercancel="endDrag"
      >
        <MoveDiagonal2 :size="14" aria-hidden="true" />
      </button>
    </div>
    <p class="sr-only" aria-live="polite">{{ announce }}</p>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  gap: var(--gap);
  grid-auto-rows: var(--row);
  min-width: 0;
}

.grid.editing {
  min-height: calc(var(--row) * 2);
  background-image: linear-gradient(to right, color-mix(in srgb, var(--border) 50%, transparent) 1px, transparent 1px);
  border-radius: var(--radius);
  outline: 1px dashed var(--border);
  outline-offset: 4px;
}

.cell {
  position: relative;
  min-width: 0;
  min-height: 0;
  transition: transform 0.12s;
}

.cell > :first-child {
  height: 100%;
}

.editing .cell {
  outline: 1px dashed transparent;
  border-radius: var(--radius);
}

.editing .cell:hover,
.editing .cell.selected {
  outline-color: var(--accent);
}

.cell.dragging {
  opacity: 0.85;
  z-index: 5;
}

.tools {
  position: absolute;
  top: -14px;
  right: 4px;
  z-index: 4;
  display: none;
  flex-wrap: wrap;
  gap: 2px;
  padding: 3px;
  max-width: calc(100% - 8px);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-pop);
}

.cell:hover .tools,
.cell.selected .tools,
.cell:focus-within .tools {
  display: flex;
}

.grip {
  cursor: grab;
  touch-action: none;
}

.resize {
  position: absolute;
  right: 2px;
  bottom: 2px;
  z-index: 3;
  width: 22px;
  height: 22px;
  display: grid;
  place-items: center;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  background: var(--surface);
  cursor: nwse-resize;
  touch-action: none;
  padding: 0;
}
</style>
