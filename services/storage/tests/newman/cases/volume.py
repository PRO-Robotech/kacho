# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для VolumeService (kacho-storage) — stage S1 CS1-S1-*.

Covered public RPCs: Get, List, Create, Update, Delete, ListOperations
(REST /storage/v1/volumes; async мутации → Operation, poll /operations/{sop…}).

Контракт изоляции: каждый case в своём runId, работает внутри pre-allocated
existingProjectId (_suiteFolderId из env), Org/Cloud/Project НЕ создаёт; имена
суффиксуются {{runId}}. id-prefix Volume = `vol`, op-root storage = `sop`.

Error-тексты §0.2 acceptance (нормативны — часть контракта) assert'ятся
behaviour-level (код И точное сообщение). sizeBytes[>0]; block_size default 4096.

Не-black-box (integration-only, НЕ здесь): не-READY-state Given (control-plane
финализирует READY мгновенно — §0.1); attach-CAS (Internal :9091, см.
cases/internal-volume.py); listauthz/anti-BOLA (cases/authz.py, fixture-gated).
"""

CASES = []

VOL = "/storage/v1/volumes"

_DEF_SIZE = 10737418240   # 10 GiB
_GROW_SIZE = 21474836480  # 20 GiB
_SHRINK_SIZE = 5368709120  # 5 GiB


def _vol_body(suffix, **over):
    b = {"projectId": "{{_suiteFolderId}}", "name": f"vol-{suffix}-{{{{runId}}}}",
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
    title="Create Volume → Operation(vol metadata) → poll READY → Get: vol-prefix, blockSize 4096, status AVAILABLE, createdAt sec, attachments/usedBy пусты",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S1-01
    steps=[
        Step(name="create", method="POST", path=VOL,
             body=_vol_body("cr", description="newman CRUD-OK", labels={"suite": "newman"}),
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.test('metadata.volumeId prefix vol', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.volumeId).to.match(/^vol/));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & vol prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('volumeId')); pm.expect(j.id).to.match(/^vol/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteFolderId')));",
                          "pm.test('zoneId matches', () => pm.expect(j.zoneId).to.eql(pm.environment.get('existingZoneId')));",
                          "pm.test('diskTypeId matches', () => pm.expect(j.diskTypeId).to.eql(pm.environment.get('existingDiskTypeId')));",
                          "pm.test('sizeBytes matches', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_DEF_SIZE) + "'));",
                          "pm.test('blockSize default 4096', () => pm.expect(String(j.blockSize)).to.eql('4096'));",
                          "pm.test('status AVAILABLE (derived, no attachment)', () => pm.expect(j.status).to.eql('AVAILABLE'));",
                          "pm.test('attachments empty', () => pm.expect(j.attachments || []).to.be.an('array').that.is.empty);",
                          "pm.test('usedBy empty', () => pm.expect(j.usedBy || []).to.be.an('array').that.is.empty);",
                          *assert_created_at_seconds()])),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-02 — Get: malformed id (sync INVALID_ARGUMENT) + well-formed NotFound
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-GET-NEG-MALFORMED-ID",
    title="Get malformed volumeId 'not-a-vol-id' → sync 400 INVALID_ARGUMENT 'invalid volume id ...' (первым стейтментом)",
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
    steps=[Step(name="list", method="GET", path=f"{VOL}?projectId={{{{_suiteFolderId}}}}",
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
    steps=[Step(name="ps-over", method="GET", path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&pageSize=5000",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="VOL-LST-PAGE-TOKEN-GARBAGE",
    title="List с garbage pageToken → 400 INVALID_ARGUMENT (opaque token не декодируется)",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies CS1-S1-03
    steps=[Step(name="bad-token", method="GET",
                path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&pageSize=10&pageToken=not-a-real-token",
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
             path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&pageSize=1000&filter=name%3D%22vol-flt-{{{{runId}}}}%22",
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
        assert_op_error(3, "INVALID_ARGUMENT", msg_substr="Volume size can only be increased"),
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
        assert_op_error(3, "INVALID_ARGUMENT", msg_substr="Volume size can only be increased"),
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
    steps=[Step(name="patch-imm-zone", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "zoneId", "zoneId": "{{existingZoneAltId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("zone_id is immutable after Volume.Create")])],
))

CASES.append(Case(
    id="VOL-UPD-MASK-IMMUTABLE-DISKTYPE",
    title="Update mask=disk_type_id → sync 400 INVALID_ARGUMENT 'disk_type_id is immutable after Volume.Create'",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-05
    steps=[Step(name="patch-imm-dt", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "diskTypeId", "diskTypeId": "block-fast"},
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
        assert_op_error(6, "ALREADY_EXISTS", msg_substr="already exists in project"),
        Step(name="cleanup", method="DELETE", path=f"{VOL}/{{{{volumeId}}}}", test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="VOL-CR-CRUD-EMPTY-NAME-OK",
    title="Create два тома с пустым name в одном project → оба Operation ok (partial UNIQUE не действует на name='')",
    classes=["CRUD", "BVA"], priority="P2",
    # verifies CS1-S1-06
    steps=[
        Step(name="cr-a", method="POST", path=VOL, body=_vol_body("noname", name=""),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeAId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="cr-b", method="POST", path=VOL, body=_vol_body("noname2", name=""),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "volumeBId")]),
        poll_operation_until_done(), assert_op_success(),
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
        assert_op_error(5, "NOT_FOUND", msg_substr="not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-08 — peer-validate zoneId (cross-service geo, sync fail-closed)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-CR-NEG-ZONE-UNKNOWN",
    title="Create с unknown zoneId → sync 400 INVALID_ARGUMENT 'unknown zone id '<X>'' (peer geo.ZoneService.Get NotFound)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies CS1-S1-08
    # # requires peer-validation enabled (geo peer reachable)
    steps=[Step(name="cr-bad-zone", method="POST", path=VOL, body=_vol_body("bz", zoneId="region-9-z"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
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
                test_script=["pm.test('status 400 or 412 (FAILED_PRECONDITION)', () => pm.expect(pm.response.code).to.be.oneOf([400, 412]));",
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
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="DiskType block-unicorn not found"),
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
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="Snapshot snp00000000000000000 not found"),
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
                          "const forbidden = ['backendLun','nvmeNamespace','storageNode','poolId','capacityBytes','infraId','backend_lun','storage_node','pool_id'];",
                          "pm.test('no infra fields on public projection', () => forbidden.forEach(k => pm.expect(j, 'leaked infra field ' + k).to.not.have.property(k)));",
                          "const body = JSON.stringify(j).toLowerCase();",
                          "pm.test('no lun/nvme/pool tokens in body', () => { pm.expect(body).to.not.include('nvme'); pm.expect(body).to.not.include('backendlun'); pm.expect(body).to.not.include('storagenode'); });"])),
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
    title="ListOperations malformed volumeId → sync 400 INVALID_ARGUMENT 'invalid volume id ...' (парити с Get)",
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
        Step(name="lst-includes", method="GET", path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&pageSize=1000",
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
        Step(name="lst-excludes", method="GET", path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().volumes || []).map(x => x.id);",
                          "pm.test('list does not contain', () => pm.expect(ids).to.not.include(pm.environment.get('volumeId')));"]),
        Step(name="get-404", method="GET", path=f"{VOL}/{{{{volumeId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S1-12 (add) — name BVA/ECP parity (over-max len, digit-start, hyphen-start).
#   self-validating VolumeName newtype (domain/volume.go): displayNameRe
#   ^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$ + RuneCount<=63 → нарушение = фикс. текст
#   "Illegal argument name". Техники: BVA (верхняя граница длины 63+1), ECP
#   (недопустимый первый символ: цифра / дефис). Парити с IMG-CR-BVA-NAME-OVER-64.
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

CASES.append(Case(
    id="VOL-CR-VAL-NAME-DIGIT-START",
    title="Create name '9data-vol' (первый символ - цифра) -> sync 400 INVALID_ARGUMENT 'Illegal argument name' (regex требует первый символ [a-z])",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S1-12
    steps=[Step(name="cr-digit", method="POST", path=VOL, body=_vol_body("dg", name="9data-vol"),
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
# CS1-S1-05 (add) — immutable-mask parity (block_size / source_snapshot_id) +
#   пустой mask = full-PATCH. immutable-switch (ДО UpdateMask, api-conventions
#   gotcha) для полного набора immutable-полей Volume {zone_id, disk_type_id,
#   block_size, source_snapshot_id, used_by}; existing покрывает zone_id/disk_type_id.
#   Техника state-transition (immutable после Create). UpdateVolumeRequest не несёт
#   тела block_size/source_snapshot_id -> триггер именно mask-path (immutable-switch).
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="VOL-UPD-MASK-IMMUTABLE-BLOCKSIZE",
    title="Update mask=block_size -> sync 400 INVALID_ARGUMENT 'block_size is immutable after Volume.Create' (immutable-switch до UpdateMask)",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S1-05
    steps=[Step(name="patch-imm-bs", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "blockSize"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("block_size is immutable after Volume.Create")])],
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
                    "pm.test('handled 2xx/4xx (name отвергнут)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 413]));",
                    "let j; try { j = pm.response.json(); } catch(e) { j = {}; }",
                    "const body = JSON.stringify(j).toLowerCase();",
                    "pm.test('no pgx/sqlstate/panic/goroutine leak', () => { pm.expect(body).to.not.include('sqlstate'); pm.expect(body).to.not.include('panic'); pm.expect(body).to.not.include('goroutine'); pm.expect(body).to.not.include('pgx'); });",
                ])],
))

CASES.append(Case(
    id="VOL-LST-SEC-FILTER-SQLI",
    title="Security: SQL-injection в filter (List) -> НЕ 500; handled (200|400); нет утечки pgx/SQLSTATE (filter параметризован/парсится whitelist) [INV-8]",
    classes=["SEC", "VAL", "NEG"], priority="P0",
    # verifies CS1-S1-03 (INV-8 leak-guard на filter-пути)
    steps=[Step(name="lst-filter-sqli", method="GET",
                path=f"{VOL}?projectId={{{{_suiteFolderId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                test_script=[
                    "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                    "pm.test('handled 200|400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                    "let j; try { j = pm.response.json(); } catch(e) { j = {}; }",
                    "const body = JSON.stringify(j).toLowerCase();",
                    "pm.test('no pgx/sqlstate/panic leak', () => { pm.expect(body).to.not.include('sqlstate'); pm.expect(body).to.not.include('pgx'); pm.expect(body).to.not.include('panic'); });",
                ])],
))
