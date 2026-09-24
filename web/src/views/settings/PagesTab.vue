<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-vue-next'
import { api } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { blankPage, slugify } from '@/lib/widgets'
import { loadPages, store } from '@/stores/app'

const router = useRouter()
const error = ref<unknown>(null)
const busy = ref(false)
const title = ref('')

onMounted(() => loadPages().catch((e) => (error.value = e)))

async function run(fn: () => Promise<unknown>) {
  busy.value = true
  error.value = null
  try {
    await fn()
    await loadPages()
  } catch (e) {
    error.value = e
  } finally {
    busy.value = false
  }
}

function create() {
  const name = title.value.trim() || t('home.defaultPageTitle')
  let slug = slugify(name)
  while (store.pages.some((p) => p.slug === slug)) slug = `${slug}-${Math.random().toString(36).slice(2, 5)}`
  return run(async () => {
    const p = await api.pages.create({ ...blankPage(name, slug, t('home.defaultGroupTitle')), order: store.pages.length })
    title.value = ''
    await router.push(`/p/${p.slug}`)
  })
}

function move(i: number, delta: number) {
  const j = i + delta
  const list = [...store.pages]
  if (j < 0 || j >= list.length) return
  ;[list[i], list[j]] = [list[j]!, list[i]!]
  return run(async () => {
    for (const [order, summary] of list.entries()) {
      if (summary.order === order) continue
      const full = await api.pages.get(summary.id)
      await api.pages.save(full.id, { title: full.title, slug: full.slug, order, theme: full.theme, groups: full.groups, widgets: full.widgets }, full.revision)
    }
  })
}

function remove(id: string, name: string) {
  if (!confirm(t('app.confirmDelete', { name }))) return
  return run(() => api.pages.remove(id))
}
</script>

<template>
  <section class="card">
    <h2>{{ t('settings.pagesList') }}</h2>
    <ApiErrorAlert :error="error" />
    <ol class="pages">
      <li v-for="(p, i) in store.pages" :key="p.id">
        <RouterLink :to="`/p/${p.slug}`">{{ p.title }}</RouterLink>
        <code class="small muted">/p/{{ p.slug }}</code>
        <span class="spacer" />
        <button type="button" class="btn small icon" :aria-label="t('settings.moveUp')" :disabled="busy || i === 0" @click="move(i, -1)">
          <ArrowUp :size="14" aria-hidden="true" />
        </button>
        <button
          type="button"
          class="btn small icon"
          :aria-label="t('settings.moveDown')"
          :disabled="busy || i === store.pages.length - 1"
          @click="move(i, 1)"
        >
          <ArrowDown :size="14" aria-hidden="true" />
        </button>
        <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" :disabled="busy" @click="remove(p.id, p.title)">
          <Trash2 :size="14" aria-hidden="true" />
        </button>
      </li>
    </ol>
    <form class="row" @submit.prevent="create">
      <label class="field">
        <span>{{ t('editor.pageTitle') }}</span>
        <input v-model="title" class="input" maxlength="100" />
      </label>
      <button type="submit" class="btn" :disabled="busy"><Plus :size="16" aria-hidden="true" /> {{ t('settings.newPage') }}</button>
    </form>
  </section>
</template>

<style scoped>
.pages {
  padding-left: 1.2em;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pages li > * {
  vertical-align: middle;
}

.pages li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.row .btn {
  margin-bottom: 12px;
}
</style>
