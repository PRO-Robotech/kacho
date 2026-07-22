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
