<script setup lang="ts">
import { computed } from 'vue'
import { Globe } from 'lucide-vue-next'
import { assetUrl } from '@/api'
import { builtinIcons, parseIcon } from '@/lib/icons'

// «favicon» — иконку с сайта ещё не забрал сервер (он делает это при сохранении): глобус.
const props = withDefaults(defineProps<{ icon?: string; size?: number }>(), { icon: '', size: 28 })
const parsed = computed(() => parseIcon(props.icon))
const component = computed(() => (parsed.value.kind === 'builtin' ? (builtinIcons[parsed.value.name] ?? Globe) : Globe))
const src = computed(() => (parsed.value.kind === 'asset' ? assetUrl(parsed.value.id) : null))
</script>

<template>
  <img v-if="src" :src="src" :width="size" :height="size" alt="" class="svc-icon" loading="lazy" />
  <component :is="component" v-else :size="size" aria-hidden="true" class="svc-icon" />
</template>

<style scoped>
.svc-icon {
  flex: none;
  object-fit: contain;
  color: var(--accent);
}
</style>
