# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для NetworkInterfaceService (kacho-vpc) — first-class NIC-ресурс.
REST: /vpc/v1/networkInterfaces.

Контракт изоляции — как везде: каждый case внутри своего runId, suite работает
в pre-allocated existingProjectId. Имена ресурсов суффиксуются {{runId}}.

Проекция NIC — lean, control-plane only: id/projectId/subnetId/v4AddressIds/v6AddressIds/
securityGroupIds/usedBy/status/name/labels. Инфра/data-plane-полей у kacho-vpc нет (появятся
на стороне будущего kacho-vpc-implement). Денилист `_LEAN_FORBIDDEN` ниже — регрессионный guard:
эти инфра-чувствительные поля НИКОГДА не должны появиться на публичной поверхности.
"""

CASES = []


def _net_subnet_steps(suffix, cidr="10.60.0.0/24"):
    """Helper: создает parent Network + Subnet, сохраняет netId/subId."""
    return [
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": f"nic-{suffix}-net-{{{{runId}}}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="pre-subnet", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": f"nic-{suffix}-sub-{{{{runId}}}}", "zoneId": "{{existingZoneId}}",
                   # VPC-1 F7: Create takes the immutable primary anchor ipv4CidrPrimary
                   # (single), not the retired v4_cidr_blocks array.
                   "ipv4CidrPrimary": cidr},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
    ]


def _cleanup_subnet():
    return retry_until_authorized(Step(name="cleanup-subnet", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
                test_script=["pm.test('cleanup subnet (200 or 400 if child leaked)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                             *save_from_response("j.id", "opId")]))


def _cleanup_net():
    return retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
                test_script=["pm.test('cleanup net (200 or 400 if child leaked)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                             *save_from_response("j.id", "opId")]))


def _cleanup_nic(env="nicId"):
    return [
        retry_until_authorized(Step(name="cleanup-nic", method="DELETE", path=f"/vpc/v1/networkInterfaces/{{{{{env}}}}}",
             test_script=["pm.test('cleanup nic (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ]


_LEAN_FORBIDDEN = ["vpnId", "hvId", "sid", "hostIface", "netns", "gatewayIp",
                   "containerId", "networkId", "instanceId", "index"]


def _assert_lean_projection():
    return [
        "const j = pm.response.json();",
        "pm.test('has id/projectId/subnetId/status', () => {",
        "  pm.expect(j.id, 'id').to.be.a('string');",
        "  pm.expect(j.projectId, 'projectId').to.be.a('string');",
        "  pm.expect(j.subnetId, 'subnetId').to.be.a('string');",
        "  pm.expect(j.status, 'status').to.be.a('string');",
        "});",
        # APPLY-04: статус выражает ПРИВЯЗКУ, и только её. У свежесозданного
        # интерфейса потребителя нет — значит AVAILABLE. Утверждается ЗНАЧЕНИЕ, а
        # не «строка»: «строка» выполнялось бы и на снятых значениях, заявлявших
        # программирование сети, — то есть ровно на том, что убрано.
        "pm.test('статус свежего интерфейса выражает отсутствие привязки', () => "
        "pm.expect(j.status, pm.response.text()).to.eql('AVAILABLE'));",
        f"pm.test('no infra-sensitive fields on public projection', () => {{",
        f"  const forbidden = {_LEAN_FORBIDDEN!r};",
        "  forbidden.forEach(k => pm.expect(j, 'leaked ' + k).to.not.have.property(k));",
        "});",
    ]


# ---------------------------------------------------------------------------
# NIC-CR
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="NIC-CR-CRUD-OK",
    title="Create NIC в свежей network+subnet → Operation → poll → get → lean public projection",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_subnet_steps("cr"),
        Step(name="create-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-cr-{{runId}}"},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-nic", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *_assert_lean_projection(),
                          "pm.test('subnetId matches', () => pm.expect(pm.response.json().subnetId).to.eql(pm.environment.get('subId')));"])),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-CR-MAC-OK",
    title="Create NIC → macAddress в lean-проекции, формат 0e:xx:xx:xx:xx:xx, стабилен при Update name",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_subnet_steps("mac"),
        Step(name="create-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-mac-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-nic-mac", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('macAddress на публичной проекции', () => pm.expect(j.macAddress, 'macAddress').to.be.a('string'));",
                          "pm.test('macAddress в Kachō-формате 0e:xx:xx:xx:xx:xx (lowercase hex)', () => pm.expect(j.macAddress).to.match(/^0e:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}$/));",
                          "pm.environment.set('savedMac', j.macAddress);"])),
        Step(name="update-nic-name", method="PATCH", path="/vpc/v1/networkInterfaces/{{nicId}}",
             body={"updateMask": "name", "name": "nic-mac-renamed-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="get-nic-mac-stable", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('macAddress не меняется при Update (stable NIC MAC)', () => pm.expect(j.macAddress).to.eql(pm.environment.get('savedMac')));"]),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-CR-NEG-DUP-NAME",
    title="Create двух NIC с одинаковым name в одном project → второй ALREADY_EXISTS (async op.error)",
    classes=["NEG", "CONF"],
    priority="P1",
    steps=[
        *_net_subnet_steps("dup"),
        Step(name="create-1", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-dup-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="create-2-dup", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-dup-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="assert-dup-failed", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('2nd create failed (already exists)', () => {",
                          "  pm.expect(j.done).to.eql(true);",
                          "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object');",
                          "  pm.expect(j.error.code).to.eql(6);",  # ALREADY_EXISTS
                          "});"]),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-CR-NEG-BAD-SUBNET",
    title="Create NIC с несуществующим subnetId → NotFound (async op.error code=5)",
    classes=["NEG"],
    priority="P1",
    steps=[
        Step(name="create-bad-subnet", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{garbageVpcId}}", "name": "nic-bs-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="assert-nf", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('op failed NotFound', () => {",
                          "  pm.expect(j.done).to.eql(true);",
                          "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object');",
                          "  pm.expect(j.error.code).to.eql(5);",  # NOT_FOUND
                          "});"]),
    ],
))

CASES.append(Case(
    id="NIC-LIST-OK",
    title="List NIC by projectId → 200, массив; созданный NIC присутствует",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_subnet_steps("lst"),
        Step(name="create-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-lst-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_present(Step(name="list", method="GET", path="/vpc/v1/networkInterfaces?projectId={{_suiteProjectId}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('networkInterfaces array', () => pm.expect(j.networkInterfaces || []).to.be.an('array'));",
                          "pm.test('created NIC present', () => pm.expect((j.networkInterfaces || []).map(n => n.id)).to.include(pm.environment.get('nicId')));"]),
             "nicId"),
        # Unscoped list — gateway authz-first 403 (no path) ЛИБО backend 400. Оба =
        # «отклонено». См. assert_unscoped_rejected (gen.py).
        Step(name="list-no-project", method="GET", path="/vpc/v1/networkInterfaces",
             test_script=[*assert_unscoped_rejected()]),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-UPD-OK",
    title="Update NIC description/labels/securityGroupIds → 200, изменения видны в GET",
    classes=["CRUD", "STATE"],
    priority="P1",
    steps=[
        *_net_subnet_steps("upd"),
        retry_until_authorized(Step(name="get-net-for-sg", method="GET", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.defaultSecurityGroupId", "defSgId")])),
        Step(name="create-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-upd-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="patch-nic", method="PATCH", path="/vpc/v1/networkInterfaces/{{nicId}}",
             body={"updateMask": "description,labels,securityGroupIds",
                   "description": "nic-upd-desc-{{runId}}",
                   "labels": {"k": "v"},
                   "securityGroupIds": ["{{defSgId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Read-your-writes: и patch-nic, и этот GET гейтятся per-object authz-Check,
        # но owner-tuple свежего NIC материализуется eventually-consistent → первый
        # read-доступ без ретрая может поймать устойчивый 403/404.
        retry_until_authorized(Step(name="get-after-upd", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('description updated', () => pm.expect(j.description).to.eql('nic-upd-desc-' + pm.environment.get('runId')));",
                          "pm.test('labels updated', () => pm.expect(j.labels && j.labels.k).to.eql('v'));",
                          "pm.test('securityGroupIds set', () => pm.expect(j.securityGroupIds || []).to.include(pm.environment.get('defSgId')));"])),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-DEL-OK",
    title="Delete NIC (не приаттаченный) → Operation → poll done без ошибки → GET 404",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_subnet_steps("del"),
        Step(name="create-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-del-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('delete op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        Step(name="get-gone", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-CR-WITH-ADDR-OK",
    title="Create internal_ipv4 Address в subnet → create NIC с этим address id в v4AddressIds → get → echoed",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_net_subnet_steps("waddr"),
        Step(name="create-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-waddr-addr-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        Step(name="create-nic-with-addr", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-waddr-{{runId}}", "v4AddressIds": ["{{addrId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-create-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get-nic", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "pm.test('v4AddressIds echoed', () => pm.expect(pm.response.json().v4AddressIds || []).to.include(pm.environment.get('addrId')));"])),
        *_cleanup_nic(),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=["pm.test('cleanup addr (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # NIC, ссылающийся на internal_ipv6 Address: создаем network + subnet (с v6 cidr)
    # + internal_ipv6 Address + NIC с этим addr id в v6AddressIds → GET NIC отдает
    # v6AddressIds.
    id="NIC-CR-WITH-V6-ADDR-OK",
    title="Create internal_ipv6 Address в subnet с v6 cidr → NIC с этим id в v6AddressIds → GET → echoed",
    classes=["CRUD"],
    priority="P2",
    steps=[
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-v6addr-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="pre-subnet", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "nic-v6addr-sub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.61.0.0/24", "ipv6CidrPrimary": "fd00:cafe:f00d::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="create-v6-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-v6addr-addr-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        Step(name="create-nic-with-v6-addr", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-v6addr-{{runId}}", "v6AddressIds": ["{{addrId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-create-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get-nic", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "pm.test('v6AddressIds echoed', () => pm.expect(pm.response.json().v6AddressIds || []).to.include(pm.environment.get('addrId')));"])),
        *_cleanup_nic(),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=["pm.test('cleanup addr (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # При линковке адреса к интерфейсу v4_address_ids или v6_address_ids (или оба
    # вместе) могут быть заполнены. Проверяем, что одновременная линковка v4 + v6
    # при создании NIC работает.
    id="NIC-CR-WITH-BOTH-ADDR-OK",
    title="Create NIC с v4_address_ids И v6_address_ids одновременно → 200, оба address привязаны",
    classes=["CRUD"], priority="P1",
    steps=[
        # Network + Subnet с v4 и v6 cidr.
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-both-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="pre-subnet", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "nic-both-sub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.62.0.0/24", "ipv6CidrPrimary": "fd00:cafe:b00b::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        # v4 + v6 addresses
        Step(name="cr-v4-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-both-v4-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "v4AddrId")]),
        poll_operation_until_done(),
        Step(name="cr-v6-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-both-v6-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "v6AddrId")]),
        poll_operation_until_done(),
        # NIC create — оба address-id одновременно.
        Step(name="cr-nic-both", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-both-{{runId}}",
                   "v4AddressIds": ["{{v4AddrId}}"],
                   "v6AddressIds": ["{{v6AddrId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-nic-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC.Create op done, no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get-nic", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('v4AddressIds linked', () => pm.expect(j.v4AddressIds || []).to.include(pm.environment.get('v4AddrId')));",
                          "pm.test('v6AddressIds linked', () => pm.expect(j.v6AddressIds || []).to.include(pm.environment.get('v6AddrId')));"])),
        # Cleanup снизу вверх: NIC → addresses → subnet → network.
        *_cleanup_nic(),
        Step(name="cleanup-v4-addr", method="DELETE", path="/vpc/v1/addresses/{{v4AddrId}}",
             test_script=["pm.test('cleanup v4 addr (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-v6-addr", method="DELETE", path="/vpc/v1/addresses/{{v6AddrId}}",
             test_script=["pm.test('cleanup v6 addr (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # Address, используемый NIC через v4AddressIds, нельзя удалить —
    # AddressService.Delete синхронно отвергает FAILED_PRECONDITION (409). После
    # удаления NIC адрес освобождается и удаляется.
    id="ADDR-DEL-NEG-USED-BY-NIC",
    title="Delete Address, который в использовании у NIC → 409 FailedPrecondition; после delete NIC → Address удаляется",
    classes=["NEG", "STATE", "CONF"],
    priority="P1",
    steps=[
        *_net_subnet_steps("delusedbynic"),
        Step(name="create-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-dubn-addr-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        Step(name="create-nic-with-addr", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-dubn-{{runId}}", "v4AddressIds": ["{{addrId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-nic-created", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="del-addr-blocked", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             # grpc-gateway маппит FAILED_PRECONDITION (9) → HTTP 400.
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('message mentions network interface', () => pm.expect(pm.response.json().message).to.include('network interface'));"])),
        # Удаляем NIC → адрес освобождается.
        Step(name="del-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Теперь Address удаляется.
        retry_until_authorized(Step(name="del-addr-ok", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-addr-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('addr delete op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        Step(name="get-addr-gone", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # network_id у SG обязателен — «network-less» SG не существует. SG создается
    # привязанной к сети NIC'а ({{netId}}); кейс проверяет, что NIC ссылается на SG
    # через securityGroupIds[].
    id="NIC-CR-WITH-UNBOUND-SG-OK",
    title="Create SG (bound к сети NIC) → create NIC c этим SG в securityGroupIds → get → echoed",
    classes=["CRUD"],
    priority="P2",
    steps=[
        *_net_subnet_steps("wsg"),
        Step(name="create-sg", method="POST", path="/vpc/v1/securityGroups",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "nic-wsg-sg-{{runId}}", "ruleSpecs": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.securityGroupId", "sgId")]),
        poll_operation_until_done(),
        Step(name="create-nic-with-sg", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-wsg-{{runId}}", "securityGroupIds": ["{{sgId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        Step(name="assert-create-ok", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('NIC create with SG done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        retry_until_authorized(Step(name="get-nic", method="GET", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "pm.test('securityGroupIds echoed', () => pm.expect(pm.response.json().securityGroupIds || []).to.include(pm.environment.get('sgId')));"])),
        *_cleanup_nic(),
        Step(name="cleanup-sg", method="DELETE", path="/vpc/v1/securityGroups/{{sgId}}",
             test_script=["pm.test('cleanup sg (200 or 400)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


# ---------------------------------------------------------------------------
# NIC negative + ephemeral lifecycle
# ---------------------------------------------------------------------------

def _make_addr(name_suffix, ip_field="internalIpv4AddressSpec"):
    """Helper: создает internal Address в subnet, сохраняет в env addrId<suffix>."""
    env_var = f"addrId{name_suffix}"
    return [
        Step(name=f"create-addr-{name_suffix}", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}",
                   # Суффикс различает шаги и переменные (A/B) и потому остаётся как есть,
                   # а в ИМЯ ресурса идёт в нижнем регистре — форма (#715).
                   "name": f"nic-multi-addr-{name_suffix.lower()}-{{{{runId}}}}",
                   ip_field: {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", env_var)]),
        poll_operation_until_done(),
    ]


CASES.append(Case(
    id="NIC-CR-NEG-MULTI-V4-ADDR",
    title="Create NIC с 2× v4_address_ids → InvalidArgument (cardinality CHECK constraint, REQ-NIC-04)",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        *_net_subnet_steps("m4"),
        *_make_addr("A"),
        *_make_addr("B"),
        Step(name="create-nic-2v4", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-m4-{{runId}}",
                   "v4AddressIds": ["{{addrIdA}}", "{{addrIdB}}"]},
             test_script=[
                 # ИСХОД НАЗВАН ОДИН, ПОТОМУ ЧТО ОН ОДИН.
                 #
                 # Прежняя редакция принимала `oneOf([200, 400])` и объясняла это тем,
                 # что отказ «может быть» синхронным либо асинхронным через DB-CHECK.
                 # Выбора нет: `validateNICAddressCardinality` стоит в create.go:108
                 # БЕЗУСЛОВНО и ДО того, как минтится Operation, поэтому 200 на этом
                 # входе недостижим. Утверждение, принимающее и успех, и отказ, —
                 # отсутствие утверждения: сними проверку кардинальности из сервиса,
                 # и кейс останется зелёным (он примет 200 и уйдёт в мёртвую ветку).
                 # DB-CHECK 0018 остаётся атомарным backstop'ом гонки, а не вторым
                 # законным ответом на этот запрос.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('отказ называет поле и потолок', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.message, pm.response.text())",
                 "    .to.eql('at most one IPv4 address per network interface (use multiple NICs for multi-IP)');",
                 "  const fv = ((j.details || []).find(d => (d.fieldViolations || []).length) || {}).fieldViolations || [];",
                 "  pm.expect(fv.map(v => v.field), pm.response.text()).to.include('v4_address_ids');",
                 "});",
                 "pm.environment.set('opId', '');",
             ]),
        # Cleanup addresses (NIC не создалось → не блокирует).
        Step(name="del-addrA", method="DELETE", path="/vpc/v1/addresses/{{addrIdA}}",
             test_script=["pm.test('del addr A 200|400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="del-addrB", method="DELETE", path="/vpc/v1/addresses/{{addrIdB}}",
             test_script=["pm.test('del addr B 200|400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="NIC-CR-NEG-MULTI-V6-ADDR",
    title="Create NIC с 2× v6_address_ids → InvalidArgument/FailedPrecondition (cardinality v6≤1, REQ-NIC-04)",
    classes=["NEG", "VAL"], priority="P0",
    steps=[
        *_net_subnet_steps("m6"),
        # Subnet с v6-CIDR — иначе нельзя allocate v6 Address.
        retry_until_authorized(Step(name="add-v6-cidr", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv6CidrBlocks": ["fd12:3456:78aa::/64"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Два v6 Address.
        Step(name="create-addrA-v6", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-m6-a-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrIdA")]),
        poll_operation_until_done(),
        Step(name="create-addrB-v6", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "nic-m6-b-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrIdB")]),
        poll_operation_until_done(),
        Step(name="create-nic-2v6", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-m6-{{runId}}",
                   "v6AddressIds": ["{{addrIdA}}", "{{addrIdB}}"]},
             test_script=[
                 # Тот же довод, что у v4-близнеца: проверка кардинальности стоит
                 # безусловно и до Operation (helpers.go:50), поэтому 200 недостижим,
                 # а ветка опроса за ним недостижима вместе с ним. Асинхронного
                 # исхода у этого входа нет — есть только backstop на гонку.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('отказ называет поле и потолок', () => {",
                 "  const j = pm.response.json();",
                 "  pm.expect(j.message, pm.response.text())",
                 "    .to.eql('at most one IPv6 address per network interface (use multiple NICs for multi-IP)');",
                 "  const fv = ((j.details || []).find(d => (d.fieldViolations || []).length) || {}).fieldViolations || [];",
                 "  pm.expect(fv.map(v => v.field), pm.response.text()).to.include('v6_address_ids');",
                 "});",
                 "pm.environment.set('opId', '');",
             ]),
        retry_until_authorized(Step(name="del-addrA-v6", method="DELETE", path="/vpc/v1/addresses/{{addrIdA}}",
             test_script=["pm.test('del 200|400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-addrB-v6", method="DELETE", path="/vpc/v1/addresses/{{addrIdB}}",
             test_script=["pm.test('del 200|400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))






# ── Ограничение полосы, задаваемое арендатором ───────────────────────────────
#
# ПРЕДПОСЫЛКА ЭТИХ ДВУХ КЕЙСОВ НАЗВАНА ПРЯМО: чарт этого дерева объявляет
# `dataplane.executor.tenantSettableBandwidthLimit: false`
# (`services/vpc/deploy/values.yaml`), и это объявление читает декларативная проба
# `services/vpc/deploy/executor_profile_test.go`. То есть предмет отказа —
# СВОЙСТВО СТЕНДА, а не универсальная истина.
#
# Что будет, если стенд объявит умение: отрицательный кейс покраснеет. Это верное
# поведение, а не хрупкость — предпосылка изменилась, и изменение обязано быть
# видно. Тогда рядом заводится положительная половина (величина применяется и
# видна в ресурсе), а этот кейс переезжает на стенд без умения. Маска, whitelist
# и «известное красное» здесь запрещены ровно как везде.

CASES.append(Case(
    id="NIC-CR-VAL-BANDWIDTH-LIMIT-NOT-DECLARED",
    title="Create NIC с bandwidthLimitMbps на стенде без умения → InvalidArgument с именем поля",
    classes=["VAL", "NEG"],
    priority="P1",
    steps=[
        *_net_subnet_steps("bwlim", cidr="10.68.0.0/24"),
        Step(name="create-with-limit", method="POST", path="/vpc/v1/networkInterfaces",
             # Величина заведомо ЗАКОННА по промежутку (выше гарантированного пола
             # 1000 Мбит/с): единственная причина отказа — необъявленное умение
             # исполнителя. Возьми величину вне промежутка — и кейс перестал бы
             # различать две разные причины отказа.
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-bwlim-{{runId}}", "bandwidthLimitMbps": 2000},
             test_script=[
                 # Отказ СИНХРОННЫЙ: величину задаёт вызывающий, и её негодность
                 # видна без обращения к БД. `oneOf([200, 400])` здесь было бы
                 # отсутствием утверждения — сними проверку из сервиса, и кейс
                 # остался бы зелёным, приняв 200.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('отказ называет поле и причину', () => {",
                 "  const j = pm.response.json();",
                 "  const fv = ((j.details || []).find(d => (d.fieldViolations || []).length) || {}).fieldViolations || [];",
                 "  pm.expect(fv.map(v => v.field), pm.response.text()).to.include('bandwidth_limit_mbps');",
                 "  pm.expect(j.message || '', pm.response.text()).to.include('does not declare');",
                 "});",
                 "pm.environment.set('opId', '');",
             ]),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="NIC-CR-VAL-BANDWIDTH-LIMIT-UNSET-OK",
    title="Create NIC с bandwidthLimitMbps=0 на том же стенде → 200 (отсутствие просьбы не есть просьба)",
    classes=["VAL", "CRUD"],
    priority="P1",
    steps=[
        # Положительный контроль к предыдущему кейсу, и он обязателен: без него
        # «отвергнуто» неотличимо от «этот стенд не принимает создание интерфейса
        # вовсе», а отказ выше был бы неотличим от заглушенного пути.
        *_net_subnet_steps("bwzero", cidr="10.69.0.0/24"),
        Step(name="create-zero-limit", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-bwzero-{{runId}}", "bandwidthLimitMbps": 0},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-nic", method="GET",
             path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200),
                          "pm.test('ограничения нет — и это видно нулём, а не отсутствием ключа', () => {",
                          "  const j = pm.response.json();",
                          "  pm.expect(j.bandwidthLimitMbps === undefined || Number(j.bandwidthLimitMbps) === 0,",
                          "    pm.response.text()).to.eql(true);",
                          "});"])),
        *_cleanup_nic(),
        _cleanup_subnet(),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))
