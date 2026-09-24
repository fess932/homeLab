import type { DriverUI } from '..'
import type { TuyaConfig } from './parse'
import TuyaForm from './TuyaForm.vue'

export default {
  kind: 'tuya',
  form: TuyaForm,
  defaults: (): TuyaConfig => ({ device_id: '', version: 'auto', schema: [] }),
  addressPlaceholder: '192.168.0.235',
  addressHint: 'devices.addressHintTuya',
} satisfies DriverUI
