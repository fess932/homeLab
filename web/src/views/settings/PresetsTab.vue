<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, ApiError, type QueryPreset, type QueryPresetInput, type Unit } from '@/api'
import MetricPanel from '@/components/MetricPanel.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import type { TimeWindow } from '@/lib/time'
import { ensureCatalog, loadPresets, store } from '@/stores/app'

const units: Unit[] = ['', 'percent', 'percent_unit', 'bytes', 'bytes_per_second', 'seconds', 'milliseconds', 'count', 'per_second', 'celsius', 'bool']

const editing = ref<{ id: string | null; revision: number; input: QueryPresetInput } | null>(null)
const previewExpr = ref('')
const previewWindow = ref<TimeWindow>({ range: '1h' })
const error = ref<unknown>(null)
const errors = ref<Record<string, string>>({})
const busy = ref(false)

const grouped = computed(() => {
  const out = new Map<string, QueryPreset[]>()
  for (const p of store.presets) out.set(p.category, [...(out.get(p.category) ?? []), p])
  return [...out.entries()]
})

onMounted(() => ensureCatalog().catch((e) => (error.value = e)))

function open(p?: QueryPreset) {
  errors.value = {}
  error.value = null
  editing.value = {
    id: p && !p.builtin ? p.id : null,
    revision: p?.revision ?? 0,
    input: {
      title: p ? (p.builtin ? `${p.title} (копия)` : p.title) : '',
      expression: p?.expression ?? '',
      unit: p?.unit ?? '',
      legend: p?.legend ?? '',
      thresholds: p?.thresholds.map((x) => ({ ...x })) ?? [],
    },
  }
  previewExpr.value = ''
}

function preview() {
  previewExpr.value = editing.value?.input.expression.trim() ?? ''
}

async function save() {
  if (!editing.value) return
  const { id, revision, input } = editing.value
  errors.value = {}
  if (!input.title.trim()) errors.value.title = t('validation.required')
  if (!input.expression.trim()) errors.value.expression = t('validation.required')
  if (Object.keys(errors.value).length) return
  busy.value = true
  error.value = null
  try {
    if (id) await api.presets.update(id, input, revision)
    else await api.presets.create(input)
    editing.value = null
    await loadPresets()
  } catch (e) {
    error.value = e
    if (e instanceof ApiError) errors.value = e.fieldErrors()
  } finally {
    busy.value = false
  }
}

async function remove(p: QueryPreset) {
  if (!confirm(t('app.confirmDelete', { name: p.title }))) return
  try {
    await api.presets.remove(p.id)
    await loadPresets()
  } catch (e) {
    error.value = e
  }
}
</script>

<template>
  <section class="card">
    <div class="toolbar">
      <h2>{{ t('monitoring.presets') }}</h2>
      <span class="spacer" />
      <button type="button" class="btn small primary" @click="open()"><Plus :size="14" aria-hidden="true" /> {{ t('monitoring.newPreset') }}</button>
    </div>
    <ApiErrorAlert v-if="!editing" :error="error" />

    <form v-if="editing" class="editor" @submit.prevent="save">
      <ApiErrorAlert :error="error" />
      <div class="row">
        <label class="field">
          <span>{{ t('widgets.title') }}</span>
          <input v-model="editing.input.title" class="input" maxlength="100" :aria-invalid="!!errors.title" />
          <span v-if="errors.title" class="error">{{ errors.title }}</span>
        </label>
        <label class="field">
          <span>{{ t('monitoring.unit') }}</span>
          <select v-model="editing.input.unit" class="input">
            <option v-for="u in units" :key="u" :value="u">{{ t(`monitoring.units.${u}`) }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('monitoring.legend') }}</span>
          <input v-model="editing.input.legend" class="input code" :placeholder="t('monitoring.legendHint')" />
        </label>
      </div>
      <label class="field">
        <span>{{ t('monitoring.expression') }}</span>
        <textarea v-model="editing.input.expression" class="input code" rows="4" spellcheck="false" :aria-invalid="!!errors.expression" />
        <span class="hint">$source_id, $service_id, $instance</span>
        <span v-if="errors.expression" class="error">{{ errors.expression }}</span>
      </label>
      <fieldset class="box">
        <legend class="small">{{ t('monitoring.thresholds') }}</legend>
        <div v-for="(th, i) in editing.input.thresholds" :key="i" class="row">
          <label class="field">
            <span class="sr-only">{{ t('monitoring.thresholds') }}</span>
            <input v-model.number="th.value" type="number" step="any" class="input" />
          </label>
          <label class="field">
            <span class="sr-only">{{ t('monitoring.thresholds') }}</span>
            <select v-model="th.color" class="input">
              <option value="ok">{{ t('monitoring.thresholdColors.ok') }}</option>
              <option value="warn">{{ t('monitoring.thresholdColors.warn') }}</option>
              <option value="crit">{{ t('monitoring.thresholdColors.crit') }}</option>
            </select>
          </label>
          <button type="button" class="btn icon danger" :aria-label="t('app.delete')" @click="editing.input.thresholds.splice(i, 1)">
            <Trash2 :size="14" aria-hidden="true" />
          </button>
        </div>
        <button type="button" class="btn small" @click="editing.input.thresholds.push({ value: 0, color: 'warn' })">
          {{ t('monitoring.addThreshold') }}
        </button>
      </fieldset>
      <div class="toolbar">
        <button type="button" class="btn" @click="preview">{{ t('monitoring.preview') }}</button>
        <span class="spacer" />
        <button type="button" class="btn" @click="editing = null">{{ t('app.cancel') }}</button>
        <button type="submit" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
      </div>
      <MetricPanel
        v-if="previewExpr"
        :title="t('monitoring.preview')"
        :query="previewExpr"
        :unit="editing.input.unit"
        :thresholds="editing.input.thresholds"
        :window="previewWindow"
        class="preview"
      />
    </form>

    <div v-for="[cat, list] in grouped" :key="cat" class="cat">
      <h3>{{ t(`monitoring.category.${cat}`) }}</h3>
      <ul class="presets">
        <li v-for="p in list" :key="p.id">
          <div class="info">
            <strong>{{ p.title }}</strong>
            <span v-if="p.builtin" class="tag">{{ t('monitoring.builtin') }}</span>
            <code class="small expr">{{ p.expression }}</code>
          </div>
          <button type="button" class="btn small" @click="open(p)">{{ p.builtin ? t('app.create') : t('app.edit') }}</button>
          <button v-if="!p.builtin" type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="remove(p)">
            <Trash2 :size="14" aria-hidden="true" />
          </button>
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.editor {
  border: 1px solid var(--accent);
  border-radius: var(--radius);
  padding: 12px;
  margin-bottom: 16px;
}

.box {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 12px;
}

.preview {
  margin-top: 12px;
}

.cat h3 {
  margin-top: 16px;
}

.presets {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.presets li {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}

.info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.expr {
  display: block;
  width: 100%;
  color: var(--text-muted);
  overflow-wrap: anywhere;
}
</style>
