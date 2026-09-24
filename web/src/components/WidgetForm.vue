<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError, type Group, type MetricRef, type RangeName, type Widget, type WidgetConfigMap } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import MetricPicker from '@/components/MetricPicker.vue'
import { t } from '@/i18n'
import { MARKDOWN_LIMIT } from '@/lib/markdown'
import { rangeNames } from '@/lib/time'
import { isHttpUrl, type Errors } from '@/lib/validate'
import { loadChecks, loadServices, store } from '@/stores/app'

const props = defineProps<{ widget: Widget; groups: Group[] }>()
const emit = defineEmits<{ save: [w: Widget]; close: [] }>()

const draft = ref<Widget>(JSON.parse(JSON.stringify(props.widget)) as Widget)
const cfg = computed(() => draft.value.config as unknown as Record<string, string>)
const typed = <K extends keyof WidgetConfigMap>() => draft.value.config as WidgetConfigMap[K]

const zones = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
  } catch {
    return []
  }
})()

const linksCfg = computed(() => typed<'links'>())

const linkMetric = computed<MetricRef>({
  get: () => typed<'link'>().metric ?? { preset_id: '', vars: {} },
  set: (m) => (typed<'link'>().metric = m.preset_id ? m : null),
})
const metric = computed<MetricRef>({
  get: () => typed<'chart'>().metric,
  set: (m) => (typed<'chart'>().metric = m),
})

function toggleService(id: string, on: boolean) {
  const c = typed<'links'>()
  c.service_ids = on ? [...c.service_ids, id] : c.service_ids.filter((x) => x !== id)
}

function moveService(idx: number, delta: number) {
  const list = [...typed<'links'>().service_ids]
  const j = idx + delta
  if (j < 0 || j >= list.length) return
  ;[list[idx], list[j]] = [list[j]!, list[idx]!]
  typed<'links'>().service_ids = list
}

const serviceName = (id: string) => store.services.find((s) => s.id === id)?.name ?? id

// «Своя ссылка»: с проверкой — создаются сервис и HTTP-проверка раз в 5 минут,
// без проверки — адрес и название хранятся в самом виджете.
const initialLink = props.widget.type === 'link' ? (props.widget.config as WidgetConfigMap['link']) : null
const custom = ref({ name: initialLink?.title ?? '', url: initialLink?.url ?? '', ping: !initialLink?.url })
const errors = ref<Errors>({})
const error = ref<unknown>(null)
const busy = ref(false)
const isCustomLink = computed(() => draft.value.type === 'link' && !typed<'link'>().service_id)

function withScheme(s: string): string {
  const v = s.trim()
  return v && !/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? `http://${v}` : v
}

async function createCustomLink(): Promise<boolean> {
  const url = withScheme(custom.value.url)
  custom.value.url = url
  if (!isHttpUrl(url)) {
    errors.value = { url: t('validation.url') }
    return false
  }
  errors.value = {}
  const c = typed<'link'>()
  if (!custom.value.ping) {
    // Новый адрес — сервер заново заберёт иконку сайта при сохранении страницы.
    if (url !== c.url || !c.icon) c.icon = 'favicon'
    c.url = url
    c.title = custom.value.name.trim()
    return true
  }
  const svc = await api.services.create({
    name: custom.value.name.trim() || new URL(url).host,
    description: '',
    url,
    icon: 'favicon',
    tags: [],
    open_mode: 'new_tab',
    source_id: null,
  })
  await api.checks.create({ service_id: svc.id, kind: 'http', target: url, expected_status: '200-399', interval_s: 300, timeout_s: 10, enabled: true, ca_pem: '' })
  c.service_id = svc.id
  c.show_status = true
  delete c.url
  delete c.title
  delete c.icon
  await Promise.all([loadServices(), loadChecks()])
  return true
}

async function submit() {
  if (isCustomLink.value) {
    busy.value = true
    error.value = null
    try {
      if (!(await createCustomLink())) return
    } catch (e) {
      error.value = e
      if (e instanceof ApiError) errors.value = { ...errors.value, ...e.fieldErrors() }
      return
    } finally {
      busy.value = false
    }
  } else if (draft.value.type === 'link') {
    delete typed<'link'>().url
    delete typed<'link'>().title
    delete typed<'link'>().icon
  }
  emit('save', draft.value)
}
</script>

<template>
  <ModalDialog :title="`${t('editor.widgetSettings')}: ${t(`widgets.types.${draft.type}`)}`" @close="emit('close')">
    <form id="widget-form" @submit.prevent="submit">
      <ApiErrorAlert :error="error" />
      <label class="field">
        <span>{{ t('editor.groupTitle') }}</span>
        <select v-model="draft.group_id" class="input">
          <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.title }}</option>
        </select>
      </label>

      <template v-if="draft.type === 'status'">
        <label class="field">
          <span>{{ t('widgets.service') }}</span>
          <select v-model="cfg.service_id" class="input" required>
            <option value="" disabled>—</option>
            <option v-for="s in store.services" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </label>
      </template>

      <template v-if="draft.type === 'link'">
        <label class="field">
          <span>{{ t('widgets.service') }}</span>
          <select v-model="cfg.service_id" class="input">
            <option value="">{{ t('widgets.customLink') }}</option>
            <option v-for="s in store.services" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </label>
        <template v-if="isCustomLink">
          <label class="field">
            <span>{{ t('widgets.linkUrl') }}</span>
            <input v-model="custom.url" class="input" inputmode="url" required placeholder="http://nas.lan:5000" :aria-invalid="!!errors.url" />
            <span v-if="errors.url" class="error">{{ errors.url }}</span>
          </label>
          <label class="field">
            <span>{{ t('widgets.linkName') }}</span>
            <input v-model="custom.name" class="input" maxlength="100" :placeholder="t('widgets.linkNameHint')" />
          </label>
          <label class="field check"><input v-model="custom.ping" type="checkbox" /> {{ t('widgets.ping') }}</label>
        </template>
        <label v-else class="field check"><input v-model="cfg.show_status" type="checkbox" /> {{ t('widgets.showStatus') }}</label>
        <label class="field check"><input v-model="cfg.show_latency" type="checkbox" /> {{ t('widgets.showLatency') }}</label>
        <MetricPicker v-model="linkMetric" optional />
      </template>

      <template v-if="draft.type === 'links'">
        <label class="field">
          <span>{{ t('widgets.title') }}</span>
          <input v-model="cfg.title" class="input" maxlength="100" />
        </label>
        <fieldset class="field">
          <legend class="small">{{ t('widgets.services') }}</legend>
          <ol class="ordered">
            <li v-for="(id, i) in linksCfg.service_ids" :key="id">
              <span>{{ serviceName(id) }}</span>
              <button type="button" class="btn small" :aria-label="t('editor.moveUp')" @click="moveService(i, -1)">↑</button>
              <button type="button" class="btn small" :aria-label="t('editor.moveDown')" @click="moveService(i, 1)">↓</button>
            </li>
          </ol>
          <label v-for="s in store.services" :key="s.id" class="field check">
            <input
              type="checkbox"
              :checked="linksCfg.service_ids.includes(s.id)"
              @change="toggleService(s.id, ($event.target as HTMLInputElement).checked)"
            />
            {{ s.name }}
          </label>
        </fieldset>
      </template>

      <template v-if="draft.type === 'clock'">
        <label class="field">
          <span>{{ t('widgets.timezone') }}</span>
          <input v-model="cfg.timezone" class="input" list="tz-list" :placeholder="t('widgets.timezoneLocal')" />
          <datalist id="tz-list">
            <option v-for="z in zones" :key="z" :value="z" />
          </datalist>
        </label>
        <label class="field check"><input v-model="cfg.hour12" type="checkbox" /> {{ t('widgets.hour12') }}</label>
        <label class="field check"><input v-model="cfg.show_date" type="checkbox" /> {{ t('widgets.showDate') }}</label>
        <label class="field check"><input v-model="cfg.show_seconds" type="checkbox" /> {{ t('widgets.showSeconds') }}</label>
      </template>

      <template v-if="draft.type === 'note'">
        <label class="field">
          <span>{{ t('widgets.markdown') }}</span>
          <textarea v-model="cfg.markdown" class="input" rows="8" :maxlength="MARKDOWN_LIMIT" />
        </label>
      </template>

      <template v-if="draft.type === 'number' || draft.type === 'chart'">
        <label class="field">
          <span>{{ t('widgets.title') }}</span>
          <input v-model="cfg.title" class="input" maxlength="100" />
        </label>
        <MetricPicker v-model="metric" />
      </template>

      <label v-if="draft.type === 'number'" class="field">
        <span>{{ t('widgets.decimals') }}</span>
        <input v-model.number="cfg.decimals" type="number" min="0" max="6" class="input" />
      </label>

      <template v-if="draft.type === 'chart'">
        <label class="field">
          <span>{{ t('widgets.range') }}</span>
          <select v-model="cfg.range" class="input">
            <option v-for="r in rangeNames" :key="r" :value="r as RangeName">{{ t(`time.ranges.${r}`) }}</option>
          </select>
        </label>
        <label class="field check"><input v-model="cfg.stacked" type="checkbox" /> {{ t('widgets.stacked') }}</label>
      </template>

    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">{{ t('app.cancel') }}</button>
      <button type="submit" form="widget-form" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
fieldset.field {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
}

.ordered {
  margin: 0 0 8px;
  padding-left: 1.2em;
}

.ordered li {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-bottom: 2px;
}

.ordered li span {
  flex: 1;
}
</style>
