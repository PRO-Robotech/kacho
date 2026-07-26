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
import os
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402

# Endpoints. Defaults match the port-forwards the newman drivers establish
# (deploy/scripts/newman-{e2e,parallel}.sh); every one is env-overridable so the
# same seeder works behind a different forward map without editing the file.
INTERNAL = os.environ.get("INTERNAL_BASE_URL", "http://localhost:18081")
PUBLIC = os.environ.get("BASE_URL", "http://localhost:18080")
IAM_GRPC = os.environ.get("IAM_INTERNAL_GRPC", "localhost:19091")
# POST target of the OAuth2 client_credentials exchange (the ONE sanctioned
# direct-Hydra hop — security.md §"iam — единый фасад": everything else, issuance
# included, goes through iam).
HYDRA_TOKEN = os.environ.get("HYDRA_TOKEN_URL", "http://localhost:14444/oauth2/token")
# `aud` of the private_key_jwt client_assertion = Hydra's advertised token endpoint
# (self.issuer + /oauth2/token). A STRING, not a dialled URL.
ASSERT_AUD = os.environ.get(
    "HYDRA_ASSERTION_AUDIENCE", "http://localhost:28080/.ory/hydra/public/oauth2/token")
# Requested token audience = api-gateway ExpectedAudience ("https://"+APIDomain).
API_AUD = os.environ.get("API_AUDIENCE", "https://api.kacho.cloud")
# GATEWAY-identity client cert for the iam :9091 grpcurl calls (NOT the mint's
# operator identity — see mint_rs256.ensure_iam_internal_cert).
MTLS_CERT = m.IAM_INTERNAL_MTLS_CERT
MTLS_KEY = m.IAM_INTERNAL_MTLS_KEY
PG_IAM_POD = os.environ.get("PG_IAM_POD", "kacho-umbrella-pg-iam-0")
KACHO_NS = os.environ.get("KACHO_NAMESPACE", "kacho")

ROLE_ADMIN = "rol21232f297a57a5a74"  # md5('admin')[:17] -> FGA admin
ROLE_EDIT = "rolde95b43bceeb4b998"   # md5('edit')[:17]  -> FGA editor
ROLE_VIEW = "rol1bda80f2be4d3658e"   # md5('view')[:17]  -> FGA viewer

RID = str(int(time.time()))[-6:]

# Zones of the seeded geo baseline. `e` is the nlb-DEDICATED zone (the suite that owns
# a v6-carrying default EXTERNAL_PUBLIC pool); it must exist before
# deploy/scripts/seed-nlb-fixtures.sh will seed that pool, and keeping it OUT of zone
# `a` is what lets the vpc case ADR-CR-EXT-V6-FAMILY-FALLTHROUGH keep asserting that
# zone `a` has no v6-capable pool. Mirrors the dev-path setup.sh block 5d ownership table.
ZONE_SUFFIXES = tuple(os.environ.get("SEED_ZONES", "a,b,c,d,e").split(","))
NLB_ZONE = os.environ.get("NLB_ZONE", "ru-central1-e")


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


def _psql(sql):
    """Run a read-only query against the iam DB from inside its own pod."""
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    return subprocess.run(args, capture_output=True, text=True).stdout.strip()


def upsert_user(ext_id, wait_s=40):
    """InternalUserService.UpsertFromIdentity → user id, waited to DURABILITY.

    The returned Operation is NOT pollable from here, and that is CORRECT, not a defect:
    the call arrives on the cluster-internal listener over mTLS, so no end-user principal
    is forwarded and the Operation is stamped `system/bootstrap`. The gateway's ops-proxy
    ownership check then refuses to let ANY tenant principal — our ServiceAccount admin
    included — read a system-owned Operation, because that would be a cross-tenant BOLA
    (gateway/internal/opsproxy, TestOpsProxy_Get_OwnershipCheck_DeniesTenantReadingSystem
    OwnedOp). Polling it anyway just burns the whole budget and then continues blind.

    So wait on what we actually need instead: the `users` row itself. `metadata.userId` is
    PRE-ALLOCATED and comes back even when the async worker later fails, so returning it
    unchecked would hand out a phantom id — the same class as reading an id out of an
    Operation without checking `error` first. The row appearing IS the durability signal
    (and migration 0049 rejects an AccessBinding whose subject User does not exist, which
    is exactly what a phantom id would produce three steps later).
    """
    body = json.dumps({"externalId": ext_id, "email": ext_id, "displayName": ext_id})
    args = ["grpcurl", "-insecure", "-cert", MTLS_CERT, "-key", MTLS_KEY,
            "-d", body, IAM_GRPC, "kacho.cloud.iam.v1.InternalUserService/UpsertFromIdentity"]
    out = subprocess.run(args, capture_output=True, text=True).stdout
    d = json.loads(out or "{}")
    uid = (d.get("metadata") or {}).get("userId", "")
    if not uid:
        return ""
    deadline = time.time() + wait_s
    while time.time() < deadline:
        if _psql(f"SELECT 1 FROM kacho_iam.users WHERE id='{uid}' LIMIT 1;") == "1":
            return uid
        time.sleep(1)
    raise RuntimeError(f"user {ext_id} ({uid}) never became durable in {wait_s}s — "
                       f"the upsert worker failed; the id would be a phantom")


def db_lookup(ext_id):
    """Discover a user's personal account + default project (ids the production
    upsert created; every real auth stays RS256)."""
    sql = (f"SET search_path=kacho_iam,public; "
           f"SELECT a.id||'|'||p.id FROM accounts a "
           f"JOIN users u ON u.id=a.owner_user_id "
           f"JOIN projects p ON p.account_id=a.id AND p.name='default' "
           f"WHERE u.external_id='{ext_id}' LIMIT 1;")
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
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
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
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
boot = m.mint_bootstrap()


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
    args = ["kubectl", "-n", KACHO_NS, "exec", PG_IAM_POD, "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
    uid = next((ln.strip() for ln in out.splitlines() if ln.strip().startswith("usr")), "")
    if not uid:
        return
    for rel in ("system_admin", "system_viewer"):
        seed_fga_cluster(f"user:{uid}", rel)


def _seed_geo_catalog():
    """Seed the admin-curated geo baseline (region ru-central1 + zones a/b/c/d, status
    UP) via the geo Internal admin RPC (:18081, gated system_admin@cluster = jwtBootstrap).

    Greenfield/fresh stands have NO geo baseline: geo goose-migrations create the schema
    but seed no rows, and the compute->geo data-migration Helm-job is a no-op on an empty
    greenfield compute -> every compute/nlb/vpc create fails "Zone/Region not found"
    (peer-validate geo.{Zone,Region}Service.Get, fail-closed) -> all downstream Get/List
    404. Seeded here as a NEWMAN PREREQUISITE (not a migration -- geo Region/Zone is an
    admin-curated catalog, so a fixture-seed via the admin RPC is the right layer, same as
    every other prerequisite). Idempotent: re-create of an existing row -> AlreadyExists,
    tolerated; the confirm-loops pass immediately when already seeded. `existingZoneId`
    etc. in the fixtures dict below already name these ids."""
    _curl("POST", "/geo/v1/internal/regions", boot,
          {"id": "ru-central1", "name": "ru-central1", "status": "UP"}, base=INTERNAL)
    for _ in range(20):  # region durable before zones (zones.region_id FK RESTRICT -> regions.id)
        if _curl("GET", "/geo/v1/regions/ru-central1", boot).get("id") == "ru-central1":
            break
        time.sleep(0.5)
    for z in ZONE_SUFFIXES:
        _curl("POST", "/geo/v1/internal/zones", boot,
              {"id": f"ru-central1-{z}", "regionId": "ru-central1",
               "name": f"ru-central1-{z}", "status": "UP"}, base=INTERNAL)
    for _ in range(30):  # zones durable (peer-validate consumers read them on request-path)
        if len((_curl("GET", "/geo/v1/zones", boot).get("zones") or [])) >= len(ZONE_SUFFIXES):
            break
        time.sleep(0.5)


def _seed_address_pool(zone_id, name, v4, v6=()):
    """Seed a default AddressPool (IPAM source for external VIPs: nlb external LB VIP
    alloc + vpc Address EXTERNAL create). Greenfield stands have no pool -> vpc
    address-EXTERNAL / nlb external creates fail (no default pool for zone). Cluster-level
    admin resource (Internal admin :18081, system_admin@cluster = jwtBootstrap; Create
    returns the pool sync, not an Operation). Idempotent: AlreadyExists / CIDR-overlap on
    re-seed -> reuse the existing named pool. Requires the geo zone seeded first (pool
    .zone_id references the geo catalog). Newman prerequisite, not a migration -- same
    layer as _seed_geo_catalog."""
    r = _curl("POST", "/vpc/v1/addressPools", boot,
              {"name": name, "description": "seed external VIP pool",
               "kind": "EXTERNAL_PUBLIC", "zoneId": zone_id,
               "v4CidrBlocks": list(v4), "v6CidrBlocks": list(v6)}, base=INTERNAL)
    pid = r.get("id", "")
    if not pid:  # AlreadyExists / CIDR-overlap -> reuse the existing named pool
        lst = _curl("GET", "/vpc/v1/addressPools?pageSize=200", boot, base=INTERNAL)
        pid = next((p.get("id", "") for p in (lst.get("pools") or [])
                    if p.get("name") == name), "")
    if pid:  # make it the default IPAM source for its (zone, kind)
        _curl("PATCH", f"/vpc/v1/addressPools/{pid}", boot,
              {"updateMask": "isDefault", "isDefault": True}, base=INTERNAL)
    return pid


def _seed_network(project_id, name):
    """A pre-existing vpc Network in `project_id` (seedNetworkA1Id / seedNetworkB1Id —
    the GET-probe targets of the authz-deny matrix). Best-effort: a failure leaves the
    key empty and prodseed_all drops empty keys rather than blanking a committed one."""
    try:
        return _await(_curl("POST", "/vpc/v1/networks", boot,
                            {"projectId": project_id, "name": name}), boot, "networkId")
    except RuntimeError:
        return ""


def seed() -> dict:
    """Provision the production-mode matrix and return the fixtures dict.

    Every authenticating token in the result is a Hydra-signed RS256 ServiceAccount
    Bearer obtained through the iam facade — MintBootstrapToken (mTLS gRPC, iam :9091)
    for the admin, then SAKeyService.Issue + a private_key_jwt client_assertion
    exchange for each subject. Nothing here is minted by the harness.
    """
    _seed_bootstrap_root_cluster()
    _seed_geo_catalog()
    # v4-only default EXTERNAL_PUBLIC pool for zone `a` (the vpc suite's IPAM source).
    # DELIBERATELY NOT named `kac-nlb-seed-ext-pool`: deploy/scripts/seed-nlb-fixtures.sh
    # declares itself the SOLE author of that name and RECLAIMS (un-defaults, then
    # deletes) any pool carrying it that sits outside the nlb-dedicated zone — so the
    # old name made the two seeders fight over one row. v6 stays empty in zone `a`: the
    # vpc case ADR-CR-EXT-V6-FAMILY-FALLTHROUGH asserts a v6 request there finds no
    # v6-capable pool and fails FailedPrecondition.
    _seed_address_pool("ru-central1-a", "kac-seed-ext-pool-a", ["198.51.100.0/24"])

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
    # kacho-iam#276 — the NEVER-granted leak-guard subject. `userNOBId` is used DOUBLY
    # (grant TARGET in the access-binding suites, leak-guard VICTIM in the see-nothing
    # probes), so the guards read this dedicated user/SA instead. Seeded as a real user
    # (the id is asserted) whose token slot is the no-grant SA below.
    usr_pure_nob = upsert_user(f"prodseed-pure-nob-{RID}@example.com")
    projA2 = _await(_curl("POST", "/iam/v1/projects", boot,
                          {"accountId": acctA, "name": f"prodseed-a2-{RID}"}), boot, "projectId")

    P = "iam.project"
    A = "iam.account"

    # ── subjects (all SA-principals) ────────────────────────────────────────────
    sva_editorA, tok_editorA = subject(acctA, f"ps-ed-a-{RID}",
                                       [(ROLE_EDIT, P, projA1), (ROLE_EDIT, P, projA2)])
    # jwtSAA / svaAId is a SEPARATE service account, editor on projA1 ONLY.
    # Reusing the A1+A2 editor above would silently OVER-GRANT it: the iam suite
    # documents this slot as "service-account-A (vpc-editor on project-A1)" and asserts
    # AUTHZ-SA-NET-LS-A2-DENY — that the same SA is DENIED listing in the cross project.
    # With an A2 grant that DENY turns 200 and the case stops testing anything.
    # svaAId must stay the id OF this token's principal: rbac-subject-channel-equivalence
    # binds roles to `svaAId` and then reads as `jwtSAA`.
    sva_saA, tok_saA = subject(acctA, f"ps-sa-a-{RID}", [(ROLE_EDIT, P, projA1)])
    sva_nogrant, tok_nogrant = subject(acctA, f"ps-nogrant-{RID}")
    # A SECOND, distinct never-granted SA for the leak-guards: sharing one no-grant token
    # between "subject under test" and "leak-guard victim" is exactly the doubly-used
    # subject #276 removed on the dev path.
    sva_purenob, tok_purenob = subject(acctA, f"ps-purenob-{RID}")
    _, tok_viewerA = subject(acctA, f"ps-view-a-{RID}", [(ROLE_VIEW, P, projA1)])
    _, tok_adminA = subject(acctA, f"ps-adm-a-{RID}", [(ROLE_ADMIN, A, acctA)])
    _, tok_adminB = subject(acctB, f"ps-adm-b-{RID}", [(ROLE_ADMIN, A, acctB)])
    _, tok_invitee = subject(acctA, f"ps-inv-{RID}", [(ROLE_ADMIN, A, acctB), (ROLE_EDIT, P, projA1)])
    # editor on cross project A2 ONLY (nlb cross-tenant move tier)
    _, tok_editorCrossA2 = subject(acctA, f"ps-ed-a2-{RID}", [(ROLE_EDIT, P, projA2)])
    _, tok_ownerA = subject(acctA, f"ps-own-a-{RID}", [(ROLE_ADMIN, P, projA1)])

    seed_net_a1 = _seed_network(projA1, f"ps-seed-net-a1-{RID}")
    seed_net_b1 = _seed_network(projB1, f"ps-seed-net-b1-{RID}")

    fixtures = {
        "jwtBootstrap": boot,
        # no-grant slots
        "jwtNoBindings": tok_nogrant,
        "jwtSANoGrant": tok_nogrant,
        "jwtStranger": tok_nogrant,
        # dedicated never-granted leak-guard subject (#276)
        "jwtPureNoBindings": tok_purenob,
        # editor @ A1 (+A2) slots
        "jwtProjectAdminA1": tok_editorA,
        "jwtProjectEditorA": tok_editorA,
        "jwtSAA": tok_saA,
        "jwtServiceAccountEditor": tok_saA,
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
        "svaAId": sva_saA,
        "svaNoGrantId": sva_nogrant,
        "svaPureNoGrantId": sva_purenob,
        # AccessBinding-subject / ownerUserId users (must EXIST — migration 0049).
        "userAAAId": usr_owner_a,
        "userAABId": usr_owner_b,
        "userNOBId": usr_nob,
        "userINVId": usr_inv,
        "userPA1Id": usr_pa1,
        "userPureNoBindingsId": usr_pure_nob,
        # pre-existing GET-probe networks of the authz-deny matrix
        "seedNetworkA1Id": seed_net_a1,
        "seedNetworkB1Id": seed_net_b1,
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
    return fixtures


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--deps", default="", help="comma list: vpc,compute,storage,registry,nlb")
    ap.parse_args()
    print(json.dumps(seed()))
