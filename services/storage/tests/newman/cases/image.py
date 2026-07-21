# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для ImageService (kacho-storage) — redesign STOR-1 (NET-NEW ресурс).

Covered public RPCs: Get, List, Create, Update, Delete, ListOperations
(REST /storage/v1/images; async мутации → Operation, poll /operations/{sop…}).
`InternalImageService.GetInternal` (infra-проекция) — gRPC-only :9091, НЕ здесь
(integration/bufconn; отсутствие на external — gateway-audit, F8/STOR-1-25 comment).

Источник истины — APPROVED `docs/specs/sub-phase-STOR-1-volume-image-acceptance.md`
(F9..F14). Каждый case несёт `# verifies STOR-1-NN`. Error-тексты — часть контракта
(api-conventions §Error-format), assert'ятся behaviour-level (код И точное сообщение).

Ключевые контрактные факты (grounded в services/storage/internal/{domain,service,repo}):
  - id-prefix Image = `img`; op-root storage = `sop`; placement — всегда REGIONAL (anycast).
  - source oneof EXACTLY-ONE (F12): both → sync INVALID_ARGUMENT
    "an image source must be either a snapshot or a volume, not both"; none → sync
    INVALID_ARGUMENT "Image source is required" (domain.Image.Validate, ДО async).
  - unknown source (well-formed) → op-error FAILED_PRECONDITION "Snapshot|Volume <id> not
    found" (same-DB FK 23503); dup name → op-error ALREADY_EXISTS.
  - immutable region_id/source_*/format → sync INVALID_ARGUMENT "<f> is immutable after
    Image.Create" (immutable-switch ДО UpdateMask).
  - Image.Delete образа, засевшего boot-Volume → ПРОХОДИТ; volumes.source_image_id FK
    ON DELETE SET NULL (provenance, STOR-1-28: том цел, lineage очищается).

Техники test-design (по classes-таксономии): ECP/VAL (source-oneof, name/labels),
BVA (name 64 / description 257 / labels 65 / pageSize 1001), STATE (immutable/update),
decision-table (source snapshot XOR volume), error-guessing (malformed/not-found/unicode),
CONF (тон ошибок, placementType/format, createdAt-truncate, lean-projection).

read-your-writes: первый Get/Update/Delete СВОЕГО свежего Image/Volume обёрнут в
`retry_until_authorized` (owner-tuple EC-окно); async-мутации → op-poll с inter-poll
задержкой. Negatives НЕ оборачиваются. Изоляция: {{runId}}-суффикс + cleanup своих
ресурсов; работа внутри pre-allocated {{_suiteFolderId}}.
"""

CASES = []

VOL = "/storage/v1/volumes"
SNP = "/storage/v1/snapshots"
IMG = "/storage/v1/images"

_SRC_SIZE = 10737418240   # 10 GiB — источник образа
_BOOT_SIZE = 21474836480  # 20 GiB — boot-Volume, материализованный из Image


def _assert_msg(substr):
    """Assert точного (case-sensitive) вхождения нормативного текста в message."""
    # substr вставляется в single-quoted JS-строку — экранируем backslash и '
    # (контракт-тексты вида "invalid image id 'x'" несут одинарные кавычки, иначе
    # ломают pm.test → "missing ) after argument list").
    _esc = substr.replace("\\", "\\\\").replace("'", "\\'")
    return [f"pm.test('message includes \"{_esc}\"', "
            f"() => pm.expect((pm.response.json().message || ''), JSON.stringify(pm.response.json())).to.include('{_esc}'));"]


def _img_body(suffix, **over):
    """Тело Image.Create; caller обязан передать источник (sourceVolumeId ЛИБО
    sourceSnapshotId) через over — source oneof exactly-one (F12)."""
    b = {"projectId": "{{_suiteFolderId}}", "regionId": "{{existingRegionId}}",
         "name": f"img-{suffix}-{{{{runId}}}}"}
    b.update(over)
    return b


def _pre_source_volume(suffix):
    """Создать READY-том (источник образа/снапшота); сохраняет sourceVolumeId."""
    return [
        Step(name=f"pre-vol-{suffix}", method="POST", path=VOL,
             body={"projectId": "{{_suiteFolderId}}", "name": f"vol-imgsrc-{suffix}-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _SRC_SIZE},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "sourceVolumeId")]),
        poll_operation_until_done(),
    ]


def _pre_source_snapshot(suffix):
    """Создать READY-том + снапшот из него (источник образа); сохраняет sourceSnapshotId
    (+ sourceVolumeId для cleanup)."""
    return [
        *_pre_source_volume(suffix),
        Step(name=f"pre-snap-{suffix}", method="POST", path=SNP,
             body={"projectId": "{{_suiteFolderId}}", "sourceVolumeId": "{{sourceVolumeId}}",
                   "name": f"snap-imgsrc-{suffix}-{{{{runId}}}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "sourceSnapshotId")]),
        poll_operation_until_done(),
    ]


def _cleanup(path_var, op_saved="opId"):
    """DELETE ресурса по env-var id + poll до done (best-effort teardown)."""
    return [
        Step(name=f"cleanup-{path_var}", method="DELETE", path=path_var,
             test_script=[*save_from_response("j.id", op_saved)]),
        poll_operation_until_done(),
    ]


def _cleanup_source_volume():
    return _cleanup(f"{VOL}/{{{{sourceVolumeId}}}}")


def _cleanup_source_snapshot():
    return [
        Step(name="cleanup-source-snap", method="DELETE", path=f"{SNP}/{{{{sourceSnapshotId}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        *_cleanup_source_volume(),
    ]


# ===========================================================================
# F10 / STOR-1-20, F12 / STOR-1-24 — Create happy (source=volume и source=snapshot)
# ===========================================================================

CASES.append(Case(
    id="IMG-CR-CRUD-OK",
    title="Create Image из sourceVolumeId → Operation(img metadata) → poll READY → Get: img-prefix, regionId, placementType REGIONAL, format STANDARD, sizeBytes/minDiskBytes derived, createdAt sec",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies STOR-1-20, STOR-1-24
    steps=[
        *_pre_source_volume("crok"),
        Step(name="create", method="POST", path=IMG,
             body=_img_body("cr", sourceVolumeId="{{sourceVolumeId}}",
                            description="newman CRUD-OK", labels={"suite": "newman"}),
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          "pm.test('metadata.imageId prefix img', () => pm.expect(pm.response.json().metadata && pm.response.json().metadata.imageId).to.match(/^img/));",
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('id matches & img prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('imageId')); pm.expect(j.id).to.match(/^img/); });",
                          "pm.test('projectId matches', () => pm.expect(j.projectId).to.eql(pm.environment.get('_suiteFolderId')));",
                          "pm.test('regionId matches', () => pm.expect(j.regionId).to.eql(pm.environment.get('existingRegionId')));",
                          "pm.test('placementType REGIONAL (anycast const)', () => pm.expect(j.placementType).to.eql('REGIONAL'));",
                          "pm.test('format STANDARD (native single-tier enum)', () => pm.expect(j.format).to.eql('STANDARD'));",
                          "pm.test('status READY (control-plane immediate)', () => pm.expect(j.status).to.eql('READY'));",
                          "pm.test('sourceVolumeId matches', () => pm.expect(j.sourceVolumeId).to.eql(pm.environment.get('sourceVolumeId')));",
                          "pm.test('sizeBytes derived from source (10 GiB)', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_SRC_SIZE) + "'));",
                          "pm.test('minDiskBytes derived from source', () => pm.expect(String(j.minDiskBytes)).to.eql('" + str(_SRC_SIZE) + "'));",
                          *assert_created_at_seconds()])),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-CR-CRUD-FROM-SNAPSHOT-OK",
    title="Create Image из sourceSnapshotId (snapshot-путь source-oneof) → poll READY → Get sourceSnapshotId, status READY",
    classes=["CRUD", "CONF"], priority="P1",
    # verifies STOR-1-24
    steps=[
        *_pre_source_snapshot("crsnap"),
        Step(name="create", method="POST", path=IMG,
             body=_img_body("crsnap", sourceSnapshotId="{{sourceSnapshotId}}"),
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('sourceSnapshotId matches', () => pm.expect(j.sourceSnapshotId).to.eql(pm.environment.get('sourceSnapshotId')));",
                          "pm.test('sourceVolumeId empty (oneof)', () => pm.expect(j.sourceVolumeId || '').to.eql(''));",
                          "pm.test('status READY', () => pm.expect(j.status).to.eql('READY'));"])),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_snapshot(),
    ],
))

# ===========================================================================
# F12 / STOR-1-24 — source oneof exactly-one: both / none / unknown-source
# ===========================================================================

CASES.append(Case(
    id="IMG-CR-VAL-SOURCE-BOTH",
    title="Create Image c обоими sourceSnapshotId+sourceVolumeId → sync 400 INVALID_ARGUMENT 'an image source must be either a snapshot or a volume, not both' (decision-table exactly-one)",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies STOR-1-24
    steps=[Step(name="cr-both", method="POST", path=IMG,
                body=_img_body("both", sourceSnapshotId="{{garbageSnapshotId}}",
                               sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("an image source must be either a snapshot or a volume, not both")])],
))

CASES.append(Case(
    id="IMG-CR-VAL-SOURCE-NONE",
    title="Create Image без источника (blank-upload DEFER) → sync 400 INVALID_ARGUMENT 'Image source is required'",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies STOR-1-24
    steps=[Step(name="cr-none", method="POST", path=IMG, body=_img_body("none"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Image source is required")])],
))

CASES.append(Case(
    id="IMG-CR-NEG-SNAPSHOT-NOTFOUND",
    title="Create Image c несуществующим sourceSnapshotId → Operation error FAILED_PRECONDITION 'Snapshot <id> not found' (same-DB FK 23503)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies STOR-1-24
    steps=[
        Step(name="cr-bad-snap", method="POST", path=IMG,
             body=_img_body("bsnap", sourceSnapshotId="{{garbageSnapshotId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="Snapshot snp00000000000000000 not found"),
    ],
))

CASES.append(Case(
    id="IMG-CR-NEG-VOLUME-NOTFOUND",
    title="Create Image c несуществующим sourceVolumeId → Operation error FAILED_PRECONDITION 'Volume <id> not found' (same-DB FK 23503)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies STOR-1-24
    steps=[
        Step(name="cr-bad-vol", method="POST", path=IMG,
             body=_img_body("bvol", sourceVolumeId="{{garbageStorageId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="Volume vol00000000000000000 not found"),
    ],
))

# ===========================================================================
# F10 / STOR-1-21 — dup name; malformed id; well-formed NotFound
# ===========================================================================

CASES.append(Case(
    id="IMG-CR-NEG-DUP-NAME",
    title="Create duplicate name в том же project → Operation error ALREADY_EXISTS 'image with name <n> already exists in project' (partial UNIQUE(project_id,name))",
    classes=["NEG", "CONC", "CONF"], priority="P1",
    # verifies STOR-1-21
    steps=[
        *_pre_source_volume("dup"),
        Step(name="cr-1", method="POST", path=IMG,
             body=_img_body("dup", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="cr-2-dup", method="POST", path=IMG,
             body=_img_body("dup", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(6, "ALREADY_EXISTS", msg_substr="already exists in project"),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-GET-NEG-MALFORMED-ID",
    title="Get malformed imageId 'not-an-img-id' → sync 400 INVALID_ARGUMENT 'invalid image id ...' (первым стейтментом, до repo)",
    classes=["NEG", "VAL", "CONF"], priority="P0",
    # verifies STOR-1-21
    steps=[Step(name="get-malformed", method="GET", path=f"{IMG}/not-an-img-id",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid image id 'not-an-img-id'")])],
))

CASES.append(Case(
    id="IMG-GET-NEG-NOTFOUND",
    title="Get well-formed-но-нет imageId → 404 NOT_FOUND 'Image <id> not found' (через repo.Get)",
    classes=["NEG", "CONF"], priority="P0",
    # verifies STOR-1-21
    steps=[Step(name="get-nx", method="GET", path=f"{IMG}/{{{{garbageImageId}}}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             *_assert_msg("Image img00000000000000000 not found")])],
))

# ===========================================================================
# F4 / STOR-1-29, F10 — peer/format validation на Create (region/project)
# ===========================================================================

CASES.append(Case(
    id="IMG-CR-VAL-PROJECT-REQUIRED",
    title="Create Image без projectId → 400 INVALID_ARGUMENT (project_id required)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies STOR-1-20
    steps=[Step(name="cr-np", method="POST", path=IMG,
                body={"regionId": "{{existingRegionId}}", "name": "img-np-{{runId}}",
                      "sourceVolumeId": "{{garbageStorageId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-CR-VAL-REGION-REQUIRED",
    title="Create Image без regionId → 400 INVALID_ARGUMENT (region_id required)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies STOR-1-20
    steps=[Step(name="cr-nr", method="POST", path=IMG,
                body={"projectId": "{{_suiteFolderId}}", "name": "img-nr-{{runId}}",
                      "sourceVolumeId": "{{garbageStorageId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-CR-NEG-REGION-UNKNOWN",
    title="Create Image c unknown regionId → sync 400 INVALID_ARGUMENT 'unknown region id '<X>'' (peer geo.RegionService.Get, request-path fail-closed)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies STOR-1-20
    # # requires peer-validation enabled (geo peer reachable)
    steps=[Step(name="cr-bad-region", method="POST", path=IMG,
                body=_img_body("breg", regionId="region-9-none", sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("unknown region id 'region-9-none'")])],
))

CASES.append(Case(
    id="IMG-CR-NEG-PROJECT-NOTFOUND",
    title="Create Image c garbage projectId → reject (peer iam.ProjectService.Get); authz-first толерантность 400/403/404/412 (scope_extractor project короткозамыкает ДО backend)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies STOR-1-29
    # # requires peer-validation enabled (iam peer reachable)
    steps=[Step(name="cr-bad-proj", method="POST", path=IMG,
                body=_img_body("bp", projectId="{{garbageProjectId}}", sourceVolumeId="{{garbageStorageId}}"),
                test_script=[
                    "pm.test('rejected authz-first-tolerant (400/403/404/412, never 200/500)', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 412]));",
                    "pm.test('grpc error body present', () => pm.expect(pm.response.json(), JSON.stringify(pm.response.json())).to.have.property('code'));"])],
))

# ===========================================================================
# F1 / F10 / STOR-1-30 — BVA/ECP name / description / labels (Image)
# ===========================================================================

CASES.append(Case(
    id="IMG-CR-VAL-NAME-UPPERCASE",
    title="Create Image name=Ubuntu_LTS (uppercase) → sync 400 INVALID_ARGUMENT 'Illegal argument name' (lowercase-only)",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies STOR-1-30
    steps=[Step(name="cr-upper", method="POST", path=IMG,
                body=_img_body("up", name="Ubuntu_LTS", sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="IMG-CR-VAL-NAME-UNICODE",
    title="Create Image name=образ (кириллица/не-ASCII) → sync 400 INVALID_ARGUMENT 'Illegal argument name' (error-guessing)",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies STOR-1-30
    steps=[Step(name="cr-unicode", method="POST", path=IMG,
                body=_img_body("uni", name="образ", sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("Illegal argument name")])],
))

CASES.append(Case(
    id="IMG-CR-BVA-NAME-OVER-64",
    title="Create Image c name длиной 64 (граница 1..63 + 1) → sync 400 INVALID_ARGUMENT (BVA верхняя граница name)",
    classes=["BVA", "VAL", "NEG"], priority="P1",
    # verifies STOR-1-30
    steps=[Step(name="cr-name64", method="POST", path=IMG,
                body=_img_body("x", name="n" + "abcdefghij" * 6 + "abc",  # 1+60+3 = 64
                               sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-CR-BVA-DESC-OVER-257",
    title="Create Image c description длиной 257 (≤256 + 1) → sync 400 INVALID_ARGUMENT (BVA верхняя граница description)",
    classes=["BVA", "VAL", "NEG"], priority="P1",
    # verifies STOR-1-30
    steps=[Step(name="cr-desc257", method="POST", path=IMG,
                body=_img_body("d", description="x" * 257, sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-CR-BVA-LABELS-OVER-65",
    title="Create Image c 65 labels (≤64 + 1) → sync 400 INVALID_ARGUMENT (BVA верхняя граница labels)",
    classes=["BVA", "VAL", "NEG"], priority="P1",
    # verifies STOR-1-30
    steps=[Step(name="cr-lbl65", method="POST", path=IMG,
                body=_img_body("l", labels={f"k{i}": f"v{i}" for i in range(65)},
                               sourceVolumeId="{{garbageStorageId}}"),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ===========================================================================
# F10 / STOR-1-22 — Update: mutable name/desc/labels; immutable region/source/format
# ===========================================================================

CASES.append(Case(
    id="IMG-UPD-CRUD-NAME-DESC-LABELS-OK",
    title="Update mask=name,description,labels → Operation ok; Get отражает изменения (STATE)",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies STOR-1-22
    steps=[
        *_pre_source_volume("upd"),
        Step(name="cr", method="POST", path=IMG,
             body=_img_body("upd", sourceVolumeId="{{sourceVolumeId}}",
                            description="init", labels={"orig": "1"}),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path=f"{IMG}/{{{{imageId}}}}",
             body={"updateMask": "name,description,labels", "name": "img-upd2-{{runId}}",
                   "description": "updated-newman", "labels": {"env": "prod"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="verify", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('name updated', () => pm.expect(j.name).to.match(/^img-upd2-/));",
                          "pm.test('description updated', () => pm.expect(j.description).to.eql('updated-newman'));",
                          "pm.test('label env', () => pm.expect((j.labels || {}).env).to.eql('prod'));"]),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-UPD-MASK-IMMUTABLE-REGION",
    title="Update mask=region_id → sync 400 INVALID_ARGUMENT 'region_id is immutable after Image.Create' (immutable-switch ДО UpdateMask)",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies STOR-1-22
    steps=[Step(name="patch-imm-region", method="PATCH", path=f"{IMG}/{{{{garbageImageId}}}}",
                body={"updateMask": "regionId", "regionId": "{{existingRegionId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("region_id is immutable after Image.Create")])],
))

CASES.append(Case(
    id="IMG-UPD-MASK-IMMUTABLE-SOURCE-SNAPSHOT",
    title="Update mask=source_snapshot_id → sync 400 INVALID_ARGUMENT 'source_snapshot_id is immutable after Image.Create'",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies STOR-1-22
    steps=[Step(name="patch-imm-src", method="PATCH", path=f"{IMG}/{{{{garbageImageId}}}}",
                body={"updateMask": "sourceSnapshotId", "sourceSnapshotId": "{{garbageSnapshotId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("source_snapshot_id is immutable after Image.Create")])],
))

CASES.append(Case(
    id="IMG-UPD-MASK-IMMUTABLE-FORMAT",
    title="Update mask=format → sync 400 INVALID_ARGUMENT 'format is immutable after Image.Create' (output-only immutable)",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    # verifies STOR-1-22
    steps=[Step(name="patch-imm-format", method="PATCH", path=f"{IMG}/{{{{garbageImageId}}}}",
                body={"updateMask": "format"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("format is immutable after Image.Create")])],
))

CASES.append(Case(
    id="IMG-UPD-MASK-UNKNOWN-FIELD",
    title="Update mask=nonexistent_field → sync 400 INVALID_ARGUMENT (known-set UpdateMask)",
    classes=["VAL", "STATE"], priority="P1",
    # verifies STOR-1-22
    steps=[Step(name="patch-unk", method="PATCH", path=f"{IMG}/{{{{garbageImageId}}}}",
                body={"updateMask": "nonexistent_field", "description": "x"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# ===========================================================================
# F14 / STOR-1-31/32/33 — List: project-scope, pagination-validate, filter, cursor
# ===========================================================================

CASES.append(Case(
    id="IMG-LST-CRUD-OK",
    title="List images в project → images array (project-scoped, listauthz row-filter)",
    classes=["CRUD"], priority="P1",
    # verifies STOR-1-31
    steps=[Step(name="list", method="GET", path=f"{IMG}?projectId={{{{_suiteFolderId}}}}",
                test_script=[*assert_status(200),
                             "pm.test('images is array', () => pm.expect(pm.response.json().images || []).to.be.an('array'));"])],
))

CASES.append(Case(
    id="IMG-LST-VAL-PROJECT-REQUIRED",
    title="List images без projectId → 400 INVALID_ARGUMENT (project_id required; anti-BOLA scope backstop)",
    classes=["VAL", "NEG"], priority="P0",
    # verifies STOR-1-31
    steps=[Step(name="list-np", method="GET", path=IMG,
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-LST-BVA-PAGESIZE-OVER-MAX",
    title="List pageSize=1001 (> max 1000) → 400 INVALID_ARGUMENT (отвергается, не clamp; format-validate ДО authz-short-circuit)",
    classes=["BVA", "VAL", "PAGE", "NEG"], priority="P1",
    # verifies STOR-1-32
    steps=[Step(name="ps-over", method="GET", path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-LST-PAGE-TOKEN-GARBAGE",
    title="List c garbage pageToken → 400 INVALID_ARGUMENT (opaque token не декодируется; ДО authz-short-circuit)",
    classes=["PAGE", "VAL", "NEG"], priority="P1",
    # verifies STOR-1-32
    steps=[Step(name="bad-token", method="GET",
                path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&pageSize=10&pageToken=!!!not-base64!!!",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="IMG-LST-FILTER-NAME-MATCH",
    title="Create → List filter=name=\"X\" → созданный образ в результате (whitelist name)",
    classes=["FILTER", "CRUD"], priority="P2",
    # verifies STOR-1-33
    steps=[
        *_pre_source_volume("flt"),
        Step(name="cr", method="POST", path=IMG,
             body=_img_body("flt", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-filtered", method="GET",
             path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&pageSize=1000&filter=name%3D%22img-flt-{{{{runId}}}}%22",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().images || []).map(x => x.id);",
                          "pm.test('filtered list contains created', () => pm.expect(ids).to.include(pm.environment.get('imageId')));"])),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-LST-FILTER-NAME-NONE",
    title="List filter=name=\"img-none-...\" (нет совпадений) → 200 пустой images[] (не ошибка)",
    classes=["FILTER", "CRUD"], priority="P2",
    # verifies STOR-1-33
    steps=[Step(name="flt-none", method="GET",
                path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&filter=name%3D%22img-none-{{{{runId}}}}%22",
                test_script=[*assert_status(200),
                             "pm.test('images empty (no match)', () => pm.expect(pm.response.json().images || []).to.be.an('array').that.is.empty);"])],
))

CASES.append(Case(
    id="IMG-LST-PAGE-CURSOR",
    title="Seed 2 images → List pageSize=1 → ≤1 item + непустой nextPageToken → follow → 200 (cursor (created_at,id) ASC)",
    classes=["PAGE", "CRUD"], priority="P2",
    # verifies STOR-1-33
    steps=[
        *_pre_source_volume("cur"),
        Step(name="cr-a", method="POST", path=IMG,
             body=_img_body("cura", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageAId")]),
        poll_operation_until_done(),
        Step(name="cr-b", method="POST", path=IMG,
             body=_img_body("curb", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageBId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="page1", method="GET",
             path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&pageSize=1",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('page1 at most 1 item', () => pm.expect((j.images || []).length).to.be.at.most(1));",
                          "pm.test('nextPageToken present (≥2 images exist)', () => pm.expect(j.nextPageToken || '').to.not.eql(''));",
                          *save_from_response("j.nextPageToken", "imgPageToken")])),
        Step(name="page2", method="GET",
             path=f"{IMG}?projectId={{{{_suiteFolderId}}}}&pageSize=1&pageToken={{{{imgPageToken}}}}",
             test_script=[*assert_status(200),
                          "pm.test('page2 returns array', () => pm.expect(pm.response.json().images || []).to.be.an('array'));"]),
        *_cleanup(f"{IMG}/{{{{imageAId}}}}"),
        *_cleanup(f"{IMG}/{{{{imageBId}}}}"),
        *_cleanup_source_volume(),
    ],
))

# ===========================================================================
# F12 / STOR-1-25 — two-projection: public Image БЕЗ blob-layout (infra) [INV-1]
# ===========================================================================

CASES.append(Case(
    id="IMG-GET-CONF-LEAN-PROJECTION",
    title="Public Image.Get → только tenant-facing поля; НЕТ blob-layout/bucket/engineNamespace/storageNode (two-projection security-инвариант)",
    classes=["CONF", "SEC"], priority="P1",
    # verifies STOR-1-25
    steps=[
        *_pre_source_volume("lean"),
        Step(name="cr", method="POST", path=IMG,
             body=_img_body("lean", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const forbidden = ['blobLayout','bucket','engineNamespace','storageNode','poolId','numericInfraId','blob_layout','engine_namespace','storage_node'];",
                          "pm.test('no infra/blob-layout fields on public projection', () => forbidden.forEach(k => pm.expect(j, 'leaked infra field ' + k).to.not.have.property(k)));",
                          "const body = JSON.stringify(j).toLowerCase();",
                          "pm.test('no blob/bucket/engine-namespace tokens in body', () => { pm.expect(body).to.not.include('bloblayout'); pm.expect(body).to.not.include('enginenamespace'); pm.expect(body).to.not.include('storagenode'); });",
                          "pm.test('public tenant-facing fields present', () => { pm.expect(j).to.have.property('regionId'); pm.expect(j).to.have.property('sizeBytes'); pm.expect(j).to.have.property('minDiskBytes'); pm.expect(j).to.have.property('format'); });"])),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

# ===========================================================================
# F10 — Delete happy + Delete well-formed-nonexistent (op-error)
# ===========================================================================

CASES.append(Case(
    id="IMG-DEL-CRUD-OK",
    title="Delete Image → Operation ok (response Empty); Get → 404 NOT_FOUND",
    classes=["CRUD", "STATE"], priority="P1",
    # verifies STOR-1-20
    steps=[
        *_pre_source_volume("delok"),
        Step(name="cr", method="POST", path=IMG,
             body=_img_body("delok", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del", method="DELETE", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get-404", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          *_assert_msg("not found")]),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-DEL-NEG-NOTFOUND",
    title="Delete well-formed-но-нет imageId → Operation error NOT_FOUND 'Image <id> not found' (0-row DELETE)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies STOR-1-21
    steps=[
        Step(name="del-nx", method="DELETE", path=f"{IMG}/{{{{garbageImageId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(5, "NOT_FOUND", msg_substr="Image img00000000000000000 not found"),
    ],
))

# ===========================================================================
# F9 / STOR-1-18 — boot-Volume материализация из Image (Volume.source_image_id)
# ===========================================================================

CASES.append(Case(
    id="IMG-VOL-CR-SOURCE-IMAGE-OK",
    title="Create boot-Volume c sourceImageId (материализация из Image) → poll → Get: sourceImageId==image, status AVAILABLE (regional-coherent zone∈image.region)",
    classes=["CRUD", "CONF", "STATE"], priority="P1",
    # verifies STOR-1-18
    steps=[
        *_pre_source_volume("bootsrc"),
        Step(name="cr-image", method="POST", path=IMG,
             body=_img_body("boot", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="cr-boot-vol", method="POST", path=VOL,
             body={"projectId": "{{_suiteFolderId}}", "name": "vol-boot-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _BOOT_SIZE, "sourceImageId": "{{imageId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "bootVolumeId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get-boot", method="GET", path=f"{VOL}/{{{{bootVolumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('sourceImageId materialised', () => pm.expect(j.sourceImageId).to.eql(pm.environment.get('imageId')));",
                          "pm.test('status AVAILABLE', () => pm.expect(j.status).to.eql('AVAILABLE'));"])),
        *_cleanup(f"{VOL}/{{{{bootVolumeId}}}}"),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-VOL-CR-SOURCE-XOR",
    title="Create Volume c обоими sourceImageId+sourceSnapshotId → sync 400 INVALID_ARGUMENT 'a volume is seeded from either a snapshot or an image, not both' (mutual-exclusion)",
    classes=["VAL", "NEG", "CONF"], priority="P1",
    # verifies STOR-1-19
    steps=[Step(name="cr-vol-xor", method="POST", path=VOL,
                body={"projectId": "{{_suiteFolderId}}", "name": "vol-xor-{{runId}}",
                      "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                      "sizeBytes": _BOOT_SIZE, "sourceImageId": "{{garbageImageId}}",
                      "sourceSnapshotId": "{{garbageSnapshotId}}"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("a volume is seeded from either a snapshot or an image, not both")])],
))

CASES.append(Case(
    id="IMG-VOL-CR-SOURCE-IMAGE-NOTFOUND",
    title="Create Volume c несуществующим sourceImageId → Operation error FAILED_PRECONDITION 'Image <id> not found' (same-DB FK 23503)",
    classes=["NEG", "CONF"], priority="P1",
    # verifies STOR-1-19
    steps=[
        Step(name="cr-vol-bad-img", method="POST", path=VOL,
             body={"projectId": "{{_suiteFolderId}}", "name": "vol-badimg-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _BOOT_SIZE, "sourceImageId": "{{garbageImageId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="Image img00000000000000000 not found"),
    ],
))

# ===========================================================================
# F9 / STOR-1-28 — Image.Delete → source_image_id SET NULL (том цел, provenance-clear)
# ===========================================================================

CASES.append(Case(
    id="IMG-DEL-SETNULL-VOLUME-INTACT",
    title="Image.Delete образа, засевшего boot-Volume → ПРОХОДИТ; boot-Volume.sourceImageId → '' (SET NULL), том цел (AVAILABLE); Image → 404 (контраст с attach RESTRICT)",
    classes=["STATE", "CONF"], priority="P1",
    # verifies STOR-1-28
    steps=[
        *_pre_source_volume("setnull"),
        Step(name="cr-image", method="POST", path=IMG,
             body=_img_body("setnull", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="cr-boot-vol", method="POST", path=VOL,
             body={"projectId": "{{_suiteFolderId}}", "name": "vol-bootsn-{{runId}}",
                   "zoneId": "{{existingZoneId}}", "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": _BOOT_SIZE, "sourceImageId": "{{imageId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", "bootVolumeId")]),
        poll_operation_until_done(), assert_op_success(),
        retry_until_authorized(Step(name="get-boot-pre", method="GET", path=f"{VOL}/{{{{bootVolumeId}}}}",
             test_script=[*assert_status(200),
                          "pm.test('precond sourceImageId set', () => pm.expect(pm.response.json().sourceImageId).to.eql(pm.environment.get('imageId')));"])),
        retry_until_authorized(Step(name="del-image", method="DELETE", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get-boot-post", method="GET", path=f"{VOL}/{{{{bootVolumeId}}}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('sourceImageId cleared (SET NULL, lineage-clear)', () => pm.expect(j.sourceImageId || '').to.eql(''));",
                          "pm.test('boot-Volume intact (AVAILABLE)', () => pm.expect(j.status).to.eql('AVAILABLE'));",
                          "pm.test('boot-Volume size unchanged', () => pm.expect(String(j.sizeBytes)).to.eql('" + str(_BOOT_SIZE) + "'));"]),
        Step(name="get-image-404", method="GET", path=f"{IMG}/{{{{imageId}}}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          *_assert_msg("not found")]),
        *_cleanup(f"{VOL}/{{{{bootVolumeId}}}}"),
        *_cleanup_source_volume(),
    ],
))

# ===========================================================================
# F10 — ListOperations (per-resource op-log) — happy + malformed-id
# ===========================================================================

CASES.append(Case(
    id="IMG-LOP-CRUD-OK",
    title="ListOperations image → ≥1 op (create), каждый с sop-id",
    classes=["CRUD"], priority="P1",
    # verifies STOR-1-20
    steps=[
        *_pre_source_volume("lop"),
        Step(name="cr", method="POST", path=IMG,
             body=_img_body("lop", sourceVolumeId="{{sourceVolumeId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "imageId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-ops", method="GET", path=f"{IMG}/{{{{imageId}}}}/operations?pageSize=10",
             test_script=[*assert_status(200),
                          "const ops = pm.response.json().operations || [];",
                          "pm.test('at least 1 op', () => pm.expect(ops.length).to.be.at.least(1));",
                          "pm.test('op ids sop-prefixed', () => ops.forEach(o => pm.expect(o.id).to.match(/^sop/)));"])),
        *_cleanup(f"{IMG}/{{{{imageId}}}}"),
        *_cleanup_source_volume(),
    ],
))

CASES.append(Case(
    id="IMG-LOP-NEG-MALFORMED-ID",
    title="ListOperations malformed imageId → sync 400 INVALID_ARGUMENT 'invalid image id ...' (парити с Get)",
    classes=["NEG", "VAL", "CONF"], priority="P1",
    # verifies STOR-1-21
    steps=[Step(name="lop-malformed", method="GET", path=f"{IMG}/not-an-img/operations",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *_assert_msg("invalid image id 'not-an-img'")])],
))
