<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type InstantSample, type QueryPreset, type Status } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import ToneBadge from '@/components/ui/ToneBadge.vue'
import RangePicker from '@/components/ui/RangePicker.vue'
import MetricPanel from '@/components/MetricPanel.vue'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { formatAgo, formatBytes, formatValue } from '@/lib/format'
import { chartLimiter } from '@/lib/queue'
import { sourceLabel, sourceTone } from '@/lib/status'
import type { TimeWindow } from '@/lib/time'
import { ensureCatalog, loadServices, loadSources, store } from '@/stores/app'

const status = ref<Status | null>(null)
const error = ref<unknown>(null)
const nodeValues = ref<Record<string, Record<string, InstantSample | null>>>({})
const win = ref<TimeWindow>({ range: '24h' })

const nodes = computed(() => store.sources.filter((s) => s.kind === 'node_exporter'))
const problems = computed(() => store.sources.filter((s) => s.status?.state === 'down'))
const downServices = computed(() =>
  store.services.filter((s) => s.check_id && s.status && ['down', 'stale'].includes(s.status.state)),
)
const nodeSummary = computed<QueryPreset[]>(() =>
  store.presets
    .filter((p) => p.category === 'node' && p.vars.includes('source_id') && (p.unit === 'percent' || p.unit === 'percent_unit'))
    .slice(0, 3),
)
const selfPresets = computed(() => store.presets.filter((p) => p.category === 'homedeck' && !p.vars.length).slice(0, 2))

usePolling(async (signal) => {
  try {
    await ensureCatalog()
    const [s] = await Promise.all([api.status(signal), loadServices(), loadSources()])
    status.value = s
    error.value = null
  } catch (e) {
    if (!signal.aborted) error.value = e
    return
  }
  const next: typeof nodeValues.value = {}
  await Promise.all(
    nodes.value.flatMap((n) =>
      nodeSummary.value.map(async (p) => {
        next[n.id] ??= {}
        try {
          const r = await chartLimiter.run(() => api.query({ preset_id: p.id, vars: { source_id: n.id } }, signal), signal)
          next[n.id]![p.id] = r.samples[0] ?? null
        } catch {
          next[n.id]![p.id] = null
        }
      }),
    ),
  )
  nodeValues.value = next
})
</script>

<template>
  <div class="page">
    <h1>{{ t('monitoring.overview') }}</h1>
    <ApiErrorAlert :error="error" />

    <div v-if="status" class="tiles">
      <div class="card tile">
        <div class="muted small">{{ t('monitoring.servicesUp') }}</div>
        <div class="big">{{ status.services.up }}</div>
        <div class="muted small">{{ t('monitoring.of', { n: status.services.total - status.services.disabled }) }}</div>
      </div>
      <div class="card tile">
        <div class="muted small">{{ t('monitoring.tsdb') }}</div>
        <ToneBadge
          :tone="status.tsdb.state === 'running' ? 'ok' : status.tsdb.state === 'failed' ? 'bad' : 'warn'"
          :text="t(`monitoring.tsdbStates.${status.tsdb.state}`)"
          :title="status.tsdb.error"
        />
        <div v-if="status.tsdb.error" class="small err">{{ status.tsdb.error }}</div>
      </div>
      <div class="card tile">
        <div class="muted small">{{ t('monitoring.disk') }}</div>
        <ToneBadge
          :tone="status.disk.level === 'ok' ? 'ok' : status.disk.level === 'warning' ? 'warn' : 'bad'"
          :text="t(`monitoring.diskLevels.${status.disk.level}`)"
        />
        <div class="muted small">
          {{ t('monitoring.free', { free: formatBytes(status.disk.free_bytes), total: formatBytes(status.disk.total_bytes) }) }}
        </div>
      </div>
    </div>

    <div class="cols">
      <section class="card">
        <h2>{{ t('monitoring.problemSources') }}</h2>
        <p v-if="!problems.length && !downServices.length" class="muted">{{ t('monitoring.noProblems') }}</p>
        <ul class="list">
          <li v-for="s in downServices" :key="s.id">
            <RouterLink :to="`/monitoring/services/${s.id}`">{{ s.name }}</RouterLink>
            <StatusBadge :status="s.status" />
            <span v-if="s.status?.error" class="small muted">{{ s.status.error }}</span>
          </li>
          <li v-for="s in problems" :key="s.id">
            <RouterLink to="/sources">{{ s.name }}</RouterLink>
            <ToneBadge :tone="sourceTone[s.status!.state]" :text="sourceLabel(s.status!.state)" />
            <span class="small muted">{{ s.status?.error }} · {{ formatAgo(s.status?.last_success) }}</span>
          </li>
        </ul>
      </section>

      <section class="card">
        <h2>{{ t('monitoring.nodes') }}</h2>
        <p v-if="!nodes.length" class="muted">{{ t('monitoring.noNodes') }}</p>
        <ul class="list">
          <li v-for="n in nodes" :key="n.id">
            <RouterLink :to="`/monitoring/nodes/${n.id}`">{{ n.name }}</RouterLink>
            <span v-for="p in nodeSummary" :key="p.id" class="small">
              <span class="muted">{{ p.title }}:</span> {{ formatValue(nodeValues[n.id]?.[p.id]?.value, p.unit) }}
            </span>
          </li>
        </ul>
      </section>
    </div>

    <section class="card">
      <h2>{{ t('monitoring.services') }}</h2>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>{{ t('monitoring.service') }}</th>
              <th>{{ t('monitoring.status') }}</th>
              <th>{{ t('monitoring.latency') }}</th>
              <th>{{ t('monitoring.lastRun') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in store.services.filter((x) => x.check_id)" :key="s.id">
              <td><RouterLink :to="`/monitoring/services/${s.id}`">{{ s.name }}</RouterLink></td>
              <td><StatusBadge :status="s.status" /></td>
              <td>{{ formatValue(s.status?.duration_ms, 'milliseconds') }}</td>
              <td>{{ formatAgo(s.status?.last_run) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <template v-if="selfPresets.length">
      <RangePicker v-model="win" class="picker" />
      <div class="charts">
        <MetricPanel v-for="p in selfPresets" :key="p.id" :title="p.title" :preset-id="p.id" :unit="p.unit" :window="win" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
  margin-bottom: 12px;
}

.tile {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: flex-start;
}

.big {
  font-size: 2rem;
  font-weight: 700;
  line-height: 1.1;
}

.err {
  color: var(--bad);
}

.cols {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
  margin-bottom: 12px;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.list li {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
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
