<script setup lang="ts">
import { computed } from 'vue'
import type { CheckStatus } from '@/api'
import { probeTone, statusText } from '@/lib/status'

// hint — своя подсказка вместо текста статуса (например, чем проверяется сервис).
const props = defineProps<{ status?: CheckStatus; compact?: boolean; hint?: string }>()
const state = computed(() => props.status?.state ?? 'unknown')
const text = computed(() => statusText(props.status))
</script>

<template>
  <span class="badge" :class="[probeTone[state], { compact }]" :title="hint || status?.error || text">
    <span class="dot" aria-hidden="true" />
    <span :class="{ 'sr-only': compact }">{{ text }}</span>
  </span>
</template>
