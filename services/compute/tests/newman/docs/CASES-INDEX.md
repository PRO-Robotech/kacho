# Cases Index — kacho-compute newman (v1)

Каталог тест-кейсов по ресурсам. Источник истины — `cases/*.py`; коллекции в `collections/`
**генерируются** `scripts/gen.py`. Здесь — обзорный перечень + уникальные паттерны.

Всего (по `gen.py`): **111 кейсов** — instance-redesign 43, authz-deny 42,
machine-type 12, operation 8, list-filter 4, sec-d 2.
Zone/Region serving removed in Stage S7 (Geography owned by kacho-geo);
Disk/Image/Snapshot/DiskType — вместе с дублем блочного хранения (владелец —
kacho-storage, его suite живёт в `services/storage/tests/newman/`).

## Уникальные паттерны (generic-блоки в gen.py)

| Блок | Что делает | Применён к |
|---|---|---|
| `list_page_block(prefix, path[, project_param])` | BVA для List: pageSize 0 / 1 / 1000 / 1001 / garbage token | INST |
| `name_validation_block(prefix, path, extra[, wrap])` | compute name regex `\|[a-z]([-_a-z0-9]{0,61}[a-z0-9])?` — empty→200, len63→200, len64→400, UPPERCASE→400, digit-start→400, hyphen-start→400, special→400 | INST |
| `description_validation_block` | desc len 256→200, 257→400 | INST |
| `labels_validation_block` | uppercase-key→400, bad-char-key→400, 64 labels→200, 65→400 | INST |
| `filter_block` | filter name="X"→200, garbage→200\|400, unknown-field→200\|400 | INST |
| `http_method_block` | PUT/DELETE-on-list → 404\|405\|501 | INST |
| `malformed_body_block` | malformed JSON → 400\|415; empty body → 400 | INST |
| `security_injection_block` | SQLi/union/XSS/cmd/path/longpayload в name + filter → не 500, без pgx/stack-leak | INST |
| `poll_operation_until_done()` (LRO helper) | GET /operations/{opId} с `setNextRequest`-retry до 8 раз; assert `done==true` | каждый Create/Update/Delete/Move/Relocate/Start/Stop/Restart/Attach/Detach/NAT/UpdateMetadata |
| `assert_op_success()` / `assert_op_error(code,name[,substr])` | проверка `Operation.response` (success) или `Operation.error.code` (failed) | NEG-кейсы (async ошибки), CRUD-кейсы (после poll) |
| `assert_created_at_seconds()` | CONF: created_at в proto-ответе без дробной секунды (verbatim YC) | INST CRUD-OK |
| `assert_operation_envelope()` | Operation.id matches `^epd[a-z0-9]+$`, metadata is object | каждый Create CRUD-OK |

## Instance (77 кейсов) — `cases/instance.py` *(многие требуют поднятого kacho-vpc)*

- **CR**: CRUD-OK (RUNNING + fqdn + boot_disk + NO NIC (no auto-NIC) + id-prefix epd + created_at sec),
  CRUD-FROM-IMAGE-BOOT-OK, CRUD-BOOT-DISK-ID-OK, VAL-MISSING-{ZONE,PLATFORM,RESOURCES,BOOTDISK,PROJECT},
  NEG-PROJECT-NOTFOUND, NEG-DUP-NAME, VAL-NAME-UPPERCASE/-DIGIT-START,
  VAL-CORE-FRACTION-INVALID, VAL-CORES-ODD-INVALID, VAL-BOOTDISK-EXACTLY-ONE, VAL-EMPTY-BODY,
  VAL-MALFORMED-JSON, CONF-ID-PREFIX-EPD; + security.
- **GET**: NEG-NOTFOUND, CONF-NF-TEXT. **LST**: CRUD-OK, VAL-PROJECT-REQUIRED, VIEW-BASIC-NO-METADATA; + блоки.
- **UPD**: CRUD-NAME-DESC-LABELS-OK, RESOURCES-REQUIRES-STOPPED (RUNNING→FailedPrec; after Stop→OK),
  MASK-IMMUTABLE-ZONE, MASK-UNKNOWN-FIELD, AUTHZ-NF-SYNC.
- **STATE**: START-FROM-RUNNING (→FailedPrec), STOP-OK, START-FROM-STOPPED-OK, STOP-FROM-STOPPED (→FailedPrec),
  RESTART-OK, RESTART-FROM-STOPPED (→FailedPrec); + START/STOP-AUTHZ-NF-SYNC.
- **AD** (attachDisk, body `attachedDiskSpec.volumeId`): CRUD-OK, NEG-WRONG-ZONE, NEG-ALREADY-ATTACHED.
  **DD** (detachDisk, body `volumeId`): CRUD-OK, NEG-BOOT (→FailedPrec), NEG-NOT-ATTACHED.
  (bootDisk/secondaryDisks projection field `volumeId`; storage Volume — source of truth.)
- **NIC** (S4, attach/detach existing kacho-vpc NIC, prefix "nic"): AD-CRUD-OK (attach→mirror index 0→detach→empty),
  DD-BYINDEX-IDEMPOTENT-OK (detach by slot index + no-op replay), AD-NEG-MALFORMED-NIC (sync 400),
  AD-NEG-INSTANCE-NF / DD-NEG-INSTANCE-NF (sync 404). UpdateNetworkInterface/AddOneToOneNat/RemoveOneToOneNat — Unimplemented.
- **UMETA**: CRUD-OK (upsert/delete + FULL-view).
- **SPO**: CRUD-OK, NEG-NOTFOUND. **SME**: CRUD-OK (no-op). (Move — removed KAC-266.)
- **LOP**: CRUD-OK, NEG-PARENT-NF.
- **DEL**: CRUD-OK, STATE-AUTODELETE-BOOT-GONE, STATE-NONAUTODELETE-DISK-REMAINS, NEG-NOTFOUND, CONF-RESPONSE-EMPTY.
- **LIFECYCLE-CONF** (Create→Get→List→Update→Stop→Start→Delete→List→Get-404).

## Zone / Region — removed (Stage S7)

Geography (Region/Zone) serving was removed from kacho-compute — it is owned by
kacho-geo (epic kacho-workspace#82). `cases/zone.py` / `cases/region-zone.py` deleted.
`Instance/Disk.zone_id` is still validated (via the geo client) — see the
`zoneId`-bearing cases in `cases/instance-redesign.py`.

## Operation (8 кейсов) — `cases/operation.py`

GET-CRUD-OK (done op + response + metadata.epd), GET-CRUD-FAILED-OP (error code 5),
GET-NEG-NOTFOUND-VALID-PREFIX, GET-CONF-NF-TEXT, GET-NEG-UNKNOWN-PREFIX (→400 "prefix"),
CANCEL-NEG-ALREADY-DONE (→FailedPrec/idempotent), CANCEL-NEG-NOTFOUND, CANCEL-NEG-UNKNOWN-PREFIX.

## `# probe-needed:` маркеры (точный Kachō-контракт ещё не verified на стенде)

| Где | Что probed | Текущая формулировка |
|---|---|---|
| INST-UPD-RESOURCES-REQUIRES-STOPPED, INST-STATE-* | "Instance must be stopped" / "is not running" / "already running" texts | проверяем только code 9 |
| INST-DD-NEG-BOOT | "Cannot detach boot disk" text | проверяем только code 9 |
| INST-SME-CRUD-OK | SimulateMaintenanceEvent: Operation или Unimplemented? | allow 200\|501 |
| OP-CANCEL-NEG-ALREADY-DONE | Cancel done-op: FailedPrecondition или idempotent 200? | allow both |
| DT-LST-PAGE-TOKEN-GARBAGE | справочник игнорирует pageToken? | allow 200\|400 |
