<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, assetUrl, type PublicPage } from '@/api'
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
const services = computed(() => new Map((data.value?.services ?? []).map((s) => [s.id, s])))

provideWidgetContext(createContext('public', () => services.value, () => new Map()))
usePageTheme(computed(() => data.value?.page.theme))

usePolling(async () => {
  try {
    data.value = await api.public.page()
    error.value = null
  } catch (e) {
    error.value = e
  } finally {
    loaded.value = true
  }
})

const logo = computed(() => assetUrl(data.value?.logo_asset_id))
</script>

<template>
  <div class="page">
    <header class="pub-head">
      <img v-if="logo" :src="logo" alt="" width="32" height="32" />
      <h1>{{ data?.title || t('app.name') }}</h1>
      <span class="spacer" />
      <RouterLink to="/login" class="btn small">{{ t('login.submit') }}</RouterLink>
    </header>
    <ApiErrorAlert v-if="loaded && !data" :error="error" />
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

.pub-head h1 {
  margin: 0;
}
</style>
