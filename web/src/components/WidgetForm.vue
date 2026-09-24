<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Group, MetricRef, RangeName, Widget, WidgetConfigMap } from '@/api'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import MetricPicker from '@/components/MetricPicker.vue'
import { t } from '@/i18n'
import { MARKDOWN_LIMIT } from '@/lib/markdown'
import { rangeNames } from '@/lib/time'
import { store } from '@/stores/app'

const props = defineProps<{ widget: Widget; groups: Group[] }>()
const emit = defineEmits<{ save: [w: Widget]; close: [] }>()

const draft = ref<Widget>(JSON.parse(JSON.stringify(props.widget)) as Widget)
const cfg = computed(() => draft.value.config as unknown as Record<string, string>)
const typed = <K extends keyof WidgetConfigMap>() => draft.value.config as WidgetConfigMap[K]

const zones = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
  } catch {
    return []
  }
})()

const linksCfg = computed(() => typed<'links'>())

const linkMetric = computed<MetricRef>({
  get: () => typed<'link'>().metric ?? { preset_id: '', vars: {} },
  set: (m) => (typed<'link'>().metric = m.preset_id ? m : null),
})
const metric = computed<MetricRef>({
  get: () => typed<'chart'>().metric,
  set: (m) => (typed<'chart'>().metric = m),
})

function toggleService(id: string, on: boolean) {
  const c = typed<'links'>()
  c.service_ids = on ? [...c.service_ids, id] : c.service_ids.filter((x) => x !== id)
}

function moveService(idx: number, delta: number) {
  const list = [...typed<'links'>().service_ids]
  const j = idx + delta
  if (j < 0 || j >= list.length) return
  ;[list[idx], list[j]] = [list[j]!, list[idx]!]
  typed<'links'>().service_ids = list
}

const serviceName = (id: string) => store.services.find((s) => s.id === id)?.name ?? id

function submit() {
  emit('save', draft.value)
}
</script>

<template>
  <ModalDialog :title="`${t('editor.widgetSettings')}: ${t(`widgets.types.${draft.type}`)}`" @close="emit('close')">
    <form id="widget-form" @submit.prevent="submit">
      <label class="field">
        <span>{{ t('editor.groupTitle') }}</span>
        <select v-model="draft.group_id" class="input">
          <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.title }}</option>
        </select>
      </label>

      <template v-if="draft.type === 'link' || draft.type === 'status'">
        <label class="field">
          <span>{{ t('widgets.service') }}</span>
          <select v-model="cfg.service_id" class="input" required>
            <option value="" disabled>—</option>
            <option v-for="s in store.services" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </label>
      </template>

      <template v-if="draft.type === 'link'">
        <label class="field check"><input v-model="cfg.show_status" type="checkbox" /> {{ t('widgets.showStatus') }}</label>
        <label class="field check"><input v-model="cfg.show_latency" type="checkbox" /> {{ t('widgets.showLatency') }}</label>
        <MetricPicker v-model="linkMetric" optional />
      </template>

      <template v-if="draft.type === 'links'">
        <label class="field">
          <span>{{ t('widgets.title') }}</span>
          <input v-model="cfg.title" class="input" maxlength="100" />
        </label>
        <fieldset class="field">
          <legend class="small">{{ t('widgets.services') }}</legend>
          <ol class="ordered">
            <li v-for="(id, i) in linksCfg.service_ids" :key="id">
              <span>{{ serviceName(id) }}</span>
              <button type="button" class="btn small" :aria-label="t('editor.moveUp')" @click="moveService(i, -1)">↑</button>
              <button type="button" class="btn small" :aria-label="t('editor.moveDown')" @click="moveService(i, 1)">↓</button>
            </li>
          </ol>
          <label v-for="s in store.services" :key="s.id" class="field check">
            <input
              type="checkbox"
              :checked="linksCfg.service_ids.includes(s.id)"
              @change="toggleService(s.id, ($event.target as HTMLInputElement).checked)"
            />
            {{ s.name }}
          </label>
        </fieldset>
      </template>

      <template v-if="draft.type === 'clock'">
        <label class="field">
          <span>{{ t('widgets.timezone') }}</span>
          <input v-model="cfg.timezone" class="input" list="tz-list" :placeholder="t('widgets.timezoneLocal')" />
          <datalist id="tz-list">
            <option v-for="z in zones" :key="z" :value="z" />
          </datalist>
        </label>
        <label class="field check"><input v-model="cfg.hour12" type="checkbox" /> {{ t('widgets.hour12') }}</label>
        <label class="field check"><input v-model="cfg.show_date" type="checkbox" /> {{ t('widgets.showDate') }}</label>
        <label class="field check"><input v-model="cfg.show_seconds" type="checkbox" /> {{ t('widgets.showSeconds') }}</label>
      </template>

      <template v-if="draft.type === 'note'">
        <label class="field">
          <span>{{ t('widgets.markdown') }}</span>
          <textarea v-model="cfg.markdown" class="input" rows="8" :maxlength="MARKDOWN_LIMIT" />
        </label>
      </template>

      <template v-if="draft.type === 'number' || draft.type === 'chart'">
        <label class="field">
          <span>{{ t('widgets.title') }}</span>
          <input v-model="cfg.title" class="input" maxlength="100" />
        </label>
        <MetricPicker v-model="metric" />
      </template>

      <label v-if="draft.type === 'number'" class="field">
        <span>{{ t('widgets.decimals') }}</span>
        <input v-model.number="cfg.decimals" type="number" min="0" max="6" class="input" />
      </label>

      <template v-if="draft.type === 'chart'">
        <label class="field">
          <span>{{ t('widgets.range') }}</span>
          <select v-model="cfg.range" class="input">
            <option v-for="r in rangeNames" :key="r" :value="r as RangeName">{{ t(`time.ranges.${r}`) }}</option>
          </select>
        </label>
        <label class="field check"><input v-model="cfg.stacked" type="checkbox" /> {{ t('widgets.stacked') }}</label>
      </template>

    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">{{ t('app.cancel') }}</button>
      <button type="submit" form="widget-form" class="btn primary">{{ t('app.save') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
fieldset.field {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
}

.ordered {
  margin: 0 0 8px;
  padding-left: 1.2em;
}

.ordered li {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-bottom: 2px;
}

.ordered li span {
  flex: 1;
}
</style>
