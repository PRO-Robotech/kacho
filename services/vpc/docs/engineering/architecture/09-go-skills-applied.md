# 09 — Принципы Go-стиля и их применение

Свод инженерных принципов, которым следует код kacho-vpc, и того, как они выражены
в репозитории. Это не история разработки, а описание текущего состояния и обоснований.

## Code style и инструменты

| Принцип | Состояние в репо |
|---|---|
| Форматирование | `gofmt` — clean; единый layout пакетов по Clean Architecture |
| Naming | MixedCaps + acronym rules; proto-mirror naming (`IpVersion`, `SetXxxId`) сохранен — переименование сломало бы proto-API |
| Современный Go | код на Go 1.22+; `copyloopvar` включен в линтере |
| Линтинг | `.golangci.yml` (v2) — **в корне монорепо, один на всё дерево**, не per-service; набор линтеров читать оттуда, а не отсюда: выписанный здесь список разошёлся бы с конфигом молча |
| CI | `.github/workflows/ci.yaml` в корне монорепо (build + vet + test-race + lint + govulncheck). Собственного workflow у `services/vpc/` нет — предикат: `git ls-files services/vpc/.github` даёт пусто |

## Error handling и context

- Sentinel-ошибки (`ErrPoolNotResolved`, `ErrInvalidIPv4`, и пр.) + оборачивание через
  `fmt.Errorf(... %w ...)`. Наружу из репо/сервиса не утекает pgx-текст — INTERNAL
  отдается с фиксированным сообщением.
- `context` чистый: нет `context.TODO` в production-path; `context.Background` только в
  shutdown-cleanup (fresh ctx для отписки — корректно).
- Безопасность: нет defer-in-loop, нет очевидных nil-deref паттернов.

## Структуры, интерфейсы, DI

- Constructor injection (порты + Clean Architecture); композиционный корень —
  единственное место wiring (`cmd/vpc/main.go`.`runServe`). DI-фреймворк не используется —
  для сервиса такого размера это лишняя абстракция.
- Аллокация вынесена из чтения/записи адреса в отдельный порт `AddressAllocator`
  (`internal/handler/internal_address_allocate_handler.go`), который удовлетворяет
  use-case `internal/apps/kacho/api/address/allocate.go`; порт-интерфейсы
  сегрегированы в `internal/apps/kacho/api/addresspool/iface.go` (`AddressRepo`,
  `NetworkRepo`, `SubnetReader`, `ZoneRegistry`). Отдельного файла с прежним именем в
  дереве нет — порты собраны в один `iface.go` на пакет, как у остальных use-case'ов.
  Embedding не используется намеренно.
- Паттерны: worker (`operations.Run`), transactional outbox, retry-on-conflict в
  allocator. Functional options не нужны (конструкторы короткие, опции не накапливаются).

## База данных

- pgx без ORM. `tx.Begin/Commit` с `defer Rollback`. Prepared statements через pgx
  auto-prepare. Outbox-запись — в той же tx, что и доменный INSERT.
- `EXCLUDE`-constraint для CIDR overlap (race-free на DB-уровне); `xmin` для optimistic
  locking; `FOR UPDATE SKIP LOCKED` для freelist-аллокации.

## Производительность

- Hot paths профилированы: cascade resolve — несколько SELECT'ов (cacheable, но кэш пока
  не нужен); allocator pick + retry — bounded по числу попыток.
- Микробенчмарки: `internal/repo/address_pool_freelist_bench_test.go` —
  `BenchmarkAllocateExternalIP_Freelist` и его `_Parallel`-близнец: мерят аллокацию
  внешнего адреса из книги учёта (`address_pool_free_ips`) целиком, вместе с
  `FOR UPDATE SKIP LOCKED`, а не отдельную функцию подбора. Прежняя редакция называла
  здесь три функции — ни одного из этих трёх имён в дереве нет.
- Подбор случайного адреса живёт в `internal/domain/cidr.go` (`PickRandomIPv4` —
  fixed-size `[4]byte` на стеке, `PickRandomIPv6`) и применяется там, где книги учёта
  нет: internal-адрес в подсети. Распознавание нарушения уникальности —
  `isUniqueViolation` (`internal/apps/kacho/api/address/helpers.go`): аллокатор ловит
  его и повторяет попытку.

## gRPC и observability

- `grpcsrv` из corelib (recovery + logging interceptors). `FromError`-маппинг в handler.
  Все RPC unary (read — sync, мутации — async через `Operation`); server-streaming RPC нет —
  изменения наблюдаются через polling `List` / `OperationService.Get`.
- `slog` (json) — стандарт логирования.

## Тестирование

- unit-тесты сервис/handler через mock-порты; integration через testcontainers (CRUD,
  EXCLUDE/FK/UNIQUE, CAS/OCC/SKIP-LOCKED races, outbox); e2e через api-gateway (newman).

## Принятые архитектурные решения и известные ограничения

Перечень осознанно выбранных компромиссов и того, что закрыто на DB-/код-уровне:

- **AuthN/AuthZ.** Per-RPC authz-gate через `InternalIAMService.Check` на **обоих**
  листенерах; строгость посадки задаётся `KACHO_VPC_AUTH_MODE`
  (`dev`/`production`/`production-strict`, `internal/apps/kacho/config/mode.go`).
  Личность вызывающего приходит метаданными, право её прислать сужается непустым
  кругом отправителей (`cmd/vpc/trusted_forwarders_test.go`).
- **Internal listener `:9091`.** mTLS — не план, а условие старта: посадку проверяет
  `Config.Validate` (`internal/apps/kacho/config/validate.go`,
  `ValidateServerMTLS`/`ValidatePeerTransport`), в production-режиме сервис
  **отказывается стартовать** без mTLS на живом ребре (`cmd/vpc/boot_refusal_test.go`).
- **Connection pool.** `KACHO_VPC_DB_MAX_CONNS` прокидывается в DSN только для pgxpool;
  `migrate` использует отдельный `MigrateDSN` без этого параметра (иначе `database/sql`
  шлет серверу неизвестный PG-параметр → `FATAL`).
- **TLS к kaname / БД.** `KACHO_VPC_IAM_TLS`, `KACHO_VPC_DB_SSLMODE`. `disable`
  допустим только во внутрипроцессных фикстурах; на **любом развёрнутом** стенде
  boot-guard требует не-`disable` (`security.md` §Production-mode обязателен ВЕЗДЕ).
- **Default SG.** Inline-создание default-SG включается конфигом
  группа правил по умолчанию создаётся БЕЗУСЛОВНО — настройки, которая бы это отменяла, нет;
  значение передаётся в use-case из композиционного корня
  (`NewCreateNetworkUseCase(..., cfg.Network.DefaultSGInline)`). Отдельного сеттера
  репозитория для этого нет — прежняя редакция называла здесь функцию, которой в
  дереве не существует.
- **Graceful shutdown воркеров.** `operations.Run` использует pkg-level registry с
  `sync.WaitGroup` + `recover`; `operations.Wait(ctx)` ждет активных воркеров на shutdown.
  Бюджет дренажа **выводится**, а не выписан константой: `cmd/vpc/main.go` строит
  `drainCtx` на `3*gracefulTimeout`, где `gracefulTimeout` = `api-server.graceful-shutdown`
  (умолчание 10s) — то есть 30s при умолчании и втрое от заданного при любом другом.
- **IPv6 allocator.** Sparse counter-based (материализованный freelist на /64 нереален —
  18 квинтиллионов адресов); двухфазный sweep → random для contention.

### Остаётся открытым (observability)

Перепись 2026-08-11 по дереву; предикат назван при каждом пункте, чтобы список нельзя
было унаследовать памятью:

- **распределённая трассировка** — `git grep -il 'otel\|opentelemetry' -- services/vpc`
  даёт **0**. Cascade resolve и аллокация адреса не трассируются между сервисами.
- **pprof** — `git grep -n pprof -- services/vpc` даёт **0**.

Что из прежнего списка **закрыто и потому снято** (иначе перечень читался бы как список
действующих долгов):

- Prometheus-метрики — `internal/observability/metrics/metrics.go`, приватный реестр,
  отдельный диагностический порт; снимает оба corelib-порта (`operations.Recorder`,
  `outbox/metrics.Recorder`) плюс `dependency_up`/`build_info`.
- mTLS на `:9091` — см. выше: условие старта, а не план.

Открытое ведётся GitHub Issues в `PRO-Robotech/kacho` с метками `observability-gap` /
`enhancement`.
