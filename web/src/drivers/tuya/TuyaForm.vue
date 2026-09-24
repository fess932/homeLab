<script setup lang="ts">
import { ref } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import { t } from '@/i18n'
import { readingLabel } from '@/lib/devices'
import type { Errors } from '@/lib/validate'
import { parseTuyaJSON, type TuyaConfig } from './parse'

const config = defineModel<TuyaConfig>({ required: true })
defineProps<{ errors: Errors; editing: boolean }>()
// Из JSON устройства форма устройства берёт название и ключ: ключ станет учётными данными.
const emit = defineEmits<{ parsed: [info: { name: string; key: string }] }>()

const pasted = ref('')
const note = ref('')

function parse() {
  note.value = ''
  try {
    const p = parseTuyaJSON(pasted.value)
    config.value = {
      ...config.value,
      device_id: p.deviceId,
      version: p.version !== 'auto' ? p.version : config.value.version,
      product_id: p.productId || config.value.product_id,
      schema: p.schema,
    }
    emit('parsed', { name: p.name, key: p.localKey })
    note.value = t('devices.parsed', { n: p.schema.length })
    pasted.value = ''
  } catch (e) {
    note.value = t('devices.parseError', { error: e instanceof Error ? e.message : String(e) })
  }
}
</script>

<template>
  <details class="box" :open="!editing && !config.device_id">
    <summary>{{ t('devices.pasteJSON') }}</summary>
    <p class="small muted">{{ t('devices.pasteHint') }}</p>
    <textarea v-model="pasted" class="input code" rows="5" spellcheck="false" :aria-label="t('devices.pasteJSON')" />
    <div class="toolbar paste-bar">
      <button type="button" class="btn small" :disabled="!pasted.trim()" @click="parse">{{ t('devices.parse') }}</button>
      <span v-if="note" class="small" role="status">{{ note }}</span>
    </div>
  </details>

  <div class="row">
    <label class="field">
      <span>{{ t('devices.deviceId') }}</span>
      <input v-model.trim="config.device_id" class="input code" :aria-invalid="!!errors['config.device_id']" />
      <span v-if="errors['config.device_id']" class="error">{{ errors['config.device_id'] }}</span>
    </label>
    <label class="field">
      <span>{{ t('devices.version') }}</span>
      <select v-model="config.version" class="input">
        <option value="auto">{{ t('devices.versionAuto') }}</option>
        <option value="3.5">3.5</option>
        <option value="3.4">3.4</option>
        <option value="3.3">3.3</option>
      </select>
    </label>
  </div>

  <fieldset class="box">
    <legend class="small">{{ t('devices.schema') }}</legend>
    <p v-if="!config.schema.length" class="small muted">{{ t('devices.schemaEmpty') }}</p>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('devices.dp') }}</th>
            <th>{{ t('devices.code') }}</th>
            <th>{{ t('devices.unit') }}</th>
            <th>{{ t('devices.scale') }}</th>
            <th><span class="sr-only">{{ t('app.delete') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(dp, i) in config.schema" :key="dp.dp">
            <td>{{ dp.dp }}</td>
            <td>
              <code>{{ dp.code }}</code>
              <span v-if="readingLabel(dp.code) !== dp.code" class="muted"> · {{ readingLabel(dp.code) }}</span>
            </td>
            <td>{{ dp.unit || (dp.range?.length ? dp.range.join(', ') : '—') }}</td>
            <td>{{ dp.scale || '' }}</td>
            <td>
              <button type="button" class="btn small icon danger" :aria-label="t('app.delete')" @click="config.schema.splice(i, 1)">
                <Trash2 :size="14" aria-hidden="true" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </fieldset>
</template>

<style scoped>
.box {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 12px;
}

.box summary {
  cursor: pointer;
  font-weight: 500;
}

.paste-bar {
  margin-top: 8px;
}
</style>
