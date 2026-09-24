<script setup lang="ts">
import { Plus, Trash2 } from 'lucide-vue-next'
import type { Unit } from '@/api'
import { t } from '@/i18n'
import type { Errors } from '@/lib/validate'
import type { HttpJsonConfig } from '.'

const config = defineModel<HttpJsonConfig>({ required: true })
defineProps<{ errors: Errors; editing: boolean }>()

const units: Unit[] = ['', 'celsius', 'percent', 'ppm', 'ugm3', 'mgm3', 'count', 'bool', 'seconds', 'milliseconds', 'bytes', 'per_second']
const err = (errors: Errors, i: number, f: string) => errors[`config.fields[${i}].${f}`]
</script>

<template>
  <fieldset class="box">
    <legend class="small">{{ t('devices.fields') }}</legend>
    <p v-if="errors['config.fields']" class="error small">{{ errors['config.fields'] }}</p>
    <div v-for="(f, i) in config.fields" :key="i" class="row field-row">
      <label class="field">
        <span>{{ t('devices.path') }}</span>
        <input v-model="f.path" class="input code" :placeholder="t('devices.pathHint')" :aria-invalid="!!err(errors, i, 'path')" />
        <span v-if="err(errors, i, 'path')" class="error">{{ err(errors, i, 'path') }}</span>
      </label>
      <label class="field">
        <span>{{ t('devices.fieldKey') }}</span>
        <input v-model="f.key" class="input code" placeholder="power" :aria-invalid="!!err(errors, i, 'key')" />
        <span v-if="err(errors, i, 'key')" class="error">{{ err(errors, i, 'key') }}</span>
      </label>
      <label class="field narrow">
        <span>{{ t('devices.unit') }}</span>
        <select v-model="f.unit" class="input">
          <option v-for="u in units" :key="u" :value="u">{{ t(`monitoring.units.${u}`) }}</option>
        </select>
      </label>
      <label class="field narrow">
        <span>{{ t('devices.multiplier') }}</span>
        <input v-model.number="f.scale" type="number" step="any" class="input" />
      </label>
      <button type="button" class="btn icon danger" :aria-label="t('app.delete')" @click="config.fields.splice(i, 1)">
        <Trash2 :size="16" aria-hidden="true" />
      </button>
    </div>
    <button type="button" class="btn small" @click="config.fields.push({ path: '', key: '', unit: '', scale: 1 })">
      <Plus :size="14" aria-hidden="true" /> {{ t('devices.addField') }}
    </button>
  </fieldset>
</template>

<style scoped>
.box {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
  margin-bottom: 12px;
}

.field-row {
  flex-wrap: nowrap;
  align-items: flex-start;
}

.field-row .field {
  margin-bottom: 6px;
}

.field-row .narrow {
  flex: 0 1 130px;
}

.field-row > .btn {
  margin-top: 26px;
}

@media (max-width: 640px) {
  .field-row {
    flex-wrap: wrap;
  }
}
</style>
