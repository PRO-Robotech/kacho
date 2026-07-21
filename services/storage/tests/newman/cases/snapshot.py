# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SnapshotService (kacho-storage) — stage S3 CS1-S3-*.

Covered public RPCs: Get, List, Create (from-READY-volume), Update, Delete
(REST /storage/v1/snapshots; async мутации → Operation, poll /operations/{sop…}).

id-prefix Snapshot = `snp`. Snapshot.sizeBytes снят из volumes атомарным
INSERT…SELECT (не из payload) — на момент снапшота. Error-тексты §0.2 assert'ятся
behaviour-level.

Не-black-box (integration-only, НЕ здесь): from-non-READY-volume (CS1-S3-02
первый When) — control-plane финализирует Volume READY мгновенно (§0.1, DoD
caveat), не-READY достижимо только DB-seed → integration-тест. from-MISSING
sourceVolumeId (CS1-S3-02 второй When) — provokable через public API, включён.
"""

CASES = []

VOL = "/storage/v1/volumes"
SNP = "/storage/v1/snapshots"

_VOL_SIZE = 16106127360  # 15 GiB — отличный от volume-suite default чтобы проверить sizeBytes-снимок


def _assert_msg(substr):
    return [f"pm.test('message includes \"{substr}\"', "
            f"() => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('{substr}'));"]


def _pre_volume(suffix="src"):
    """Создать READY-том (source для снапшота); сохраняет sourceVolumeId."""
    return [
        Step(name=f"pre-vol-{suffix}", method="POST", path=VOL,
             body={"projectId": "{{_suiteFolderId}}", "name": f"vol-snapsrc-{suffix}-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _VOL_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "sourceVolumeId")]),
        poll_operation_until_done(),
    ]


def _cleanup_source_volume():
    return [
        Step(name="cleanup-source-vol", method="DELETE", path=f"{VOL}/{{{{sourceVolumeId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


def _snap_body(suffix, **over):
    b = {"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{sourceVolumeId}}",
         "name": f"snap-{suffix}-{{{{runId}}}}"}
    b.update(over)
    return b


# ---------------------------------------------------------------------------
# CS1-S3-01 — Create happy from-READY-volume → Operation → poll READY → Get
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-CRUD-OK",
    title="Create Snapshot из READY-тома → Operation(snp metadata) → poll → Get: snp-prefix, sourceVolumeId, status READY, sizeBytes==vol.sizeBytes, createdAt sec",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies CS1-S3-01
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
        retry_until_authorized(Step(name="get", method="GET", path=f"{SNP}/{{{{snapshotId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & snp prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('snapshotId')); pm.expect(j.id).to.match(/^snp/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteFolderId')));",
                          "pm.test('sourceVolumeId matches', () => pm.expect(j.sourceVolumeId).to.eql(pm.environment.get('sourceVolumeId')));",
                          "pm.test('status READY', () => pm.expect(j.status).to.eql('READY'));",
                          "pm.test('sizeBytes == source volume size (snapshotted)', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_VOL_SIZE) + "'));",
                          *assert_created_at_seconds()])),
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
             body={"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{garbageStorageId}}",
                   "name": "snap-badsrc-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="Volume vol00000000000000000 not found"),
    ],
))

# ---------------------------------------------------------------------------
# CS1-S3-03 — peer-validate projectId + input-validation name (sync)
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-CR-VAL-PROJECT-REQUIRED",
    title="Create Snapshot без projectId → 400 INVALID_ARGUMENT (project_id required)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="cr-np", method="POST", path=SNP,
                body={"sourceVolumeId": "{{garbageStorageId}}", "name": "snap-np-{{runId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="SNP-CR-VAL-SOURCE-REQUIRED",
    title="Create Snapshot без sourceVolumeId → 400 INVALID_ARGUMENT (source_volume_id required)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-03
    steps=[Step(name="cr-ns", method="POST", path=SNP,
                body={"projectId": "{{_suiteFolderId}}", "name": "snap-ns-{{runId}}"},
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
                test_script=["pm.test('status 400 or 412 (FAILED_PRECONDITION)', () => pm.expect(pm.response.code).to.be.oneOf([400, 412]));",
                             *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             *_assert_msg("Project b1gnonexistent999999 not found")])],
))

CASES.append(Case(
    id="SNP-CR-VAL-NAME-UPPERCASE",
    title="Create Snapshot name=Bad_Name (uppercase) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    steps=[Step(name="cr-upper", method="POST", path=SNP,
                body={"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{garbageStorageId}}", "name": "Bad_Name"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="SNP-CR-VAL-NAME-UNICODE",
    title="Create Snapshot name=снимок (кириллица/не-ASCII) → sync 400 INVALID_ARGUMENT 'Illegal argument name'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies CS1-S3-03
    steps=[Step(name="cr-unicode", method="POST", path=SNP,
                body={"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{garbageStorageId}}", "name": "снимок"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

# ---------------------------------------------------------------------------
# CS1-S3-04 — Get malformed + NotFound + List pagination
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SNP-GET-NEG-MALFORMED-ID",
    title="Get malformed snapshotId 'nope' → sync 400 INVALID_ARGUMENT 'invalid snapshot id 'nope''",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies CS1-S3-04
    steps=[Step(name="get-malformed", method="GET", path=f"{SNP}/nope",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid snapshot id 'nope'")])],
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
    steps=[Step(name="list", method="GET", path=f"{SNP}?projectId={{{{_suiteFolderId}}}}",
                test_script=[*assert_status(200),
                             "pm.test('snapshots is array', () => pm.expect(pm.response.json().snapshots || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="SNP-LST-VAL-PROJECT-REQUIRED",
    title="List snapshots без projectId → 400 INVALID_ARGUMENT",
    classes=["VAL", "NEG"], priority="P0",
    # verifies CS1-S3-04
    steps=[Step(name="list-np", method="GET", path=SNP,
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="SNP-LST-PAGE-TOKEN-GARBAGE",
    title="List snapshots с garbage pageToken → 400 INVALID_ARGUMENT",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies CS1-S3-04
    steps=[Step(name="bad-token", method="GET",
                path=f"{SNP}?projectId={{{{_suiteFolderId}}}}&pageSize=10&pageToken=not-a-real-token",
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
    steps=[Step(name="patch-imm-src", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "source_volume_id", "sourceVolumeId": "{{garbageStorageId}}"},
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
        assert_op_error(5, "NOT_FOUND", msg_substr="not found"),
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
    steps=[Step(name="ps-over", method="GET", path=f"{SNP}?projectId={{{{_suiteFolderId}}}}&pageSize=5000",
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
                body={"updateMask": "project_id"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("project_id is immutable after Snapshot.Create")])],
))

CASES.append(Case(
    id="SNP-UPD-MASK-IMMUTABLE-SIZE",
    title="Update mask=size_bytes -> sync 400 INVALID_ARGUMENT 'size_bytes is immutable after Snapshot.Create'",
    classes=["STATE", "VAL", "CONF", "NEG"], priority="P1",
    # verifies CS1-S3-05
    steps=[Step(name="patch-imm-size", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "size_bytes"},
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
                body={"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{garbageStorageId}}",
                      "name": "n" + "abcdefghij" * 6 + "abc"},  # 1+60+3 = 64
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))
