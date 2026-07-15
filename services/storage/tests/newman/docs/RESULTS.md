# RESULTS — kacho-storage newman (CS-1)

Прогон/статус black-box regression-suite (сгенерировано `scripts/gen.py`).

## Состав (по `python3 scripts/gen.py`)

| Коллекция | Кейсов | Стадия |
|---|---:|---|
| volume | 30 | S1 (CS1-S1-*) |
| snapshot | 16 | S3 (CS1-S3-*) |
| disk-type | 7 | S2 (CS1-S2-*) |
| operation | 3 | OperationService (OpsProxy sop) |
| authz | 9 | INV-10 public authz (fixture-gated) |
| internal-volume | 4 | S4 INV-7a external-absence |
| **Всего** | **69** | |

`scripts/validate-cases.py` → OK (69 уникальных case-id, нет дублей, все
каталогизированы). `python3 scripts/gen.py` → OK (6 коллекций).

## Прогон против стенда

_Не выполнен в рамках bootstrap-таска (TEST-ONLY; live-стенд/деплой не поднимался)._
Требует: `kacho-deploy` up + `reload-svc SVC=storage` + port-forward api-gateway → :18080,
`newman` установлен. Значения `existingProjectId`/`existingZoneId`/`existingDiskTypeId` в
`environments/local.postman_environment.json` — сверить с фактическим seed стенда.

## Integration-only (НЕ black-box reproducible через public API — по DoD §Тесты caveat)

Эти CS1-сценарии **не** покрываются newman и **не** должны — они покрыты
integration-тестами (`internal/repo/pg/*integration_test.go`, testcontainers +
concurrent `-race`), т.к. недостижимы через external public API:

| CS1 | Почему не black-box |
|---|---|
| CS1-S4-01/02/03/06/07/09/12 attach-CAS happy/idempotent/single-attach/device/boot/batch | `InternalVolumeService.Attach/*` только на :9091 mTLS internal-mux + seeded Instance; external endpoint не маршрутизирует |
| CS1-S4-05 zone/project-mismatch раздельными текстами | attach-CAS predicate — тот же internal :9091 путь |
| CS1-S4-08 auto-device-name concurrency (`-race`) | concurrent goroutines + `23505` retry — internal integration, не e2e |
| CS1-S4-10 double-attach race (`-race`) | concurrent goroutines — internal integration |
| CS1-S4-04 attach-not-ready / CS1-S3-02 snapshot-from-non-READY | control-plane финализирует Volume READY **мгновенно** (§0.1); не-READY достижимо только DB-seed |
| CS1-S2-02/03/05 admin DiskType Create/Update/Delete happy + FK delete-in-use | `InternalDiskTypeService.*` sync admin CRUD только на :9091 internal-mux + `system_admin` Check |

**Провокабельная (и включённая) часть S4/S2:** INV-7a — Internal-only RPC **отсутствуют**
на external endpoint → `cases/internal-volume.py` (`IVOL-*-EXTERNAL-ABSENT`, CS1-S4-11) и
`cases/disk-type.py` (`DT-{CR,UPD,DEL}-NEG-EXTERNAL-ABSENT`, CS1-S2-04). Обе — runnable
black-box против external baseUrl (route absent → 404/405/501).

## Fixture-gated (требуют authz-профиль стенда)

`cases/authz.py` (INV-10 CS1-S1-13/14, CS1-S3-07/08) — требует authz-enforced стенд
(не dev-passthrough) с identity `jwtProjectAdminA1` (alice, авторизована на `projectA1Id`,
не на `projectB1Id`), переиспользованной из compute authz-deny fixture (shared iam/fga seed).
Гоняется в authz-профиле — как compute `authz-deny.py` (`# requires`). DENY-кейсы
fixture-минимальны (existence-non-revealing → 403 независимо от существования цели);
`AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK` требует `viewer@projectA1` tuple.

## Newman-provokable негативы (закрывают DoD «≥1 negative на ресурс», black-box)

- **Volume:** malformed-id (sync `invalid volume id`), well-formed-not-found, size-shrink/equal
  reject (op-error `Volume size can only be increased`), dup-name (op-error ALREADY_EXISTS),
  unknown-zone (sync `unknown zone id`), project-not-found (sync FAILED_PRECONDITION),
  same-DB FK diskType/snapshot not-found (op-error), sizeBytes=0 / uppercase / unicode name (sync).
- **Snapshot:** malformed-id, not-found, source-missing (op-error), project-not-found (sync),
  uppercase/unicode name (sync), immutable source_volume_id (sync), delete-not-found (op-error).
- **DiskType:** not-found (`DiskType <id> not found`), pageSize-over-max, admin-external-absence.
