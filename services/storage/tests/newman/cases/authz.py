# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set public-authz (INV-10) для kacho-storage — CS1-S1-13/14, CS1-S3-07/08.

Публичные VolumeService/SnapshotService (:9090) — НЕ «read = всем можно». Каждый
public-RPC проходит per-RPC InternalIAMService.Check с proto-scope_extractor:
  - object-scoped (анти-BOLA): Volume.Get/Update/Delete → {storage_volume, volume_id};
    Snapshot.Get/Update/Delete → {storage_snapshot, snapshot_id}. Caller без
    viewer(read)/editor(мутация) на проект ЦЕЛЕВОГО объекта → PERMISSION_DENIED
    (existence-non-revealing — тот же `permission denied`, есть цель или нет; §0.2).
  - list-scoped + result-filter (listauthz): Volume.List/Snapshot.List → {project,
    project_id}. Caller без viewer на запрошенный projectId → PERMISSION_DENIED;
    при наличии — результат отфильтрован listauthz (нет кросс-проектной утечки).

ИЗМЕРЕНИЕ ВИДИМОСТИ ВНУТРИ ПРОЕКТА (`AUTHZ-{VOL,SNP,IMG}-LST-OVERSHOW-LEAK-GUARD`,
внизу файла) — over-show leak-guard по образцу compute `LF-INST-LST-OVERSHOW-LEAK-GUARD`:
владелец создаёт объект в проекте сюиты, аутентифицированный НИКОГДА-не-гранченый
субъект листит ТОТ ЖЕ проект → объект не должен появиться. Закрывает то, что раньше
здесь было ЯВНО объявлено дырой: до этого набор проверял ТОЛЬКО кросс-ПРОЕКТНОЕ
измерение, поэтому зелёный прогон storage НЕ был доказательством per-object фильтра
(`viewer ∪ v_list` через iam BatchCheck, internal/authzfilter) — его лочили только
Go-тесты use-case-слоя (internal/service/*/list_filter_test.go) и CI-гейт
tools/audit-list-filter.sh.

Что этот сторож доказывает, а что — нет (читать буквально, не шире):
  * ДОКАЗЫВАЕТ: List не отдаёт объект принципалу, у которого на него нет НИ ОДНОГО
    гранта — сквозь ВСЮ цепочку (edge project-scope Check → backend PermissionMap →
    per-object фильтр), fail-closed. Утечка на ЛЮБОМ из этих слоёв роняет кейс.
  * НЕ доказывает изолированно новый per-object фильтр: у никогда-не-гранченого
    субъекта нет и project-tier viewer, поэтому edge (scope_extractor {project,
    project_id}, required_relation viewer) отвечает 403 раньше бэкенда. Кейс это
    ТОЛЕРИРУЕТ (200 либо 403) и в обеих ветках требует отсутствия объекта — ровно
    как compute-образец.
  * Субъект, который прошёл бы project-gate и при этом НЕ имел бы per-object грантов,
    в этой фикстуре не существует by construction: единственные принципалы с доступом
    к проекту сюиты (jwtBootstrap, jwtProjectEditorA) держат project-scoped биндинги,
    а iam-реконсайлер материализует из них per-object verbs на КАЖДЫЙ объект проекта
    (data-integrity.md, flat Contract-A) — они видят объект ЗАКОННО. Взять такого
    «участника проекта» значило бы получить RED на корректном поведении.
  * Субъект выбран ИМЕННО никогда-не-гранченый (jwtPureNoBindings, не jwtNoBindings):
    jwtNoBindings реально гранят iam-сюиты access-binding'ов, и под параллельным
    прогоном account→project containment делает его транзиторно авторизованным →
    сторож падал бы от загрязнения фикстуры, а не от утечки (kacho-iam#276).

Storage-контракт (отличие от compute hide-existence): denied → 403 / code 7 /
`permission denied` (НЕ 404 — §0.2, storage раскрывает PERMISSION_DENIED, но не
существование цели). Assert — behaviour-level (код + фикс. текст).

# requires: authz-fixture стенд (authz enforced, НЕ dev-passthrough) с identity
# `jwtProjectAdminA1` (alice), авторизованной на projectA1Id и НЕ на projectB1Id.
# Идентичности переиспользованы из compute authz-deny suite (тот же shared iam/fga
# seed). DENY-кейсы существенно fixture-минимальны: alice без права на цель +
# existence-non-revealing → 403 независимо от существования цели. ALLOW-NOLEAK
# требует viewer@projectA1 tuple. Гоняется в authz-профиле стенда.
"""

CASES = []

VOL = "/storage/v1/volumes"
SNP = "/storage/v1/snapshots"
IMG = "/storage/v1/images"

_ALICE = "jwtProjectAdminA1"  # authorized on projectA1Id, NOT on projectB1Id


def _deny(case_id):
    """PERMISSION_DENIED: 403 / code 7 / `permission denied` (existence-non-revealing)."""
    return [
        f"pm.test('[{case_id}] DENY: status 403', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.equal(403));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"pm.test('[{case_id}] DENY: grpc code 7 (PERMISSION_DENIED)', () => pm.expect(j && j.code, JSON.stringify(j)).to.equal(7));",
        f"pm.test('[{case_id}] DENY: message contains permission denied', () => pm.expect((j && j.message || '').toLowerCase()).to.contain('permission denied'));",
    ]


# ---------------------------------------------------------------------------
# CS1-S1-13 — Volume.List listauthz: cross-project deny + own-project no-leak
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AUTHZ-VOL-LIST-CROSS-DENY",
    title="[INV-10] alice List volumes projectId=projectB1 (нет viewer) → 403 PERMISSION_DENIED (scope {project,project_id})",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S1-13
    steps=[Step(name="list-cross", method="GET", path=f"{VOL}?projectId={{{{projectB1Id}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-VOL-LIST-CROSS-DENY"))],
))

CASES.append(Case(
    id="AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK",
    # verifies https://github.com/PRO-Robotech/kacho/issues/62 (edit role does not materialize storage verbs — RED until iam fix)
    title="[INV-10] alice List volumes projectId=projectA1 (есть viewer) → not 403; result содержит ТОЛЬКО projectA1 (нет кросс-проектной утечки)",
    classes=["AUTHZ", "SEC", "POS"], priority="P0",
    # verifies CS1-S1-13
    steps=[Step(name="list-own", method="GET", path=f"{VOL}?projectId={{{{projectA1Id}}}}",
                auth=_ALICE,
                test_script=[
                    "pm.test('[AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK] ALLOW: not 403', () => pm.expect(pm.response.code, 'unexpected 403: ' + pm.response.text()).to.not.equal(403));",
                    "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
                    "pm.test('[AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK] not Unauthenticated (16)', () => pm.expect(j && j.code, JSON.stringify(j)).to.not.equal(16));",
                    "if (j && Array.isArray(j.volumes)) { pm.test('[AUTHZ-VOL-LIST-OWN-ALLOW-NOLEAK] no cross-project leak (all projectA1)', () => j.volumes.forEach(v => pm.expect(v.projectId, 'leaked cross-project volume ' + v.id).to.equal(pm.environment.get('projectA1Id')))); }",
                ])],
))

# ---------------------------------------------------------------------------
# CS1-S1-14 — Volume.Get/Update/Delete object-scoped анти-BOLA
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AUTHZ-VOL-GET-CROSS-DENY",
    title="[INV-10] alice Get чужого volume (scope {storage_volume,volume_id}) → 403 PERMISSION_DENIED (анти-BOLA, existence-non-revealing)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S1-14
    steps=[Step(name="get-cross", method="GET", path=f"{VOL}/{{{{garbageStorageId}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-VOL-GET-CROSS-DENY"))],
))

CASES.append(Case(
    id="AUTHZ-VOL-UPDATE-CROSS-DENY",
    title="[INV-10] alice Update чужого volume → 403 PERMISSION_DENIED (editor-tier анти-BOLA; мутация не доходит до Operation)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S1-14
    steps=[Step(name="upd-cross", method="PATCH", path=f"{VOL}/{{{{garbageStorageId}}}}",
                body={"updateMask": "description", "description": "bola-attempt"},
                auth=_ALICE, test_script=_deny("AUTHZ-VOL-UPDATE-CROSS-DENY"))],
))

CASES.append(Case(
    id="AUTHZ-VOL-DELETE-CROSS-DENY",
    title="[INV-10] alice Delete чужого volume → 403 PERMISSION_DENIED (editor-tier анти-BOLA)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S1-14
    steps=[Step(name="del-cross", method="DELETE", path=f"{VOL}/{{{{garbageStorageId}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-VOL-DELETE-CROSS-DENY"))],
))

# ---------------------------------------------------------------------------
# CS1-S3-07 — Snapshot.List listauthz cross-project deny
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AUTHZ-SNP-LIST-CROSS-DENY",
    title="[INV-10] alice List snapshots projectId=projectB1 (нет viewer) → 403 PERMISSION_DENIED (scope {project,project_id})",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S3-07
    steps=[Step(name="snp-list-cross", method="GET", path=f"{SNP}?projectId={{{{projectB1Id}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-SNP-LIST-CROSS-DENY"))],
))

# ---------------------------------------------------------------------------
# CS1-S3-08 — Snapshot.Get/Update/Delete object-scoped анти-BOLA
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="AUTHZ-SNP-GET-CROSS-DENY",
    title="[INV-10] alice Get чужого snapshot (scope {storage_snapshot,snapshot_id}) → 403 PERMISSION_DENIED (анти-BOLA)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S3-08
    steps=[Step(name="snp-get-cross", method="GET", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-SNP-GET-CROSS-DENY"))],
))

CASES.append(Case(
    id="AUTHZ-SNP-UPDATE-CROSS-DENY",
    title="[INV-10] alice Update чужого snapshot → 403 PERMISSION_DENIED (editor-tier анти-BOLA)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S3-08
    steps=[Step(name="snp-upd-cross", method="PATCH", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                body={"updateMask": "description", "description": "bola-attempt"},
                auth=_ALICE, test_script=_deny("AUTHZ-SNP-UPDATE-CROSS-DENY"))],
))

CASES.append(Case(
    id="AUTHZ-SNP-DELETE-CROSS-DENY",
    title="[INV-10] alice Delete чужого snapshot → 403 PERMISSION_DENIED (editor-tier анти-BOLA)",
    classes=["AUTHZ", "SEC", "NEG"], priority="P0",
    # verifies CS1-S3-08
    steps=[Step(name="snp-del-cross", method="DELETE", path=f"{SNP}/{{{{garbageSnapshotId}}}}",
                auth=_ALICE, test_script=_deny("AUTHZ-SNP-DELETE-CROSS-DENY"))],
))

# ---------------------------------------------------------------------------
# Over-show leak-guard — видимость ВНУТРИ проекта (per-object listauthz).
#
# Раньше это измерение было дырой, объявленной в шапке файла: List волюмов/снапшотов/
# образов проходил ТОЛЬКО project-tier Check на edge + сужение по project_id в SQL —
# ни один из них не отвечает на вопрос «можно ли ЭТОМУ вызывающему видеть ЭТИ
# объекты», поэтому страница отдавалась целиком, хотя Get/Update/Delete тех же самых
# ресурсов корректно требовали per-object грант. Фикс (per-object BatchCheck-фильтр
# страницы, internal/authzfilter) был проверен Go-тестами, но чёрным ящиком — нет.
#
# Форма (compute `LF-INST-LST-OVERSHOW-LEAK-GUARD`): владелец создаёт объект в
# проекте сюиты → аутентифицированный НИКОГДА-не-гранченый субъект листит ТОТ ЖЕ
# проект → объекта в ответе быть не должно. Границы того, что сторож доказывает —
# в шапке файла (читать ДО того, как усиливать/ослаблять ассерты).
#
# Дисциплина:
#   * СТРОГО single-shot: никаких retry вокруг листа не-гранченым субъектом. Retry
#     здесь маскировал бы настоящую утечку (testing.md: оборачивать МОЖНО только
#     первый доступ к СВОЕМУ свежему ресурсу).
#   * pageSize=1000 — чтобы «не видно» не оказалось артефактом первой страницы.
#   * seed/cleanup — дефолтным актором (jwtBootstrap), в изолированном проекте сюиты
#     (_suiteProjectId), имена с {{runId}} (идемпотентность повторного прогона).
#   * seed завершается `assert_op_success` (done && response && !error) ПЕРЕД тем как
#     id используется в листе/удалении. Operation несёт pre-allocated id в metadata
#     даже на done+error, поэтому «прочитал id из Create-ответа и пошёл дальше» дал бы
#     ФАНТОМ: лист чужим субъектом «не находит» несуществующий объект и сторож
#     проходит вакуумно. Здесь падение сида — это RED сида, а не тихо-зелёный сторож.
# ---------------------------------------------------------------------------

_NOB = "jwtPureNoBindings"  # authenticated, NEVER granted by any suite (setup.sh)


def _assert_absent(case_id, list_field, id_var, what):
    """Fail-closed + объекта нет в выдаче. Обе ветки (200/403) обязательны:
    403 — edge отверг не-гранченого на project-scope; 200 — дошло до бэкенда и
    страница обязана быть отфильтрована. 5xx/утечка → RED."""
    return [
        f"pm.test('[{case_id}] fail-closed: 200 или 403, не 5xx', "
        "() => pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([200, 403]));",
        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
        f"const ids = ((j && j.{list_field}) || []).map(x => x.id);",
        f"pm.test('[{case_id}] {what} НЕ виден никогда-не-гранченому субъекту "
        f"(per-object listauthz, не только project-scope)', "
        f"() => pm.expect(ids, 'leaked page: ' + JSON.stringify(ids))"
        f".to.not.include(pm.environment.get('{id_var}')));",
    ]


def _seed_volume(name_suffix, id_var):
    """Создать READY-том дефолтным актором; id — только после assert !op.error."""
    return [
        Step(name=f"seed-vol-{name_suffix}", method="POST", path=VOL,
             body={"projectId": "{{_suiteProjectId}}",
                   "name": f"vol-leak-{name_suffix}-{{{{runId}}}}",
                   "zoneId": "{{existingZoneId}}",
                   "diskTypeId": "{{existingDiskTypeId}}",
                   "sizeBytes": 10737418240},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.volumeId", id_var)]),
        poll_operation_until_done(), assert_op_success(),
    ]


def _delete(path_base, id_var, step_name):
    return [
        Step(name=step_name, method="DELETE", path=f"{path_base}/{{{{{id_var}}}}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


CASES.append(Case(
    id="AUTHZ-VOL-LST-OVERSHOW-LEAK-GUARD",
    title="[leak] jwtPureNoBindings листит проект сюиты → только что созданный владельцем Volume НЕ виден (per-object listauthz внутри проекта)",
    classes=["AUTHZ", "SEC", "NEG", "LST"], priority="P0",
    # index: within-project per-object visibility (over-show leak guard)
    steps=[
        *_seed_volume("vol", "leakVolumeId"),
        Step(name="list-as-pure-nob", method="GET",
             path=f"{VOL}?projectId={{{{_suiteProjectId}}}}&pageSize=1000", auth=_NOB,
             test_script=_assert_absent("AUTHZ-VOL-LST-OVERSHOW-LEAK-GUARD",
                                        "volumes", "leakVolumeId", "Volume")),
        *_delete(VOL, "leakVolumeId", "cleanup-vol"),
    ],
))

CASES.append(Case(
    id="AUTHZ-SNP-LST-OVERSHOW-LEAK-GUARD",
    title="[leak] jwtPureNoBindings листит проект сюиты → только что созданный владельцем Snapshot НЕ виден (per-object listauthz внутри проекта)",
    classes=["AUTHZ", "SEC", "NEG", "LST"], priority="P0",
    # index: within-project per-object visibility (over-show leak guard)
    steps=[
        *_seed_volume("snpsrc", "leakSnapSrcVolumeId"),
        Step(name="seed-snapshot", method="POST", path=SNP,
             body={"projectId": "{{_suiteProjectId}}",
                   "sourceVolumeId": "{{leakSnapSrcVolumeId}}",
                   "name": "snap-leak-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.snapshotId", "leakSnapshotId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="list-as-pure-nob", method="GET",
             path=f"{SNP}?projectId={{{{_suiteProjectId}}}}&pageSize=1000", auth=_NOB,
             test_script=_assert_absent("AUTHZ-SNP-LST-OVERSHOW-LEAK-GUARD",
                                        "snapshots", "leakSnapshotId", "Snapshot")),
        *_delete(SNP, "leakSnapshotId", "cleanup-snapshot"),
        *_delete(VOL, "leakSnapSrcVolumeId", "cleanup-snap-src-vol"),
    ],
))

CASES.append(Case(
    id="AUTHZ-IMG-LST-OVERSHOW-LEAK-GUARD",
    title="[leak] jwtPureNoBindings листит проект сюиты → только что созданный владельцем Image НЕ виден (per-object listauthz внутри проекта)",
    classes=["AUTHZ", "SEC", "NEG", "LST"], priority="P0",
    # index: within-project per-object visibility (over-show leak guard)
    steps=[
        *_seed_volume("imgsrc", "leakImgSrcVolumeId"),
        Step(name="seed-image", method="POST", path=IMG,
             body={"projectId": "{{_suiteProjectId}}", "regionId": "{{existingRegionId}}",
                   "name": "img-leak-{{runId}}",
                   "sourceVolumeId": "{{leakImgSrcVolumeId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.imageId", "leakImageId")]),
        poll_operation_until_done(), assert_op_success(),
        Step(name="list-as-pure-nob", method="GET",
             path=f"{IMG}?projectId={{{{_suiteProjectId}}}}&pageSize=1000", auth=_NOB,
             test_script=_assert_absent("AUTHZ-IMG-LST-OVERSHOW-LEAK-GUARD",
                                        "images", "leakImageId", "Image")),
        *_delete(IMG, "leakImageId", "cleanup-image"),
        *_delete(VOL, "leakImgSrcVolumeId", "cleanup-img-src-vol"),
    ],
))
