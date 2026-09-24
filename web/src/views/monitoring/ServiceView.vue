<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import MetricPanel from '@/components/MetricPanel.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import RangePicker from '@/components/ui/RangePicker.vue'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { formatAgo, formatTime, formatValue } from '@/lib/format'
import type { TimeWindow } from '@/lib/time'
import { ensureCatalog, loadChecks, loadServices, store } from '@/stores/app'

const route = useRoute()
const win = ref<TimeWindow>({ range: '24h' })
const error = ref<unknown>(null)
const serviceId = computed(() => String(route.params.serviceId))
const service = computed(() => store.services.find((s) => s.id === serviceId.value))
const check = computed(() => store.checks.find((c) => c.id === service.value?.check_id))
const probePresets = computed(() => store.presets.filter((p) => p.category === 'probe' && p.vars.includes('service_id')))
const sourcePresets = computed(() => {
  const src = store.sources.find((s) => s.id === service.value?.source_id)
  if (!src) return []
  const cat = src.kind === 'cadvisor' ? 'container' : src.kind === 'node_exporter' ? 'node' : ''
  return store.presets.filter((p) => p.category === cat && p.vars.includes('source_id')).slice(0, 4)
})

usePolling(async () => {
  try {
    await ensureCatalog()
    await Promise.all([loadServices(), loadChecks()])
    error.value = null
  } catch (e) {
    error.value = e
  }
})
</script>

<template>
  <div class="page">
    <p><RouterLink to="/monitoring">← {{ t('monitoring.overview') }}</RouterLink></p>
    <ApiErrorAlert :error="error" />
    <template v-if="service">
      <header class="head">
        <ServiceIcon :icon="service.icon" :size="32" />
        <h1>{{ service.name }}</h1>
        <StatusBadge v-if="service.check_id" :status="service.status" />
        <a :href="service.url" target="_blank" rel="noopener noreferrer" class="btn small">{{ t('monitoring.open') }}</a>
      </header>
      <section class="card">
        <p v-if="!check" class="muted">{{ t('status.noCheck') }}</p>
        <dl v-else class="props">
          <dt>{{ t('settings.checkKind') }}</dt>
          <dd>{{ t(`status.basis`, { kind: check.kind.toUpperCase(), target: check.target }) }}</dd>
          <dt>{{ t('monitoring.latency') }}</dt>
          <dd>{{ formatValue(service.status?.duration_ms, 'milliseconds') }}</dd>
          <dt>{{ t('monitoring.lastRun') }}</dt>
          <dd :title="formatTime(service.status?.last_run)">{{ formatAgo(service.status?.last_run) }}</dd>
          <dt>{{ t('monitoring.lastSuccess') }}</dt>
          <dd :title="formatTime(service.status?.last_success)">{{ formatAgo(service.status?.last_success) }}</dd>
          <template v-if="service.status?.http_status">
            <dt>{{ t('monitoring.httpStatus') }}</dt>
            <dd>{{ service.status.http_status }}</dd>
          </template>
          <template v-if="service.status?.error">
            <dt>{{ t('monitoring.lastError') }}</dt>
            <dd class="err">{{ service.status.error }}</dd>
          </template>
        </dl>
      </section>
      <RangePicker v-model="win" class="picker" />
      <div class="charts">
        <MetricPanel
          v-for="p in probePresets"
          :key="p.id"
          :title="p.title"
          :preset-id="p.id"
          :vars="{ service_id: service.id }"
          :unit="p.unit"
          :thresholds="p.thresholds"
          :window="win"
        />
        <MetricPanel
          v-for="p in sourcePresets"
          :key="p.id"
          :title="p.title"
          :preset-id="p.id"
          :vars="{ source_id: service.source_id! }"
          :unit="p.unit"
          :thresholds="p.thresholds"
          :window="win"
        />
      </div>
    </template>
    <p v-else-if="store.loaded.services" class="muted">{{ t('errors.not_found') }}</p>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
}

.head h1 {
  margin: 0;
}

.err {
  color: var(--bad);
}

.picker {
  margin: 16px 0 12px;
}

.charts {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr));
}
</style>
