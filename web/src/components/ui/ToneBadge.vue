<script setup lang="ts">
import type { Tone } from '@/lib/status'

defineProps<{ tone: Tone; text: string; title?: string }>()
</script>

<template>
  <span class="badge" :class="tone" :title="title || text">
    <span class="dot" aria-hidden="true" />
    <span>{{ text }}</span>
  </span>
</template>
