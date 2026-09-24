# HomeDeck — эксплуатация

## Запуск

```sh
mkdir -p data && sudo chown 1000:1000 data
docker compose up -d --build
docker compose exec homedeck homedeck setup-token
```

Откройте `http://<хост>:8080`, введите setup-token, создайте администратора. После создания администратора token удаляется, а `POST /api/v1/setup` отвечает 409.

Локально без Docker:

```sh
just run            # собирает UI и Go, скачивает VictoriaMetrics в .bin/, данные в ./data
just test           # go test -race + vitest
just test-integration  # тесты с настоящей VictoriaMetrics
```

## Переменные окружения

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `HOMEDECK_LISTEN` | `:8080` | Адрес UI и API |
| `HOMEDECK_DATA_DIR` | `/data` | Каталог данных, единственный обязательный volume |
| `HOMEDECK_RETENTION` | `30d` | Хранение метрик: число и суффикс `h`, `d`, `w`, `y` |
| `HOMEDECK_SECRET_KEY_FILE` | `/data/secrets.key` | Ключ шифрования credentials; создаётся с правами 0600 при первом старте |
| `HOMEDECK_TRUSTED_PROXIES` | — | CIDR или IP reverse proxy через запятую; только от них учитываются `X-Forwarded-*` |
| `HOMEDECK_ALLOWED_HOSTS` | — | Разрешённые значения `Host` через запятую; защита от DNS rebinding, рекомендуется задать |
| `HOMEDECK_MIN_FREE_DISK` | `512MiB` | Ниже этого свободного места TSDB прекращает приём данных |
| `HOMEDECK_VM_MEMORY_PERCENT` | `40` | Доля памяти cgroup для кешей VictoriaMetrics |
| `HOMEDECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `HOMEDECK_RUNTIME_DIR` | `/tmp/homedeck` | Приватный runtime-каталог, пересоздаётся при старте |
| `HOMEDECK_VM_BINARY` | `/usr/local/bin/victoria-metrics` | Путь к VictoriaMetrics |

Внутренние адреса `HOMEDECK_VM_LISTEN` (`127.0.0.1:8428`), `HOMEDECK_INTERNAL_LISTEN` (`127.0.0.1:9091`), `HOMEDECK_EGRESS_LISTEN` (`127.0.0.1:9092`) менять не требуется. Наружу публикуется только 8080.

Параметры запуска меняются только через окружение и перезапуск контейнера; UI показывает их в «Настройки → Диагностика».

## Процессы и порты

```
tini (PID 1)
└── homedeck serve                  :8080 UI/API, 127.0.0.1:9091 метрики и проверки, 127.0.0.1:9092 egress-proxy
    └── victoria-metrics            127.0.0.1:8428
```

- Старт: права на `/data` → flock `/data/.lock` → проверка целостности и миграции SQLite → ключ секретов → runtime-каталог → scrape-конфиг из SQLite → внутренние endpoint → TSDB → API. Пока TSDB прогревается, ссылки и настройки работают, мониторинг отвечает 503 `tsdb_unavailable`.
- Падение TSDB: перезапуск через 1, 2, 4, 8 секунд; пятая неудача подряд завершает контейнер с кодом 1, дальше перезапускает Docker. Минута стабильной работы сбрасывает счётчик.
- SIGTERM: остановка проверок, завершение HTTP-запросов (до 10 с), корректная остановка TSDB (до 45 с), закрытие SQLite. Бюджет — 60 с (`stop_grace_period`).
- Логи — JSON в stdout, поле `component`: `app`, `api`, `tsdb`, `scheduler`, `reconciler`, `watcher`, `egress`.

## Сбор метрик

- Scrape внешних источников выполняет VictoriaMetrics по конфигу, который Go генерирует из SQLite в `/tmp/homedeck/scrape.yaml`. Credentials и CA лежат рядом в `/tmp/homedeck/secrets/<source_id>/` с правами 0600 и подключаются через `*_file`, в YAML их значений нет.
- Применение: `desired_revision` растёт при изменении источников и секретов → dry-run `victoria-metrics -promscrape.config.dryRun -promscrape.config.strictParse` → атомарная замена файлов → `POST /-/reload` → проверка `vm_promscrape_config_reloads_errors_total` и `vm_promscrape_config_last_reload_success_timestamp_seconds` → `applied_revision`. При ошибке остаётся прежний рабочий конфиг, ошибка видна в UI и `/api/v1/status`, reconciler повторяет попытку каждые 30 с. Последние 20 применённых конфигов сохраняются в `/data/config-revisions/scrape-*.yaml`.
- Все пользовательские scrape идут через внутренний egress-proxy (`proxy_url`). Он и HTTP/TCP-проверки Go открывают соединения через одну политику: запрещены loopback, `0.0.0.0/8`, link-local (включая `169.254.169.254`), multicast, `100.100.100.200`, `fd00:ec2::254`, IPv4-mapped и NAT64-формы этих адресов. Проверяется фактический IP соединения после DNS, поэтому DNS rebinding не обходит запрет. Redirects выключены.
- Лимиты: `sample_limit: 5000` и `series_limit: 20000` на источник, ответ scrape до 16 MiB. Превышение отклоняет scrape источника с понятной ошибкой в карточке источника.
- Метки: `source_id` назначается принудительно (`honor_labels: false`, пользовательские labels не могут содержать `source_id`, `job`, `instance`, `service_id`). Результаты проверок: `homedeck_probe_*{service_id, kind}`.

### Узлы и контейнеры

- Linux-узел: node_exporter на узле, источник типа `node_exporter`. Шаблоны `tpl_node_*` фильтруют по `source_id`.
- Контейнеры: cAdvisor или совместимый exporter, источник типа `cadvisor`. Шаблоны `tpl_container_*` ожидают метки `name` (имя контейнера) и метрики `container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`, `container_network_{receive,transmit}_bytes_total`, `container_last_seen`. Показываются 10 самых нагруженных контейнеров.
- HomeDeck не читает метрики хоста из своего контейнера: без node_exporter на хосте ресурсные графики хоста не строятся.

## Запросы графиков

- Таймаут 5 с, не более 100 рядов, 1000 точек на ряд, 10 MiB ответа. Лимиты продублированы флагами VictoriaMetrics: `-search.maxQueryDuration=5s`, `-search.maxResponseSeries=100`, `-search.maxUniqueTimeseries=50000`, `-search.maxSamplesPerQuery=2e8`, `-search.maxMemoryPerQuery=128MiB`, `-search.maxConcurrentRequests=8`.
- Шаг — из диапазона и числа точек, не меньше интервала сбора источника или проверки, округлён до 15s…1d. Одинаковые запросы объединяются и кешируются на 5 с; до 4 одновременных запросов на сессию и 8 к TSDB. Отмена запроса браузером отменяет запрос к TSDB, если его больше никто не ждёт.
- Шаблоны для длинных диапазонов используют `rate`/`avg_over_time`/`max_over_time` без явного окна: MetricsQL берёт окно, равное шагу, и не теряет данные между точками.

## Данные

```
/data/
  app.db, app.db-wal     настройки, пользователи, сессии, зашифрованные credentials
  app.db.pre-migration-N копия перед миграцией схемы N → N+1
  secrets.key            ключ шифрования (если не задан HOMEDECK_SECRET_KEY_FILE)
  setup-token            только до создания администратора
  assets/                загруженные иконки и фоны
  metrics/               VictoriaMetrics
  config-revisions/      scrape-конфиги и снимки перед импортом, без секретов
```

Один volume — ровно один экземпляр: второй процесс не стартует из-за flock.

## Backup и восстановление (холодная копия)

```sh
docker compose stop homedeck                    # дождаться полной остановки
tar -C ./data -czf homedeck-$(date +%F).tgz .    # весь /data, включая secrets.key
# если ключ передаётся отдельным mount — сохранить его отдельно
docker compose start homedeck
```

Восстановление — в пустой каталог с сохранением владельца:

```sh
docker compose stop homedeck
mkdir data.new && tar -C data.new -xzpf homedeck-YYYY-MM-DD.tgz && sudo chown -R 1000:1000 data.new
mv data data.old && mv data.new data
docker compose start homedeck
```

Без `secrets.key` восстановленные credentials не расшифровываются: источники с секретами покажут ошибку, секреты нужно задать заново. Копирование `/data` работающего контейнера обычным `cp` не поддерживается.

## Обновление и откат

1. Холодная копия `/data`.
2. Новый образ с фиксированным тегом или digest, `docker compose up -d`.
3. Миграции выполняются при старте; перед ними создаётся `app.db.pre-migration-N`.
4. Проверка: `docker compose ps` (healthy), `GET /readyz`.

Откат на старый образ после миграции невозможен без копии: старая версия откажется стартовать с сообщением «схема базы новее поддерживаемой». Восстановите копию `/data`, сделанную до обновления.

## Диск

`/api/v1/status` и «Настройки → Диагностика» показывают объём и свободное место. Предупреждение — меньше 20% свободного, критично — меньше 10% или 1 GiB. При `HOMEDECK_MIN_FREE_DISK` TSDB перестаёт принимать данные; HomeDeck ничего не удаляет автоматически.

Стартовый размер volume — от 10 GiB для малого профиля. Реальный объём считается по суточному приросту `metrics/` на стенде с запасом не менее 30%.

## Безопасность

- Пароли — Argon2id (m=19 MiB, t=2, p=1); сессии — HttpOnly, SameSite=Lax cookie, Secure при HTTPS; CSRF-токен на изменяющие запросы и проверка `Sec-Fetch-Site`/`Origin`; ограничение частоты login/setup.
- Контейнер без root, read-only корень, `cap_drop: ALL`, `no-new-privileges`, Docker socket не монтируется.
- Загрузка файлов: PNG, JPEG, WebP до 5 MiB и 8192×8192, тип проверяется по содержимому; SVG не принимается. Файлы отдаются с `Content-Security-Policy: sandbox` и `nosniff`.
- Экспорт не содержит секретов, хешей паролей и сессий.

## Этап 0: зафиксированные версии и проверенное поведение

| Компонент | Версия |
| --- | --- |
| Go | 1.27 |
| VictoriaMetrics single-node | v1.152.0 (`victoriametrics/victoria-metrics@sha256:86ca5fdb…`) |
| Alpine runtime | 3.24 |
| Node.js (сборка UI) | 22 |
| Vue / Vite / ECharts | 3.5 / 8.3 / 6.1 |
| SQLite | modernc.org/sqlite 1.59 (pure Go, без CGO) |

Проверено на VictoriaMetrics v1.152.0 (darwin/arm64, интеграционные тесты в `internal/tsdb`):

- `-promscrape.config.dryRun` проверяет конфиг без запуска хранилища; неизвестное поле при `strictParse` отклоняется.
- `/-/reload` асинхронный: ошибка видна по росту `vm_promscrape_config_reloads_errors_total`, прежний конфиг продолжает работать.
- `proxy_url` в scrape job направляет HTTP и HTTPS (CONNECT) через Go egress-proxy с проверкой адреса.
- `kill -9` TSDB: `/readyz` сразу 503, ссылки и настройки доступны, перезапуск через 1 с.
- SIGTERM: полная остановка контейнерного процесса за доли секунды на пустых данных.

Не проверено в этой среде (нет Docker): сборка и запуск образа на amd64/arm64, read-only FS, лимит памяти cgroup и измерения из раздела 11 ТЗ. См. `NEEDS.md`.
