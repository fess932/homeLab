<script setup lang="ts">
import { onMounted, ref, useId } from 'vue'
import { X } from 'lucide-vue-next'
import { t } from '@/i18n'

defineProps<{ title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const titleId = useId()

onMounted(() => dialog.value?.showModal?.())

function onCancel(e: Event) {
  e.preventDefault()
  emit('close')
}
</script>

<template>
  <dialog ref="dialog" class="modal" :class="{ wide }" :aria-labelledby="titleId" @cancel="onCancel">
    <header class="modal-head">
      <h2 :id="titleId">{{ title }}</h2>
      <button type="button" class="btn icon" :aria-label="t('app.close')" @click="emit('close')">
        <X :size="18" aria-hidden="true" />
      </button>
    </header>
    <div class="modal-body">
      <slot />
    </div>
    <footer v-if="$slots.footer" class="modal-foot">
      <slot name="footer" />
    </footer>
  </dialog>
</template>

<style scoped>
.modal {
  width: min(560px, calc(100vw - 20px));
  max-height: calc(100dvh - 20px);
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  color: var(--text);
  box-shadow: 0 20px 50px rgb(0 0 0 / 0.25);
  display: flex;
  flex-direction: column;
}

.modal:not([open]) {
  display: none;
}

.modal.wide {
  width: min(900px, calc(100vw - 20px));
}

.modal::backdrop {
  background: rgb(0 0 0 / 0.45);
}

.modal-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}

.modal-head h2 {
  margin: 0;
  flex: 1;
  font-size: 1.1rem;
}

.modal-body {
  padding: 16px;
  overflow: auto;
}

.modal-foot {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
}
</style>
