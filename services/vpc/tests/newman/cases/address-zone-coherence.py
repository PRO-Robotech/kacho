# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""External Address zone-coherence cases (ZC-VPC-ADDR-*) — track B GAP-3 (RED → GREEN).

Acceptance: docs/specs/sub-phase-nlb-vpc-zone-coherence-acceptance.md
  * ZC-VPC-ADDR-ZONE-01/02 — external Address (v4/v6) с несуществующей zone_id →
    sync InvalidArgument "unknown zone id '<X>'" (verbatim-зеркало subnet.validateZoneID).
  * ZC-VPC-ADDR-ZONE-03 — существующая zone_id → проходит.
  * ZC-VPC-ADDR-ZONE-04 — anycast (zone_id='') → освобождён от проверки.
    Кейс доводится до конца ЗОНЕ-НЕЗАВИСИМОЙ полосой резолва пула, а она обслуживается
    ТОЛЬКО пулом с `zone_id IS NULL` — его сеет `_SETUP-POOL-ANYCAST` (gen.py). Зональный
    сид этой полосе не помогает: полосы взаимоисключающие, зональный запрос до
    зоне-независимого пула не доходит, а зоне-независимый — до зонального.
Norm: .claude/rules/data-integrity.md §Placement-coherence («непроверенная зона
внешнего адреса — баг»).

Behaviour-level (skill testing-product-coach): negative ассертит ТОЧНУЮ строку
ошибки. RED до фикса rpc-implementer'а: CreateAddressUseCase не валидирует
ExternalSpec.ZoneID через geo → external Address с несуществующей зоной создаётся
(Create отдаёт 200 Operation вместо sync 400) → ZC-VPC-ADDR-ZONE-01/02 красные до GREEN.

REST base: /vpc/v1/addresses
"""

CASES = []

_ADDR = "/vpc/v1/addresses"
# Заведомо несуществующая зона (не в kacho_geo.zones).
_UNKNOWN_ZONE = "zzz-nonexistent-9"
_MSG_UNKNOWN_ZONE = f"unknown zone id '{_UNKNOWN_ZONE}'"


def _anycast_poll():
    """Опрос create-операции аникаст-адреса: id + утверждения о размещении.

    Захват id идёт ИЗ ОПРОСА (`capture_id_to`), а не из синхронного ответа POST:
    Operation несёт ПРЕДВЫДЕЛЕННЫЙ `addressId` ещё до работы воркера, поэтому он
    присутствует и у операции, завершившейся ОШИБКОЙ. Захват из POST — это захват id
    ресурса, которого может не существовать: дальше кейс удаляет ФАНТОМ, край
    отвечает `403` (scope_extractor не резолвит несуществующий объект), и кейс
    сообщает «expected 403 to deeply equal 200» вместо настоящей причины —
    отказа АНИКАСТ-ПОЛОСЫ резолва пула. Норма — testing.md §«Fixture-seed обязан
    проверять op.error перед извлечением resource-id из metadata».

    Плюс — предмет самого кейса: аникаст-адрес обязан быть выделен ЗОНЕ-НЕЗАВИСИМОЙ
    полосой резолва. Непустая зона у выделенного адреса означала бы, что запрос
    обслужен ЗОНАЛЬНЫМ пулом — placement-lie (адрес объявляет зону, которой у его
    префикса нет, и не защищён её failure-domain'ом).
    """
    step = poll_operation_until_done(capture_id_to="zcAddrId",
                                     id_expr="j.metadata && j.metadata.addressId")
    step.test_script.extend([
        "if (!j.error) {",
        "  const _a = (j.response || {});",
        "  pm.test('anycast address allocated (has external ipv4)', () => "
        "    pm.expect(_a.externalIpv4Address, JSON.stringify(j)).to.be.an('object'));",
        "  pm.test('anycast: allocated zoneId stays empty (zone-independent lane)', () => "
        "    pm.expect((_a.externalIpv4Address || {}).zoneId || '').to.eql(''));",
        "}",
    ])
    return step


CASES.append(Case(
    # index: ZC-VPC-ADDR-ZONE-01 (placement-coherence GAP-3)
    id="ZC-VPC-ADDR-ZONE-01-NEG-UNKNOWN-ZONE",
    title="Create external IPv4 Address с несуществующей zone_id → sync 400 unknown-zone "
          "(Verifies ZC-VPC-ADDR-ZONE-01)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="create-unknown-zone-v4", method="POST", path=_ADDR,
             body={"projectId": "{{_suiteProjectId}}", "name": "zc-addr-uz-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": _UNKNOWN_ZONE}},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 f"pm.test('unknown zone id verbatim', () => pm.expect(pm.response.json().message).to.eql({_MSG_UNKNOWN_ZONE!r}));",
             ]),
    ],
))

CASES.append(Case(
    # index: ZC-VPC-ADDR-ZONE-02 (placement-coherence GAP-3, v6 symmetry)
    id="ZC-VPC-ADDR-ZONE-02-NEG-UNKNOWN-ZONE-V6",
    title="Create external IPv6 Address с несуществующей zone_id → sync 400 unknown-zone "
          "(Verifies ZC-VPC-ADDR-ZONE-02)",
    classes=["NEG", "CONF"], priority="P1",
    steps=[
        Step(name="create-unknown-zone-v6", method="POST", path=_ADDR,
             body={"projectId": "{{_suiteProjectId}}", "name": "zc-addr-uz6-{{runId}}",
                   "externalIpv6AddressSpec": {"zoneId": _UNKNOWN_ZONE}},
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 f"pm.test('unknown zone id verbatim', () => pm.expect(pm.response.json().message).to.eql({_MSG_UNKNOWN_ZONE!r}));",
             ]),
    ],
))

CASES.append(Case(
    # index: ZC-VPC-ADDR-ZONE-03 (placement-coherence GAP-3 happy)
    id="ZC-VPC-ADDR-ZONE-03-KNOWN-ZONE-OK",
    title="Create external IPv4 Address с существующей zone_id → проходит existence-check "
          "(Verifies ZC-VPC-ADDR-ZONE-03)",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create-known-zone", method="POST", path=_ADDR,
             body={"projectId": "{{_suiteProjectId}}", "name": "zc-addr-kz-{{runId}}",
                   "externalIpv4AddressSpec": {"zoneId": "{{existingZoneId}}"}},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        # id берётся ИЗ ОПРОСА, а не из синхронного ответа POST: Operation несёт
        # предвыделенный addressId ещё до работы воркера, поэтому он присутствует и
        # у операции, завершившейся ошибкой. Захват из POST — захват id ресурса,
        # которого может не существовать (testing.md §«Fixture-seed обязан
        # проверять op.error перед извлечением resource-id из metadata»).
        poll_operation_until_done(capture_id_to="zcAddrId",
                                  id_expr="j.metadata && j.metadata.addressId"),
        retry_until_authorized(Step(name="get-known-zone", method="GET", path=f"{_ADDR}/{{{{zcAddrId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcAddrId')) return;",
                 *assert_status(200),
                 "pm.test('has external ipv4', () => pm.expect(pm.response.json().externalIpv4Address).to.be.an('object'));",
             ])),
        Step(name="cleanup-known-zone", method="DELETE", path=f"{_ADDR}/{{{{zcAddrId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcAddrId')) { pm.environment.set('opId', ''); return; }",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
    ],
))

CASES.append(Case(
    # index: ZC-VPC-ADDR-ZONE-04 (placement-coherence GAP-3 anycast exempt)
    id="ZC-VPC-ADDR-ZONE-04-ANYCAST-EMPTY-ZONE-OK",
    title="Create external IPv4 Address БЕЗ zone_id (anycast/global) → освобождён от проверки "
          "(Verifies ZC-VPC-ADDR-ZONE-04)",
    classes=["CRUD"], priority="P1",
    steps=[
        Step(name="create-anycast", method="POST", path=_ADDR,
             body={"projectId": "{{_suiteProjectId}}", "name": "zc-addr-any-{{runId}}",
                   "externalIpv4AddressSpec": {}},
             test_script=[
                 "pm.test('empty external zone (anycast) NOT rejected → 200 Operation', () => "
                 "  pm.expect(pm.response.code).to.eql(200));",
                 *save_from_response("j.id", "opId"),
             ]),
        # Синхронное «не отвергнут» — ещё не «создан»: предмет кейса доводится до конца
        # только опросом. Захват id здесь же утверждает отсутствие op.error, поэтому
        # отказ АНИКАСТ-ПОЛОСЫ резолва пула называется своей причиной, а не уезжает
        # фантомным id в удаление, где край отвечает 403 (scope_extractor не резолвит
        # несуществующий объект) — код, не называющий причину вовсе.
        # Утверждения о РАЗМЕЩЕНИИ снимаются с ответа самой операции (он несёт
        # созданный Address целиком), а не отдельным Get — лишний шаг сдвинул бы
        # сквозную нумерацию авто-обёрнутых шагов во ВСЕХ коллекциях набора и утопил
        # предмет правки в переименованиях.
        _anycast_poll(),
        Step(name="cleanup-anycast", method="DELETE", path=f"{_ADDR}/{{{{zcAddrId}}}}",
             test_script=[
                 "if (!pm.environment.get('zcAddrId')) { pm.environment.set('opId', ''); return; }",
                 *save_from_response("j.id", "opId"),
             ]),
        poll_operation_until_done(),
    ],
))
