<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Trash2, Upload } from 'lucide-vue-next'
import { api, type Asset } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { formatBytes, formatTime } from '@/lib/format'

const MAX = 5 * 1024 * 1024
const assets = ref<Asset[]>([])
const error = ref<unknown>(null)
const localError = ref('')
const busy = ref(false)
const input = ref<HTMLInputElement | null>(null)

async function load() {
  try {
    assets.value = await api.assets.list()
  } catch (e) {
    error.value = e
  }
}

onMounted(load)

async function upload(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  localError.value = ''
  error.value = null
  if (!f) return
  if (f.size > MAX) {
    localError.value = t('settings.tooLarge')
    return
  }
  busy.value = true
  try {
    await api.assets.upload(f)
    await load()
  } catch (err) {
    error.value = err
  } finally {
    busy.value = false
    if (input.value) input.value.value = ''
  }
}

async function remove(a: Asset) {
  if (!confirm(t('app.confirmDelete', { name: a.id }))) return
  try {
    await api.assets.remove(a.id)
    await load()
  } catch (e) {
    error.value = e
  }
}
</script>

<template>
  <section class="card">
    <h2>{{ t('settings.assets') }}</h2>
    <ApiErrorAlert :error="error" />
    <div v-if="localError" class="alert bad" role="alert">{{ localError }}</div>
    <label class="btn upload">
      <Upload :size="16" aria-hidden="true" /> {{ t('settings.upload') }}
      <input ref="input" type="file" accept="image/png,image/jpeg,image/webp" class="sr-only" :disabled="busy" @change="upload" />
    </label>
    <span class="hint small muted">{{ t('settings.uploadHint') }}</span>
    <div class="assets">
      <figure v-for="a in assets" :key="a.id" class="asset">
        <img :src="a.url" :alt="a.id" loading="lazy" />
        <figcaption class="small">
          <code>{{ a.id }}</code>
          <span class="muted">{{ a.width }}×{{ a.height }} · {{ formatBytes(a.size) }} · {{ formatTime(a.created_at) }}</span>
        </figcaption>
        <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="remove(a)">
          <Trash2 :size="14" aria-hidden="true" />
        </button>
      </figure>
    </div>
  </section>
</template>

<style scoped>
.upload {
  margin-right: 8px;
}

.upload:focus-within {
  outline: 3px solid var(--accent);
  outline-offset: 2px;
}

.assets {
  margin-top: 12px;
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 180px), 1fr));
}

.asset {
  margin: 0;
  position: relative;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.asset img {
  width: 100%;
  height: 100px;
  object-fit: contain;
  background: var(--surface-2);
  border-radius: 6px;
}

.asset figcaption {
  display: flex;
  flex-direction: column;
  overflow-wrap: anywhere;
}

.asset .btn {
  position: absolute;
  top: 12px;
  right: 12px;
}
</style>
