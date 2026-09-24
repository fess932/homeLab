<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api'
import ApiErrorAlert from '@/components/ui/ApiErrorAlert.vue'
import { t } from '@/i18n'
import { store } from '@/stores/app'

const router = useRouter()
const form = reactive({ token: '', username: 'admin', password: '', repeat: '', title: t('setup.defaultTitle') })
const error = ref<unknown>(null)
const fieldErrors = ref<Record<string, string>>({})
const busy = ref(false)
const done = ref(false)

onMounted(async () => {
  try {
    done.value = !(await api.setupRequired()).required
  } catch (e) {
    error.value = e
  }
})

async function submit() {
  fieldErrors.value = {}
  if (form.password !== form.repeat) {
    fieldErrors.value = { repeat: t('setup.passwordMismatch') }
    return
  }
  busy.value = true
  error.value = null
  try {
    store.session = await api.setup({
      token: form.token.trim(),
      username: form.username.trim(),
      password: form.password,
      title: form.title.trim(),
    })
    store.settings = null
    await router.replace('/')
  } catch (e) {
    error.value = e
    if (e instanceof ApiError) fieldErrors.value = e.fieldErrors()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth">
    <form class="card" @submit.prevent="submit">
      <h1>{{ t('setup.title') }}</h1>
      <template v-if="done">
        <p>{{ t('setup.done') }}</p>
        <RouterLink to="/login" class="btn primary">{{ t('login.submit') }}</RouterLink>
      </template>
      <template v-else>
        <p class="muted small">{{ t('setup.intro') }}</p>
        <pre class="code cmd">{{ t('setup.command') }}</pre>
        <ApiErrorAlert :error="error" />
        <label class="field">
          <span>{{ t('setup.token') }}</span>
          <input v-model="form.token" class="input code" required autocomplete="one-time-code" :aria-invalid="!!fieldErrors.token" />
          <span v-if="fieldErrors.token" class="error">{{ fieldErrors.token }}</span>
        </label>
        <label class="field">
          <span>{{ t('setup.username') }}</span>
          <input v-model="form.username" class="input" required maxlength="64" autocomplete="username" />
          <span v-if="fieldErrors.username" class="error">{{ fieldErrors.username }}</span>
        </label>
        <label class="field">
          <span>{{ t('setup.password') }}</span>
          <input v-model="form.password" type="password" class="input" required minlength="8" maxlength="256" autocomplete="new-password" />
          <span class="hint">{{ t('setup.passwordHint') }}</span>
          <span v-if="fieldErrors.password" class="error">{{ fieldErrors.password }}</span>
        </label>
        <label class="field">
          <span>{{ t('setup.passwordRepeat') }}</span>
          <input v-model="form.repeat" type="password" class="input" required autocomplete="new-password" :aria-invalid="!!fieldErrors.repeat" />
          <span v-if="fieldErrors.repeat" class="error" role="alert">{{ fieldErrors.repeat }}</span>
        </label>
        <label class="field">
          <span>{{ t('setup.panelTitle') }}</span>
          <input v-model="form.title" class="input" maxlength="100" />
        </label>
        <button type="submit" class="btn primary" :disabled="busy">{{ t('setup.submit') }}</button>
      </template>
    </form>
  </main>
</template>
