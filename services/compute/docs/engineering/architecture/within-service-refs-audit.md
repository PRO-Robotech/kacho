# Within-service refs audit — DB-уровневое покрытие constraints (KAC-85)

> [!warning] Исторический документ. Схема, которую он аудирует, с тех пор разобрана
>
> Аудит снят **2026-05-15** с схемы `kacho_compute` того времени. К **2026-08-01**
> предмет большей части находок физически удалён из сервиса: блочное хранение
> ретайрено в пользу `kacho-storage`, Geography вынесена в `kacho-geo`. Ни одна из
> заявленных ниже «High»-находок сегодня не описывает живой код.
>
> Документ сохраняется как **trail метода и чисел** (какие рёбра проверялись, по
> какому правилу, что было закрыто конструкцией базы, а что признано by-design), а
> не как список задач. Перед тем как заводить работу по любому `G<n>` — сверься со
> статусом в §3: он перепроверен по дереву.
>
> Пошаговые сценарии гонок и предлагавшиеся миграции из §2 **сняты**: они описывали
> таблицы и функции, которых больше нет, то есть были одновременно неверны и
> избыточно подробны для публичного репозитория (`security.md` — признак
> восстановимости). Предмет находки, её класс и итог сохранены.

> **Контекст**
>
> Этот документ — полный аудит ссылочных полей и инвариантов всех таблиц схемы
> `kacho_compute` против правила workspace `CLAUDE.md` § «Within-service refs —
> DB-уровень обязателен» (запрет #10): любая ссылочная зависимость **внутри одной
> БД сервиса** и любой инвариант должны быть зафиксированы на уровне Postgres-
> constraint (FK / partial UNIQUE / EXCLUDE / CHECK / atomic conditional UPDATE
> с CAS / `FOR UPDATE SKIP LOCKED`). Software-side `Get → check → Update`
> запрещён — это TOCTOU-prone (см. инцидент 2026-05-14, KAC-52 в kacho-vpc).
>
> Источник истины **на момент аудита**:
> - Миграции `internal/migrations/0001_initial.sql` (squashed baseline) + `0002..0005`.
> - Слой use-case `internal/apps/kacho/api/<resource>/*.go` (software-prechecks как UX-layer).
> - Repo-слой `internal/repo/*.go` (маппинг ошибок в sentinel-errors).
>
> Парный аудит для kacho-vpc — `services/vpc/docs/engineering/architecture/within-service-refs-audit.md`
> (KAC-84).
>
> **Cross-service ссылки** (`project_id` → kaname;
> `instance_network_interfaces.subnet_id` / `security_group_ids` /
> `primary_v4_nat.address_id` → kacho-vpc) — **out of scope**: для них DB-уровневые
> FK невозможны (`database-per-service` запрет #8), валидация делается через
> peer-API + грациозный dangling-ref.
>
> **Историческая правка (KAC-265)**: на момент аудита coverage включал таблицы
> kube-ovn-эпохи (`0004_hypervisors.sql`) с пометкой «pending drop». Они удалены в
> KAC-36/79/80 (`0006_drop_hypervisors.sql`); строки coverage и gap'ы сняты.

---

## Summary

Состояние **на момент аудита (2026-05-15)**:

- **Проверено**: 8 ресурсных/служебных таблиц схемы `kacho_compute`.
- **Покрыто DB-уровнем** (FK / partial UNIQUE / EXCLUDE / CHECK / CAS / SKIP LOCKED): большинство рёбер.
- **Gap'ов выявлено**: **12** (G1–G11, G14; G12/G13 относились к удалённым таблицам и сняты).
- Из них: 5 предлагалось закрыть DB-уровнем (G1/G3/G4/G5/G9), 3 признаны by-design (G6/G7/G8),
  2 — code-only / doc-only (G2/G14), 1 уже был закрыт (G11), 1 — cross-service N/A (G10).

Состояние **на 2026-08-01** (перепроверено по дереву, см. §3):

- **0** находок описывают живой дефект.
- **1** закрыта кодом (G2 — переход состояния машины стал атомарным).
- **4** утратили предмет вместе с таблицами (G1/G3/G4/G5).
- **1** утратила предмет частично (G9 — enum-колонки удалённых таблиц).
- **3** остаются by-design и по-прежнему верны как описание решения (G6/G7/G8).

| #   | Gap (формулировка аудита) | Severity (тогда) | Тип нарушения | Статус 2026-08-01 |
|-----|-----|----------|----------------|---|
| G1  | Disk-attach: привязка диска к машине не была защищена уникальностью на уровне базы | **High** | Missing partial UNIQUE + TOCTOU | **Предмета нет** |
| G2  | Переход состояния машины писался безусловно, а не сверкой с ожидаемым | **High** | TOCTOU lost-write | **Закрыт** |
| G3  | `instances.zone_id` → `zones(id)` без FK | **High** | Missing within-service FK | **Предмета нет** |
| G4  | `disks.zone_id` → `zones(id)` без FK | **High** | Missing within-service FK | **Предмета нет** |
| G5  | `disks.type_id` → `disk_types(id)` без FK | Medium | Missing within-service FK | **Предмета нет** |
| G6  | `disks.source_image_id` / `source_snapshot_id` без FK — **by-design** soft-ref | Info | Documented divergence | Верно как решение |
| G7  | `images.source_*_id` без FK — **by-design** | Info | Documented divergence | Верно как решение |
| G8  | `snapshots.source_disk_id` без FK — **by-design** | Info | Documented divergence | Верно как решение |
| G9  | Enum-like колонки без CHECK | Low | Missing CHECK | Частично без предмета |
| G10 | Cross-service refs NIC → kacho-vpc, FK невозможен | N/A | N/A | Верно как решение |
| G11 | «Ровно один boot-диск на машину» — уже был enforced | OK | — | Closed (предмета нет) |
| ~~G14~~ | ~~Move с conflict по `(project_id, name)`~~ | — | — | Снято (RPC удалены, KAC-266) |

> G12/G13 (таблицы kube-ovn-эпохи) сняты: удалены в KAC-36/79/80.

Полная таблица coverage — ниже; она описывает схему **на момент аудита**.

---
## 1. Полная таблица coverage

> Снимок схемы **на 2026-05-15**. Большинство перечисленных таблиц с тех пор
> удалено (см. §3) — таблица сохраняется как запись о том, что проверялось.

Колонки таблицы:
- **Resource.field / invariant** — что проверяем.
- **Что гарантируется** — продуктовый инвариант.
- **DB constraint** — Postgres-механизм (✅ есть / ❌ отсутствует / N/A — cross-service).
- **Software check** — есть ли дублирующий software-precheck (для UX).
- **Решение** — OK / G<n> (отсылка к gap-секции) / N/A.

### 1.1 `zones`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `zones_pkey` ✅ | n/a | OK |
| `region_id` | существует в `regions` | `zones_region_id_fkey` FK ON DELETE RESTRICT ✅ (0003) | sync (region admin-tooling редкий путь) | OK |
| `name` | произвольный display name | NOT NULL DEFAULT '' ✅ | n/a | OK |
| `status` (TEXT: UP / DOWN / STATUS_UNSPECIFIED) | значение из enum | ❌ нет CHECK | sync mapping в `zoneStatusFromName` | **G9** (minor) |
| `(zone)` deletable если нет dependents (instances/disks/disk_types) | software-precheck в `ZoneService.Delete` (через handler) | ❌ нет FK на zones из instances/disks (G3/G4); FK на disk_types отсутствует тоже | sync precheck в handler/service Delete | **G3/G4** (см. ниже) |

### 1.2 `regions`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `regions_pkey` ✅ (0003) | n/a | OK |
| `name` | произвольный display name | NOT NULL DEFAULT '' ✅ | n/a | OK |
| `(region)` deletable если нет zones | `zones_region_id_fkey` FK RESTRICT ✅ | sync `CountZones` precheck в `RegionService.Delete` | OK |

### 1.3 `disk_types`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный (литерал: `network-ssd`, `network-hdd`, ...) | `disk_types_pkey` ✅ | n/a | OK |
| `zone_ids` (JSONB array) | каждый id существует в `zones(id)` | ❌ нет FK (jsonb array нельзя FK напрямую) | sync: НЕТ проверки в `DiskTypeService.Create/Update` | acceptable (admin-only ресурс; raw INSERT мусора маловероятен; при ошибочном zone_id в zone_ids — disk типа просто не доступен в этой зоне, не fatal) |

### 1.4 `disks`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `disks_pkey` ✅ | n/a | OK |
| `project_id` (id владельца-проекта) | существует в kaname | N/A (cross-service) | `ProjectClient.Exists` в `service.checkProject` | OK (cross-service) |
| `(project_id, name)` | уникальный non-empty | `disks_project_name_uniq` partial UNIQUE WHERE `name <> ''` ✅ | n/a | OK |
| `zone_id` | существует, RESTRICT удаления зоны | ❌ **нет FK** | sync `zones.GetZone` в `DiskService.doCreate` | **G4** |
| `type_id` | существует в `disk_types(id)` | ❌ **нет FK** | sync `diskTypeRepo.Get` в `DiskService.doCreate` | **G5** |
| `source_image_id` (nullable, '' = none) | если задан — existed at create time | ❌ нет FK (by-design soft-ref: source can be deleted) | sync `imageRepo.Get` в `doCreate` | **G6** (by-design, documented) |
| `source_snapshot_id` (nullable, '' = none) | если задан — existed at create time | ❌ нет FK (by-design) | sync `snapshotRepo.Get` в `doCreate` | **G6** (by-design) |
| `size` ≥ `min_disk_size` source-image / `disk_size` source-snapshot | sync-only при Create | sync в `doCreate` | n/a | OK (immutable после Create — нет race) |
| `status` (TEXT: CREATING/READY/ERROR/DELETING) | значение из enum | ❌ нет CHECK | sync mapping | **G9** (minor) |
| `(disk)` deletable если не attached | `attached_disks.disk_id` FK ON DELETE RESTRICT ✅ (23503→ErrFailedPrecondition в `wrapPgErr`) | sync `IsAttached` precheck в `DiskService.Delete` | OK (DB FK даёт реальную защиту; software-precheck — UX) |
| `Relocate` (zone change) precondition: not attached | software `IsAttached` check; нет CAS на `disks.zone_id` | sync precheck | **G2-like (Disk.Relocate)** — sub-case G2 (см. ниже) |

### 1.5 `images`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `images_pkey` ✅ | n/a | OK |
| `project_id` (id владельца-проекта) | существует в kaname | N/A (cross-service) | `ProjectClient.Exists` | OK (cross-service) |
| `(project_id, name)` | уникальный non-empty | `images_project_name_uniq` partial UNIQUE WHERE `name <> ''` ✅ | n/a | OK |
| `(project_id, family, created_at desc)` ordering для GetLatestByFamily | индекс есть | `images_family_idx` ✅ | n/a | OK |
| `family` | regex `^([a-z][-a-z0-9]{1,61}[a-z0-9])?$` | ❌ нет CHECK | sync `validateImageFamily` в `ImageService.Create` | acceptable (immutable после Create; нет raw-INSERT admin-path) |
| `source_image_id` (nullable) | если задан — existed at create time | ❌ нет FK (by-design soft-ref: source can be deleted) | sync `imageRepo.Get` в `doCreate` | **G7** (by-design) |
| `source_snapshot_id` (nullable) | если задан — existed at create time | ❌ нет FK (by-design) | sync `snapshotRepo.Get` | **G7** (by-design) |
| `source_disk_id` (nullable) | если задан — existed at create time | ❌ нет FK (by-design) | sync `diskRepo.Get` | **G7** (by-design) |
| `status` (TEXT: CREATING/READY/ERROR/DELETING) | значение из enum | ❌ нет CHECK | sync mapping | **G9** (minor) |
| `os_type` (TEXT: LINUX/WINDOWS/TYPE_UNSPECIFIED) | значение из enum | ❌ нет CHECK | sync mapping | **G9** (minor) |

### 1.6 `snapshots`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `snapshots_pkey` ✅ | n/a | OK |
| `project_id` (id владельца-проекта) | существует в kaname | N/A (cross-service) | `ProjectClient.Exists` | OK (cross-service) |
| `(project_id, name)` | уникальный non-empty | `snapshots_project_name_uniq` partial UNIQUE WHERE `name <> ''` ✅ | n/a | OK |
| `source_disk_id` | existed at create time, Disk был READY | ❌ нет FK (by-design soft-ref: source disk can be deleted) | sync `diskRepo.Get` + status check в `SnapshotService.doCreate` | **G8** (by-design) |
| `source_disk_idx` для observability | `snapshots_source_disk_idx` partial WHERE `source_disk_id <> ''` ✅ | n/a | OK |
| `status` (TEXT) | значение из enum | ❌ нет CHECK | sync | **G9** (minor) |

### 1.7 `instances`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `id` PK | уникальный | `instances_pkey` ✅ | n/a | OK |
| `project_id` (id владельца-проекта) | существует в kaname | N/A (cross-service) | `ProjectClient.Exists` в `service.checkProject` | OK (cross-service) |
| `(project_id, name)` | уникальный non-empty | `instances_project_name_uniq` partial UNIQUE WHERE `name <> ''` ✅ | n/a | OK |
| `zone_id` | существует, immutable после Create | ❌ **нет FK** | sync `zones.GetZone` в `doCreate` | **G3** |
| `status` (TEXT: PROVISIONING/RUNNING/STOPPING/STOPPED/STARTING/RESTARTING/UPDATING/ERROR/CRASHED/DELETING) | значение из state-машины (см. CLAUDE.md §8) | ❌ нет CHECK на enum; ❌ переходы делаются `Get → if status != from → SetStatus` без CAS | sync precondition-check в `InstanceService.lifecycle/AttachDisk/DetachDisk/AddOneToOneNat/RemoveOneToOneNat/Update touchesCompute` | **G2** + **G9** (TOCTOU на state + missing CHECK) |
| ~~`Move(dest_project_id)`~~ | снято — RPC `Instance.Move` удалён в KAC-266 (контракт-removal) | n/a | n/a | Closed (removed) |
| `(instance)` cascade на NIC / attached_disks | `instance_network_interfaces.instance_id` FK CASCADE ✅; `attached_disks.instance_id` FK CASCADE ✅ | n/a | OK |
| `metadata` ≤ 256 KiB | sync-validation | ❌ нет CHECK на `octet_length(metadata::text)` | sync в request-path | acceptable (sync на API-уровне; raw INSERT — admin edge case) |

### 1.8 `instance_network_interfaces`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `(instance_id, idx)` PK | уникальный NIC-index в пределах instance | `instance_network_interfaces_pkey` ✅ | n/a | OK |
| `instance_id` → `instances(id)` | существует, CASCADE при delete instance | FK ON DELETE CASCADE ✅ | n/a | OK |
| `subnet_id` | существует subnet в kacho-vpc | N/A (cross-service) | `Instance.Create` строк интерфейсов не заводит, но подсеть **проверяет** peer-вызовом (зональная когерентность) | OK (cross-service: FK через границу запрещён) |
| `subnet_idx` для cascade-check на Subnet.Delete | `instance_nic_subnet_idx` partial WHERE `subnet_id <> ''` ✅ | n/a | OK |
| `security_group_ids` (JSONB array) | каждый id existed in kacho-vpc at attach time | N/A (cross-service) | n/a — NIC не создаётся на Create (auto-NIC удалён в KAC-266) | OK (cross-service) |
| `primary_v4_address_id` (TEXT, '' = none) | если задан — Address-ресурс в kacho-vpc | N/A (cross-service) | (создан в `vpcClient.CreateInternalAddress`, не валидируется на чтение) | OK (cross-service) |
| `nic_id` (TEXT, '' = legacy/skipPeer) | если задан — kacho-vpc NetworkInterface ресурс | N/A (cross-service) | `vpcClient.GetNetworkInterface` в `attachExistingNIC` (с CAS на vpc-side, KAC-52) | OK (cross-service; защита от race — на vpc-стороне через partial UNIQUE + atomic CAS) |
| `mac_address` (TEXT, '' = unset) | unique в пределах cloud (если задан); compute не enforce'ит сам — это vpc-домен | N/A (cross-service NIC owner) | n/a | OK |
| `primary_v4_nat` (JSONB, nullable) | OneToOneNat ref на vpc Address | N/A (cross-service) | sync в `resolveNatAddress` | OK |

### 1.9 `attached_disks`

| Resource.field / invariant | Что гарантируется | DB constraint | Software check | Решение |
|---|---|---|---|---|
| `(instance_id, disk_id)` PK | один disk прикреплён к одному instance максимум 1 раз | `attached_disks_pkey` ✅ | n/a | OK |
| `instance_id` → `instances(id)` | существует, CASCADE при delete instance | FK ON DELETE CASCADE ✅ | n/a | OK |
| `disk_id` → `disks(id)` | существует, RESTRICT при delete disk | FK ON DELETE RESTRICT ✅ | sync `IsAttached` precheck в `Disk.Delete` (UX); 23503→ErrFailedPrecondition backstop | OK |
| `(instance_id) WHERE is_boot` | один boot disk на instance | `attached_disks_boot_uniq` partial UNIQUE ✅ | sync `target.IsBoot` check в `DetachDisk` | OK |
| `(instance_id, device_name) WHERE device_name <> ''` | unique device_name в пределах instance | `attached_disks_device_uniq` partial UNIQUE ✅ | sync check duplicate device_name в `AttachDisk` (UX) | OK |
| `disk_id` invariant «один disk → 0 или 1 instance» (i.e. не может быть в `attached_disks` дважды для разных instance) | ❌ **нет partial UNIQUE on `disk_id`** — двое разных instances могут параллельно вставить `(instA, diskX)` и `(instB, diskX)` если оба прошли software `IsAttached(diskX) == false` (TOCTOU) | sync `IsAttached` в `InstanceService.resolveDiskSource` / `AttachDisk` (через service-слой); 23503-RESTRICT при delete disk не спасает от двойного attach | **G1** (high — parity с KAC-52) |
| `mode` (TEXT: READ_ONLY/READ_WRITE/MODE_UNSPECIFIED) | значение из enum | ❌ нет CHECK | sync mapping в `attachedDiskModeName` | **G9** (minor) |

### 1.10 `operations`, `compute_outbox`

| Table.field / invariant | Что гарантируется | DB constraint | Решение |
|---|---|---|---|
| `operations.id` PK | уникальный | `operations_pkey` ✅ | OK |
| `compute_outbox.sequence_no` PK + BIGSERIAL | строго возрастающий, уникальный | `PRIMARY KEY` + sequence default ✅ | OK |
| `compute_outbox_notify_trg` AFTER INSERT | каждый INSERT → `pg_notify('compute_outbox', sequence_no)` | trigger ✅ | OK |
| outbox row atomicity с ресурс-row | в одной tx | все `emitCompute` вызовы — в той же tx, что INSERT/UPDATE ресурса; review-rule ✅ | OK |

---

## 2. Детализация gap'ов

> Ниже — предмет каждой находки и её судьба. Пошаговые сценарии гонок, листинги
> тогдашнего кода и тексты предлагавшихся миграций **сняты намеренно**: описываемых
> ими таблиц и функций в сервисе больше нет, поэтому текст был бы неверен, а его
> форма (координата + условие + следствие в одной сборке) запрещена дисциплиной
> публичного репозитория. Проверить любую строку ниже можно по миграциям, которые
> названы поимённо.

### G1 — привязка диска к машине без уникальности на уровне базы

**Severity на момент аудита**: High.

**Предмет.** Инвариант «один диск прикреплён максимум к одной машине» держался
только software-прекчеком в сервисном слое, а вставка строки привязки шла
безусловно. Составной первичный ключ таблицы привязок этот инвариант не выражал:
он запрещал повтор пары «машина+диск», а не второй машине забрать тот же диск.
Класс — точный аналог инцидента KAC-52 в kacho-vpc (NIC-attach race).
Предлагавшееся закрытие — partial UNIQUE по диску + маппинг 23505 в контрактный
`FailedPrecondition`.

**Статус 2026-08-01: предмета нет.** Таблица привязок дропнута миграцией
`0013_drop_attached_disks.sql` (drop зарегистрирован в `dropguard.json`); сам
ресурс Disk ретайрен в `0021_drop_block_storage_duplicates.sql`. Привязка тома к
машине живёт теперь у владельца — `volume_attachments` в `kacho-storage`, и
инвариант там выражен CAS'ом на стороне владельца. Compute локальной строки
привязки не пишет вовсе: `AttachDisk` пробрасывает вызов в storage.

**Что осталось открытым рядом и НЕ является G1.** Прекчек перед пробросом —
read-only, он **сужает, а не закрывает** гонку «привязка против удаления»:
между прекчеком и ответом владельца машина может уйти в удаление. Это признанный
остаток, названный в godoc у самого прекчека (комментарий у гейта обязан называть,
что он не закрывает, — иначе следующий снимет его как избыточный). Отдельная
работа со своей приёмкой; направление и вход здесь намеренно не приводятся.

---

### G2 — переход состояния машины без сверки с ожидаемым

**Severity на момент аудита**: High.

**Предмет.** Lifecycle-RPC (Start / Stop / Restart и соседние) читали текущее
состояние, сверяли его в Go и затем писали новое **безусловно**. Между чтением и
записью состояние мог сменить конкурирующий вызов — классический lost-write:
оба вызывающих проходили проверку, второй затирал результат первого. Правило
запрета #10 требует выражать такой инвариант compare-and-swap'ом в самом
операторе записи, а не проверкой перед ним.

**Статус 2026-08-01: закрыт кодом.** Запись состояния выполняется единственным
условным оператором, сверяющим ожидаемое состояние в самом `WHERE`; ноль
затронутых строк разводится на «нет ресурса» и «состояние не то» отдельной
пробой. Не-CAS вариант сеттера из портов и репозитория **удалён**, то есть
обойти CAS нечем: интерфейс порта предлагает только его. Регрессия —
`internal/repo/instance_state_race_integration_test.go` (конкурентные
Stop-на-STOPPED, Restart-на-RUNNING, Stop-против-Restart, CAS-miss → NotFound),
плюс `instance_update_status_race_integration_test.go` и
`instance_resize_stopped_race_integration_test.go`.

---

### G3 — `instances.zone_id` → `zones(id)` без FK

**Severity на момент аудита**: High.

**Предмет.** После KAC-15 Geography (Region/Zone) жила **в самой БД compute**,
поэтому `instances.zone_id` был within-service ref и обязан был нести FK. Его не
было: индекс на колонке стоял, ограничения — нет. Следствие — админ мог удалить
зону, на которую ссылаются машины, и получить dangling-ref.

**Статус 2026-08-01: предмета нет — предпосылка инвертирована.** Geography
вынесена в `kacho-geo` (эпик kacho-workspace#82); миграция
`0011_drop_geography.sql` дропнула `zones` и `regions` из БД compute. `zone_id`
стал **cross-service** ссылкой, для которой DB-уровневый FK запрещён by
construction (запрет #8, database-per-service). Существование зоны валидируется
peer-вызовом `geo.v1.ZoneService.Get` через `internal/clients/geo_client.go`,
fail-closed. Остаточный риск — обычный класс cross-service dangling-ref с
грациозной деградацией, а не пропущенное ограничение.

> Формулировка предпосылки в исходном аудите («Geography перенесена **в**
> kacho-compute, таблица `zones` живёт в той же БД») с тех пор обратна
> действительности. Оставлена как есть в §1 — это снимок состояния 2026-05-15.

---

### G4 — `disks.zone_id` → `zones(id)` без FK

**Severity на момент аудита**: High. Тот же случай, что G3, но для дисков.

**Статус 2026-08-01: предмета нет дважды.** Целевая таблица `zones` дропнута
(`0011_drop_geography.sql`), и таблица-источник `disks` тоже —
`0021_drop_block_storage_duplicates.sql`. Зональность тома живёт теперь в
`kacho-storage` и проверяется на когерентность с регионом образа внутри
insert-CAS (см. `services/storage/docs/engineering/architecture/compute-storage-parity.md` §4).

---

### G5 — `disks.type_id` → `disk_types(id)` без FK

**Severity на момент аудита**: Medium. Тип диска immutable после создания,
поэтому реальная поверхность сводилась к админскому удалению типа под живыми
дисками.

**Статус 2026-08-01: предмета нет.** `disks` дропнута
(`0021_drop_block_storage_duplicates.sql`), `disk_types` — следом
(`0022_drop_disk_types.sql`, снят «последним из четырёх, потому что его читал
только ретайренный Disk»). Каталог типов — нативный у storage.

---

### G6 — `disks.source_image_id` / `disks.source_snapshot_id` без FK (by-design)

**Severity**: Info — осознанное отступление от FK-правила.

**Решение.** Источник (Image / Snapshot) разрешено удалить, оставив созданный из
него диск: поле хранит «откуда создан» в observability-целях, а не живую
зависимость. FK заблокировал бы удаление источника, то есть выразил бы инвариант,
которого продукт не хочет. Формализовано в FK-контракте сервиса.

**Статус 2026-08-01.** Как описание решения — верно и переиспользовано: та же
семантика soft-ref действует у владельца-storage. Таблицы compute, на которых
находка была снята, удалены.

---

### G7 — `images.source_*_id` без FK (by-design)

**Severity**: Info. То же решение, что G6, для происхождения образа
(`source_image_id` / `source_snapshot_id` / `source_disk_id`): источник
удаляем, ссылка — происхождение, не зависимость.

---

### G8 — `snapshots.source_disk_id` без FK (by-design)

**Severity**: Info. То же решение: снимок переживает удаление диска, из которого
снят. FK сделал бы диск неудаляемым при живом снимке — не тот контракт.

---

### G9 — enum-like колонки без CHECK

**Severity**: Low — поверхность для прямых INSERT'ов «мусора»; в тогдашнем
сервисном потоке недостижимо (значения писал маппинг из proto-enum).

**Затрагивало** статусные и режимные текстовые колонки восьми таблиц: статусы
дисков / образов / снимков / машин / зон, тип ОС образа, тип сетевых настроек
машины, режим авторизации последовательного порта, политику обслуживания, режим
привязки диска.

**Статус 2026-08-01: частично без предмета.** Колонки дисков, образов, снимков,
зон и привязок ушли вместе с таблицами (`0011`, `0013`, `0021`, `0022`).
Остались статусные и режимные колонки `instances` — по ним находка формально жива
в классе Low и осознанно не закрывалась: предлагавшийся CHECK удорожает добавление
нового значения enum (расширять ограничение миграцией на каждое пополнение), а
писателя, кроме маппинга из proto, у колонок нет.

---

### G10 — cross-service refs `instance_network_interfaces.*` — N/A

Все ссылки NIC на ресурсы kacho-vpc (subnet, security group, address) — через
границу сервиса. FK невозможны (запрет #8). Покрытие — peer-валидация +
грациозный dangling-ref; защита от гонок живёт на стороне vpc (атомарный CAS
владения, partial UNIQUE на MAC; KAC-52, KAC-55). **Action**: ничего.

---

### G11 — «ровно один boot-диск на машину» — уже был closed

Инвариант был выражен partial UNIQUE по машине с предикатом «загрузочный» ещё в
исходной миграции. Включён в аудит для полноты — как доказательство, что не всякий
инвариант оказался gap'ом. Предмета сегодня нет (таблица привязок дропнута);
эквивалент живёт у владельца-storage.

---

### G12 / G13 — сняты (таблицы kube-ovn-эпохи удалены)

Относились к `hypervisors` / `hypervisor_node_index_free`. Таблицы и связанный
software-слой удалены в KAC-36/79/80 (`0006_drop_hypervisors.sql`).

---

### ~~G14 — Move с conflict по `(project_id, name)`~~ (снято, KAC-266)

RPC `Instance.Move` / `Disk.Move` и worker-сеттер владельца под ними удалены в
KAC-266 (контракт-removal). Move-семантики больше нет — finding закрыт.

---
## 3. Сводная таблица «исправлено / open» (перепроверена 2026-08-01)

Перепроверка сделана **по дереву**, а не по тону исходного текста: для каждой
находки установлено, существует ли ещё её предмет.

| Категория | Field/invariant | Статус |
|---|---|---|
| **Было закрыто DB-уровнем уже на момент аудита** | | |
| Все PK уникальности | 10 ресурсных таблиц | ✅ |
| `(project_id, name)` partial UNIQUE по 4 ресурсам | disks/images/snapshots/instances | ✅ |
| Пара «машина+диск» уникальна | PK таблицы привязок | ✅ |
| Один загрузочный диск на машину | partial UNIQUE | ✅ (G11) |
| `device_name` уникален в пределах машины | partial UNIQUE | ✅ |
| Диск не удалить, пока привязан | FK ON DELETE RESTRICT | ✅ |
| Строки привязок и NIC чистятся при удалении машины | FK ON DELETE CASCADE | ✅ |
| Регион не удалить, пока есть зоны | FK ON DELETE RESTRICT | ✅ (0003) |
| Порядок событий outbox | PK `sequence_no` + trigger notify | ✅ |
| **Закрыто после аудита** | | |
| Переход состояния машины | стал условным (CAS); не-CAS сеттер удалён из порта | ✅ **G2** |
| **Предмет удалён вместе с таблицами** | | |
| Уникальность привязки диска | таблица дропнута `0013`; владелец — storage | — **G1** |
| `instances.zone_id` FK | `zones` дропнута `0011`; ссылка стала cross-service | — **G3** |
| `disks.zone_id` FK | `zones` `0011` + `disks` `0021` | — **G4** |
| `disks.type_id` FK | `disks` `0021` + `disk_types` `0022` | — **G5** |
| Enum CHECK по дискам/образам/снимкам/зонам/привязкам | те же миграции | — **G9** (частично) |
| ~~Move name-conflict~~ | RPC удалены в KAC-266 | — ~~**G14**~~ |
| **By-design (остаются верны как решение)** | | |
| `disks.source_*_id` без FK | soft-ref происхождения | **G6** (Info) |
| `images.source_*_id` без FK | soft-ref происхождения | **G7** (Info) |
| `snapshots.source_disk_id` без FK | soft-ref происхождения | **G8** (Info) |
| Cross-service refs NIC → vpc | FK невозможен (запрет #8) | **G10** (N/A) |
| **Остаётся открытым** | | |
| Enum CHECK по статусным/режимным колонкам `instances` | осознанно не закрывается: удорожает пополнение enum, писателя кроме маппинга нет | **G9** (Low) |

---

## 4. Follow-up: что стало с планом

Аудит предлагал эпик KAC-87 «within-service refs DB-coverage closure (compute)» из
семи подзадач. Итог **не по плану, но по существу**: пять из семи закрылись не
миграцией-ограничением, а **удалением предмета** — раскол блочного хранения
(`kacho-storage` стал владельцем Volume/Snapshot/Image/DiskType) и вынос Geography
(`kacho-geo` стал владельцем Region/Zone) убрали из БД compute сами таблицы, к
которым предлагалось добавлять FK.

| Подзадача плана | Предмет | Исход |
|---|---|---|
| KAC-87.compute.1 — G1 | partial UNIQUE на привязку диска | Не понадобилась: таблица дропнута, инвариант живёт у владельца-storage |
| KAC-87.compute.2 — G2 | CAS на переход состояния | **Сделана** (code-only, как и планировалось) + гоночные integration-тесты |
| KAC-87.compute.3 — G3+G4+G5 | within-service FK на зону и тип | Не понадобилась: ссылки стали cross-service, FK через границу запрещён |
| KAC-87.compute.4 — G9 | enum CHECK | Предмет большей частью снят вместе с таблицами (см. §3); на живых колонках решение не пересматривалось |
| ~~KAC-87.compute.5~~ — G14 | Move-семантика | Снята: RPC удалены (KAC-266) |
| KAC-87.compute.6 — G6/G7/G8 | doc-only | Решения зафиксированы; переиспользованы владельцем-storage |
| KAC-87.compute.7 — G12/G13 | таблицы kube-ovn-эпохи | Закрыта: `0006_drop_hypervisors.sql` |

**Урок метода, ради которого документ и сохраняется.** Аудит был прав в классе
(software-side `Get → check → write` через границу конкуренции — дефект) и прав в
приоритете (G1/G2 — High). Но предложенное лекарство в четырёх случаях из пяти
оказалось не нужно, потому что правильный ответ был не «добавить ограничение», а
«вернуть ресурс владельцу». Аудит, меряющий только покрытие ограничениями, этого
варианта не видит — он спрашивает «где не хватает FK», а не «должна ли эта строка
вообще лежать здесь».

---

## 5. Ссылки

- Workspace `.claude/rules/data-integrity.md` § «Within-service инварианты — только на DB-уровне» (запрет #10)
- kacho-vpc parity-аудит: `services/vpc/docs/engineering/architecture/within-service-refs-audit.md` (KAC-84)
- KAC-52 — NIC-attach race в kacho-vpc, инцидент 2026-05-14 (источник pattern'а для G1/G2)
- KAC-15 — Geography переносилась в compute (породила G3/G4); эпик kacho-workspace#82 — вынесена в kacho-geo (сняла их)
- KAC-36/79/80 — таблицы kube-ovn-эпохи удалены (`0006_drop_hypervisors.sql`)
- KAC-266 — `Move` RPC удалены (снял G14)
- Раскол блочного хранения: `services/storage/docs/engineering/architecture/compute-storage-parity.md`
- Миграции, снявшие предмет находок: `0011_drop_geography.sql`, `0013_drop_attached_disks.sql`,
  `0021_drop_block_storage_duplicates.sql`, `0022_drop_disk_types.sql`
