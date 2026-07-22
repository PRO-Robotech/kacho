#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""NLB cross-domain resource deps on top of the production-mode SA matrix (#59).

The NLB suite (NLB-1d) needs a regional suite project/region plus vpc dependencies
(network + zonal subnets + a security group + linked vpc Addresses) that its cases
reference by id (existingSubnetId / existingAddressId / …). Most balancer/listener
cases self-provision their own VIP source, so only the pre-existing linked deps are
seeded here. Instance-target deps (existingInstanceId / existingNicId) require a full
compute Instance (machineType + boot + NIC) and are left to a follow-up seeder — the
targets.py / cross-resource.py cases that need a live Instance degrade to fixture-blocked
(declared in RESULTS.md), never masked.

Reuses the matrix home project (projA1, editor-granted to jwtProjectEditorA/jwtSAA) as
the suite project so the matrix subject grants apply without a fresh binding. Single
region stand (existingRegionAltId == existingRegionId == ru-central1); the "cross-region"
subnet is a second zonal subnet in the same region.
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
RID = pm.RID

proj = MATRIX["projectA1Id"]
proj_cross = MATRIX["projectA2Id"]
region = MATRIX["existingRegionId"]
region_alt = MATRIX["existingRegionAltId"]
zone_a = MATRIX["existingZoneId"]       # ru-central1-a
zone_b = MATRIX["existingZoneAltId"]    # ru-central1-b

out: dict[str, str] = {
    "_suiteProjectId": proj,
    "_suiteProjectCrossId": proj_cross,
    "_suiteRegionId": region,
    "_suiteRegionAltId": region_alt,
}


def create(path, body, key):
    return pm._await(pm._curl("POST", path, boot, body), boot, key)


# --- vpc network + zonal subnets + security group (suite project) ------------
net = create("/vpc/v1/networks", {"projectId": proj, "name": f"ps-nlb-net-{RID}"}, "networkId")
out["existingNetworkId"] = net

sub = create("/vpc/v1/subnets",
             {"projectId": proj, "networkId": net, "name": f"ps-nlb-sub-a-{RID}",
              "zoneId": zone_a, "ipv4CidrPrimary": "10.196.0.0/24"}, "subnetId")
out["existingSubnetId"] = sub
out["existingSubnetCidr"] = "10.196.0.0/24"

sub_x = create("/vpc/v1/subnets",
               {"projectId": proj, "networkId": net, "name": f"ps-nlb-sub-b-{RID}",
                "zoneId": zone_b, "ipv4CidrPrimary": "10.196.1.0/24"}, "subnetId")
out["existingSubnetCrossRegionId"] = sub_x

sg = create("/vpc/v1/securityGroups",
            {"projectId": proj, "networkId": net, "name": f"ps-nlb-sg-{RID}"}, "securityGroupId")
out["existingSgId"] = sg

# --- linked vpc Addresses (internal v4/v6 auto-alloc from the subnet) ---------
# CreateAddressRequest carries the spec oneof at TOP LEVEL (internalIpv4AddressSpec /
# externalIpv4AddressSpec), NOT under an addressSpec wrapper. Internal specs do not
# need an external AddressPool (external-VIP cases self-provision + depend on the seeded
# EXTERNAL pool, out of scope for this seeder).
addr = create("/vpc/v1/addresses",
              {"projectId": proj, "name": f"ps-nlb-adr-{RID}",
               "internalIpv4AddressSpec": {"subnetId": sub}}, "addressId")
out["existingAddressId"] = addr

addr_used = create("/vpc/v1/addresses",
                   {"projectId": proj, "name": f"ps-nlb-adru-{RID}",
                    "internalIpv4AddressSpec": {"subnetId": sub}}, "addressId")
out["existingAddressUsedId"] = addr_used

try:
    addr6 = create("/vpc/v1/addresses",
                   {"projectId": proj, "name": f"ps-nlb-adr6-{RID}",
                    "internalIpv6AddressSpec": {"subnetId": sub}}, "addressId")
    out["existingAddressIPv6Id"] = addr6
except Exception:  # noqa: BLE001 — v6 may be unavailable on the subnet; degrade, don't abort
    pass

# cross-project address (jwtProjectEditorA also holds editor on projA2 in the matrix).
net_x = create("/vpc/v1/networks", {"projectId": proj_cross, "name": f"ps-nlb-netx-{RID}"}, "networkId")
sub_cp = create("/vpc/v1/subnets",
                {"projectId": proj_cross, "networkId": net_x, "name": f"ps-nlb-sub-cp-{RID}",
                 "zoneId": zone_a, "ipv4CidrPrimary": "10.196.2.0/24"}, "subnetId")
addr_cp = create("/vpc/v1/addresses",
                 {"projectId": proj_cross, "name": f"ps-nlb-adrcp-{RID}",
                  "internalIpv4AddressSpec": {"subnetId": sub_cp}}, "addressId")
out["existingAddressCrossProjectId"] = addr_cp

print(json.dumps(out))
