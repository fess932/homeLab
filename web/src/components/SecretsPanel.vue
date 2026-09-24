<script setup lang="ts">
import { ref } from 'vue'
import { Plus, Trash2 } from 'lucide-vue-next'
import { api, type Secret, type SecretInput } from '@/api'
import ModalDialog from '@/components/ui/ModalDialog.vue'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'

const props = defineProps<{ secrets: Secret[]; names?: Record<string, string> }>()
const usedBy = (s: Secret) => s.used_by.map((id) => props.names?.[id] ?? id).join(', ')
const emit = defineEmits<{ changed: [] }>()

const editing = ref<{ id: string | null; input: SecretInput } | null>(null)
const error = ref<unknown>(null)
const busy = ref(false)

function open(s?: Secret) {
  error.value = null
  editing.value = {
    id: s?.id ?? null,
    input: { name: s?.name ?? '', kind: s?.kind ?? 'basic', username: '', password: '', token: '', key: '' },
  }
}

async function submit() {
  if (!editing.value) return
  busy.value = true
  error.value = null
  const { id, input } = editing.value
  const body: SecretInput =
    input.kind === 'basic'
      ? { name: input.name, kind: 'basic', username: input.username, password: input.password }
      : input.kind === 'key'
        ? { name: input.name, kind: 'key', key: input.key }
        : { name: input.name, kind: 'bearer', token: input.token }
  try {
    if (id) await api.secrets.update(id, body)
    else await api.secrets.create(body)
    editing.value = null
    emit('changed')
  } catch (e) {
    error.value = e
  } finally {
    busy.value = false
  }
}

async function remove(s: Secret) {
  if (!confirm(t('app.confirmDelete', { name: s.name }))) return
  error.value = null
  try {
    await api.secrets.remove(s.id)
    emit('changed')
  } catch (e) {
    error.value = e
  }
}
</script>

<template>
  <section class="card">
    <div class="toolbar">
      <h2>{{ t('sources.secrets') }}</h2>
      <span class="spacer" />
      <button type="button" class="btn small" @click="open()"><Plus :size="14" aria-hidden="true" /> {{ t('sources.addSecret') }}</button>
    </div>
    <ApiErrorAlert v-if="!editing" :error="error" />
    <ul class="secrets">
      <li v-for="s in secrets" :key="s.id">
        <strong>{{ s.name }}</strong>
        <span class="tag">{{ t(`sources.secretKinds.${s.kind}`) }}</span>
        <code class="small">{{ s.mask }}</code>
        <span class="small muted">{{ s.used_by.length ? t('sources.usedBy', { list: usedBy(s) }) : t('sources.notUsed') }}</span>
        <span class="spacer" />
        <button type="button" class="btn small" @click="open(s)">{{ t('sources.secretReplace') }}</button>
        <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="remove(s)">
          <Trash2 :size="14" aria-hidden="true" />
        </button>
      </li>
    </ul>
    <ModalDialog v-if="editing" :title="editing.id ? t('sources.secretReplace') : t('sources.addSecret')" @close="editing = null">
      <ApiErrorAlert :error="error" />
      <form id="secret-form" autocomplete="off" @submit.prevent="submit">
        <label class="field">
          <span>{{ t('sources.name') }}</span>
          <input v-model="editing.input.name" class="input" required maxlength="100" />
        </label>
        <label class="field">
          <span>{{ t('sources.kind') }}</span>
          <select v-model="editing.input.kind" class="input">
            <option value="basic">{{ t('sources.secretKinds.basic') }}</option>
            <option value="bearer">{{ t('sources.secretKinds.bearer') }}</option>
            <option value="key">{{ t('sources.secretKinds.key') }}</option>
          </select>
        </label>
        <template v-if="editing.input.kind === 'basic'">
          <label class="field">
            <span>{{ t('sources.username') }}</span>
            <input v-model="editing.input.username" class="input" required autocomplete="off" />
          </label>
          <label class="field">
            <span>{{ t('sources.password') }}</span>
            <input v-model="editing.input.password" type="password" class="input" required autocomplete="new-password" />
          </label>
        </template>
        <label v-else-if="editing.input.kind === 'key'" class="field">
          <span>{{ t('sources.key') }}</span>
          <input v-model="editing.input.key" type="password" class="input" required autocomplete="off" maxlength="256" />
          <span class="hint">{{ t('sources.keyHint') }}</span>
        </label>
        <label v-else class="field">
          <span>{{ t('sources.token') }}</span>
          <input v-model="editing.input.token" type="password" class="input" required autocomplete="off" />
        </label>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="editing = null">{{ t('app.cancel') }}</button>
        <button type="submit" form="secret-form" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
      </template>
    </ModalDialog>
  </section>
</template>

<style scoped>
.secrets {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.secrets li {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
</style>
