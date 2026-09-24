<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import MetricPanel from '@/components/MetricPanel.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import RangePicker from '@/components/ui/RangePicker.vue'
import ToneBadge from '@/components/ui/ToneBadge.vue'
import { t } from '@/i18n'
import { formatAgo } from '@/lib/format'
import { sourceLabel, sourceTone } from '@/lib/status'
import type { TimeWindow } from '@/lib/time'
import { ensureCatalog, store } from '@/stores/app'

const route = useRoute()
const win = ref<TimeWindow>({ range: '24h' })
const error = ref<unknown>(null)
const sourceId = computed(() => String(route.params.sourceId))
const source = computed(() => store.sources.find((s) => s.id === sourceId.value))
const category = computed(() => (source.value?.kind === 'cadvisor' ? 'container' : 'node'))
const presets = computed(() =>
  store.presets.filter((p) => p.category === category.value && p.vars.includes('source_id')),
)
const vars = computed(() => ({ source_id: sourceId.value }))

onMounted(() => ensureCatalog().catch((e) => (error.value = e)))
</script>

<template>
  <div class="page">
    <p><RouterLink to="/monitoring">← {{ t('monitoring.overview') }}</RouterLink></p>
    <ApiErrorAlert :error="error" />
    <template v-if="source">
      <header class="head">
        <h1>{{ t('monitoring.node') }}: {{ source.name }}</h1>
        <ToneBadge
          v-if="source.status"
          :tone="sourceTone[source.status.state]"
          :text="sourceLabel(source.status.state)"
          :title="source.status.error"
        />
        <span class="small muted">{{ t('sources.lastSuccess') }}: {{ formatAgo(source.status?.last_success) }}</span>
      </header>
      <RangePicker v-model="win" />
      <div class="charts">
        <MetricPanel
          v-for="p in presets"
          :key="p.id"
          :title="p.title"
          :preset-id="p.id"
          :vars="vars"
          :unit="p.unit"
          :thresholds="p.thresholds"
          :window="win"
        />
      </div>
    </template>
    <p v-else-if="store.loaded.sources" class="muted">{{ t('errors.not_found') }}</p>
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

.charts {
  margin-top: 12px;
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 420px), 1fr));
}
</style>
