# kacho-storage

Kachō control-plane сервис домена **Storage**: `Volume` · `VolumeAttachment` ·
`Snapshot` · `DiskType`. Владелец домена (database-per-service, схема `kacho_storage`).

> **Статус:** скелет (service-scaffolder). Слои Clean Architecture, stub-хендлеры,
> composition root, build/CI/deploy на месте; бизнес-логика (RPC end-to-end) —
> отдельными задачами `rpc-implementer` по строгому TDD.

## Поверхность API

| Листенер | Сервисы |
|---|---|
| public `:9090` | `VolumeService` (Get/List sync · Create/Update/Delete async Operation · ListOperations), `SnapshotService` (Get/List · Create/Update/Delete async), `DiskTypeService` (Get/List read-only), `OperationService` (LRO poll) |
| cluster-internal `:9091` | `InternalVolumeService` (Attach/Detach/ListAttachments/GetInternal — ребро compute→storage), `InternalDiskTypeService` (admin CRUD, sync), `OperationService` |
| diagnostic `:9095` | `/healthz` |

`Internal*` — только на `:9091`, никогда на внешнем TLS endpoint (ban #6).

## Proto

Сервис `.proto` НЕ содержит — импортирует stubs
`github.com/PRO-Robotech/kacho-proto/gen/go/kacho/cloud/storage/v1`. Все определения
живут в `kacho-proto`.

## Runtime cross-domain edges

- `storage → geo` — валидация `zone_id` (`geo.v1.ZoneService.Get`, fail-closed).
- `storage → iam` — валидация `project_id` (`ProjectService.Get`) + per-RPC authz
  (`InternalIAMService.Check`).
- `compute → storage` — `InternalVolumeService.Attach/Detach/ListAttachments`.

## Разработка

```bash
make build           # bin/storage
make build-migrator  # bin/kacho-migrator (goose up|down|status)
make test            # go test ./... -race
make vet lint        # go vet + golangci-lint
make sync-migrations # подтянуть corelib operations → internal/migrations/common/
make docker          # образ (контекст — родительский каталог; см. Dockerfile)
```

Локальная сборка резолвит siblings через `replace ../kacho-proto` +
`replace ../kacho-corelib` (go.mod). Подробности — `docs/architecture/overview.md`.

## Структура (Clean Architecture)

```
cmd/storage/       composition root (serve.go) + interceptors + main
cmd/migrator/      отдельный бинарь goose-миграций
internal/domain/   чистые сущности (stdlib), self-validating
internal/service/  use-cases + port-интерфейсы (анкеры rpc-implementer)
internal/ports/    sentinel-ошибки + portmock
internal/repo/pg/  pgx-adapter (реализует порты)
internal/clients/  gRPC-клиенты geo/iam (реализуют порты)
internal/handler/  тонкий transport (public/internal/operation)
internal/protoconv/ domain↔proto (timestamp truncate)
internal/config/   KACHO_STORAGE_* через corelib config
internal/migrations/ goose SQL (embed) + common/ (sync из corelib)
deploy/            Helm chart
```
