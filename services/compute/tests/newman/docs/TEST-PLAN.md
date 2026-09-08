# Test Plan — newman coverage map (kacho-compute, актуально на 2026-05-11)

Карта `(ресурс, RPC) → классы → факт реализации`. Статусы:
`□` не начато, `◐` частично (есть happy ИЛИ negative), `■` базовое покрытие
(≥1 happy + ≥1 negative), `▣` расширенное (с BVA/STATE/CONF).

**Сводка (v1):** 296 кейсов / 7 коллекций. 100% публичных RPC compute-домена покрыты ≥1 кейсом
(кроме явных `blocked:*` / scope-cut — см. ниже).

## DiskService (8 публичных RPC — без access-bindings)

| RPC | Классы покрыто | Кейсы | Статус |
|---|---|---|---|
| Get | NEG (NotFound), CONF (NF-text) | DISK-GET-NEG-NOTFOUND, DISK-GET-CONF-NF-TEXT | ■ |
| List | CRUD, VAL (project req), PAGE (0/1/1000/1001+token), FILTER (name/garbage/unknown + match) | DISK-LST-* (8) | ▣ |
| Create | CRUD (empty, type-explicit, from-image), VAL (project/zone/size req, name regex/len, labels, desc), NEG (project-NF, zone-unknown, type-unknown, dup-name, source-NF), BVA (size min/below/max/above), CONF (id-prefix epd, created_at sec), SEC | DISK-CR-* (~35) | ▣ |
| Update | CRUD (name/desc/labels, size-increase), STATE (immutable type/zone, full-PATCH silent ignore, size-decrease reject, unknown-mask), NEG (sync-NF) | DISK-UPD-* (8) | ▣ |
| Delete | CRUD, NEG (sync-NF), CONF (response=Empty + metadata) | DISK-DEL-* (3) | ▣ |
| Move | — removed (KAC-266) | — | □ |
| Relocate | CRUD-OK, NEG (dest-zone-unknown) | DISK-REL-* (2) | ■ |
| ListOperations | CRUD-OK (≥1 op), NEG (parent-NF) | DISK-LOP-* (2) | ■ |

**Coverage: 7/7 RPC (100%); Move removed (KAC-266); ListSnapshotSchedules — RPC снят вместе со stillborn SnapshotSchedule.**

## ImageService (7 публичных RPC — без access-bindings)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| Get | NEG, CONF | IMG-GET-* (2) | ■ |
| GetLatestByFamily | CRUD-OK (newer wins), NEG (family-NF), VAL (project req) | IMG-GLF-* (3) | ▣ |
| List | CRUD, VAL (project req), PAGE, FILTER | IMG-LST-* + блоки | ▣ |
| Create | CRUD (from disk/uri/image/snapshot), VAL (project req, no-source, multiple-source, family regex, name/labels/desc), NEG (source-NF disk/image, project-NF, dup-name), CONF (id-prefix fd8, created_at sec), SEC | IMG-CR-* (~25) | ▣ |
| Update | CRUD (name/desc/labels), STATE (empty-mask full-PATCH, immutable family, unknown-mask), NEG (sync-NF) | IMG-UPD-* (5) | ▣ |
| Delete | CRUD, CONF (response=Empty), NEG (sync-NF) | IMG-DEL-* (3) | ▣ |
| ListOperations | CRUD-OK, NEG (parent-NF) | IMG-LOP-* (2) | ▣ |

**Coverage: 7/7 RPC (100%).** os_product_ids в Create — `blocked:kacho-marketplace`.

## SnapshotService (6 публичных RPC — без access-bindings)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| Get | NEG, CONF | SNAP-GET-* (2) | ■ |
| List | CRUD, VAL (project req), PAGE, FILTER | SNAP-LST-* + блоки | ▣ |
| Create | CRUD (from disk), VAL (project/disk req, name/labels/desc), NEG (disk-NF, project-NF, dup-name), CONF (id-prefix fd8, created_at sec, disk_size==disk.size, source_disk_id), SEC | SNAP-CR-* (~20) | ▣ |
| Update | CRUD (name/desc/labels), STATE (empty-mask full-PATCH, immutable source_disk_id, unknown-mask), NEG (sync-NF) | SNAP-UPD-* (5) | ▣ |
| Delete | CRUD, CONF (response=Empty), NEG (sync-NF), STATE (Disk удаляем после Snapshot) | SNAP-DEL-* (4) | ▣ |
| ListOperations | CRUD-OK, NEG (parent-NF) | SNAP-LOP-* (2) | ▣ |

**Coverage: 6/6 RPC (100%).**

## InstanceService (public RPC — без access-bindings)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| Get | NEG, CONF (NF-text), BASIC-view metadata omission (через List) | INST-GET-* (2), INST-LST-VIEW-BASIC-NO-METADATA | ▣ |
| List | CRUD, VAL (project req), PAGE, FILTER, view=BASIC conformance | INST-LST-* + блоки | ▣ |
| Create | CRUD (boot disk_spec / disk_id / from-image; no auto-NIC — KAC-266), VAL (missing zone/platform/resources/bootdisk/project, name regex, core_fraction, cores, bootdisk-exactly-one), NEG (project-NF, dup-name), CONF (id-prefix epd, created_at sec, fqdn, no NIC), SEC, BVA | INST-CR-* | ▣ |
| Update | CRUD (name/desc/labels), STATE (resources_spec requires STOPPED, immutable zone, unknown-mask), NEG (sync-NF) | INST-UPD-* (5) | ▣ |
| Start | STATE (←STOPPED only; from-RUNNING→FailedPrec, from-STOPPED→OK), NEG (sync-NF) | INST-STATE-START-* (2), INST-START-AUTHZ-NF-SYNC | ▣ |
| Stop | STATE (←RUNNING only; OK, from-STOPPED→FailedPrec), NEG (sync-NF) | INST-STATE-STOP-* (2), INST-STOP-AUTHZ-NF-SYNC | ▣ |
| Restart | STATE (←RUNNING only; OK, from-STOPPED→FailedPrec) | INST-STATE-RESTART-* (2) | ▣ |
| Delete | CRUD, NEG (sync-NF), STATE (auto_delete boot gone / non-auto remains), CONF (response=Empty + metadata) | INST-DEL-* (5) | ▣ |
| Move | — removed (KAC-266) | — | □ |
| ListOperations | CRUD-OK, NEG (parent-NF) | INST-LOP-* (2) | ■ |
| AttachDisk | CRUD-OK (secondary_disks updated), NEG (wrong-zone, already-attached) | INST-AD-* (3) | ▣ |
| DetachDisk | CRUD-OK, NEG (boot disk → FailedPrec, not-attached) | INST-DD-* (3) | ▣ |
| ~~UpdateMetadata~~ | RPC снят с контракта вместе с полем `metadata` — предмета проверки нет | — | — |
| GetSerialPortOutput | CRUD-OK (contents string), NEG (NotFound) | INST-SPO-* (2) | ■ |
| AttachNetworkInterface / DetachNetworkInterface / UpdateNetworkInterface | — NIC binding removed from Instance lifecycle (KAC-266, no auto-NIC); proto-level RPC cleanup tracked separately | — | □ |
| AddOneToOneNat / RemoveOneToOneNat | — NIC binding removed from Instance lifecycle (KAC-266); Instance has no NIC to NAT | — | □ |
| Relocate | — | — `blocked` (cross-zone disk move) | □ |
| SimulateMaintenanceEvent | CRUD-OK (no-op) | INST-SME-CRUD-OK | ◐ |

**KAC-266: Move + all NIC-coupled RPCs (Attach/Detach/UpdateNetworkInterface, AddOneToOneNat/RemoveOneToOneNat) removed from the active test surface — NIC binding is no longer part of the Instance lifecycle (no auto-NIC).**
Кросс-сервис: INST-* CRUD требуют поднятого kaname (project existence); NEG-PROJECT-NOTFOUND требует `KACHO_COMPUTE_SKIP_PEER_VALIDATION!=true`.

## DiskTypeService (2 RPC — read-only)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| List | CRUD (≥4 seeded, contains network-ssd/-hdd, zoneIds non-empty), BVA (page 0/1/1001 + token) | DT-LST-* (5) | ▣ |
| Get | CRUD (network-ssd / network-hdd), NEG (garbage→404), CONF (NF-text) | DT-GET-* (4) | ▣ |
| (Create — read-only) | NEG (POST → 405) | DT-CR-NEG-NOT-ALLOWED | ◐ |

**Coverage: 2/2 RPC (100%).**

## ZoneService (2 RPC — read-only)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| List | CRUD (≥3 seeded, contains ru-central1-{a,b,d}, status UP, regionId), PAGE (0/1/1001 + roundtrip) | ZONE-LST-* (6) | ▣ |
| Get | CRUD (ru-central1-a / -b), NEG (garbage→404), CONF (NF-text) | ZONE-GET-* (5) | ▣ |
| (Create — read-only) | NEG (POST → 405) | ZONE-CR-NEG-NOT-ALLOWED | ◐ |

**Coverage: 2/2 RPC (100%).**

## OperationService (2 RPC — Get / Cancel; через api-gateway OpsProxy)

| RPC | Классы | Кейсы | Статус |
|---|---|---|---|
| Get | CRUD-OK (done op + response + metadata.epd), CRUD-FAILED-OP (error code 5), NEG (NotFound valid-prefix, unknown-prefix→400), CONF (NF-text) | OP-GET-* (5) | ▣ |
| Cancel | NEG (already-done → FailedPrec/idempotent, NotFound, unknown-prefix→400) | OP-CANCEL-* (3) | ■ |

**Coverage: 2/2 RPC (100%).**

## Out-of-scope до rpc-implementer (Unimplemented / unwired — grep-verified 2026-07-21)

Проверено grep'ом `codes.Unimplemented` в `services/compute/internal/handler|service/*.go` + списком
`Register*ServiceServer` в composition-root. **Newman НЕ пишется** для этого — RPC вернёт `Unimplemented`
(impl-gap для `rpc-implementer`, НЕ test-gap):

- **Unwired services** (не в `Register*ServiceServer` → gateway `unknown service`): `MaintenanceService`.
  `InternalResourceLifecycleService` **удалён** тем же критерием: объявление без единой реализации — ни
  сервера, ни клиента, ни одной неgenerated-ссылки, включая типы сообщений (живой фид жизненного цикла
  есть только в `loadbalancer.v1`). Вместе с объявлением снята и его запись каталога прав.
  `DiskPlacementGroupService` / `FilesystemService` / `SnapshotScheduleService` **удалены** — mёртворождённые
  контракты без реализации, регистрации и модели прав. Тем же критерием и той же процедурой удалены
  `GpuClusterService` / `HostGroupService` / `HostTypeService` / `PlacementGroupService` /
  `ReservedInstancePoolService` (восстановление gate'а дрейфа модели прав).
- **Unimplemented RPC в wired-сервисах** (наследуются из `Unimplemented*ServiceServer`):
  `Instance.{AddOneToOneNat, RemoveOneToOneNat, UpdateNetworkInterface, Relocate}`,
  все `*.{ListAccessBindings, SetAccessBindings, UpdateAccessBindings}` (AAA-скелет). `Disk.kms_key_id` →
  sync `Unimplemented` (`blocked:kacho-kms`). См. `docs/engineering/architecture/07-known-divergences.md`.
- **`InternalDiskTypeService.{Create,Update,Delete}`** (:9091 admin, `system_admin`) — wired, но **cluster-internal**
  listener, не external TLS endpoint → вне public newman-suite (`security.md` Internal-vs-external; TAXONOMY scope-cut).
  Покрытие — unit/integration + отдельная internal-suite.
- **`MachineTypeService` / `InternalMachineTypeService`** — **не существуют** в AS-IS compute (ни в proto, ни в
  served surface). Появляются только в редизайне **COMP-1** (`project/kacho`, not-yet-landed) — покроются, когда
  `rpc-implementer` их зальёт.

## Conformance

Conformance-класс проверяет контракт Kachō против его собственного источника истины —
proto-определений и APPROVED acceptance-дока: форма ресурса, id-префиксы, усечение
timestamp'ов до секунд, тон сообщений об ошибке, sync-vs-async. Прогон против чужого
облака как эталона не существует (второй non-negotiable); харнесс, который для этого
держали, удалён. Кейсы с `# probe-needed:` фиксируют текущее поведение там, где спека
ещё не высказалась — список в `REQUIREMENTS.md`.
