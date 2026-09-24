import type { Reading } from '@/api'
import { t } from '@/i18n'
import { formatValue } from '@/lib/format'

/** Подпись величины: общий ключ по-русски, остальное — как пришло от устройства. */
export function readingLabel(key: string): string {
  const label = t(`devices.keys.${key}`)
  return label === `devices.keys.${key}` ? key : label
}

/** Сырые точки данных Tuya без описания приходят с ключом dp_<номер>. */
export const isRawReading = (r: Reading) => /^dp_\d+$/.test(r.key)

/** Значение для показа: у перечислений — их состояние, у остальных — число с единицей. */
export function readingText(r: Reading): string {
  return r.state || formatValue(r.value, r.unit)
}
