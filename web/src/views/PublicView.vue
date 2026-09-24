<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, assetUrl, type PublicPage } from '@/api'
import PageBoard from '@/components/PageBoard.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { createContext, provideWidgetContext } from '@/components/widgets/context'
import { usePageTheme, useViewportBreakpoint } from '@/composables/theme'
import { usePolling } from '@/composables/polling'
import { t } from '@/i18n'

const data = ref<PublicPage | null>(null)
const error = ref<unknown>(null)
const loaded = ref(false)
const bp = useViewportBreakpoint()
const route = useRoute()
const slug = computed(() => (typeof route.params.slug === 'string' ? route.params.slug : ''))
const services = computed(() => new Map((data.value?.services ?? []).map((s) => [s.id, s])))
const presets = computed(() => new Map((data.value?.presets ?? []).map((p) => [p.id, p])))

provideWidgetContext(createContext('public', () => services.value, () => presets.value, undefined, () => slug.value))
usePageTheme(computed(() => data.value?.page.theme))

usePolling(async () => {
  if (!slug.value) {
    loaded.value = true
    return
  }
  try {
    data.value = await api.public.page(slug.value)
    error.value = null
  } catch (e) {
    error.value = e
  } finally {
    loaded.value = true
  }
})

const missing = computed(() => !slug.value || (error.value instanceof ApiError && error.value.status === 404))
const logo = computed(() => assetUrl(data.value?.logo_asset_id))
</script>

<template>
  <div class="page">
    <header class="pub-head">
      <img v-if="logo" :src="logo" alt="" width="32" height="32" />
      <h1>{{ data?.title || t('app.name') }}</h1>
    </header>
    <div v-if="loaded && !data && missing" class="card empty">
      <h2>{{ t('public.missingTitle') }}</h2>
      <p class="muted">{{ t('public.missingHint') }}</p>
    </div>
    <ApiErrorAlert v-else-if="loaded && !data" :error="error" />
    <p v-if="!loaded" class="muted">{{ t('app.loading') }}</p>
    <main v-if="data">
      <PageBoard :page="data.page" :bp="bp" />
    </main>
  </div>
</template>

<style scoped>
.pub-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

.empty {
  max-width: 560px;
  margin: 48px auto;
  text-align: center;
}

.pub-head h1 {
  margin: 0;
}
</style>
