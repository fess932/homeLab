<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, House, LogOut, RadioTower, Settings } from 'lucide-vue-next'
import { api, assetUrl } from '@/api'
import { t } from '@/i18n'
import { resetStore, store } from '@/stores/app'

const route = useRoute()
const router = useRouter()
const bare = computed(() => route.meta.bare)
const logo = computed(() => assetUrl(store.settings?.logo_asset_id))
const title = computed(() => store.settings?.title || t('app.name'))

const links = [
  { to: '/', label: 'nav.home', icon: House, match: (p: string) => p === '/' || p.startsWith('/p/') },
  { to: '/monitoring', label: 'nav.monitoring', icon: Activity, match: (p: string) => p.startsWith('/monitoring') },
  { to: '/sources', label: 'nav.sources', icon: RadioTower, match: (p: string) => p.startsWith('/sources') },
  { to: '/settings', label: 'nav.settings', icon: Settings, match: (p: string) => p.startsWith('/settings') },
] as const

async function logout() {
  try {
    await api.logout()
  } finally {
    resetStore()
    await router.replace('/login')
  }
}
</script>

<template>
  <a href="#main" class="skip-link">{{ t('app.skipToContent') }}</a>
  <header v-if="!bare" class="appbar">
    <RouterLink to="/" class="brand">
      <img v-if="logo" :src="logo" alt="" width="28" height="28" />
      <span>{{ title }}</span>
    </RouterLink>
    <nav class="nav" :aria-label="t('nav.main')">
      <RouterLink
        v-for="l in links"
        :key="l.to"
        :to="l.to"
        class="nav-link"
        :aria-current="l.match(route.path) ? 'page' : undefined"
      >
        <component :is="l.icon" :size="18" aria-hidden="true" />
        <span class="label">{{ t(l.label) }}</span>
      </RouterLink>
    </nav>
    <button v-if="store.session" type="button" class="btn icon logout" :aria-label="t('app.logout')" :title="t('app.logout')" @click="logout">
      <LogOut :size="18" aria-hidden="true" />
    </button>
  </header>
  <main id="main" tabindex="-1">
    <RouterView />
  </main>
</template>

<style scoped>
.appbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  position: relative;
  z-index: 20;
}

.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--text);
  text-decoration: none;
  min-width: 0;
}

.brand span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.nav {
  display: flex;
  gap: 2px;
  flex: 1;
  justify-content: flex-end;
}

.nav-link {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: var(--radius-sm);
  color: var(--text);
  text-decoration: none;
}

.nav-link:hover {
  background: var(--surface-2);
}

.nav-link[aria-current='page'] {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 12%, transparent);
}

main:focus {
  outline: none;
}

@media (max-width: 720px) {
  .nav-link .label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  .nav-link {
    padding: 8px;
  }

  .appbar {
    padding: 8px 10px;
    gap: 6px;
  }
}
</style>
