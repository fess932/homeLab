<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, ApiError, type Device, type DeviceInput, type DeviceStatus, type DriverInfo, type Secret, type SecretInput } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import { driverUI } from '@/drivers'
import { t } from '@/i18n'
import { readingLabel, readingText } from '@/lib/devices'
import { intervalErrors, isLabelKey, type Errors } from '@/lib/validate'

const props = defineProps<{
  device: Device | null
  secrets: Secret[]
  drivers: DriverInfo[]
  /** Заготовка нового устройства, например из поиска в сети. */
  initial?: Partial<DeviceInput>
}>()
const emit = defineEmits<{ saved: [d: Device]; close: [] }>()

// Значение select учётных данных, при котором ключ вводится прямо в форме и
// сохраняется сервером вместе с устройством.
const NEW_KEY = '__new'

const src = props.device ?? props.initial
const form = ref<DeviceInput>({
  name: src?.name ?? '',
  kind: src?.kind ?? props.drivers[0]?.kind ?? 'tuya',
  address: src?.address ?? '',
  interval_s: src?.interval_s ?? 30,
  timeout_s: src?.timeout_s ?? 5,
  labels: { ...(src?.labels ?? {}) },
  secret_id: src?.secret_id ?? null,
  enabled: src?.enabled ?? true,
  // Копия через JSON: structuredClone не умеет реактивные объекты Vue.
  config: JSON.parse(JSON.stringify(src?.config ?? {})),
})
if (!Object.keys(form.value.config).length) form.value.config = { ...(driverUI(form.value.kind)?.defaults() ?? {}) }

const labels = ref(Object.entries(form.value.labels).map(([k, v]) => ({ k, v })))
const secretChoice = ref<string>(form.value.secret_id ?? '')
const newKey = ref('')
const errors = ref<Errors>({})
const error = ref<unknown>(null)
const busy = ref(false)
const testing = ref(false)
const test = ref<DeviceStatus | null>(null)

const info = computed(() => props.drivers.find((d) => d.kind === form.value.kind))
const ui = computed(() => driverUI(form.value.kind))
const takesKey = computed(() => info.value?.secret_kinds.includes('key') ?? false)
const secretOptions = computed(() => props.secrets.filter((s) => info.value?.secret_kinds.includes(s.kind)))

// Другой драйвер — другие настройки и другие учётные данные.
watch(
  () => form.value.kind,
  (kind, prev) => {
    if (!prev || kind === prev) return
    form.value.config = { ...(driverUI(kind)?.defaults() ?? {}) }
    secretChoice.value = ''
    test.value = null
  },
)

function onParsed(p: { name: string; key: string }) {
  if (!form.value.name.trim() && p.name) form.value.name = p.name
  if (p.key) {
    secretChoice.value = NEW_KEY
    newKey.value = p.key
  }
}

function collect(): (DeviceInput & { secret?: SecretInput }) | null {
  const f = form.value
  const e: Errors = {}
  if (!f.name.trim()) e.name = t('validation.required')
  if (!f.address.trim()) e.address = t('validation.required')
  Object.assign(e, intervalErrors(f.interval_s, f.timeout_s))
  if (info.value?.secret_required && f.enabled && !secretChoice.value) e.secret_id = t('validation.required')
  if (secretChoice.value === NEW_KEY && !newKey.value) e.new_key = t('validation.required')
  const out: Record<string, string> = {}
  labels.value.forEach((l, i) => {
    if (!l.k && !l.v) return
    if (!isLabelKey(l.k)) e[`labels.${i}`] = t('validation.label')
    out[l.k] = l.v
  })
  errors.value = e
  if (Object.keys(e).length) return null
  const input: DeviceInput & { secret?: SecretInput } = {
    ...f,
    name: f.name.trim(),
    address: f.address.trim(),
    labels: out,
    secret_id: secretChoice.value && secretChoice.value !== NEW_KEY ? secretChoice.value : null,
  }
  if (secretChoice.value === NEW_KEY) input.secret = { name: t('devices.keyName', { name: input.name }), kind: 'key', key: newKey.value }
  return input
}

function fail(e: unknown) {
  error.value = e
  if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
}

async function runTest() {
  const input = collect()
  if (!input) return
  testing.value = true
  test.value = null
  error.value = null
  try {
    test.value = await api.devices.test(input)
  } catch (e) {
    fail(e)
  } finally {
    testing.value = false
  }
}

async function submit() {
  const input = collect()
  if (!input) return
  busy.value = true
  error.value = null
  try {
    const saved = props.device
      ? await api.devices.updateWith(props.device.id, input, props.device.revision)
      : await api.devices.createWith(input)
    emit('saved', saved)
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="device ? device.name : t('devices.add')" wide @close="emit('close')">
    <ApiErrorAlert :error="error" />
    <form id="device-form" autocomplete="off" @submit.prevent="submit">
      <div class="row">
        <label class="field">
          <span>{{ t('sources.name') }}</span>
          <input v-model="form.name" class="input" maxlength="100" :aria-invalid="!!errors.name" />
          <span v-if="errors.name" class="error">{{ errors.name }}</span>
        </label>
        <label class="field">
          <span>{{ t('sources.kind') }}</span>
          <select v-model="form.kind" class="input" :disabled="!!device">
            <option v-for="d in drivers" :key="d.kind" :value="d.kind">{{ d.title }}</option>
          </select>
        </label>
      </div>

      <label class="field">
        <span>{{ t('devices.address') }}</span>
        <input v-model="form.address" class="input" :placeholder="ui?.addressPlaceholder" :aria-invalid="!!errors.address" />
        <span v-if="ui" class="hint">{{ t(ui.addressHint) }}</span>
        <span v-if="errors.address" class="error">{{ errors.address }}</span>
      </label>

      <component
        :is="ui.form"
        v-if="ui"
        v-model="form.config"
        :errors="errors"
        :editing="!!device"
        @parsed="onParsed"
      />
      <p v-if="errors.config" class="error small">{{ errors.config }}</p>

      <div v-if="info?.secret_kinds.length" class="row">
        <label class="field">
          <span>{{ takesKey ? t('devices.secretTuya') : t('devices.secretJSON') }}</span>
          <select v-model="secretChoice" class="input" :aria-invalid="!!errors.secret_id">
            <option value="">{{ takesKey ? t('devices.noKey') : t('sources.noSecret') }}</option>
            <option v-for="s in secretOptions" :key="s.id" :value="s.id">{{ s.name }} ({{ s.mask }})</option>
            <option v-if="takesKey" :value="NEW_KEY">{{ t('devices.newKey') }}</option>
          </select>
          <span v-if="errors.secret_id" class="error">{{ errors.secret_id }}</span>
        </label>
        <label v-if="secretChoice === NEW_KEY" class="field">
          <span>{{ t('sources.key') }}</span>
          <input v-model="newKey" type="password" class="input code" autocomplete="off" maxlength="256" :aria-invalid="!!(errors.new_key || errors['secret.key'])" />
          <span class="hint">{{ t('devices.keyWillBeCreated', { name: t('devices.keyName', { name: form.name || '…' }) }) }}</span>
          <span v-if="errors.new_key || errors['secret.key']" class="error">{{ errors.new_key || errors['secret.key'] }}</span>
        </label>
      </div>

      <div class="row">
        <label class="field">
          <span>{{ t('sources.interval') }}</span>
          <input v-model.number="form.interval_s" type="number" min="5" max="3600" class="input" :aria-invalid="!!errors.interval_s" />
          <span v-if="errors.interval_s" class="error">{{ errors.interval_s }}</span>
        </label>
        <label class="field">
          <span>{{ t('sources.timeout') }}</span>
          <input v-model.number="form.timeout_s" type="number" min="1" class="input" :aria-invalid="!!errors.timeout_s" />
          <span class="hint">{{ t('sources.timeoutHint') }}</span>
          <span v-if="errors.timeout_s" class="error">{{ errors.timeout_s }}</span>
        </label>
      </div>

      <fieldset class="box">
        <legend class="small">{{ t('sources.labels') }}</legend>
        <div v-for="(l, i) in labels" :key="i" class="row label-row">
          <label class="field">
            <span class="sr-only">{{ t('sources.labelKey') }}</span>
            <input v-model="l.k" class="input code" :placeholder="t('sources.labelKey')" :aria-invalid="!!errors[`labels.${i}`]" />
            <span v-if="errors[`labels.${i}`]" class="error">{{ errors[`labels.${i}`] }}</span>
          </label>
          <label class="field">
            <span class="sr-only">{{ t('sources.labelValue') }}</span>
            <input v-model="l.v" class="input" :placeholder="t('sources.labelValue')" />
          </label>
          <button type="button" class="btn icon danger" :aria-label="t('app.delete')" @click="labels.splice(i, 1)">
            <Trash2 :size="16" aria-hidden="true" />
          </button>
        </div>
        <button type="button" class="btn small" @click="labels.push({ k: '', v: '' })">
          <Plus :size="14" aria-hidden="true" /> {{ t('sources.addLabel') }}
        </button>
      </fieldset>
      <label class="field check"><input v-model="form.enabled" type="checkbox" /> {{ t('sources.enabled') }}</label>
    </form>

    <div v-if="test" class="alert" :class="test.state === 'up' ? 'ok' : 'bad'" role="status">
      <template v-if="test.state === 'up'">
        <strong>{{ t('devices.testOk', { ms: Math.round(test.duration_ms ?? 0) }) }}</strong>
        <span v-if="test.protocol" class="muted"> · {{ t('devices.protocol', { v: test.protocol }) }}</span>
        <dl v-if="test.readings.length" class="props small readings">
          <template v-for="r in test.readings" :key="r.key">
            <dt>{{ readingLabel(r.key) }}</dt>
            <dd>{{ readingText(r) }}</dd>
          </template>
        </dl>
        <div v-else class="small">{{ t('devices.noReadings') }}</div>
      </template>
      <template v-else>
        <strong>{{ t(`devices.errorKinds.${test.error_kind ?? ''}`) }}</strong>
        <div class="small">{{ test.error }}</div>
      </template>
    </div>
    <template #footer>
      <button type="button" class="btn" :disabled="testing" @click="runTest">
        {{ testing ? t('devices.testing') : t('devices.test') }}
      </button>
      <span class="spacer" />
      <button type="button" class="btn" @click="emit('close')">{{ t('app.cancel') }}</button>
      <button type="submit" form="device-form" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.box {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 12px;
}

.label-row {
  flex-wrap: nowrap;
  align-items: flex-start;
}

.label-row .field {
  margin-bottom: 6px;
}

.readings {
  margin-top: 8px;
}
</style>
