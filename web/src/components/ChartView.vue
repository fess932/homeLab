<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { RangeResult, Threshold, Unit } from '@/api'
import { init, type ECharts } from '@/lib/echarts'
import { formatAxis, formatTime, formatValue, lastValue } from '@/lib/format'
import { t } from '@/i18n'

const props = withDefaults(
  defineProps<{
    result: RangeResult | null
    unit?: Unit
    thresholds?: Threshold[]
    stacked?: boolean
    spark?: boolean
    label?: string
  }>(),
  { unit: '', thresholds: () => [], stacked: false, spark: false, label: '' },
)

const el = ref<HTMLDivElement | null>(null)
let chart: ECharts | null = null
let ro: ResizeObserver | null = null

const hasData = computed(() => props.result?.series.some((s) => s.values.some((v) => v !== null)) ?? false)

const summary = computed(() => {
  const r = props.result
  if (!r || !hasData.value) return t('units.noData')
  return r.series
    .slice(0, 5)
    .map((s) => {
      const last = lastValue(s.values, r.start, r.step)
      return `${s.name || ''} ${formatValue(last.value, props.unit)}`.trim()
    })
    .join('; ')
})

function css(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || '#888'
}

const thresholdColors = { ok: '--ok', warn: '--warn', crit: '--bad' } as const

function hasIsolated(values: (number | null)[]) {
  return values.some((v, i) => v !== null && (values[i - 1] ?? null) === null && (values[i + 1] ?? null) === null)
}

function render() {
  if (!el.value || !props.result) return
  chart ??= init(el.value, undefined, { renderer: 'canvas' })
  const r = props.result
  const text = css('--text-muted')
  const border = css('--border')
  const accent = css('--accent')
  const series = r.series.map((s, i) => ({
    name: s.name || Object.values(s.labels).join(' ') || `#${i + 1}`,
    type: 'line' as const,
    showSymbol: hasIsolated(s.values),
    symbolSize: 3,
    connectNulls: false,
    stack: props.stacked ? 'total' : undefined,
    areaStyle: props.stacked || props.spark ? { opacity: props.spark ? 0.15 : 0.35 } : undefined,
    lineStyle: { width: props.spark ? 1.5 : 1.8 },
    emphasis: { disabled: props.spark },
    data: s.values.map((v, idx) => [(r.start + idx * r.step) * 1000, v]),
    markLine:
      i === 0 && props.thresholds.length && !props.spark
        ? {
            silent: true,
            symbol: 'none',
            label: { show: false },
            data: props.thresholds.map((th) => ({
              yAxis: th.value,
              lineStyle: { color: css(thresholdColors[th.color]), type: 'dashed' as const },
            })),
          }
        : undefined,
  }))
  chart.setOption(
    {
      animation: false,
      color: r.series.length === 1 || props.spark ? [accent] : undefined,
      grid: props.spark
        ? { left: 0, right: 0, top: 2, bottom: 0 }
        : { left: 8, right: 12, top: r.series.length > 1 ? 36 : 12, bottom: 8, containLabel: true },
      legend: props.spark || r.series.length < 2 ? { show: false } : { type: 'scroll', top: 0, textStyle: { color: text } },
      tooltip: props.spark
        ? { show: false }
        : {
            trigger: 'axis',
            valueFormatter: (v: unknown) => formatValue(typeof v === 'number' ? v : null, props.unit),
          },
      xAxis: {
        type: 'time',
        show: !props.spark,
        axisLabel: { color: text, hideOverlap: true },
        axisLine: { lineStyle: { color: border } },
      },
      yAxis: {
        type: 'value',
        show: !props.spark,
        scale: props.spark,
        axisLabel: { color: text, formatter: (v: number) => formatAxis(v, props.unit) },
        splitLine: { lineStyle: { color: border } },
        min: props.unit === 'percent' && !props.spark ? 0 : undefined,
      },
      series,
    },
    { notMerge: true },
  )
}

onMounted(() => {
  render()
  if (el.value) {
    ro = new ResizeObserver(() => chart?.resize())
    ro.observe(el.value)
  }
})

watch(() => [props.result, props.unit, props.thresholds, props.stacked], render, { deep: false })

onBeforeUnmount(() => {
  ro?.disconnect()
  chart?.dispose()
  chart = null
})

const lastTime = computed(() => {
  const r = props.result
  if (!r) return null
  let best: number | null = null
  for (const s of r.series) {
    const l = lastValue(s.values, r.start, r.step)
    if (l.time !== null && (best === null || l.time > best)) best = l.time
  }
  return best
})

defineExpose({ lastTime })
</script>

<template>
  <div class="chart-view" :class="{ spark }">
    <div ref="el" class="canvas" role="img" :aria-label="`${label} ${summary}`.trim()" />
    <div v-if="result && !hasData" class="empty muted small">{{ t('units.noData') }}</div>
    <div v-if="!spark && lastTime" class="stamp muted small">
      {{ t('widgets.lastValueAt', { time: formatTime(lastTime) }) }}
    </div>
  </div>
</template>

<style scoped>
.chart-view {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 60px;
  display: flex;
  flex-direction: column;
}

.chart-view.spark {
  min-height: 24px;
}

.canvas {
  flex: 1;
  min-height: 0;
  width: 100%;
}

.empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  pointer-events: none;
}

.stamp {
  text-align: right;
  font-size: 0.75rem;
}
</style>
