# HomeDeck

Домашняя панель со ссылками, виджетами и мониторингом инфраструктуры в одном контейнере.

- [Техническое задание: HomeDeck](docs/homedeck-spec.md)
- [Эксплуатация: запуск, backup, обновление](docs/operations.md)
- [API: OpenAPI 3.1](docs/openapi.yaml)

Стек: Go, VictoriaMetrics single-node, SQLite, Vue 3 + TypeScript. UI встраивается в Go-бинарник; VictoriaMetrics работает дочерним процессом внутри того же контейнера.

```sh
mkdir -p data && sudo chown 1000:1000 data
docker compose up -d --build
docker compose exec homedeck homedeck setup-token
```

Разработка (нужен [just](https://github.com/casey/just)): `just run`, `just test`, `just test-integration`, `just lint`.

| Каталог | Содержимое |
| --- | --- |
| `cmd/homedeck` | CLI: `serve`, `healthcheck`, `setup-token`, `version` |
| `internal/app` | Сборка процесса, порядок старта и остановки |
| `internal/api` | REST API, сессии, CSRF, публичная страница, раздача UI |
| `internal/store` | SQLite, миграции, атомарные сохранения |
| `internal/tsdb` | Супервизор VictoriaMetrics, scrape-конфиг и reload, клиент запросов с лимитами |
| `internal/probe` | HTTP/TCP-проверки и состояния `unknown/up/down/stale/disabled` |
| `internal/netguard` | Политика исходящих соединений и egress-proxy |
| `internal/importer` | Импорт/экспорт HomeDeck YAML и импорт Homer |
| `web` | Vue 3 UI |
