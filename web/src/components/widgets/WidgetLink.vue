<script setup lang="ts">
import { computed, toRef } from 'vue'
import type { Widget } from '@/api'
import ChartView from '@/components/ChartView.vue'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import { t } from '@/i18n'
import { formatValue, isValue } from '@/lib/format'
import { useWidgetContext, useWidgetData } from './context'

const props = defineProps<{ widget: Widget<'link'> }>()
const ctx = useWidgetContext()
const cfg = computed(() => props.widget.config)
const service = computed(() => ctx.service(cfg.value.service_id))
const check = computed(() => ctx.check(service.value?.check_id ?? null))
const hasMetric = computed(() => !!cfg.value.metric?.preset_id)
const unit = computed(() => (cfg.value.metric ? (ctx.preset(cfg.value.metric.preset_id)?.unit ?? '') : ''))
const { data } = useWidgetData(toRef(props, 'widget'), computed(() => ({ range: '1h' as const, points: 60 })), hasMetric)
const basis = computed(() =>
  check.value ? t('status.basis', { kind: check.value.kind.toUpperCase(), target: check.value.target }) : '',
)
// Простая ссылка без сервиса: адрес и название из самого виджета, без статуса.
const plainHost = computed(() => {
  try {
    return new URL(cfg.value.url ?? '').host
  } catch {
    return cfg.value.url ?? ''
  }
})
const plainTitle = computed(() => cfg.value.title || plainHost.value)
const newTab = computed(() => service.value?.open_mode === 'new_tab')
const latency = computed(() => service.value?.status?.duration_ms)
const showStatus = computed(() => cfg.value.show_status && !!service.value?.check_id)

function guard(e: MouseEvent) {
  if (ctx.mode === 'edit') e.preventDefault()
}
</script>

<template>
  <a
    v-if="!cfg.service_id && cfg.url"
    class="card w-link"
    :href="cfg.url"
    target="_blank"
    rel="noopener noreferrer"
    :draggable="false"
    @click="guard"
  >
    <ServiceIcon :icon="cfg.icon" :size="28" />
    <div class="body">
      <div class="head">
        <span class="name">{{ plainTitle }}</span>
        <span class="sr-only">({{ t('widgets.openNewTab') }})</span>
      </div>
      <p v-if="cfg.title" class="desc muted small">{{ plainHost }}</p>
    </div>
  </a>
  <div v-else-if="!service" class="card w-link missing muted">
    {{ cfg.service_id ? t('widgets.missingService') : t('widgets.noService') }}
  </div>
  <a
    v-else
    class="card w-link"
    :href="service.url"
    :target="newTab ? '_blank' : undefined"
    :rel="newTab ? 'noopener noreferrer' : undefined"
    :draggable="false"
    @click="guard"
  >
    <ServiceIcon :icon="service.icon" :size="28" />
    <div class="body">
      <div class="head">
        <span class="name">{{ service.name }}</span>
        <span v-if="newTab" class="sr-only">({{ t('widgets.openNewTab') }})</span>
        <span v-if="cfg.show_latency && service.check_id" class="latency" :title="t('widgets.latency')">
          {{ isValue(latency) ? formatValue(latency, 'milliseconds') : t('units.noData') }}
        </span>
        <StatusBadge v-if="showStatus" class="led" :status="service.status" :hint="basis || undefined" compact />
      </div>
      <div v-if="service.description || service.tags.length" class="sub">
        <p class="desc muted small">{{ service.description }}</p>
        <span v-for="tag in service.tags" :key="tag" class="tag">{{ tag }}</span>
      </div>
    </div>
    <div v-if="hasMetric" class="spark">
      <ChartView :result="data?.range ?? null" :unit="unit" spark :label="service.name" />
    </div>
  </a>
</template>

<style scoped>
.w-link {
  display: flex;
  gap: 12px;
  align-items: center;
  height: 100%;
  color: var(--text);
  text-decoration: none;
  overflow: hidden;
}

a.w-link:hover {
  --edge: var(--accent);
}

.missing {
  align-items: center;
  justify-content: center;
}

.body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.name {
  flex: 1;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.latency {
  flex: none;
  font-family: var(--font-display);
  font-stretch: 85%;
  font-size: 0.8rem;
  color: var(--text-muted);
}

.sub {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.desc {
  flex: 1;
  min-width: 0;
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.spark {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 28px;
  opacity: 0.9;
  pointer-events: none;
}
</style>
