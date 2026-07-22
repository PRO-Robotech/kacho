#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Production-mode SA-principal seed for the vpc `network` collection (#59).

Proves the production-strict path at SUITE level: every authenticating token is a
Hydra-signed RS256 ServiceAccount token (acr-exempt, api-audience whitelisted) —
NO HS256 dev-bypass. The org/geo structure is provisioned through the same
production APIs (bootstrap-admin SA + iam internal upsert + geo internal catalog).

Emits the fixtures dict (jwtBootstrap, jwtProjectAdminA1, project/zone ids) so
patch-env.py can merge it into the vpc newman environment.
"""
from __future__ import annotations

import json
import subprocess
import sys
import time
import urllib.request

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402

INTERNAL = "http://localhost:18081"   # api-gateway internal-rest (bootstrap-mint, geo internal)
PUBLIC = "http://localhost:18080"     # api-gateway public
IAM_GRPC = "localhost:19091"          # iam-internal :9091 (mTLS)
HYDRA_TOKEN = "http://localhost:14444/oauth2/token"
ASSERT_AUD = "http://localhost:28080/.ory/hydra/public/oauth2/token"
API_AUD = "https://api.kacho.cloud"
MTLS_CERT = "/tmp/iam-mtls/client.crt"
MTLS_KEY = "/tmp/iam-mtls/client.key"


def _curl(method, path, token, body=None, base=PUBLIC):
    args = ["curl", "-sS", "-X", method, "-H", "Content-Type: application/json"]
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


def _poll(op_id, token, budget=30):
    deadline = time.time() + budget
    while time.time() < deadline:
        d = _curl("GET", f"/operations/{op_id}", token)
        if d.get("done"):
            return d
        time.sleep(0.4)
    return {}


def _await(resp, token, key):
    """Poll an async Create Operation; assert no error; return metadata[key]."""
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
        _poll(op, boot, budget=30)
    return (d.get("metadata") or {}).get("userId", "")


def db_lookup(ext_id):
    """Discover a user's personal account + default project via the iam DB
    (read-only discovery of ids the production upsert created; every actual auth
    stays production-strict RS256)."""
    sql = (f"SET search_path=kacho_iam,public; "
           f"SELECT a.id||'|'||p.id FROM accounts a "
           f"JOIN users u ON u.id=a.owner_user_id "
           f"JOIN projects p ON p.account_id=a.id AND p.name='default' "
           f"WHERE u.external_id='{ext_id}' LIMIT 1;")
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    # The personal account/project is created in the async upsert worker; retry to
    # absorb the post-upsert lag.
    for _ in range(20):
        out = subprocess.run(args, capture_output=True, text=True).stdout.strip()
        row = next((ln for ln in out.splitlines() if "|" in ln), None)
        if row:
            acct, proj = row.split("|")
            return acct.strip(), proj.strip()
        time.sleep(1)
    raise RuntimeError(f"db_lookup({ext_id}) empty after retries")


def _grant(sva, role_id, scope_type, scope_id):
    rb = _curl("POST", "/iam/v1/accessBindings", boot, {
        "subjectType": "service_account", "subjectId": sva, "roleId": role_id,
        "scopeType": scope_type, "scopeId": scope_id, "target": {"allInScope": {}}})
    if rb.get("id"):
        _poll(rb["id"], boot)


def make_sa_token(account_id, name, role_id=None, scope_type=None, scope_id=None):
    """Create an SA in account_id, optionally grant role on scope, issue an
    api-audience SA-key, and complete the client_credentials exchange → RS256."""
    r = _curl("POST", "/iam/v1/serviceAccounts", boot,
              {"accountId": account_id, "name": name})
    sva = _await(r, boot, "serviceAccountId")
    if role_id:
        rb = _curl("POST", "/iam/v1/accessBindings", boot, {
            "subjectType": "service_account", "subjectId": sva, "roleId": role_id,
            "scopeType": scope_type, "scopeId": scope_id, "target": {"allInScope": {}}})
        _await(rb, boot, "accessBindingId") if rb.get("id") else None
    kr = _curl("POST", f"/iam/v1/serviceAccounts/{sva}/keys", boot,
               {"serviceAccountId": sva, "audience": [API_AUD]})
    done = _poll(kr.get("id"), boot)
    if done.get("error"):
        raise RuntimeError(f"SA-key issue errored for {sva}: {done['error']}")
    resp = done.get("response", {})
    cid, key, kid = m._extract_oauth(resp)
    assertion = m.sign_client_assertion(cid, key, kid, ASSERT_AUD)
    return sva, m.exchange(HYDRA_TOKEN, assertion, API_AUD)


ROLE_EDIT = "rolde95b43bceeb4b998"  # md5('edit')[:17] → FGA editor
RID = str(int(time.time()))[-6:]    # run-id suffix — fresh names per run (idempotent reruns)

boot = m.mint_bootstrap(INTERNAL)

# 1) org structure: seed-owner user → personal account + default project (A1).
owner_ext = "prodseed-net-owner@example.com"
upsert_user(owner_ext)
acctA, projA1 = db_lookup(owner_ext)
# cross project A2 in the same account (bootstrap-admin SA = system_admin).
projA2 = _await(_curl("POST", "/iam/v1/projects", boot,
                      {"accountId": acctA, "name": f"prodseed-net-a2-{RID}"}), boot, "projectId")

# 2) subjects: jwtProjectAdminA1 = editor SA @ A1 (+ @ A2 so cross-project
#    isolation cases can List projB and assert projA's resource is absent — a
#    200-empty check, not a 403 no-access).
sva_editor, editor_tok = make_sa_token(acctA, f"prodseed-net-ed-{RID}", ROLE_EDIT, "iam.project", projA1)
_grant(sva_editor, ROLE_EDIT, "iam.project", projA2)

fixtures = {
    "jwtBootstrap": boot,
    "jwtProjectAdminA1": editor_tok,
    "existingProjectId": projA1,
    "projectA1Id": projA1,
    "existingProjectCrossId": projA2,
    "projectA2Id": projA2,
    "existingZoneId": "ru-central1-a",
    "existingZoneAltId": "ru-central1-b",
    "zoneA": "ru-central1-a",
}
print(json.dumps(fixtures))
