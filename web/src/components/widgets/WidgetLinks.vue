<script setup lang="ts">
import { computed } from 'vue'
import type { Widget } from '@/api'
import ServiceIcon from '@/components/ui/ServiceIcon.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'
import { t } from '@/i18n'
import { useWidgetContext } from './context'

const props = defineProps<{ widget: Widget<'links'> }>()
const ctx = useWidgetContext()
const items = computed(() => props.widget.config.service_ids.map((id) => ({ id, service: ctx.service(id) })))

function guard(e: MouseEvent) {
  if (ctx.mode === 'edit') e.preventDefault()
}
</script>

<template>
  <section class="card w-links">
    <h3 v-if="widget.config.title">{{ widget.config.title }}</h3>
    <ul>
      <li v-for="it in items" :key="it.id">
        <a
          v-if="it.service"
          :href="it.service.url"
          :target="it.service.open_mode === 'new_tab' ? '_blank' : undefined"
          :rel="it.service.open_mode === 'new_tab' ? 'noopener noreferrer' : undefined"
          :draggable="false"
          @click="guard"
        >
          <ServiceIcon :icon="it.service.icon" :url="it.service.url" :size="20" />
          <span class="name">{{ it.service.name }}</span>
          <StatusBadge v-if="it.service.check_id" :status="it.service.status" compact />
        </a>
        <span v-else class="muted small">{{ t('widgets.missingService') }}</span>
      </li>
    </ul>
    <p v-if="!items.length" class="muted small">{{ t('widgets.noService') }}</p>
  </section>
</template>

<style scoped>
.w-links {
  height: 100%;
  overflow: auto;
}

ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

a {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border-left: 2px solid transparent;
  color: var(--text);
  text-decoration: none;
}

a:hover {
  background: color-mix(in srgb, var(--accent) 7%, transparent);
  border-left-color: var(--accent);
}

.name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
