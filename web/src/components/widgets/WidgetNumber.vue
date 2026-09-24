<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import type { Widget } from '@/api'
import { errorText, t } from '@/i18n'
import { formatAgo, formatTime, formatValue, thresholdColor } from '@/lib/format'
import { useWidgetContext, useWidgetData } from './context'

const props = defineProps<{ widget: Widget<'number'> }>()
const ctx = useWidgetContext()
const { data, error } = useWidgetData(toRef(props, 'widget'), ref({}), computed(() => !!props.widget.config.metric.preset_id))
const preset = computed(() => ctx.preset(props.widget.config.metric.preset_id))
const unit = computed(() => data.value?.instant?.unit ?? preset.value?.unit ?? '')
const sample = computed(() => data.value?.instant?.samples[0] ?? null)
const title = computed(() => props.widget.config.title || preset.value?.title || '')
const tone = computed(() => thresholdColor(sample.value?.value, data.value?.thresholds ?? preset.value?.thresholds))
</script>

<template>
  <div class="card w-number">
    <div class="title small muted">{{ title }}</div>
    <div class="value" :class="tone">{{ formatValue(sample?.value, unit, widget.config.decimals) }}</div>
    <div v-if="error" class="small err" role="alert">{{ errorText(error) }}</div>
    <div v-else-if="sample?.time" class="small muted" :title="formatTime(sample.time)">
      {{ t('widgets.lastValueAt', { time: formatAgo(sample.time) }) }}
    </div>
  </div>
</template>

<style scoped>
.w-number {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
}

.title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.value {
  font-size: clamp(1.3rem, 3vw, 2rem);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
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
