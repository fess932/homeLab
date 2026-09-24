<script setup lang="ts">
import { computed, useId } from 'vue'
import type { MetricRef } from '@/api'
import { t } from '@/i18n'
import { store } from '@/stores/app'

const model = defineModel<MetricRef>({ required: true })
defineProps<{ optional?: boolean }>()
const id = useId()

const groups = computed(() => {
  const out = new Map<string, typeof store.presets>()
  for (const p of store.presets) {
    const list = out.get(p.category) ?? []
    list.push(p)
    out.set(p.category, list)
  }
  return [...out.entries()]
})
const preset = computed(() => store.presets.find((p) => p.id === model.value.preset_id))
const userSources = computed(() => store.sources)

function setPreset(pid: string) {
  const p = store.presets.find((x) => x.id === pid)
  const vars: Record<string, string> = {}
  for (const v of p?.vars ?? []) vars[v] = model.value.vars[v] ?? ''
  model.value = { preset_id: pid, vars }
}

function setVar(name: string, value: string) {
  model.value = { ...model.value, vars: { ...model.value.vars, [name]: value } }
}
</script>

<template>
  <div class="metric-picker">
    <label class="field" :for="`${id}-preset`">
      <span>{{ t('widgets.preset') }}</span>
      <select :id="`${id}-preset`" class="input" :value="model.preset_id" @change="setPreset(($event.target as HTMLSelectElement).value)">
        <option value="">{{ optional ? t('widgets.noMetric') : '—' }}</option>
        <optgroup v-for="[cat, list] in groups" :key="cat" :label="t(`monitoring.category.${cat}`)">
          <option v-for="p in list" :key="p.id" :value="p.id">{{ p.title }}</option>
        </optgroup>
      </select>
    </label>
    <fieldset v-if="preset?.vars.length" class="vars">
      <legend class="small">{{ t('widgets.vars') }}</legend>
      <label v-for="v in preset.vars" :key="v" class="field">
        <span>{{ t(`widgets.var.${v}`) }}</span>
        <select
          v-if="v === 'source_id'"
          class="input"
          :value="model.vars[v] ?? ''"
          required
          @change="setVar(v, ($event.target as HTMLSelectElement).value)"
        >
          <option value="" disabled>—</option>
          <option v-for="s in userSources" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <select
          v-else-if="v === 'service_id'"
          class="input"
          :value="model.vars[v] ?? ''"
          required
          @change="setVar(v, ($event.target as HTMLSelectElement).value)"
        >
          <option value="" disabled>—</option>
          <option v-for="s in store.services" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
        <select
          v-else-if="v === 'device_id'"
          class="input"
          :value="model.vars[v] ?? ''"
          required
          @change="setVar(v, ($event.target as HTMLSelectElement).value)"
        >
          <option value="" disabled>—</option>
          <option v-for="d in store.devices" :key="d.id" :value="d.id">{{ d.name }}</option>
        </select>
        <input v-else class="input" :value="model.vars[v] ?? ''" @input="setVar(v, ($event.target as HTMLInputElement).value)" />
      </label>
    </fieldset>
  </div>
</template>

<style scoped>
.vars {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px 0;
  margin: 0 0 12px;
}
</style>
