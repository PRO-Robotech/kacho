# kacho-storage — архитектурный обзор (скелет)

Записаны осознанные дизайн-решения скелета (service-scaffolder), чтобы
`rpc-implementer` наполнял код без переоткрытия контекста.

## Слои и dependency rule

Строго по `architecture.md`:

- `internal/domain` — чистый Go (stdlib), self-validating сущности. Без pgx/grpc.
- `internal/service/<res>` — use-cases; объявляют порты (`volume.Reader`/`Writer`,
  `GeoClient`/`IAMClient`, `snapshot.Repo`, `disktype.Repo`). Импортируют domain +
  corelib `operations`; НЕ импортируют transport.
- `internal/repo/pg` — pgx-adapter, реализует порты. `internal/clients` — gRPC-adapter.
- `internal/handler` — тонкий transport (parse → use-case → format), регистрирует
  gen-сервисы. `cmd/storage` — единственное место wiring.

Скелет прошивает read-путь сквозняком (handler → use-case → adapter-заглушка) —
adapter-стабы возвращают `codes.Unimplemented`, поэтому wiring проверяем `go test`.

## Async vs sync (api-conventions.md)

- `Volume`/`Snapshot`: `Get`/`List` sync; `Create`/`Update`/`Delete` → `Operation`.
- `DiskType`: `Get`/`List` sync (public); admin CRUD (`InternalDiskTypeService`)
  СИНХРОНЕН (возвращает ресурс, не Operation — admin-справочник без LRO).

## CS-1 design decisions (реализовано — network-disk foundation)

Осознанные within-service инварианты CS-1 (все на DB-уровне / атомарным CAS, не
software TOCTOU — data-integrity.md ban #10):

- **Attach placement-coherence — ДВА раздельных текста (INV-4).** attach-CAS-предикат
  требует `volumes.zone_id = $instance_zone_id` **и** `volumes.project_id = $project_id`.
  disambiguation после 0-row CAS различает, какой предикат не сматчил, и отдаёт **свой**
  контрактный `FAILED_PRECONDITION`: расходится зона → `Volume and Instance must be in
  the same zone`; расходится проект → `Volume and Instance must be in the same project`
  (zone-текст НЕ переиспользуется — исправление относительно companion S2-04).
- **Auto device-name — retry-until-free (INV-2).** Пустой `deviceName` → repo выбирает
  первое свободное `sdb..sdz` и вставляет; конкурент, занявший имя между выбором и
  вставкой, даёт `23505` на `UNIQUE(instance_id,device_name)` → repo пересчитывает
  следующее свободное и повторяет (bounded ≤25). `23505` auto-пути наружу НЕ всплывает
  (в отличие от явного `deviceName` — там коллизия = контрактный
  `device <n> is already in use on Instance <id>`). Пространство исчерпано →
  `FAILED_PRECONDITION` `no free device name on Instance <id>`.
- **Public List — project-scoped Check (listauthz posture, INV-10).** `Volume.List`/
  `Snapshot.List` требуют `projectId`, gateway гейтит scope_extractor'ом
  `{project, project_id}`, а repo-запрос сужает строки по `project_id` → caller,
  авторизованный на `prj-1`, **никогда** не видит ресурсы `prj-2` by construction.
  Это **project-scoped Check** (AddressPool-style), а НЕ per-object `ListAllowedIDs`
  (как vpc/compute). **In-service backstop:** use-case `List` отвергает пустой
  `projectId` синхронно (`INVALID_ARGUMENT` `projectId is required`) первым
  стейтментом — иначе пустой scope вернул бы строки ВСЕХ проектов (repo сужает лишь
  при `ProjectID != ""`), поэтому «by construction» держится и без gateway-скоупинга.
  CI-гейт `make audit-list-filter` (`tools/audit-list-filter.sh`) роняет PR, если
  **тело** `repo.List` перестаёт сужать по `project_id` **или** use-case `List`
  перестаёт требовать непустой `projectId`; `DiskType` (cluster-каталог `{cluster,*}`)
  whitelisted.
- **`InternalVolumeService.GetInternal` — UNIMPLEMENTED анкер (§0.4).** infra-проекция
  (`VolumeInternal`: backend-LUN/pool/node/числовой инфра-id) — будущий data-plane
  инкремент; в CS-1 repo возвращает `ErrUnimplemented` → `codes.Unimplemented`. Это
  осознанный out-of-scope, НЕ tech-debt (data-plane отсутствует).

## Зависимости go.mod — versioned modules (без `replace`)

`kacho-storage` пинит `kacho-proto`/`kacho-corelib` **versioned-require** (pseudo-version),
без `replace github.com/PRO-Robotech/...` (polyrepo.md non-negotiable — `replace ../` не
резолвится при single-repo checkout CI/Docker). Локальная кросс-репо разработка — через
gitignored root `go.work` (`use ./kacho-*`); CI его не видит → versioned require.
`replace`-директивы сняты (drop-replace, commit `e6d67c8`).

## Осталось для rpc-implementer (НЕ в скелете)

- Доменные миграции: `volumes` (FK `disk_type_id`, size increase-only CHECK,
  placement-coherence zone), `volume_attachments` (attach-CAS, FK RESTRICT к volumes),
  `snapshots` (FK `source_volume_id`), `disk_types`. + встроить corelib `operations`
  (`make sync-migrations` → `internal/migrations/common/`, ревью `db-architect-reviewer`).
- Repo-логика: handwritten pgx + sqlc-gen (`internal/repo/pg/gen`, `queries/`),
  attach — атомарный CAS (data-integrity.md), НЕ software TOCTOU.
- Use-case тела: LRO (`operations.Run` + writer в worker), update_mask discipline,
  malformed-id-first, peer-validate (geo/iam) с per-call deadline.
- Clients: реальный дозвон `geo.v1.ZoneService.Get` / `iam.v1.ProjectService.Get`.
- Authz: подключить `InternalIAMService.Check` в `authz*Interceptor` (оба листенера,
  fail-closed) — сейчас passthrough-анкер (security.md инвариант).
- Тесты: integration (testcontainers, concurrent attach-CAS race) + newman e2e.
- Public RPC → регистрация через `api-gateway-registrar`.
