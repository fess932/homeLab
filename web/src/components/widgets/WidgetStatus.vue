<script setup lang="ts">
import { computed } from 'vue'
import type { Widget } from '@/api'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import { t } from '@/i18n'
import { formatAgo, formatValue } from '@/lib/format'
import { useWidgetContext } from './context'

const props = defineProps<{ widget: Widget<'status'> }>()
const ctx = useWidgetContext()
const service = computed(() => ctx.service(props.widget.config.service_id))
const check = computed(() => ctx.check(service.value?.check_id ?? null))
const status = computed(() => service.value?.status)
</script>

<template>
  <div class="card w-status">
    <template v-if="service">
      <div class="head">
        <ServiceIcon :icon="service.icon" :size="22" />
        <span class="name">{{ service.name }}</span>
      </div>
      <template v-if="service.check_id">
        <StatusBadge :status="status" />
        <div class="small muted">
          {{ formatValue(status?.duration_ms, 'milliseconds') }} · {{ formatAgo(status?.last_run) }}
        </div>
        <div v-if="check" class="small muted basis">
          {{ t('status.basis', { kind: check.kind.toUpperCase(), target: check.target }) }}
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
  gap: 4px;
  justify-content: center;
  overflow: hidden;
}

.head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.basis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
