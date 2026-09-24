<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type Asset, type CheckInput, type Service, type ServiceInput } from '@/api'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import { t } from '@/i18n'
import { builtinIcons } from '@/lib/icons'
import { intervalErrors, isHostPort, isHttpUrl, parseTags, type Errors } from '@/lib/validate'
import { store } from '@/stores/app'

const props = defineProps<{ service: Service | null }>()
const emit = defineEmits<{ saved: []; close: [] }>()

const existingCheck = computed(() => store.checks.find((c) => c.id === props.service?.check_id) ?? null)

const form = ref<ServiceInput>({
  name: props.service?.name ?? '',
  description: props.service?.description ?? '',
  url: props.service?.url ?? '',
  icon: props.service?.icon ?? 'builtin:server',
  tags: props.service?.tags ?? [],
  open_mode: props.service?.open_mode ?? 'new_tab',
  source_id: props.service?.source_id ?? null,
})
const tagsText = ref(form.value.tags.join(', '))
const withCheck = ref(!!existingCheck.value)
const check = ref<Omit<CheckInput, 'service_id'>>(
  existingCheck.value
    ? {
        kind: existingCheck.value.kind,
        target: existingCheck.value.target,
        expected_status: existingCheck.value.expected_status,
        interval_s: existingCheck.value.interval_s,
        timeout_s: existingCheck.value.timeout_s,
        enabled: existingCheck.value.enabled,
        ca_pem: existingCheck.value.ca_pem ?? '',
      }
    : { kind: 'http', target: '', expected_status: '200-399', interval_s: 30, timeout_s: 5, enabled: true, ca_pem: '' },
)
const assets = ref<Asset[]>([])
const errors = ref<Errors>({})
const error = ref<unknown>(null)
const busy = ref(false)

const iconKind = computed({
  get: () =>
    form.value.icon === 'favicon' ? 'favicon' : form.value.icon.startsWith('asset:') ? 'asset' : form.value.icon.startsWith('builtin:') ? 'builtin' : 'none',
  set: (k: string) => {
    form.value.icon = k === 'builtin' ? 'builtin:server' : k === 'asset' ? `asset:${assets.value[0]?.id ?? ''}` : k === 'favicon' ? 'favicon' : ''
  },
})

onMounted(async () => {
  try {
    assets.value = await api.assets.list()
  } catch {
    assets.value = []
  }
})

function useUrlAsTarget() {
  if (!check.value.target && isHttpUrl(form.value.url)) check.value.target = form.value.url
}

function validate(): boolean {
  const e: Errors = {}
  if (!form.value.name.trim()) e.name = t('validation.required')
  if (!isHttpUrl(form.value.url)) e.url = t('validation.url')
  if (withCheck.value) {
    if (check.value.kind === 'http' && !isHttpUrl(check.value.target)) e['check.target'] = t('validation.url')
    if (check.value.kind === 'tcp' && !isHostPort(check.value.target)) e['check.target'] = t('validation.hostPort')
    Object.assign(e, intervalErrors(check.value.interval_s, check.value.timeout_s, 'check.'))
  }
  errors.value = e
  return !Object.keys(e).length
}

async function submit() {
  form.value.tags = parseTags(tagsText.value)
  if (!validate()) return
  busy.value = true
  error.value = null
  try {
    const input = { ...form.value, name: form.value.name.trim() }
    const svc = props.service
      ? await api.services.update(props.service.id, input, props.service.revision)
      : await api.services.create(input)
    const c = existingCheck.value
    if (withCheck.value) {
      const body: CheckInput = { ...check.value, service_id: svc.id, target: check.value.target.trim() }
      if (c) await api.checks.update(c.id, body, c.revision)
      else await api.checks.create(body)
    } else if (c) {
      await api.checks.remove(c.id)
    }
    emit('saved')
  } catch (e) {
    error.value = e
    if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="service ? service.name : t('settings.newService')" wide @close="emit('close')">
    <ApiErrorAlert :error="error" />
    <form id="service-form" @submit.prevent="submit">
      <div class="row">
        <label class="field">
          <span>{{ t('settings.serviceName') }}</span>
          <input v-model="form.name" class="input" maxlength="100" :aria-invalid="!!errors.name" />
          <span v-if="errors.name" class="error">{{ errors.name }}</span>
        </label>
        <label class="field">
          <span>{{ t('settings.url') }}</span>
          <input v-model="form.url" class="input" inputmode="url" placeholder="http://nas.lan:5000" :aria-invalid="!!errors.url" @blur="useUrlAsTarget" />
          <span v-if="errors.url" class="error">{{ errors.url }}</span>
        </label>
      </div>
      <label class="field">
        <span>{{ t('settings.description') }}</span>
        <input v-model="form.description" class="input" maxlength="500" />
      </label>
      <div class="row">
        <label class="field">
          <span>{{ t('settings.tags') }}</span>
          <input v-model="tagsText" class="input" aria-describedby="tags-hint" />
          <span id="tags-hint" class="hint">{{ t('settings.tagsHint') }}</span>
        </label>
        <label class="field">
          <span>{{ t('settings.openMode') }}</span>
          <select v-model="form.open_mode" class="input">
            <option value="new_tab">{{ t('settings.openModes.new_tab') }}</option>
            <option value="same_tab">{{ t('settings.openModes.same_tab') }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('settings.metricsSource') }}</span>
          <select v-model="form.source_id" class="input">
            <option :value="null">—</option>
            <option v-for="s in store.sources.filter((x) => !x.system)" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </label>
      </div>
      <fieldset class="box">
        <legend class="small">{{ t('settings.icon') }}</legend>
        <div class="row icon-row">
          <ServiceIcon :icon="form.icon" :size="32" />
          <label class="field">
            <span class="sr-only">{{ t('settings.icon') }}</span>
            <select v-model="iconKind" class="input">
              <option value="none">{{ t('settings.iconNone') }}</option>
              <option value="favicon">{{ t('settings.iconFavicon') }}</option>
              <option value="builtin">{{ t('settings.iconBuiltin') }}</option>
              <option value="asset" :disabled="!assets.length">{{ t('settings.iconAsset') }}</option>
            </select>
          </label>
          <span v-if="iconKind === 'favicon'" class="hint">{{ t('settings.iconFaviconHint') }}</span>
          <label v-if="iconKind === 'builtin'" class="field">
            <span class="sr-only">{{ t('settings.iconBuiltin') }}</span>
            <select v-model="form.icon" class="input">
              <option v-for="name in Object.keys(builtinIcons)" :key="name" :value="`builtin:${name}`">{{ name }}</option>
            </select>
          </label>
          <label v-if="iconKind === 'asset'" class="field">
            <span class="sr-only">{{ t('settings.iconAsset') }}</span>
            <select v-model="form.icon" class="input">
              <option v-for="a in assets" :key="a.id" :value="`asset:${a.id}`">{{ a.id }} · {{ a.width }}×{{ a.height }}</option>
            </select>
          </label>
        </div>
      </fieldset>
      <fieldset class="box">
        <legend class="small">{{ t('settings.check') }}</legend>
        <label class="field check"><input v-model="withCheck" type="checkbox" /> {{ t('settings.check') }}</label>
        <template v-if="withCheck">
          <div class="row">
            <label class="field">
              <span>{{ t('settings.checkKind') }}</span>
              <select v-model="check.kind" class="input">
                <option value="http">{{ t('settings.checkKinds.http') }}</option>
                <option value="tcp">{{ t('settings.checkKinds.tcp') }}</option>
              </select>
            </label>
            <label class="field grow">
              <span>{{ t('settings.checkTarget') }}</span>
              <input v-model="check.target" class="input" :aria-invalid="!!errors['check.target']" aria-describedby="target-hint" />
              <span id="target-hint" class="hint">{{ t('settings.checkTargetHint') }}</span>
              <span v-if="errors['check.target']" class="error">{{ errors['check.target'] }}</span>
            </label>
          </div>
          <div class="row">
            <label v-if="check.kind === 'http'" class="field">
              <span>{{ t('settings.expectedStatus') }}</span>
              <input v-model="check.expected_status" class="input code" />
            </label>
            <label class="field">
              <span>{{ t('settings.interval') }}</span>
              <input v-model.number="check.interval_s" type="number" min="5" max="3600" class="input" :aria-invalid="!!errors['check.interval_s']" />
              <span v-if="errors['check.interval_s']" class="error">{{ errors['check.interval_s'] }}</span>
            </label>
            <label class="field">
              <span>{{ t('settings.timeout') }}</span>
              <input v-model.number="check.timeout_s" type="number" min="1" class="input" :aria-invalid="!!errors['check.timeout_s']" />
              <span v-if="errors['check.timeout_s']" class="error">{{ errors['check.timeout_s'] }}</span>
            </label>
          </div>
          <details v-if="check.kind === 'http'">
            <summary class="small">{{ t('settings.caPem') }}</summary>
            <textarea v-model="check.ca_pem" class="input code" rows="4" spellcheck="false" />
          </details>
          <label class="field check"><input v-model="check.enabled" type="checkbox" /> {{ t('settings.checkEnabled') }}</label>
        </template>
      </fieldset>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">{{ t('app.cancel') }}</button>
      <button type="submit" form="service-form" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
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

.icon-row {
  align-items: center;
}

.icon-row .field {
  margin: 0;
}

.grow {
  flex: 3 1 240px !important;
}
</style>
