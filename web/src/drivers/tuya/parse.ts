export type TuyaVersion = 'auto' | '3.3' | '3.4' | '3.5'

/** Точка данных устройства Tuya, как её хранит драйвер на сервере (drivers/tuya.DP). */
export interface TuyaDP {
  dp: string
  code: string
  type: string
  unit: string
  scale: number
  range?: string[]
}

/** Настройки устройства Tuya в DeviceInput.config (drivers/tuya.Config). */
export interface TuyaConfig {
  device_id: string
  version: TuyaVersion
  product_id?: string
  schema: TuyaDP[]
}

export interface ParsedTuya {
  name: string
  deviceId: string
  localKey: string
  version: TuyaVersion
  productId: string
  schema: TuyaDP[]
}

type Obj = Record<string, unknown>

const isObj = (v: unknown): v is Obj => !!v && typeof v === 'object' && !Array.isArray(v)
const str = (v: unknown) => (typeof v === 'string' ? v : '')

// Описание значения приходит JSON-строкой: {"unit":"℃","scale":0,...} или {"range":[...]}.
function describe(dp: string, code: string, type: string, desc: unknown): TuyaDP {
  let d: Obj = {}
  if (isObj(desc)) d = desc
  else if (typeof desc === 'string' && desc) {
    try {
      const parsed: unknown = JSON.parse(desc)
      if (isObj(parsed)) d = parsed
    } catch {
      /* описание необязательно */
    }
  }
  const out: TuyaDP = { dp, code, type, unit: str(d.unit), scale: typeof d.scale === 'number' ? d.scale : 0 }
  if (Array.isArray(d.range)) out.range = d.range.filter((x): x is string => typeof x === 'string')
  return out
}

function fromCloud(o: Obj): TuyaDP[] {
  const strategy = isObj(o.local_strategy) ? o.local_strategy : {}
  return Object.entries(strategy).flatMap(([dp, v]) => {
    if (!isObj(v) || !isObj(v.config_item)) return []
    return [describe(dp, str(v.status_code), str(v.config_item.valueType), v.config_item.valueDesc)]
  })
}

function fromTinytuya(o: Obj): TuyaDP[] {
  const mapping = isObj(o.mapping) ? o.mapping : {}
  return Object.entries(mapping).flatMap(([dp, v]) => (isObj(v) ? [describe(dp, str(v.code), str(v.type), v.values)] : []))
}

/**
 * Разбирает JSON устройства Tuya: ответ облака (local_strategy/status_range) или
 * запись devices.json из tinytuya wizard (key/mapping). Из массива берётся первое устройство.
 */
export function parseTuyaJSON(text: string): ParsedTuya {
  let root: unknown = JSON.parse(text)
  if (Array.isArray(root)) root = root[0]
  if (isObj(root) && isObj(root.result)) root = root.result
  if (!isObj(root)) throw new Error('ожидается объект устройства')
  const deviceId = str(root.id) || str(root.devId) || str(root.device_id)
  if (!deviceId) throw new Error('нет поля id')
  const version = str(root.version)
  const schema = (root.local_strategy ? fromCloud(root) : fromTinytuya(root))
    .filter((d) => /^\d{1,4}$/.test(d.dp) && d.code)
    .sort((a, b) => Number(a.dp) - Number(b.dp))
  return {
    name: str(root.name) || str(root.product_name),
    deviceId,
    localKey: str(root.local_key) || str(root.key),
    version: (['3.3', '3.4', '3.5'].includes(version) ? version : 'auto') as TuyaVersion,
    productId: str(root.product_id) || str(root.productKey),
    schema,
  }
}
