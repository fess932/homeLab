-- Настройки драйвера теперь хранятся как есть, без обёртки {"<драйвер>": {...}}.
UPDATE devices SET config = json(json_extract(config, '$.tuya')) WHERE kind = 'tuya' AND json_type(config, '$.tuya') = 'object';
UPDATE devices SET config = json(json_extract(config, '$.http_json')) WHERE kind = 'http_json' AND json_type(config, '$.http_json') = 'object';
