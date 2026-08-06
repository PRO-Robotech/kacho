# Cases Index — kacho-compute newman (v1)

Каталог тест-кейсов по ресурсам. Источник истины — `cases/*.py`; коллекции в `collections/`
**генерируются** `scripts/gen.py`. Здесь — обзорный перечень + уникальные паттерны.

Всего (по `gen.py`): **117 кейсов** — instance-redesign 49, authz-deny 42,
machine-type 12, operation 8, list-filter 4, sec-d 2. Здесь стояло «111 — 43» — число
дрейфует само, потому что переписано рукой; сверяется одной командой
(`python3 scripts/gen.py` печатает счётчик по каждой коллекции, стенд не нужен), и
расхождение с этой строкой — находка, а не опечатка.
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
| `assert_created_at_seconds()` | CONF: created_at в proto-ответе без дробной секунды (конвенция Kachō) | INST CRUD-OK |
| `assert_operation_envelope()` | Operation.id matches `^epd[a-z0-9]+$`, metadata is object | каждый Create CRUD-OK |

## Instance — `cases/instance-redesign.py`

Здесь стоял перечень на 77 кейсов, отнесённый к отдельному файлу кейсов инстанса. Того
файла в дереве нет — кейсы инстанса живут в `cases/instance-redesign.py`, — а перечень
описывал шаги снятого контракта (сырое описание ресурсов, платформа, загрузочный диск,
блочное хранение). Он противоречил переписи в шапке ЭТОГО ЖЕ файла, то есть был не
устаревшей подробностью, а утверждением о покрытии, которого нет. Имя снятого файла здесь
намеренно не воспроизводится: в обратных кавычках оно читается как живая координата — и
именно так прежняя редакция этого абзаца сама стала находкой.

Пер-кейсовый перечень сюда НЕ переписывается: источник истины — `cases/*.py`, а вторая
копия расходится с ним молча (ровно это и случилось). Что прогоняется прямо сейчас,
печатает прогонщик без стенда:

```bash
./scripts/run-incremental.sh --list      # коллекции и case-id, которые будут прогнаны
```

## Zone / Region — removed (Stage S7)

Geography (Region/Zone) serving was removed from kacho-compute — it is owned by
kacho-geo (epic kacho-workspace#82). The two zone/region case files were deleted along
with it; their names are deliberately not quoted here — a path in backticks reads as a live
coordinate even inside the sentence that says it is gone.
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
