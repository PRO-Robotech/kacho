# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set: kacho-geo OperationService envelope + OpsProxy malformed-id guard.

GEO-1 landed-контракт (F5, module-geo rule 4): admin-мутации geo
(InternalRegion/ZoneService) — синхронно-завершённые config-INSERT'ы, возвращают
`Operation{done:true}` НЕМЕДЛЕННО (syncop.Commit): `metadata` → CreateRegionMetadata
{regionId} (id доступен сразу), `result.response` → полное public-тело ресурса.
Клиент разворачивает `.response` — op-poll НЕ требуется (нет async-worker'а, нет
саги). Поэтому «дотянуться до done» здесь = прочитать done:true из синхронного
ответа мутации, а не поллить через публичный OpsProxy.

Заметка (обновлено под GEO-1): прежний RED-lock на PRO-Robotech/kacho#55 (gateway
OpsProxy `prefixToBackend` не содержит prefix `geo` → geo op-id не проксируется
через ПУБЛИЧНЫЙ `/operations/{id}`) снят из этого suite: под GEO-1 geo op —
done:true синхронно и admin/internal-only; публичный OpsProxy-роутинг geo-op НЕ
является контрактом GEO-1 acceptance (GEO-1-16 поллит опционально через internal
`/geo/v1/internal/operations/{id}`, а unwrap `.response` достаточен). Тестировать
неконтрактную поверхность RED-локом = держать suite красным впустую → заменён на
позитив done:true (см. docs/RESULTS.md).

Test-design: VAL/NEG (malformed op-id → InvalidArgument, OpsProxy shape-validation,
GREEN), STATE (Operation done:true synchronous, GEO-1-16).
"""

CASES = []


# ---------------------------------------------------------------------------
# GOP-GET-VAL-MALFORMED — malformed operation id → 400 InvalidArgument.
# OpsProxy validates the id shape (not 20-char, no legacy '_' prefix) and rejects
# with InvalidArgument BEFORE any backend routing — a generic OpsProxy guard.
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GOP-GET-VAL-MALFORMED",
    title="Get operation with malformed id → 400 InvalidArgument 'invalid operation id' (OpsProxy validation)",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="get-malformed-op", method="GET", path="/operations/garbage-not-an-op-id",
             test_script=[
                 *assert_status(400),
                 *assert_grpc_code(3, "INVALID_ARGUMENT"),
                 "pm.test('message: invalid operation id', () => pm.expect(String(pm.response.json().message).toLowerCase()).to.include('invalid operation id'));",
             ]),
    ],
))


# ---------------------------------------------------------------------------
# GEO-IOP-SYNC-DONE-OK — geo admin mutation returns a synchronously-completed
# Operation{done:true, metadata.regionId, response=public Region} (GEO-1-16): the
# client unwraps .response, no op-poll needed.
# verifies GEO-1-16
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="GEO-IOP-SYNC-DONE-OK",
    title="InternalRegionService.Create → Operation{done:true} synchronously (metadata.regionId + response=public Region); unwrap .response (GEO-1-16)",
    classes=["STATE", "CRUD"], priority="P0",
    steps=[
        Step(name="create-region-for-op", method="POST", path="/geo/v1/internal/regions", internal=True,
             body={"id": "qa-reg-op-{{runId}}", "name": "QA Region Op {{runId}}", "countryCode": "RU", "status": "UP"},
             test_script=[
                 *assert_operation_envelope(),
                 "const j = pm.response.json();",
                 "pm.test('Operation done:true synchronously (config-INSERT, no saga)', () => pm.expect(j.done).to.eql(true));",
                 "pm.test('metadata.regionId available immediately', () => pm.expect(j.metadata.regionId).to.eql('qa-reg-op-' + pm.environment.get('runId')));",
                 "pm.test('result.response unwraps to the public Region (id matches)', () => {",
                 "  pm.expect(j.response, JSON.stringify(j)).to.be.an('object');",
                 "  pm.expect(j.response.id).to.eql('qa-reg-op-' + pm.environment.get('runId'));",
                 "});",
                 "pm.test('response is the public Region (openForPlacement true; NO raw status)', () => {",
                 "  pm.expect(j.response.openForPlacement).to.eql(true);",
                 "  pm.expect(j.response).to.not.have.property('status');",
                 "});",
             ]),
        Step(name="cleanup", method="DELETE", path="/geo/v1/internal/regions/qa-reg-op-{{runId}}", internal=True,
             test_script=[*assert_operation_envelope()]),
    ],
))
