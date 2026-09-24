import type { Component } from 'vue'
import type { MessageKey } from '@/i18n'
import httpjson from './httpjson'
import tuya from './tuya'

/**
 * Часть драйвера устройства в UI. Серверная часть — пакет drivers/<драйвер> в Go:
 * он проверяет настройки и опрашивает устройство, а здесь — только форма настроек.
 * Новый драйвер: каталог drivers/<драйвер> с формой и index.ts, плюс строка в списке ниже.
 */
export interface DriverUI {
  kind: string
  /** Форма настроек: v-model — DeviceInput.config, props errors (ключи config.*) и editing. */
  form: Component
  defaults(): object
  addressPlaceholder: string
  addressHint: MessageKey
}

const all: DriverUI[] = [tuya, httpjson]

export function driverUI(kind: string): DriverUI | undefined {
  return all.find((d) => d.kind === kind)
}
