<script setup lang="ts">
import { ref, watch } from 'vue'
import { Pause, Play, RefreshCw } from 'lucide-vue-next'
import { t } from '@/i18n'
import { fromLocalInput, rangeNames, toLocalInput, type TimeWindow } from '@/lib/time'
import { useRefreshClock } from '@/composables/polling'

const model = defineModel<TimeWindow>({ required: true })
const clock = useRefreshClock()

const now = new Date()
const from = ref(toLocalInput(model.value.start ?? new Date(now.getTime() - 86400_000)))
const to = ref(toLocalInput(model.value.end ?? now))
const error = ref('')

watch(
  () => model.value.range,
  (r) => {
    if (r !== 'custom') error.value = ''
  },
)

function pick(r: TimeWindow['range']) {
  model.value = r === 'custom' ? { range: 'custom', start: fromLocalInput(from.value)!, end: fromLocalInput(to.value)! } : { range: r }
}

function applyCustom() {
  const s = fromLocalInput(from.value)
  const e = fromLocalInput(to.value)
  if (!s || !e || s >= e) {
    error.value = t('time.invalidRange')
    return
  }
  error.value = ''
  model.value = { range: 'custom', start: s, end: e }
}
</script>

<template>
  <div class="range-picker">
    <div class="seg" role="group" :aria-label="t('widgets.range')">
      <button
        v-for="r in rangeNames"
        :key="r"
        type="button"
        class="btn small"
        :aria-pressed="model.range === r"
        @click="pick(r)"
      >
        {{ t(`time.ranges.${r}`) }}
      </button>
      <button type="button" class="btn small" :aria-pressed="model.range === 'custom'" @click="pick('custom')">
        {{ t('time.custom') }}
      </button>
    </div>
    <form v-if="model.range === 'custom'" class="custom" @submit.prevent="applyCustom">
      <label class="field">
        <span>{{ t('time.from') }}</span>
        <input v-model="from" type="datetime-local" class="input" />
      </label>
      <label class="field">
        <span>{{ t('time.to') }}</span>
        <input v-model="to" type="datetime-local" class="input" />
      </label>
      <button type="submit" class="btn small">{{ t('time.apply') }}</button>
      <span v-if="error" class="error small" role="alert">{{ error }}</span>
    </form>
    <div class="seg">
      <button type="button" class="btn small icon" :aria-label="t('time.refresh')" :title="t('time.refresh')" @click="clock.refresh">
        <RefreshCw :size="16" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="btn small"
        :aria-pressed="clock.paused.value"
        @click="clock.toggle"
      >
        <Play v-if="clock.paused.value" :size="16" aria-hidden="true" />
        <Pause v-else :size="16" aria-hidden="true" />
        {{ clock.paused.value ? t('time.resume') : t('time.pause') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.range-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-end;
}

.seg {
  display: flex;
  flex-wrap: wrap;
}

.seg .btn {
  border-radius: 0;
  margin-left: -1px;
}

.seg .btn:first-child {
  margin-left: 0;
}

.seg .btn[aria-pressed='true'] {
  position: relative;
  z-index: 1;
  background: color-mix(in srgb, var(--accent) 16%, var(--surface));
  border-color: var(--accent);
  color: var(--accent-ink);
  box-shadow: inset 0 -2px 0 var(--accent);
}

.custom {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-end;
}

.custom .field {
  margin: 0;
}

.error {
  color: var(--bad);
}
</style>
