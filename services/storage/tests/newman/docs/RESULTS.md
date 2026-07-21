# RESULTS — kacho-storage newman (CS-1)

Прогон/статус black-box regression-suite (сгенерировано `scripts/gen.py`).

## Состав (по `python3 scripts/gen.py`)

| Коллекция | Кейсов | Стадия |
|---|---:|---|
| volume | 38 | S1 (CS1-S1-*) |
| **image** | **39** | **redesign STOR-1 (F9..F14, NET-NEW `img-`)** |
| snapshot | 21 | S3 (CS1-S3-*) |
| disk-type | 8 | S2 (CS1-S2-*) |
| operation | 3 | OperationService (OpsProxy sop) |
| authz | 9 | INV-10 public authz (fixture-gated) |
| internal-volume | 4 | S4 INV-7a external-absence |
| **Всего** | **122** | |

`scripts/validate-cases.py` → OK (122 уникальных case-id, нет дублей, все
каталогизированы). `python3 scripts/gen.py` → OK (7 коллекций).

## STOR-1 redesign — Image (`cases/image.py`, 39 кейсов)

NET-NEW ресурс `Image` (VM boot-образ, REGIONAL/anycast, `img-` prefix) + Volume↔Image
boot-materialize (`source_image_id`). Трассировка `IMG-* ↔ STOR-1-NN` (F9..F14) — см.
`docs/CASES-INDEX.md`. Покрыто black-box через api-gateway: Image CRUD+ListOperations
(STOR-1-20/22), source-oneof exactly-one snapshot XOR volume (STOR-1-24: both/none →
sync INVALID_ARGUMENT, unknown-source → op-error FAILED_PRECONDITION FK), UNIQUE(name)
dup (STOR-1-21), malformed/not-found тон, region/project peer-validate (STOR-1-20/29,
authz-first tolerant), BVA name/desc/labels (STOR-1-30), immutable region/source/format
(STOR-1-22), List listauthz+pagination-validate-before-authz+filter+cursor (STOR-1-31/32/33),
two-projection lean field-absence (STOR-1-25), boot-Volume materialize `sourceImageId`
(STOR-1-18), Volume source XOR (STOR-1-19), **Image.Delete → volume.sourceImageId SET NULL,
том цел** (STOR-1-28).

**Internal/gated НЕ в этой суите** (integration/bufconn, по acceptance §DoD): attach-CAS
`InternalVolumeService.Attach` + concurrent-race (F3/STOR-1-06..10), `GetInternal`/
`InternalImageService` (F8/F12/STOR-1-16/17/25 internal-часть), `usedBy`→`common.v1.Referrer`
(F7/STOR-1-14 — B1 Phase-0-gated, `usedBy` пока legacy `reference.Reference`; экспозиция —
attach-driven :9091), owner-tuple materialization anti-BOLA (F13/STOR-1-27 — fixture-gated).

## Прогон против стенда

_Не выполнен (TEST-ONLY; local newman env-blocked — port-forward/HTTPS harness, см.
memory `local-newman-env-blocked`). Кейсы авторены против APPROVED acceptance STOR-1 +
реального контракта (proto/handler/domain/errmap — error-тексты grounded в коде). RED→GREEN
исполняет CI-раннер._ Требует: `kacho-deploy` up + `reload-svc SVC=storage` + port-forward
api-gateway → :18080, `newman` установлен. Значения `existingProjectId`/`existingZoneId`/
`existingRegionId`/`existingDiskTypeId`/`garbageImageId` в
`environments/local.postman_environment.json` — сверить с фактическим seed стенда.

**Known failing — product bugs:** нет (расхождений прода от контракта STOR-1 при авторинге
не выявлено; все IMG-кейсы — ожидаемо-зелёные regression против landed-редизайна).

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

## Parity-добор (qa, +14 кейсов) — Volume/Snapshot/DiskType до паритета с Image-шаблоном

Добор negatives/edge-паритета (grounded в proto/domain/service — не против live-стенда;
RED→GREEN исполняет CI-раннер, локальный newman env-blocked). Все техники в `description`
кейсов. Ни один не требует не-READY / :9091 / attach-CAS (integration-only остаётся вне scope):

- **Volume (+8):** `VOL-CR-BVA-NAME-OVER-64` (BVA len 63+1 → `Illegal argument name`,
  domain `RuneCount>63`), `VOL-CR-VAL-NAME-{DIGIT,HYPHEN}-START` (ECP первого символа,
  displayNameRe), `VOL-UPD-MASK-IMMUTABLE-{BLOCKSIZE,SOURCESNAPSHOT}` (immutable-switch,
  полный набор `{zone_id,disk_type_id,block_size,source_snapshot_id,used_by}`),
  `VOL-UPD-MASK-EMPTY-FULL-PATCH-OK` (пустой mask = full-PATCH, mutable применён, immutable
  zone цел — CS1-S1-05 gap), `VOL-CR-SEC-NAME-INJECTION` + `VOL-LST-SEC-FILTER-SQLI`
  (INV-8 no-leak black-box: не 500, нет pgx/SQLSTATE/panic/goroutine).
- **Snapshot (+5):** `SNP-LST-BVA-PAGESIZE-OVER-MAX` (validate.PageSize > 1000),
  `SNP-UPD-MASK-UNKNOWN-FIELD` (UpdateMask known-set), `SNP-UPD-MASK-IMMUTABLE-{PROJECT,SIZE}`
  (immutable-switch `{source_volume_id,project_id,size_bytes}`), `SNP-CR-BVA-NAME-OVER-64`.
- **DiskType (+1):** `DT-LST-PAGE-TOKEN-GARBAGE` (decodePageToken → ErrInvalidArg 400).

Grounding-заметки (проверено в коде redesign/integration): нет validate-interceptor в цепочке
(recovery→logging→principal→authz) → `description`/`labels` НЕ валидируются доменно (volume.go/
image.go `Validate` их не проверяют) → BVA desc-257/labels-65 для Volume/Snapshot НЕ добавлены
(были бы false-red; image.py их несёт как unverified — не реплицированы). name (regex+len) и
size_bytes валидируются доменно → grounded. immutable-switch и page_size/page_token/update_mask —
в service-слое (volume/snapshot/disktype UseCase), не в interceptor.

**Known failing — product bugs:** нет (расхождений прода от контракта при авторинге не выявлено).
