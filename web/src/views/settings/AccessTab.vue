<script setup lang="ts">
import { ref } from 'vue'
import { api, ApiError } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'

const current = ref('')
const next = ref('')
const repeat = ref('')
const error = ref<unknown>(null)
const mismatch = ref(false)
const done = ref(false)
const busy = ref(false)

async function submit() {
  mismatch.value = next.value !== repeat.value
  done.value = false
  if (mismatch.value) return
  busy.value = true
  error.value = null
  try {
    await api.changePassword(current.value, next.value)
    done.value = true
    current.value = next.value = repeat.value = ''
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 401 ? new Error(t('login.failed')) : e
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="card narrow" @submit.prevent="submit">
    <h2>{{ t('settings.password') }}</h2>
    <ApiErrorAlert :error="error" />
    <div v-if="done" class="alert ok" role="status">{{ t('settings.passwordChanged') }}</div>
    <input type="text" autocomplete="username" :value="''" class="sr-only" tabindex="-1" aria-hidden="true" />
    <label class="field">
      <span>{{ t('settings.currentPassword') }}</span>
      <input v-model="current" type="password" class="input" required autocomplete="current-password" />
    </label>
    <label class="field">
      <span>{{ t('settings.newPassword') }}</span>
      <input v-model="next" type="password" class="input" required minlength="10" autocomplete="new-password" />
      <span class="hint">{{ t('setup.passwordHint') }}</span>
    </label>
    <label class="field">
      <span>{{ t('setup.passwordRepeat') }}</span>
      <input v-model="repeat" type="password" class="input" required autocomplete="new-password" :aria-invalid="mismatch" />
      <span v-if="mismatch" class="error" role="alert">{{ t('setup.passwordMismatch') }}</span>
    </label>
    <button type="submit" class="btn primary" :disabled="busy">{{ t('app.save') }}</button>
  </form>
</template>

<style scoped>
.narrow {
  max-width: 480px;
}
</style>
