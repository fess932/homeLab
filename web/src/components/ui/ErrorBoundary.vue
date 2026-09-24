<script setup lang="ts">
import { onErrorCaptured, ref } from 'vue'
import { t } from '@/i18n'

const failed = ref<string | null>(null)

onErrorCaptured((err) => {
  failed.value = err instanceof Error ? err.message : String(err)
  console.error(err)
  return false
})

function reset() {
  failed.value = null
}
</script>

<template>
  <div v-if="failed" class="boundary" role="alert">
    <strong>{{ t('widgets.error') }}</strong>
    <span class="muted small">{{ failed }}</span>
    <button type="button" class="btn small" @click="reset">{{ t('app.retry') }}</button>
  </div>
  <slot v-else />
</template>

<style scoped>
.boundary {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: flex-start;
  height: 100%;
  padding: var(--pad);
  border: 1px dashed var(--bad);
  border-radius: var(--radius);
  background: var(--surface);
  overflow: hidden;
}
</style>
