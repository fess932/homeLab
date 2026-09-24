<script setup lang="ts">
import { computed, ref } from 'vue'
import { Plus, Radar, Trash2 } from 'lucide-vue-next'
import { api, type Device, type DeviceInput, type DriverInfo, type Secret, type Source, type SourceTestResult, type Status } from '@/api'
import DeviceForm from '@/components/DeviceForm.vue'
import DiscoverDialog from '@/components/DiscoverDialog.vue'
import SecretsPanel from '@/components/SecretsPanel.vue'
import SourceForm from '@/components/SourceForm.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ToneBadge from '@/components/ui/ToneBadge.vue'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { isRawReading, readingLabel, readingText } from '@/lib/devices'
import { formatAgo, formatBytes, formatTime, formatValue } from '@/lib/format'
import { sourceLabel, sourceTone } from '@/lib/status'
import { loadDevices, loadSources, store } from '@/stores/app'

const secrets = ref<Secret[]>([])
const status = ref<Status | null>(null)
const error = ref<unknown>(null)
const editing = ref<Source | null | 'new'>(null)
const tests = ref<Record<string, SourceTestResult | 'running'>>({})
const editingDevice = ref<Device | null | 'new'>(null)
const newDevice = ref<Partial<DeviceInput> | undefined>(undefined)
const drivers = ref<DriverInfo[]>([])
const discovering = ref<DriverInfo | null>(null)
const discoverable = computed(() => drivers.value.filter((d) => d.discover || d.accounts))
const names = computed(() => Object.fromEntries([...store.sources, ...store.devices].map((x) => [x.id, x.name])))

const system = computed(() => store.sources.filter((s) => s.system))
const user = computed(() => store.sources.filter((s) => !s.system))

async function refresh(signal?: AbortSignal) {
  try {
    const [, sec, st, , drv] = await Promise.all([loadSources(), api.secrets.list(), api.status(signal), loadDevices(), drivers.value.length ? drivers.value : api.drivers.list()])
    drivers.value = drv
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
  editingDevice.value = null
  newDevice.value = undefined
  await refresh()
}

function addDevice(initial?: Partial<DeviceInput>) {
  newDevice.value = initial
  discovering.value = null
  editingDevice.value = 'new'
}

async function removeDevice(d: Device) {
  if (!confirm(t('app.confirmDelete', { name: d.name }))) return
  try {
    await api.devices.remove(d.id)
    await refresh()
  } catch (e) {
    error.value = e
  }
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

    <section class="devices" aria-labelledby="devices-title">
      <div class="toolbar head">
        <h2 id="devices-title">{{ t('devices.title') }}</h2>
        <span class="spacer" />
        <button v-for="d in discoverable" :key="d.kind" type="button" class="btn" @click="discovering = d">
          <Radar :size="16" aria-hidden="true" /> {{ discoverable.length > 1 ? `${t('discover.open')}: ${d.title}` : t('discover.open') }}
        </button>
        <button type="button" class="btn" @click="addDevice()"><Plus :size="16" aria-hidden="true" /> {{ t('devices.add') }}</button>
      </div>
      <p class="small muted">{{ t('devices.hint') }}</p>
      <p v-if="store.loaded.devices && !store.devices.length" class="muted">{{ t('devices.empty') }}</p>
      <div class="grid-cards list">
        <article v-for="d in store.devices" :key="d.id" class="card src">
          <header class="src-head">
            <h2>{{ d.name }}</h2>
            <ToneBadge :tone="sourceTone[d.status.state]" :text="t(`devices.states.${d.status.state}`)" />
          </header>
          <div class="small muted url">
            {{ t(`devices.kinds.${d.kind}`) }} · {{ d.address }}<template v-if="d.status.protocol"> · {{ t('devices.protocol', { v: d.status.protocol }) }}</template>
          </div>
          <dl v-if="d.status.readings.some((r) => !isRawReading(r))" class="props readings">
            <template v-for="r in d.status.readings.filter((r) => !isRawReading(r))" :key="r.key">
              <dt>{{ readingLabel(r.key) }}</dt>
              <dd>{{ readingText(r) }}</dd>
            </template>
          </dl>
          <details v-if="d.status.readings.some(isRawReading)" class="raw small">
            <summary>{{ t('devices.rawReadings', { n: d.status.readings.filter(isRawReading).length }) }}</summary>
            <dl class="props">
              <template v-for="r in d.status.readings.filter(isRawReading)" :key="r.key">
                <dt>{{ r.key }}</dt>
                <dd>{{ readingText(r) }}</dd>
              </template>
            </dl>
          </details>
          <dl class="props small">
            <dt>{{ t('devices.lastPoll') }}</dt>
            <dd :title="formatTime(d.status.last_attempt)">{{ formatAgo(d.status.last_attempt) }}</dd>
            <dt>{{ t('sources.duration') }}</dt>
            <dd>{{ formatValue(d.status.duration_ms, 'milliseconds') }}</dd>
          </dl>
          <div v-if="d.status.error" class="alert bad small">
            <strong>{{ t(`devices.errorKinds.${d.status.error_kind ?? ''}`) }}</strong> {{ d.status.error }}
          </div>
          <footer class="toolbar">
            <button type="button" class="btn small" @click="editingDevice = d">{{ t('app.edit') }}</button>
            <span class="spacer" />
            <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="removeDevice(d)">
              <Trash2 :size="14" aria-hidden="true" />
            </button>
          </footer>
        </article>
      </div>
    </section>

    <SecretsPanel :secrets="secrets" :names="names" @changed="refresh()" />

    <DeviceForm
      v-if="editingDevice"
      :device="editingDevice === 'new' ? null : editingDevice"
      :initial="newDevice"
      :secrets="secrets"
      :drivers="drivers"
      @saved="onSaved"
      @close="editingDevice = null"
    />
    <DiscoverDialog v-if="discovering" :driver="discovering" @added="refresh()" @configure="addDevice" @close="discovering = null" />

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

.devices {
  margin-bottom: 16px;
}

.devices .head h2 {
  margin: 0;
}

.devices > .muted {
  margin-top: 0;
}

/* Текущие значения устройства — главное в карточке: крупнее служебных полей. */
.readings dd {
  font-family: var(--font-display);
  font-weight: 500;
}

.raw summary {
  cursor: pointer;
  color: var(--text-muted);
}

.raw dl {
  margin-top: 6px;
}
</style>
