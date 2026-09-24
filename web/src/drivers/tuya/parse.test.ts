import { describe, expect, it } from 'vitest'
import { parseTuyaJSON } from './parse'

const strategy = (code: string, valueType: string, valueDesc: string) => ({
  value_convert: 'default',
  status_code: code,
  config_item: { statusFormat: `{"${code}":"$"}`, valueDesc, valueType, enumMappingMap: {}, pid: 'x' },
})

describe('parseTuyaJSON', () => {
  it('reads cloud device JSON: id, key and data points from local_strategy', () => {
    const cloud = {
      name: 'MT15/MT29',
      id: 'eb398c7f26966400abs3ju',
      local_key: 'fake_key_0123456',
      ip: '79.101.225.133',
      local_strategy: {
        '22': strategy('battery_percentage', 'Integer', '{"unit":"%","min":0,"max":100,"scale":0,"step":1}'),
        '1': strategy('air_quality_index', 'Enum', '{"range":["level_1","level_2","level_3"]}'),
        '5': strategy('ch2o_value', 'Integer', '{"unit":"mg/m³","min":0,"max":9999,"scale":3,"step":1}'),
      },
    }
    const p = parseTuyaJSON(JSON.stringify(cloud))
    expect(p).toMatchObject({ name: 'MT15/MT29', deviceId: 'eb398c7f26966400abs3ju', localKey: 'fake_key_0123456', version: 'auto' })
    expect(p.schema).toEqual([
      { dp: '1', code: 'air_quality_index', type: 'Enum', unit: '', scale: 0, range: ['level_1', 'level_2', 'level_3'] },
      { dp: '5', code: 'ch2o_value', type: 'Integer', unit: 'mg/m³', scale: 3 },
      { dp: '22', code: 'battery_percentage', type: 'Integer', unit: '%', scale: 0 },
    ])
  })

  it('reads tinytuya devices.json and takes the first device', () => {
    const wizard = [
      {
        name: 'Розетка',
        id: 'bf1234567890abcdef',
        key: 'fake_key_6543210',
        version: '3.3',
        mapping: { '1': { code: 'switch_1', type: 'Boolean', values: {} }, '19': { code: 'cur_power', type: 'Integer', values: { unit: 'W', scale: 1 } } },
      },
    ]
    const p = parseTuyaJSON(JSON.stringify(wizard))
    expect(p).toMatchObject({ name: 'Розетка', deviceId: 'bf1234567890abcdef', localKey: 'fake_key_6543210', version: '3.3' })
    expect(p.schema.map((d) => [d.dp, d.code, d.unit, d.scale])).toEqual([
      ['1', 'switch_1', '', 0],
      ['19', 'cur_power', 'W', 1],
    ])
  })

  it('rejects JSON without device id', () => {
    expect(() => parseTuyaJSON('{"name":"x"}')).toThrow('id')
    expect(() => parseTuyaJSON('not json')).toThrow()
  })
})
