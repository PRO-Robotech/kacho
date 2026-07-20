# Cases Index — kacho-compute newman (v1)

Каталог тест-кейсов по ресурсам. Источник истины — `cases/*.py`; коллекции в `collections/`
**генерируются** `scripts/gen.py`. Здесь — обзорный перечень + уникальные паттерны.

Всего (core-ресурсы, по `gen.py`): **325 кейсов** (disk 70, instance 77,
**instance-redesign 36**, **machine-type 12**, image 60, snapshot 52, disk-type 10,
operation 8); + authz-deny 186, list-filter 4, sec-d 2.
Zone/Region serving removed in Stage S7 (Geography owned by kacho-geo).
**COMP-1 redesign** (Instance core + MachineType) — `cases/instance-redesign.py` +
`cases/machine-type.py`; сверка с реализацией + нюансы контракта — `docs/RESULTS.md`.

## Уникальные паттерны (generic-блоки в gen.py)

| Блок | Что делает | Применён к |
|---|---|---|
| `list_page_block(prefix, path[, folder_param])` | BVA для List: pageSize 0 / 1 / 1000 / 1001 / garbage token | DISK, IMG, SNAP, INST (+ инлайн варианты для DT/ZONE) |
| `name_validation_block(prefix, path, extra[, wrap])` | compute name regex `\|[a-z]([-_a-z0-9]{0,61}[a-z0-9])?` — empty→200, len63→200, len64→400, UPPERCASE→400, digit-start→400, hyphen-start→400, special→400 | DISK, IMG, SNAP (wrap=pre-disk) |
| `description_validation_block` | desc len 256→200, 257→400 | DISK, IMG, SNAP |
| `labels_validation_block` | uppercase-key→400, bad-char-key→400, 64 labels→200, 65→400 | DISK, IMG, SNAP |
| `filter_block` | filter name="X"→200, garbage→200\|400, unknown-field→200\|400 | DISK, IMG, SNAP, INST |
| `http_method_block` | PUT/DELETE-on-list → 404\|405\|501 | DISK, IMG, SNAP, INST |
| `malformed_body_block` | malformed JSON → 400\|415; empty body → 400 | DISK, IMG, SNAP (+ инлайн для INST) |
| `security_injection_block` | SQLi/union/XSS/cmd/path/longpayload в name + filter → не 500, без pgx/stack-leak | DISK, IMG, SNAP, INST |
| `poll_operation_until_done()` (LRO helper) | GET /operations/{opId} с `setNextRequest`-retry до 8 раз; assert `done==true` | каждый Create/Update/Delete/Move/Relocate/Start/Stop/Restart/Attach/Detach/NAT/UpdateMetadata |
| `assert_op_success()` / `assert_op_error(code,name[,substr])` | проверка `Operation.response` (success) или `Operation.error.code` (failed) | NEG-кейсы (async ошибки), CRUD-кейсы (после poll) |
| `assert_created_at_seconds()` | CONF: created_at в proto-ответе без дробной секунды (verbatim YC) | DISK/IMG/SNAP/INST CRUD-OK |
| `assert_operation_envelope()` | Operation.id matches `^epd[a-z0-9]+$`, metadata is object | каждый Create CRUD-OK |

## Disk (74 кейса) — `cases/disk.py`

- **CR**: CRUD-OK (id-prefix epd + created_at sec), CRUD-TYPE-EXPLICIT, CRUD-FROM-IMAGE-OK,
  VAL-FOLDER/ZONE/SIZE-REQUIRED, NEG-FOLDER/ZONE/TYPE-NOTFOUND, NEG-DUP-NAME,
  NEG-SOURCE-IMAGE/SNAPSHOT-NOTFOUND, BVA-SIZE-MIN-OK / BELOW-MIN / CREATE-MAX-OK / ABOVE-CREATE-MAX,
  CONF-ID-PREFIX-EPD; + name/desc/labels/security блоки (~25).
- **GET**: NEG-NOTFOUND, CONF-NF-TEXT.
- **LST**: CRUD-OK, VAL-FOLDER-REQUIRED, FILTER-MATCH; + list-page/filter блоки.
- **UPD**: CRUD-NAME-DESC-LABELS-OK, SIZE-INCREASE-OK, SIZE-DECREASE-REJECT, MASK-EMPTY-FULL-PATCH,
  MASK-IMMUTABLE-TYPE / -ZONE, MASK-UNKNOWN-FIELD, AUTHZ-NF-SYNC.
- **DEL**: CRUD-OK, CONF-RESPONSE-EMPTY, NEG-NOTFOUND.
- **MV**: CRUD-OK, NEG-DEST-NOTFOUND, AUTHZ-NF-SYNC, VAL-NO-DEST.
- **REL**: CRUD-OK, NEG-DEST-ZONE-UNKNOWN.
- **LOP**: CRUD-OK, NEG-PARENT-NF. **LIFECYCLE-CONF**.

## Image (60 кейсов) — `cases/image.py`

- **CR**: CRUD-OK (from disk; id-prefix fd8 + created_at sec), CRUD-FROM-URI-OK, CRUD-FROM-IMAGE-OK,
  CRUD-FROM-SNAPSHOT-OK, VAL-FOLDER-REQUIRED, VAL-NO-SOURCE, VAL-MULTIPLE-SOURCE, VAL-FAMILY-INVALID,
  NEG-SOURCE-DISK/IMAGE-NOTFOUND, NEG-FOLDER-NOTFOUND, NEG-DUP-NAME, CONF-ID-PREFIX-FD8; + name/desc/labels/security.
- **GLF**: CRUD-OK (2 images same family → newer wins), NEG-NOTFOUND, VAL-FOLDER-REQUIRED.
- **GET**: NEG-NOTFOUND, CONF-NF-TEXT. **LST**: CRUD-OK, VAL-FOLDER-REQUIRED; + блоки.
- **UPD**: CRUD-NAME-DESC-LABELS-OK, MASK-IMMUTABLE-FAMILY, MASK-UNKNOWN-FIELD, AUTHZ-NF-SYNC.
- **DEL**: CRUD-OK, NEG-NOTFOUND. **LOP**: CRUD-OK. **LIFECYCLE-CONF**.

## Snapshot (52 кейса) — `cases/snapshot.py`

- **CR**: CRUD-OK (from disk; id-prefix fd8 + created_at sec + disk_size==disk.size + source_disk_id),
  VAL-FOLDER-REQUIRED, VAL-NO-DISK, NEG-DISK-NOTFOUND, NEG-FOLDER-NOTFOUND, NEG-DUP-NAME,
  CONF-ID-PREFIX-FD8; + name/desc/labels (wrap=pre-disk) / security.
- **GET**: NEG-NOTFOUND, CONF-NF-TEXT. **LST**: CRUD-OK, VAL-FOLDER-REQUIRED; + блоки.
- **UPD**: CRUD-NAME-DESC-LABELS-OK, MASK-IMMUTABLE-SOURCE-DISK, MASK-UNKNOWN-FIELD, AUTHZ-NF-SYNC.
- **DEL**: CRUD-OK, NEG-NOTFOUND, STATE-DISK-DELETABLE-AFTER (Disk удаляем, Snapshot остаётся).
- **LOP**: CRUD-OK. **LIFECYCLE-CONF**.

## Instance (77 кейсов) — `cases/instance.py` *(многие требуют поднятого kacho-vpc)*

- **CR**: CRUD-OK (RUNNING + fqdn + boot_disk + NO NIC (no auto-NIC) + id-prefix epd + created_at sec),
  CRUD-FROM-IMAGE-BOOT-OK, CRUD-BOOT-DISK-ID-OK, VAL-MISSING-{ZONE,PLATFORM,RESOURCES,BOOTDISK,FOLDER},
  NEG-FOLDER-NOTFOUND, NEG-DUP-NAME, VAL-NAME-UPPERCASE/-DIGIT-START,
  VAL-CORE-FRACTION-INVALID, VAL-CORES-ODD-INVALID, VAL-BOOTDISK-EXACTLY-ONE, VAL-EMPTY-BODY,
  VAL-MALFORMED-JSON, CONF-ID-PREFIX-EPD; + security.
- **GET**: NEG-NOTFOUND, CONF-NF-TEXT. **LST**: CRUD-OK, VAL-FOLDER-REQUIRED, VIEW-BASIC-NO-METADATA; + блоки.
- **UPD**: CRUD-NAME-DESC-LABELS-OK, RESOURCES-REQUIRES-STOPPED (RUNNING→FailedPrec; after Stop→OK),
  MASK-IMMUTABLE-ZONE, MASK-UNKNOWN-FIELD, AUTHZ-NF-SYNC.
- **STATE**: START-FROM-RUNNING (→FailedPrec), STOP-OK, START-FROM-STOPPED-OK, STOP-FROM-STOPPED (→FailedPrec),
  RESTART-OK, RESTART-FROM-STOPPED (→FailedPrec); + START/STOP-AUTHZ-NF-SYNC.
- **AD** (attachDisk, body `attachedDiskSpec.volumeId`): CRUD-OK, NEG-WRONG-ZONE, NEG-ALREADY-ATTACHED.
  **DD** (detachDisk, body `volumeId`): CRUD-OK, NEG-BOOT (→FailedPrec), NEG-NOT-ATTACHED.
  (bootDisk/secondaryDisks projection field `volumeId`; storage Volume — source of truth.)
- **DISK-DEL-WHILE-ATTACHED** (Disk.Delete пока attached → FailedPrec "is being used"; Detach→Delete OK).
- **NIC** (S4, attach/detach existing kacho-vpc NIC, prefix "nic"): AD-CRUD-OK (attach→mirror index 0→detach→empty),
  DD-BYINDEX-IDEMPOTENT-OK (detach by slot index + no-op replay), AD-NEG-MALFORMED-NIC (sync 400),
  AD-NEG-INSTANCE-NF / DD-NEG-INSTANCE-NF (sync 404). UpdateNetworkInterface/AddOneToOneNat/RemoveOneToOneNat — Unimplemented.
- **UMETA**: CRUD-OK (upsert/delete + FULL-view).
- **SPO**: CRUD-OK, NEG-NOTFOUND. **SME**: CRUD-OK (no-op). (Move — removed KAC-266.)
- **LOP**: CRUD-OK, NEG-PARENT-NF.
- **DEL**: CRUD-OK, STATE-AUTODELETE-BOOT-GONE, STATE-NONAUTODELETE-DISK-REMAINS, NEG-NOTFOUND, CONF-RESPONSE-EMPTY.
- **LIFECYCLE-CONF** (Create→Get→List→Update→Stop→Start→Delete→List→Get-404).

## DiskType (10 кейсов) — `cases/disk-type.py`

LST-CRUD-OK (≥4 seeded, contains network-ssd/-hdd, seeded-types zoneIds non-empty), GET-CRUD-OK (network-ssd, zoneIds⊇existingZone),
GET-CRUD-HDD-OK, GET-NEG-NOTFOUND, GET-CONF-NF-TEXT, LST-BVA-PAGESIZE-{1,ZERO,OVER-1001}, LST-PAGE-TOKEN-GARBAGE,
CR-NEG-EMPTY-ID (admin Create is Internal InternalDiskTypeService, ban #6; empty id → 400 INVALID_ARGUMENT or 404 route-absent — non-mutating).

## Zone / Region — removed (Stage S7)

Geography (Region/Zone) serving was removed from kacho-compute — it is owned by
kacho-geo (epic kacho-workspace#82). `cases/zone.py` / `cases/region-zone.py` deleted.
`Instance/Disk.zone_id` is still validated (via the geo client) — see the
`zoneId`-bearing cases in `cases/instance.py` / `cases/disk.py`.

## Operation (8 кейсов) — `cases/operation.py`

GET-CRUD-OK (done op + response + metadata.epd), GET-CRUD-FAILED-OP (error code 5),
GET-NEG-NOTFOUND-VALID-PREFIX, GET-CONF-NF-TEXT, GET-NEG-UNKNOWN-PREFIX (→400 "prefix"),
CANCEL-NEG-ALREADY-DONE (→FailedPrec/idempotent), CANCEL-NEG-NOTFOUND, CANCEL-NEG-UNKNOWN-PREFIX.

## MachineType (12 кейсов) — `cases/machine-type.py` *(COMP-1 F7 sync sizing-каталог)*

Каталог пуст на стенде → self-seed через `InternalMachineTypeService.Create` ({{internalBaseUrl}} :8081, ban #6) + cleanup.
- **CR (admin, Internal\*)**: `MT-CR-ADMIN-INTERNAL-CRUD-OK` (seed→public Get→internal Delete→Get 404),
  `MT-CR-ADMIN-NEG-NO-NAME` (empty name → 400 'name is required').
- **GET (public)**: `MT-GET-CRUD-OK` (flat: id mt-/name/family/effectiveResources° {vCpu,memoryMib MiB,gpus}/
  availableZones°/status/createdAt° sec), `MT-GET-VAL-MALFORMED-ID` (→ 400 'invalid machine type id', malformed-first),
  `MT-GET-NEG-NOTFOUND` (→ 404 'not found').
- **LST (public)**: `MT-LST-CRUD-OK` (array contains seeded), `MT-LST-FILTER-FAMILY-MINGPUS-NAME`
  (family=GPU дискаверит GPU-flavor'ы; minGpus=4 отсекает gpus<4; name= exact — GPU-count = гранулярность каталога),
  + `list_page_block` (pageSize 0/1/1000/1001 + garbage token).

## Instance redesign (36 кейсов) — `cases/instance-redesign.py` *(COMP-1 core; НЕ legacy instance.py)*

Sizing self-seed'ится через internal MachineType; async Operation (epd op-prefix, `ins-` resource-prefix).
- **CR positive**: `-CRUD-VM-OK` (kind VM + vmSpec/metadataOptions ENABLED + machineTypeId echo +
  effectiveResources° mirror + bootSource echo + status PROVISIONING + createdAt° sec; COMP-1-01/05/09/23),
  `-CRUD-CONTAINER-OK` (kind CONTAINER + containerSpec + guard-exempt; 02/15), `-CRUD-MACHINETYPE-BYNAME`
  (имя→canonical mt- echo; 06), `-CRUD-UNREACHABLE-ACK` (ack снимает guard; 14), `-CRUD-USE-DEFAULT-NETWORK` (16),
  `-CRUD-SERVICEACCOUNT` (SA Referrer echo; 12).
- **CR sync-negative (400)**: KIND-REQUIRED / KIND-VM-WITH-CONTAINERSPEC / KIND-CONTAINER-WITH-VMSPEC (03) ·
  MACHINETYPE-REQUIRED / RAW-SIZING-RETIRED (07) · CPU-GUARANTEE-OVER (08, BVA) · BOOTSOURCE-REQUIRED /
  BARE-UNTAGGED / UNKNOWN-TYPE / OUTPUT-FIELDS (10/11) · SERVICEACCOUNT-MALFORMED (13) · UNREACHABLE-GUARD (14) ·
  NO-NETWORK (16) · SECONDARY-VOLUME-SIZE (17, BVA).
- **CR async-negative (Operation.error)**: MACHINETYPE-NOTFOUND (FailedPrecondition; 07), DUP-NAME
  (ALREADY_EXISTS; 30), ZONE-UNKNOWN (peer-validate; 33).
- **GET**: MALFORMED-ID (400|403 authz-first; 22), NEG-ABSENT (403|404; 22).
- **UPD**: `-CRUD-LIVE-OK` (name/labels; 25), `-STATE-IMMUTABLE-MATRIX` (kind/zone immutable + boot Reinstall +
  fqdn unknown-mask + machine_type_id STOPPED-gate; 04/26/27), `-CRUD-NEXTBOOT-DEFERRAL` (statusReason; 27).
- **GET field-absence**: `-GET-CONF-FIELD-ABSENCE` (retired YC-cruft + infra-поля отсутствуют; 24/28).
- **LST**: `-CRUD-FILTER-OK` (listauthz row-filter present + filter name=; 34/36), `-BVA-PAGESIZE-OVER-1001` /
  `-PAGE-TOKEN-GARBAGE` (pagination-validate; 35), `-FILTER-KIND-TOLERANT` (F14 filter-whitelist gap, документ).
- **DEL**: `-CRUD-NAME-RECYCLE` (hard-delete → 403|404 + name снова Create-able; 37), MALFORMED-ID / NEG-ABSENT (38).

## `# probe-needed:` маркеры (точный Kachō-контракт ещё не verified на стенде)

| Где | Что probed | Текущая формулировка |
|---|---|---|
| DISK-CR-NEG-ZONE-UNKNOWN, DISK-REL-NEG-DEST-ZONE-UNKNOWN | unknown zone → InvalidArgument или NotFound? | allow code 3\|5 |
| DISK-CR-NEG-TYPE-UNKNOWN | unknown disk type text | предполагаем "Disk type ... not found" (substr "disk type") |
| DISK-CR-NEG-DUP-NAME, IMG/SNAP/INST -CR-NEG-DUP-NAME | ALREADY_EXISTS text | проверяем только code 6 |
| DISK-UPD-SIZE-DECREASE-REJECT | "Disk size can only be increased" text | проверяем только code 3 |
| DISK-GET/IMG-GET/SNAP-GET/DT-GET/ZONE-GET/OP-GET -CONF-NF-TEXT | "<Resource> <id> not found" verbatim | проверяем substr "not found" |
| INST-UPD-RESOURCES-REQUIRES-STOPPED, INST-STATE-* | "Instance must be stopped" / "is not running" / "already running" texts | проверяем только code 9 |
| INST-DD-NEG-BOOT | "Cannot detach boot disk" text | проверяем только code 9 |
| INST-DISK-DEL-WHILE-ATTACHED | "The disk ... is being used" text | проверяем только code 9 |
| INST-SME-CRUD-OK | SimulateMaintenanceEvent: Operation или Unimplemented? | allow 200\|501 |
| OP-CANCEL-NEG-ALREADY-DONE | Cancel done-op: FailedPrecondition или idempotent 200? | allow both |
| DT-LST-PAGE-TOKEN-GARBAGE | справочник игнорирует pageToken? | allow 200\|400 |
