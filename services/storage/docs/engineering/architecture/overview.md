# kacho-storage — архитектурный обзор

Записаны осознанные дизайн-решения сервиса, чтобы не переоткрывать контекст.
Документ описывает РЕАЛИЗОВАННОЕ поведение: раздел «осталось сделать» здесь не
живёт — задел ведётся тикетами, а не доком, который никто не сверяет с деревом.

## Слои и dependency rule

Строго по `architecture.md`:

- `internal/domain` — чистый Go (stdlib), self-validating сущности. Без pgx/grpc.
- `internal/apps/kacho/api/<res>` — use-cases; объявляют порты (`volume.Reader`/`Writer`,
  `GeoClient`/`IAMClient`, `snapshot.Repo`, `disktype.Repo`). Импортируют domain +
  corelib `operations`; НЕ импортируют transport.
- `internal/repo/pg` — pgx-adapter, реализует порты. `internal/clients` — gRPC-adapter.
- `internal/handler` — тонкий transport (parse → use-case → format), регистрирует
  gen-сервисы. `cmd/storage` — единственное место wiring.
- `internal/errors` — pgx-free sentinel-семейство (leaf, тянется и use-case, и
  adapter'ами); `internal/apps/kacho/shared/serviceerr` — перевод sentinel в
  gRPC-статус; `internal/repo/repomock` — in-memory моки портов для unit-тестов
  use-case (Postgres не нужен).

Раскладка — та же, что у iam/vpc/nlb/registry/geo: слой бизнес-логики живёт под
`internal/apps/kacho/api/<resource>`, перевод ошибок — под
`internal/apps/kacho/shared/serviceerr`. Отдельного пакета «порты» нет by design:
каждый порт объявляет тот use-case, который им пользуется.

Все пути прошиты до pgx-адаптера. Единственный оставшийся анкер `Unimplemented` —
`Volume.GetInternal` (infra-проекция, ждёт data-plane; см. ниже).

## Async vs sync (api-conventions.md)

- `Volume`/`Snapshot`/`Image`: `Get`/`List` sync; `Create`/`Update`/`Delete` и действия
  `:verb` (`Volume:changeDiskType`, `Snapshot:copy`, `Image:copy`) → `Operation`.
- `DiskType`: `Get`/`List` sync (public); admin-регистрация (`InternalDiskTypeService`,
  включая `SetLifecycle`) СИНХРОННА (возвращает ресурс, не Operation — admin-справочник
  без LRO).
- `InternalStorageBackendService` / `InternalDiskTypeBindingService` — синхронны по той же
  причине (админ-плоскость каталога хранения, не LRO) и **несут REST-привязки**:
  `google.api.http` объявлен у каждого их RPC (`/storage/v1/storageBackends`,
  `/storage/v1/diskTypeBindings`), а край регистрирует оба сервиса **только** на
  cluster-internal мультиплексоре (`gateway/internal/restmux/mux.go`, ветка
  `storageInternalAddr`). На внешнем слушателе эти пути не обслуживаются: диспетчер
  классифицирует по принадлежности RPC `Internal*`-сервису, а не по форме пути. Они несут
  координату инфраструктуры (адрес кластера данных, имя пула, пространство имён) —
  публиковать их наружу запрещено (`security.md` §инфра-чувствительные данные, ban #6).
- `InternalImageService` (`GetInternal` / `Register`) — синхронен и **gRPC-only**:
  `google.api.http` не объявлен ни у одного его RPC, поэтому grpc-gateway строит
  unbound-маршрут по имени метода, и он тоже живёт только на внутреннем мультиплексоре.
  Тот же случай — `InternalVolumeService`.

**`Operation.done` = «намерение закоммичено», не «объект у бэкенда готов».** Ресурс
рождается в `CREATING`; готовность объявляет сверщик, сводя наблюдаемое с желаемым. Иначе
провижининг, длящийся дольше потолка исполнителя операций, дал бы ложное «готово» при
отсутствующем объекте — разрешитель осиротевших операций признаёт строку завершённой,
читая нашу БД, а не хранилище.

## Политика ресурса — ссылка на НЕИЗМЕНЯЕМУЮ ревизию (несущее свойство модели)

`disk_types` (продуктовая политика) изменяем; `disk_type_bindings` — append-only снимок
политики для пары (класс, зона), у которого **нет метода правки**, и это не пробел, а
механизм: ресурс хранит `binding_id` и потому не может задним числом сменить свойства при
правке справочника. Цель ссылки неизменяема by construction ⇒ джойн не «уезжает».

«Ровно одна ACTIVE-ревизия на пару» держит partial UNIQUE
`(disk_type_id, zone_id) WHERE status='ACTIVE'`, не read-then-write (ban #10): две
конкурентные регистрации иначе обе прошли бы чтение и обе записали.

Способности живут на ревизии (свойство бэкенда), публичный класс их **выводит
пересечением** действующих ревизий — консервативно, потому что класс предлагается в
нескольких зонах, а зоны могут обслуживаться разными бэкендами. На вход Create/Update
способности не принимаются: иначе у одного факта два источника, и разошлись бы они молча.

## Желаемое ≠ наблюдаемое — две колонки на трёх ресурсах

`state` (желаемое) и `observed_state` (наблюдённое) + `observed_at` + `status_reason` на
`volumes`/`snapshots`/`images`. Свести в одну колонку значит сделать дрейф невидимым by
construction; рабочий список сверщика — частичный индекс `WHERE state <> observed_state`,
а не очередь (очередь пришлось бы держать в согласии с реальностью, индекс из неё
выводится).

`used_bytes` — nullable: `NULL` = «бэкенд не сообщил», и это НЕ ноль. На wire поле
`optional`, потому что ноль здесь — утверждение «том пуст», по которому считают стоимость.

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
- **Source placement-coherence — ОТДЕЛЬНАЯ полоса того же INSERT.** Проект отвечает на
  «чей источник», размещение — на «откуда данные»; сливать их одним предикатом нельзя.
  Обе стороны засева проверяются симметрично и обе — внутри стейтмента вставки:
  Volume(ZONAL) ← Image(REGIONAL) требует, чтобы зона тома принадлежала региону образа
  (`Volume and Image must be in the same region`); Volume(ZONAL) ← Snapshot требует ТУ ЖЕ
  зону (`Volume and Snapshot must be in the same zone`), а Image(REGIONAL) ← Volume/Snapshot
  требует, чтобы зона источника входила в регион образа. Своей колонки размещения у
  снапшота нет — его единственное свидетельство это том, с которого он снят
  (`snapshots.source_volume_id`); происхождение занулено (`ON DELETE SET NULL`) ⇒
  сравнивать не с чем ⇒ полоса пропускает, а не выдумывает зону. Источник СВОЕГО проекта
  вызывающему уже виден (`Image.Get`/`Snapshot.Get` отдают его успехом), поэтому отказ
  по размещению **называется вслух**; источник ЧУЖОГО проекта решает полоса проекта
  ПЕРВОЙ и остаётся byte-identical настоящему промаху (hide-existence не ослаблено).
  Regression: `internal/repo/pg/volume_image_region_integration_test.go`,
  `services/storage/internal/repo/pg/volume_source_snapshot_zone_integration_test.go`, `services/storage/internal/repo/pg/image_source_region_integration_test.go`.
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
  per-object фильтр `services/storage/internal/authzfilter` (kaname
  `AuthorizeService.BatchCheck` через общий сужатель `pkg/listnarrow`, предикат
  `v_get` — то же отношение, которым каталог гейтит `Get`; батчи ≤100 ограниченным
  fan-out'ом) и отдаёт только видимые
  строки в порядке курсора. Без этого слоя ЛЮБОЙ член проекта видел каждый том,
  снимок и образ проекта независимо от per-object грантов (over-show / BOLA-lite),
  хотя `Get`/`Update`/`Delete` тех же ресурсов грант требовали — списки противоречили
  остальным путям. Видимость = Check-allow (read==enforce).
  Форма вопроса принципиальна: спрашивается «можно ли этому subject'у ЭТИ объекты»
  по прочитанной странице, а НЕ «перечисли всё разрешённое»: до стадии S6 вердикт
  давал внешний движок прав, и его перечислительный запрос нёс жёсткий предел без
  continuation-token'а, из-за которого собственный ресурс тенанта молча выпадал за
  предел. Приём снят у всех сервисов, а имена `ListAllowedIDs` / `ListObjects` стоят
  в списке запрещённых у `audit-list-filter`.
  Ошибка iam → **fail-closed** `UNAVAILABLE` (нефильтрованная страница не отдаётся
  никогда); запрос без caller-identity → пустая страница, не bypass. Фильтр
  конфигурируется `KACHO_STORAGE_LIST_FILTER_*`; production boot-guard
  (`config.Validate`) не пускает старт с `LIST_FILTER_ENABLED=false`.
  CI-гейт `make -C services/storage audit-list-filter` (`tools/audit-list-filter.sh`,
  продублирован в `go test ./tools/...`; цели с таким именем нет в корневом Makefile —
  она есть у каждого сервиса, поэтому голое `make audit-list-filter` из корня не
  исполняется, и CI вызывает именно `-C`-форму по каждому сервису,
  `.github/workflows/ci.yaml`, job `authz-artifacts`) роняет PR, если **тело**
  `repo.List` перестаёт сужать по
  `project_id`, use-case `List` перестаёт требовать непустой `projectId` **или**
  перестаёт фильтровать страницу per-object; `DiskType` (cluster-каталог
  `{cluster,*}`, per-object грантов не несёт) whitelisted.
- **`InternalVolumeService.GetInternal` — UNIMPLEMENTED анкер (§0.4).** infra-проекция
  тома: repo возвращает `ErrUnimplemented` → `codes.Unimplemented`. Номера инфра-полей в
  `VolumeInternal` зарезервированы и не заняты — заполнять проекцию нечем, а заглушка,
  отдающая пустую структуру, была бы хуже отказа: пустой ответ принимают за настоящий.
  Проекция образа (`ImageInternal`) наблюдаемые поля уже несёт: `binding_id`,
  `backend_object`, `observed_state`, `observed_at`, `status_reason`.

- **Multi-attach — предикат ревизии, а не форма ключа.** PK `volume_attachments` стал
  составным `(volume_id, instance_id)`; «сколько машин допускает том» читает
  `cap_multi_attach` ревизии **внутри** вставки. Прежний однополевой PK запрещал вторую
  привязку by construction — то есть способность бэкенда не могла быть реализована, каким
  бы он ни был. Смена PK — миграция на живой таблице, поэтому сделана сейчас, а не когда
  множественная привязка понадобится.

- **Снимок несёт СВОЙ `zone_id` (0014).** Прежде зона добиралась через
  `source_volume_id`, а эта ссылка обнуляется при удалении тома (`ON DELETE SET NULL` —
  снимок обязан пережить источник). Значит проверка когерентности при засеве из такого
  снимка вырождалась в тождественно-истинную: сравнивать не с чем, предикат проходит
  всегда — защита отказывала ровно в том случае, ради которого писалась, и молча. Backfill
  брал зону у живого источника; где ссылка уже обнулена, зона осталась ПУСТОЙ (честное
  «размещение неизвестно») и запрещает засев fail-closed.

- **`block_size` снят с контракта.** Поле принималось, хранилось, эхалось — и ни один путь
  не читал его значение; единственное упоминание в логике было в перечне immutable-полей
  маски, то есть охрана величины, которая ни на что не влияет (принято-и-проигнорировано,
  api-conventions). Номер и имя `reserved` навсегда; колонка остаётся в схеме со своим
  умолчанием и контрактом не адресуется.

## Зависимости — storage не отдельный модуль, а пакет монорепо

> [!note] Здесь стояла раскладка полирепо — у неё нет предмета (перемерено 2026-08-13)
> Прежняя редакция утверждала, что сервис пинит два прежних репозитория (контракты и общий
> фундамент) versioned-require, что локальная кросс-репо разработка идёт через git-ignored
> файл рабочего пространства Go по его отслеживаемому образцу, и называла хеш коммита,
> снявшего `replace`. **Ни одного из этих предметов в дереве нет:** `go.mod` в репозитории
> **один**, требований с именами прежних репозиториев в нём **ноль**, файла-образца
> рабочего пространства нет, а названный хеш не резолвится. Сами мёртвые имена и хеш здесь
> намеренно не воспроизводятся: цитата в обратных кавычках читается как координата — и
> проверкой свежести, и человеком, который по ней пойдёт.
>
> Предикаты, которыми это перемеряется: `git ls-files '*go.mod'` (→ 1),
> `grep -c '^replace' go.mod` (→ 0), `grep -c 'PRO-Robotech/kacho-' go.mod` (→ 0).

Части продукта — пакеты **одного** модуля `github.com/PRO-Robotech/kacho`, поэтому у
storage внутренних версионированных зависимостей нет by construction: контракты приезжают
из `pkg/api/kacho/cloud/storage/v1` (сгенерированы из `proto/`, руками не правятся), общий
фундамент — из `pkg/`. Порядок между ними — порядок импортов, а не пинов.

Запрет `replace github.com/PRO-Robotech/...` (`polyrepo.md`) остаётся нормой, но сегодня
**рассматривать ему нечего**: `replace` в дереве ноль. Не считать прохождение этого гейта
свидетельством — он тривиально зелёный. Правило вернётся к работе, если продукт снова
разъедется на полирепо.
