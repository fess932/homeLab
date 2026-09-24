<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError } from '@/api'
import { errorText, t } from '@/i18n'
import { store } from '@/stores/app'

const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  busy.value = true
  error.value = ''
  try {
    store.session = await api.login(username.value.trim(), password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.replace(next)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) error.value = t('login.failed')
    else if (e instanceof ApiError && e.status === 429) error.value = t('login.rateLimited')
    else error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth">
    <form class="card" @submit.prevent="submit">
      <h1>{{ t('login.title') }}</h1>
      <div v-if="error" class="alert bad" role="alert">{{ error }}</div>
      <label class="field">
        <span>{{ t('login.username') }}</span>
        <input v-model="username" class="input" required autocomplete="username" autofocus />
      </label>
      <label class="field">
        <span>{{ t('login.password') }}</span>
        <input v-model="password" type="password" class="input" required autocomplete="current-password" />
      </label>
      <button type="submit" class="btn primary" :disabled="busy">{{ t('login.submit') }}</button>
    </form>
  </main>
</template>
