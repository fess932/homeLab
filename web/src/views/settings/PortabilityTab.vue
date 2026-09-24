<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type ConfigRevision, type ImportFormat, type ImportPreview } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { formatTime } from '@/lib/format'
import { resetStore, store } from '@/stores/app'

const format = ref<ImportFormat>('homedeck')
const file = ref<File | null>(null)
const assets = ref<File | null>(null)
const preview = ref<ImportPreview | null>(null)
const result = ref<ImportPreview | null>(null)
const revisions = ref<ConfigRevision[]>([])
const error = ref<unknown>(null)
const busy = ref(false)

async function loadRevisions() {
  try {
    revisions.value = await api.revisions()
  } catch {
    revisions.value = []
  }
}

onMounted(loadRevisions)

function pick(e: Event, target: 'file' | 'assets') {
  const f = (e.target as HTMLInputElement).files?.[0] ?? null
  if (target === 'file') file.value = f
  else assets.value = f
  preview.value = null
  result.value = null
}

async function runPreview() {
  if (!file.value) return
  busy.value = true
  error.value = null
  result.value = null
  try {
    preview.value = await api.importPreview(format.value, file.value, assets.value ?? undefined)
  } catch (e) {
    error.value = e
    preview.value = null
  } finally {
    busy.value = false
  }
}

async function apply() {
  if (!preview.value) return
  busy.value = true
  error.value = null
  try {
    result.value = await api.importApply(preview.value.token)
    preview.value = null
    const session = store.session
    resetStore()
    store.session = session
    await loadRevisions()
  } catch (e) {
    error.value = e
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card">
    <h2>{{ t('settings.import') }}</h2>
    <ApiErrorAlert :error="error" />
    <form @submit.prevent="runPreview">
      <div class="row">
        <label class="field">
          <span>{{ t('settings.importFormat') }}</span>
          <select v-model="format" class="input" @change="preview = null">
            <option value="homedeck">{{ t('settings.formats.homedeck') }}</option>
            <option value="homer">{{ t('settings.formats.homer') }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('settings.importFile') }}</span>
          <input type="file" class="input" accept=".yml,.yaml,application/yaml,text/yaml" required @change="pick($event, 'file')" />
        </label>
        <label v-if="format === 'homer'" class="field">
          <span>{{ t('settings.importAssets') }}</span>
          <input type="file" class="input" accept=".zip,application/zip" @change="pick($event, 'assets')" />
        </label>
      </div>
      <button type="submit" class="btn" :disabled="busy || !file">{{ t('settings.importPreview') }}</button>
    </form>

    <div v-if="preview" class="preview">
      <div class="alert warn">{{ t('settings.importWarning') }}</div>
      <h3>{{ t('settings.importChanges') }}</h3>
      <ul class="changes">
        <li v-for="(c, i) in preview.changes" :key="i">
          <span class="tag">{{ t(`settings.actions.${c.action}`) }}</span>
          {{ t(`settings.entities.${c.entity}`) }}: <strong>{{ c.name }}</strong>
        </li>
      </ul>
      <h3>{{ t('settings.importWarnings') }}</h3>
      <p v-if="!preview.warnings.length" class="muted">{{ t('settings.importNoWarnings') }}</p>
      <ul v-else class="warnings">
        <li v-for="(w, i) in preview.warnings" :key="i"><code>{{ w.path }}</code> — {{ w.message }}</li>
      </ul>
      <button type="button" class="btn primary" :disabled="busy" @click="apply">{{ t('settings.importApply') }}</button>
    </div>
    <div v-if="result" class="alert ok" role="status">
      {{ t('settings.importDone', { id: result.revision_id ?? '—' }) }}
      <ul v-if="result.warnings.length" class="warnings">
        <li v-for="(w, i) in result.warnings" :key="i"><code>{{ w.path }}</code> — {{ w.message }}</li>
      </ul>
    </div>
  </section>

  <section class="card">
    <h2>{{ t('settings.export') }}</h2>
    <p class="muted small">{{ t('settings.exportHint') }}</p>
    <a :href="api.exportUrl" class="btn" download="homedeck.yaml">{{ t('settings.exportDownload') }}</a>
  </section>

  <section class="card">
    <h2>{{ t('settings.revisions') }}</h2>
    <div class="table-wrap">
      <table class="table">
        <tbody>
          <tr v-for="r in revisions" :key="r.id">
            <td>#{{ r.id }}</td>
            <td>{{ formatTime(r.created_at) }}</td>
            <td>{{ r.reason }}</td>
            <td class="muted">v{{ r.schema_version }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.preview {
  margin-top: 16px;
}

.changes,
.warnings {
  padding-left: 1.2em;
  overflow-wrap: anywhere;
}
</style>
