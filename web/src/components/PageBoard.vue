<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-vue-next'
import type { Breakpoint, PageInput, Widget, WidgetType } from '@/api'
import GridSection from '@/components/GridSection.vue'
import WidgetForm from '@/components/WidgetForm.vue'
import { t } from '@/i18n'
import { columnsFor, findSpot, resolveLayout, type Item } from '@/lib/grid'
import { defaultConfig, defaultSize, layoutSources, newId, widgetTypes } from '@/lib/widgets'

const props = defineProps<{ page: PageInput; bp: Breakpoint; editing?: boolean }>()

const cols = computed(() => columnsFor(props.bp, props.page.theme.columns))
const editingWidget = ref<Widget | null>(null)
const adding = ref<Record<string, WidgetType>>({})

function widgetsOf(groupId: string) {
  return props.page.widgets.filter((w) => w.group_id === groupId)
}

function applyLayout(items: Item[]) {
  const map = new Map(items.map((i) => [i.id, i]))
  for (const w of props.page.widgets) {
    const r = map.get(w.id)
    if (r) w.layout = { ...w.layout, [props.bp]: { x: r.x, y: r.y, w: r.w, h: r.h } }
  }
}

function addGroup() {
  props.page.groups.push({ id: newId(), title: t('home.defaultGroupTitle') })
}

function moveGroup(i: number, delta: number) {
  const j = i + delta
  const g = props.page.groups
  if (j < 0 || j >= g.length) return
  ;[g[i], g[j]] = [g[j]!, g[i]!]
}

function removeGroup(id: string) {
  const g = props.page.groups.find((x) => x.id === id)
  if (!g || !confirm(t('editor.deleteGroupConfirm', { name: g.title }))) return
  props.page.groups.splice(props.page.groups.indexOf(g), 1)
  props.page.widgets = props.page.widgets.filter((w) => w.group_id !== id)
}

function addWidget(groupId: string) {
  const type = adding.value[groupId] ?? 'link'
  const existing = resolveLayout(layoutSources(widgetsOf(groupId)), props.bp, cols.value)
  const spot = findSpot(existing, defaultSize[type], cols.value)
  const w: Widget = {
    id: newId(),
    group_id: groupId,
    type,
    public: false,
    config: defaultConfig(type),
    layout: { [props.bp]: spot },
  }
  editingWidget.value = w
}

function saveWidget(w: Widget) {
  const idx = props.page.widgets.findIndex((x) => x.id === w.id)
  if (idx >= 0) {
    const prev = props.page.widgets[idx]!
    if (prev.group_id !== w.group_id) w.layout = {}
    props.page.widgets[idx] = w
  } else props.page.widgets.push(w)
  editingWidget.value = null
}

function editWidget(id: string) {
  editingWidget.value = props.page.widgets.find((w) => w.id === id) ?? null
}

function removeWidget(id: string) {
  props.page.widgets = props.page.widgets.filter((w) => w.id !== id)
}
</script>

<template>
  <div class="board">
    <p v-if="!page.groups.length && !editing" class="muted">{{ t('home.emptyPage') }}</p>
    <section v-for="(g, i) in page.groups" :key="g.id" class="group" :aria-labelledby="`g-${g.id}`">
      <header class="group-head">
        <template v-if="editing">
          <label class="sr-only" :for="`g-${g.id}`">{{ t('editor.groupTitle') }}</label>
          <input :id="`g-${g.id}`" v-model="g.title" class="input group-title" maxlength="100" />
          <button type="button" class="btn small icon" :aria-label="t('editor.groupUp')" :disabled="i === 0" @click="moveGroup(i, -1)">
            <ArrowUp :size="14" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="btn small icon"
            :aria-label="t('editor.groupDown')"
            :disabled="i === page.groups.length - 1"
            @click="moveGroup(i, 1)"
          >
            <ArrowDown :size="14" aria-hidden="true" />
          </button>
          <button type="button" class="btn small icon danger" :aria-label="t('editor.deleteGroup')" @click="removeGroup(g.id)">
            <Trash2 :size="14" aria-hidden="true" />
          </button>
          <span class="spacer" />
          <label class="sr-only" :for="`add-${g.id}`">{{ t('editor.addWidget') }}</label>
          <select :id="`add-${g.id}`" v-model="adding[g.id]" class="input add-type">
            <option v-for="wt in widgetTypes" :key="wt" :value="wt">{{ t(`widgets.types.${wt}`) }}</option>
          </select>
          <button type="button" class="btn small" @click="addWidget(g.id)">
            <Plus :size="14" aria-hidden="true" /> {{ t('editor.addWidget') }}
          </button>
        </template>
        <h2 v-else :id="`g-${g.id}`">{{ g.title }}</h2>
      </header>
      <GridSection
        :widgets="widgetsOf(g.id)"
        :bp="bp"
        :cols="cols"
        :editing="editing"
        @layout="applyLayout"
        @edit="editWidget"
        @remove="removeWidget"
      />
      <p v-if="!widgetsOf(g.id).length && editing" class="muted small">{{ t('home.emptyGroup') }}</p>
    </section>
    <button v-if="editing" type="button" class="btn" @click="addGroup">
      <Plus :size="16" aria-hidden="true" /> {{ t('editor.addGroup') }}
    </button>
    <WidgetForm
      v-if="editingWidget"
      :widget="editingWidget"
      :groups="page.groups"
      @save="saveWidget"
      @close="editingWidget = null"
    />
  </div>
</template>

<style scoped>
.board {
  display: flex;
  flex-direction: column;
  gap: calc(var(--gap) * 2);
}

.group-head {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  margin-bottom: var(--gap);
}

.group-head h2 {
  margin: 0;
  font-size: 1.05rem;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.group-title {
  max-width: 280px;
  font-weight: 600;
}

.add-type {
  width: auto;
  min-width: 150px;
}
</style>
