# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SnapshotService (kacho-storage) — stage S3 CS1-S3-*.

Covered public RPCs: Get, List, Create (from-READY-volume), Copy, Update, Delete,
ListOperations (REST /storage/v1/snapshots; async мутации → Operation, poll
/operations/{sop…}).

id-prefix Snapshot = `snp`. Snapshot.sizeBytes снят из volumes атомарным
INSERT…SELECT (не из payload) — на момент снапшота. Error-тексты §0.2 assert'ятся
behaviour-level.

ЗОНА У СНИМКА СВОЯ. `zoneId` наследуется от тома-источника на Create, output-only
и НЕИЗМЕНЯЕМА. Своя она потому, что ссылка на источник обнуляется при удалении
тома: зона, добираемая через источник, однажды стала бы пустой строкой, и проверка
когерентности выродилась бы в тождественно-истинную ровно в тот день, когда том
удалили.

ИСТОЧНИК ОБЯЗАН БЫТЬ ГОТОВ. Снимок снимается только с готового тома, а готовность
объявляет СВЕРЩИК, увидев объект у плоскости данных, — не `Operation.done`. Поэтому
фикстура источника ЖДЁТ пригодности (`wait_until_ready`) и падает, назвав
состояние: иначе предмет кейса отвергался бы предусловием, а виновником назывался
бы шаг, сделавший ровно то, что положено при неготовом источнике.

Не-black-box (integration-only, НЕ здесь): from-non-READY-volume (CS1-S3-02
первый When) — состояние источника здесь не подделать. from-MISSING
sourceVolumeId (CS1-S3-02 второй When) — provokable через public API, включён.
"""

CASES = []

VOL = "/storage/v1/volumes"
SNP = "/storage/v1/snapshots"

_VOL_SIZE = 16106127360  # 15 GiB — отличный от volume-suite default чтобы проверить sizeBytes-снимок


def _assert_msg(substr):
    # substr вставляется в single-quoted JS-строку — экранируем backslash и '
    # (контракт-тексты вида "invalid resource id 'nope'" несут одинарные кавычки,
    # иначе ломают pm.test → "missing ) after argument list").
    _esc = substr.replace("\\", "\\\\").replace("'", "\\'")
    return [f"pm.test('message includes \"{_esc}\"', "
            f"() => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('{_esc}'));"]


def _pre_volume(suffix="src"):
    """Создать том-источник и ДОЖДАТЬСЯ его пригодности; сохраняет sourceVolumeId.

    Ожидание — не перестраховка: `snapshotInsertCAS` берёт только том в состоянии
    READY, а `Operation.done` о состоянии объекта у плоскости данных не говорит
    ничего. Без ожидания каждый снимок этой суиты отвергался бы предусловием.
    """
    return [
        Step(name=f"pre-vol-{suffix}", method="POST", path=VOL,
             body={"projectId": "{{_suiteProjectId}}", "name": f"vol-snapsrc-{suffix}-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _VOL_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "sourceVolumeId")]),
        poll_operation_until_done(), assert_op_success(),
        wait_until_ready_step(f"pre-vol-{suffix}-ready", f"{VOL}/{{{{sourceVolumeId}}}}",
                              ready="AVAILABLE", subject="Том-источник"),
    ]


def _cleanup_source_volume():
    return [
        Step(name="cleanup-source-vol", method="DELETE", path=f"{VOL}/{{{{sourceVolumeId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


def _snap_body(suffix, **over):
    b = {"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{sourceVolumeId}}",
         "name": f"snap-{suffix}-{{{{runId}}}}"}
    b.update(over)
    return b


# ---------------------------------------------------------------------------
# CS1-S3-01 — Create happy from-READY-volume → Operation → poll READY → Get
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-CRUD-OK",
    title="Create Snapshot из готового тома → Operation(snp metadata) → ДОЖДАТЬСЯ пригодности → Get: snp-prefix, sourceVolumeId, zoneId унаследована от тома, statusReason не заполнена, sizeBytes==vol.sizeBytes, createdAt sec",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S3-01
    #
    # Прежняя редакция требовала READY сразу после `done`. Снимок рождается
    # СОЗДАВАЕМЫМ: строка закоммичена, объекта у бэкенда ещё нет, и готовым его
    # объявляет сверщик. Утверждение «READY немедленно» говорило не о контракте, а
    # о том, успел ли обход.
    steps=[
        *_pre_volume("crok"),
        Step(name="create", method="POST", path=SNP,
             body=_snap_body("cr", description="newman CRUD-OK", labels={"suite": "newman"}),
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.test('metadata.snapshotId prefix snp', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.snapshotId).to.match(/^snp/));",
                          "pm.test('metadata.sourceVolumeId matches', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.sourceVolumeId).to.eql(pm.environment.get('sourceVolumeId')));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(), assert_op_success(),
        wait_until_ready(Step(name="get", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('id matches & snp prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('snapshotId')); pm.expect(j.id).to.match(/^snp/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                          "pm.test('sourceVolumeId matches', () => pm.expect(j.sourceVolumeId).to.eql(pm.environment.get('sourceVolumeId')));",
                          "pm.test('zoneId унаследована от тома-источника', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                          "pm.test('statusReason не заполнена у готового снимка', () => pm.expect(String(j.statusReason)).to.eql('STATUS_REASON_UNSPECIFIED'));",
                          "pm.test('sizeBytes == source volume size (snapshotted)', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_VOL_SIZE) + "'));",
                          *assert_created_at_seconds()]), ready="READY", subject="Snapshot"),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S3-02 — from-MISSING source volume (provokable) → op-error FAILED_PRECONDITION
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-NEG-SOURCE-MISSING",
    title="Create Snapshot из несуществующего sourceVolumeId → Operation error FAILED_PRECONDITION 'Volume <id> not found' (from-READY-CAS 0-row)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S3-02 (from-MISSING branch; from-non-READY — integration-only, §0.1)
    steps=[
        Step(name="cr-bad-src", method="POST", path=SNP,
             body={"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{garbageStorageId}}",
                   "name": "snap-badsrc-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # `msg_substr` приводит обе стороны к нижнему регистру и заглавную имени
        # ресурса не различает; `msg_regex` сверяет текст владельца как есть.
        assert_op_error(9, "FAILED_PRECONDITION", msg_regex="Volume vol00000000000000000 not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S3-03 — peer-validate projectId + input-validation name (sync)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-VAL-PROJECT-REQUIRED",
    title="Create Snapshot без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped; #62 project-scope)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="cr-np", method="POST", path=SNP,
                body={"sourceVolumeId": "{{garbageStorageId}}", "name": "snap-np-{{runId}}"},
                test_script=[*assert_unscoped_rejected()])],
))

CASES.append(Case(
    id="SNP-CR-VAL-SOURCE-REQUIRED",
    title="Create Snapshot без sourceVolumeId → 400 INVALID_ARGUMENT (source_volume_id required)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="cr-ns", method="POST", path=SNP,
                body={"projectId": "{{_suiteProjectId}}", "name": "snap-ns-{{runId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="SNP-CR-NEG-PROJECT-NOTFOUND",
    title="Create Snapshot с garbage projectId → sync FAILED_PRECONDITION 'Project <id> not found' (peer iam, request-path)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    # # requires peer-validation enabled (iam peer reachable)
    steps=[Step(name="cr-bad-proj", method="POST", path=SNP,
                body={"projectId": "{{garbageProjectId}}", "sourceVolumeId": "{{garbageStorageId}}", "name": "snap-bp-{{runId}}"},
                test_script=["pm.test('status 400 (FAILED_PRECONDITION; 412 краем не производится)', () => pm.expect(pm.response.code).to.eql(400));",
                             *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             *_assert_msg("Project b1gnonexistent999999 not found")])],
))

CASES.append(Case(
    id="SNP-CR-VAL-NAME-UPPERCASE",
    title="Create Snapshot name=Bad_Name (uppercase) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    steps=[Step(name="cr-upper", method="POST", path=SNP,
                body={"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{garbageStorageId}}", "name": "Bad_Name"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="SNP-CR-VAL-NAME-UNICODE",
    title="Create Snapshot name=снимок (кириллица/не-ASCII) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    steps=[Step(name="cr-unicode", method="POST", path=SNP,
                body={"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{garbageStorageId}}", "name": "снимок"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-04 — Get malformed + NotFound + List pagination
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-GET-NEG-MALFORMED-ID",
    title="Get malformed snapshotId 'nope' → sync 400 INVALID_ARGUMENT 'invalid resource id 'nope''",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies CS1-S3-04
    steps=[Step(name="get-malformed", method="GET", path=f"{SNP}/nope",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid resource id 'nope'")])],
))

CASES.append(Case(
    id="SNP-GET-NEG-NOTFOUND",
    title="Get well-formed-но-нет snapshotId → 404 NOT_FOUND 'Snapshot <id> not found'",
    classes=["NEG", "CONF"], priority="P0",
    # verifies CS1-S3-04
    steps=[Step(name="get-nx", method="GET", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             *_assert_msg("Snapshot snp00000000000000000 not found")])],
))

CASES.append(Case(
    id="SNP-LST-CRUD-OK",
    title="List snapshots в project → snapshots array (project-scoped)",
    classes=["CRUD"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="list", method="GET", path=f"{SNP}?projectId={{{{_suiteProjectId}}}}",
                test_script=[*assert_status(200),
                             "pm.test('snapshots is array', () => pm.expect(pm.response.json().snapshots || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="SNP-LST-VAL-PROJECT-REQUIRED",
    title="List snapshots без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped; #62 project-scope)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-04
    steps=[Step(name="list-np", method="GET", path=SNP,
                test_script=[*assert_unscoped_rejected()])],
))

CASES.append(Case(
    id="SNP-LST-PAGE-TOKEN-GARBAGE",
    title="List snapshots с garbage pageToken → 400 INVALID_ARGUMENT",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="bad-token", method="GET",
                path=f"{SNP}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=not-a-real-token",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-05 — Update immutable source_volume_id (sync) + mutable name/labels
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-UPD-MASK-IMMUTABLE-SOURCE",
    title="Update mask=source_volume_id → sync 400 INVALID_ARGUMENT 'source_volume_id is immutable after Snapshot.Create'",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies CS1-S3-05
    # The MASK carries the assertion. `sourceVolumeId` is not a field of
    # UpdateSnapshotRequest by design (the origin of a snapshot is fixed at
    # Create), so such a body key would be dropped at the edge before the
    # service saw it — present in the fixture, absent from the contract.
    steps=[Step(name="patch-imm-src", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "sourceVolumeId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("source_volume_id is immutable after Snapshot.Create")])],
))

CASES.append(Case(
    id="SNP-UPD-CRUD-NAME-LABELS-OK",
    title="Update mask=name,labels → применены (Get)",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies CS1-S3-05
    steps=[
        *_pre_volume("upd"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("upd", labels={"orig": "1"}),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "name,labels", "name": "snap-upd2-{{runId}}", "labels": {"env": "prod"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('name updated', () => pm.expect(j.name).to.match(/^snap-upd2-/));",
                          "pm.test('label env', () => pm.expect((j.labels || {}).env).to.eql('prod'));"]),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S3-06 — Delete happy + Delete well-formed-nonexistent → op-error NOT_FOUND
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-DEL-CRUD-OK",
    title="Delete Snapshot → Operation ok (response Empty); Get → 404 NOT_FOUND",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies CS1-S3-06
    steps=[
        *_pre_volume("delok"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("delok"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get-404", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="SNP-DEL-NEG-NOTFOUND",
    title="Delete well-formed-но-нет snapshotId → Operation error NOT_FOUND 'Snapshot <id> not found' (0-row DELETE)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S3-06
    steps=[
        Step(name="del-nx", method="DELETE", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Целиком, а не «not found»: голая подстрока зеленела на сообщении о ЛЮБОМ ресурсе.
        assert_op_error(5, "NOT_FOUND", msg_regex="Snapshot [^ ]+ not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S3-04 (add) — List pageSize BVA parity (validate.PageSize, > max 1000).
#   Существующий SNP-LST-PAGE-TOKEN-GARBAGE есть, но pageSize-over-max отсутствовал
#   (Volume/Image его несут). Техника BVA (верхняя граница page_size).
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-LST-BVA-PAGESIZE-OVER-MAX",
    title="List snapshots pageSize=5000 (> max 1000) -> 400 INVALID_ARGUMENT (validate.PageSize; парити с Volume/Image)",
    classes=["BVA", "VAL", "PAGE", "NEG"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="ps-over", method="GET", path=f"{SNP}?projectId={{{{_suiteProjectId}}}}&pageSize=5000",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-05 (add) — Update mask parity: unknown-field (known-set) + immutable
#   project_id / size_bytes. immutable-switch snapshot {source_volume_id (есть),
#   project_id, size_bytes}; existing покрывает только source_volume_id. UpdateMask
#   known-set отвергает unknown-field конвенц. InvalidArgument. Техника
#   state-transition + ECP (unknown vs immutable vs mutable поле в mask).
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-UPD-MASK-UNKNOWN-FIELD",
    title="Update mask=nonexistent_field -> sync 400 INVALID_ARGUMENT (UpdateMask known-set; парити с Volume)",
    classes=["VAL", "STATE", "NEG"], priority="P1",
    # verifies CS1-S3-05
    steps=[Step(name="patch-unk", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "nonexistent_field", "description": "x"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="SNP-UPD-MASK-IMMUTABLE-PROJECT",
    title="Update mask=project_id -> sync 400 INVALID_ARGUMENT 'project_id is immutable after Snapshot.Create' (immutable-switch до UpdateMask)",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S3-05
    steps=[Step(name="patch-imm-proj", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "projectId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("project_id is immutable after Snapshot.Create")])],
))

CASES.append(Case(
    id="SNP-UPD-MASK-IMMUTABLE-SIZE",
    title="Update mask=size_bytes -> sync 400 INVALID_ARGUMENT 'size_bytes is immutable after Snapshot.Create'",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S3-05
    steps=[Step(name="patch-imm-size", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "sizeBytes"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("size_bytes is immutable after Snapshot.Create")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-03 (add) — name BVA parity (over-max len). Shared validateDisplayName
#   (SnapshotName newtype, RuneCount<=63) -> "Illegal argument name". Sync до
#   source-volume резолва (парити с существующим SNP-CR-VAL-NAME-UPPERCASE).
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-BVA-NAME-OVER-64",
    title="Create Snapshot name длиной 64 (граница 1..63 + 1) -> sync 400 INVALID_ARGUMENT 'Illegal argument name' (BVA; парити с Volume/Image)",
    classes=["BVA", "VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    steps=[Step(name="cr-name64", method="POST", path=SNP,
                body={"projectId": "{{_suiteProjectId}}", "sourceVolumeId": "{{garbageStorageId}}",
                      "name": "n" + "abcdefghij" * 6 + "abc"},  # 1+60+3 = 64
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-05 (add) — Update: BVA description / labels + формат имени НА ПУТИ
#   ОБНОВЛЕНИЯ.
#
#   Снимок был шире тома и образа: те хотя бы прогоняли имя через доменный
#   валидатор, снимок не проверял на обновлении НИ ОДНО из трёх. Переразмерное
#   описание, переполненные метки и незаконное имя доезжали до UPDATE, ловились
#   snapshots_description_check / snapshots_labels_valid / snapshots_name_check и
#   возвращались АСИНХРОННО в ошибке операции обобщённым "Illegal argument" —
#   то есть 200 на PATCH, а отказ позже.
#
#   Для описания и меток утверждаются ДЕТАЛИ (fieldViolations[].field), а не
#   текст: общий валидатор держит сообщение обобщённым по контракту. Для имени
#   наоборот — его контрактный текст сам называет поле ("Illegal argument name").
#
#   Граница остаётся проходимой: ровно 256 символов и ровно 64 метки — не отказ.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-UPD-BVA-DESC-256-257",
    title="Update description снимка: ровно 256 -> 200 + Operation ok + Get отражает; 257 -> СИНХРОННЫЙ 400 INVALID_ARGUMENT с fieldViolation 'description'",
    classes=["BVA", "VAL", "STATE", "CONF"], priority="P1",
    # verifies CS1-S3-05
    steps=[
        *_pre_volume("updbvad"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("updbvad"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-desc-256", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "description", "description": "x" * 256},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify-256-applied", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('description at the limit was stored', () => pm.expect((pm.response.json().description || '').length).to.eql(256));"]),
        Step(name="patch-desc-257", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "description", "description": "x" * 257},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *assert_field_violation("description")]),
        Step(name="verify-257-not-applied", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('rejected description was not stored', () => pm.expect((pm.response.json().description || '').length).to.eql(256));"]),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="SNP-UPD-BVA-LABELS-64-65",
    title="Update labels снимка: ровно 64 -> 200 + Operation ok; 65 -> СИНХРОННЫЙ 400 INVALID_ARGUMENT с fieldViolation 'labels'",
    classes=["BVA", "VAL", "STATE", "CONF"], priority="P1",
    # verifies CS1-S3-05
    steps=[
        *_pre_volume("updbval"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("updbval"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-labels-64", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "labels", "labels": {f"k{i}": f"v{i}" for i in range(64)}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify-64-applied", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('64 labels at the limit were stored', () => pm.expect(Object.keys(pm.response.json().labels || {}).length).to.eql(64));"]),
        Step(name="patch-labels-65", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "labels", "labels": {f"k{i}": f"v{i}" for i in range(65)}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *assert_field_violation("labels")]),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="SNP-UPD-VAL-NAME-UPPERCASE",
    title="Update name снимка = 'Snap_Upper' -> СИНХРОННЫЙ 400 INVALID_ARGUMENT 'Illegal argument name' (доменный валидатор на пути обновления; снимок его не звал вовсе)",
    classes=["VAL", "NEG", "STATE", "CONF"], priority="P1",
    # verifies CS1-S3-05
    steps=[
        *_pre_volume("updname"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("updname"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch-name-upper", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "name", "name": "Snap_Upper"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          *_assert_msg("Illegal argument name")])),
        Step(name="verify-name-unchanged", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('rejected name was not stored', () => pm.expect(pm.response.json().name).to.match(/^snap-updname-/));"]),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))


# ---------------------------------------------------------------------------
# zoneId — НОВОЕ поле снимка: наследуется от тома-источника, output-only,
#   неизменяемое. Правки через маску быть не может: перенос между зонами
#   выражается КОПИЕЙ, а не сменой якоря у существующей строки — иначе уже
#   засеянные ею тома ссылались бы на источник, размещения которого больше нет.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-UPD-MASK-ZONE-REJECTED",
    title="Update mask=zoneId → СИНХРОННЫЙ 400 INVALID_ARGUMENT: якорь размещения снимка правкой не меняется (перенос — только Copy)",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S3-05
    #
    # Текст отказа НЕ пинится намеренно: `zoneId` не входит в набор изменяемых
    # полей, поэтому отказ приходит полосой известного набора маски, а не перечнем
    # неизменяемых. Обе полосы — отказ, и обе сохраняют смысл утверждения; пин на
    # одну из них залочил бы внутреннюю очерёдность проверок вместо контракта.
    steps=[Step(name="patch-zone", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "zoneId"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ---------------------------------------------------------------------------
# Copy — перенос снимка в другую зону НОВОЙ строкой
#   (POST /storage/v1/snapshots/{snapshotId}:copy, editor@project)
#
# Право спрашивается у РОДИТЕЛЯ — `editor@project` из `projectId` ТЕЛА, ровно как
# у Create, и потому `projectId` обязателен, хотя выглядит выводимым из источника:
# именно он — объект вопроса о правах. Гейт «v_get на источник» отвергнут
# осознанно (роль наблюдателя материализует чтение на каждый объект проекта, и
# такой гейт дал бы читателю право неограниченно порождать снимки).
# ---------------------------------------------------------------------------

_COPY_ZONES_DIFFER = [
    "pm.test('предусловие фикстуры: целевая зона ОТЛИЧАЕТСЯ от зоны источника "
    "(иначе перенос нечем отличить от дубликата)', () => {",
    "  pm.expect(String(pm.environment.get('existingZoneAltId')),",
    "    'existingZoneAltId').to.not.eql(String(pm.environment.get('existingZoneId')));",
    "});",
]

CASES.append(Case(
    id="SNP-COPY-CRUD-OK",
    title="Copy готового снимка в другую зону → Operation(новый snp + sourceSnapshotId) → копия в ЦЕЛЕВОЙ зоне со своим id и своим именем; источник не изменился",
    classes=["CRUD", "CONF", "STATE"], priority="P1",
    # verifies CS1-S3-01
    steps=[
        *_pre_volume("copyok"),
        Step(name="create-src", method="POST", path=SNP, body=_snap_body("copysrc"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(), assert_op_success(),
        # Копия читает данные источника: `copySnapshotSQL` берёт только READY-строку.
        wait_until_ready(Step(name="src-ready", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=_COPY_ZONES_DIFFER), ready="READY", subject="Снимок-источник"),
        retry_until_authorized(Step(name="copy", method="POST", path=f"{SNP}/{{{{snapshotId}}}}:copy",
             body={"projectId": "{{_suiteProjectId}}", "targetZoneId": "{{existingZoneAltId}}",
                   "name": "snap-copy-{{runId}}", "description": "newman copy",
                   "labels": {"suite": "newman"}},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "const m = pm.response.json().metadata || {};",
                          "pm.test('metadata.snapshotId — НОВЫЙ снимок, не источник', () => { pm.expect(String(m.snapshotId)).to.match(/^snp/); pm.expect(String(m.snapshotId)).to.not.eql(String(pm.environment.get('snapshotId'))); });",
                          "pm.test('metadata.sourceSnapshotId — источник', () => pm.expect(String(m.sourceSnapshotId)).to.eql(String(pm.environment.get('snapshotId'))));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotCopyId")]),
            budget=30, interval_ms=500, retry_on=(403,)),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get-copy", method="GET", path=f"{SNP}/{{{{snapshotCopyId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('копия лежит в ЦЕЛЕВОЙ зоне', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneAltId')));",
                          "pm.test('копия несёт своё имя, а не имя источника', () => pm.expect(j.name).to.match(/^snap-copy-/));",
                          "pm.test('projectId копии — проект источника', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteProjectId')));",
                          # Происхождение копии не проверял никто, а столбец родителя вставка
                          # писала с самого начала — просто не выходил наружу и не признавался
                          # доменом, отчего глагол не работал ни разу.
                          "pm.test('копия помнит родителя-снимок', () => pm.expect(String(j.sourceSnapshotId)).to.eql(String(pm.environment.get('snapshotId'))));",
                          "pm.test('происхождение ровно одно: том не заполнен', () => pm.expect(String(j.sourceVolumeId || '')).to.eql(''));",
                          "pm.test('sizeBytes унаследован от источника', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_VOL_SIZE) + "'));"])),
        Step(name="verify-source-intact", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('зона источника не изменилась копированием', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                          "pm.test('источник остался READY', () => pm.expect(j.status).to.eql('READY'));"]),
        Step(name="del-copy", method="DELETE", path=f"{SNP}/{{{{snapshotCopyId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="SNP-COPY-VAL-PROJECT-REQUIRED",
    title="Copy без projectId → rejected (400 InvalidArgument ИЛИ 403 authz-first, unscoped): право «создать» спрашивают у родителя, и назвать его обязан вызывающий",
    classes=["VAL", "NEG", "AUTHZ"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="copy-np", method="POST", path=f"{SNP}/{{{{garbageSnapshotId}}}}:copy",
                body={"targetZoneId": "{{existingZoneId}}", "name": "snap-copy-np-{{runId}}"},
                test_script=[*assert_unscoped_rejected()])],
))

CASES.append(Case(
    id="SNP-COPY-VAL-TARGET-ZONE-REQUIRED",
    title="Copy без targetZoneId → sync 400 INVALID_ARGUMENT 'target_zone_id: required' (умолчание «та же зона» превратило бы перенос в дубликатор без предмета)",
    classes=["VAL", "NEG", "CONF"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="copy-nz", method="POST", path=f"{SNP}/{{{{garbageSnapshotId}}}}:copy",
                body={"projectId": "{{_suiteProjectId}}", "name": "snap-copy-nz-{{runId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("target_zone_id: required")])],
))

CASES.append(Case(
    id="SNP-COPY-NEG-TARGET-ZONE-UNKNOWN",
    title="Copy в несуществующую зону → sync 400 FAILED_PRECONDITION 'unknown zone id ...' (существование зоны спрашивается у владельца географии на пути запроса)",
    classes=["NEG", "CONF", "VAL"], priority="P1",
    # verifies CS1-S3-03
    # # requires peer-validation enabled (geo peer reachable)
    steps=[Step(name="copy-badzone", method="POST", path=f"{SNP}/{{{{garbageSnapshotId}}}}:copy",
                body={"projectId": "{{_suiteProjectId}}", "targetZoneId": "region-9-z",
                      "name": "snap-copy-bz-{{runId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             *_assert_msg("unknown zone id 'region-9-z'")])],
))

CASES.append(Case(
    id="SNP-COPY-NEG-MALFORMED-ID",
    title="Copy по malformed snapshotId → sync 400 INVALID_ARGUMENT 'invalid snapshot id ...' (первым стейтментом; конкретный тип — область Copy проектная, край путь не разбирает)",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies CS1-S3-04
    # ТЕКСТ ЗДЕСЬ КОНКРЕТНЕЕ, ЧЕМ У Get, И ЭТО СЛЕДСТВИЕ ПОЛОСЫ, А НЕ РАСХОЖДЕНИЕ.
    # Край разбирает лишь те идентификаторы, которыми САМ ограничивает область. У
    # Get областью служит сам ресурс — край видит его id, находит негодным и
    # отвечает своей РОДОВОЙ формулировкой («тип мне неизвестен»). У Copy областью
    # служит ПРОЕКТ (иначе читатель проекта заводил бы ресурсы), поэтому путь до
    # края не разбирается и на негодный id отвечает ВЛАДЕЛЕЦ — конкретным типом,
    # как и предписывает конвенция «invalid <ресурс> id '<X>'».
    steps=[Step(name="copy-malformed", method="POST", path=f"{SNP}/nope:copy",
                body={"projectId": "{{_suiteProjectId}}", "targetZoneId": "{{existingZoneId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid snapshot id 'nope'")])],
))

CASES.append(Case(
    id="SNP-COPY-NEG-SOURCE-NOTFOUND",
    title="Copy well-formed-но-нет снимка → sync 404 NOT_FOUND 'Snapshot <id> not found' (источник резолвится ДО заведения операции)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="copy-nx", method="POST", path=f"{SNP}/{{{{garbageSnapshotId}}}}:copy",
                body={"projectId": "{{_suiteProjectId}}", "targetZoneId": "{{existingZoneId}}",
                      "name": "snap-copy-nx-{{runId}}"},
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             *_assert_msg("Snapshot snp00000000000000000 not found")])],
))

# ---------------------------------------------------------------------------
# ListOperations — журнал операций снимка
#   (GET /storage/v1/snapshots/{snapshotId}/operations, v_list)
#
# Мутации асинхронны, поэтому «что с моим снимком происходило» отвечается только
# журналом: клиент, потерявший идентификатор операции, иначе не восстановит ни её
# исход, ни причину отказа. У тома и образа журнал был, у снимка — нет.
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-LOP-CRUD-OK",
    title="ListOperations снимка → ≥1 операция (создание), каждая с sop-id; после правки в журнале ≥2",
    classes=["CRUD"], priority="P1",
    # verifies CS1-S3-01
    steps=[
        *_pre_volume("lop"),
        Step(name="cr", method="POST", path=SNP, body=_snap_body("lop"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "snapshotId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="list-ops", method="GET",
             path=f"{SNP}/{{{{snapshotId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const ops = pm.response.json().operations || [];",
                          "pm.test('журнал содержит хотя бы операцию создания', () => pm.expect(ops.length).to.be.at.least(1));",
                          "pm.test('идентификаторы операций с префиксом sop', () => ops.forEach(o => pm.expect(String(o.id)).to.match(/^sop/)));",
                          "pm.environment.set('snapOpsBefore', String(ops.length));"])),
        Step(name="patch", method="PATCH", path=f"{SNP}/{{{{snapshotId}}}}",
             body={"updateMask": "description", "description": "lop-conf"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="list-ops-after", method="GET",
             path=f"{SNP}/{{{{snapshotId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const ops = pm.response.json().operations || [];",
                          "pm.test('правка добавила запись в журнал', () => pm.expect(ops.length).to.be.above(parseInt(pm.environment.get('snapOpsBefore') || '0', 10)));"]),
        Step(name="del-snap", method="DELETE", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="SNP-LOP-NEG-MALFORMED-ID",
    title="ListOperations по malformed snapshotId → sync 400 INVALID_ARGUMENT 'invalid resource id ...' (парити с Get и с томом/образом)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="lop-malformed", method="GET", path=f"{SNP}/nope/operations",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid resource id 'nope'")])],
))

CASES.append(Case(
    id="SNP-LOP-BVA-PAGESIZE-OVER-MAX",
    title="ListOperations снимка pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT (validate.PageSize; парити со списком снимков)",
    classes=["BVA", "VAL", "PAGE", "NEG"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="lop-ps-over", method="GET",
                path=f"{SNP}/{{{{garbageSnapshotId}}}}/operations?pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))
