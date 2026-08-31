# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для SubnetService (kacho-vpc)."""

CASES = []

def _make_net(name_suffix="net"):
    """Helper: набор шагов для создания parent Network + сохранения netId."""
    return [
        Step(
            name="pre-create-net",
            method="POST",
            path="/vpc/v1/networks",
            # Сеть объявляет план ОБОИХ семейств: подсеть нарезается ИЗ объявленного
            # плана, а адрес может приходить и позже, отдельным глаголом на подсети —
            # эту форму первая редакция гейта тоже не видела. Блоки шире всех
            # диапазонов этого файла (10.x/24 и fd00../fd12../fd34../64); негативов на
            # вложенность здесь нет, поэтому широкий план ничего не ослабляет.
            # Суффикс приводится к нижнему регистру ЗДЕСЬ, а не у вызывающих: имя
            # ресурса обязано отвечать единственной форме дерева (#715), а суффиксы
            # исторически писались верблюжьим регистром (dupCidr, luaCount). Правка
            # в помощнике закрывает и тех вызывающих, которых ещё нет.
            body={"projectId": "{{_suiteProjectId}}", "name": f"sub-{name_suffix.lower()}-{{{{runId}}}}",
                  "ipv4CidrBlocks": ["10.0.0.0/8"], "ipv6CidrBlocks": ["fd00::/8"]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.networkId", "netId")],
        ),
        poll_operation_until_done(),
    ]


def _cleanup_net():
    return retry_until_authorized(Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
                test_script=[*assert_status(200), *save_from_response("j.id", "opId")]))


def _cleanup_net_lenient():
    # См. route-table.py::_cleanup_net_lenient — wrap'нутый Create мог пройти permissive'но
    # (subnet создан) → DELETE сети блокируется FK RESTRICT (400). Полос две, и
    # КАЖДАЯ читается своей подписью (`assert_cleanup_delete`), а не принимается скопом.
    # retry_on=(403,): DELETE своей свежей сети может краснеть 403, пока owner-tuple
    # материализуется (eventual-consistency после opgate) — ретраим ТОЛЬКО этот транзиент;
    # 200/400 — терминальны, 404 не крутим (сеть не удаляется дважды в этих кейсах).
    return retry_until_authorized(
        Step(name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_cleanup_delete("сеть", "в сети остались подсети, таблицы маршрутизации или группы"),
                          *save_from_response("j.id", "opId")]),
        retry_on=(403,))


# ---------------------------------------------------------------------------
# SUB-CR
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-CR-CRUD-OK",
    title="Create subnet → Operation → Subnet visible in GET",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_make_net("cr"),
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/subnets",
            body={
                "projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                "name": "sub-cr-{{runId}}", "zoneId": "{{existingZoneId}}",
                "ipv4CidrPrimary": "10.42.0.0/24",
            },
            test_script=[*assert_status(200), *assert_operation_envelope(),
                         *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-confirms",
            method="GET",
            path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('cidr matches', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.include('10.42.0.0/24'));"],
        )),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# VPC-1 F8: подсеть без явного routeTableId привязывается к ОБЪЯВЛЕННОМУ дефолту
# сети (`network.defaultRouteTableId°`, system-created на Network.Create), а не к
# «какой-нибудь» RouteTable этой сети. Кейс держит в сети ВТОРУЮ, tenant-созданную
# RT — конкурента, которого выбрал бы снятый DB-выбор («самая ранняя»/«последняя
# вставленная»), — и требует, чтобы подсеть взяла именно дефолт.
CASES.append(Case(
    id="SUB-CR-STATE-DEFAULT-RT-NOT-ARBITRARY",
    title="Create Subnet в сети с дополнительной tenant-RT → routeTableId = network.defaultRouteTableId°, НЕ произвольная RT сети (F8)",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_make_net("autopick"),
        retry_until_authorized(Step(name="get-net-default-rt", method="GET", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200),
                          "pm.test('network carries a system default RT', () => "
                          "pm.expect(pm.response.json().defaultRouteTableId, JSON.stringify(pm.response.json()))"
                          ".to.be.a('string').and.not.empty);",
                          *save_from_response("j.defaultRouteTableId", "defRtId")])),
        # 1. Вторая (tenant) RouteTable в этой сети — конкурент дефолту.
        Step(name="cr-rt", method="POST", path="/vpc/v1/routeTables",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-autopick-rt-{{runId}}", "staticRoutes": []},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.routeTableId", "rtId")]),
        poll_operation_until_done(),
        # 2. Subnet без явного routeTableId.
        Step(name="cr-sub-autopick", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-autopick-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.246.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="assert-sub-created", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('Subnet.Create op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        # 3. Главная проверка: взят объявленный дефолт, а не tenant-RT.
        retry_until_authorized(Step(name="get-sub-autopicked", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('subnet.routeTableId == network.defaultRouteTableId', () => pm.expect(j.routeTableId, JSON.stringify(j)).to.eql(pm.environment.get('defRtId')));",
                 "pm.test('НЕ произвольная RT сети (tenant-RT не выбрана)', () => pm.expect(j.routeTableId).to.not.eql(pm.environment.get('rtId')));",
             ])),
        # Cleanup снизу вверх.
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=["pm.test('cleanup sub (200/400/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="cleanup-rt", method="DELETE", path="/vpc/v1/routeTables/{{rtId}}",
             test_script=["pm.test('cleanup rt (200/400/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));",
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-VAL-ZONE-REQUIRED",
    title="Create без zone_id → InvalidArgument (zone_id required)",
    classes=["VAL"],
    priority="P0",
    steps=[
        *_make_net("noz"),
        Step(
            name="create-no-zone",
            method="POST",
            path="/vpc/v1/subnets",
            # placement_type — server-derived (F6): в тело не передаётся. Ни zoneId,
            # ни regionId не заданы → нет placement-anchor → server-derive отвергает
            # sync 400 «exactly one of zone_id, region_id must be set». Тестируем
            # anchor-required (без zoneId subnet не размещается).
            body={"ipv4CidrPrimary": "10.100.0.0/24", "projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-noz-{{runId}}"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")],
        ),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-VAL-ZONE-UNKNOWN",
    title="Create с несуществующей зоной → sync 400 FAILED_PRECONDITION \"unknown zone id '...'\"",
    classes=["VAL"],
    priority="P0",
    steps=[
        *_make_net("zu"),
        Step(
            name="create-unknown-zone",
            method="POST",
            path="/vpc/v1/subnets",
            body={"ipv4CidrPrimary": "10.101.0.0/24", "projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-zu-{{runId}}", "zoneId": "zone-z-fake"},
            # Отказ — flat {code,message} body, не Operation.
            test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                         "pm.test('unknown zone text', () => pm.expect(pm.response.json().message).to.match(/^unknown zone id '.*'$/));"],
        ),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-NEG-NO-ANCHOR",
    title="Create subnet без ОБОИХ адресных якорей → 400 InvalidArgument, отказ называет поле",
    classes=["NEG", "VAL"],
    priority="P1",
    steps=[
        *_make_net("noanchor"),
        Step(
            name="create-no-anchor",
            method="POST",
            path="/vpc/v1/subnets",
            # Предмет: подсеть, у которой пусты ОБА якоря, отвергается синхронно.
            # Реестр решений §2: подсеть может быть одной семьи, но не «без CIDR
            # вообще как норма». Из такой подсети нельзя выделить ни один адрес и
            # ни один интерфейс, а имя в проекте она занимает.
            #
            # Два положительных контроля к этому отрицанию стоят отдельными
            # кейсами: SUB-CR-V6ONLY-NO-V4-OK (только v6) и SUB-CR-CRUD-OK
            # (только v4). Без них отказ зеленел бы и на реализации, отвергающей
            # ЛЮБУЮ подсеть, и — увереннее — на реализации, ТРЕБУЮЩЕЙ оба семейства.
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-noanchor-{{runId}}", "zoneId": "{{existingZoneId}}"},
            test_script=[
                *assert_status(400),
                # Отказ обязан НАЗЫВАТЬ ПОЛЕ: «пришло 400» истинно и при отказе по
                # зоне, по имени и по метке, поэтому само по себе оно предмета не
                # утверждает.
                *assert_field_violation("ipv4_cidr_primary"),
            ],
        ),
        # Отвергнутый запрос не оставляет следа: то же имя с законным якорем
        # проходит. То есть отказ синхронный, ДО записи, а не откат после неё.
        Step(
            name="same-name-with-anchor-succeeds",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-noanchor-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.210.0.0/24"},
            test_script=[*assert_status(200), *assert_operation_envelope(),
                         *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE",
             path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


CASES.append(Case(
    id="SUB-CR-V6ONLY-NO-V4-OK",
    # Прежний идентификатор — SUB-CR-NO-CIDR-OK, и его предмет («подсеть без ни
    # одного адресного якоря законна») СНЯТ: реестр решений §2 называет границу
    # прямо — пустым может быть ОДИН якорь из двух, но не оба. Кейс не удалён, а
    # переработан, потому что его вторая половина остаётся ценной и ничем больше
    # не покрыта: у односемейной подсети набор ЧУЖОГО семейства пуст, и первый
    # блок этого семейства добавляется отдельным глаголом.
    title="Create v6-only subnet → success; набор IPv4-диапазонов пуст; addCidrBlocks добавляет первый",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_make_net("nocidr"),
        Step(
            name="create-v6-only",
            method="POST",
            path="/vpc/v1/subnets",
            # Якорь ТОЛЬКО v6: тогда «набор IPv4 пуст» — проверенное утверждение о
            # законной подсети, а не следствие снятого поведения.
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-v6only-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv6CidrPrimary": "fd00::/64"},
            test_script=[*assert_status(200), *assert_operation_envelope(),
                         *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-empty-cidr",
            method="GET",
            path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('no ipv4 ranges', () => pm.expect(" + SUBNET_V4_CIDRS + " || []).to.have.lengthOf(0));"],
        )),
        Step(
            name="add-cidr",
            method="POST",
            path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": ["10.77.0.0/24"]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")],
        ),
        poll_operation_until_done(),
        Step(
            name="get-has-cidr",
            method="GET",
            path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('cidr now present', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.include('10.77.0.0/24'));"],
        ),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-V6-OK",
    title="Create subnet с ipv6CidrPrimary → echoed back в GET",
    classes=["CRUD"],
    priority="P2",
    steps=[
        *_make_net("v6"),
        Step(
            name="create-v6",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-v6-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.78.0.0/24", "ipv6CidrPrimary": "fd00:dead:beef::/64"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-v6",
            method="GET",
            path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('v6 cidr echoed', () => pm.expect(" + SUBNET_V6_CIDRS + " || []).to.include('fd00:dead:beef::/64'));"],
        )),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # VPC-1 F7: CIDR is immutable via Update — the primary anchor never changes;
    # additional ranges move only through :add/:remove-cidr-blocks. A CIDR field in
    # update_mask is rejected SYNC by the Update immutable-switch (subnet/update.go
    # covers ipv4/ipv6_cidr_primary + _blocks). (Pre-redesign this was a soft no-op
    # 200; the redesign made it a hard immutable-reject.) NB: proto3 FieldMask paths
    # are lowerCamelCase in JSON (protojson converts to snake internally) — a
    # snake_case mask value fails FieldMask parse before reaching the handler.
    id="SUB-UPD-V6-NOOP",
    title="Update mask=ipv6CidrPrimary → sync 400 'ipv6_cidr_primary is immutable after Subnet.Create' (VPC-1 F7)",
    classes=["STATE", "VAL", "NEG"],
    priority="P2",
    steps=[
        *_make_net("v6upd"),
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-v6upd-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.79.0.0/24"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="patch-v6",
            method="PATCH",
            path="/vpc/v1/subnets/{{subId}}",
            # Mask-only: `ipv6_cidr_primary` is not a field of UpdateSubnetRequest,
            # so naming it in update_mask is the whole probe — a value alongside it
            # would be dropped by the edge and could not change the answer.
            body={"updateMask": "ipv6CidrPrimary"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('verbatim immutable text', () => pm.expect(pm.response.json().message).to.eql('ipv6_cidr_primary is immutable after Subnet.Create'));"],
        )),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # Address с explicit internal_ipv4 в CIDR-less подсеть → FailedPrecondition
    # "subnet <id> has no IPv4 CIDR" (guard в address.go).
    id="SUB-CR-NEG-ADDR-INTO-V6ONLY",
    # Прежний идентификатор назывался CIDRLESS: фикстура строилась подсетью без
    # обоих якорей, а такая подсеть больше не создаётся. Предмет кейса — «в
    # подсети нет IPv4-плана, значит v4-адрес выделить некуда» — сохранён ПОЛНОСТЬЮ
    # и выражен законной v6-only подсетью.
    title="Address.Create internal_ipv4 в v6-only subnet → 400 FailedPrecondition",
    classes=["NEG", "CONF"],
    priority="P1",
    steps=[
        *_make_net("addrcl"),
        Step(name="create-v6only-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-addrcl-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv6CidrPrimary": "fd00::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="addr-into-cidrless", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "addr-cl-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}", "address": "10.5.5.5"}},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('mentions no IPv4 CIDR', () => pm.expect(pm.response.json().message).to.include('no IPv4 CIDR'));"]),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-VAL-CIDR-HOSTBITS",
    title="Create с host-bits в CIDR (10.0.0.5/24) → InvalidArgument",
    classes=["VAL"],
    priority="P0",
    steps=[
        *_make_net("hb"),
        Step(
            name="create-hostbits",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-hb-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.0.0.5/24"},
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")],
        ),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-NEG-NETWORK-NOT-FOUND",
    title="Create в несуществующей network → sync 404 NOT_FOUND",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(
            name="create",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{garbageVpcId}}",
                  "name": "sub-nf-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.204.0.0/24"},
            # Текст владельца целиком (services/vpc/.../api/subnet/create.go), а не
            # слово «network»: под ним проходили 27 разных отказов vpc (#1520).
            test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                         *assert_refusal_message("Network {{garbageVpcId}} not found")],
        ),
    ],
))

CASES.append(Case(
    id="SUB-CR-NEG-CIDR-OVERLAP",
    title="Create двух subnet с пересекающимися CIDR → второй sync 400 FAILED_PRECONDITION",
    classes=["NEG"],
    priority="P0",
    steps=[
        *_make_net("ov"),
        Step(
            name="create-first",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-ov1-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.50.0.0/16"},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId1")],
        ),
        poll_operation_until_done(),
        Step(
            name="create-second-overlap",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-ov2-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": "10.50.5.0/24"},  # overlaps with /16
            test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                         "pm.test('overlap text', () => pm.expect(pm.response.json().message).to.eql('Subnet CIDRs can not overlap'));"],
        ),
        retry_until_authorized(Step(name="cleanup-sub1", method="DELETE", path="/vpc/v1/subnets/{{subId1}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# ---------------------------------------------------------------------------
# SUB-GET / SUB-LST
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-GET-NEG-NOT-FOUND",
    title="Get malformed id → 400 InvalidArgument 'invalid subnet id'",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(
            name="get-garbage",
            method="GET",
            path="/vpc/v1/subnets/{{garbageId}}",
            # malformed id (нет известного 3-char префикса) → 400 InvalidArgument
            # "invalid subnet id '<X>'". Проверка family-agnostic.
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
            ],
        ),
    ],
))

CASES.append(Case(
    id="SUB-LST-CRUD-OK",
    title="List subnets в project → 200",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(
            name="list",
            method="GET",
            path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=10",
            test_script=[*assert_status(200),
                         "pm.test('subnets array', () => pm.expect(pm.response.json().subnets || []).to.be.an('array'));"],
        ),
    ],
))

CASES.append(Case(
    id="SUB-LST-VAL-PROJECT-REQUIRED",
    title="List без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped)",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        # Unscoped list — gateway authz-first 403 (no path) ЛИБО backend 400. Оба =
        # «отклонено». См. assert_unscoped_rejected (gen.py).
        Step(name="list-no-project", method="GET", path="/vpc/v1/subnets",
             test_script=[*assert_unscoped_rejected()]),
    ],
))

# ---------------------------------------------------------------------------
# SUB-UPD / SUB-DEL / SUB-MV
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-UPD-AUTHZ-NF-SYNC",
    title="Update несуществующего → sync 404 от AuthZ-Get",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH", path="/vpc/v1/subnets/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SUB-UPD-STATE-IMMUTABLE-CIDR",
    title="Update mask=ipv4CidrPrimary → sync 400 'ipv4_cidr_primary is immutable after Subnet.Create' (VPC-1 F7)",
    classes=["STATE", "VAL", "NEG"],
    priority="P1",
    steps=[
        *_make_net("im"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-im-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.30.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        # VPC-1 F7: CIDR is immutable via Update — a CIDR field in update_mask is
        # rejected sync by the Update immutable-switch (subnet/update.go). The primary
        # anchor never changes; additional ranges move via :add/:remove-cidr-blocks.
        # proto3 FieldMask paths are lowerCamelCase in JSON (protojson → snake).
        retry_until_authorized(Step(name="patch-cidr-via-mask", method="PATCH", path="/vpc/v1/subnets/{{subId}}",
             # Mask-only, same reason as SUB-UPD-V6-NOOP: the anchor is absent from
             # UpdateSubnetRequest, so the mask carries the assertion by itself.
             body={"updateMask": "ipv4CidrPrimary"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('verbatim immutable text', () => pm.expect(pm.response.json().message).to.eql('ipv4_cidr_primary is immutable after Subnet.Create'));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-DEL-AUTHZ-NF-SYNC",
    title="Delete несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/subnets/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

# ---------------------------------------------------------------------------
# SUB-ACB / SUB-RCB / SUB-REL / SUB-LUA
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-ACB-CRUD-OK",
    title="AddCidrBlocks → новый блок виден в GET",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_make_net("acb"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-acb-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.60.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-cidr", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.60.1.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="verify", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200),
                          "pm.test('has both cidrs', () => { const c = " + SUBNET_V4_CIDRS + "; pm.expect(c).to.include('10.60.0.0/24'); pm.expect(c).to.include('10.60.1.0/24'); });"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-LUA-CRUD-OK",
    title="ListUsedAddresses на пустой subnet → empty",
    classes=["CRUD"],
    priority="P2",
    steps=[
        *_make_net("lua"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-lua-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.205.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="list-used", method="GET", path="/vpc/v1/subnets/{{subId}}/addresses",
             test_script=[*assert_status(200),
                          "pm.test('addresses array', () => pm.expect(pm.response.json().usedAddresses || pm.response.json().addresses || []).to.be.an('array'));"]),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-LOP-CRUD-OK",
    title="ListOperations возвращает create-op",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_make_net("lop"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-lop-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.206.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.id", "createOpId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-ops", method="GET", path="/vpc/v1/subnets/{{subId}}/operations",
             test_script=[*assert_status(200),
                          "pm.test('at least 1 op', () => pm.expect((pm.response.json().operations || []).length).to.be.at.least(1));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# Расширение: BVA + CONF + STATE + AUTHZ-Move + Move-CRUD
CASES.extend(crud_list_bva_block("SUB", "/vpc/v1/subnets"))
CASES.append(conf_not_found_text("SUB", "/vpc/v1/subnets", "Subnet"))
CASES.append(state_update_unknown_mask("SUB", "/vpc/v1/subnets"))

CASES.append(Case(
    id="SUB-UPD-CRUD-OK",
    title="Update Subnet description",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("upd"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-upd-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.207.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path="/vpc/v1/subnets/{{subId}}",
             body={"updateMask": "description", "description": "upd-newman"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-RCB-CRUD-OK",
    title="RemoveCidrBlocks: убрать дополнительный CIDR",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("rcb"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-rcb-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.140.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-cidr", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.140.1.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="remove-cidr", method="POST", path="/vpc/v1/subnets/{{subId}}:remove-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.140.1.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # verify (GET после remove-cidr) — read-your-writes на СВОЁМ subnet: под параллелью
        # remove-cidr Update ре-регистрирует ресурс (register-intent → forward→full re-materialize)
        # → краткое v_get окно → GET 404 (флейк). retry_until_authorized(404) доводит до 200
        # (op уже done → cidr снят), затем assert. Sibling verify (add-cidr выше) уже обёрнут.
        retry_until_authorized(Step(name="verify", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200),
                          "pm.test('cidr removed', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.not.include('10.140.1.0/24'));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# Дополнение: STATE immutable project + VAL move-no-dest + BVA pagesize=1
CASES.append(state_immutable_project("SUB", "/vpc/v1/subnets"))
CASES.append(list_pagesize_1_bva("SUB", "/vpc/v1/subnets"))

# STATE для Subnet ACB/RCB/REL — пометить existing CRUD кейсы класса STATE
# через дополнительные state-сценарии
CASES.append(Case(
    id="SUB-ACB-STATE-DISJOINT-CIDRS",
    title="AddCidrBlocks с пересекающимися CIDR в одном запросе → InvalidArgument",
    classes=["STATE", "VAL", "CONF"], priority="P1",
    steps=[
        *_make_net("acbdj"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-acbdj-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.150.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="add-overlapping", method="POST",
             path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.151.0.0/24", "10.151.0.5/30"]},
             test_script=[
                 "pm.test('rejected (400 sync)', () => pm.expect(pm.response.code).to.eql(400));",
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CR-CONF-NET-NF-TEXT",
    title="Create subnet в garbage network → точный текст 'Network ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create-bad-net", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{garbageVpcId}}",
                   "name": "sub-confnf-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.208.0.0/24"},
             test_script=[
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim Network ... not found', () => pm.expect(pm.response.json().message).to.match(/^Network .* not found$/));",
             ]),
    ],
))

CASES.append(Case(
    id="SUB-CR-NEG-DUP-NAME",
    title="Subnet duplicate name в project → ALREADY_EXISTS (migration 0002 UNIQUE)",
    classes=["NEG", "CONF", "CONC"], priority="P0",
    steps=[
        *_make_net("dup"),
        Step(name="create-first", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-dup-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.180.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId1")]),
        poll_operation_until_done(),
        Step(name="create-dup", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-dup-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.181.0.0/24"},  # другой CIDR — дубль только по name
             # Текст владельца дословно (services/vpc/.../api/subnet/create.go), а не
             # общая часть тона: под `include('already exists')` проходил отказ ЛЮБОГО
             # ресурса vpc (#1520).
             test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                          *assert_refusal_message("Subnet with name sub-dup-{{runId}} already exists")]),
        Step(name="cleanup-1", method="DELETE", path="/vpc/v1/subnets/{{subId1}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# === Финальное добивание ===
CASES.append(Case(
    id="SUB-DEL-CRUD-OK",
    title="Subnet Delete happy path",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("delok"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-delok-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.209.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="delete-happy", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get-after-del", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(404)]),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-ACB-NEG-OVERLAP",
    title="AddCidrBlocks с CIDR пересекающимся с existing → InvalidArgument/FailedPrecondition",
    classes=["NEG"], priority="P1",
    steps=[
        *_make_net("acbov"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-acbov-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.210.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-overlap-self", method="POST",
             path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.210.0.0/24"]},  # overlaps with existing
             # AddCidrBlocks выполняет работу СИНХРОННО внутри вызова
             # (operations.RunSync), но отказ наружу кодом ответа не поднимается —
             # он записывается В САМУ операцию, которая возвращается уже
             # завершённой. Значит исход один: 200 с операцией, несущей ошибку.
             # Прежнее `oneOf([400, 200])` под заголовком «rejected» проходило и
             # при полном отсутствии отказа, и ни один другой шаг отказ не смотрел.
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('operation completed inline', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "pm.test('overlapping CIDR refused (FailedPrecondition)', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(9));",
                 "pm.test('refusal names the overlap', () => pm.expect((j.error && j.error.message) || '').to.match(/overlap/i));",
             ])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-RCB-NEG-NF",
    title="RemoveCidrBlocks с несуществующим CIDR → InvalidArgument",
    classes=["NEG", "VAL", "STATE"], priority="P1",
    steps=[
        *_make_net("rcbnf"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-rcbnf-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.220.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="rcb-nonexistent", method="POST",
             path="/vpc/v1/subnets/{{subId}}:remove-cidr-blocks",
             body={"ipv4CidrBlocks": ["192.168.99.0/24"]},  # never was in subnet
             # Тот же порядок: работа синхронна, отказ живёт в возвращённой
             # операции. Удаление блока, которого в подсети нет, — отказ, а не
             # идемпотентный no-op (remove_cidr_blocks.go: removedV4 != len(v4)).
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "pm.test('operation completed inline', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "pm.test('absent CIDR refused (FailedPrecondition)', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(9));",
                 "pm.test('refusal names the missing block', () => pm.expect((j.error && j.error.message) || '').to.contain('not found in subnet'));",
             ])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-LOP-NEG-PARENT-NF",
    title="ListOperations несуществующего subnet → 404 или 200 пустой",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="lop-nx", method="GET", path="/vpc/v1/subnets/{{garbageVpcId}}/operations",
             test_script=["pm.test('200/403/404', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
))

CASES.append(Case(
    id="SUB-LUA-NEG-PARENT-NF",
    title="ListUsedAddresses несуществующего subnet → 404 или 200",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="lua-nx", method="GET", path="/vpc/v1/subnets/{{garbageVpcId}}/addresses",
             test_script=["pm.test('200/403/404', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
))

CASES.append(Case(
    id="SUB-DEL-CONF-NF-TEXT",
    title="Delete несуществующего Subnet → точный текст 'Subnet ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/subnets/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SUB-UPD-CONF-NF-TEXT",
    title="Update несуществующего Subnet → точный текст 'Subnet ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="upd-nx", method="PATCH", path="/vpc/v1/subnets/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="SUB-RCB-CONF-STATE",
    title="STATE для RemoveCidrBlocks: проверка инварианта после операции",
    classes=["STATE"], priority="P1",
    steps=[
        *_make_net("rcbstate"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-rcbst-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.230.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-then-remove", method="POST",
             path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.230.1.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="remove-it", method="POST",
             path="/vpc/v1/subnets/{{subId}}:remove-cidr-blocks",
             body={"ipv4CidrBlocks": ["10.230.1.0/24"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Bounded read-your-writes retry: RemoveCidrBlocks вернул Operation.done с response=Subnet
        # (subnet DURABLE, primary CIDR kept), но первый пост-мутационный Get своей же строки может
        # кратко отдать 404 на read-consistency окне (та же eventual-consistency, что owner-tuple lag).
        # retry_until_authorized ретраит SELF на 403/404 и затем гоняет реальные ассерты один раз
        # (genuine non-converging 404 после бюджета всё равно FAIL — не маскируется).
        retry_until_authorized(Step(name="verify-state", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200),
                          "pm.test('removed cidr gone', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.not.include('10.230.1.0/24'));",
                          "pm.test('primary cidr kept', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.include('10.230.0.0/24'));"])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

# Exhaustive ECP/BVA: используем shared network на каждый кейс
# (более дорого, но изолировано). Альтернатива — общий network через preflight item.
# Делаем подмножество кейсов с общим preflight сетью.

def _sub_body_extra():
    # `ipv4CidrPrimary` обязателен для ВСЕХ генерируемых кейсов: подсеть без ни
    # одного адресного якоря отвергается синхронно (реестр решений §2). Без якоря
    # здесь два исхода, и оба плохие: кейс, ждущий успеха, краснеет, а кейс,
    # ждущий отказа по СВОЕМУ предмету (имя, описание, метки), позеленел бы
    # ВАКУУМНО — получил бы 400 по чужой причине и перестал проверять то, ради
    # чего написан. Каждый такой кейс обёрнут в свою сеть, поэтому одно значение
    # на всех коллизии не даёт: непересечение диапазонов — свойство сети.
    return {
        "networkId": "{{netId}}", "zoneId": "{{existingZoneId}}",
        "ipv4CidrPrimary": "10.200.0.0/24",
    }


# Каждый ECP-кейс упакован в Case с _make_net+_cleanup_net
def _wrap_with_net(prefix, suffix, inner_case):
    """Обернуть inner_case (от ecp_*_block) в network preflight/teardown.
    Используем inner_case.id как суффикс — гарантированно уникален per case."""
    # Превратим case-id в short ASCII suffix (без дефисов и uppercase)
    uniq = inner_case.id.lower().replace("-", "")[-12:]
    return Case(
        id=inner_case.id,
        title=inner_case.title,
        classes=inner_case.classes,
        priority=inner_case.priority,
        steps=[
            *_make_net(uniq),
            *inner_case.steps,
            _cleanup_net_lenient(),
        ],
    )


for c in ecp_name_block("SUB", "/vpc/v1/subnets", _sub_body_extra()):
    CASES.append(_wrap_with_net("SUB", "ecp-n", c))
for c in ecp_description_block("SUB", "/vpc/v1/subnets", _sub_body_extra()):
    CASES.append(_wrap_with_net("SUB", "ecp-d", c))
for c in ecp_labels_block("SUB", "/vpc/v1/subnets", _sub_body_extra()):
    CASES.append(_wrap_with_net("SUB", "ecp-l", c))
CASES.extend(updatemask_decision_table("SUB", "/vpc/v1/subnets"))
CASES.extend(filter_syntax_block("SUB", "/vpc/v1/subnets"))
CASES.append(pagination_roundtrip("SUB", "/vpc/v1/subnets"))

# v7: update-per-field wrap'ed в network
for c in update_happy_per_field("SUB", "/vpc/v1/subnets", "/vpc/v1/subnets",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.210.0.0/24"}):
    CASES.append(_wrap_with_net("SUB", "v7", c))

CASES.extend(perf_baseline_block("SUB", "/vpc/v1/subnets"))
CASES.extend(verbatim_text_pack("SUB", "Subnet", "/vpc/v1/subnets"))
CASES.extend(authz_caller_headers_block("SUB", "/vpc/v1/subnets"))

# move-self для subnet
# v8 subnet
CASES.append(_wrap_with_net("SUB", "v8m",
    update_happy_multi_field("SUB", "/vpc/v1/subnets", "/vpc/v1/subnets",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
         "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.211.0.0/24"})))
CASES.append(_wrap_with_net("SUB", "v8f",
    list_filter_match_block("SUB", "/vpc/v1/subnets",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
         "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.212.0.0/24"})))
for c in neg_invalid_types_block("SUB", "/vpc/v1/subnets",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.213.0.0/24"}):
    CASES.append(_wrap_with_net("SUB", "v8nt", c))
CASES.extend(http_method_not_allowed_block("SUB", "/vpc/v1/subnets"))
CASES.extend(malformed_body_block("SUB", "/vpc/v1/subnets"))

# dup-name для Subnet покрыт hand-written SUB-CR-NEG-DUP-NAME (использует РАЗНЫЕ CIDR
# у обеих подсетей). Generated alreadyexists_dup_name_for тут не применим: он создает
# две подсети с одинаковым телом (тот же CIDR) → overlap проверяется раньше
# name-uniqueness и возвращается FAILED_PRECONDITION "Subnet CIDRs can not overlap",
# а не ALREADY_EXISTS.
for c in update_mask_partial_block("SUB", "/vpc/v1/subnets", "/vpc/v1/subnets",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.214.0.0/24"}):
    CASES.append(_wrap_with_net("SUB", "v9p", c))
CASES.append(_wrap_with_net("SUB", "v9pf",
    perf_baseline_get_block("SUB", "/vpc/v1/subnets",
        {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
         "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.215.0.0/24"})))
CASES.extend(list_total_size_check_block("SUB", "/vpc/v1/subnets"))

# SUB-CR-DHCP-IGNORED-VPC143 — retired 2026-07-28.
#
# The case sent `dhcpOptions` on Subnet.Create and asserted 200 with the reading
# "the removed field is silently ignored, not validated, not rejected". What it
# actually pinned was the edge's tolerance for keys the request message does not
# declare — a concession made for the console, not a promise of this API; Kachō's
# own convention is the opposite (a request field the service does not read has
# three lawful fates, and "accept and drop" is not among them). Keeping the case
# turned that concession into a guaranteed contract, and it did so through a
# subnet fixture, where nothing about subnets was under test.
#
# The part of VPC-1-43 that IS a product statement — DHCP knobs are gone from the
# Subnet projection — is asserted by SUB-CR-V1-DHCP-DROPPED (vpc1.py) reading a
# real subnet and requiring the property to be absent, and by the unit-level lock
# in subnet dhcp_removed_test.go. Neither needs a body key nobody reads.

# CIDR prefix boundary: /28 принимается; /29, /30, /31 → 400
# "Illegal argument Invalid network prefix /N".
CASES.append(_wrap_with_net("SUB", "v10cidr28",
    Case(
        id="SUB-CR-BVA-CIDR-28",
        title="Create subnet с prefix /28 → 200 (минимальный размер)",
        classes=["BVA", "CRUD"], priority="P2",
        steps=[
            Step(name="cr-prefix-28", method="POST", path="/vpc/v1/subnets",
                 body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                       "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.255.0.0/28",
                       "name": "sub-cidr-28-{{runId}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
            poll_operation_until_done(),
            retry_until_authorized(Step(name="cleanup-28", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
        ],
    )))
for _n in ("29", "30", "31"):
    CASES.append(_wrap_with_net("SUB", "v10cidr" + _n,
        Case(
            id=f"SUB-CR-BVA-CIDR-{_n}",
            title=f"Create subnet с prefix /{_n} → 400 'Illegal argument Invalid network prefix /{_n}'",
            classes=["BVA", "VAL", "NEG"], priority="P2",
            steps=[
                Step(name=f"cr-prefix-{_n}", method="POST", path="/vpc/v1/subnets",
                     body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                           "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": f"10.255.0.0/{_n}",
                           "name": f"sub-cidr-{_n}-{{{{runId}}}}"},
                     test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                                  f"pm.test('verbatim text', () => pm.expect(pm.response.json().message).to.eql('Illegal argument Invalid network prefix /{_n}'));"]),
            ],
        )))

# === Delete Subnet с зависимыми Address ===

CASES.append(Case(
    id="SUB-DEL-NEG-HAS-ADDRESSES",
    title="Delete Subnet с internal Address → FailedPrecondition (FK RESTRICT)",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        *_make_net("hasad"),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-hasad-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.251.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-internal-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-hasad-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub-blocked", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[
                 # Delete subnet с internal Address → sync FAILED_PRECONDITION
                 # "Subnet has allocated internal addresses".
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('verbatim text', () => pm.expect(pm.response.json().message).to.eql('Subnet has allocated internal addresses'));",
             ])),
        # cleanup
        Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # v6-counterpart of SUB-DEL-NEG-HAS-ADDRESSES: внутренний IPv6-адрес,
    # выделенный в подсети, блокирует ее удаление точно так же, как v4. AddressesBySubnet
    # покрывает internal_ipv6, а generated-колонка addresses.internal_subnet_id выводится
    # из v4 ИЛИ v6.
    id="SUB-DEL-NEG-HAS-V6-ADDRESS",
    title="Delete Subnet с internal IPv6 Address → FailedPrecondition",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        *_make_net("hasv6ad"),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-hasv6ad-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.249.0.0/24", "ipv6CidrPrimary": "fd34:5678:9abc::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-internal-v6-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-hasv6ad-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub-blocked", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[
                 # Внутренний v6-адрес блокирует подсеть так же, как v4 →
                 # sync FAILED_PRECONDITION "Subnet has allocated internal addresses".
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('mentions internal address', () => pm.expect(pm.response.json().message).to.include('internal address'));",
             ])),
        # cleanup: delete the address → subnet delete now succeeds
        Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-sub-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('subnet delete op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # Полная RESTRICT-цепочка Network → Subnet → Address → NIC: каждый родитель
    # жестко блокируется, пока существует ребенок; удаление снизу вверх.
    id="NET-SUBNET-ADDR-NIC-DELETE-CHAIN",
    title="RESTRICT chain: network/subnet/address все блокируются детьми; удаление снизу вверх",
    classes=["NEG", "CONF", "STATE"], priority="P0",
    steps=[
        *_make_net("chain"),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-chain-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.248.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-addr", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-chain-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        Step(name="cr-nic", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": "nic-chain-{{runId}}", "v4AddressIds": ["{{addrId}}"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        # 5. delete network → blocked (not empty)
        Step(name="del-net-blocked", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             # Текст владельца (services/vpc/.../api/network/delete.go) — ВХОЖДЕНИЕМ:
             # он называет ПЕРЕЧЕНЬ помех (`(subnets: 1, …)`), а перечень зависит от
             # того, что успело завестись к моменту шага. Утверждается всё, что от
             # входа не зависит; слово «not empty» под собой пропускало отказ пула
             # адресов и отказ группы (#1520).
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          *assert_refusal_message_contains("Network {{netId}} is not empty (")]),
        # 6. delete subnet → blocked: address check runs first (not the NIC)
        retry_until_authorized(Step(name="del-sub-blocked", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('mentions internal address', () => pm.expect(pm.response.json().message).to.include('internal address'));"])),
        # 7. delete address → blocked by the NIC
        retry_until_authorized(Step(name="del-addr-blocked", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('mentions network interface', () => pm.expect(pm.response.json().message).to.include('network interface'));"])),
        # 8. cleanup bottom-up: NIC → address → subnet → network, all succeed
        retry_until_authorized(Step(name="del-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-net-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('network delete op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
    ],
))

CASES.append(Case(
    # NIC→Subnet FK — ON DELETE RESTRICT: NIC жестко блокирует свою подсеть, даже без
    # адресов. Удалять снизу вверх: NIC → Address → Subnet → Network.
    id="SUB-DEL-NEG-HAS-NIC",
    title="Delete Subnet с NIC (без address) → sync FailedPrecondition; после delete NIC — OK",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        *_make_net("hasnic"),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-hasnic-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.253.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="cr-nic-no-addr", method="POST", path="/vpc/v1/networkInterfaces",
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}", "name": "nic-hasnic-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub-blocked", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[
                 *assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                 "pm.test('mentions network interface', () => pm.expect(pm.response.json().message).to.include('network interface'));",
             ])),
        # delete NIC → subnet delete now succeeds
        retry_until_authorized(Step(name="del-nic", method="DELETE", path="/vpc/v1/networkInterfaces/{{nicId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-sub-deleted", method="GET", path="/operations/{{opId}}",
             test_script=["const j = pm.response.json();",
                          "pm.test('subnet delete op done no error', () => pm.expect(j.done && !j.error).to.eql(true));"]),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-DEL-CRUD-EMPTY-OK",
    title="Delete Subnet без зависимостей → OK",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("delempty"),
        Step(name="cr-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-delempty-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.216.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-empty-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="assert-success", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('done with no error', () => pm.expect(j.done && !j.error).to.eql(true));",
             ]),
        _cleanup_net(),
    ],
))

# === Required-field matrix + Immutable matrix для Subnet ===
# Subnet нужен parent network — wrap в _wrap_with_net
for c in required_fields_matrix("SUB", "/vpc/v1/subnets",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "name": "sub-req-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.217.0.0/24"},
    # `v4CidrBlocks` used to sit in this list, which produced SUB-CR-VAL-REQ-V4CIDRBLOCKS —
    # "omit the required field and expect a rejection" for a field CreateSubnetRequest does
    # not have (retired VPC-1 F7) and never required. Omitting a key the edge already drops
    # changes nothing about the request, so the case could only ever pass. Dropped, along
    # with the matching literal in the body above.
    # `name` снят по тому же основанию, что и `v4CidrBlocks` выше, только основание
    # другое: поле в сообщении ЕСТЬ, но контракт допускает его ПУСТЫМ
    # (`services/vpc/internal/domain/types.go`, RcNameVPC — «empty allowed», залочено
    # `types_test.go` кейсом {"empty allowed", "", false}), а сгенерированный сосед
    # `SUB-CR-VAL-NAME-NULL` прямо утверждает `name=null → 200`. «Убери name → отказ»
    # зеленел только потому, что принимал успех.
    ["projectId", "networkId", "zoneId"]):
    CASES.append(_wrap_with_net("SUB", "req", c))
# Имена — КОНТРАКТНЫЕ. Прежде здесь стояли `v4_cidr_blocks` / `v6_cidr_blocks` —
# поля, которого у подсети нет ни одним сообщением (снято VPC-1 F7, а с kacho#1628
# имя закрыто `reserved` явно). Матрица утверждает ДОСЛОВНЫЙ текст «<field> is
# immutable», а снятое имя такого текста не производит: оно не доходит до
# immutable-switch и отвергается generic-отказом маски. Кейс зеленел лишь потому,
# что матрица допускает «другую 4xx», — то есть проверял не то, чем назван.
CASES.extend(immutable_fields_matrix("SUB", "/vpc/v1/subnets",
    ["project_id", "network_id", "zone_id", "ipv4_cidr_blocks", "ipv6_cidr_blocks"]))

# === Subnet CIDR expand/shrink pack — каждый кейс в СВОЕЙ сцене ===
# Обёртка применяется к КАЖДОМУ кейсу набора: своя сеть, своя подсеть с
# единственным primary CIDR 10.180.0.0/24, свой teardown. Накопления блоков
# между кейсами нет — кейс, которому нужно предшествующее состояние, создаёт его
# сам (см. SUB-ACB-NEG-OVERLAP-SELF). Прежняя редакция этой строки обещала
# обратное («накапливаем 4 cidr, потом гоняем 8 кейсов»), и на неё опиралась
# проверка пересечения, у предпосылки которой не было производителя.
def _subnet_cidr_setup_teardown(case):
    return Case(
        id=case.id, title=case.title, classes=case.classes, priority=case.priority,
        steps=[
            *_make_net("cidrexp"),
            Step(name="setup-sub", method="POST", path="/vpc/v1/subnets",
                 body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                       "name": "sub-cidrexp-{{runId}}", "zoneId": "{{existingZoneId}}",
                       "ipv4CidrPrimary": "10.180.0.0/24"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.subnetId", "addedSubId")]),
            poll_operation_until_done(),
            # Settle-barrier: block until the fresh subnet's read-tuple is visible
            # (owner/viewer FGA tuple materialises eventually after opgate removal) so
            # the pack's own reads of {{addedSubId}} (verify-*/state-*) never race the
            # visibility window with a hide-existence 404. retry_until_authorized retries
            # SELF on 403/404 then fails for real if it never converges (not masked).
            retry_until_authorized(Step(name="settle-added-sub", method="GET",
                 path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200)])),
            *case.steps,
            retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
            _cleanup_net(),
        ],
    )

for case in subnet_cidr_expand_shrink_pack():
    CASES.append(_subnet_cidr_setup_teardown(case))

# v14 — pairwise + security (parent net wrap)
for c in pairwise_subnet_pack():
    CASES.append(_wrap_with_net("SUB", "pw", c))
for c in security_injection_block("SUB", "/vpc/v1/subnets", "/vpc/v1/subnets",
    {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
     "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.218.0.0/24"}):
    CASES.append(_wrap_with_net("SUB", "sec", c))

# ---------------------------------------------------------------------------
# IPv6 CIDR add/remove on subnet verbs
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-CIDR-ADD-V6-OK",
    title="AddCidrBlocks с ipv6CidrBlocks → IPv6-блок виден в GET",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("acb6"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-acb6-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.220.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-cidr-v6", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv6CidrBlocks": ["fd12:3456:789a::/64"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="verify", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200),
                          "pm.test('v6 cidr present', () => pm.expect(" + SUBNET_V6_CIDRS + ").to.include('fd12:3456:789a::/64'));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    # VPC-1 F7: ipv6CidrPrimary (blocks[0]) is an immutable anchor — Remove of the
    # primary is rejected ("ipv6_cidr_primary is immutable after Subnet.Create").
    # Only ADDITIONAL ranges can be removed. So create v4+v6 primaries, ADD an extra
    # v6 range, then REMOVE that additional range; primary must remain.
    id="SUB-CIDR-REMOVE-V6-OK",
    title="RemoveCidrBlocks убирает дополнительный IPv6-блок (primary-anchor сохранён)",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_net("rcb6"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-rcb6-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.221.0.0/24", "ipv6CidrPrimary": "fd12:3456:789b::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="add-cidr-v6", method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv6CidrBlocks": ["fd12:3456:789c::/64"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="remove-cidr-v6", method="POST", path="/vpc/v1/subnets/{{subId}}:remove-cidr-blocks",
             body={"ipv6CidrBlocks": ["fd12:3456:789c::/64"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="verify", method="GET", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200),
                          "pm.test('additional v6 cidr removed', () => pm.expect(" + SUBNET_V6_CIDRS + " || []).to.not.include('fd12:3456:789c::/64'));",
                          "pm.test('v6 primary anchor kept', () => pm.expect(" + SUBNET_V6_CIDRS + " || []).to.include('fd12:3456:789b::/64'));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))

CASES.append(Case(
    id="SUB-CIDR-ADD-V6-NEG-HOSTBITS",
    title="AddCidrBlocks v6 с ненулевыми host-bits → InvalidArgument (sync 400)",
    classes=["NEG", "VAL"], priority="P1",
    steps=[
        *_make_net("acb6hb"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-acb6hb-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.222.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="add-cidr-v6-hostbits", method="POST",
             path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
             body={"ipv6CidrBlocks": ["fd12:3456:789a::1/64"]},
             test_script=[
                 "pm.test('rejected (400 sync)', () => pm.expect(pm.response.code).to.eql(400));",
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
             ]),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


# ---------------------------------------------------------------------------
# Subnet v6 overlap / util / rollback
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="SUB-CR-NEG-DUP-CIDR-EXACT",
    title="Create Subnet с CIDR, совпадающим с existing Subnet → sync FailedPrecondition (create-time overlap precheck)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        *_make_net("dupCidr"),
        Step(name="sub1", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-dup1-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.230.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId1")]),
        poll_operation_until_done(),
        # v4-overlap (exact dup within the same network) is rejected SYNCHRONOUSLY by
        # the create-time overlap precheck (FailedPrecondition), before any Operation
        # is created — not as an async op error. The DB EXCLUDE constraint is only the
        # backstop; the sync precheck fires first.
        Step(name="sub2-same-cidr", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-dup2-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.230.0.0/24"},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION")]),
        retry_until_authorized(Step(name="cleanup-sub1", method="DELETE", path="/vpc/v1/subnets/{{subId1}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="SUB-CR-NEG-V6-OVERLAP",
    title="2 v6-subnet с overlapping CIDR в одной Network → 2nd FailedPrecondition (EXCLUDE subnets_no_overlap_v6)",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        *_make_net("v6ov"),
        Step(name="sub1-v6", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-v6ov1-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.231.0.0/24", "ipv6CidrPrimary": "fd12:3456:7800::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId1")]),
        poll_operation_until_done(),
        # Полный overlap по v6, разные v4 — должен fail через EXCLUDE v6.
        Step(name="sub2-v6-overlap", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-v6ov2-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.231.1.0/24", "ipv6CidrPrimary": "fd12:3456:7800::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        Step(name="poll-fail-v6", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "let _t = 0;",
                 "const _s = () => pm.sendRequest({",
                 "  url: pm.environment.get('baseUrl') + '/operations/' + pm.environment.get('opId'),",
                 "  method: 'GET',",
                 "  header: { 'Authorization': 'Bearer ' + pm.environment.get('jwtProjectAdminA1') },",
                 "}, (err, res) => {",
                 "  let j = null; try { j = res.json(); } catch (e) {}",
                 "  if (j && j.done) {",
                 "    pm.test('v6 overlap rejected', () => pm.expect(!!j.error, JSON.stringify(j)).to.eql(true));",
                 "    pm.test('FailedPrecondition (9)', () => pm.expect(j.error.code).to.eql(9));",
                 "  } else if (++_t < 8) { setTimeout(_s, 500); }",
                 "  else { pm.test('op resolved', () => pm.expect.fail('timeout')); }",
                 "});",
                 "_s();",
             ]),
        retry_until_authorized(Step(name="cleanup-sub1", method="DELETE", path="/vpc/v1/subnets/{{subId1}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="SUB-LUA-CRUD-COUNT",
    title="Allocate 3 internal Address → ListUsedAddresses возвращает все 3",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_make_net("luaCount"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-luac-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.232.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        Step(name="addr1", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "luac-a1-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId1")]),
        poll_operation_until_done(),
        Step(name="addr2", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "luac-a2-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId2")]),
        poll_operation_until_done(),
        Step(name="addr3", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "luac-a3-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId3")]),
        poll_operation_until_done(),
        # ListUsedAddresses is `GET /vpc/v1/subnets/{subnet_id}/addresses`
        # (subnet_service.proto google.api.http; gateway route table + permission
        # catalog agree — `vpc.used_addresses.listUsedAddresses`, v_list on
        # vpc_subnet). The suffix-verb form `:listUsedAddresses` this case used to
        # call is NOT a route: an unresolvable path yields no FQN, the catalog
        # lookup misses and the gateway fail-closes 403 AUTHZ_DENIED. The old
        # comment blamed a "stale CI catalog" — that was never true (the entry is
        # present in both embedded copies and every route in the table is
        # catalogued); the path was simply wrong, so the 403 branch was taken on
        # EVERY run and the `>= 3 used` invariant below never executed once.
        #
        # Asserting 200 strictly is what makes the invariant real: a regression
        # back to a misrouted path (or a genuinely missing catalog entry) is a
        # catalog-miss 403 and now FAILS instead of satisfying the case.
        retry_until_authorized(Step(name="list-used", method="GET", path="/vpc/v1/subnets/{{subId}}/addresses",
             test_script=[
                 "pm.test('ListUsedAddresses → 200 (a 403 here is a catalog miss / misrouted path, not a pass)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const used = (pm.response.json().addresses) || [];",
                 "pm.test('ListUsedAddresses returns >= 3 entries (3 allocated)', () => pm.expect(used.length, JSON.stringify(used)).to.be.at.least(3));",
                 "pm.test('every used entry carries a non-empty address', () => used.forEach(u => pm.expect(u.address, JSON.stringify(u)).to.be.a('string').and.not.empty));",
             ])),
        Step(name="del-a1", method="DELETE", path="/vpc/v1/addresses/{{addrId1}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-a2", method="DELETE", path="/vpc/v1/addresses/{{addrId2}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-a3", method="DELETE", path="/vpc/v1/addresses/{{addrId3}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="SUB-LUA-STATE-FRAGMENT",
    title="Allocate 5 → delete middle 3 → ListUsedAddresses возвращает только оставшиеся 2 (фрагментация)",
    classes=["STATE"], priority="P2",
    steps=[
        *_make_net("luaFrag"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-luaf-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.233.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        # Allocate 5 sequential.
        *[s for i in range(5) for s in [
            Step(name=f"addr-{i}", method="POST", path="/vpc/v1/addresses",
                 body={"projectId": "{{_suiteProjectId}}", "name": f"luaf-{i}-{{{{runId}}}}",
                       "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response("j.metadata && j.metadata.addressId", f"addrId{i}")]),
            poll_operation_until_done(),
        ]],
        # Same route correction as SUB-LUA-COUNT above. The `-1` sentinel that used
        # to be written on a 403 existed only to let `list-after` skip the
        # fragmentation delta — with the correct path there is nothing to skip, so
        # the count is always real and the delta below is always enforced.
        retry_until_authorized(Step(name="list-before-delete", method="GET", path="/vpc/v1/subnets/{{subId}}/addresses",
             test_script=[
                 "pm.test('ListUsedAddresses → 200 (a 403 here is a catalog miss / misrouted path, not a pass)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "pm.test('5 allocated are all visible before the deletes', () => pm.expect((pm.response.json().addresses || []).length, pm.response.text()).to.be.at.least(5));",
                 "pm.environment.set('countBefore', String((pm.response.json().addresses || []).length));",
             ])),
        # Delete middle 3 (indices 1, 2, 3).
        Step(name="del-1", method="DELETE", path="/vpc/v1/addresses/{{addrId1}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="del-2", method="DELETE", path="/vpc/v1/addresses/{{addrId2}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="del-3", method="DELETE", path="/vpc/v1/addresses/{{addrId3}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-after", method="GET", path="/vpc/v1/subnets/{{subId}}/addresses",
             test_script=[
                 "pm.test('ListUsedAddresses → 200 (a 403 here is a catalog miss / misrouted path, not a pass)', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                 "const before = parseInt(pm.environment.get('countBefore') || '-1', 10);",
                 "pm.test('countBefore was actually captured (list-before-delete ran)', () => pm.expect(before).to.be.at.least(0));",
                 "const after = (pm.response.json().addresses || []).length;",
                 "pm.test('count decreased by exactly 3', () => pm.expect(before - after, `before=${before} after=${after}`).to.eql(3));",
             ])),
        retry_until_authorized(Step(name="del-0", method="DELETE", path="/vpc/v1/addresses/{{addrId0}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="del-4", method="DELETE", path="/vpc/v1/addresses/{{addrId4}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="SUB-CR-NEG-ROLLBACK-NO-RESOURCE-IN-GET",
    title="Failed Subnet.Create (parent network NF) → sync NotFound, ресурс НЕ создан/visible",
    classes=["NEG", "STATE"], priority="P1",
    steps=[
        # Parent-network existence is a SYNC precheck: a well-formed but non-existent
        # networkId is rejected synchronously with NotFound ("Network <id> not found"),
        # before any Operation is created or any subnet row is inserted. There is thus
        # no Operation to poll and nothing to roll back — the resource never comes into
        # being. (`net00000000000000000` is a well-formed, never-allocated network id.)
        Step(name="create-fail", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}",
                   "networkId": "net00000000000000000",
                   "name": "sub-rollback-{{runId}}", "zoneId": "{{existingZoneId}}", "ipv4CidrPrimary": "10.219.0.0/24"},
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        # List by project must not contain a subnet with the (unique) attempted name —
        # confirms no partial/leaked resource from the failed create.
        Step(name="list-not-include", method="GET",
             path="/vpc/v1/subnets?projectId={{_suiteProjectId}}&pageSize=1000",
             test_script=[
                 *assert_status(200),
                 "const subs = pm.response.json().subnets || [];",
                 "const failedName = 'sub-rollback-' + pm.environment.get('runId');",
                 "pm.test('List не содержит subnet с именем failed Create', () => pm.expect(subs.map(s => s.name), `name=${failedName}`).to.not.include(failedName));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# Диапазоны, зарезервированные платформой (SUB-*-CONF-RESERVED-*)
# ---------------------------------------------------------------------------
#
# ПРЕДМЕТ. Часть адресного пространства обслуживает саму платформу. Подсеть
# арендатора поверх такого диапазона проходит все проверки контура и НЕ РАБОТАЕТ,
# причём симптом выглядит сетевым, а причина лежит в перекрытии. Продукт отвергает
# такой ввод синхронно, до создания Operation, и текст отказа — часть контракта
# (`services/vpc/internal/apps/kacho/api/subnet/reserved_prefixes.go`,
# `reservedOverlapMsg`). Дословность цитаты ниже держит третья проверка
# `scripts/validate-cases.py` (`scripts/contract_texts.py`): разойдётся текст в
# коде и здесь — покраснеет она, а не прогон против стенда.
#
# ЧЕМ ПРОВЕРЯЕТСЯ ПЕРЕСЕЧЕНИЕ ЗДЕСЬ. Перечень служебных диапазонов задаёт ПОСАДКА
# (`services/vpc/deploy/values.yaml`, `dataplane.reservedPrefixes`), и профиль
# объявляет своим базовым составом ровно то, что зарезервировано на ЛЮБОЙ посадке —
# link-local обоих семейств. Кейсы берут именно этот базовый диапазон: он не
# описывает физику конкретного стенда (это стандарт адресации, а не служебная
# топология) и уже объявлен в том же публичном дереве, поэтому кейс ничего не
# раскрывает сверх профиля. Замер 2026-08-13: `reservedPrefixes` объявлен ровно в
# одном файле дерева, ни один профиль зонтичного чарта его не переопределяет —
# значит на всех посадках состав один и тот же.
#
# ПОЧЕМУ СЕТЬ ОБЪЯВЛЯЕТ 169.254.0.0/15. План сети обязан покрывать ОБА префикса —
# и отвергаемый, и законный, — иначе положительный контроль упирался бы в другую
# проверку («subnet CIDR ... is not within any network CIDR block», в worker'е), и
# пара «отказ/проход» перестала бы отличать предмет от плана адресации. /15 берёт
# служебный /16 и ровно столько же адресов рядом с ним.
_RESERVED_NET_SUPERNET = "169.254.0.0/15"
# Внутри служебного диапазона.
_RESERVED_INSIDE = "169.254.10.0/24"
# Рядом, за его границей — законный ввод.
_RESERVED_LEGAL = "169.255.10.0/24"
_RESERVED_LEGAL_2 = "169.255.11.0/24"

# Текст отказа — ЦЕЛИКОМ и ДОСЛОВНО. Равенство (а не «содержит») здесь несёт
# второе утверждение: в ответе нет ничего, кроме имени слота и присланного
# значения, — то есть перечень служебных диапазонов отказом не раскрывается.
_RESERVED_MSG_V4_INSIDE_CREATE = (
    f"ipv4CidrPrimary {_RESERVED_INSIDE} overlaps an address range reserved by the platform"
)
# `:add-cidr-blocks` принимает МАССИВ, поэтому его отказ называет слот: вызывающий
# обязан понять, КОТОРЫЙ из присланных блоков негоден. У `Create` якорь один, и
# индекс там был бы утверждением о теле, которого вызывающий не отправлял.
_RESERVED_MSG_V4_INSIDE_ADD = (
    f"ipv4CidrBlocks[0] {_RESERVED_INSIDE} overlaps an address range reserved by the platform"
)


def _make_reserved_net(name_suffix):
    """Сеть, чей план ПЕРЕКРЫВАЕТ служебный диапазон и оставляет место рядом с ним."""
    return [
        Step(name="pre-create-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": f"sub-{name_suffix}-{{{{runId}}}}",
                   "ipv4CidrBlocks": [_RESERVED_NET_SUPERNET]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
    ]


CASES.append(Case(
    id="SUB-CR-CONF-RESERVED-OVERLAP",
    title="Create subnet поверх диапазона, зарезервированного платформой → sync 400 + точный текст; законный префикс той же сети проходит",
    classes=["CONF", "NEG", "VAL"], priority="P0",
    steps=[
        *_make_reserved_net("resv"),
        Step(
            name="create-over-reserved",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-resv-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": _RESERVED_INSIDE},
            test_script=[
                # Отказ СИНХРОННЫЙ: тело — flat {code,message}, а не Operation.
                # Проверять только код нельзя — 400 приходит и от формата, и от
                # размера, и от размещения; предмет утверждает текст.
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('текст отказа — дословный контракт, и в нём НЕТ ничего, кроме имени поля и "
                "присланного значения', () => pm.expect(String(pm.response.json().message || ''))"
                f".to.eql('{_RESERVED_MSG_V4_INSIDE_CREATE}'));",
                # Операция не создаётся вовсе: у синхронного отказа нет id, который
                # можно было бы опросить. Утверждение отделяет «отвергнуто до
                # записи» от «принято и упало в worker'е».
                "pm.test('операция не создана', () => pm.expect(pm.response.json().id).to.be.undefined);",
                # Полоса отказа различима МАШИННО. Без признака автомат, планирующий
                # адресацию, не может разветвиться: «префикс служебный, бери
                # следующий кандидат» неотличимо от «ввод негоден по форме». Разбор
                # прозы контракт запрещает, поэтому утверждается признак, а не текст
                # (текст утверждается выше — отдельно и дословно).
                "pm.test('полоса отказа различима машинно — ErrorInfo.reason', () => {"
                " const d = pm.response.json().details || [];"
                " pm.expect(d.some(x => x && x.reason === 'SUBNET_CIDR_RESERVED'),"
                " JSON.stringify(d)).to.eql(true); });",
            ],
        ),
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ — тот же запрос, тот же план сети, отличается ТОЛЬКО
        # префикс. Без него отказ выше зеленел бы и на реализации, отвергающей любую
        # подсеть в этой сети, и — что тоньше — на реализации, отвергающей всё, что
        # лежит рядом со служебным диапазоном (касание границ пересечением не является).
        Step(
            name="legal-prefix-passes",
            method="POST",
            path="/vpc/v1/subnets",
            body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                  "name": "sub-resv-ok-{{runId}}", "zoneId": "{{existingZoneId}}",
                  "ipv4CidrPrimary": _RESERVED_LEGAL},
            test_script=[*assert_status(200), *assert_operation_envelope(),
                         *save_from_response("j.id", "opId"),
                         *save_from_response("j.metadata && j.metadata.subnetId", "subId")],
        ),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="get-confirms-legal",
            method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('законный префикс записан', () => pm.expect("
                         + SUBNET_V4_CIDRS + f").to.include('{_RESERVED_LEGAL}'));"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


CASES.append(Case(
    id="SUB-ACB-CONF-RESERVED-OVERLAP",
    title="AddCidrBlocks служебного диапазона к уже созданной подсети → sync 400 + точный текст; законный блок тем же глаголом проходит",
    classes=["CONF", "NEG", "VAL"], priority="P0",
    steps=[
        # Второй и последний глагол, которым диапазон подсети ОБЪЯВЛЯЕТСЯ. Закрыть
        # только Create значило бы починить громкий подслучай: подсеть создаётся
        # законным блоком, а служебный добавляется вторым запросом.
        *_make_reserved_net("resvacb"),
        Step(name="create-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "sub-resvacb-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": _RESERVED_LEGAL},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="add-reserved-block",
            method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": [_RESERVED_INSIDE]},
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                "pm.test('текст отказа — дословный контракт', () => "
                "pm.expect(String(pm.response.json().message || ''))"
                f".to.eql('{_RESERVED_MSG_V4_INSIDE_ADD}'));",
                "pm.test('операция не создана', () => pm.expect(pm.response.json().id).to.be.undefined);",
                # Полоса отказа различима МАШИННО. Без признака автомат, планирующий
                # адресацию, не может разветвиться: «префикс служебный, бери
                # следующий кандидат» неотличимо от «ввод негоден по форме». Разбор
                # прозы контракт запрещает, поэтому утверждается признак, а не текст
                # (текст утверждается выше — отдельно и дословно).
                "pm.test('полоса отказа различима машинно — ErrorInfo.reason', () => {"
                " const d = pm.response.json().details || [];"
                " pm.expect(d.some(x => x && x.reason === 'SUBNET_CIDR_RESERVED'),"
                " JSON.stringify(d)).to.eql(true); });",
            ])),
        # NB обёртка на ОТРИЦАНИИ — исключение, и оно обосновано: это ПЕРВОЕ
        # обращение к своей только что созданной подсети, то есть окно видимости
        # владельца (403/404) здесь реально и даёт ложный красный. Маскировки нет:
        # ждётся только полоса видимости, 400 терминален и есть предмет кейса, а по
        # исчерпании бюджета настоящие утверждения исполняются на том коде, который
        # пришёл, — не сошедшийся отказ по-прежнему валит кейс.
        # ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ тем же глаголом на той же подсети.
        retry_until_authorized(Step(
            name="add-legal-block",
            method="POST", path="/vpc/v1/subnets/{{subId}}:add-cidr-blocks",
            body={"ipv4CidrBlocks": [_RESERVED_LEGAL_2]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="verify-blocks",
            method="GET", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200),
                         "pm.test('законный блок добавлен, служебный — нет', () => {",
                         "  const c = " + SUBNET_V4_CIDRS + ";",
                         f"  pm.expect(c, JSON.stringify(c)).to.include('{_RESERVED_LEGAL_2}');",
                         f"  pm.expect(c, JSON.stringify(c)).to.not.include('{_RESERVED_INSIDE}');",
                         "});"])),
        retry_until_authorized(Step(name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        _cleanup_net(),
    ],
))


# VPC-1 F7 — the Subnet CIDR shape this suite writes against.
#
# Create takes an immutable primary anchor (`ipv4CidrPrimary` / `ipv6CidrPrimary`);
# further ranges move through the :add-cidr-blocks / :remove-cidr-blocks verb pair
# (`ipv4CidrBlocks` / `ipv6CidrBlocks`); the read projection exposes the anchor plus
# the additional ranges, which is what SUBNET_V4_CIDRS / SUBNET_V6_CIDRS join.
#
# The retired flat arrays (`v4_cidr_blocks` / `v6_cidr_blocks`) are RESERVED in both
# request messages, so the edge drops those JSON keys without a word: a body still
# carrying them creates a subnet with no CIDR at all and answers 200. This suite used
# to write the retired names and repair them in a post-load pass, which is how
# thirty-three creates kept shipping a key nobody read — the pass converted only a
# whitelist and left the rest untouched. The names are now written as the contract
# has them, and the guard below refuses to generate anything that reverts to the old
# ones instead of quietly papering over it.
_RETIRED_SUBNET_CIDR_KEYS = ("v4CidrBlocks", "v6CidrBlocks")


def _assert_no_retired_cidr_keys(case):
    for st in case.steps:
        if not st.body or not isinstance(st.body, dict):
            continue
        if not st.path.startswith("/vpc/v1/subnets"):
            continue
        for k in _RETIRED_SUBNET_CIDR_KEYS:
            if k in st.body:
                raise AssertionError(
                    f"{case.id} / {st.name}: {k!r} was retired from the Subnet request "
                    f"messages (VPC-1 F7) — the edge drops it and the step silently "
                    f"does nothing. Use ipv4CidrPrimary/ipv6CidrPrimary on Create, "
                    f"ipv4CidrBlocks/ipv6CidrBlocks on :add-cidr-blocks/:remove-cidr-blocks."
                )


for _c in CASES:
    _assert_no_retired_cidr_keys(_c)
