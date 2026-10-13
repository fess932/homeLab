<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRef } from 'vue'
import { Cloud, CloudDrizzle, CloudFog, CloudLightning, CloudMoon, CloudRain, CloudSnow, CloudSun, Moon, Sun } from 'lucide-vue-next'
import type { Widget } from '@/api'
import { errorText, t } from '@/i18n'
import { useWidgetData } from './context'

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

const { data, error } = useWidgetData(toRef(props, 'widget'), ref({}), computed(() => !!props.widget.config.city?.trim()))

// Коды явлений WMO, как их отдаёт Open-Meteo.
function kind(code: number) {
  if (code === 0) return 'clear'
  if (code <= 2) return 'partly'
  if (code === 3) return 'cloudy'
  if (code <= 48) return 'fog'
  if (code <= 57) return 'drizzle'
  if (code <= 67 || (code >= 80 && code <= 82)) return 'rain'
  if (code <= 86) return 'snow'
  return 'storm'
}

const icons = { cloudy: Cloud, fog: CloudFog, drizzle: CloudDrizzle, rain: CloudRain, snow: CloudSnow, storm: CloudLightning }

const weather = computed(() => {
  const w = data.value?.weather
  if (!w) return null
  const k = kind(w.code)
  const icon = k === 'clear' ? (w.is_day ? Sun : Moon) : k === 'partly' ? (w.is_day ? CloudSun : CloudMoon) : icons[k]
  const temp = Math.round(w.temperature)
  return { icon, city: w.city, label: t(`widgets.weather.${k}`), temp: `${temp > 0 ? '+' : temp < 0 ? '−' : ''}${Math.abs(temp)}°` }
})
</script>

<template>
  <div class="card w-clock">
    <div class="now">
      <time class="time" :datetime="now.toISOString()">{{ time }}</time>
      <div v-if="widget.config.show_date" class="date muted">{{ date }}</div>
      <div v-if="tz" class="small muted">{{ tz }}</div>
    </div>
    <div v-if="weather" class="weather" :title="weather.city">
      <div class="temp"><component :is="weather.icon" :size="22" aria-hidden="true" /> {{ weather.temp }}</div>
      <div class="small muted">{{ weather.label }}</div>
    </div>
    <div v-else-if="error" class="weather small err" role="alert">{{ errorText(error) }}</div>
  </div>
</template>

<style scoped>
.w-clock {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  overflow: hidden;
}

.now {
  min-width: 0;
}

.time {
  font-family: var(--font-display);
  font-stretch: 75%;
  font-size: clamp(1.7rem, 3.5vw, 2.6rem);
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1.05;
  color: var(--accent-ink);
  text-shadow: 0 0 calc(var(--glow) * 1.6) color-mix(in srgb, var(--accent) 50%, transparent);
}

.date::first-letter {
  text-transform: uppercase;
}

.weather {
  flex: none;
  max-width: 50%;
  text-align: right;
}

.temp {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  font-family: var(--font-display);
  font-stretch: 75%;
  font-size: clamp(1.2rem, 2.2vw, 1.6rem);
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
}

.err {
  color: var(--bad);
}
</style>
