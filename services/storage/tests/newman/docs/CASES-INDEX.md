# Cases Index — kacho-storage newman (CS-1)

Каталог тест-кейсов по ресурсам storage-домена. Источник истины — `cases/*.py`;
коллекции в `collections/` **генерируются** `scripts/gen.py`. `scripts/validate-cases.py`
требует, чтобы каждый case-id (кроме `internal-*.py`, каталогизированных заметкой ниже)
присутствовал здесь литерально ИЛИ был помечен `# index:`-тегом.

Sub-phase acceptance: `docs/specs/sub-phase-CS-1-storage-network-disk-acceptance.md`
(эпик kacho-workspace#132). Каждый кейс несёт `# verifies CS1-Sx-yy`-аннотацию.
Error-тексты §0.2 assert'ятся **behaviour-level** (код И точное сообщение).

**Redesign STOR-1** (`docs/specs/sub-phase-STOR-1-volume-image-acceptance.md`) —
NET-NEW ресурс `Image` (`cases/image.py`) + Volume↔Image boot-materialize
(`source_image_id`). Кейсы `IMG-*` несут `# verifies STOR-1-NN` (F9..F14).

## Уникальные паттерны / helper'ы (в scripts/gen.py)

| Хелпер | Что делает | Применён к |
|---|---|---|
| `poll_operation_until_done()` | GET /operations/{sop-id} с `setNextRequest`-retry до 8 раз; assert done | каждый Volume/Snapshot Create/Update/Delete |
| `assert_op_success()` / `assert_op_error(code,name[,substr])` | проверка `Operation.response` (success) или `Operation.error.code/message` (failed) | async NEG (op-error) + CRUD (после poll) |
| `assert_operation_envelope()` | Operation.id matches `^sop[a-z0-9]+$`, metadata is object | каждый Create CRUD-OK |
| `assert_created_at_seconds()` | CONF: created_at без дробной секунды (truncate до секунд) [INV-9] | VOL/SNP CRUD-OK |
| per-step `auth` (bearer override) | Authorization: Bearer {{envVar}} для authz-кейсов | cases/authz.py |
| `wait_until_ready(step, ready, subject)` / `wait_until_ready_step(...)` | ограниченное ожидание ПРИГОДНОСТИ ресурса — её объявляет сверщик, а не `Operation.done`; при исчерпании бюджета падает, назвав `status` и `statusReason` | каждая фикстура-источник и каждый happy, утверждающий готовность |

## Volume (`cases/volume.py`) — stage S1

| case-id | CS1 | класс |
|---|---|---|
| VOL-CR-CRUD-OK | CS1-S1-01 | happy |
| VOL-GET-NEG-MALFORMED-ID | CS1-S1-02 | negative |
| VOL-GET-NEG-NOTFOUND | CS1-S1-02 | negative |
| VOL-LST-CRUD-OK | CS1-S1-03 | happy |
| VOL-LST-VAL-PROJECT-REQUIRED | CS1-S1-03 | negative |
| VOL-LST-BVA-PAGESIZE-OVER-MAX | CS1-S1-03 | negative (BVA) |
| VOL-LST-PAGE-TOKEN-GARBAGE | CS1-S1-03 | negative |
| VOL-LST-FILTER-NAME-MATCH | CS1-S1-03 | happy (filter) |
| VOL-UPD-SIZE-GROW-OK | CS1-S1-04 | happy |
| VOL-UPD-SIZE-SHRINK-REJECT | CS1-S1-04 | negative (op-error) |
| VOL-UPD-SIZE-EQUAL-REJECT | CS1-S1-04 | negative (op-error) |
| VOL-UPD-CRUD-NAME-DESC-LABELS-OK | CS1-S1-04 | happy |
| VOL-UPD-MASK-IMMUTABLE-ZONE | CS1-S1-05 | negative (sync) |
| VOL-UPD-MASK-IMMUTABLE-DISKTYPE | CS1-S1-05 | negative (sync) |
| VOL-UPD-MASK-UNKNOWN-FIELD | CS1-S1-05 | negative (sync) |
| VOL-CR-NEG-DUP-NAME | CS1-S1-06 | negative (op-error) |
| VOL-CR-CRUD-EMPTY-NAME-OK | CS1-S1-06 | happy (имя подставлено идентификатором) |
| VOL-DEL-CRUD-OK | CS1-S1-07 | happy |
| VOL-DEL-NEG-NOTFOUND | CS1-S1-07 | negative (op-error) |
| VOL-CR-NEG-ZONE-UNKNOWN | CS1-S1-08 | negative (peer sync) |
| VOL-CR-NEG-PROJECT-NOTFOUND | CS1-S1-09 | negative (peer sync) |
| VOL-CR-NEG-DISKTYPE-NOTFOUND | CS1-S1-10 | negative (op-error FK) |
| VOL-CR-NEG-SNAPSHOT-NOTFOUND | CS1-S1-10 | negative (op-error FK) |
| VOL-GET-CONF-LEAN-PROJECTION | CS1-S1-11 | conformance (INV-6) |
| VOL-CR-VAL-SIZE-ZERO | CS1-S1-12 | negative (sync) |
| VOL-CR-VAL-NAME-UPPERCASE | CS1-S1-12 | negative (sync) |
| VOL-CR-VAL-NAME-UNICODE | CS1-S1-12 | negative (sync) |
| VOL-LOP-CRUD-OK | CS1-S1-15 | happy |
| VOL-LOP-NEG-MALFORMED-ID | CS1-S1-15 | negative (sync) |
| VOL-LIFECYCLE-CONF | CS1-S1-01/04/07 | conformance |
| VOL-CR-BVA-NAME-OVER-64 | CS1-S1-12 | negative (BVA) |
| VOL-CR-VAL-NAME-UNDERSCORE | CS1-S1-12 | negative (sync; заменил NAME-DIGIT-START — цифра первым символом теперь законна) |
| VOL-CR-VAL-NAME-HYPHEN-START | CS1-S1-12 | negative (sync) |
| VOL-UPD-MASK-RETIRED-BLOCKSIZE-REJECTED | CS1-S1-05 | negative (sync; слот снят с контракта — маска, называющая его, отвергается) |
| VOL-UPD-MASK-IMMUTABLE-SOURCESNAPSHOT | CS1-S1-05 | negative (sync) |
| VOL-UPD-MASK-EMPTY-FULL-PATCH-OK | CS1-S1-05 | happy (full-PATCH) |
| VOL-CR-SEC-NAME-INJECTION | CS1-S1-11 | negative (SEC no-leak) |
| VOL-LST-SEC-FILTER-SQLI | CS1-S1-03 | negative (SEC no-leak) |
| VOL-UPD-BVA-DESC-256-257 | CS1-S1-04 | BVA обе стороны границы (update-path, sync + details) |
| VOL-UPD-BVA-LABELS-64-65 | CS1-S1-04 | BVA обе стороны границы (update-path, sync + details) |
| VOL-OBJSELF-PROJECT-SCOPED-CRUD | #71 | happy (object-self anti-BOLA, project-scoped actor — not cluster-admin-masked) |
| VOL-CDT-CRUD-OK | CS1-S1-04 | happy (ChangeDiskType: перемещение данных выделенным глаголом; целевой класс берётся ИЗ СПИСКА, не из литерала посева) |
| VOL-CDT-NEG-MALFORMED-ID | CS1-S1-02 | negative (sync) |
| VOL-CDT-VAL-DISKTYPE-REQUIRED | CS1-S1-12 | negative (sync; пустой класс — запрос без предмета) |
| VOL-CDT-NEG-VOLUME-NOTFOUND | CS1-S1-07 | negative (op-error) |
| VOL-CDT-NEG-DISKTYPE-UNKNOWN | CS1-S1-10 | negative (op-error; класс тома не изменился отвергнутым глаголом) |

## Snapshot (`cases/snapshot.py`) — stage S3

| case-id | CS1 | класс |
|---|---|---|
| SNP-CR-CRUD-OK | CS1-S3-01 | happy |
| SNP-CR-NEG-SOURCE-MISSING | CS1-S3-02 | negative (op-error; from-MISSING branch) |
| SNP-CR-VAL-PROJECT-REQUIRED | CS1-S3-03 | negative |
| SNP-CR-VAL-SOURCE-REQUIRED | CS1-S3-03 | negative |
| SNP-CR-NEG-PROJECT-NOTFOUND | CS1-S3-03 | negative (peer sync) |
| SNP-CR-VAL-NAME-UPPERCASE | CS1-S3-03 | negative (sync) |
| SNP-CR-VAL-NAME-UNICODE | CS1-S3-03 | negative (sync) |
| SNP-GET-NEG-MALFORMED-ID | CS1-S3-04 | negative (sync) |
| SNP-GET-NEG-NOTFOUND | CS1-S3-04 | negative |
| SNP-LST-CRUD-OK | CS1-S3-04 | happy |
| SNP-LST-VAL-PROJECT-REQUIRED | CS1-S3-04 | negative |
| SNP-LST-PAGE-TOKEN-GARBAGE | CS1-S3-04 | negative |
| SNP-UPD-MASK-IMMUTABLE-SOURCE | CS1-S3-05 | negative (sync) |
| SNP-UPD-CRUD-NAME-LABELS-OK | CS1-S3-05 | happy |
| SNP-DEL-CRUD-OK | CS1-S3-06 | happy |
| SNP-DEL-NEG-NOTFOUND | CS1-S3-06 | negative (op-error) |
| SNP-LST-BVA-PAGESIZE-OVER-MAX | CS1-S3-04 | negative (BVA) |
| SNP-UPD-MASK-UNKNOWN-FIELD | CS1-S3-05 | negative (sync) |
| SNP-UPD-MASK-IMMUTABLE-PROJECT | CS1-S3-05 | negative (sync) |
| SNP-UPD-MASK-IMMUTABLE-SIZE | CS1-S3-05 | negative (sync) |
| SNP-CR-BVA-NAME-OVER-64 | CS1-S3-03 | negative (BVA) |
| SNP-UPD-BVA-DESC-256-257 | CS1-S3-05 | BVA обе стороны границы (update-path, sync + details) |
| SNP-UPD-BVA-LABELS-64-65 | CS1-S3-05 | BVA обе стороны границы (update-path, sync + details) |
| SNP-UPD-VAL-NAME-UPPERCASE | CS1-S3-05 | negative (sync; формат имени на update-path) |
| SNP-UPD-MASK-ZONE-REJECTED | CS1-S3-05 | negative (sync; якорь размещения снимка правкой не меняется) |
| SNP-COPY-CRUD-OK | CS1-S3-01 | happy (Copy в другую зону: НОВАЯ строка, источник цел) |
| SNP-COPY-VAL-PROJECT-REQUIRED | CS1-S3-03 | negative (unscoped; право «создать» — у родителя) |
| SNP-COPY-VAL-TARGET-ZONE-REQUIRED | CS1-S3-03 | negative (sync) |
| SNP-COPY-NEG-TARGET-ZONE-UNKNOWN | CS1-S3-03 | negative (peer geo sync) |
| SNP-COPY-NEG-MALFORMED-ID | CS1-S3-04 | negative (sync) |
| SNP-COPY-NEG-SOURCE-NOTFOUND | CS1-S3-04 | negative (sync 404) |
| SNP-LOP-CRUD-OK | CS1-S3-01 | happy (журнал операций снимка — паритет с томом и образом) |
| SNP-LOP-NEG-MALFORMED-ID | CS1-S3-04 | negative (sync) |
| SNP-LOP-BVA-PAGESIZE-OVER-MAX | CS1-S3-04 | negative (BVA) |

## Image (`cases/image.py`) — redesign STOR-1 (NET-NEW ресурс `img-`)

| case-id | STOR-1 | класс |
|---|---|---|
| IMG-CR-CRUD-OK | STOR-1-20/24 | happy (source=volume) |
| IMG-CR-CRUD-FROM-SNAPSHOT-OK | STOR-1-24 | happy (source=snapshot) |
| IMG-CR-VAL-SOURCE-BOTH | STOR-1-24 | negative (sync; decision-table) |
| IMG-CR-VAL-SOURCE-NONE | STOR-1-24 | negative (sync; blank DEFER) |
| IMG-CR-NEG-SNAPSHOT-NOTFOUND | STOR-1-24 | negative (op-error FK) |
| IMG-CR-NEG-VOLUME-NOTFOUND | STOR-1-24 | negative (op-error FK) |
| IMG-CR-NEG-DUP-NAME | STOR-1-21 | negative (op-error UNIQUE) |
| IMG-GET-NEG-MALFORMED-ID | STOR-1-21 | negative (sync) |
| IMG-GET-NEG-NOTFOUND | STOR-1-21 | negative |
| IMG-CR-VAL-PROJECT-REQUIRED | STOR-1-20 | negative (sync) |
| IMG-CR-VAL-REGION-REQUIRED | STOR-1-20 | negative (sync) |
| IMG-CR-NEG-REGION-UNKNOWN | STOR-1-20 | negative (peer geo sync) |
| IMG-CR-NEG-PROJECT-NOTFOUND | STOR-1-29 | negative (peer iam; authz-first tolerant) |
| IMG-CR-VAL-NAME-UPPERCASE | STOR-1-30 | negative (sync) |
| IMG-CR-VAL-NAME-UNICODE | STOR-1-30 | negative (sync) |
| IMG-CR-BVA-NAME-OVER-64 | STOR-1-30 | negative (BVA) |
| IMG-CR-BVA-DESC-OVER-257 | STOR-1-30 | negative (BVA) |
| IMG-CR-BVA-LABELS-OVER-65 | STOR-1-30 | negative (BVA) |
| IMG-UPD-CRUD-NAME-DESC-LABELS-OK | STOR-1-22 | happy |
| IMG-UPD-MASK-IMMUTABLE-REGION | STOR-1-22 | negative (sync) |
| IMG-UPD-MASK-IMMUTABLE-SOURCE-SNAPSHOT | STOR-1-22 | negative (sync) |
| IMG-UPD-MASK-IMMUTABLE-FORMAT | STOR-1-22 | negative (sync) |
| IMG-UPD-MASK-UNKNOWN-FIELD | STOR-1-22 | negative (sync) |
| IMG-LST-CRUD-OK | STOR-1-31 | happy |
| IMG-LST-VAL-PROJECT-REQUIRED | STOR-1-31 | negative |
| IMG-LST-BVA-PAGESIZE-OVER-MAX | STOR-1-32 | negative (BVA; validate-before-authz) |
| IMG-LST-PAGE-TOKEN-GARBAGE | STOR-1-32 | negative (validate-before-authz) |
| IMG-LST-FILTER-NAME-MATCH | STOR-1-33 | happy (filter) |
| IMG-LST-FILTER-NAME-NONE | STOR-1-33 | edge (empty) |
| IMG-LST-PAGE-CURSOR | STOR-1-33 | edge (cursor) |
| IMG-GET-CONF-LEAN-PROJECTION | STOR-1-25 | conformance (two-projection INV-1) |
| IMG-UPD-BVA-DESC-256-257 | STOR-1-22 | BVA обе стороны границы (update-path, sync + details) |
| IMG-UPD-BVA-LABELS-64-65 | STOR-1-22 | BVA обе стороны границы (update-path, sync + details) |
| IMG-DEL-CRUD-OK | STOR-1-20 | happy |
| IMG-DEL-NEG-NOTFOUND | STOR-1-21 | negative (op-error) |
| IMG-VOL-CR-SOURCE-IMAGE-OK | STOR-1-18 | happy (boot-Volume materialize) |
| IMG-VOL-CR-SOURCE-XOR | STOR-1-19 | negative (sync mutual-exclusion) |
| IMG-VOL-CR-SOURCE-IMAGE-NOTFOUND | STOR-1-19 | negative (op-error FK) |
| IMG-VOL-CR-SOURCE-IMAGE-FOREIGN-REGION-NAMED | STOR-1-18 | negative (свой образ чужого региона → отказ НАЗВАН «must be in the same region», не hide-existence; сеет второй регион через internal geo) |
| IMG-VOL-CR-SOURCE-IMAGE-FOREIGN-PROJECT-HIDDEN | STOR-1-19 | negative (образ чужого проекта → текст настоящего промаха; разведение полос не создало оракула, security.md §6) |
| IMG-DEL-SETNULL-VOLUME-INTACT | STOR-1-28 | edge (FK SET NULL, том цел) |
| IMG-LOP-CRUD-OK | STOR-1-20 | happy |
| IMG-LOP-NEG-MALFORMED-ID | STOR-1-21 | negative (sync) |
| IMG-COPY-CRUD-OK | STOR-1-20 | happy (Copy в ДРУГОЙ регион: сеет второй регион, зону и действующую ревизию привязки) |
| IMG-COPY-VAL-PROJECT-REQUIRED | STOR-1-20 | negative (unscoped; право «создать» — у родителя) |
| IMG-COPY-VAL-TARGET-REGION-REQUIRED | STOR-1-20 | negative (sync) |
| IMG-COPY-NEG-TARGET-REGION-UNKNOWN | STOR-1-20 | negative (peer geo sync) |
| IMG-COPY-NEG-MALFORMED-ID | STOR-1-21 | negative (sync) |
| IMG-COPY-NEG-SOURCE-NOTFOUND | STOR-1-21 | negative (sync 404) |

## DiskType (`cases/disk-type.py`) — stage S2

**Каталог классов НЕ посеян** (`0016_disk_type_policy.sql` снял посев `0004`), поэтому ни
один кейс не вправе утверждать длину выдачи: пустой каталог — законный ответ. Утверждается
СВОЙСТВО каждой записи (ярус и состояние обращения — из закрытых словарей, ни одного
инфра-поля), и оно различающее: ярус свободной строкой роняет кейс. Классы и действующие
ревизии привязки заводит шаг подъёма стенда `make -C deploy seed-storage`.

| case-id | CS1 | класс |
|---|---|---|
| DT-LST-CRUD-OK | CS1-S2-01 | happy (посева нет — длина НЕ утверждается; утверждается свойство КАЖДОЙ записи) |
| DT-GET-CRUD-OK | CS1-S2-01 | happy |
| DT-GET-NEG-NOTFOUND | CS1-S2-01 | negative |
| DT-LST-BVA-PAGESIZE-OVER-MAX | CS1-S2-01 | negative (BVA) |
| DT-LST-PAGE-TOKEN-GARBAGE | CS1-S2-01 | negative (PAGE) |
| DT-CR-NEG-EXTERNAL-ABSENT | CS1-S2-04 | negative (INV-7a) |
| DT-UPD-NEG-EXTERNAL-ABSENT | CS1-S2-04 | negative (INV-7a) |
| DT-DEL-NEG-EXTERNAL-ABSENT | CS1-S2-04 | negative (INV-7a) |

## Operation (`cases/operation.py`) — OperationService via OpsProxy (sop-prefix)

Паритет с compute-набором доведён перед сжатием раскола блочного хранения (Get:
failed-op + контракт-тон not-found; Cancel: вся поверхность — её у storage не было).

| case-id | класс |
|---|---|
| OP-GET-CRUD-OK | happy |
| OP-GET-CRUD-FAILED-OP | negative (ветка `error` в `oneof result`) |
| OP-GET-NEG-NOTFOUND-VALID-PREFIX | negative |
| OP-GET-CONF-NF-TEXT | negative (контракт-тон сообщения) |
| OP-GET-NEG-UNKNOWN-PREFIX | negative |
| OP-CANCEL-NEG-ALREADY-DONE | negative (STATE) |
| OP-CANCEL-NEG-NOTFOUND | negative |
| OP-CANCEL-NEG-UNKNOWN-PREFIX | negative |

## Public-authz (`cases/authz.py`) — INV-10 (listauthz + object-scoped анти-BOLA)

`# requires` authz-fixture стенд (см. шапку `cases/authz.py`). Отказ на МУТАЦИИ и на
СПИСКЕ → 403 / code 7 / `permission denied`. Отказ на одиночном ЧТЕНИИ
(`<Resource>.Get`) → **404 текстом владельца**, байт в байт как настоящий промах
(`Volume <id> not found`): чтение гейтится глаголом `v_get` на самом объекте, и
различимый текст отказа был бы оракулом существования.

| case-id | CS1 | класс |
|---|---|---|
| AUTHZ-VOL-LIST-CROSS-DENY | CS1-S1-13 | negative (listauthz) |
| AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK | CS1-S1-13 | positive (no-leak) |
| AUTHZ-VOL-GET-CROSS-DENY | CS1-S1-14 | negative (анти-BOLA) |
| AUTHZ-VOL-UPDATE-CROSS-DENY | CS1-S1-14 | negative (анти-BOLA) |
| AUTHZ-VOL-DELETE-CROSS-DENY | CS1-S1-14 | negative (анти-BOLA) |
| AUTHZ-SNP-LIST-CROSS-DENY | CS1-S3-07 | negative (listauthz) |
| AUTHZ-SNP-GET-CROSS-DENY | CS1-S3-08 | negative (анти-BOLA) |
| AUTHZ-SNP-UPDATE-CROSS-DENY | CS1-S3-08 | negative (анти-BOLA) |
| AUTHZ-SNP-DELETE-CROSS-DENY | CS1-S3-08 | negative (анти-BOLA) |
| AUTHZ-VOL-LST-OVERSHOW-LEAK-GUARD | CS1-S1-13 | leak-guard (per-object listauthz **внутри** проекта) |
| AUTHZ-SNP-LST-OVERSHOW-LEAK-GUARD | CS1-S3-07 | leak-guard (per-object listauthz **внутри** проекта) |
| AUTHZ-IMG-LST-OVERSHOW-LEAK-GUARD | CS1-S2-* | leak-guard (per-object listauthz **внутри** проекта) |
| AUTHZ-VOL-VERB-CUT-NOT-TIER | CS1-S1-14 | verb-cut (выдача не шире названного: `v_list` да, `v_get`/`v_update`/`v_delete` нет) |

`AUTHZ-VOL-VERB-CUT-NOT-TIER` закрывает разрез, которого до перевода object-self RPC
storage на глаголы не существовало: пока эти RPC спрашивали ЯРУС, выдача, назвавшая
`create` и `list`, открывала на объекте ещё и чтение, правку и удаление — реконсайлер
кладёт рядом с `v_*` ярусный кортеж, выведенный из класса глаголов, а модель объявляет
`editor ⇒ viewer`. Предъявитель — `jwtStorageCreateListOnlyA` (узкая роль
`storage.volumes {create,list}` @ projectA1, посев `ps_storage_crlist_*`).
Положительная половина идёт ПЕРВОЙ и обязательна: `ListOperations` (`v_list`) обязан
пройти, иначе три отказа неотличимы от недоехавшей привязки. Парный контроль на ТОМ ЖЕ
объекте — полноправный `jwtProjectEditorA` правит его успешно.

Три `*-LST-OVERSHOW-LEAK-GUARD` закрывают измерение, которое до них было ЯВНОЙ дырой
(шапка `cases/authz.py`): набор проверял только кросс-**проектную** изоляцию, поэтому
зелёный прогон storage не был доказательством per-object фильтра страницы List
(`internal/authzfilter`, per-object `viewer` через iam BatchCheck). Форма — образец
compute `LF-INST-LST-OVERSHOW-LEAK-GUARD`: владелец создаёт объект в проекте сюиты,
**никогда**-не-гранченый аутентифицированный субъект (`jwtPureNoBindings`, НЕ
`jwtNoBindings` — того реально гранят iam-сюиты, kacho-iam#276) листит тот же проект,
объекта в выдаче быть не должно. Строго single-shot (retry маскировал бы утечку),
`pageSize=1000` (чтобы «не видно» не было артефактом первой страницы). Что именно
сторож доказывает и чего НЕ доказывает — буквально расписано в шапке `cases/authz.py`.

## SEC-D owner-tuple (`cases/sec-d.py`) — register/unregister через kaname

Полный цикл owner-hierarchy tuple на ВСЕХ трёх ресурсах, которые его эмитят
(`storage_volume` / `storage_snapshot` / `storage_image`): Create → op done+success →
Get владельцем → **прямая проба отношения** на cluster-internal `iam:check` →
Delete → Get 404 → проба снятия. Обратная сторона (unregister) проверяется явно:
односторонний кейс пропустил бы залипший grant на удалённый ресурс.

Кейсы гоняются под `jwtAccountAdminA` (владелец аккаунта суиты), **не** под дефолтным
cluster-admin: для последнего решение принимается каскадом до вопроса об объекте, и
кейс отдал бы 200 при полностью не материализованном tuple. Обоснование — в шапке
`cases/sec-d.py`.

| case-id | ресурс / FGA-тип | класс |
|---|---|---|
| SECD-VOL-CR-GET-AFTER-TUPLE-OK | Volume / `storage_volume` | happy (register+unregister) |
| SECD-SNP-CR-GET-AFTER-TUPLE-OK | Snapshot / `storage_snapshot` | happy (register+unregister) |
| SECD-IMG-CR-GET-AFTER-TUPLE-OK | Image / `storage_image` | happy (register+unregister) |
| SECD-DEL-NEG-NOT-FOUND | Volume | negative (нет Operation → нет осиротевшего intent) |

## Каталог DiskType — матрица доступа (`cases/authz-catalog.py`)

Шесть субъектов × три операции над admin-каталогом DiskType: публичное чтение
(List/Get) доступно любому аутентифицированному, административная мутация — никому
из них. Матрица зеркалит compute-набор для того же каталога, чтобы сжатие раскола
блочного хранения не унесло единственную живую проверку этой поверхности.

Две линии отказа различаются намеренно: без учётных данных → 401 / code 16
UNAUTHENTICATED; с учётными данными, но без права → 403 / code 7 PERMISSION_DENIED.

| case-id | scope | вердикт |
|---|---|---|
| SDT-LS-ANON | catalog-read | UNAUTH (401/16) |
| SDT-LS-NOB | catalog-read | ALLOW (200) |
| SDT-LS-PA1 | catalog-read | ALLOW (200) |
| SDT-LS-AAA | catalog-read | ALLOW (200) |
| SDT-LS-AAB | catalog-read | ALLOW (200) |
| SDT-LS-INV | catalog-read | ALLOW (200) |
| SDT-GT-ANON | catalog-read | UNAUTH (401/16) |
| SDT-GT-NOB | catalog-read | ALLOW (200) |
| SDT-GT-PA1 | catalog-read | ALLOW (200) |
| SDT-GT-AAA | catalog-read | ALLOW (200) |
| SDT-GT-AAB | catalog-read | ALLOW (200) |
| SDT-GT-INV | catalog-read | ALLOW (200) |
| SDT-CR-ANON | catalog-mutate | UNAUTH (401/16) |
| SDT-CR-NOB | catalog-mutate | DENY (403/7) |
| SDT-CR-PA1 | catalog-mutate | DENY (403/7) |
| SDT-CR-AAA | catalog-mutate | DENY (403/7) |
| SDT-CR-AAB | catalog-mutate | DENY (403/7) |
| SDT-CR-INV | catalog-mutate | DENY (403/7) |

## InternalVolumeService (`cases/internal-volume.py`) — stage S4 (заметка)

Файл `internal-*` — validate-cases освобождает его от таблицы-паттернов (каталогизирован
**этой заметкой**, как в vpc `internal-*.py`). Кейсы `IVOL-{ATTACH,DETACH,LISTATTACHMENTS,
GETINTERNAL}-EXTERNAL-ABSENT` — INV-7a: Internal-only RPC отсутствуют на external endpoint
(→ 404), провокабельная часть CS1-S4-11. Attach-CAS happy/negative/race (CS1-S4-01..12) —
**integration-only** (:9091 mTLS + seeded Instance + concurrent `-race`), не black-box
(см. `docs/RESULTS.md` «Integration-only»).
