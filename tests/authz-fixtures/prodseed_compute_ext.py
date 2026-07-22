#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Compute resource deps on top of the production-mode SA matrix (#59).

Compute instances need a zone-coherent NIC spec (network + subnet + security group)
resolvable in the suite project. Seed them once (as bootstrap admin) in the matrix
home project and emit existingNetworkId / existingSubnetId / existingSgId.
"""
from __future__ import annotations

import json
import sys

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import mint_rs256 as m  # noqa: E402
import prodseed_matrix as pm  # noqa: E402

MATRIX = json.loads(open("/tmp/matrix.json").read())
pm.boot = m.mint_bootstrap(pm.INTERNAL)   # fresh bootstrap (cached one may be expired)
boot = pm.boot
proj = MATRIX["projectA1Id"]
RID = pm.RID
ZONE = "ru-central1-a"

net = pm._await(pm._curl("POST", "/vpc/v1/networks", boot,
                         {"projectId": proj, "name": f"ps-cmp-net-{RID}"}), boot, "networkId")
sub = pm._await(pm._curl("POST", "/vpc/v1/subnets", boot,
                         {"projectId": proj, "networkId": net, "name": f"ps-cmp-sub-{RID}",
                          "zoneId": ZONE, "ipv4CidrPrimary": "10.194.0.0/24"}), boot, "subnetId")
sg = pm._await(pm._curl("POST", "/vpc/v1/securityGroups", boot,
                        {"projectId": proj, "networkId": net, "name": f"ps-cmp-sg-{RID}"}), boot, "securityGroupId")

print(json.dumps({
    "existingNetworkId": net,
    "existingSubnetId": sub,
    "existingSgId": sg,
}))
