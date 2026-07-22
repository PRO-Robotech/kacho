#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""VPC list-filter-d fixtures on top of the production-mode SA matrix (#59).

Per-object filtered-List needs FGA tuples that the public AccessBinding API cannot
express (vpc_subnet object-scope). Subject S (subset-viewer) gets project#v_list
(method-gate) + vpc_subnet#v_list/v_get on ONE visible subnet; subject N
(no-subnet-grant) gets project#v_list only (List returns 200 empty — project tier
does NOT cascade visibility onto subnets in the explicit model). The hidden subnet
is granted to nobody (no-leak). Both subjects are RS256 ServiceAccount principals.

Reads /tmp/matrix.json (boot token + acctA), seeds the resources + tuples, and
emits ONLY the list-filter extra fixtures on stdout.
"""
from __future__ import annotations

import json
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402
import prodseed_matrix as pm  # noqa: E402  (reuse helpers: _curl,_await,make_sa,sa_token,etc.)

MATRIX = json.loads(open("/tmp/matrix.json").read())
# Mint a FRESH bootstrap token — the cached jwtBootstrap has a 1h TTL and may have
# expired since the matrix was seeded (silent code=16 "token validation failed").
pm.boot = m.mint_bootstrap(pm.INTERNAL)   # rebind module-level boot for helper reuse
boot = pm.boot
acctA = MATRIX["accountAId"]
RID = pm.RID


def fga_write(user, relation, obj):
    """Insert an FGA owner tuple into iam fga_outbox (drainer materialises it)."""
    sql = (
        "INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at) "
        "SELECT 'fga.tuple.write', "
        f"jsonb_build_object('user','{user}','relation','{relation}','object','{obj}'), now() "
        "WHERE NOT EXISTS (SELECT 1 FROM kacho_iam.fga_outbox "
        f"WHERE payload->>'user'='{user}' AND payload->>'relation'='{relation}' AND payload->>'object'='{obj}');"
    )
    args = ["kubectl", "-n", "kacho", "exec", "kacho-umbrella-pg-iam-0", "-c", "postgresql",
            "--", "sh", "-c", f'PGPASSWORD="$POSTGRES_PASSWORD" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc "{sql}"']
    subprocess.run(args, capture_output=True, text=True)


# 1) list-filter project + network + visible/hidden subnets (as bootstrap admin).
lf_proj = pm._await(pm._curl("POST", "/iam/v1/projects", boot,
                             {"accountId": acctA, "name": f"ps-lf-{RID}"}), boot, "projectId")
lf_net = pm._await(pm._curl("POST", "/vpc/v1/networks", boot,
                            {"projectId": lf_proj, "name": f"ps-lf-net-{RID}"}), boot, "networkId")
lf_vis = pm._await(pm._curl("POST", "/vpc/v1/subnets", boot,
                            {"projectId": lf_proj, "networkId": lf_net, "name": f"ps-lf-vis-{RID}",
                             "zoneId": "ru-central1-a", "ipv4CidrPrimary": "10.193.0.0/24"}), boot, "subnetId")
lf_hid = pm._await(pm._curl("POST", "/vpc/v1/subnets", boot,
                            {"projectId": lf_proj, "networkId": lf_net, "name": f"ps-lf-hid-{RID}",
                             "zoneId": "ru-central1-a", "ipv4CidrPrimary": "10.193.1.0/24"}), boot, "subnetId")

# 2) two SA subjects (no AccessBinding grants — pure FGA tuple grants below).
sva_sv = pm.make_sa(acctA, f"ps-lf-sv-{RID}")   # subset-viewer S
sva_ng = pm.make_sa(acctA, f"ps-lf-ng-{RID}")   # no-subnet-grant N
tok_sv = pm.sa_token(sva_sv)
tok_ng = pm.sa_token(sva_ng)

# 3) FGA tuples (service_account subjects).
#    method-gate: both get project#v_list → List returns 200 (not method-403).
fga_write(f"service_account:{sva_sv}", "v_list", f"project:{lf_proj}")
fga_write(f"service_account:{sva_ng}", "v_list", f"project:{lf_proj}")
#    per-object visibility: only S sees the visible subnet; hidden granted to nobody.
fga_write(f"service_account:{sva_sv}", "v_list", f"vpc_subnet:{lf_vis}")
fga_write(f"service_account:{sva_sv}", "v_get", f"vpc_subnet:{lf_vis}")

time.sleep(3)  # let the drainer materialise the tuples before newman reads

print(json.dumps({
    "listFilterProjectId": lf_proj,
    "subnetVisibleId": lf_vis,
    "subnetHiddenId": lf_hid,
    "jwtSubnetSubsetViewer": tok_sv,
    "jwtNoSubnetGrant": tok_ng,
}))
