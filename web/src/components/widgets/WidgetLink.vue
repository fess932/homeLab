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
const newTab = computed(() => service.value?.open_mode === 'new_tab')
const latency = computed(() => service.value?.status?.duration_ms)

function guard(e: MouseEvent) {
  if (ctx.mode === 'edit') e.preventDefault()
}
</script>

<template>
  <div v-if="!service" class="card w-link missing muted">
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
    <ServiceIcon :icon="service.icon" :size="32" />
    <div class="body">
      <div class="head">
        <span class="name">{{ service.name }}</span>
        <span v-if="newTab" class="sr-only">({{ t('widgets.openNewTab') }})</span>
        <StatusBadge v-if="cfg.show_status && service.check_id" :status="service.status" :title="basis" />
      </div>
      <p v-if="service.description" class="desc muted small">{{ service.description }}</p>
      <div v-if="cfg.show_latency && service.check_id" class="small muted">
        {{ t('widgets.latency') }}: {{ isValue(latency) ? formatValue(latency, 'milliseconds') : t('units.noData') }}
      </div>
      <div v-if="service.tags.length" class="tags">
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
  align-items: flex-start;
  height: 100%;
  color: var(--text);
  text-decoration: none;
  overflow: hidden;
  position: relative;
}

a.w-link:hover {
  border-color: var(--accent);
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
  gap: 4px;
}

.head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.desc {
  margin: 0;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
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
