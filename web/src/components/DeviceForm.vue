<script setup lang="ts">
import { computed, ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, ApiError, type Device, type DeviceInput, type DeviceStatus, type JSONField, type Secret, type SecretInput, type Unit } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import { t } from '@/i18n'
import { readingLabel, readingText } from '@/lib/devices'
import { parseTuyaJSON } from '@/lib/tuya'
import { intervalErrors, isHttpUrl, isLabelKey, type Errors } from '@/lib/validate'

const props = defineProps<{ device: Device | null; secrets: Secret[] }>()
const emit = defineEmits<{ saved: [d: Device]; close: [] }>()

// Значение select учётных данных, при котором ключ вводится прямо в форме и сохраняется при сохранении устройства.
const NEW_KEY = '__new'
const units: Unit[] = ['', 'celsius', 'percent', 'ppm', 'ugm3', 'mgm3', 'count', 'bool', 'seconds', 'milliseconds', 'bytes', 'per_second']

function blank(): DeviceInput {
  return {
    name: '',
    kind: 'tuya',
    address: '',
    interval_s: 30,
    timeout_s: 5,
    labels: {},
    secret_id: null,
    enabled: true,
    tuya: { device_id: '', version: 'auto', schema: [] },
    http_json: { fields: [{ path: '', key: '', unit: '', scale: 1 }] },
  }
}

const initial = blank()
if (props.device) {
  const d = props.device
  Object.assign(initial, {
    name: d.name,
    kind: d.kind,
    address: d.address,
    interval_s: d.interval_s,
    timeout_s: d.timeout_s,
    labels: { ...d.labels },
    secret_id: d.secret_id,
    enabled: d.enabled,
  })
  if (d.tuya) initial.tuya = { ...d.tuya, schema: [...d.tuya.schema] }
  if (d.http_json) initial.http_json = { fields: d.http_json.fields.map((f) => ({ ...f })) }
}
const form = ref(initial)
const labels = ref(Object.entries(form.value.labels).map(([k, v]) => ({ k, v })))
const secretChoice = ref<string>(form.value.secret_id ?? '')
const newKey = ref('')
const pasted = ref('')
const parseNote = ref('')
const errors = ref<Errors>({})
const error = ref<unknown>(null)
const busy = ref(false)
const testing = ref(false)
const test = ref<DeviceStatus | null>(null)

const isTuya = computed(() => form.value.kind === 'tuya')
const secretOptions = computed(() => props.secrets.filter((s) => (isTuya.value ? s.kind === 'key' : s.kind !== 'key')))

function parse() {
  parseNote.value = ''
  try {
    const p = parseTuyaJSON(pasted.value)
    const tuya = form.value.tuya!
    tuya.device_id = p.deviceId
    tuya.schema = p.schema
    if (p.version !== 'auto') tuya.version = p.version
    if (!form.value.name.trim() && p.name) form.value.name = p.name
    if (p.localKey) {
      secretChoice.value = NEW_KEY
      newKey.value = p.localKey
    }
    parseNote.value = t('devices.parsed', { n: p.schema.length })
    pasted.value = ''
  } catch (e) {
    parseNote.value = t('devices.parseError', { error: e instanceof Error ? e.message : String(e) })
  }
}

function collect(): DeviceInput | null {
  const f = form.value
  const e: Errors = {}
  if (!f.name.trim()) e.name = t('validation.required')
  if (!f.address.trim()) e.address = t('validation.required')
  else if (!isTuya.value && !isHttpUrl(f.address)) e.address = t('validation.url')
  Object.assign(e, intervalErrors(f.interval_s, f.timeout_s))
  if (isTuya.value && !f.tuya!.device_id.trim()) e['tuya.device_id'] = t('validation.required')
  if (isTuya.value && f.enabled && !secretChoice.value) e.secret_id = t('validation.required')
  if (secretChoice.value === NEW_KEY && newKey.value.length !== 16) e.new_key = t('devices.keyLength')
  const out: Record<string, string> = {}
  labels.value.forEach((l, i) => {
    if (!l.k && !l.v) return
    if (!isLabelKey(l.k)) e[`labels.${i}`] = t('validation.label')
    out[l.k] = l.v
  })
  errors.value = e
  if (Object.keys(e).length) return null
  const input: DeviceInput = {
    name: f.name.trim(),
    kind: f.kind,
    address: f.address.trim(),
    interval_s: f.interval_s,
    timeout_s: f.timeout_s,
    labels: out,
    secret_id: secretChoice.value && secretChoice.value !== NEW_KEY ? secretChoice.value : null,
    enabled: f.enabled,
  }
  if (isTuya.value) input.tuya = f.tuya
  else input.http_json = { fields: f.http_json!.fields.filter((x) => x.path || x.key) }
  return input
}

function inlineSecret(): SecretInput | undefined {
  return secretChoice.value === NEW_KEY ? { name: '', kind: 'key', key: newKey.value } : undefined
}

async function runTest() {
  const input = collect()
  if (!input) return
  testing.value = true
  test.value = null
  error.value = null
  try {
    test.value = await api.devices.test({ ...input, secret: inlineSecret() })
  } catch (e) {
    error.value = e
    if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
  } finally {
    testing.value = false
  }
}

async function submit() {
  const input = collect()
  if (!input) return
  busy.value = true
  error.value = null
  let created: Secret | null = null
  try {
    // Ключ из формы сохраняется отдельными учётными данными; если устройство не сохранится, удаляем его.
    if (secretChoice.value === NEW_KEY) {
      created = await api.secrets.create({ name: t('devices.keyName', { name: input.name }), kind: 'key', key: newKey.value })
      input.secret_id = created.id
    }
    const saved = props.device
      ? await api.devices.update(props.device.id, input, props.device.revision)
      : await api.devices.create(input)
    emit('saved', saved)
  } catch (e) {
    if (created) await api.secrets.remove(created.id).catch(() => undefined)
    error.value = e
    if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
  } finally {
    busy.value = false
  }
}

function addField() {
  form.value.http_json!.fields.push({ path: '', key: '', unit: '', scale: 1 } satisfies JSONField)
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
          <select v-model="form.kind" class="input" @change="secretChoice = ''">
            <option value="tuya">{{ t('devices.kinds.tuya') }}</option>
            <option value="http_json">{{ t('devices.kinds.http_json') }}</option>
          </select>
        </label>
      </div>

      <details v-if="isTuya" class="box" :open="!device">
        <summary>{{ t('devices.pasteJSON') }}</summary>
        <p class="small muted">{{ t('devices.pasteHint') }}</p>
        <textarea v-model="pasted" class="input code" rows="5" spellcheck="false" :aria-label="t('devices.pasteJSON')" />
        <div class="toolbar paste-bar">
          <button type="button" class="btn small" :disabled="!pasted.trim()" @click="parse">{{ t('devices.parse') }}</button>
          <span v-if="parseNote" class="small" role="status">{{ parseNote }}</span>
        </div>
      </details>

      <label class="field">
        <span>{{ t('devices.address') }}</span>
        <input
          v-model="form.address"
          class="input"
          :inputmode="isTuya ? 'text' : 'url'"
          :placeholder="isTuya ? '192.168.0.235' : 'http://shelly.lan/status'"
          :aria-invalid="!!errors.address"
        />
        <span class="hint">{{ isTuya ? t('devices.addressHintTuya') : t('devices.addressHintJSON') }}</span>
        <span v-if="errors.address" class="error">{{ errors.address }}</span>
      </label>

      <div v-if="isTuya" class="row">
        <label class="field">
          <span>{{ t('devices.deviceId') }}</span>
          <input v-model="form.tuya!.device_id" class="input code" :aria-invalid="!!errors['tuya.device_id']" />
          <span v-if="errors['tuya.device_id']" class="error">{{ errors['tuya.device_id'] }}</span>
        </label>
        <label class="field">
          <span>{{ t('devices.version') }}</span>
          <select v-model="form.tuya!.version" class="input">
            <option value="auto">{{ t('devices.versionAuto') }}</option>
            <option value="3.5">3.5</option>
            <option value="3.4">3.4</option>
            <option value="3.3">3.3</option>
          </select>
        </label>
      </div>

      <div class="row">
        <label class="field">
          <span>{{ isTuya ? t('devices.secretTuya') : t('devices.secretJSON') }}</span>
          <select v-model="secretChoice" class="input" :aria-invalid="!!errors.secret_id">
            <option value="">{{ isTuya ? t('devices.noKey') : t('sources.noSecret') }}</option>
            <option v-for="s in secretOptions" :key="s.id" :value="s.id">{{ s.name }} ({{ s.mask }})</option>
            <option v-if="isTuya" :value="NEW_KEY">{{ t('devices.newKey') }}</option>
          </select>
          <span v-if="errors.secret_id" class="error">{{ errors.secret_id }}</span>
        </label>
        <label v-if="secretChoice === NEW_KEY" class="field">
          <span>local_key</span>
          <input v-model="newKey" type="password" class="input code" autocomplete="off" maxlength="16" :aria-invalid="!!errors.new_key" />
          <span class="hint">{{ t('devices.keyWillBeCreated', { name: t('devices.keyName', { name: form.name || '…' }) }) }}</span>
          <span v-if="errors.new_key" class="error">{{ errors.new_key }}</span>
        </label>
      </div>

      <fieldset v-if="isTuya" class="box">
        <legend class="small">{{ t('devices.schema') }}</legend>
        <p v-if="!form.tuya!.schema.length" class="small muted">{{ t('devices.schemaEmpty') }}</p>
        <div v-else class="table-wrap">
          <table class="table">
            <thead>
              <tr>
                <th>{{ t('devices.dp') }}</th>
                <th>{{ t('devices.code') }}</th>
                <th>{{ t('devices.unit') }}</th>
                <th>{{ t('devices.scale') }}</th>
                <th><span class="sr-only">{{ t('app.delete') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(dp, i) in form.tuya!.schema" :key="dp.dp">
                <td>{{ dp.dp }}</td>
                <td>
                  <code>{{ dp.code }}</code>
                  <span v-if="readingLabel(dp.code) !== dp.code" class="muted"> · {{ readingLabel(dp.code) }}</span>
                </td>
                <td>{{ dp.unit || (dp.range?.length ? dp.range.join(', ') : '—') }}</td>
                <td>{{ dp.scale || '' }}</td>
                <td>
                  <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="form.tuya!.schema.splice(i, 1)">
                    <Trash2 :size="14" aria-hidden="true" />
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </fieldset>

      <fieldset v-else class="box">
        <legend class="small">{{ t('devices.fields') }}</legend>
        <div v-for="(f, i) in form.http_json!.fields" :key="i" class="row field-row">
          <label class="field">
            <span>{{ t('devices.path') }}</span>
            <input v-model="f.path" class="input code" :placeholder="t('devices.pathHint')" :aria-invalid="!!errors[`http_json.fields[${i}].path`]" />
            <span v-if="errors[`http_json.fields[${i}].path`]" class="error">{{ errors[`http_json.fields[${i}].path`] }}</span>
          </label>
          <label class="field">
            <span>{{ t('devices.fieldKey') }}</span>
            <input v-model="f.key" class="input code" placeholder="power" :aria-invalid="!!errors[`http_json.fields[${i}].key`]" />
            <span v-if="errors[`http_json.fields[${i}].key`]" class="error">{{ errors[`http_json.fields[${i}].key`] }}</span>
          </label>
          <label class="field narrow">
            <span>{{ t('devices.unit') }}</span>
            <select v-model="f.unit" class="input">
              <option v-for="u in units" :key="u" :value="u">{{ t(`monitoring.units.${u}`) }}</option>
            </select>
          </label>
          <label class="field narrow">
            <span>{{ t('devices.multiplier') }}</span>
            <input v-model.number="f.scale" type="number" step="any" class="input" />
          </label>
          <button type="button" class="btn icon danger" :aria-label="t('app.delete')" @click="form.http_json!.fields.splice(i, 1)">
            <Trash2 :size="16" aria-hidden="true" />
          </button>
        </div>
        <button type="button" class="btn small" @click="addField"><Plus :size="14" aria-hidden="true" /> {{ t('devices.addField') }}</button>
      </fieldset>

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
        <div v-for="(l, i) in labels" :key="i" class="row field-row">
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

.box summary {
  cursor: pointer;
  font-weight: 500;
}

.paste-bar {
  margin-top: 8px;
}

.field-row {
  flex-wrap: nowrap;
  align-items: flex-start;
}

.field-row .field {
  margin-bottom: 6px;
}

.field-row .narrow {
  flex: 0 1 130px;
}

.field-row > .btn {
  margin-top: 26px;
}

.readings {
  margin-top: 8px;
}

@media (max-width: 640px) {
  .field-row {
    flex-wrap: wrap;
  }
}
</style>
