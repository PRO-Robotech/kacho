# kacho-storage

Kachō control-plane сервис домена **Storage**: `Volume` · `VolumeAttachment` · `Snapshot` ·
`Image` · `DiskType` · `StorageBackend` · `DiskTypeBinding`. Владелец домена
(database-per-service, схема `kacho_storage`).

> [!note] Здесь стоял статус «скелет, stub-хендлеры, бизнес-логика — отдельными задачами»
> Утверждение пережило свой предмет и продолжало обещать пустоту там, где лежит рабочий
> сервис. Перемерено 2026-08-13: заглушек в дереве **одна** — `Volume.GetInternal`
> (infra-проекция, анкер плоскости данных, см. `docs/engineering/architecture/overview.md`);
> все прочие пути прошиты до pgx-адаптера. Предикат (одна строка — один анкер):
> `grep -rn 'r\.notReady(' services/storage/internal --include=*.go | grep -v _test`.

## Поверхность API

| Листенер | Сервисы |
|---|---|
| public `:9090` | `VolumeService` (Get/List sync · Create/Update/Delete/ChangeDiskType async Operation · ListOperations), `SnapshotService` (Get/List · Create/Copy/Update/Delete async · ListOperations), `ImageService` (Get/List · Create/Copy/Update/Delete async · ListOperations), `DiskTypeService` (Get/List read-only), `OperationService` (LRO poll) |
| cluster-internal `:9091` | `InternalVolumeService` (Attach/Detach/ListAttachments/GetInternal — ребро compute→storage), `InternalImageService` (GetInternal/Register), `InternalDiskTypeService` (admin CRUD + SetLifecycle, sync), `InternalStorageBackendService` (CRUD, sync), `InternalDiskTypeBindingService` (Create/Get/List, sync — ревизия неизменяема, вытеснение = следствие создания), `OperationService` |
| diagnostic `:9095` | `/healthz`, `/metrics` |

`Internal*` — только на `:9091`, никогда на внешнем TLS endpoint (ban #6). REST-привязки у
`InternalStorageBackendService` / `InternalDiskTypeBindingService` **есть** (`google.api.http`
объявлен), но край регистрирует их только на cluster-internal мультиплексоре; остальные
`Internal*` сервисы http-опции не объявляют вовсе.

**Каталог классов диска НЕ посеян.** Миграция `0016_disk_type_policy.sql` сняла прежний
посев из `0004`: класс — регистрация того, что провайдер действительно даёт, поэтому
выдуманного каталога быть не должно. Пустой каталог — законное состояние (том не создаётся,
пока класс не зарегистрирован). Классы и действующие ревизии привязки заводит шаг подъёма
стенда `make -C deploy seed-storage`.

**`Operation.done` = «намерение закоммичено», НЕ «объект у плоскости данных существует».**
Ресурс рождается в намерении; пригодным его объявляет сверщик, увидев объект у бэкенда.

## Proto

Сервис `.proto` НЕ содержит. Контракты живут в `proto/kacho/cloud/storage/v1/`, Go-стабы
генерируются в `pkg/api/kacho/cloud/storage/v1` (руками не правятся) и импортируются оттуда.

## Runtime cross-domain edges

- `storage → geo` — валидация `zone_id` / `region_id` (`geo.v1.ZoneService.Get`,
  `RegionService.Get`, fail-closed).
- `storage → iam` — валидация `project_id` (`ProjectService.Get`) + per-RPC authz
  (`InternalIAMService.Check`) + `RegisterResource`/`UnregisterResource` (fga-proxy).
- `compute → storage` — `InternalVolumeService.Attach/Detach/ListAttachments`.

## Разработка

```bash
make build           # bin/storage
make build-migrator  # bin/kacho-migrator (goose up|down|status)
make test            # делегирует в корневой Makefile: test-service SVC=storage
make vet lint        # go vet + golangci-lint
```

Образ — multi-stage `Dockerfile`; его `COPY` и пути сборки
(`./services/storage/cmd/{storage,migrator}`) требуют контекстом **корень репозитория**:
`docker build -f services/storage/Dockerfile -t kacho-storage:dev .`

Все части продукта — пакеты одного модуля `github.com/PRO-Robotech/kacho`; siblings-репо и
`replace ../` в дереве нет. Подробности — `docs/engineering/architecture/overview.md`.

## Структура (Clean Architecture)

```
cmd/storage/       composition root (serve.go: дескриптор + регистрация) + main
cmd/migrator/      отдельный бинарь goose-миграций
internal/domain/   чистые сущности (stdlib), self-validating
internal/apps/kacho/api/<res>/     use-cases + их port-интерфейсы
internal/apps/kacho/shared/serviceerr/  sentinel → gRPC-статус
internal/errors/   sentinel-ошибки (pgx-free, leaf)
internal/repo/pg/  pgx-adapter (реализует порты)
internal/repo/repomock/  in-memory моки портов для unit-тестов use-case
internal/clients/  gRPC-клиенты geo/iam + опенер плоскости данных (реализуют порты)
internal/blockbackend/  плоскость данных: вид бэкенда + его объекты
internal/handler/  тонкий transport (public/internal/operation)
internal/authzfilter/   per-object фильтр страницы List (iam BatchCheck)
internal/fgaregister/   owner-tuple: outbox-эмиссия + register-drainer
internal/reconciler/    сведение желаемого с наблюдаемым (state ≠ observed_state)
internal/operationresolver/  разрешитель осиротевших операций
internal/check/    самопроверки посадки
internal/protoconv/ domain↔proto (timestamp truncate)
internal/observability/  логи/метрики/трассировка
internal/config/   KACHO_STORAGE_* через corelib config
internal/migrations/ goose SQL (embed)
deploy/            Helm chart
tools/             CI-гейты сервиса (audit-list-filter, audit-known-failing)
```
