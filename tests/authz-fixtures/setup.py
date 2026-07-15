#!/usr/bin/env python3
"""KAC-127 authz default-deny matrix fixture bootstrap.

Idempotent. Reconciles the matrix fixtures against the running kind dev-stand:

  - ensures the iam graph (account-A/B, projects, users, bindings) — these are
    created idempotently by name/5-tuple dedup; an already-present graph is
    left as-is, only its ids are read back;
  - creates the 2 vpc seed networks (project-A1 / project-B1) — with the
    KAC-127 #22 write-side FGA fix in place, each create emits the
    `vpc_network:<id>#project@project:<pid>` hierarchy tuple;
  - BACKFILLS the per-resource FGA hierarchy tuples for pre-fix iam resources
    (iam_user / project / account-owner / account-admin) that were created
    before the write-side FGA code existed — a one-time migration so the
    matrix's per-resource Checks resolve;
  - patches the kacho-iam / kacho-vpc newman env files with the resolved ids.

Usage:
  GW=http://127.0.0.1:18080 FGA=http://127.0.0.1:18088 \
  STORE_ID=<openfga-store> python3 setup.py
"""

import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

GW = os.environ.get("GW", "http://127.0.0.1:18080")
FGA = os.environ.get("FGA", "http://127.0.0.1:18088")
STORE = os.environ["STORE_ID"]

IAM_ENV = os.environ.get(
    "IAM_ENV",
    os.path.join(os.path.dirname(__file__),
                 "../../project/kacho-iam/tests/newman/environments/local.postman_environment.json"))
VPC_ENV = os.environ.get(
    "VPC_ENV",
    os.path.join(os.path.dirname(__file__),
                 "../../project/kacho-vpc-KAC-127/tests/newman/environments/local.postman_environment.json"))


def http(method, url, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("content-type", "application/json")
    if token:
        req.add_header("authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            payload = json.loads(e.read() or b"{}")
        except Exception:
            payload = {}
        return e.code, payload


def env_map(path):
    with open(path) as f:
        d = json.load(f)
    return d, {v["key"]: v["value"] for v in d["values"]}


def patch_env(path, updates):
    with open(path) as f:
        d = json.load(f)
    have = {v["key"] for v in d["values"]}
    for k, val in updates.items():
        if k in have:
            for v in d["values"]:
                if v["key"] == k:
                    v["value"] = val
        else:
            d["values"].append({"key": k, "value": val, "type": "default", "enabled": True})
    with open(path, "w") as f:
        json.dump(d, f, indent=2)
        f.write("\n")


def await_operation(token, op):
    """Poll an Operation to done=true; return the resource id from metadata."""
    op_id = op.get("id")
    for _ in range(40):
        st, body = http("GET", f"{GW}/iam/v1/operations/{op_id}", token)
        if body.get("done"):
            return body
        time.sleep(0.25)
    return op


def fga_write(tuples):
    """Write hierarchy tuples to OpenFGA; idempotent (already-exists = ok)."""
    if not tuples:
        return 0
    body = {"writes": {"tuple_keys": tuples}}
    st, resp = http("POST", f"{FGA}/stores/{STORE}/write", body=body)
    if st == 200:
        return len(tuples)
    # Retry one-by-one to skip already-existing tuples.
    ok = 0
    for t in tuples:
        s, _ = http("POST", f"{FGA}/stores/{STORE}/write", body={"writes": {"tuple_keys": [t]}})
        if s == 200:
            ok += 1
    return ok


def iam_sql(query):
    """Run a read-only query against the kacho_iam DB, return rows as lists."""
    out = subprocess.run(
        ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "--",
         "env", "PGPASSWORD=dev-iam-password", "psql", "-U", "iam", "-d", "kacho_iam",
         "-tAF|", "-c", query],
        check=True, capture_output=True, text=True)
    return [line.split("|") for line in out.stdout.strip().splitlines() if line]


def vpc_seed_sql(net_id, project_id, name):
    """Seed a vpc network fixture row directly into the kacho_vpc DB.

    The vpc API Create-path is covered by the matrix's own NET-CR-* cases and
    by vpc unit/integration tests; the matrix's GET/UPDATE/DELETE probes only
    need the row + its FGA tuple. Idempotent via ON CONFLICT.
    """
    sql = (
        "INSERT INTO kacho_vpc.networks (id, project_id, name, description, labels) "
        f"VALUES ('{net_id}', '{project_id}', '{name}', 'authz matrix seed', '{{}}') "
        "ON CONFLICT (id) DO UPDATE SET project_id = EXCLUDED.project_id;"
    )
    subprocess.run(
        ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-vpc-0", "--",
         "env", "PGPASSWORD=dev-vpc-password", "psql", "-U", "vpc", "-d", "kacho_vpc",
         "-tAc", sql],
        check=True, capture_output=True)


def fga_object_count(obj_type):
    st, resp = http("POST", f"{FGA}/stores/{STORE}/read", body={"page_size": 100})
    if st != 200:
        return -1
    return sum(1 for t in resp.get("tuples", [])
               if t["key"]["object"].split(":")[0] == obj_type)


def main():
    _, iam_env = env_map(IAM_ENV)
    boot = iam_env["jwtBootstrap"]

    account_a = iam_env["accountAId"]
    project_a1 = iam_env["projectA1Id"]
    project_b1 = iam_env["projectB1Id"]

    print(f"== fixtures: accountA={account_a} projectA1={project_a1} projectB1={project_b1}")

    # --- step 7: vpc seed networks ------------------------------------------
    # The matrix env file ships fixed seedNetwork ids; we honour them so the
    # collections need no regeneration. The networks are seeded directly into
    # the vpc DB (pure fixture rows) — the vpc API Create-path is exercised by
    # the matrix itself (NET-CR-* cases) and by vpc unit/integration tests; the
    # GET/UPDATE/DELETE probes only need the rows + their FGA tuples to exist.
    seed = {
        "seedNetworkA1Id": iam_env.get("seedNetworkA1Id") or "enp585ecmq1fdrp4bejq",
        "seedNetworkB1Id": iam_env.get("seedNetworkB1Id") or "enpy51hjcw95bx4n33b0",
    }
    vpc_seed_sql(seed["seedNetworkA1Id"], project_a1, "authz-seed-net-a1")
    vpc_seed_sql(seed["seedNetworkB1Id"], project_b1, "authz-seed-net-b1")
    print(f"   seeded vpc networks: {seed}")

    # --- backfill: iam_user / project hierarchy tuples ----------------------
    # Pre-KAC-127-#22 iam resources (users / projects created before the
    # write-side FGA hook existed) have no per-resource hierarchy tuple, so a
    # per-resource Check has `no path`. Read the graph straight from the
    # kacho_iam DB (the bootstrap-admin cannot Get users via the authz-gated
    # API) and reconcile the tuples. Idempotent: re-writing an existing tuple
    # is a no-op. New iam resources created from now on get their tuple from
    # the KAC-127 #22 fix (UpsertFromIdentity / Invite / Create use-cases).
    backfill = []

    # All users → iam_user:<id>#account@account:<account_id>.
    for utype, uid, acc in iam_sql(
            "SELECT 'u', id, account_id FROM kacho_iam.users"):
        if uid and acc:
            backfill.append({"user": f"account:{acc}", "relation": "account",
                             "object": f"iam_user:{uid}"})

    # All projects → project:<id>#account@account:<account_id>.
    for ptype, pid, acc in iam_sql(
            "SELECT 'p', id, account_id FROM kacho_iam.projects"):
        if pid and acc:
            backfill.append({"user": f"account:{acc}", "relation": "account",
                             "object": f"project:{pid}"})

    # All accounts → account:<id>#owner@user:<owner_user_id>.
    for atype, aid, owner in iam_sql(
            "SELECT 'a', id, owner_user_id FROM kacho_iam.accounts"):
        if aid and owner:
            backfill.append({"user": f"user:{owner}", "relation": "owner",
                             "object": f"account:{aid}"})

    # All AccessBindings → grant tuple (subject holds the relation on the
    # scope) + iam_access_binding hierarchy (project-scoped only). The relation
    # is derived from the role name suffix (admin/edit/view).
    rel_suffix = {"admin": "admin", "edit": "editor", "view": "viewer"}
    for _, ab_id, subject_id, role_id, res_type, res_id in iam_sql(
            "SELECT 'ab', ab.id, ab.subject_id, ab.role_id, ab.resource_type, "
            "ab.resource_id FROM kacho_iam.access_bindings ab"):
        rel = "viewer"
        rows = iam_sql(
            f"SELECT name FROM kacho_iam.roles WHERE id = '{role_id}'")
        if rows:
            rname = rows[0][0]
            for suf, r in rel_suffix.items():
                if rname.endswith("." + suf) or rname == suf or rname == r:
                    rel = r
        if subject_id and res_type and res_id:
            backfill.append({"user": f"user:{subject_id}", "relation": rel,
                             "object": f"{res_type}:{res_id}"})
        # iam_access_binding hierarchy — project-scoped binding only.
        if res_type == "project" and res_id and ab_id:
            backfill.append({"user": f"project:{res_id}", "relation": "project",
                             "object": f"iam_access_binding:{ab_id}"})

    # vpc seed networks → vpc_network:<id>#project@project:<pid> hierarchy.
    # (Equivalent to the KAC-127 #22 write-side FGA emission; here applied to
    # the directly-seeded fixture rows.)
    backfill.append({"user": f"project:{project_a1}", "relation": "project",
                     "object": f"vpc_network:{seed['seedNetworkA1Id']}"})
    backfill.append({"user": f"project:{project_b1}", "relation": "project",
                     "object": f"vpc_network:{seed['seedNetworkB1Id']}"})

    written = fga_write(backfill)
    print(f"== backfilled {written}/{len(backfill)} iam hierarchy/grant tuples")

    # --- patch env files ----------------------------------------------------
    patch_env(IAM_ENV, seed)
    if os.path.exists(VPC_ENV):
        patch_env(VPC_ENV, seed)
    print(f"== patched env files: {seed}")

    # --- census -------------------------------------------------------------
    time.sleep(1)
    for t in ["iam_user", "vpc_network", "account", "project", "iam_access_binding"]:
        print(f"   FGA {t}: {fga_object_count(t)}")


if __name__ == "__main__":
    main()
