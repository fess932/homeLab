<script setup lang="ts">
import { computed } from 'vue'
import type { Widget } from '@/api'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import { t } from '@/i18n'
import { formatAgo, formatValue } from '@/lib/format'
import { probeLabel, probeTone, statusText } from '@/lib/status'
import { useWidgetContext } from './context'

const props = defineProps<{ widget: Widget<'status'> }>()
const ctx = useWidgetContext()
const service = computed(() => ctx.service(props.widget.config.service_id))
const check = computed(() => ctx.check(service.value?.check_id ?? null))
const status = computed(() => service.value?.status)
const detail = computed(() =>
  [statusText(status.value), status.value?.error, check.value && t('status.basis', { kind: check.value.kind.toUpperCase(), target: check.value.target })]
    .filter(Boolean)
    .join('\n'),
)
const tone = computed(() => (service.value?.check_id ? probeTone[status.value?.state ?? 'unknown'] : ''))
</script>

<template>
  <div class="card w-status" :class="tone">
    <template v-if="service">
      <span v-if="service.check_id" class="led" aria-hidden="true" />
      <div class="head">
        <ServiceIcon :icon="service.icon" :size="22" />
        <span class="name">{{ service.name }}</span>
        <span v-if="service.check_id" class="state" :title="detail">{{ probeLabel(status?.state ?? 'unknown') }}</span>
      </div>
      <template v-if="service.check_id">
        <span class="sr-only">{{ detail }}</span>
        <div class="small muted metrics">
          {{ formatValue(status?.duration_ms, 'milliseconds') }} · {{ formatAgo(status?.last_run) }}
        </div>
      </template>
      <div v-else class="small muted">{{ t('status.noCheck') }}</div>
    </template>
    <div v-else class="muted">{{ widget.config.service_id ? t('widgets.missingService') : t('widgets.noService') }}</div>
  </div>
</template>

<style scoped>
.w-status {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 2px;
  justify-content: center;
  padding-left: calc(var(--pad) + 4px);
  overflow: hidden;
}

.led {
  position: absolute;
  left: 1px;
  top: var(--cut);
  bottom: 1px;
  width: 3px;
  background: var(--muted);
}

.ok .led {
  background: var(--ok);
  box-shadow: 0 0 var(--glow) var(--ok);
}

.bad .led {
  background: var(--bad);
  box-shadow: 0 0 var(--glow) var(--bad);
}

.warn .led {
  background: var(--warn);
  box-shadow: 0 0 var(--glow) var(--warn);
}

.head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.name {
  flex: 1;
  min-width: 0;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.state {
  flex: none;
  font-family: var(--font-display);
  font-size: 0.85rem;
  color: var(--muted);
}

.ok .state {
  color: var(--ok);
}

.bad .state {
  color: var(--bad);
}

.warn .state {
  color: var(--warn);
}

.metrics {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
