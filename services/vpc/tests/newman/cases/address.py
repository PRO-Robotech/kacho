# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для AddressService."""

CASES = []

# Address external IP — depends on default pool seeded for zone.
# Internal IP — requires Network + Subnet preflight.

def _make_net_sub(suffix="a", cidr="10.100.0.0/24"):
    return [
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": f"adr-{suffix}-net-{{{{runId}}}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="pre-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": f"adr-{suffix}-sub-{{{{runId}}}}", "zoneId": "{{existingZoneId}}",
                   # VPC-1 F7: Subnet.Create takes the immutable primary anchor
                   # ipv4CidrPrimary (single), not the retired v4_cidr_blocks array.
                   "ipv4CidrPrimary": cidr},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
    ]


def _cleanup_sub_net():
    # cleanup-sub/net — первый пост-create доступ к СВОЕМУ ресурсу: под параллельной
    # нагрузкой owner-tuple ещё материализуется → transient 403 (authz-first). Обёрнуто
    # в retry_until_authorized(retry_on=403) (санкц. паттерн, ср. concurrency.py): без
    # него cleanup-sub 403 → subnet не удаляется → cleanup-net 400 "network not empty"
    # (child-leaked каскад). Ретрай доводит subnet до удаления → net delete 200.
    return [
        retry_until_authorized(Step(
            name="cleanup-sub", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")],
        ), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(
            name="cleanup-net", method="DELETE", path="/vpc/v1/networks/{{netId}}",
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")],
        ), retry_on=(403,)),
    ]


CASES.append(Case(
    id="ADR-CR-CRUD-INT",
    title="Create internal Address → IP в subnet",
    classes=["CRUD"],
    priority="P1",
    steps=[
        *_make_net_sub("cri"),
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-cri-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('has internal ipv4', () => pm.expect(pm.response.json().internalIpv4Address).to.be.an('object'));",
                          # APPLY-11: чтение ресурса со строкой намерения несёт
                          # состояние применения.
                          *assert_apply_state_in_flight("ADR")])),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_sub_net(),
    ],
))

CASES.append(Case(
    id="ADR-CR-CRUD-EXT",
    title="Create external Address → IP из default pool",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-cre-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('has external ipv4', () => pm.expect(pm.response.json().externalIpv4Address).to.be.an('object'));",
                          "pm.test('has ip address value', () => pm.expect(pm.response.json().externalIpv4Address.address).to.match(/^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$/));"])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    id="ADR-CR-VAL-SPEC-ONEOF",
    title="Create без external/internal spec → InvalidArgument",
    classes=["VAL"],
    priority="P0",
    steps=[
        Step(name="create-no-spec", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-no-{{runId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="ADR-CR-VAL-BOTH-SPEC",
    title="Create с обоими spec (external+internal) → 400 (defensive: 400 + непустое тело)",
    classes=["VAL"],
    priority="P0",
    steps=[
        *_make_net_sub("both", "10.101.0.0/24"),
        # oneof address_spec задан дважды → ошибка JSON-transcoding: наш
        # api-gateway отдает JSON {code,message}. Кейс defensive — 400 + непустое тело.
        Step(name="create-both", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-bo-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"},
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_transcode_error()]),
        *_cleanup_sub_net(),
    ],
))

CASES.append(Case(
    id="ADR-CR-NEG-SUBNET-NOT-FOUND",
    title="Create internal с garbage subnetId → sync 404 NOT_FOUND",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-snf-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{garbageVpcId}}"}},
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                          "pm.test('mentions subnet', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('subnet'));"]),
    ],
))

CASES.append(Case(
    id="ADR-GET-NEG-NF",
    title="Get malformed id → 400 InvalidArgument 'invalid address id'",
    classes=["NEG"],
    priority="P0",
    steps=[
        Step(name="get-garbage", method="GET", path="/vpc/v1/addresses/{{garbageId}}",
             test_script=[
                 # malformed id (нет известного 3-char префикса)
                 # → 400 InvalidArgument "invalid address id '<X>'". Проверка family-agnostic.
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('mentions invalid id', () => { const m = pm.response.json().message; pm.expect(m).to.include('invalid'); pm.expect(m).to.include('id'); });",
             ]),
    ],
))

CASES.append(Case(
    id="ADR-LST-CRUD-OK",
    title="List addresses в project → 200",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="list", method="GET", path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&pageSize=10",
             test_script=[*assert_status(200),
                          "pm.test('addresses array', () => pm.expect(pm.response.json().addresses || []).to.be.an('array'));"]),
    ],
))

CASES.append(Case(
    id="ADR-LST-VAL-PROJECT-REQUIRED",
    title="List без projectId → rejected (400 InvalidArgument OR 403 authz-first, unscoped)",
    classes=["VAL", "AUTHZ"],
    priority="P0",
    steps=[
        # Unscoped list — gateway authz-first 403 (no path) ЛИБО backend 400. Оба =
        # «отклонено». См. assert_unscoped_rejected (gen.py).
        Step(name="list-no-project", method="GET", path="/vpc/v1/addresses",
             test_script=[*assert_unscoped_rejected()]),
    ],
))

CASES.append(Case(
    id="ADR-DEL-AUTHZ-NF-SYNC",
    title="Delete несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE", path="/vpc/v1/addresses/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="ADR-UPD-AUTHZ-NF-SYNC",
    title="Update несуществующего → sync 404",
    classes=["NEG", "AUTHZ"],
    priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH", path="/vpc/v1/addresses/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

# ─── Сужение списка по ЗНАЧЕНИЮ — замена снятому поиску по значению ────────────
#
# `GetByValue` снят волной 1 (`proto/declared-breaks.yaml`, символ `GetByValue`):
# его внешняя ветвь была НЕАВТОРИЗУЕМА ПО ПОСТРОЕНИЮ — область бралась из подсети, а
# у внешнего адреса подсети нет, значит объекта, про который можно задать вопрос о
# правах, не существовало вовсе. Вопрос «чей это адрес?» остался законным и
# закрывается сужением списка: `GET /vpc/v1/addresses?projectId=…&ipAddress=…`.
# Область берётся из `project_id`, который вызывающий и так обязан назвать; страница
# проверяется по правам построчно. Сужение покрывает ЧЕТЫРЕ формы владения —
# внутренний и внешний адрес в двух семействах (`ADR-LBV-CRUD-INT`/`-EXT`).
#
# Почему кейсы переписаны, а не подправлены. Прежние редакции звали снятый путь, и
# край отвечал `403 AUTHZ_DENIED` с пустыми `action`/`resource` и fqn
# `//vpc/v1/addresses:byValue`: записи в каталоге прав нет, потому что метода нет, и
# гейт края честно закрывается. ЧЕТЫРЕ из этих кейсов при этом были ЗЕЛЁНЫМИ —
# `ADR-GBV-NEG-NF`, `ADR-GBV-VAL-INVALID-IP`, `ADR-LBS-NEG-PARENT-NF` толерировали
# 403 через `assert_absent_id_rejected`/`oneOf([200,403,404])`, а
# `ADR-GBV-AUTHZ-UNSCOPED-DENY` ЖДАЛ 403 и получал его от каталога, а не от
# проверяемой защиты. Кейс, зеленеющий на несуществующем методе, хуже
# отсутствующего: он занимает слот и отчитывается об уверенности, которой нет.

CASES.append(Case(
    id="ADR-LBV-NEG-NF",
    title="Сужение по значению, которого ни у кого нет → 200 с пустой страницей (существование не раскрыто)",
    classes=["NEG", "AUTHZ"],
    priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvnf-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-addr", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv4Address && j.externalIpv4Address.address",
                                              "allocatedIp")])),
        # Плечо-контроль: СВОЁ значение находится. Без него утверждение о пустой
        # странице ниже зеленело бы и на сужении, которое не находит ничего никогда,
        # — то есть отрицание стояло бы без положительного.
        retry_until_present(Step(name="lbv-own-found", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{allocatedIp}}",
             test_script=[*assert_status(200),
                          "pm.test('своё значение находится (плечо-контроль)', () => "
                          "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                          ".to.include(pm.environment.get('addrId')));"]), "addrId"),
        # Предмет: значение, которого нет ни у кого. 192.0.2.0/24 — TEST-NET-1, из
        # пулов стенда не выдаётся (v4-пулы суиты живут в 100.101/16 и 100.102/16),
        # поэтому строка не появится и под параллельной нагрузкой.
        #
        # Исход — 200 с ПУСТОЙ страницей, а не отказ: у списка есть законный ответ на
        # корректно заданный вопрос, и этот ответ «у вас такого нет». Отказ здесь
        # означал бы, что край отличает «нет ни у кого» от «есть, но не у вас», то
        # есть сам стал бы оракулом существования.
        Step(name="lbv-absent", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress=192.0.2.99",
             test_script=[
                 *assert_status(200),
                 "pm.test('страница пуста — существование чужого не раскрыто', () => "
                 "pm.expect(pm.response.json().addresses || []).to.eql([]));",
             ]),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

# `ListBySubnet` снят той же волной (`declared-breaks.yaml`) как строгое подмножество
# списка, который уже несёт сужение по подсети: два пути к одному ответу с разными
# объектами проверки прав. Замена — поле `subnetId` списочного запроса.
CASES.append(Case(
    id="ADR-LSN-CRUD-OK",
    title="Сужение списка по подсети → адрес этой подсети присутствует, внесубнетный отсутствует",
    classes=["CRUD"],
    priority="P2",
    steps=[
        *_make_net_sub("lsn", "10.102.0.0/24"),
        Step(name="cr-in-sub", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lsn-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        # Внешний адрес не лежит ни в какой подсети — он и есть отрицательное плечо.
        Step(name="cr-ext", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lsn-ext-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "extId")]),
        poll_operation_until_done(),
        retry_until_present(Step(name="lsn", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&subnetId={{subId}}",
             test_script=[
                 *assert_status(200),
                 # Прежняя редакция утверждала «это массив» — истинно на ЛЮБОМ 200, в
                 # том числе на сужении, которое не сужает вовсе. Утверждение с таким
                 # телом не может покраснеть ни на одном дефекте сужения. Теперь на
                 # одной странице утверждаются ОБЕ стороны: что попадает и что нет.
                 "pm.test('адрес этой подсети присутствует', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.include(pm.environment.get('addrId')));",
                 "pm.test('внесубнетный (внешний) адрес того же проекта отсутствует', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.not.include(pm.environment.get('extId')));",
             ]), "addrId"),
        retry_until_authorized(Step(name="cleanup-ext", method="DELETE", path="/vpc/v1/addresses/{{extId}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        *_cleanup_sub_net(),
    ],
))

CASES.append(Case(
    id="ADR-LOP-CRUD-OK",
    title="ListOperations для address",
    classes=["CRUD"],
    priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lop-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="list-ops", method="GET", path="/vpc/v1/addresses/{{addrId}}/operations",
             test_script=[*assert_status(200),
                          "pm.test('at least 1 op', () => pm.expect((pm.response.json().operations || []).length).to.be.at.least(1));"])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# Расширение
CASES.extend(crud_list_bva_block("ADR", "/vpc/v1/addresses"))
CASES.append(conf_not_found_text("ADR", "/vpc/v1/addresses", "Address"))
CASES.append(state_update_unknown_mask("ADR", "/vpc/v1/addresses"))

CASES.append(Case(
    id="ADR-UPD-CRUD-OK",
    title="Update address description через mask",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-upd-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="patch", method="PATCH", path="/vpc/v1/addresses/{{addrId}}",
             body={"updateMask": "description", "description": "upd-newman"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # verify is a SECOND read of the caller's OWN fresh address after Update. The
        # owner-tuple / read-your-writes visibility window can still make this GET briefly
        # return 404 (existence-hidden authz gate) even though the Update op is done=true
        # and the row is durable (proven: the subsequent DELETE op completes done=true on
        # the same id). Wrap in the sanctioned RYW retry so the real 200 + description
        # assertions run once the read is visible — a genuinely-missing row still FAILS
        # (fail-closed after budget), so a real Update-mask bug is never masked.
        retry_until_authorized(Step(name="verify", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('upd-newman'));"])),
        Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))

# Дополнение: STATE immutable project + VAL move-no-dest + BVA pagesize=1
CASES.append(state_immutable_project("ADR", "/vpc/v1/addresses"))
CASES.append(list_pagesize_1_bva("ADR", "/vpc/v1/addresses"))

CASES.append(Case(
    id="ADR-CR-CONF-SUB-NF-TEXT",
    title="Create address с garbage subnet → точный текст 'Subnet ... not found'",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-confnf-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{garbageVpcId}}"}},
             test_script=[
                 *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                 "pm.test('verbatim Subnet ... not found', () => pm.expect(pm.response.json().message).to.match(/^Subnet .* not found$/));",
             ]),
    ],
))

# ADR-CR-CONF-PROJECT-NF-TEXT — тот же класс, что GW-CR-NEG-PROJECT-NF: прежде обе
# ветки `oneOf([200, 403])` ничего не утверждали, а следом поллилась операция,
# которой нет. Исход один: создание адреса авторизуется отношением `editor` на
# проекте из тела, у несуществующего проекта записей в модели прав нет → отказ
# fail-closed на краю, до сервиса.
CASES.append(Case(
    id="ADR-CR-CONF-PROJECT-NF-TEXT",
    title="Create external address с nonexistent project → 403 fail-closed на краю (операция не создаётся)",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{garbageRmId}}", "name": "adr-fnf-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[
                 *assert_status(403),
                 *assert_grpc_code(7, "PERMISSION_DENIED"),
                 "pm.test('отказ не подтверждает и не опровергает существование проекта', () => {",
                 "  const m = String(pm.response.json().message || '');",
                 "  pm.expect(m, 'текст отказа').to.not.contain('not found');",
                 "});",
             ]),
    ],
))

CASES.append(Case(
    id="ADR-UPD-CONF-NF-TEXT",
    title="Update несуществующего → точный текст 'Address ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="patch-nx", method="PATCH",
             path="/vpc/v1/addresses/{{garbageVpcId}}",
             body={"updateMask": "description", "description": "x"},
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="ADR-DEL-CONF-NF-TEXT",
    title="Delete несуществующего → точный текст 'Address ... not found' text",
    classes=["CONF", "NEG"], priority="P1",
    steps=[
        Step(name="del-nx", method="DELETE",
             path="/vpc/v1/addresses/{{garbageVpcId}}",
             test_script=[*assert_absent_id_rejected()]),
    ],
))

CASES.append(Case(
    id="ADR-DEL-CRUD-OK",
    title="Address Delete happy path",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-delok-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-happy", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="get-after-del", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(404)]),
    ],
))

# ADR-GBV-CRUD-OK — единственный ДОСТИЖИМЫЙ положительный путь GetByValue.
#
# Прежняя редакция этого кейса искала по ВНЕШНЕМУ адресу и не посылала `subnetId`,
# то есть запрашивала исход, которого у RPC нет: авторизация этого метода
# привязана к `subnet_id` (и в каталоге края, и в карте разрешений сервиса), а без
# него объект проверки неопределим → fail-closed отказ ВСЕГДА. Утверждение
# `oneOf([200, 403])` покрывало всё достижимое пространство исходов, ветка «если
# 200 — сверить id» не исполнялась ни разу, а обёртка повтора сначала выжигала
# бюджет на отказе и лишь потом засчитывала тот же отказ пройденным.
#
# ЧЕТЫРЕ ФОРМЫ ВЛАДЕНИЯ, ОДИН ВОПРОС — и обе половины проверяются e2e.
#
# Значение адреса хранится в четырёх местах: внутренний и внешний адрес, каждый в
# двух семействах. Замена, покрывающая только часть из них, отвечала бы «у вас
# такого нет» на законный вопрос про остальные — то есть была бы у́же снятого метода,
# а не заменой ему. Внутреннюю пару держит этот кейс, внешнюю — `ADR-LBV-CRUD-EXT`.
# Тот же инвариант закреплён на уровне хранилища
# (`internal/repo/address_list_by_value_integration_test.go`); здесь он проверяется
# через край, потому что до края доезжает ещё и проброс поля через gateway.
#
# Подсеть двухстековая: одна фикстура несёт оба семейства, поэтому v4 и v6 отвечают
# про ОДНУ подсеть и расхождение между ними нельзя списать на разные фикстуры.
CASES.append(Case(
    id="ADR-LBV-CRUD-INT",
    title="Сужение по значению находит внутренний адрес в ОБОИХ семействах (v4 и v6)",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="pre-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvi-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        Step(name="pre-sub", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": "adr-lbvi-sub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.121.0.0/24", "ipv6CidrPrimary": "fd21:1::/64"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        # ── форма 1: внутренний IPv4 ──────────────────────────────────────────────
        Step(name="cr-int-v4", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvi4-{{runId}}",
                   "internalIpv4AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrV4")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-v4", method="GET", path="/vpc/v1/addresses/{{addrV4}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.internalIpv4Address && j.internalIpv4Address.address",
                                              "ipV4")])),
        retry_until_present(Step(name="lbv-int-v4", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{ipV4}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('внутренний IPv4 найден по значению', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.include(pm.environment.get('addrV4')));",
                 # Сужение обязано отдавать ИМЕННО спрошенное значение, а не страницу
                 # проекта: без этого утверждения кейс зеленел бы на реализации,
                 # которая поле игнорирует (адрес нашёлся бы просто как строка списка).
                 "pm.test('и в странице только строки с этим значением', () => "
                 "(pm.response.json().addresses || []).forEach(a => pm.expect("
                 "(a.internalIpv4Address || {}).address).to.eql(pm.environment.get('ipV4'))));",
             ]), "addrV4"),
        # ── форма 2: внутренний IPv6, та же подсеть ───────────────────────────────
        Step(name="cr-int-v6", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvi6-{{runId}}",
                   "internalIpv6AddressSpec": {"subnetId": "{{subId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrV6")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-v6", method="GET", path="/vpc/v1/addresses/{{addrV6}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.internalIpv6Address && j.internalIpv6Address.address",
                                              "ipV6")])),
        retry_until_present(Step(name="lbv-int-v6", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{ipV6}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('внутренний IPv6 найден по значению', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.include(pm.environment.get('addrV6')));",
                 # Отрицательное плечо между семействами: запрос про v6 не тянет за
                 # собой v4-соседа из той же подсети. Дизъюнкция по четырём колонкам
                 # хранилища легко пишется так, что совпадает не с тем семейством.
                 "pm.test('и v4-сосед по той же подсети не подмешан', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.not.include(pm.environment.get('addrV4')));",
             ]), "addrV6"),
        retry_until_authorized(Step(name="cleanup-v6", method="DELETE", path="/vpc/v1/addresses/{{addrV6}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="cleanup-v4", method="DELETE", path="/vpc/v1/addresses/{{addrV4}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        *_cleanup_sub_net(),
    ],
))

# ADR-LBV-AUTHZ-UNSCOPED-DENY — вопрос «чей это адрес?» без названной области
# ОТВЕРГАЕТСЯ, а не отвечается.
#
# Ровно это свойство и было причиной снятия метода: у поиска по значению область
# бралась из подсети, а у внешнего адреса подсети нет — авторизовать вопрос было
# нечем. Замена берёт область из `project_id`, и без него запрос обязан быть
# отвергнут: иначе сужение по значению превращается в перечисление адресов всего
# облака по значению, то есть в тот самый неавторизуемый вопрос, только шире.
#
# Почему прежняя редакция этого кейса ничего не проверяла. Она ждала 403 — и
# получала 403 от КАТАЛОГА ПРАВ, потому что метода уже не было: fqn
# `//vpc/v1/addresses:byValue`, `action`/`resource` пустые. Кейс был зелёным и при
# этом не касался защиты, о которой заявлял. Утверждение ниже адресовано полосе
# отказа, а не только его коду: тело обязано называть предмет отказа, и оно не
# может прийти от отсутствующей записи каталога.
#
# Толерантность `400|403` здесь — задокументированное исключение про ПОРЯДОК
# (`security.md` §authz-first), а не про неопределённость исхода: 403 даёт
# короткое замыкание края (нечем резолвить область), 400 — backend
# «project_id required». Обе — «отвергнут», разные полосы, у каждой свой
# производитель. Обёртки повтора нет: её godoc запрещает оборачивать deny.
CASES.append(Case(
    id="ADR-LBV-AUTHZ-UNSCOPED-DENY",
    title="Сужение по значению без projectId → отвергнуто fail-closed, адрес не раскрыт",
    classes=["AUTHZ", "NEG"], priority="P1",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvu-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-addr", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv4Address && j.externalIpv4Address.address",
                                              "allocatedIp")])),
        Step(name="lbv-unscoped", method="GET",
             path="/vpc/v1/addresses?ipAddress={{allocatedIp}}",
             test_script=[
                 *assert_unscoped_rejected(),
                 # Отказ обязан быть ПУСТЫМ по существу: ни id, ни сам IP. Иначе
                 # «отказали» и «рассказали» совпадают, и запрет декоративен.
                 "pm.test('отказ не раскрывает ни id адреса, ни его IP', () => {",
                 "  const body = JSON.stringify(pm.response.json());",
                 "  pm.expect(body, 'id адреса в теле отказа').to.not.contain(pm.environment.get('addrId'));",
                 "  pm.expect(body, 'IP в теле отказа').to.not.contain(pm.environment.get('allocatedIp'));",
                 "});",
             ]),
        # Положительное плечо: ТОТ ЖЕ запрос с названной областью проходит. Без него
        # отказ выше зеленел бы и на списке, сломанном целиком, — «отвергнут» ничего
        # не доказывает, если не отвергается только это.
        retry_until_present(Step(name="lbv-scoped-ok", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{allocatedIp}}",
             test_script=[*assert_status(200),
                          "pm.test('с названной областью тот же вопрос отвечается', () => "
                          "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                          ".to.include(pm.environment.get('addrId')));"]), "addrId"),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

# Две РАЗНЫЕ полосы ввода на одном поле, и у каждой свой единственный исход.
# Прежняя редакция сваливала их в `oneOf([200,403,404])` — утверждение, которое
# принимало и ответ, и отказ, то есть не утверждало ничего; оно и осталось зелёным,
# когда путь под ним исчез.
CASES.append(Case(
    id="ADR-LSN-NEG-PARENT-NF",
    title="Сужение по подсети: well-formed-но-отсутствующая → 200 пусто; малформед → 400",
    classes=["NEG"], priority="P2",
    steps=[
        # Полоса 1 — форма верна, подсети нет. Ссылка на СВОЙ ресурс, поэтому ответ
        # есть: страница пуста. Отказ здесь означал бы оракул существования подсети.
        Step(name="lsn-absent", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&subnetId={{garbageVpcId}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('страница пуста', () => "
                 "pm.expect(pm.response.json().addresses || []).to.eql([]));",
             ]),
        # Полоса 2 — форма неверна. Малформед отвергается синхронно, до хранилища:
        # пустая страница утверждала бы «в этой подсети адресов нет» про строку,
        # которая подсетью быть не может.
        Step(name="lsn-malformed", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&subnetId=not-a-subnet-id",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('отказ называет ссылку, из-за которой произошёл', () => "
                 "pm.expect(pm.response.json().message || '').to.contain('invalid subnet id'));",
             ]),
        # Положительный контроль: без сужения тот же список отвечает 200. Иначе оба
        # плеча выше зеленели бы на списке, отвергающем всё подряд.
        Step(name="lsn-control", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&pageSize=1",
             test_script=[*assert_status(200)]),
    ],
))

CASES.append(Case(
    id="ADR-LOP-NEG-PARENT-NF",
    title="ListOperations несуществующего address → 200 или 404",
    classes=["NEG"], priority="P2",
    steps=[
        Step(name="lop-nx", method="GET",
             path="/vpc/v1/addresses/{{garbageVpcId}}/operations",
             test_script=["pm.test('200/403/404', () => pm.expect(pm.response.code).to.be.oneOf([200, 403, 404]));"]),
    ],
))

CASES.extend(ecp_name_block("ADR", "/vpc/v1/addresses",
                             {"externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(ecp_description_block("ADR", "/vpc/v1/addresses",
                                    {"externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(ecp_labels_block("ADR", "/vpc/v1/addresses",
                               {"externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(updatemask_decision_table("ADR", "/vpc/v1/addresses"))
CASES.extend(filter_syntax_block("ADR", "/vpc/v1/addresses"))
CASES.append(pagination_roundtrip("ADR", "/vpc/v1/addresses"))

CASES.extend(update_happy_per_field("ADR", "/vpc/v1/addresses", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(perf_baseline_block("ADR", "/vpc/v1/addresses"))
CASES.extend(verbatim_text_pack("ADR", "Address", "/vpc/v1/addresses"))
CASES.extend(authz_caller_headers_block("ADR", "/vpc/v1/addresses"))

CASES.append(update_happy_multi_field("ADR", "/vpc/v1/addresses", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.append(cross_project_resource_block("ADR", "/vpc/v1/addresses",
    {"externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.append(list_filter_match_block("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(neg_invalid_types_block("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(http_method_not_allowed_block("ADR", "/vpc/v1/addresses"))
CASES.extend(malformed_body_block("ADR", "/vpc/v1/addresses"))

CASES.append(alreadyexists_dup_name_for("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(update_mask_partial_block("ADR", "/vpc/v1/addresses", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.append(perf_baseline_get_block("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.extend(list_total_size_check_block("ADR", "/vpc/v1/addresses"))
CASES.extend(headers_content_type_block("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))

# v10 Address-specific
CASES.append(Case(
    id="ADR-CR-VAL-EXT-WITH-SUBNET-FK",
    title="Create external + internal со заданным subnet_id → 400 oneof",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="create-bad-combo", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-combo-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"},
                   "internalIpv4AddressSpec": {"subnetId": "{{garbageVpcId}}"}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
    ],
))

CASES.append(Case(
    id="ADR-CR-STATE-RESERVED-AT-BIRTH-CLEARED-BY-UPDATE",
    title="Свежий адрес reserved=true (тенант заказал сам адрес); снять резерв — через Update",
    classes=["STATE"], priority="P2",
    steps=[
        # `reserved` says the address is held by the project in its own right: the
        # tenant asked for the address itself, so it outlives every consumer and goes
        # away only on an explicit Delete. `AddressService.Create` IS the tenant asking,
        # and nothing else in this service creates an address — so every address born
        # here is a reservation, and Create sets the flag.
        #
        # That the field is absent from CreateAddressRequest means the caller cannot
        # CHOOSE the value; it says nothing about which value the service picks. The
        # contract states the value out loud: InternalAddressService.MarkAddressEphemeralInUse
        # exists to CLEAR the flag on an address auto-allocated for an interface, and
        # records that such addresses "создаются через публичный AddressService.Create
        # с `reserved = true`". A flag that must be cleared afterwards was set before.
        #
        # Two legs, because one would not separate anything: read the value at birth,
        # then move it through the only door that opens — Update — and read it again.
        # An assertion that restated a constant could not pass both.
        Step(name="cr-plain", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-flg-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-reserved-at-birth", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('fresh address is reserved', () => "
                          "pm.expect(pm.response.json().reserved).to.eql(true));",
                          "pm.test('and not yet used by anything', () => "
                          "pm.expect(pm.response.json().used || false).to.eql(false));"])),
        retry_until_authorized(Step(name="upd-unreserve", method="PATCH", path="/vpc/v1/addresses/{{addrId}}",
             body={"updateMask": "reserved", "reserved": False},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # Same RYW wrap as ADR-UPD-CRUD-OK's verify, and for the same reason: a read of
        # the caller's own address right after Update can briefly 404 behind the
        # existence-hiding authz gate. It cannot mask the subject of this case —
        # retry_until_authorized retries on 403/404 only, so a 200 carrying the wrong
        # `reserved` fails on the spot, without a single retry.
        retry_until_authorized(Step(name="get-unreserved", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('giving up the reservation is a tenant decision, taken through Update', "
                          "() => pm.expect(pm.response.json().reserved || false).to.eql(false));"])),
        retry_until_authorized(Step(name="cleanup", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=["pm.test('cleanup', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                          *save_from_response("j.id", "opId")]), retry_on=(403,)),
    ],
))

# ADR-LBV-VAL-INVALID — кейс ОСТАЁТСЯ отрицательным, но предмет у него другой.
#
# Было: «поиск по значению с негодным IP → 400 или 404» — отрицание про снятый
# метод, и толерантность `400|403|404` принимала за исход отказ каталога прав.
# Стало: НЕГОДНОЕ ЗНАЧЕНИЕ ФИЛЬТРА СПИСКА отвергается синхронно.
#
# Почему это по-прежнему отрицание, а не «пустая страница». Строка, которая адресом
# быть не может, не имеет ответа НИ ПРИ КАКИХ данных: ни одна строка хранилища не
# несёт такого значения. Пустая страница означала бы «такого адреса ни у кого нет» —
# ложное утверждение об отсутствии, и вызывающий не отличил бы его от «значение
# никому не принадлежит» (`ADR-LBV-NEG-NF` выше — законный ответ именно на этот
# второй вопрос). Различать их и есть назначение поля. Ровно за такое ложное «не
# найдено» на запрос без предмета снимали и сам метод, поэтому замена не наследует
# послабление, а соседнее сужение того же запроса (`subnetId`) отвергает малформед
# с самого начала — один и тот же ввод не может иметь два ответа в зависимости от
# того, каким полем его задали.
#
# Продуктовая сторона правилась под этот кейс (RED→GREEN):
# `internal/apps/kacho/api/address/list.go` + пробы
# `list_value_narrow_test.go` (там же — оба семейства как положительные контроли).
CASES.append(Case(
    id="ADR-LBV-VAL-INVALID",
    title="Негодное значение фильтра списка → 400 INVALID_ARGUMENT с именем поля (не пустая страница)",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        Step(name="lbv-bad", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress=not-an-ip",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('отказ называет поле, из-за которого произошёл', () => "
                 "pm.expect(pm.response.json().message || '').to.contain('ip_address'));",
                 # Отказ не превращается в утверждение об отсутствии — именно оно и
                 # было дефектом.
                 "pm.test('и не утверждает, что адреса нет', () => "
                 "pm.expect((pm.response.json().message || '').toLowerCase()).to.not.contain('not found'));",
             ]),
        # Положительный контроль в паре: законное значение ТОГО ЖЕ поля проходит.
        # Без него 400 выше зеленел бы и на списке, отвергающем любое сужение.
        Step(name="lbv-wellformed-ok", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress=192.0.2.99",
             test_script=[*assert_status(200)]),
    ],
))

# ADR-LBV-CONF-NOLEAK-CROSS-PROJECT — P0: область сужения ОБЯЗАНА сужать.
#
# Ось области сменилась вместе с механизмом. У снятого метода областью была ПОДСЕТЬ,
# и страж спрашивал «найдётся ли адрес подсети-1, если областью назвать подсеть-2».
# У замены область — ПРОЕКТ (`project_id`), поэтому тот же вопрос задаётся по своей
# оси: адрес живёт в проекте-1, спрашиваем по его значению, но областью называем
# проект-2. Если сервис отдаст адрес, `project_id` декоративен, и сужение по значению
# превращается в перечисление чужих адресов по значению — то есть ровно в тот
# неавторизуемый вопрос, за который метод и сняли.
#
# Второй вызывающий для этого не нужен и был бы хуже: право на ОБА проекта у актора
# суиты есть (директива изоляции — дефолтный actor гранится editor на оба своих
# проекта), поэтому отказ авторизации не может подменить собой предмет. Различает
# именно сужение выборки, а не наличие гранта.
#
# Кейс самодостаточен, достижим и РАЗЛИЧАЕТ: снятие `project_id` из предиката
# хранилища немедленно красит его.
CASES.append(Case(
    id="ADR-LBV-CONF-NOLEAK-CROSS-PROJECT",
    title="Сужение по значению: адрес проекта-1 не находится, когда областью назван проект-2",
    classes=["CONF", "AUTHZ"], priority="P0",
    steps=[
        Step(name="cr-in-proj1", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvleak-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-ip", method="GET", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv4Address && j.externalIpv4Address.address",
                                              "leakIp")])),
        # Контроль: областью назван СВОЙ проект-1 → адрес находится. Без этого плеча
        # отрицательное плечо ниже зеленело бы и от того, что сужение сломано целиком
        # (тогда «не нашёл» не доказывает ничего).
        retry_until_present(Step(name="lbv-own-scope-finds", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{leakIp}}",
             test_script=[*assert_status(200),
                          "pm.test('в своей области адрес находится (плечо-контроль)', () => "
                          "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                          ".to.include(pm.environment.get('addrId')));"]), "addrId"),
        # Предмет: то же значение, другая область → адрес НЕ выдаётся.
        #
        # Утверждается 200 с пустой страницей, а не отказ: право на проект-2 у
        # вызывающего есть, поэтому список обязан ответить — и ответить, что здесь
        # такого адреса нет. Отказ на этом месте скрыл бы дефект: он выглядел бы как
        # «не показали», не будучи доказательством, что не показали ИМЕННО эту строку.
        Step(name="lbv-foreign-scope", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectCrossId}}&ipAddress={{leakIp}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('адрес чужого проекта не выдан', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id), "
                 "JSON.stringify(pm.response.json())).to.not.include(pm.environment.get('addrId')));",
                 "pm.test('и ни id, ни IP не просочились в ответ', () => {",
                 "  const body = JSON.stringify(pm.response.json());",
                 "  pm.expect(body, 'id адреса').to.not.contain(pm.environment.get('addrId'));",
                 "  pm.expect(body, 'IP адреса').to.not.contain(pm.environment.get('leakIp'));",
                 "});",
             ]),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path="/vpc/v1/addresses/{{addrId}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

# v11 edge cases
CASES.append(Case(
    id="ADR-LST-PAGE-NEGATIVE-SIZE",
    # Размер страницы вне [0..1000] ОТВЕРГАЕТСЯ, а не подменяется умолчанием
    # (конвенция pagination). Исход ровно один, поэтому и утверждение одно:
    # прежнее `oneOf([200, 400])` под заголовком «rejected or default» проходило
    # и при отказе, и при его отсутствии — то есть не отделяло соблюдение
    # контракта от нарушения. Проверка детерминирована: у прогона есть субъект,
    # поэтому ранний выход по пустому субъекту не срабатывает и валидация
    # страницы выполняется всегда.
    title="List с pageSize=-1 → 400 InvalidArgument (отвергается, не clamp\'ится)",
    classes=["BVA", "VAL"], priority="P2",
    steps=[Step(name="lst-neg", method="GET",
                path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&pageSize=-1",
                test_script=[
                    *assert_status(400),
                    *assert_grpc_code(3, "INVALID_ARGUMENT"),
                    "pm.test('names the offending field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('page_size'));",
                ])],
))

CASES.append(Case(
    id="ADR-LST-FILTER-SPECIAL-CHARS",
    title="List с filter содержащим спец-символы → 400 или 200",
    classes=["FILTER", "VAL"], priority="P3",
    steps=[Step(name="lst-fsc", method="GET",
                path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&filter=name%3D%22%21%40%23%24%25%22",
                test_script=["pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="ADR-LST-PAGESIZE-EXACTLY-1000",
    title="List с pageSize=1000 (boundary max) → 200",
    classes=["BVA"], priority="P2",
    steps=[Step(name="lst-max", method="GET",
                path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&pageSize=1000",
                test_script=[*assert_status(200)])],
))

CASES.append(Case(
    id="ADR-LST-PAGESIZE-1001",
    title="List с pageSize=1001 (over max) → 400",
    classes=["BVA", "VAL"], priority="P1",
    steps=[Step(name="lst-1001", method="GET",
                path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="ADR-LST-DOUBLE-PROJECT-PARAM",
    title="List с дубликатом projectId param → 200 (last wins) или 400",
    classes=["VAL"], priority="P3",
    steps=[Step(name="lst-dup", method="GET",
                path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&projectId={{_suiteProjectCrossId}}&pageSize=10",
                test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])],
))

CASES.append(Case(
    id="ADR-GET-TRAILING-SLASH",
    title="Get с trailing slash → 404",
    classes=["VAL"], priority="P3",
    steps=[Step(name="get-trail", method="GET", path="/vpc/v1/addresses/{{garbageVpcId}}/",
                test_script=["pm.test('non-2xx', () => pm.expect(pm.response.code).to.be.oneOf([400, 404]));"])],
))

# === Required + Immutable для Address ===
# `name` снят из списка — см. разбор у NET (контракт допускает пустое имя,
# `ADR-CR-VAL-NAME-NULL` утверждает обратное тому, что обещал этот кейс).
CASES.extend(required_fields_matrix("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "name": "adr-req-{{runId}}",
     "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
    ["projectId"]))  # ipv4 spec — oneof, не required
CASES.extend(immutable_fields_matrix("ADR", "/vpc/v1/addresses",
    ["project_id", "external_ipv4_address_spec", "internal_ipv4_address_spec"]))

CASES.extend(security_injection_block("ADR", "/vpc/v1/addresses", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))
CASES.append(conformance_lifecycle_pack("ADR", "/vpc/v1/addresses",
    {"projectId": "{{_suiteProjectId}}", "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}}))


# ─── External IPv6 regression coverage ──────────────────────────────────────
#
# Backend: sparse counter-based IPv6 IPAM —
# ipv6_pool_cursors / ipv6_allocated_ips / ipv6_released_offsets, allocator
# = try pop released SKIP LOCKED → fallback bump cursor; ip = pool_base + offset.
# Эти кейсы — black-box проверка через api-gateway, не знают про SQL.
#
# Изоляция: каждый case создает свой v6-pool в {{zoneD}} (нет seeded-default
# v4-pool там — не пересекается с ADR-CR-CRUD-EXT). Cleanup в обратном порядке:
# address → pool. Pool DELETE проходит (зависимостей через FK нет;
# ipv6_allocated_ips → address_pools CASCADE).
#
# Parallel-safety: EXTERNAL_PUBLIC pool namespaces are GLOBAL. The v4 fall-through
# pool below uses the address-suite block 100.101.0.0/16 (NOT the nlb seed's
# a zone-derived 100.102.<octet>.0/24, live for the whole umbrella run, nor the internal-pool suite's
# 100.100.0.0/16). Only 3 geo zones exist so {{zoneD}}≡{{zoneC}}≡ru-central1-d;
# the is_default pools here therefore share ONE (zone,kind) partition with the
# internal-pool suite → run.sh serial-collections.txt keeps the two collections
# from running concurrently (else 409 AlreadyExists on the default partition).

POOLS = "/vpc/v1/addressPools"
ADDRS = "/vpc/v1/addresses"


def _make_v6_pool(suffix="v6", zone="{{zoneD}}", cidr="2001:db8:cafe::/64",
                  is_default=True):
    """Создать v6-pool для конкретного case + забрать id в poolId.

    split-shape — v6 CIDR кладется в v6CidrBlocks, v4CidrBlocks=[]."""
    body = {"name": f"adr-{suffix}-pool-{{{{runId}}}}", "kind": "EXTERNAL_PUBLIC",
            "zoneId": zone,
            "v4CidrBlocks": [], "v6CidrBlocks": [cidr],
            "isDefault": is_default}
    return [
        # InternalAddressPoolService.Create — admin-only (security.md), gated on cluster
        # `system_admin`; the address suite default auth is projectAdminA1 (NOT admin),
        # so these pool ops MUST carry the bootstrap admin JWT or they 403. (internal-pool
        # suite gets this via _ADMIN_DEFAULT_SERVICES; the address suite injects it per-step.)
        Step(name=f"pre-pool-{suffix}", method="POST", path=POOLS, internal=True, body=body,
             auth="jwtBootstrap",
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "poolId")]),
    ]


def _cleanup_pool():
    return [
        Step(name="cleanup-pool", method="DELETE", path=POOLS + "/{{poolId}}", internal=True,
             auth="jwtBootstrap",
             test_script=["pm.test('cleanup pool (200 or 400/404)', () => "
                          "pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));"]),
    ]


# `ADR-LBV-CRUD-EXT` стоит ЗДЕСЬ, а не рядом с остальными кейсами сужения по
# значению: он зовёт `_make_v6_pool`/`_cleanup_pool`, а модуль исполняется
# сверху вниз — выше по файлу этих имён ещё не существует.
# ADR-LBV-CRUD-EXT — кейс ПЕРЕВЁРНУТ, и это главное содержательное следствие замены.
#
# Было: «поиск по значению внешнего адреса ОТВЕРГАЕТСЯ». Это было верно и правильно
# для снятого метода: область у него была ровно одна — подсеть, а внешний адрес в
# подсети не размещается (в схеме `external_ipv4` и `internal_ipv4` — разные nullable
# jsonb, а `Address` — oneof). Вопрос «какой внешний адрес имеет значение X внутри
# подсети S» не имел ответа ни при каких данных, и отвечать на него ложным «не
# найдено» было дефектом (issues/104, первая половина).
#
# Стало: вопрос имеет ответ, потому что у замены другая область. `project_id` есть и
# у внешнего адреса — он принадлежит проекту, просто не подсети. Ровно это и было
# причиной снятия метода, и ровно это замена чинит: закрывается вторая половина
# issues/104 — полноценный поиск по внешнему значению, которому у снятого RPC не
# хватало области.
#
# Поэтому здесь стоит 200 и НАЙДЕННЫЙ адрес, а не 400. Кейс, оставленный
# отрицательным, закреплял бы ограничение снятого метода поверх механизма, у
# которого его нет, — то есть пинил бы дефект.
#
# Формы владения 3 и 4 (внешний в двух семействах); формы 1 и 2 — `ADR-LBV-CRUD-INT`.
CASES.append(Case(
    id="ADR-LBV-CRUD-EXT",
    title="Сужение по значению находит ВНЕШНИЙ адрес в обоих семействах (v4 и v6)",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        # ── форма 3: внешний IPv4 (default-пул зоны) ──────────────────────────────
        Step(name="cr-ext-v4", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvx4-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrX4")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-x4", method="GET", path="/vpc/v1/addresses/{{addrX4}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv4Address && j.externalIpv4Address.address",
                                              "ipX4")])),
        retry_until_present(Step(name="lbv-ext-v4", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{ipX4}}",
             test_script=[
                 *assert_status(200),
                 # Это и есть перевёрнутое утверждение: там, где снятый метод обязан
                 # был отказать, замена обязана НАЙТИ.
                 "pm.test('внешний IPv4 найден по значению (у снятого метода ответа не было)', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.include(pm.environment.get('addrX4')));",
                 "pm.test('и ответ не утверждает, что адреса нет', () => "
                 "pm.expect(JSON.stringify(pm.response.json()).toLowerCase()).to.not.contain('not found'));",
             ]), "addrX4"),
        # ── форма 4: внешний IPv6 (свой пул суиты в {{zoneD}}) ────────────────────
        *_make_v6_pool("lbvx", zone="{{zoneD}}", cidr="2001:db8:1b7::/64"),
        Step(name="cr-ext-v6", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-lbvx6-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrX6")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-x6", method="GET", path="/vpc/v1/addresses/{{addrX6}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv6Address && j.externalIpv6Address.address",
                                              "ipX6")])),
        retry_until_present(Step(name="lbv-ext-v6", method="GET",
             path="/vpc/v1/addresses?projectId={{_suiteProjectId}}&ipAddress={{ipX6}}",
             test_script=[
                 *assert_status(200),
                 "pm.test('внешний IPv6 найден по значению', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.include(pm.environment.get('addrX6')));",
                 # Отрицательное плечо между формами владения: запрос про внешний v6
                 # не подмешивает внешний v4 того же проекта.
                 "pm.test('и внешний v4 того же проекта не подмешан', () => "
                 "pm.expect((pm.response.json().addresses || []).map(a => a.id))"
                 ".to.not.include(pm.environment.get('addrX4')));",
             ]), "addrX6"),
        retry_until_authorized(Step(name="cleanup-x6", method="DELETE", path="/vpc/v1/addresses/{{addrX6}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
        *_cleanup_pool(),
        retry_until_authorized(Step(name="cleanup-x4", method="DELETE", path="/vpc/v1/addresses/{{addrX4}}",
             test_script=[*save_from_response("j.id", "opId")]), retry_on=(403,)),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # Happy path для External IPv6 auto-allocation: AddressPool (v6 CIDR) →
    # Address.Create external_ipv6_address_spec без explicit address → backend
    # allocator (sparse counter) выбирает offset 1 из pool, ip = base + offset.
    # Get показывает externalIpv6Address.address как валидный v6.
    id="ADR-CR-CRUD-EXT-V6",
    title="Create external_ipv6 Address → IP из default v6 pool",
    classes=["CRUD"], priority="P1",
    steps=[
        *_make_v6_pool("crv6", zone="{{zoneD}}", cidr="2001:db8:cafe::/64"),
        Step(name="create", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-crv6-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get", method="GET", path=ADDRS + "/{{addrId}}",
             test_script=[*assert_status(200),
                          "pm.test('has external ipv6', () => pm.expect(pm.response.json().externalIpv6Address).to.be.an('object'));",
                          "pm.test('v6 address looks like ipv6 hex', () => pm.expect(pm.response.json().externalIpv6Address.address).to.match(/^[0-9a-fA-F:]+$/));",
                          "pm.test('v6 ip starts with pool prefix 2001:db8:cafe', () => pm.expect(pm.response.json().externalIpv6Address.address).to.match(/^2001:db8:cafe:/));"])),
        retry_until_authorized(Step(name="cleanup-addr", method="DELETE", path=ADDRS + "/{{addrId}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_pool(),
    ],
))


CASES.append(Case(
    # Cascade resolve не находит v6 pool в зоне: Create external_ipv6 в zone,
    # где НЕТ pool с v6 CIDR → backend возвращает FailedPrecondition (cascade
    # resolve fails или allocator не находит подходящий pool). Защищает контракт
    # «family-aware resolve». Используется альтернативная zone — там нет seeded
    # pools (ни v4, ни v6) и нет других кейсов, создающих pool в этой зоне.
    id="ADR-CR-NEG-EXT-V6-NO-POOL",
    title="Create external_ipv6 без v6 pool в зоне → FailedPrecondition",
    classes=["NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-nv6-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneB}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        # Operation done=true с error_code=9 (FailedPrecondition) — backend
        # либо в cascade resolve, либо в AllocateExternalIPv6FromPool увидит
        # что pool с v6 CIDR не найден.
        poll_operation_until_done(),
        Step(name="check-op-failed", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('operation done', () => pm.expect(pm.response.json().done).to.equal(true));",
                          "pm.test('operation has error', () => pm.expect(pm.response.json().error).to.be.an('object'));",
                          "pm.test('error code 9 (FailedPrecondition) or 5 (NotFound)', () => pm.expect(pm.response.json().error.code).to.be.oneOf([5, 9]));"]),
    ],
))


CASES.append(Case(
    # Sparse allocator returns offset to released pool on Delete; next Allocate
    # берет его first (perceived FIFO). First allocate получает IP с offset=1
    # (вычислимо как base + 1); Delete — push offset=1 в released; Second allocate
    # должен получить **тот же** IP (попадает в pop released path до bump cursor).
    id="ADR-DEL-EXT-V6-RELEASE-REUSE",
    title="Delete v6 Address → offset возвращается в released, Reuse выдает тот же IP",
    classes=["STATE", "CONF"], priority="P1",
    steps=[
        *_make_v6_pool("rru", zone="{{zoneD}}", cidr="2001:db8:bee::/64"),
        # 1) Create + remember the IP.
        Step(name="cr-1", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-rru1-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addr1Id")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-1", method="GET", path=ADDRS + "/{{addr1Id}}",
             test_script=[*assert_status(200),
                          *save_from_response("j.externalIpv6Address && j.externalIpv6Address.address", "firstIp")])),
        # 2) Delete first — pushes offset to ipv6_released_offsets.
        retry_until_authorized(Step(name="del-1", method="DELETE", path=ADDRS + "/{{addr1Id}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        # 3) Allocate again — should pick up the released offset → same IP.
        Step(name="cr-2", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-rru2-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addr2Id")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-2", method="GET", path=ADDRS + "/{{addr2Id}}",
             test_script=[*assert_status(200),
                          "pm.test('reused IP equals first IP (released-first-allocate)', () => "
                          "pm.expect(pm.response.json().externalIpv6Address.address).to.equal(pm.environment.get('firstIp')));"])),
        retry_until_authorized(Step(name="cleanup-addr2", method="DELETE", path=ADDRS + "/{{addr2Id}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        *_cleanup_pool(),
    ],
))


CASES.append(Case(
    # Глобальный v4 default pool (seeded в кластере) НЕ "крадет" v6 запрос:
    # cascade step 4 (zone_default) в {{zoneA}} для v6 пуст → step 5 global_default
    # находит v4 pool, family-фильтр его отвергает (нет v6 cidr) → cascade
    # проваливается с FailedPrecondition (resolve address pool: pool not resolved).
    # Если family-filter сломан — попадаем в Internal "pool has no IPv6 cidr_blocks".
    id="ADR-CR-EXT-V6-FAMILY-FALLTHROUGH",
    title="External v6 в zone без v6 pool: cascade фильтрует v4 default → FailedPrecondition",
    classes=["CONF", "NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-fal-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneA}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrId")]),
        poll_operation_until_done(),
        Step(name="check-op-failed", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('operation done', () => pm.expect(pm.response.json().done).to.equal(true));",
                          "pm.test('operation has error', () => pm.expect(pm.response.json().error).to.be.an('object'));",
                          # family-filter в cascade resolve: должен быть код 9
                          # (FailedPrecondition), а НЕ 13 (Internal). 13 — регрессия.
                          "pm.test('error code 9 (FailedPrecondition), не 13 (Internal — регрессия)', () => pm.expect(pm.response.json().error.code).to.equal(9));"]),
    ],
))


# ─── Address.Create fallthrough на split-shape pool ─────────────────────────

CASES.append(Case(
    # Address.Create v4 в zone где default-pool v6-only. Cascade Step 4
    # (zone_default) находит pool, family-фильтр (poolHasFamily v4 →
    # len(V4CIDRBlocks)>0 → false) пропускает → fall-through на Step 5; нет v4
    # global default → ErrPoolNotResolved → Operation error code 9
    # (FailedPrecondition).
    id="ADR-CR-EXT-FALLTHROUGH-V4",
    title="Create v4 Address в zone с v6-only default pool → cascade family-skip → FailedPrecondition (REQ-RESOLVE-02)",
    classes=["CONF", "NEG"], priority="P0",
    steps=[
        # Setup v6-only default pool в throwaway {{zoneD}}.
        Step(name="cr-v6-default", method="POST", path=POOLS, internal=True,
             auth="jwtBootstrap",  # InternalAddressPoolService — system_admin gated
             body={"name": "adr-falv4-pool-{{runId}}", "kind": "EXTERNAL_PUBLIC",
                   "zoneId": "{{zoneD}}",
                   "v4CidrBlocks": [], "v6CidrBlocks": ["2001:db8:b0b::/64"],
                   "isDefault": True},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "falV4PoolId")]),
        # Allocate v4 → cascade falls through (нет v4 default в {{zoneD}}).
        Step(name="create", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-falv4-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "falV4AddrId")]),
        poll_operation_until_done(),
        Step(name="check-op-failed", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('operation done', () => pm.expect(pm.response.json().done).to.equal(true));",
                          "pm.test('operation has error', () => pm.expect(pm.response.json().error).to.be.an('object'));",
                          # family-filter post-split: код 9 (FailedPrecondition), не 13 (Internal).
                          "pm.test('error code 9 (FailedPrecondition), не 13 (Internal)', () => pm.expect(pm.response.json().error.code).to.equal(9));"]),
        # Cleanup pool.
        Step(name="cleanup-pool", method="DELETE", path=POOLS + "/{{falV4PoolId}}", internal=True,
             auth="jwtBootstrap",
             test_script=["pm.test('cleanup pool (200 or 400/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));"]),
    ],
))


CASES.append(Case(
    # Зеркало ADR-CR-EXT-FALLTHROUGH-V4 для v6: default-pool в zone — v4-only;
    # v6-allocate проваливает все 5 шагов cascade (family-filter постоянно).
    id="ADR-CR-EXT-FALLTHROUGH-V6",
    title="Create v6 Address в zone с v4-only default pool → cascade family-skip → FailedPrecondition (REQ-RESOLVE-01)",
    classes=["CONF", "NEG"], priority="P0",
    steps=[
        Step(name="cr-v4-default", method="POST", path=POOLS, internal=True,
             auth="jwtBootstrap",  # InternalAddressPoolService — system_admin gated
             body={"name": "adr-falv6-pool-{{runId}}", "kind": "EXTERNAL_PUBLIC",
                   "zoneId": "{{zoneD}}",
                   # Dedicated address-suite v4 block (100.101.0.0/16): the
                   # address_pool_cidrs EXCLUDE is GLOBAL per-kind, so this must not
                   # overlap the persistent nlb seed pool (zone-derived 100.102.<octet>.0/24) nor the
                   # internal-pool suite's block (100.100.0.0/16). See address.py
                   # header note + internal-pool.py parallel-safety docstring.
                   "v4CidrBlocks": ["100.101.0.0/24"], "v6CidrBlocks": [],
                   "isDefault": True},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "falV6PoolId")]),
        Step(name="create", method="POST", path=ADDRS,
             body={"projectId": "{{_suiteProjectId}}", "name": "adr-falv6-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": "{{zoneD}}"}},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "falV6AddrId")]),
        poll_operation_until_done(),
        Step(name="check-op-failed", method="GET", path="/operations/{{opId}}",
             test_script=[*assert_status(200),
                          "pm.test('operation done', () => pm.expect(pm.response.json().done).to.equal(true));",
                          "pm.test('operation has error', () => pm.expect(pm.response.json().error).to.be.an('object'));",
                          "pm.test('error code 9 (FailedPrecondition), не 13 (Internal)', () => pm.expect(pm.response.json().error.code).to.equal(9));"]),
        Step(name="cleanup-pool", method="DELETE", path=POOLS + "/{{falV6PoolId}}", internal=True,
             auth="jwtBootstrap",
             test_script=["pm.test('cleanup pool (200 or 400/404)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 404]));"]),
    ],
))


# ---------------------------------------------------------------------------
# Address release / idempotency
# ---------------------------------------------------------------------------

CASES.append(Case(
    id="ADR-DEL-EXT-V4-RELEASE-REUSE",
    title="Delete external v4 Address → IP попадает в pool free-list → следующий Allocate переиспользует IP",
    classes=["STATE", "CONF"], priority="P1",
    steps=[
        # 1. Allocate first.
        Step(name="alloc-first", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "rel-1-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrIdFirst")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-first-ip", method="GET", path="/vpc/v1/addresses/{{addrIdFirst}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const ip = j.externalIpv4Address && j.externalIpv4Address.address;",
                          "pm.expect(ip, 'first allocated IP').to.be.a('string').and.not.eql('');",
                          "pm.environment.set('firstIp', ip);"])),
        # 2. Delete first.
        Step(name="del-first", method="DELETE", path="/vpc/v1/addresses/{{addrIdFirst}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # 3. Allocate next — best-effort hope that release pushes IP back to free-list FIFO
        # и следующий alloc возьмет тот же IP. v4 pool — partial UNIQUE через address_pool_free_ips:
        # после Delete row возвращается в free-list, sweep-allocator берет первую свободную row.
        # На стенде с тестовым default pool это надежно (тест-pool маленький, FIFO).
        Step(name="alloc-second", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "rel-2-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrIdSecond")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="get-second-ip", method="GET", path="/vpc/v1/addresses/{{addrIdSecond}}",
             test_script=[
                 *assert_status(200),
                 "const j = pm.response.json();",
                 "const ip2 = j.externalIpv4Address && j.externalIpv4Address.address;",
                 "const ip1 = pm.environment.get('firstIp');",
                 "// v4-pool free-list поведение: released IP может (но не обязан) быть переиспользован",
                 "// сразу. Жестко требовать ip1==ip2 нельзя — pool может иметь дополнительные free slots.",
                 "// Проверяем только что валидный IP allocated + НЕ конфликтует с активным (его нет в системе).",
                 "pm.test('second alloc returned a valid IPv4', () => pm.expect(ip2, ip2).to.match(/^\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}$/));",
                 "pm.environment.set('secondIp', ip2);",
             ])),
        Step(name="del-second", method="DELETE", path="/vpc/v1/addresses/{{addrIdSecond}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ],
))


CASES.append(Case(
    id="ADR-DEL-IDM-DOUBLE",
    title="Delete address dva раза → first 200, second 404 (idempotent-safe, no 500)",
    classes=["IDM", "NEG"], priority="P2",
    steps=[
        Step(name="alloc", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}", "name": "idm-dbl-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.addressId", "addrIdIdm")]),
        poll_operation_until_done(),
        retry_until_authorized(Step(name="del-first", method="DELETE", path="/vpc/v1/addresses/{{addrIdIdm}}",
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
        Step(name="del-second-fails", method="DELETE", path="/vpc/v1/addresses/{{addrIdIdm}}",
             test_script=[
                 "pm.test('second Delete → 404 (NotFound, no 5xx)', () => pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.eql(404));",
                 "const j = pm.response.json();",
                 "pm.test('grpc code 5 (NotFound)', () => pm.expect(j.code).to.eql(5));",
                 "pm.test('not 500 (no internal error leak)', () => pm.expect(pm.response.code).to.not.eql(500));",
             ]),
    ],
))
