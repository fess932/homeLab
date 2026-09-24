<script setup lang="ts">
import type { Unit, WindowAverage } from '@/api'
import { t } from '@/i18n'
import { formatValue } from '@/lib/format'

// Средние за окна норм: «за сутки 18 мкг/м³ — выше нормы 15».
defineProps<{ averages: WindowAverage[]; unit: Unit }>()
</script>

<template>
  <ul class="averages small">
    <li v-for="a in averages" :key="a.window" :class="a.exceeded ? a.norm.color : 'ok'">
      {{ t(`widgets.windows.${a.window}`) }} {{ formatValue(a.value, unit) }} —
      {{
        a.exceeded
          ? t(a.norm.below ? 'widgets.normBelow' : 'widgets.normAbove', { norm: formatValue(a.norm.value, unit) })
          : t(a.norm.below ? 'widgets.normOkBelow' : 'widgets.normOk', { norm: formatValue(a.norm.value, unit) })
      }}
    </li>
  </ul>
</template>

<style scoped>
.averages {
  list-style: none;
  margin: 0;
  padding: 0;
  color: var(--text-muted);
}

.warn {
  color: var(--warn);
}

.crit {
  color: var(--bad);
}
</style>
