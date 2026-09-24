<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, type RangeResult, type Threshold, type Unit } from '@/api'
import ChartView from '@/components/ChartView.vue'
import { usePolling } from '@/composables/polling'
import { errorText } from '@/i18n'
import { chartLimiter } from '@/lib/queue'
import { pointsForWidth, resolveWindow, type TimeWindow } from '@/lib/time'

const props = withDefaults(
  defineProps<{
    title: string
    window: TimeWindow
    presetId?: string
    query?: string
    vars?: Record<string, string>
    unit?: Unit
    thresholds?: Threshold[]
    height?: number
  }>(),
  { presetId: '', query: '', vars: () => ({}), unit: '', thresholds: () => [], height: 220 },
)

const root = ref<HTMLElement | null>(null)
const width = ref(600)
const result = ref<RangeResult | null>(null)
const error = ref<unknown>(null)
let ro: ResizeObserver | null = null

onMounted(() => {
  if (!root.value) return
  width.value = root.value.clientWidth || 600
  ro = new ResizeObserver(([e]) => {
    const w = Math.round(e?.contentRect.width ?? 0)
    if (w && Math.abs(w - width.value) > 60) width.value = w
  })
  ro.observe(root.value)
})
onBeforeUnmount(() => ro?.disconnect())

const key = computed(() => JSON.stringify([props.presetId, props.query, props.vars, props.window]))

const poll = usePolling(async (signal) => {
  if (!props.presetId && !props.query) return
  const { start, end } = resolveWindow(props.window)
  try {
    result.value = await chartLimiter.run(
      () =>
        api.queryRange(
          {
            preset_id: props.presetId || undefined,
            query: props.presetId ? undefined : props.query,
            vars: props.vars,
            start: start.toISOString(),
            end: end.toISOString(),
            points: pointsForWidth(width.value),
          },
          signal,
        ),
      signal,
    )
    error.value = null
  } catch (e) {
    if (!signal.aborted) error.value = e
  }
}, [key, width])

watch(key, () => (result.value = null))
defineExpose({ reload: poll.run })
</script>

<template>
  <section ref="root" class="card panel">
    <h3>{{ title }}</h3>
    <div v-if="error" class="alert bad small" role="alert">{{ errorText(error) }}</div>
    <div class="plot" :style="{ height: `${height}px` }">
      <ChartView :result="result" :unit="result?.unit || unit" :thresholds="thresholds" :label="title" />
    </div>
  </section>
</template>

<style scoped>
.panel h3 {
  font-size: 0.95rem;
  margin-bottom: 6px;
}
</style>
