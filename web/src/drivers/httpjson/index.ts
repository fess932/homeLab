import type { Unit } from '@/api'
import type { DriverUI } from '..'
import HttpJsonForm from './HttpJsonForm.vue'

/** Поле ответа: путь через точку, ключ метрики, единица и множитель (drivers/httpjson.Field). */
export interface JSONField {
  path: string
  key: string
  unit: Unit
  scale: number
}

/** Настройки устройства HTTP JSON в DeviceInput.config (drivers/httpjson.Config). */
export interface HttpJsonConfig {
  fields: JSONField[]
}

export default {
  kind: 'http_json',
  form: HttpJsonForm,
  defaults: (): HttpJsonConfig => ({ fields: [{ path: '', key: '', unit: '', scale: 1 }] }),
  addressPlaceholder: 'http://shelly.lan/status',
  addressHint: 'devices.addressHintJSON',
} satisfies DriverUI
