<script setup lang="ts">
import { ref } from 'vue'
import { api, type Readiness, type Settings, type Status } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ToneBadge from '@/components/ui/ToneBadge.vue'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { formatAgo, formatBytes } from '@/lib/format'

const status = ref<Status | null>(null)
const settings = ref<Settings | null>(null)
const ready = ref<{ ok: boolean; body: Readiness } | null>(null)
const error = ref<unknown>(null)

usePolling(async (signal) => {
  try {
    const [st, se, res] = await Promise.all([
      api.status(signal),
      api.settings.get(),
      fetch('/readyz', { signal, headers: { Accept: 'application/json' } }),
    ])
    status.value = st
    settings.value = se
    let body: Readiness = {}
    try {
      body = (await res.json()) as Readiness
    } catch {
      body = {}
    }
    ready.value = { ok: res.ok, body }
    error.value = null
  } catch (e) {
    if (!signal.aborted) error.value = e
  }
})
</script>

<template>
  <ApiErrorAlert :error="error" />
  <div v-if="settings?.restart_required?.length" class="alert warn" role="status">
    {{ t('settings.restartRequired', { list: settings.restart_required.join(', ') }) }}
  </div>
  <div class="grid-cards">
    <section v-if="settings?.runtime" class="card">
      <h2>{{ t('settings.runtime') }}</h2>
      <dl class="props">
        <dt>{{ t('settings.version') }}</dt>
        <dd>{{ settings.runtime.version }}</dd>
        <dt>{{ t('settings.tsdbVersion') }}</dt>
        <dd>{{ settings.runtime.tsdb_version }}</dd>
        <dt>{{ t('settings.listen') }}</dt>
        <dd>{{ settings.runtime.listen }}</dd>
        <dt>{{ t('settings.dataDir') }}</dt>
        <dd>{{ settings.runtime.data_dir }}</dd>
        <dt>{{ t('settings.retention') }}</dt>
        <dd>{{ settings.runtime.retention }}</dd>
      </dl>
    </section>
    <section v-if="status" class="card">
      <h2>{{ t('monitoring.tsdb') }}</h2>
      <dl class="props">
        <dt>{{ t('monitoring.status') }}</dt>
        <dd>
          <ToneBadge
            :tone="status.tsdb.state === 'running' ? 'ok' : status.tsdb.state === 'failed' ? 'bad' : 'warn'"
            :text="t(`monitoring.tsdbStates.${status.tsdb.state}`)"
          />
        </dd>
        <dt>{{ t('settings.restarts') }}</dt>
        <dd>{{ status.tsdb.restarts }} · {{ formatAgo(status.tsdb.since) }}</dd>
        <template v-if="status.tsdb.error">
          <dt>{{ t('monitoring.lastError') }}</dt>
          <dd class="err">{{ status.tsdb.error }}</dd>
        </template>
      </dl>
      <h3>{{ t('settings.config') }}</h3>
      <dl class="props">
        <dt>{{ t('settings.desired') }}</dt>
        <dd>{{ status.config.desired_revision }}</dd>
        <dt>{{ t('settings.applied') }}</dt>
        <dd>{{ status.config.applied_revision }}</dd>
        <template v-if="status.config.error">
          <dt>{{ t('monitoring.lastError') }}</dt>
          <dd class="err">{{ status.config.error }}</dd>
        </template>
      </dl>
    </section>
    <section v-if="status" class="card">
      <h2>{{ t('monitoring.disk') }}</h2>
      <ToneBadge
        :tone="status.disk.level === 'ok' ? 'ok' : status.disk.level === 'warning' ? 'warn' : 'bad'"
        :text="t(`monitoring.diskLevels.${status.disk.level}`)"
      />
      <dl class="props">
        <dt>{{ t('settings.freeSpace') }}</dt>
        <dd>{{ formatBytes(status.disk.free_bytes) }} / {{ formatBytes(status.disk.total_bytes) }}</dd>
        <dt>{{ t('settings.metricsSize') }}</dt>
        <dd>{{ formatBytes(status.disk.metrics_bytes) }}</dd>
        <dt>{{ t('settings.appSize') }}</dt>
        <dd>{{ formatBytes(status.disk.app_bytes) }}</dd>
      </dl>
    </section>
    <section v-if="ready" class="card">
      <h2>/readyz</h2>
      <ToneBadge :tone="ready.ok ? 'ok' : 'bad'" :text="ready.ok ? t('settings.ready') : t('settings.notReady')" />
      <dl class="props">
        <template v-for="(v, k) in ready.body" :key="k">
          <dt>{{ k }}</dt>
          <dd>{{ v }}</dd>
        </template>
      </dl>
    </section>
  </div>
</template>

<style scoped>
.err {
  color: var(--bad);
}

.props {
  margin-top: 8px;
}
</style>
