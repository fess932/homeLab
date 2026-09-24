<script setup lang="ts">
import { computed } from 'vue'
import type { Unit, WindowAverage } from '@/api'
import { t } from '@/i18n'
import { formatValue } from '@/lib/format'

// Нарушенные нормы за окно: «за сутки 18 мкг/м³ — выше нормы 15». Сервер отдаёт только
// полные окна, а в норме строки нет — показываем лишь превышение.
const props = defineProps<{ averages: WindowAverage[]; unit: Unit }>()
const exceeded = computed(() => props.averages.filter((a) => a.exceeded))
</script>

<template>
  <ul v-if="exceeded.length" class="averages small">
    <li v-for="a in exceeded" :key="a.window" :class="a.norm.color">
      {{ t(`widgets.windows.${a.window}`) }} {{ formatValue(a.value, unit) }} —
      {{ t(a.norm.below ? 'widgets.normBelow' : 'widgets.normAbove', { norm: formatValue(a.norm.value, unit) }) }}
    </li>
  </ul>
</template>

<style scoped>
.averages {
  list-style: none;
  margin: 0;
  padding: 0;
}

.warn {
  color: var(--warn);
}

.crit {
  color: var(--bad);
}
</style>
