# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для VolumeService (kacho-storage) — stage S1 CS1-S1-*.

Covered public RPCs: Get, List, Create, Update, Delete, ListOperations
(REST /storage/v1/volumes; async мутации → Operation, poll /operations/{sop…}).

Контракт изоляции: каждый case в своём runId, работает внутри pre-allocated
existingProjectId (_suiteProjectId из env), Org/Cloud/Project НЕ создаёт; имена
суффиксуются {{runId}}. id-prefix Volume = `vol`, op-root storage = `sop`.

Error-тексты §0.2 acceptance (нормативны — часть контракта) assert'ятся
behaviour-level (код И точное сообщение). sizeBytes[>0].

ТОМ РОЖДАЕТСЯ В НАМЕРЕНИИ. `Operation.done` означает «строка закоммичена», и
ТОЛЬКО это; пригодным том объявляет СВЕРЩИК, увидев объект у плоскости данных.
Поэтому всё, что требует готовности — рост размера, снятие снимка, захват образа,
смена класса, — ждёт её `wait_until_ready`, а не считает завершение операции
доказательством существования объекта. Ожидание ограничено и падает, назвав
наблюдённое `status` и `statusReason`.

`blockSize` СНЯТ С КОНТРАКТА (номер 11 и имя зарезервированы): размер блока задаёт
бэкенд вместе с классом диска, а не арендатор по каждому тому. Здесь он ни в одном
теле не шлётся, а его ОТСУТСТВИЕ в ответе утверждается — иначе снятие поля прошло
бы мимо чёрного ящика.

Не-black-box (integration-only, НЕ здесь): attach-CAS (Internal :9091, см.
cases/internal-volume.py); listauthz/anti-BOLA (cases/authz.py, fixture-gated).
"""

CASES = []

VOL = "/storage/v1/volumes"
DT = "/storage/v1/diskTypes"

_DEF_SIZE = 10737418240   # 10 GiB
_GROW_SIZE = 21474836480  # 20 GiB
_SHRINK_SIZE = 5368709120  # 5 GiB

# Закрытые словари ответа. Публичная поверхность обязана отдавать значение ИЗ НИХ:
# свободная строка в состоянии или причине — прямой канал утечки физики (имя пула,
# координата узла), которого гейт двухпроекционности не увидит, потому что
# перечисляет ИМЕНА полей, а не значения.
_VOL_STATUSES = ["STATUS_UNSPECIFIED", "CREATING", "AVAILABLE", "IN_USE",
                 "DELETING", "ERROR", "MIGRATING"]
_STATUS_REASONS = ["STATUS_REASON_UNSPECIFIED", "BACKEND_UNAVAILABLE", "BACKEND_REJECTED",
                   "BACKEND_CAPACITY_EXHAUSTED", "SOURCE_NOT_READY", "PRECONDITION_FAILED",
                   "INTERNAL_ERROR"]


def _vol_body(suffix, **over):
    b = {"projectId": "{{_suiteProjectId}}", "name": f"vol-{suffix}-{{{{runId}}}}",
         "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
         "sizeBytes": _DEF_SIZE}
    b.update(over)
    return b


def _assert_msg(substr):
    """Assert точного (case-sensitive) вхождения нормативного §0.2-текста в message."""
    # substr вставляется в single-quoted JS-строку — экранируем backslash и '
    # (контракт-тексты вида "invalid volume id 'x'" несут одинарные кавычки, иначе
    # ломают pm.test → "missing ) after argument list").
    _esc = substr.replace("\\", "\\\\").replace("'", "\\'")
    return [f"pm.test('message includes \"{_esc}\"', "
            f"() => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('{_esc}'));"]


# ---------------------------------------------------------------------------
# CS1-S1-01 — Create happy → Operation → poll READY → Get
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-CRUD-OK",
    title="Create Volume → Operation(vol metadata) → ДОЖДАТЬСЯ пригодности (её объявляет сверщик) → Get: vol-prefix, status AVAILABLE, statusReason не заполнена, blockSize отсутствует, createdAt sec, attachments/usedBy пусты",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S1-01
    #
    # Прежняя редакция читала состояние СРАЗУ после `done` и требовала AVAILABLE.
    # Это утверждало не о контракте, а о том, успел ли обход сверщика: том
    # рождается в намерении, и готовность производит тот, кто УВИДЕЛ объект у
    # плоскости данных. Ожидание ограничено и падает, назвав status и statusReason.
    steps=[
        Step(name="create", method="POST", path=VOL,
             body=_vol_body("cr", description="newman CRUD-OK", labels={"suite": "newman"}),
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.test('metadata.volumeId prefix vol', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.volumeId).to.match(/^vol/));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        wait_until_ready(Step(name="get", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('id matches & vol prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('volumeId')); pm.expect(j.id).to.match(/^vol/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                          "pm.test('zoneId matches', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                          "pm.test('diskTypeId matches', () => pm.expect(j.diskTypeId).to.eql(pm.environment.get('existingDiskTypeId')));",
                          "pm.test('sizeBytes matches', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_DEF_SIZE) + "'));",
                          # Снятое с контракта поле не возвращается. Край отдаёт публичную
                          # проекцию с явными нулевыми значениями (EmitUnpopulated), поэтому
                          # ЖИВОЕ поле в ответе присутствует всегда — отсутствие ключа здесь
                          # различающе, а не следствие пустого значения.
                          "pm.test('blockSize снят с контракта — ключа в ответе нет', () => pm.expect(j).to.not.have.property('blockSize'));",
                          "pm.test('statusReason не заполнена у пригодного тома', () => pm.expect(String(j.statusReason)).to.eql('STATUS_REASON_UNSPECIFIED'));",
                          "pm.test('attachments empty', () => pm.expect(j.attachments || []).to.be.an('array').that.is.empty);",
                          "pm.test('usedBy empty', () => pm.expect(j.usedBy || []).to.be.an('array').that.is.empty);",
                          *assert_created_at_seconds()]), ready="AVAILABLE", subject="Volume"),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-71 — object-self Get/Update/Delete under a PROJECT-SCOPED actor (#71)
#
# The rest of this suite runs CRUD as jwtBootstrap (cluster system_admin), whose
# Check SHORT-CIRCUITS the per-object storage_volume Check — so it can 200 a
# volume GET even when the storage_volume FGA type / reconciler wiring is missing,
# MASKING that a real project-scoped tenant is denied (verified live: same volume,
# jwtBootstrap GET=200 but jwtProjectEditorA GET=403 "no authorization path"). That
# false-green shipped storage object-self reads broken (#71). This case exercises
# the tenant anti-BOLA path as jwtProjectEditorA (editor on _suiteProjectId) so the
# suite actually covers it. retry_until_authorized absorbs ONLY the read-your-writes
# owner-tuple materialization window (never a real deny — a genuine 403 still fails
# the assertion once the budget is spent).
#
# ЗДЕСЬ СТОЯЛО «RED until the #71 FGA model+wiring deploys … GREEN after» И ПОМЕТКА
# `# verifies #71` НА САМОМ КЕЙСЕ. Оба пережили свой предмет: типы `storage_volume` /
# `storage_snapshot` / `storage_image` в модели есть
# (`proto/kaname/cloud/iam/v1/fga_model.fga`), тикет `kacho#71` закрыт COMPLETED
# 2026-08-06 — значит объявление «ожидаемо красный» стало ЛОЖНЫМ утверждением о
# продукте, а пометка выкупала из «всё обязано быть зелёным» кейс, который обязан быть
# зелёным. Отдельно: пометка называла тикет БЕЗ репозитория (`#71`), а такую ссылку
# нельзя разрешить — у неё не было срока жизни даже в принципе.
#
# ЧТО ЗАЩИЩАЕТ КЕЙС: полный object-self CRUD своего тома выполняется под
# ПРОЕКТНО-ОГРАНИЧЕННЫМ действующим лицом, а не только под кластерным администратором,
# чья проверка короткозамыкает пообъектную. Именно эта пара и отличает «доступ
# материализован» от «маскируется вышестоящим правом».
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-OBJSELF-PROJECT-SCOPED-CRUD",
    title="[#71] project-scoped editor Get/Update/Delete OWN volume → 200 (object-self anti-BOLA; NOT cluster-admin-masked)",
    classes=["CRUD", "AUTHZ", "CONF"], priority="P0",
    steps=[
        # Create as the default cluster-admin (reliable seed); the volume lands in
        # _suiteProjectId, on which jwtProjectEditorA holds an editor binding.
        Step(name="cr", method="POST", path=VOL, body=_vol_body("objself"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        # GET own volume as the PROJECT-SCOPED editor → object-self Check on
        # storage_volume:<id> must resolve v_get for the editor (materialized from
        # the project binding). Pre-#71: 403 (type missing) → RED.
        #
        # Ожидание здесь двойное по предмету и одно по механике: окно видимости
        # owner-tuple И окно пригодности тома. Второе обязательно — рост размера
        # применяется размер-CAS'ом только к ГОТОВОМУ тому, поэтому без него правка
        # ниже отвергалась бы предусловием, а кейс называл бы виновником authz.
        wait_until_ready(Step(name="objself-get", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             auth="jwtProjectEditorA",
             test_script=["const j = pm.response.json();",
                          "pm.test('project-editor resolves own volume (v_get materialized)', () => pm.expect(j.id).to.eql(pm.environment.get('volumeId')));"]),
             ready="AVAILABLE", subject="Volume"),
        # UPDATE (grow) as the project-scoped editor → object-self v_update.
        retry_until_authorized(Step(name="objself-patch", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             auth="jwtProjectEditorA",
             body={"updateMask": "sizeBytes", "sizeBytes": _GROW_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          "pm.test('project-editor Operation envelope (v_update materialized)', () => pm.expect(pm.response.json().id).to.match(/^sop/));"])),
        # The Update op was CREATED by jwtProjectEditorA → it must be POLLED by the same
        # creator: OperationService.Get is creator-only (checkOperationOwnership, анти-BOLA);
        # polling under the default cluster-admin actor → gateway denies 404 (#71 fixture-fix).
        poll_operation_until_done(auth="jwtProjectEditorA"), assert_op_success(auth="jwtProjectEditorA"),
        # DELETE as the project-scoped editor → object-self v_delete (editor co-materializes delete).
        retry_until_authorized(Step(name="objself-delete", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             auth="jwtProjectEditorA",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          "pm.test('project-editor deletes own volume (v_delete materialized)', () => pm.expect(pm.response.json().id).to.match(/^sop/));"])),
        # Delete op created by jwtProjectEditorA → poll under the same creator (creator-only Op.Get).
        poll_operation_until_done(auth="jwtProjectEditorA"), assert_op_success(auth="jwtProjectEditorA"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-02 — Get: malformed id (sync INVALID_ARGUMENT) + well-formed NotFound
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-GET-NEG-MALFORMED-ID",
    title="Get malformed volumeId 'not-a-vol-id' → sync 400 INVALID_ARGUMENT 'invalid resource id ...' (первым стейтментом)",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies CS1-S1-02
    steps=[Step(name="get-malformed", method="GET", path=f"{VOL}/not-a-vol-id",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid resource id 'not-a-vol-id'")])],
))

CASES.append(Case(
    id="VOL-GET-NEG-NOTFOUND",
    title="Get well-formed-но-нет volumeId → 404 NOT_FOUND 'Volume <id> not found'",
    classes=["NEG", "CONF"], priority="P0",
    # verifies CS1-S1-02
    steps=[Step(name="get-nx", method="GET", path=f"{VOL}/{{{{garbageStorageId}}}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             *_assert_msg("Volume vol00000000000000000 not found")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-03 — List: project-scope + pagination BVA + filter
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-LST-CRUD-OK",
    title="List volumes в project → volumes array (project-scoped)",
    classes=["CRUD"], priority="P1",
    # verifies CS1-S1-03
    steps=[Step(name="list", method="GET", path=f"{VOL}?projectId={{{{_suiteProjectId}}}}",
                test_script=[*assert_status(200),
                             "pm.test('volumes is array', () => pm.expect(pm.response.json().volumes || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="VOL-LST-VAL-PROJECT-REQUIRED",
    title="List без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped; #62 project-scope)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S1-03
    steps=[Step(name="list-np", method="GET", path=VOL,
                test_script=[*assert_unscoped_rejected()])],
))

CASES.append(Case(
    id="VOL-LST-BVA-PAGESIZE-OVER-MAX",
    title="List pageSize=5000 (> max 1000) → 400 INVALID_ARGUMENT",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    # verifies CS1-S1-03
    steps=[Step(name="ps-over", method="GET", path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=5000",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="VOL-LST-PAGE-TOKEN-GARBAGE",
    title="List с garbage pageToken → 400 INVALID_ARGUMENT (opaque token не декодируется)",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies CS1-S1-03
    steps=[Step(name="bad-token", method="GET",
                path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=not-a-real-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="VOL-LST-FILTER-NAME-MATCH",
    title="Create → List filter=name=\"X\" → созданный том в результате (whitelist name), cursor page_size",
    classes=["FILTER", "CRUD"], priority="P2",
    # verifies CS1-S1-03
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("flt"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        Step(name="list-filtered", method="GET",
             path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=1000&filter=name%3D%22vol-flt-{{{{runId}}}}%22",
             test_script=[*assert_status(200),
                          "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                          "pm.test('filtered list contains created', () => pm.expect(ids).to.include(pm.environment.get('volumeId')));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-04 — Update size increase-only (grow OK, shrink/equal → op-error)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-UPD-SIZE-GROW-OK",
    title="Update mask=size_bytes рост (10→20 GiB) → Operation ok; Get sizeBytes больше (online, derived status)",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("grow", sizeBytes=_DEF_SIZE),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        # Размер-CAS применяется ТОЛЬКО к готовому тому: без ожидания рост отвергался
        # бы предусловием, а падал бы шаг проверки размера — то есть невиновный.
        wait_until_ready(Step(name="await-ready", method="GET", path=f"{VOL}/{{{{volumeId}}}}"),
                         ready="AVAILABLE", subject="Volume"),
        retry_until_authorized(Step(name="patch-grow", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "sizeBytes", "sizeBytes": _GROW_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('sizeBytes grew', () => pm.expect(String(pm.response.json().sizeBytes)).to.eql('" + str(_GROW_SIZE) + "'));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-UPD-SIZE-SHRINK-REJECT",
    title="Update mask=size_bytes уменьшение → Operation error INVALID_ARGUMENT 'Volume size can only be increased'",
    classes=["NEG", "STATE", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("shrink", sizeBytes=_GROW_SIZE),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        Step(name="patch-shrink", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "sizeBytes", "sizeBytes": _SHRINK_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(3, "INVALID_ARGUMENT", msg_regex="Volume size can only be increased"),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-UPD-SIZE-EQUAL-REJECT",
    title="Update mask=size_bytes равно текущему → Operation error INVALID_ARGUMENT 'Volume size can only be increased' (не строго больше)",
    classes=["NEG", "STATE", "BVA", "CONF"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("equal", sizeBytes=_DEF_SIZE),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        Step(name="patch-equal", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "sizeBytes", "sizeBytes": _DEF_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(3, "INVALID_ARGUMENT", msg_regex="Volume size can only be increased"),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-UPD-CRUD-NAME-DESC-LABELS-OK",
    title="Update mask=name,description,labels → все три применены (Get)",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("upd", description="init", labels={"orig": "1"}),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "name,description,labels", "name": "vol-upd2-{{runId}}",
                   "description": "updated-newman", "labels": {"env": "prod"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('name updated', () => pm.expect(j.name).to.match(/^vol-upd2-/));",
                          "pm.test('description updated', () => pm.expect(j.description).to.eql('updated-newman'));",
                          "pm.test('label env', () => pm.expect((j.labels || {}).env).to.eql('prod'));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-05 — Update immutable in mask (sync) + unknown field + empty-mask PATCH
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-UPD-MASK-IMMUTABLE-ZONE",
    title="Update mask=zone_id → sync 400 INVALID_ARGUMENT 'zone_id is immutable after Volume.Create' (immutable-switch до UpdateMask)",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-05
    # The MASK carries the assertion. `zoneId` is not a field of
    # UpdateVolumeRequest by design — a placement anchor is not expressible in
    # Update — so such a body key would be dropped at the edge before the
    # service saw it: present in the fixture, absent from the contract.
    steps=[Step(name="patch-imm-zone", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "zoneId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("zone_id is immutable after Volume.Create")])],
))

CASES.append(Case(
    id="VOL-UPD-MASK-IMMUTABLE-DISKTYPE",
    title="Update mask=disk_type_id → sync 400 INVALID_ARGUMENT 'disk_type_id is immutable after Volume.Create'",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-05
    # Mask-driven, as above: `diskTypeId` is not a field of UpdateVolumeRequest.
    steps=[Step(name="patch-imm-dt", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "diskTypeId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("disk_type_id is immutable after Volume.Create")])],
))

CASES.append(Case(
    id="VOL-UPD-MASK-UNKNOWN-FIELD",
    title="Update mask=nonexistent_field → sync 400 INVALID_ARGUMENT (unknown field, known-set)",
    classes=["VAL", "STATE"], priority="P1",
    # verifies CS1-S1-05
    steps=[Step(name="patch-unk", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "nonexistent_field", "description": "x"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-06 — partial UNIQUE(project_id, name) — dup → op-error ALREADY_EXISTS
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-NEG-DUP-NAME",
    title="Create duplicate name в том же project → Operation error ALREADY_EXISTS 'volume with name <n> already exists in project'",
    classes=["NEG", "CONC", "CONF"], priority="P1",
    # verifies CS1-S1-06
    steps=[
        Step(name="cr-1", method="POST", path=VOL, body=_vol_body("dup"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="cr-2-dup", method="POST", path=VOL, body=_vol_body("dup"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Целиком: подстрока без имени ресурса не отличает дубль тома от дубля образа.
        assert_op_error(6, "ALREADY_EXISTS", msg_regex="volume with name [^ ]+ already exists in project"),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-CR-CRUD-EMPTY-NAME-OK",
    title="Create два тома с пустым name в одном project → у каждого имя подставлено СВОИМ id, оба Operation ok",
    classes=["CRUD", "BVA"], priority="P2",
    # verifies CS1-S1-06
    #
    # Прежний заголовок объяснял сосуществование тем, что «partial UNIQUE не
    # действует на name=''». Это объяснение пережило свой предмет: пустое имя
    # больше не доживает до вставки — validate.NameOrDefault подставляет вместо
    # него идентификатор ресурса, поэтому в базе лежат ДВА РАЗНЫХ непустых
    # имени, и уникальность соблюдена обычным образом, а не в обход неё.
    #
    # Утверждается именно подстановка, а не «оба создались»: кейс, проверяющий
    # только успех операций, остаётся зелёным и тогда, когда оба имени пусты, —
    # то есть ровно при том поведении, которое контракт теперь запрещает.
    steps=[
        Step(name="cr-a", method="POST", path=VOL, body=_vol_body("noname", name=""),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeAId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get-a-name-substituted", method="GET",
             path=f"{VOL}/{{{{volumeAId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('имя тома A подставлено его идентификатором', () => "
                          "pm.expect(pm.response.json().name, pm.response.text())"
                          ".to.eql(pm.environment.get('volumeAId')));"])),
        Step(name="cr-b", method="POST", path=VOL, body=_vol_body("noname2", name=""),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeBId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get-b-name-substituted", method="GET",
             path=f"{VOL}/{{{{volumeBId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('имя тома B подставлено его идентификатором', () => "
                          "pm.expect(pm.response.json().name, pm.response.text())"
                          ".to.eql(pm.environment.get('volumeBId')));",
                          "pm.test('подставленные имена различны — уникальность соблюдена, а не обойдена', () => "
                          "pm.expect(pm.environment.get('volumeAId'))"
                          ".to.not.eql(pm.environment.get('volumeBId')));"])),
        Step(name="cleanup-a", method="DELETE", path=f"{VOL}/{{{{volumeAId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-b", method="DELETE", path=f"{VOL}/{{{{volumeBId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-07 — Delete happy + Delete well-formed-nonexistent → op-error NOT_FOUND
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-DEL-CRUD-OK",
    title="Delete Volume → Operation ok (response Empty); Get → 404 NOT_FOUND",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies CS1-S1-07
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("delok"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get-404", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          *_assert_msg("not found")]),
    ],
))

CASES.append(Case(
    id="VOL-DEL-NEG-NOTFOUND",
    title="Delete well-formed-но-нет volumeId → Operation error NOT_FOUND 'Volume <id> not found' (0-row DELETE)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S1-07
    steps=[
        Step(name="del-nx", method="DELETE", path=f"{VOL}/{{{{garbageStorageId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Целиком, а не «not found»: голая подстрока зеленела на сообщении о ЛЮБОМ ресурсе.
        assert_op_error(5, "NOT_FOUND", msg_regex="Volume [^ ]+ not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-08 — peer-validate zoneId (cross-service geo, sync fail-closed)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-NEG-ZONE-UNKNOWN",
    title="Create с unknown zoneId → sync 400 FAILED_PRECONDITION 'unknown zone id '<X>'' (peer geo.ZoneService.Get NotFound)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-08
    # # requires peer-validation enabled (geo peer reachable)
    steps=[Step(name="cr-bad-zone", method="POST", path=VOL, body=_vol_body("bz", zoneId="region-9-z"),
                test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             *_assert_msg("unknown zone id 'region-9-z'")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-09 — peer-validate projectId (cross-service iam, sync fail-closed)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-NEG-PROJECT-NOTFOUND",
    title="Create с garbage projectId → sync FAILED_PRECONDITION 'Project <id> not found' (peer iam.ProjectService.Get)",
    classes=["NEG", "CONF"], priority="P0",
    # verifies CS1-S1-09
    # # requires peer-validation enabled (iam peer reachable)
    steps=[Step(name="cr-bad-proj", method="POST", path=VOL, body=_vol_body("bp", projectId="{{garbageProjectId}}"),
                test_script=["pm.test('status 400 (FAILED_PRECONDITION; 412 краем не производится)', () => pm.expect(pm.response.code).to.eql(400));",
                             *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             *_assert_msg("Project b1gnonexistent999999 not found")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-10 — same-DB FK on diskTypeId / sourceSnapshotId (op-error FAILED_PRECONDITION)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-NEG-DISKTYPE-NOTFOUND",
    title="Create с несуществующим diskTypeId=block-unicorn → Operation error FAILED_PRECONDITION 'DiskType block-unicorn not found' (same-DB FK RESTRICT)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S1-10
    steps=[
        Step(name="cr-bad-dt", method="POST", path=VOL, body=_vol_body("bdt", diskTypeId="block-unicorn"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(9, "FAILED_PRECONDITION", msg_regex="DiskType block-unicorn not found"),
    ],
))

CASES.append(Case(
    id="VOL-CR-NEG-SNAPSHOT-NOTFOUND",
    title="Create с несуществующим sourceSnapshotId → Operation error FAILED_PRECONDITION 'Snapshot <id> not found' (same-DB FK)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S1-10
    steps=[
        Step(name="cr-bad-snap", method="POST", path=VOL,
             body=_vol_body("bsnap", sourceSnapshotId="{{garbageSnapshotId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(9, "FAILED_PRECONDITION", msg_regex="Snapshot snp00000000000000000 not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-11 — lean public projection (no infra fields) [INV-6]
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-GET-CONF-LEAN-PROJECTION",
    title="Public Volume.Get → только tenant-facing поля; НЕТ инфра-полей (backend-LUN/nvme/storage-node/pool-id/capacity) [INV-6]",
    classes=["CONF", "SEC"], priority="P1",
    # verifies CS1-S1-11
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("lean"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const forbidden = ['backendLun','nvmeNamespace','storageNode','poolId','capacityBytes','infraId','backend_lun','storage_node','pool_id','bindingId','backendObject','desiredBindingId'];",
                          "pm.test('no infra fields on public projection', () => forbidden.forEach(k => pm.expect(j, 'leaked infra field ' + k).to.not.have.property(k)));",
                          "const body = JSON.stringify(j).toLowerCase();",
                          "pm.test('no lun/nvme/pool tokens in body', () => { pm.expect(body).to.not.include('nvme'); pm.expect(body).to.not.include('backendlun'); pm.expect(body).to.not.include('storagenode'); });",
                          # Состояние и причина — ЗАКРЫТЫЕ словари, и это не косметика:
                          # свободная строка причины несёт текст бэкенда целиком (имя пула,
                          # координата узла), а гейт двухпроекционности перечисляет ИМЕНА
                          # полей и такое значение пропускает. Канал закрывается по
                          # ЗНАЧЕНИЯМ — здесь это и проверяется.
                          f"const STATUSES = {_VOL_STATUSES!r}.map(String);",
                          f"const REASONS = {_STATUS_REASONS!r}.map(String);",
                          "pm.test('status из закрытого словаря', () => pm.expect(STATUSES, JSON.stringify(j)).to.include(String(j.status)));",
                          "pm.test('statusReason из закрытого словаря (свободной строки бэкенда наружу нет)', () => pm.expect(REASONS, JSON.stringify(j)).to.include(String(j.statusReason)));"])),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-12 — input-validation sizeBytes / name (sync)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-VAL-SIZE-ZERO",
    title="Create sizeBytes=0 → sync 400 INVALID_ARGUMENT 'Illegal argument size_bytes'",
    classes=["VAL", "NEG", "BVA", "CONF"], priority="P0",
    # verifies CS1-S1-12
    steps=[Step(name="cr-size0", method="POST", path=VOL, body=_vol_body("sz0", sizeBytes=0),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument size_bytes")])],
))

CASES.append(Case(
    id="VOL-CR-VAL-NAME-UPPERCASE",
    title="Create name=Data_Uppercase (uppercase) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-upper", method="POST", path=VOL, body=_vol_body("up", name="Data_Uppercase"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="VOL-CR-VAL-NAME-UNICODE",
    title="Create name=том (кириллица/не-ASCII) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-unicode", method="POST", path=VOL, body=_vol_body("uni", name="том"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-15 — ListOperations (per-resource op-log) — happy + malformed-id
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-LOP-CRUD-OK",
    title="ListOperations volume → ≥1 op (create), каждый с sop-id, done, порядок (createdAt,id)",
    classes=["CRUD"], priority="P1",
    # verifies CS1-S1-15
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("lop"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-ops", method="GET", path=f"{VOL}/{{{{volumeId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const ops = pm.response.json().operations || [];",
                          "pm.test('at least 1 op', () => pm.expect(ops.length).to.be.at.least(1));",
                          "pm.test('op ids sop-prefixed', () => ops.forEach(o => pm.expect(o.id).to.match(/^sop/)));"])),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-LOP-NEG-MALFORMED-ID",
    title="ListOperations malformed volumeId → sync 400 INVALID_ARGUMENT 'invalid resource id ...' (парити с Get)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-15
    steps=[Step(name="lop-malformed", method="GET", path=f"{VOL}/not-a-vol/operations",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid resource id 'not-a-vol'")])],
))

# ---------------------------------------------------------------------------
# Lifecycle conformance (Create→Get→List-includes→Update→Get→Delete→List-excludes→Get-404)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-LIFECYCLE-CONF",
    title="Full lifecycle conformance: CRUD-инварианты Volume",
    classes=["CRUD", "CONF", "STATE"], priority="P1",
    # verifies CS1-S1-01, CS1-S1-04, CS1-S1-07
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("life"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-1", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), "pm.test('id', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('volumeId')));"])),
        Step(name="lst-includes", method="GET", path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().volumes || []).map(x => x.id);",
                          "pm.test('list contains', () => pm.expect(ids).to.include(pm.environment.get('volumeId')));"]),
        Step(name="upd", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "description", "description": "life-conf"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-after-upd", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), "pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('life-conf'));"]),
        Step(name="del", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="lst-excludes", method="GET", path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().volumes || []).map(x => x.id);",
                          "pm.test('list does not contain', () => pm.expect(ids).to.not.include(pm.environment.get('volumeId')));"]),
        Step(name="get-404", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-12 (add) — name BVA/ECP parity (over-max len, underscore, hyphen-start).
#   Форма имени — ЕДИНСТВЕННАЯ в дереве (pkg/validate.NameForm, DNS label по
#   RFC 1123): строчные буквы, цифры, дефис; первый и последний символ — буква
#   ИЛИ ЦИФРА; 1..63. Нарушение → фикс. текст "Illegal argument name".
#   Техники: BVA (верхняя граница длины 63+1), ECP (символ вне набора:
#   подчёркивание; дефис по краю). Парити с IMG-CR-BVA-NAME-OVER-64.
#
#   Здесь стояло «недопустимый первый символ: цифра / дефис» и своя копия
#   регулярки `^[a-z](...)`. Оба утверждения пережили свой предмет: цифра первым
#   символом теперь законна, а копия формы разошлась бы с каноном в день его
#   правки — молча, потому что заголовок кейса никто не перечитывает.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-BVA-NAME-OVER-64",
    title="Create name длиной 64 (граница 1..63 + 1) -> sync 400 INVALID_ARGUMENT 'Illegal argument name' (BVA верхняя граница; domain RuneCount>63)",
    classes=["BVA", "VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-name64", method="POST", path=VOL,
                body=_vol_body("n64", name="n" + "abcdefghij" * 6 + "abc"),  # 1+60+3 = 64
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ЗДЕСЬ БЫЛ КЕЙС «имя начинается с цифры → 400» (VOL-CR-VAL-NAME-DIGIT-START).
# Его предмета больше нет: DNS label по RFC 1123 разрешает цифру первым символом,
# и '9data-vol' теперь ЗАКОННОЕ имя тома. Кейс не удалён, а переведён на ось,
# которая у формы действительно сузилась и отрицания у storage не имела, —
# подчёркивание (прежний `displayNameRe` его тоже не принимал, но проверено это
# не было ни одним кейсом).
CASES.append(Case(
    id="VOL-CR-VAL-NAME-UNDERSCORE",
    title="Create name 'data_vol' (подчёркивание) -> sync 400 INVALID_ARGUMENT 'Illegal argument name' (форма имени: буквы, цифры, дефис)",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-underscore", method="POST", path=VOL, body=_vol_body("us", name="data_vol"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="VOL-CR-VAL-NAME-HYPHEN-START",
    title="Create name '-data-vol' (первый символ - дефис) -> sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-hyphen", method="POST", path=VOL, body=_vol_body("hy", name="-data-vol"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ---------------------------------------------------------------------------
# CS1-S1-05 (add) — mask-parity: снятый с контракта слот + immutable
#   source_snapshot_id + пустой mask = full-PATCH. immutable-switch (ДО UpdateMask,
#   api-conventions gotcha) для immutable-полей Volume {zone_id, disk_type_id,
#   source_snapshot_id, source_image_id, used_by}; existing покрывает
#   zone_id/disk_type_id. Техника state-transition (immutable после Create).
#   UpdateVolumeRequest не несёт тела source_snapshot_id -> триггер именно
#   mask-path (immutable-switch).
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-UPD-MASK-RETIRED-BLOCKSIZE-REJECTED",
    title="Update mask=blockSize (слот СНЯТ с контракта, номер и имя зарезервированы) -> СИНХРОННЫЙ 400 INVALID_ARGUMENT; молча принят быть не может",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S1-05
    #
    # ЗДЕСЬ СТОЯЛ ПИН НА ТЕКСТ «block_size is immutable after Volume.Create». Он
    # пережил свой предмет: поле снято с контракта целиком (Volume номер 11 и
    # CreateVolumeRequest номер 8 зарезервированы номером И именем), а «неизменяемое
    # после Create» — утверждение о ПОЛЕ РЕСУРСА, которого больше нет. Пин на такой
    # текст запирал бы сервис в описании снятой величины и краснел бы на уборке,
    # которая контракта не меняет.
    #
    # Что утверждается вместо: маска, называющая снятый слот, отвергается СИНХРОННО
    # и с INVALID_ARGUMENT. Это и есть предмет — принято-и-проигнорировано не исход:
    # приняв такую маску, край ответил бы успехом на правку величины, которой нет.
    # Утверждение переживёт и то, что отказ придёт от известного набора маски, а не
    # от перечня неизменяемых: обе полосы — отказ, и обе сохраняют смысл.
    steps=[Step(name="patch-retired-bs", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "blockSize"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="VOL-UPD-MASK-IMMUTABLE-SOURCESNAPSHOT",
    title="Update mask=source_snapshot_id -> sync 400 INVALID_ARGUMENT 'source_snapshot_id is immutable after Volume.Create'",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S1-05
    steps=[Step(name="patch-imm-srcsnap", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "sourceSnapshotId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("source_snapshot_id is immutable after Volume.Create")])],
))

CASES.append(Case(
    id="VOL-UPD-MASK-EMPTY-FULL-PATCH-OK",
    title="Update пустой updateMask -> full-object PATCH: mutable name+description применены; immutable zone не тронут; Operation ok, Get отражает (CS1-S1-05 пустой mask = full-PATCH)",
    classes=["CRUD", "STATE", "CONF"], priority="P1",
    # verifies CS1-S1-05
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("empmask", description="init-desc"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-empty-mask", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "", "name": "vol-empmask2-{{runId}}", "description": "full-patch-desc"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('name applied (full-PATCH)', () => pm.expect(j.name).to.match(/^vol-empmask2-/));",
                          "pm.test('description applied (full-PATCH)', () => pm.expect(j.description).to.eql('full-patch-desc'));",
                          "pm.test('zoneId unchanged (immutable, full-PATCH не трогает)', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-11 (add) — INV-8 no-leak black-box lock (error-guessing: injection).
#   Payload в name / filter не должен вызвать 500 и НЕ должен утечь pgx/SQLSTATE/
#   panic/goroutine наружу (фикс. INTERNAL / контрактный InvalidArgument). Парити
#   с compute security_injection_block; фокус на observable no-leak инварианте.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-SEC-NAME-INJECTION",
    title="Security: SQL-injection payload в name -> НЕ 500; нет утечки pgx/SQLSTATE/panic/goroutine; handled (name отвергнут sync 400) [INV-8 no-leak]",
    classes=["SEC", "VAL", "NEG"], priority="P0",
    # verifies CS1-S1-11 (INV-8 leak-guard, behaviour-level)
    steps=[Step(name="cr-sqli", method="POST", path=VOL,
                body=_vol_body("sec", name="vol'; DROP TABLE volumes;--"),
                test_script=[
                    "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                    # Заголовок кейса заявляет СИНХРОННЫЙ отказ — и утверждение теперь
                    # требует ровно его. Прежде здесь стоял oneOf([200, 400, 413]): рядом
                    # с отказом принимался и УСПЕХ, то есть ровно тот исход, ради ловли
                    # которого кейс написан. И это не «терпимость к стенду» — 200 здесь
                    # недостижим by construction: имя проверяется формой на кромке
                    # запроса (self-validating VolumeName, тот же charset, что DB-CHECK),
                    # полезная нагрузка внедрения ему не соответствует. Пара к этому
                    # утверждению зафиксирована в
                    # services/storage/internal/domain/domain_test.go
                    # (TestVolumeNameValidate), поэтому требование не догадка.
                    "pm.test('name отвергнут синхронно: 400', () => pm.expect(pm.response.code, pm.response.text()).to.eql(400));",
                    "let j; try { j = pm.response.json(); } catch(e) { j = {}; }",
                    "if (pm.response.code === 400) {",
                    "  pm.test('grpc code 3 (INVALID_ARGUMENT)', () => pm.expect(j.code).to.eql(3));",
                    "  pm.test('контрактный текст: Illegal argument name', () => pm.expect(j.message || '').to.include('Illegal argument name'));",
                    "}",
                    "const body = JSON.stringify(j).toLowerCase();",
                    "pm.test('no pgx/sqlstate/panic/goroutine leak', () => { pm.expect(body).to.not.include('sqlstate'); pm.expect(body).to.not.include('panic'); pm.expect(body).to.not.include('goroutine'); pm.expect(body).to.not.include('pgx'); });",
                ])],
))

CASES.append(Case(
    id="VOL-LST-SEC-FILTER-SQLI",
    title="Security: SQL-injection в filter (List) -> 200 и ПУСТАЯ страница; нет утечки pgx/SQLSTATE [INV-8]",
    classes=["SEC", "VAL", "NEG"], priority="P0",
    # verifies CS1-S1-03 (INV-8 leak-guard на filter-пути)
    #
    # Прежде здесь стояло `oneOf([200, 400])` под заголовком «handled». Исход при
    # этом УСТАНОВЛЕН: `name="a\' OR 1=1--"` разбирается штатно (`pkg/filter`.`Parse`
    # — поле `name` в белом списке use-case\'а `volume.List`, значение в кавычках,
    # хвоста нет), значение уезжает ПАРАМЕТРОМ запроса, и страница приходит пустой,
    # потому что тома с таким именем нет. `400` производится только негодным
    # СИНТАКСИСОМ выражения, которого эта нагрузка не содержит, — то есть прежняя
    # запись перечисляла исход, которого на этом входе не бывает, и одновременно
    # приняла бы регрессию разбора фильтра.
    #
    # Пустота проверяется по составу ответа: у публичной полосы края
    # `EmitUnpopulated=true`, поэтому пустой список приходит как `[]`, и у
    # списочного ответа ровно один массив верхнего уровня.
    steps=[Step(name="lst-filter-sqli", method="GET",
                path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                test_script=[
                    "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                    "pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                    "let j; try { j = pm.response.json(); } catch(e) { j = {}; }",
                    "pm.test('страница пуста: тома с таким именем нет', () => {",
                    "  const keys = Object.keys(j).filter(k => Array.isArray(j[k]));",
                    "  pm.expect(keys, JSON.stringify(j)).to.have.lengthOf(1);",
                    "  pm.expect(j[keys[0]], JSON.stringify(j)).to.have.lengthOf(0);",
                    "});",
                    "const body = JSON.stringify(j).toLowerCase();",
                    "pm.test('no pgx/sqlstate/panic leak', () => { pm.expect(body).to.not.include('sqlstate'); pm.expect(body).to.not.include('pgx'); pm.expect(body).to.not.include('panic'); });",
                ])],
))

# ---------------------------------------------------------------------------
# CS1-S1-04 (add) — Update: BVA description / labels НА ПУТИ ОБНОВЛЕНИЯ.
#
#   Create прогонял оба поля через validate.* на кромке запроса, Update — нет:
#   он валидировал только имя. Переразмерное описание доезжало до UPDATE,
#   ловилось volumes_description_check и возвращалось АСИНХРОННО в ошибке
#   операции обобщённым "Illegal argument" — то есть 200 на PATCH, а отказ
#   позже и без имени поля.
#
#   Наблюдаемое, которое здесь фиксируется: отказ СИНХРОННЫЙ (400 на самом
#   PATCH, а не 200 + упавшая операция) и имя поля приезжает в ДЕТАЛЯХ
#   (google.rpc.BadRequest.fieldViolations[].field). Текст сообщения намеренно
#   НЕ утверждается — общий валидатор держит его обобщённым по контракту, и
#   утверждение текста залочило бы не ту половину.
#
#   Граница остаётся проходимой: ровно 256 символов и ровно 64 метки — не отказ,
#   и Get показывает применённое значение.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-UPD-BVA-DESC-256-257",
    title="Update description: ровно 256 -> 200 + Operation ok + Get отражает; 257 -> СИНХРОННЫЙ 400 INVALID_ARGUMENT с fieldViolation 'description' (BVA обе стороны границы)",
    classes=["BVA", "VAL", "STATE", "CONF"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("updbvad"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-desc-256", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "description", "description": "x" * 256},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify-256-applied", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('description at the limit was stored', () => pm.expect((pm.response.json().description || '').length).to.eql(256));"]),
        Step(name="patch-desc-257", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "description", "description": "x" * 257},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *assert_field_violation("description")]),
        Step(name="verify-257-not-applied", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('rejected description was not stored', () => pm.expect((pm.response.json().description || '').length).to.eql(256));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-UPD-BVA-LABELS-64-65",
    title="Update labels: ровно 64 -> 200 + Operation ok; 65 -> СИНХРОННЫЙ 400 INVALID_ARGUMENT с fieldViolation 'labels' (BVA обе стороны границы)",
    classes=["BVA", "VAL", "STATE", "CONF"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("updbval"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-labels-64", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "labels", "labels": {f"k{i}": f"v{i}" for i in range(64)}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify-64-applied", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('64 labels at the limit were stored', () => pm.expect(Object.keys(pm.response.json().labels || {}).length).to.eql(64));"]),
        Step(name="patch-labels-65", method="PATCH", path=f"{VOL}/{{{{volumeId}}}}",
             body={"updateMask": "labels", "labels": {f"k{i}": f"v{i}" for i in range(65)}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *assert_field_violation("labels")]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


# ---------------------------------------------------------------------------
# ChangeDiskType — ВЫДЕЛЕННЫЙ глагол смены класса диска
#   (POST /storage/v1/volumes/{volumeId}:changeDiskType, v_update)
#
# Почему не поле правки: это ПЕРЕМЕЩЕНИЕ ДАННЫХ. Оно длится (том в MIGRATING),
# может отказать на половине — и тогда данные остаются на исходном классе, — и
# меняет физическое расположение объекта у бэкенда. `disk_type_id` через Update
# отвергается как неизменяемый (VOL-UPD-MASK-IMMUTABLE-DISKTYPE выше), и отказ
# называет глагол; здесь проверяется сам глагол.
#
# Предусловия глагола (все — на стороне сервиса, одним стейтментом): том в
# AVAILABLE либо IN_USE; целевой класс ACTIVE и предлагается В ТОЙ ЖЕ зоне
# (зона глаголом не меняется); размер тома укладывается в границы класса.
# ---------------------------------------------------------------------------

# Выбор ЦЕЛЕВОГО класса — из ответа списка, а не из литерала. Каталог посева
# стенда здесь не пинится: он заводится шагом подъёма и его состав — свойство
# стенда, а не контракта. Отбор повторяет предусловия глагола настолько, насколько
# они видны публично: обращение ACTIVE, зона тома предлагается классом (пустой
# список зон = «во всех»), размер тома внутри объявленных границ.
#
# int64 приезжает СТРОКОЙ (protojson), поэтому границы приводятся Number() — иначе
# сравнение шло бы лексикографически и '2' оказалось бы больше '10737418240'.
_PICK_ALT_DISK_TYPE = [
    # Имя определяется ДО отбора и всегда: неопределённое `{{altDiskTypeId}}` уехало
    # бы в тело ЛИТЕРАЛОМ (страж неразрешённого адреса читает URL, а не тело), и
    # отказ пришёл бы формой имени класса вместо «второго класса в каталоге нет».
    "pm.environment.set('altDiskTypeId', '');",
    "const j = pm.response.json();",
    "const zone = String(pm.environment.get('existingZoneId'));",
    "const cur = String(pm.environment.get('existingDiskTypeId'));",
    "const size = " + str(_DEF_SIZE) + ";",
    "const fits = t => {",
    "  const lim = t.limits || {};",
    "  const min = Number(lim.minSizeBytes || 0), max = Number(lim.maxSizeBytes || 0);",
    "  const step = Number(lim.sizeStepBytes || 0);",
    "  if (min > 0 && size < min) return false;",
    "  if (max > 0 && size > max) return false;",
    "  if (step > 0 && size % step !== 0) return false;",
    "  return true;",
    "};",
    "const alt = (j.diskTypes || []).filter(t => String(t.id) !== cur",
    "  && String(t.lifecycle) === 'ACTIVE'",
    "  && ((t.zoneIds || []).length === 0 || (t.zoneIds || []).indexOf(zone) >= 0)",
    "  && fits(t));",
    "pm.test('в каталоге есть ВТОРОЙ пригодный класс — иначе смену класса не на что проверять "
    "(состав каталога заводит шаг подъёма стенда: make -C deploy seed-storage)', () => {",
    "  pm.expect(alt.length, 'подходящих классов помимо ' + cur + ' среди ' + "
    "((j.diskTypes || []).length) + ' в каталоге').to.be.at.least(1);",
    "});",
    "if (alt.length) { pm.environment.set('altDiskTypeId', String(alt[0].id)); }",
]

CASES.append(Case(
    id="VOL-CDT-CRUD-OK",
    title="ChangeDiskType: готовый том → другой пригодный класс той же зоны → Operation ok; Get отдаёт НОВЫЙ diskTypeId, зона не изменилась",
    classes=["CRUD", "STATE", "CONF"], priority="P1",
    # verifies CS1-S1-04
    steps=[
        Step(name="list-disk-types", method="GET", path=f"{DT}?pageSize=1000",
             test_script=[*assert_status(200), *_PICK_ALT_DISK_TYPE]),
        Step(name="cr", method="POST", path=VOL, body=_vol_body("cdt"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        # Глагол принимает ТОЛЬКО готовый том (AVAILABLE/IN_USE): без ожидания
        # получили бы FAILED_PRECONDITION по состоянию, а кейс читался бы как
        # «смена класса сломана».
        wait_until_ready(Step(name="await-ready", method="GET", path=f"{VOL}/{{{{volumeId}}}}"),
                         ready="AVAILABLE", subject="Volume"),
        Step(name="change-disk-type", method="POST",
             path=f"{VOL}/{{{{volumeId}}}}:changeDiskType",
             body={"diskTypeId": "{{altDiskTypeId}}"},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.test('metadata.volumeId — тот же том', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.volumeId).to.eql(pm.environment.get('volumeId')));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('diskTypeId стал целевым', () => pm.expect(String(j.diskTypeId)).to.eql(String(pm.environment.get('altDiskTypeId'))));",
                          "pm.test('zoneId глаголом не меняется', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                          "pm.test('sizeBytes не тронут', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_DEF_SIZE) + "'));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-CDT-NEG-MALFORMED-ID",
    title="ChangeDiskType по malformed volumeId → sync 400 INVALID_ARGUMENT 'invalid resource id ...' (первым стейтментом, парити с Get)",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies CS1-S1-02
    steps=[Step(name="cdt-malformed", method="POST", path=f"{VOL}/not-a-vol-id:changeDiskType",
                body={"diskTypeId": "{{existingDiskTypeId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid resource id 'not-a-vol-id'")])],
))

CASES.append(Case(
    id="VOL-CDT-VAL-DISKTYPE-REQUIRED",
    title="ChangeDiskType с пустым diskTypeId → sync 400 INVALID_ARGUMENT 'disk_type_id: required' (пустое — не «оставить как есть», а запрос без предмета)",
    classes=["VAL", "NEG", "CONF"], priority="P0",
    # verifies CS1-S1-12
    #
    # Цель — СВОЙ созданный том, а не well-formed-отсутствующий: у глагола
    # scope_extractor берёт объект из volume_id, и на несуществующем томе отказ
    # пришёл бы authz-полосой, то есть кейс проверял бы не то, что заявляет.
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("cdtreq"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="cdt-empty", method="POST",
             path=f"{VOL}/{{{{volumeId}}}}:changeDiskType", body={"diskTypeId": ""},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *_assert_msg("disk_type_id: required")])),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-CDT-NEG-VOLUME-NOTFOUND",
    title="ChangeDiskType по well-formed-но-нет volumeId → Operation error NOT_FOUND 'Volume <id> not found' (состояние тома проверять не на чем)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S1-07
    steps=[
        Step(name="cdt-nx", method="POST",
             path=f"{VOL}/{{{{garbageStorageId}}}}:changeDiskType",
             body={"diskTypeId": "{{existingDiskTypeId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(5, "NOT_FOUND", msg_regex="Volume vol00000000000000000 not found"),
    ],
))

CASES.append(Case(
    id="VOL-CDT-NEG-DISKTYPE-UNKNOWN",
    title="ChangeDiskType готового тома на несуществующий класс → Operation error FAILED_PRECONDITION 'DiskType block-unicorn not found'; класс тома не изменился",
    classes=["NEG", "CONF", "STATE"], priority="P1",
    # verifies CS1-S1-10
    steps=[
        Step(name="cr", method="POST", path=VOL, body=_vol_body("cdtunk"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        # Готовность нужна, чтобы отказ пришёл ПО КЛАССУ, а не по состоянию тома:
        # сервис разбирает нулевую выборку по порядку и о неготовом томе говорит
        # первым делом про состояние.
        wait_until_ready(Step(name="await-ready", method="GET", path=f"{VOL}/{{{{volumeId}}}}"),
                         ready="AVAILABLE", subject="Volume"),
        Step(name="cdt-unknown", method="POST",
             path=f"{VOL}/{{{{volumeId}}}}:changeDiskType", body={"diskTypeId": "block-unicorn"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(9, "FAILED_PRECONDITION", msg_regex="DiskType block-unicorn not found"),
        Step(name="verify-unchanged", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('класс тома не изменился отвергнутым глаголом', () => pm.expect(String(pm.response.json().diskTypeId)).to.eql(String(pm.environment.get('existingDiskTypeId'))));"]),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))
