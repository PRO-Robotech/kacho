# RESULTS — kacho-storage newman (CS-1)

Прогон/статус black-box regression-suite (сгенерировано `scripts/gen.py`).

## Состав (по `python3 scripts/gen.py`)

| Коллекция | Кейсов | Стадия |
|---|---:|---|
| volume | 41 | S1 (CS1-S1-*) |
| **image** | **43** | **redesign STOR-1 (F9..F14, NET-NEW `img-`)** |
| snapshot | 24 | S3 (CS1-S3-*) |
| disk-type | 8 | S2 (CS1-S2-*) |
| operation | 8 | OperationService (OpsProxy sop) |
| authz | 13 | INV-10 public authz (fixture-gated) |
| authz-catalog | 18 | матрица доступа к admin-каталогу DiskType (6 субъектов × 3 операции) |
| internal-volume | 4 | S4 INV-7a external-absence |
| sec-d | 4 | SEC-D owner-tuple через iam (outbox → RegisterResource) |
| **Всего** | **163** | |

`scripts/validate-cases.py` → OK (163 уникальных case-id, нет дублей, все
каталогизированы). `python3 scripts/gen.py` → OK (9 коллекций).

Таблица выше — не заметка «на память»: её сверяет с самими коллекциями гейт
`make -C services/storage audit-known-failing` (`tools/audit-known-failing.sh`,
дублирован в `go test ./tools/...`). Ростер, отстающий от сюиты, — такое же ложное
утверждение, как протухшая запись «известное красное» ниже, просто таблицей выше;
поэтому оба измерения проверяются одним гейтом, а не сверяются глазами.

## Production-mode прогон (#59)

RS256 SA-principal seed (`tests/authz-fixtures/prodseed_matrix.py`), api-gateway
`authn.mode=production-strict`. Прод-баг `img`-prefix (gateway authz-edge 400'ил все
image Get/Update/Delete) — **пофикшен** (`fix(ids)`: register storage image prefix,
gateway-only rebuild) → image get-by-id восстановлен.

### Открытый долг покрытия — per-object фильтр страницы (3 пробы, чёрным ящиком НЕ покрыт)

Заявляю числом, а не молчанием: **три** пробы —
`AUTHZ-{VOL,SNP,IMG}-LST-OVERSHOW-LEAK-GUARD` — названы «over-show leak-guard», но
**per-object фильтр страницы не выполняют** и выполнить не могут. Это не регресс, это
граница, которую раньше маскировало утверждение, принимавшее два исхода.

**Почему не могут.** Перечисления гейтятся краем: `viewer` на объекте проекта из
запроса, без `scope_filtered`. Субъект проб — никогда-не-гранченый, значит на project-
gate он получает терминальный `403`, и бэкенд (а с ним и фильтр страницы) не набирается
вовсе. До правки 2026-07-30 пробы принимали `oneOf([200, 403])` и проверяли отсутствие
объекта в массиве из тела — на 403 массив пуст by construction, поэтому главное
утверждение проходило вакуумно в ЕДИНСТВЕННОМ достижимом исходе, а `200` (открывшийся
project-gate у субъекта без грантов — настоящая утечка) принималось как законный ответ.
Теперь пробы заявляют отказ и отсутствие любых сведений в его теле; `200` на этом пути —
падение. То, что они доказывают, стало правдой; то, чего не доказывают, стало видно.

**Какая фикстура нужна, чтобы долг закрыть.** Субъект, который project-gate ПРОХОДИТ, но
на созданный объект гранта не имеет. Обычный участник проекта таким быть не может: из
project-scoped выдачи реконсайлер материализует per-object глаголы на КАЖДЫЙ объект
проекта (плоская модель), поэтому он видит объект законно, и проба краснела бы на
корректном поведении. Значит нужна выдача, **суженная селектором по меткам**, которая
метке созданного объекта не соответствует: субъект проходит project-gate, per-object
глагола на этот объект у него нет, и объект обязан не появиться в странице. Это отдельная
работа по фикстуре (iam умеет селекторы — см. его суиты отзыва по метке), и она не
делается правкой утверждения.

**Чем инвариант закрыт сейчас** (то есть чего именно НЕ хватает — только чёрного ящика):
Go-тесты use-case-слоя (`internal/apps/kacho/api/*/list_filter_test.go`) и CI-гейт
`tools/audit-list-filter.sh`. Оба остаются зелёными и обязательными.

### Known failing — product bugs: нет (обе прежние записи ЗАКРЫТЫ вместе со своими дефектами)

Записей нет: **оба** прежних дефекта закрыты в дереве, и вместе с ними сняты
`# verifies …/issues/N`-аннотации кейсов.

- **#61** (Image.Create не валидировал `description`>256 / `labels`>64 синхронно) —
  `validate.Description`/`validate.Labels` стоят на входе `image.UseCase.Create`,
  до любого peer/DB-вызова; `IMG-CR-BVA-{DESC-OVER-257,LABELS-OVER-65}` — обычные
  зелёные кейсы.
- **#62** (`edit`-роль не материализовала storage-verbs) — селекторы системных ролей
  расширены на storage-типы миграцией iam `0060_storage_system_role_selectors`;
  `AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK` — обычный зелёный кейс.

Обе записи прожили дольше своих фиксов и продолжали утверждать, что продукт сломан.
Чтобы это не повторилось, срок жизни записи теперь проверяется механически: гейт
`audit-known-failing` требует, чтобы у каждой записи был ОТКРЫТЫЙ тикет и кейс с
парной аннотацией, и падает на записи, которой больше нечего исключать.

Остальные исходные красные (malformed-id тон `invalid resource id` vs family-specific;
internal-volume external-absence 403 fail-closed vs [404,405,501]; FieldMask snake→camel)
были **test-staleness** — приведены к фактическому gateway-контракту (family-agnostic
edge-message; fail-closed uncatalogued 403; camelCase FieldMask paths), см. диффы кейсов.

## STOR-1 redesign — Image (`cases/image.py`, 43 кейса)

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
рабочий файл окружения в `environments/` (прогонщик делает его копией из отслеживаемого
`environments/local.postman_environment.template.json`; сама копия под `.gitignore`) —
сверить с фактическим seed стенда.

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
fixture-минимальны (исход не зависит от существования цели): мутация и список → 403,
одиночное чтение → 404 текстом владельца, байт в байт как настоящий промах.
`AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK` требует `viewer@projectA1` tuple.
`AUTHZ-VOL-VERB-CUT-NOT-TIER` требует узкой роли посева `ps_storage_crlist_*` и её
предъявителя `jwtStorageCreateListOnlyA` (`storage.volumes {create,list}` @ projectA1):
без них разрез «глагол, а не ярус» предъявить нечем.

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
  displayNameRe), `VOL-UPD-MASK-IMMUTABLE-{BLOCKSIZE,SOURCESNAPSHOT}` (immutable-switch;
  выписанный здесь «полный набор» не был удержан ничем и разошёлся с деревом — сверять
  предикатом `grep -n 'case "zone_id"' services/storage/internal/apps/kacho/api/volume/volume.go`,
  а не этой строкой),
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
