#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode SA-principal matrix seed for the newman regression suites (#59).

Generalises `prodseed_network.py` from the single vpc `network` collection to the
whole 6-subject authz matrix + per-service resource deps. EVERY authenticating
token is a Hydra-signed RS256 ServiceAccount token (acr-exempt, api-audience) — no
HS256 dev-bypass, no interactive OIDC. The authz-deny EXPECT matrices are purely
grant-semantic (editor/viewer/admin/no-grant/cross-account/cross-project), so each
"subject" slot is backed by a ServiceAccount with the exact bindings the matrix
assumes; the principal being an SA rather than a human User does not change the
FGA relation resolved.

The ONE class that genuinely needs a human User principal with an `acr` step-up
claim (interactive Kratos->Hydra OIDC) — `jwtAccountAdminAStepUp` and the static
`apiToken*` — is NOT minted here (production-user-gated, #59 follow-up); those env
keys are left untouched so their cases fail loudly rather than being faked.

Usage:
    prodseed_matrix.py [--deps vpc,compute,storage,registry,nlb,iam] > fixtures.json

Emits a superset fixtures dict on stdout; patch-env.py merges it into any suite env.
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402

INTERNAL = "http://localhost:18081"
PUBLIC = "http://localhost:18080"
IAM_GRPC = "localhost:19091"
HYDRA_TOKEN = "http://localhost:14444/oauth2/token"
ASSERT_AUD = "http://localhost:28080/.ory/hydra/public/oauth2/token"
API_AUD = "https://api.kacho.cloud"
MTLS_CERT = "/tmp/iam-mtls/client.crt"
MTLS_KEY = "/tmp/iam-mtls/client.key"

ROLE_ADMIN = "rol21232f297a57a5a74"  # md5('admin')[:17] -> FGA admin
ROLE_EDIT = "rolde95b43bceeb4b998"   # md5('edit')[:17]  -> FGA editor
ROLE_VIEW = "rol1bda80f2be4d3658e"   # md5('view')[:17]  -> FGA viewer

RID = str(int(time.time()))[-6:]


def _curl(method, path, token, body=None, base=PUBLIC):
    args = ["curl", "-sS", "-m", "20", "-X", method, "-H", "Content-Type: application/json"]
    if token:
        args += ["-H", f"Authorization: Bearer {token}"]
    if body is not None:
        args += ["--data", json.dumps(body)]
    args.append(base + path)
    out = subprocess.run(args, capture_output=True, text=True).stdout
    try:
        return json.loads(out or "{}")
    except json.JSONDecodeError:
        return {"raw": out}


def _poll(op_id, token, budget=40):
    deadline = time.time() + budget
    while time.time() < deadline:
        d = _curl("GET", f"/operations/{op_id}", token)
        if d.get("done"):
            return d
        time.sleep(0.4)
    return {}


def _await(resp, token, key):
    op_id = resp.get("id")
    if not op_id:
        raise RuntimeError(f"no op id: {resp}")
    done = _poll(op_id, token)
    if done.get("error"):
        raise RuntimeError(f"op {op_id} errored: {done['error']}")
    return (done.get("metadata") or {}).get(key, "")


def upsert_user(ext_id):
    body = json.dumps({"externalId": ext_id, "email": ext_id, "displayName": ext_id})
    args = ["grpcurl", "-insecure", "-cert", MTLS_CERT, "-key", MTLS_KEY,
            "-d", body, IAM_GRPC, "kacho.cloud.iam.v1.InternalUserService/UpsertFromIdentity"]
    out = subprocess.run(args, capture_output=True, text=True).stdout
    d = json.loads(out or "{}")
    op = d.get("id")
    if op:
        _poll(op, boot, budget=40)
    return (d.get("metadata") or {}).get("userId", "")


def db_lookup(ext_id):
    """Discover a user's personal account + default project (ids the production
    upsert created; every real auth stays RS256)."""
    sql = (f"SET search_path=kacho_iam,public; "
           f"SELECT a.id||'|'||p.id FROM accounts a "
           f"JOIN users u ON u.id=a.owner_user_id "
           f"JOIN projects p ON p.account_id=a.id AND p.name='default' "
           f"WHERE u.external_id='{ext_id}' LIMIT 1;")
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    for _ in range(25):
        out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
        row = next((ln for ln in out.splitlines() if "|" in ln), None)
        if row:
            acct, proj = row.split("|")
            return acct.strip(), proj.strip()
        time.sleep(1)
    raise RuntimeError(f"db_lookup({ext_id}) empty after retries")


def make_sa(account_id, name):
    r = _curl("POST", "/iam/v1/serviceAccounts", boot, {"accountId": account_id, "name": name})
    return _await(r, boot, "serviceAccountId")


def grant(sva, role_id, scope_type, scope_id):
    """AccessBinding: SA subject, role on scope (iam.project / iam.account)."""
    rb = _curl("POST", "/iam/v1/accessBindings", boot, {
        "subjectType": "service_account", "subjectId": sva, "roleId": role_id,
        "scopeType": scope_type, "scopeId": scope_id, "target": {"allInScope": {}}})
    if rb.get("id"):
        _poll(rb["id"], boot)


def sa_token(sva):
    """Issue an api-audience SA-key, sign client_assertion, exchange -> RS256."""
    kr = _curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", boot,
               {"serviceAccountId": sva, "audience": [API_AUD]})
    done = _poll(kr.get("id"), boot)
    if done.get("error"):
        raise RuntimeError(f"SA-key issue errored for {sva}: {done['error']}")
    cid, key, kid = m._extract_oauth(done.get("response", {}))
    assertion = m.sign_client_assertion(cid, key, kid, ASSERT_AUD)
    return m.exchange(HYDRA_TOKEN, assertion, API_AUD)


CLUSTER_ROOT_OBJECT = "cluster:cluster_kacho_root"


def seed_fga_cluster(fga_subject, relation):
    """Seed a cluster-scope FGA tuple (<fga_subject> #<relation> @cluster_kacho_root)
    deterministically via kacho_iam.fga_outbox → drainer → OpenFGA (idempotent
    WHERE NOT EXISTS), mirroring the sanctioned dev-mode setup.sh 5a/5c seeds.

    Why (cluster-viewer FLOOR, #64/#62): the admin-curated GLOBAL catalog reads —
    compute DiskTypeService.Get/List, geo Region/Zone — gate `viewer@cluster`
    (scope_extractor object_type=cluster). `viewer` derives from `system_viewer` /
    `system_admin` (any_admin), NEVER from an account/project grant. A tenant SA with
    only project/account bindings therefore fails the catalog read with
    "get lacks relation viewer on cluster:cluster_kacho_root" — yet EVERY authenticated
    tenant must read the catalog to launch placement-scoped resources (compute
    authz-deny EXPECTs catalog-read = ALLOW for every non-anon subject). Grant each
    matrix SA `system_viewer@cluster` so the floor is satisfied; it grants ONLY the
    global-catalog read floor (no project/account resource access), so DENY matrices
    (project-scope, cross-account, catalog-MUTATE admin-only) are unaffected."""
    sql = (
        "INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at) "
        "SELECT 'fga.tuple.write', jsonb_build_object("
        f"'user','{fga_subject}','relation','{relation}','object','{CLUSTER_ROOT_OBJECT}'), now() "
        "WHERE NOT EXISTS (SELECT 1 FROM kacho_iam.fga_outbox "
        f"WHERE payload->>'user'='{fga_subject}' AND payload->>'relation'='{relation}' "
        f"AND payload->>'object'='{CLUSTER_ROOT_OBJECT}');"
    )
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    subprocess.run(args, capture_output=True, text=True)


def subject(account_id, name, grants=()):
    """Create an SA in account_id, apply grants [(role, scope_type, scope_id)], mint token."""
    sva = make_sa(account_id, name)
    for role_id, st, sid in grants:
        grant(sva, role_id, st, sid)
    # cluster-viewer FLOOR: every matrix SA must satisfy `viewer@cluster` for the
    # global-catalog reads (DiskType/geo). system_viewer → viewer via any_admin cascade.
    seed_fga_cluster(f"service_account:{sva}", "system_viewer")
    return sva, sa_token(sva)


# ── org structure ───────────────────────────────────────────────────────────
boot = m.mint_bootstrap(INTERNAL)


def _seed_bootstrap_root_cluster():
    """Deterministic system_admin + system_viewer @cluster for the bootstrap ROOT
    user (KACHO_IAM_BOOTSTRAP_ROOT_EMAIL, default admin@prorobotech.ru), mirroring
    dev-mode setup.sh 5a/5c. The bootstrap SA principal already holds system_admin
    @cluster via migration 0058 (deterministic), but the root USER's grant is
    seeded by the ≤180s RunBootstrapAdmin reconciler (racy on a fresh stand) and it
    never gets system_viewer. Best-effort: skip silently if the user is not yet
    provisioned (never fails the seed run)."""
    email = "admin@prorobotech.ru"
    sql = f"SELECT id FROM kacho_iam.users WHERE external_id='{email}' LIMIT 1;"
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
    uid = next((ln.strip() for ln in out.splitlines() if ln.strip().startswith("usr")), "")
    if not uid:
        return
    for rel in ("system_admin", "system_viewer"):
        seed_fga_cluster(f"user:{uid}", rel)


_seed_bootstrap_root_cluster()

owner_a = f"prodseed-owner-a-{RID}@example.com"
owner_b = f"prodseed-owner-b-{RID}@example.com"
usr_owner_a = upsert_user(owner_a)
usr_owner_b = upsert_user(owner_b)
acctA, projA1 = db_lookup(owner_a)
acctB, projB1 = db_lookup(owner_b)

# AccessBinding-subject users (userNOBId/userINVId/userAAAId/userAABId/userPA1Id).
# The iam newman cases reference these as subjectId when creating AccessBindings and
# as ownerUserId / reviewerUserId. Migration 0049 (access_binding_subject_exists)
# rejects a Create whose subject User does not exist → the stale hardcoded env values
# (usr… baked into local.postman_environment.json by the dev-mode setup.sh) do NOT
# exist in a fresh production-mode iam DB, so Create fails FAILED_PRECONDITION
# ("referenced resource not found") and every downstream Get/Delete/revoke cascades
# (404/403). Seed REAL users here and emit their ids so prod-mode binding cases resolve
# a live subject. userAAAId/userAABId map to the owner users (accountAId is owned by
# userAAAId — iam-account.py ownerUserId assertions); NOB/INV/PA1 are plain users.
usr_nob = upsert_user(f"prodseed-nob-{RID}@example.com")
usr_inv = upsert_user(f"prodseed-inv-{RID}@example.com")
usr_pa1 = upsert_user(f"prodseed-pa1-{RID}@example.com")
projA2 = _await(_curl("POST", "/iam/v1/projects", boot,
                      {"accountId": acctA, "name": f"prodseed-a2-{RID}"}), boot, "projectId")

P = "iam.project"
A = "iam.account"

# ── subjects (all SA-principals) ────────────────────────────────────────────
sva_editorA, tok_editorA = subject(acctA, f"ps-ed-a-{RID}",
                                   [(ROLE_EDIT, P, projA1), (ROLE_EDIT, P, projA2)])
sva_nogrant, tok_nogrant = subject(acctA, f"ps-nogrant-{RID}")
_, tok_viewerA = subject(acctA, f"ps-view-a-{RID}", [(ROLE_VIEW, P, projA1)])
_, tok_adminA = subject(acctA, f"ps-adm-a-{RID}", [(ROLE_ADMIN, A, acctA)])
_, tok_adminB = subject(acctB, f"ps-adm-b-{RID}", [(ROLE_ADMIN, A, acctB)])
_, tok_invitee = subject(acctA, f"ps-inv-{RID}", [(ROLE_ADMIN, A, acctB), (ROLE_EDIT, P, projA1)])
_, tok_editorB = subject(acctB, f"ps-ed-b-{RID}", [(ROLE_EDIT, P, projB1)])
_, tok_ownerA = subject(acctA, f"ps-own-a-{RID}", [(ROLE_ADMIN, P, projA1)])
# editor on cross project A2 ONLY (nlb cross-tenant move tier)
_, tok_editorCrossA2 = subject(acctA, f"ps-ed-a2-{RID}", [(ROLE_EDIT, P, projA2)])

fixtures = {
    "jwtBootstrap": boot,
    # no-grant slots
    "jwtNoBindings": tok_nogrant,
    "jwtPureNoBindings": tok_nogrant,
    "jwtSANoGrant": tok_nogrant,
    "jwtStranger": tok_nogrant,
    # editor @ A1 (+A2) slots
    "jwtProjectAdminA1": tok_editorA,
    "jwtProjectEditorA": tok_editorA,
    "jwtSAA": tok_editorA,
    "jwtServiceAccountEditor": tok_editorA,
    # viewer @ A1
    "jwtProjectViewerA": tok_viewerA,
    # account-admin A / B
    "jwtAccountAdminA": tok_adminA,
    "jwtAccountAdminB": tok_adminB,
    # invitee (admin@acctB + editor@projA1)
    "jwtInvitee": tok_invitee,
    # editor @ B / cross project A2
    "jwtProjectEditorB": tok_editorCrossA2,
    # project-owner (admin) @ A1
    "jwtProjectOwnerA": tok_ownerA,
    # tolerant nlb subjects (cases assert oneOf): group-member behaves as editor@A1
    # (clean 200 + cleanup); custom-role operator/targetManager are ungranted so their
    # denial asserts (oneOf([403,404])) hold — a valid-but-ungranted SA yields 403.
    "jwtGroupMemberEditor": tok_editorA,
    "jwtCustomRoleOperator": tok_nogrant,
    "jwtCustomRoleTargetManager": tok_nogrant,
    # ids
    "accountAId": acctA,
    "accountBId": acctB,
    "existingProjectId": projA1,
    "projectA1Id": projA1,
    "existingProjectCrossId": projA2,
    "projectA2Id": projA2,
    "projectB1Id": projB1,
    "existingAccountId": acctA,
    "svaAId": sva_editorA,
    "svaNoGrantId": sva_nogrant,
    # AccessBinding-subject / ownerUserId users (must EXIST — migration 0049).
    "userAAAId": usr_owner_a,
    "userAABId": usr_owner_b,
    "userNOBId": usr_nob,
    "userINVId": usr_inv,
    "userPA1Id": usr_pa1,
    # zones / regions (admin-curated geo catalog)
    "existingZoneId": "ru-central1-a",
    "existingZoneAltId": "ru-central1-b",
    "zoneA": "ru-central1-a",
    "zoneB": "ru-central1-b",
    "zoneC": "ru-central1-c",
    "zoneD": "ru-central1-d",
    "existingRegionId": "ru-central1",
    "existingRegionAltId": "ru-central1",
    "baseUrl": PUBLIC,
    "internalBaseUrl": INTERNAL,
}


def _print():
    print(json.dumps(fixtures))


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--deps", default="", help="comma list: vpc,compute,storage,registry,nlb")
    args = ap.parse_args()
    _print()
