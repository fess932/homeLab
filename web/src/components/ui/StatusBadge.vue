<script setup lang="ts">
import { computed } from 'vue'
import type { CheckStatus } from '@/api'
import { probeTone, statusText } from '@/lib/status'

const props = defineProps<{ status?: CheckStatus; compact?: boolean }>()
const state = computed(() => props.status?.state ?? 'unknown')
const text = computed(() => statusText(props.status))
</script>

<template>
  <span class="badge" :class="probeTone[state]" :title="status?.error || text">
    <span class="dot" aria-hidden="true" />
    <span :class="{ 'sr-only': compact }">{{ text }}</span>
  </span>
</template>
