<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRef } from 'vue'
import type { Widget } from '@/api'
import ChartView from '@/components/ChartView.vue'
import { errorText } from '@/i18n'
import { pointsForWidth } from '@/lib/time'
import { useWidgetContext, useWidgetData } from './context'

const props = defineProps<{ widget: Widget<'chart'> }>()
const ctx = useWidgetContext()
const root = ref<HTMLElement | null>(null)
const width = ref(600)
let ro: ResizeObserver | null = null

onMounted(() => {
  if (!root.value) return
  width.value = root.value.clientWidth || 600
  ro = new ResizeObserver(([entry]) => {
    const w = Math.round(entry?.contentRect.width ?? 0)
    if (w && Math.abs(w - width.value) > 60) width.value = w
  })
  ro.observe(root.value)
})
onBeforeUnmount(() => ro?.disconnect())

const req = computed(() => ({ range: props.widget.config.range, points: pointsForWidth(width.value) }))
const { data, error } = useWidgetData(toRef(props, 'widget'), req, computed(() => !!props.widget.config.metric.preset_id))
const preset = computed(() => ctx.preset(props.widget.config.metric.preset_id))
const title = computed(() => props.widget.config.title || preset.value?.title || '')
const unit = computed(() => data.value?.range?.unit ?? preset.value?.unit ?? '')
</script>

<template>
  <section ref="root" class="card w-chart">
    <h3 class="title">{{ title }}</h3>
    <div v-if="error" class="alert bad small" role="alert">{{ errorText(error) }}</div>
    <div class="plot">
      <ChartView
        :result="data?.range ?? null"
        :unit="unit"
        :thresholds="data?.thresholds ?? preset?.thresholds ?? []"
        :stacked="widget.config.stacked"
        :label="title"
      />
    </div>
  </section>
</template>

<style scoped>
.w-chart {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.title {
  font-size: 0.95rem;
  margin: 0 0 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plot {
  flex: 1;
  min-height: 0;
}
</style>
