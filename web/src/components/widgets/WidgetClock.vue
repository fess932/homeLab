<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import type { Widget } from '@/api'

const props = defineProps<{ widget: Widget<'clock'> }>()
const now = ref(new Date())
const timer = setInterval(() => (now.value = new Date()), 1000)
onBeforeUnmount(() => clearInterval(timer))

const tz = computed(() => {
  const zone = props.widget.config.timezone
  if (!zone) return undefined
  try {
    new Intl.DateTimeFormat('ru-RU', { timeZone: zone })
    return zone
  } catch {
    return undefined
  }
})

const time = computed(() =>
  now.value.toLocaleTimeString('ru-RU', {
    hour: '2-digit',
    minute: '2-digit',
    second: props.widget.config.show_seconds ? '2-digit' : undefined,
    hour12: props.widget.config.hour12,
    timeZone: tz.value,
  }),
)
const date = computed(() =>
  now.value.toLocaleDateString('ru-RU', { weekday: 'long', day: 'numeric', month: 'long', timeZone: tz.value }),
)
</script>

<template>
  <div class="card w-clock">
    <time class="time" :datetime="now.toISOString()">{{ time }}</time>
    <div v-if="widget.config.show_date" class="date muted">{{ date }}</div>
    <div v-if="tz" class="small muted">{{ tz }}</div>
  </div>
</template>

<style scoped>
.w-clock {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
}

.time {
  font-size: clamp(1.4rem, 3.5vw, 2.2rem);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
}
</style>
