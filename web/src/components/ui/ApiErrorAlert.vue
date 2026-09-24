<script setup lang="ts">
import { computed } from 'vue'
import { ApiError } from '@/api'
import { errorText, t } from '@/i18n'

const props = defineProps<{ error: unknown }>()
const text = computed(() => (props.error ? errorText(props.error) : ''))
const requestId = computed(() => (props.error instanceof ApiError ? props.error.requestId : ''))
</script>

<template>
  <div v-if="error" class="alert bad" role="alert">
    {{ text }}
    <div v-if="requestId" class="small muted">{{ t('app.requestId', { id: requestId }) }}</div>
  </div>
</template>
