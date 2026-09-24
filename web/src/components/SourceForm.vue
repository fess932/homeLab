<script setup lang="ts">
import { computed, ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, ApiError, type Secret, type Source, type SourceInput, type SourceTestResult } from '@/api'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { formatBytes } from '@/lib/format'
import { intervalErrors, isHttpUrl, isLabelKey, type Errors } from '@/lib/validate'

const props = defineProps<{ source: Source | null; secrets: Secret[] }>()
const emit = defineEmits<{ saved: [s: Source]; close: [] }>()

const blank = (): SourceInput => ({
  name: '',
  kind: 'node_exporter',
  url: '',
  interval_s: 15,
  timeout_s: 5,
  labels: {},
  secret_id: null,
  tls: { ca_pem: '', server_name: '' },
  enabled: true,
})

const form = ref<SourceInput>(
  props.source
    ? {
        name: props.source.name,
        kind: props.source.kind,
        url: props.source.url,
        interval_s: props.source.interval_s,
        timeout_s: props.source.timeout_s,
        labels: { ...props.source.labels },
        secret_id: props.source.secret_id,
        tls: { ca_pem: props.source.tls?.ca_pem ?? '', server_name: props.source.tls?.server_name ?? '' },
        enabled: props.source.enabled,
      }
    : blank(),
)
const labels = ref(Object.entries(form.value.labels).map(([k, v]) => ({ k, v })))
const errors = ref<Errors>({})
const error = ref<unknown>(null)
const busy = ref(false)
const testing = ref(false)
const test = ref<SourceTestResult | null>(null)
const readonly = computed(() => !!props.source?.system)

function collect(): SourceInput | null {
  const e: Errors = {}
  if (!form.value.name.trim()) e.name = t('validation.required')
  if (!isHttpUrl(form.value.url)) e.url = t('validation.url')
  Object.assign(e, intervalErrors(form.value.interval_s, form.value.timeout_s))
  const out: Record<string, string> = {}
  labels.value.forEach((l, i) => {
    if (!l.k && !l.v) return
    if (!isLabelKey(l.k)) e[`labels.${i}`] = t('validation.label')
    out[l.k] = l.v
  })
  errors.value = e
  if (Object.keys(e).length) return null
  return { ...form.value, name: form.value.name.trim(), labels: out }
}

async function runTest() {
  const input = collect()
  if (!input) return
  testing.value = true
  test.value = null
  error.value = null
  try {
    test.value = await api.sources.testDraft(input)
  } catch (e) {
    error.value = e
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
    const saved = props.source
      ? await api.sources.update(props.source.id, input, props.source.revision)
      : await api.sources.create(input)
    emit('saved', saved)
  } catch (e) {
    error.value = e
    if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="source ? source.name : t('sources.add')" wide @close="emit('close')">
    <p v-if="readonly" class="alert">{{ t('sources.systemHint') }}</p>
    <ApiErrorAlert :error="error" />
    <form id="source-form" @submit.prevent="submit">
      <fieldset :disabled="readonly" class="plain">
        <div class="row">
          <label class="field">
            <span>{{ t('sources.name') }}</span>
            <input v-model="form.name" class="input" maxlength="100" :aria-invalid="!!errors.name" />
            <span v-if="errors.name" class="error">{{ errors.name }}</span>
          </label>
          <label class="field">
            <span>{{ t('sources.kind') }}</span>
            <select v-model="form.kind" class="input">
              <option value="node_exporter">{{ t('sources.kinds.node_exporter') }}</option>
              <option value="cadvisor">{{ t('sources.kinds.cadvisor') }}</option>
              <option value="prometheus">{{ t('sources.kinds.prometheus') }}</option>
            </select>
          </label>
        </div>
        <label class="field">
          <span>{{ t('sources.url') }}</span>
          <input v-model="form.url" class="input" inputmode="url" :aria-invalid="!!errors.url" :placeholder="t('sources.urlHint')" />
          <span v-if="errors.url" class="error">{{ errors.url }}</span>
        </label>
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
          <label class="field">
            <span>{{ t('sources.secret') }}</span>
            <select v-model="form.secret_id" class="input">
              <option :value="null">{{ t('sources.noSecret') }}</option>
              <option v-for="s in secrets" :key="s.id" :value="s.id">{{ s.name }} ({{ s.mask }})</option>
            </select>
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
        <details class="box">
          <summary>TLS</summary>
          <label class="field">
            <span>{{ t('sources.serverName') }}</span>
            <input v-model="form.tls.server_name" class="input" />
          </label>
          <label class="field">
            <span>{{ t('sources.caPem') }}</span>
            <textarea v-model="form.tls.ca_pem" class="input code" rows="4" spellcheck="false" />
          </label>
        </details>
        <label class="field check"><input v-model="form.enabled" type="checkbox" /> {{ t('sources.enabled') }}</label>
      </fieldset>
    </form>
    <div v-if="test" class="alert" :class="test.ok ? 'ok' : 'bad'" role="status">
      <template v-if="test.ok">
        {{ t('sources.testOk', { samples: test.samples, bytes: formatBytes(test.bytes), ms: Math.round(test.duration_ms) }) }}
      </template>
      <template v-else>
        <strong>{{ t(`sources.errorKinds.${test.error_kind}`) || t('sources.testFail') }}</strong>
        <span v-if="test.http_status"> (HTTP {{ test.http_status }})</span>
        <div class="small">{{ test.error }}</div>
      </template>
    </div>
    <template #footer>
      <button v-if="!readonly" type="button" class="btn" :disabled="testing" @click="runTest">
        {{ testing ? t('sources.testing') : t('sources.test') }}
      </button>
      <span class="spacer" />
      <button type="button" class="btn" @click="emit('close')">{{ readonly ? t('app.close') : t('app.cancel') }}</button>
      <button v-if="!readonly" type="submit" form="source-form" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.plain {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}

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
</style>
