<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import type { Widget } from '@/api'
import { errorText, t } from '@/i18n'
import { formatAgo, formatTime, formatValue, thresholdColor } from '@/lib/format'
import { useWidgetContext, useWidgetData } from './context'
import WindowAverages from './WindowAverages.vue'

const props = defineProps<{ widget: Widget<'number'> }>()
const ctx = useWidgetContext()
const { data, error } = useWidgetData(toRef(props, 'widget'), ref({}), computed(() => !!props.widget.config.metric.preset_id))
const preset = computed(() => ctx.preset(props.widget.config.metric.preset_id))
const unit = computed(() => data.value?.instant?.unit ?? preset.value?.unit ?? '')
const sample = computed(() => data.value?.instant?.samples[0] ?? null)
const title = computed(() => props.widget.config.title || preset.value?.title || '')
const STALE_S = 300
const stale = computed(() => !!sample.value?.time && Date.now() / 1000 - sample.value.time > STALE_S)
const tone = computed(() => thresholdColor(sample.value?.value, data.value?.thresholds ?? preset.value?.thresholds))
</script>

<template>
  <div class="card w-number">
    <div class="top small">
      <span class="title">{{ title }}</span>
      <span v-if="!error && stale && sample" class="ago" :title="formatTime(sample.time)">
        {{ t('widgets.lastValueAt', { time: formatAgo(sample.time) }) }}
      </span>
    </div>
    <div v-if="error" class="small err" role="alert">{{ errorText(error) }}</div>
    <div
      v-else
      class="value"
      :class="[tone, { empty: !sample }]"
      :title="sample ? t('widgets.lastValueAt', { time: formatTime(sample.time) }) : undefined"
    >{{ formatValue(sample?.value, unit, widget.config.decimals) }}</div>
    <WindowAverages v-if="!error && data?.averages?.length" :averages="data.averages" :unit="unit" />
  </div>
</template>

<style scoped>
.w-number {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  padding-top: calc(var(--pad) * 0.7);
  padding-bottom: calc(var(--pad) * 0.7);
  overflow: hidden;
}

.top {
  display: flex;
  gap: 8px;
  align-items: baseline;
  color: var(--text-muted);
}

.title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ago {
  flex: none;
  font-size: 0.75rem;
  color: var(--warn);
}

.value {
  font-family: var(--font-display);
  font-stretch: 75%;
  font-size: clamp(1.5rem, 2.6vw, 2.1rem);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 0 calc(var(--glow) * 1.6) color-mix(in srgb, currentColor 45%, transparent);
}

.value.empty {
  font-family: var(--font);
  font-size: 1rem;
  font-weight: 400;
  color: var(--text-muted);
  text-shadow: none;
}

.value.ok {
  color: var(--ok);
}

.value.warn {
  color: var(--warn);
}

.value.crit {
  color: var(--bad);
}

.err {
  color: var(--bad);
}
</style>
