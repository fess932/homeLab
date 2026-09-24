<script setup lang="ts">
import { computed, ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, type Secret, type Source, type SourceTestResult, type Status } from '@/api'
import SecretsPanel from '@/components/SecretsPanel.vue'
import SourceForm from '@/components/SourceForm.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ToneBadge from '@/components/ui/ToneBadge.vue'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { formatAgo, formatBytes, formatTime, formatValue } from '@/lib/format'
import { sourceLabel, sourceTone } from '@/lib/status'
import { loadSources, store } from '@/stores/app'

const secrets = ref<Secret[]>([])
const status = ref<Status | null>(null)
const error = ref<unknown>(null)
const editing = ref<Source | null | 'new'>(null)
const tests = ref<Record<string, SourceTestResult | 'running'>>({})

const system = computed(() => store.sources.filter((s) => s.system))
const user = computed(() => store.sources.filter((s) => !s.system))

async function refresh(signal?: AbortSignal) {
  try {
    const [, sec, st] = await Promise.all([loadSources(), api.secrets.list(), api.status(signal)])
    secrets.value = sec
    status.value = st
    error.value = null
  } catch (e) {
    if (!signal?.aborted) error.value = e
  }
}

usePolling(refresh)

async function test(s: Source) {
  tests.value[s.id] = 'running'
  try {
    tests.value[s.id] = await api.sources.test(s.id)
  } catch (e) {
    delete tests.value[s.id]
    error.value = e
  }
}

async function remove(s: Source) {
  if (!confirm(t('app.confirmDelete', { name: s.name }))) return
  try {
    await api.sources.remove(s.id)
    await refresh()
  } catch (e) {
    error.value = e
  }
}

async function onSaved() {
  editing.value = null
  await refresh()
}

const pending = computed(() => status.value && status.value.config.applied_revision < status.value.config.desired_revision)
const testResult = (id: string) => {
  const r = tests.value[id]
  return r && r !== 'running' ? r : null
}
</script>

<template>
  <div class="page">
    <div class="toolbar head">
      <h1>{{ t('sources.title') }}</h1>
      <span class="spacer" />
      <button type="button" class="btn primary" @click="editing = 'new'"><Plus :size="16" aria-hidden="true" /> {{ t('sources.add') }}</button>
    </div>
    <ApiErrorAlert :error="error" />
    <div v-if="status?.config.error" class="alert bad" role="alert">
      {{ t('sources.configError', { error: status.config.error }) }}
    </div>
    <div v-else-if="pending" class="alert warn" role="status">
      {{ t('sources.configPending', { desired: status!.config.desired_revision, applied: status!.config.applied_revision }) }}
    </div>

    <p v-if="store.loaded.sources && !user.length" class="muted">{{ t('sources.empty') }}</p>
    <div class="grid-cards list">
      <article v-for="s in [...user, ...system]" :key="s.id" class="card src">
        <header class="src-head">
          <h2>{{ s.name }}</h2>
          <span v-if="s.system" class="tag">{{ t('sources.system') }}</span>
          <ToneBadge v-if="s.status" :tone="sourceTone[s.status.state]" :text="sourceLabel(s.status.state)" />
        </header>
        <div class="small muted url">{{ t(`sources.kinds.${s.kind}`) }} · {{ s.url }}</div>
        <dl class="props small">
          <dt>{{ t('sources.lastAttempt') }}</dt>
          <dd :title="formatTime(s.status?.last_attempt)">{{ formatAgo(s.status?.last_attempt) }}</dd>
          <dt>{{ t('sources.lastSuccess') }}</dt>
          <dd :title="formatTime(s.status?.last_success)">{{ formatAgo(s.status?.last_success) }}</dd>
          <dt>{{ t('sources.duration') }}</dt>
          <dd>{{ formatValue(s.status?.duration_ms, 'milliseconds') }}</dd>
          <dt>{{ t('sources.samples') }}</dt>
          <dd>{{ formatValue(s.status?.samples, 'count') }}</dd>
          <dt>{{ t('sources.interval') }}</dt>
          <dd>{{ s.interval_s }} / {{ s.timeout_s }}</dd>
        </dl>
        <div v-if="s.status?.error" class="alert bad small">{{ s.status.error }}</div>
        <div v-if="testResult(s.id)" class="alert small" :class="testResult(s.id)!.ok ? 'ok' : 'bad'" role="status">
          <template v-if="testResult(s.id)!.ok">
            {{
              t('sources.testOk', {
                samples: testResult(s.id)!.samples,
                bytes: formatBytes(testResult(s.id)!.bytes),
                ms: Math.round(testResult(s.id)!.duration_ms),
              })
            }}
          </template>
          <template v-else>
            <strong>{{ t(`sources.errorKinds.${testResult(s.id)!.error_kind}`) || t('sources.testFail') }}</strong>
            {{ testResult(s.id)!.error }}
          </template>
        </div>
        <footer class="toolbar">
          <button v-if="!s.system" type="button" class="btn small" :disabled="tests[s.id] === 'running'" @click="test(s)">
            {{ tests[s.id] === 'running' ? t('sources.testing') : t('sources.test') }}
          </button>
          <button type="button" class="btn small" @click="editing = s">{{ s.system ? t('sources.diagnostics') : t('app.edit') }}</button>
          <RouterLink v-if="s.kind !== 'prometheus'" :to="`/monitoring/nodes/${s.id}`" class="btn small">{{ t('monitoring.open') }}</RouterLink>
          <span class="spacer" />
          <button v-if="!s.system" type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="remove(s)">
            <Trash2 :size="14" aria-hidden="true" />
          </button>
        </footer>
      </article>
    </div>

    <SecretsPanel :secrets="secrets" @changed="refresh()" />

    <SourceForm
      v-if="editing"
      :source="editing === 'new' ? null : editing"
      :secrets="secrets"
      @saved="onSaved"
      @close="editing = null"
    />
  </div>
</template>

<style scoped>
.head {
  margin-bottom: 12px;
}

.head h1 {
  margin: 0;
}

.list {
  margin-bottom: 16px;
}

.src {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.src-head {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.src-head h2 {
  margin: 0;
  font-size: 1rem;
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.url {
  overflow-wrap: anywhere;
}

.src .alert {
  margin: 0;
}
</style>
