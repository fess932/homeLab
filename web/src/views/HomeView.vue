<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { Pencil, Plus, Save, Search, Settings2, Undo2 } from 'lucide-vue-next'
import { api, ApiError, type Page, type PageInput, type Widget } from '@/api'
import PageBoard from '@/components/PageBoard.vue'
import PageSettingsForm from '@/components/PageSettingsForm.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import WidgetLink from '@/components/widgets/WidgetLink.vue'
import { createContext, provideWidgetContext } from '@/components/widgets/context'
import { usePageTheme, useViewportBreakpoint } from '@/composables/theme'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'
import { blankPage, slugify } from '@/lib/widgets'
import {
  checksById,
  ensureCatalog,
  loadPages,
  loadServices,
  loadSettings,
  presetsById,
  reorderPages,
  servicesById,
  store,
} from '@/stores/app'

const route = useRoute()
const router = useRouter()
const page = ref<Page | null>(null)
const draft = ref<PageInput | null>(null)
const error = ref<unknown>(null)
const conflict = ref(false)
const saving = ref(false)
const loading = ref(true)
const query = ref('')
const showPageSettings = ref(false)
const bp = useViewportBreakpoint()

const editing = computed(() => draft.value !== null)
const shown = computed<PageInput | null>(() => draft.value ?? page.value)
usePageTheme(computed(() => shown.value?.theme))

provideWidgetContext(
  createContext(
    () => (editing.value ? 'edit' : 'view'),
    () => servicesById.value,
    () => presetsById.value,
    () => checksById.value,
  ),
)

const dirty = computed(() => editing.value && JSON.stringify(draft.value) !== JSON.stringify(strip(page.value)))

function strip(p: Page | null): PageInput | null {
  if (!p) return null
  return { title: p.title, slug: p.slug, order: p.order, theme: p.theme, groups: p.groups, widgets: p.widgets }
}

async function resolveTarget(): Promise<string | null> {
  const slug = route.params.slug
  if (typeof slug === 'string' && slug) return slug
  const settings = store.settings ?? (await loadSettings())
  if (settings.start_page_id && store.pages.some((p) => p.id === settings.start_page_id)) return settings.start_page_id
  return store.pages[0]?.id ?? null
}

async function load() {
  loading.value = true
  error.value = null
  try {
    await Promise.all([loadPages(), ensureCatalog(), store.settings ? null : loadSettings()])
    const target = await resolveTarget()
    page.value = target ? await api.pages.get(target) : null
    draft.value = null
    conflict.value = false
  } catch (e) {
    error.value = e
    page.value = null
  } finally {
    loading.value = false
  }
}

watch(() => route.params.slug, load)
onMounted(load)

usePolling(async () => {
  if (!loading.value) await loadServices().catch(() => undefined)
})

function startEdit() {
  if (!page.value) return
  draft.value = JSON.parse(JSON.stringify(strip(page.value))) as PageInput
  conflict.value = false
}

function cancelEdit() {
  draft.value = null
  conflict.value = false
  error.value = null
}

async function save() {
  if (!page.value || !draft.value) return
  saving.value = true
  error.value = null
  try {
    const saved = await api.pages.save(page.value.id, draft.value, page.value.revision)
    const slugChanged = saved.slug !== page.value.slug
    page.value = saved
    draft.value = null
    await loadPages()
    if (slugChanged && route.params.slug) await router.replace(`/p/${saved.slug}`)
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) conflict.value = true
    else error.value = e
  } finally {
    saving.value = false
  }
}

async function createPage() {
  error.value = null
  try {
    const title = store.pages.length ? `${t('home.defaultPageTitle')} ${store.pages.length + 1}` : t('home.defaultPageTitle')
    let slug = slugify(title)
    while (store.pages.some((p) => p.slug === slug)) slug = `${slug}-${Math.random().toString(36).slice(2, 5)}`
    const created = await api.pages.create({ ...blankPage(title, slug, t('home.defaultGroupTitle')), order: store.pages.length })
    await loadPages()
    await router.push(`/p/${created.slug}`)
    if (route.params.slug === created.slug) {
      page.value = created
      startEdit()
    }
  } catch (e) {
    error.value = e
  }
}

function applyPageSettings(p: Pick<PageInput, 'title' | 'slug' | 'theme'>) {
  if (draft.value) Object.assign(draft.value, p)
  showPageSettings.value = false
}

const results = computed<Widget<'link'>[]>(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return []
  return store.services
    .filter((s) => s.name.toLowerCase().includes(q) || s.tags.some((tag) => tag.toLowerCase().includes(q)))
    .map((s) => ({
      id: `search_${s.id}`,
      group_id: '',
      type: 'link' as const,
      config: { service_id: s.id, show_status: true, show_latency: false, metric: null },
      layout: {},
    }))
})

const activeId = computed(() => page.value?.id)

function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
// Перетаскивание табов страниц: порядок меняется на лету, сохраняется при отпускании.
const dragId = ref<string | null>(null)
let dropped = false

function onDragStart(e: DragEvent, id: string) {
  dragId.value = id
  dropped = false
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }
}

function onDragOver(e: DragEvent, overId: string) {
  if (!dragId.value) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (overId === dragId.value) return
  const list = [...store.pages]
  const from = list.findIndex((p) => p.id === dragId.value)
  const to = list.findIndex((p) => p.id === overId)
  if (from < 0 || to < 0) return
  list.splice(to, 0, ...list.splice(from, 1))
  store.pages = list
}

async function onDrop(e: DragEvent) {
  if (!dragId.value) return
  e.preventDefault()
  dropped = true
  dragId.value = null
  error.value = null
  try {
    const changed = await reorderPages(store.pages)
    // У сохранённой страницы сменилась ревизия: перечитываем открытую, чтобы следующая правка не дала конфликт.
    if (page.value && changed.includes(page.value.id)) page.value = await api.pages.get(page.value.id)
  } catch (e) {
    error.value = e
  }
}

function onDragEnd() {
  if (dropped || !dragId.value) return
  dragId.value = null
  loadPages().catch((e) => (error.value = e))
}

onBeforeRouteLeave(() => !dirty.value || confirm(t('editor.unsaved')))
</script>

<template>
  <div class="page home">
    <div class="topbar">
      <nav v-if="store.pages.length" class="tabs" :aria-label="t('home.pages')">
        <RouterLink
          v-for="p in store.pages"
          :key="p.id"
          :to="`/p/${p.slug}`"
          class="tab"
          :class="{ dragging: p.id === dragId }"
          :aria-current="p.id === activeId ? 'page' : undefined"
          :draggable="!editing"
          @dragstart="onDragStart($event, p.id)"
          @dragover="onDragOver($event, p.id)"
          @drop="onDrop"
          @dragend="onDragEnd"
        >
          {{ p.title }}
        </RouterLink>
        <button v-if="!editing" type="button" class="btn small icon" :aria-label="t('settings.newPage')" :title="t('settings.newPage')" @click="createPage">
          <Plus :size="16" aria-hidden="true" />
        </button>
      </nav>
      <span class="spacer" />
      <label v-if="!editing && page" class="search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t('app.search') }}</span>
        <input v-model="query" type="search" class="input" :placeholder="t('app.searchPlaceholder')" />
      </label>
      <button v-if="!editing && page" type="button" class="btn" @click="startEdit">
        <Pencil :size="16" aria-hidden="true" /> {{ t('home.edit') }}
      </button>
    </div>

    <div v-if="editing" class="editbar card" role="region" :aria-label="t('editor.title')">
      <strong>{{ t('editor.title') }}</strong>
      <span class="muted small">{{ t('editor.breakpoint') }} {{ t(`editor.bp.${bp}`) }}</span>
      <span v-if="dirty" class="small warn-text">{{ t('editor.draft') }}</span>
      <span class="spacer" />
      <button type="button" class="btn" @click="showPageSettings = true">
        <Settings2 :size="16" aria-hidden="true" /> {{ t('editor.pageSettings') }}
      </button>
      <button type="button" class="btn" @click="cancelEdit"><Undo2 :size="16" aria-hidden="true" /> {{ t('editor.cancel') }}</button>
      <button type="button" class="btn primary" :disabled="saving || !dirty" @click="save">
        <Save :size="16" aria-hidden="true" /> {{ t('editor.save') }}
      </button>
    </div>

    <div v-if="conflict" class="alert warn" role="alert">
      {{ t('editor.conflict') }}
      <button type="button" class="btn small" @click="load">{{ t('editor.reload') }}</button>
    </div>
    <ApiErrorAlert :error="error" />

    <p v-if="loading && !page" class="muted">{{ t('app.loading') }}</p>
    <div v-else-if="!page && !error" class="empty card">
      <p>{{ route.params.slug ? t('home.notFound') : t('home.noPages') }}</p>
      <button type="button" class="btn primary" @click="createPage">{{ t('home.createFirst') }}</button>
    </div>

    <template v-else-if="shown">
      <section v-if="query.trim()" :aria-label="t('app.search')">
        <p v-if="!results.length" class="muted">{{ t('home.nothingFound') }}</p>
        <div class="grid-cards results">
          <WidgetLink v-for="w in results" :key="w.id" :widget="w" />
        </div>
      </section>
      <PageBoard v-else :page="shown" :bp="bp" :editing="editing" />
    </template>

    <PageSettingsForm v-if="showPageSettings && draft && page" :page="draft" :page-id="page.id" @save="applyPageSettings" @close="showPageSettings = false" />
  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 20px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  min-width: 0;
}

.tab {
  position: relative;
  padding: 4px 12px;
  font-family: var(--font-display);
  font-size: 1.05rem;
  color: var(--text-muted);
  text-decoration: none;
}

.tab[draggable='true'] {
  cursor: grab;
}

.tab.dragging {
  opacity: 0.4;
}

.tab:hover {
  color: var(--text);
}

.tab[aria-current='page'] {
  color: var(--text);
}

.tab[aria-current='page']::after {
  content: '';
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: -11px;
  height: 2px;
  background: var(--accent);
  box-shadow: 0 0 var(--glow) var(--accent);
}

.search {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 200px;
  max-width: 320px;
  color: var(--text-muted);
}

.editbar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 16px;
}

.warn-text {
  color: var(--warn);
}

.empty {
  text-align: center;
  padding: 32px;
}

.results {
  grid-auto-rows: minmax(var(--row), auto);
}
</style>
