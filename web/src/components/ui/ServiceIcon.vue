<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Globe } from 'lucide-vue-next'
import { assetUrl } from '@/api'
import { builtinIcons, faviconCandidates, parseIcon } from '@/lib/icons'

// url нужен для icon="favicon": иконка берётся с сайта, при неудаче — глобус.
const props = withDefaults(defineProps<{ icon?: string; size?: number; url?: string }>(), { icon: '', size: 28, url: '' })
const parsed = computed(() => parseIcon(props.icon))
const component = computed(() => (parsed.value.kind === 'builtin' ? (builtinIcons[parsed.value.name] ?? Globe) : Globe))
const favicons = computed(() => (parsed.value.kind === 'favicon' ? faviconCandidates(props.url) : []))
const tried = ref(0)
watch(favicons, () => (tried.value = 0))
const src = computed(() => {
  if (parsed.value.kind === 'asset') return assetUrl(parsed.value.id)
  return favicons.value[tried.value] ?? null
})
</script>

<template>
  <img
    v-if="src"
    :src="src"
    :width="size"
    :height="size"
    alt=""
    class="svc-icon"
    loading="lazy"
    referrerpolicy="no-referrer"
    @error="tried++"
  />
  <component :is="component" v-else :size="size" aria-hidden="true" class="svc-icon" />
</template>

<style scoped>
.svc-icon {
  flex: none;
  object-fit: contain;
  color: var(--accent);
}
</style>
