<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type Asset, type Settings } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { loadPages, loadSettings, store } from '@/stores/app'

const form = ref<Settings | null>(null)
const assets = ref<Asset[]>([])
const error = ref<unknown>(null)
const saved = ref(false)
const busy = ref(false)

onMounted(async () => {
  try {
    const [s, a] = await Promise.all([loadSettings(), api.assets.list(), loadPages()])
    form.value = { ...s }
    assets.value = a
  } catch (e) {
    error.value = e
  }
})

async function submit() {
  if (!form.value) return
  busy.value = true
  error.value = null
  saved.value = false
  try {
    const { title, logo_asset_id, start_page_id } = form.value
    store.settings = await api.settings.update(
      { title, logo_asset_id, start_page_id },
      form.value.revision ?? 0,
    )
    form.value = { ...store.settings }
    saved.value = true
  } catch (e) {
    error.value = e
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form v-if="form" class="card" @submit.prevent="submit">
    <ApiErrorAlert :error="error" />
    <div v-if="saved" class="alert ok" role="status">{{ t('app.saved') }}</div>
    <label class="field">
      <span>{{ t('settings.panelTitle') }}</span>
      <input v-model="form.title" class="input" maxlength="100" />
    </label>
    <label class="field">
      <span>{{ t('settings.logo') }}</span>
      <select v-model="form.logo_asset_id" class="input">
        <option :value="null">{{ t('settings.noLogo') }}</option>
        <option v-for="a in assets" :key="a.id" :value="a.id">{{ a.id }} · {{ a.width }}×{{ a.height }}</option>
      </select>
    </label>
    <label class="field">
      <span>{{ t('settings.startPage') }}</span>
      <select v-model="form.start_page_id" class="input">
        <option :value="null">—</option>
        <option v-for="p in store.pages" :key="p.id" :value="p.id">{{ p.title }}</option>
      </select>
    </label>
    <div class="toolbar">
      <button type="submit" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
    </div>
  </form>
  <ApiErrorAlert v-else :error="error" />
</template>
