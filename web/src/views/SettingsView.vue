<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useRoute } from 'vue-router'
import { t } from '@/i18n'
import AccessTab from './settings/AccessTab.vue'
import AppearanceTab from './settings/AppearanceTab.vue'
import AssetsTab from './settings/AssetsTab.vue'
import DiagnosticsTab from './settings/DiagnosticsTab.vue'
import PagesTab from './settings/PagesTab.vue'
import PortabilityTab from './settings/PortabilityTab.vue'
import PresetsTab from './settings/PresetsTab.vue'
import ServicesTab from './settings/ServicesTab.vue'

const tabs: Record<string, Component> = {
  appearance: AppearanceTab,
  pages: PagesTab,
  services: ServicesTab,
  presets: PresetsTab,
  portability: PortabilityTab,
  assets: AssetsTab,
  access: AccessTab,
  diagnostics: DiagnosticsTab,
}

const route = useRoute()
const active = computed(() => {
  const tab = String(route.params.tab || 'appearance')
  return tab in tabs ? tab : 'appearance'
})
</script>

<template>
  <div class="page settings">
    <h1>{{ t('settings.title') }}</h1>
    <nav class="tabs" :aria-label="t('settings.title')">
      <RouterLink
        v-for="name in Object.keys(tabs)"
        :key="name"
        :to="`/settings/${name}`"
        class="tab"
        :aria-current="active === name ? 'page' : undefined"
      >
        {{ t(`settings.tabs.${name}`) }}
      </RouterLink>
    </nav>
    <div class="stack">
      <component :is="tabs[active]" :key="active" />
    </div>
  </div>
</template>

<style scoped>
.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  margin-bottom: 20px;
  border-bottom: 1px solid var(--border);
}

.tab {
  position: relative;
  padding: 7px 12px 9px;
  color: var(--text-muted);
  text-decoration: none;
  font-weight: 500;
}

.tab:hover {
  color: var(--text);
}

.tab[aria-current='page'] {
  color: var(--accent-ink);
}

.tab[aria-current='page']::after {
  content: '';
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: -1px;
  height: 2px;
  background: var(--accent);
  box-shadow: 0 0 var(--glow) var(--accent);
}
</style>
