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
- **Source project-coherence — предикат в самом INSERT (анти-BOLA).** Ссылка «ресурс
  засеян из другого ресурса» (`images.source_snapshot_id`/`source_volume_id`,
  `volumes.source_snapshot_id`/`source_image_id`, `snapshots.source_volume_id`) обязана
  указывать на строку **того же проекта**. Голого FK недостаточно — он проверяет лишь
  существование, поэтому caller мог подставить id ЧУЖОГО приватного тома/снапшота/образа
  и материализовать его содержимое в свой ресурс (cross-project data disclosure). Предикат
  `AND <src>.project_id = $project` живёт **в том же INSERT…SELECT**, что и вставка
  (атомарно, не software check-then-act — ban #10). 0-row исход дизамбигуируется в
  контрактный `FAILED_PRECONDITION` `"<Resource> <id> not found"` — **byte-identical**
  настоящему miss'у (hide-existence, security.md §6): «чужой ресурс существует»
  неотличимо от «ресурса нет». По той же причине disambiguation снапшота резолвит том
  project-scoped — иначе чужой не-READY том выдавал бы себя текстом `is not ready`
  (state-oracle). Regression: `internal/repo/pg/source_project_coherence_integration_test.go`.
- **Auto device-name — retry-until-free (INV-2).** Пустой `deviceName` → repo выбирает
  первое свободное `sdb..sdz` и вставляет; конкурент, занявший имя между выбором и
  вставкой, даёт `23505` на `UNIQUE(instance_id,device_name)` → repo пересчитывает
  следующее свободное и повторяет (bounded ≤25). `23505` auto-пути наружу НЕ всплывает
  (в отличие от явного `deviceName` — там коллизия = контрактный
  `device <n> is already in use on Instance <id>`). Пространство исчерпано →
  `FAILED_PRECONDITION` `no free device name on Instance <id>`.
- **Public List — project-scope И per-object видимость (два слоя, INV-10).**
  `Volume.List`/`Snapshot.List`/`Image.List` требуют `projectId`; gateway гейтит его
  scope_extractor'ом `{project, project_id}`, repo-запрос сужает строки по
  `project_id`, а use-case отвергает пустой `projectId` синхронно
  (`INVALID_ARGUMENT` `projectId is required`) — иначе пустой scope вернул бы строки
  ВСЕХ проектов (repo сужает лишь при `ProjectID != ""`). Этот слой закрывает
  **кросс-проектную** утечку by construction.
  **Но project-scope отвечает лишь «чей это проект», не «какие объекты этому caller'у
  можно».** Поэтому use-case, прочитав СТРАНИЦУ курсором, прогоняет её id через
  per-object фильтр `internal/authzfilter` (kacho-iam `AuthorizeService.BatchCheck`,
  `viewer ∪ v_list`, батчи ≤100 ограниченным fan-out'ом) и отдаёт только видимые
  строки в порядке курсора. Без этого слоя ЛЮБОЙ член проекта видел каждый том,
  снимок и образ проекта независимо от per-object грантов (over-show / BOLA-lite),
  хотя `Get`/`Update`/`Delete` тех же ресурсов грант требовали — списки противоречили
  остальным путям. Видимость = Check-allow (read==enforce).
  Форма вопроса принципиальна: спрашивается «можно ли этому subject'у ЭТИ объекты»
  по прочитанной странице, а НЕ `ListAllowedIDs` («перечисли всё разрешённое» —
  у OpenFGA `ListObjects` жёсткий предел без continuation-token'а, из-за которого
  собственный ресурс тенанта молча выпадал за префикс; приём снят у всех сервисов).
  Ошибка iam → **fail-closed** `UNAVAILABLE` (нефильтрованная страница не отдаётся
  никогда); запрос без caller-identity → пустая страница, не bypass. Фильтр
  конфигурируется `KACHO_STORAGE_LIST_FILTER_*`; production boot-guard
  (`config.Validate`) не пускает старт с `LIST_FILTER_ENABLED=false`.
  CI-гейт `make -C services/storage audit-list-filter` (`tools/audit-list-filter.sh`,
  продублирован в `go test ./tools/...`; корневого Makefile в репо нет, поэтому голое
  `make audit-list-filter` из корня не исполняется — CI вызывает именно `-C`-форму,
  `.github/workflows/ci.yaml`, job `authz-artifacts`) роняет PR, если **тело**
  `repo.List` перестаёт сужать по
  `project_id`, use-case `List` перестаёт требовать непустой `projectId` **или**
  перестаёт фильтровать страницу per-object; `DiskType` (cluster-каталог
  `{cluster,*}`, per-object грантов не несёт) whitelisted.
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
