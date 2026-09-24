<script setup lang="ts">
import { computed } from 'vue'
import type { Widget } from '@/api'
import { renderMarkdown } from '@/lib/markdown'

const props = defineProps<{ widget: Widget<'note'> }>()
const html = computed(() => renderMarkdown(props.widget.config.markdown))
</script>

<template>
  <div class="card w-note md" v-html="html" />
</template>

<style scoped>
.w-note {
  height: 100%;
  overflow: auto;
  overflow-wrap: anywhere;
}

.md :deep(p) {
  margin: 0 0 0.5em;
}

.md :deep(:last-child) {
  margin-bottom: 0;
}

.md :deep(pre) {
  overflow-x: auto;
  background: var(--surface-2);
  padding: 8px;
  border-radius: var(--radius-sm);
}

.md :deep(code) {
  font-family: var(--mono);
  font-size: 0.85em;
}

.md :deep(ul),
.md :deep(ol) {
  padding-left: 1.3em;
  margin: 0 0 0.5em;
}
</style>
