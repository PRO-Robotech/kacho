# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для MachineTypeService (kacho-compute) — COMP-1 F7 sync sizing catalog.

Covered RPCs:
  * public read  — MachineTypeService.Get / List (GET /compute/v1/machineTypes[/{id}]);
    ambient cluster-scoped read (viewer, project-scope EXEMPT parity с geo-каталогом).
  * admin CRUD   — InternalMachineTypeService.Create/Delete на cluster-internal REST
    listener ({{internalBaseUrl}} :8081, POST/DELETE /compute/v1/internal/machineTypes;
    ban #6 — НИКОГДА на публичном :8080). Async Operation (epd op-prefix) → poll.

Каталог НЕ засеян на стенде (миграция 0015 создаёт пустую таблицу) — каждый read-кейс
**self-seed'ит** свежий MachineType через internal admin Create ({{runId}}-уникальное имя,
UNIQUE(name) cluster-wide) и убирает за собой (internal Delete). Seed идёт на internal-mux;
poll /operations/{id} — на public OpsProxy (op owned дефолтным jwtBootstrap = system_admin@cluster).

Трассировка: COMP-1-18 (Get flat-проекция) · COMP-1-19 (List + filter family=/minGpus=/name=,
GPU-гранулярность) · COMP-1-20 (malformed-first + NOT_FOUND) · COMP-1-21 (admin-CRUD на Internal*).
Техники (testing-product-coach): ECP/BVA (pageSize границы), decision-table (family×minGpus),
error-guessing (malformed vs absent id), conformance (flat-shape, createdAt truncate).
"""

CASES = []

MT = "/compute/v1/machineTypes"                    # public read (:8080)
MT_INT = "/compute/v1/internal/machineTypes"        # admin CRUD (:8081, ban #6)


# --- self-seed helpers (internal admin CRUD) -------------------------------

def _seed_mt(suffix, family="STANDARD", vcpu=2, mem=8192, gpus=0, gputype="",
             zones=None, status="AVAILABLE", id_var="mtId", name_var=None):
    """Seed a MachineType via InternalMachineTypeService.Create (:8081). Sets `id_var`
    to the mt- id (from op.metadata, checked !error via assert_op_success) and, if
    `name_var` given, to the deterministic stable name ('mt<suffix>'+runId). Каждый seed —
    {{runId}}-уникальное имя (UNIQUE(name) на DB-уровне)."""
    zones = zones if zones is not None else ["{{existingZoneId}}", "{{existingZoneAltId}}"]
    nm = f"mt{suffix}{{{{runId}}}}"
    er = {"vCpu": vcpu, "memoryMib": mem, "gpus": gpus}
    if gputype:
        er["gpuType"] = gputype
    body = {"name": nm, "family": family, "effectiveResources": er,
            "availableZones": zones, "status": status}
    ts = [*assert_status(200), *assert_operation_envelope(),
          *save_from_response("j.id", "opId"),
          *save_from_response("j.metadata && j.metadata.machineTypeId", id_var)]
    if name_var:
        ts.append(f"pm.environment.set('{name_var}', 'mt{suffix}' + pm.environment.get('runId'));")
    return [
        Step(name=f"seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
             test_script=ts),
        poll_operation_until_done(),
        assert_op_success(),   # phantom-id discipline: !op.error before trusting id_var
    ]


def _cleanup_mt(id_var="mtId", name="cleanup-mt"):
    return [
        Step(name=name, method="DELETE", path=MT_INT + "/{{" + id_var + "}}", internal=True,
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
    ]


# ===========================================================================
# MT-CR — admin CRUD (InternalMachineTypeService, :8081, ban #6)
# ===========================================================================

CASES.append(Case(
    id="MT-CR-ADMIN-INTERNAL-CRUD-OK",
    title="COMP-1-21: InternalMachineTypeService.Create (:8081 internal-mux) → Operation(epd) → done → "
          "public Get отдаёт засеянный mt-; internal Delete → done (admin-CRUD живёт ТОЛЬКО на Internal*, ban #6). "
          "[class:CRUD priority:P1 verifies COMP-1-21 · use-case flow]",
    classes=["CRUD"], priority="P1",
    steps=[
        *_seed_mt("adm", name_var="mtName"),
        retry_until_authorized(Step(name="get-after-seed", method="GET", path=MT + "/{{mtId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('mt- id echoed', () => { pm.expect(j.id).to.eql(pm.environment.get('mtId')); pm.expect(j.id).to.match(/^mt-/); });",
                         "pm.test('name matches seed', () => pm.expect(j.name).to.eql(pm.environment.get('mtName')));"])),
        *_cleanup_mt(),
        # after Delete → public Get → NOT_FOUND (durable hard-delete of the catalog row).
        Step(name="get-after-delete", method="GET", path=MT + "/{{mtId}}",
             test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
    ],
))

CASES.append(Case(
    id="MT-CR-ADMIN-NEG-NO-NAME",
    title="COMP-1-21: InternalMachineTypeService.Create с пустым name → 400 INVALID_ARGUMENT "
          "'name is required' (admin Create input-validate ДО insert; НЕ мутирует). "
          "[class:VAL,NEG priority:P2 verifies COMP-1-21 · ECP required-field]",
    classes=["VAL", "NEG"], priority="P2",
    steps=[Step(name="cr-no-name", method="POST", path=MT_INT, internal=True,
                body={"name": "", "family": "STANDARD",
                      "effectiveResources": {"vCpu": 2, "memoryMib": 8192}},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text mentions name required', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('name is required'));"])],
))


# ===========================================================================
# MT-GET — public read (COMP-1-18/20)
# ===========================================================================

CASES.append(Case(
    id="MT-GET-CRUD-OK",
    title="COMP-1-18: MachineType.Get → flat public-проекция (id mt-, name, family, effectiveResources° "
          "{vCpu,memoryMib MiB,gpus}, availableZones°, status, createdAt° усечён до секунд); ambient read. "
          "[class:CRUD,CONF priority:P1 verifies COMP-1-18 · conformance flat-shape]",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        *_seed_mt("get", family="STANDARD", vcpu=2, mem=8192, gpus=0, name_var="mtName"),
        retry_until_authorized(Step(name="get", method="GET", path=MT + "/{{mtId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('id matches & mt- prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('mtId')); pm.expect(j.id).to.match(/^mt-/); });",
                         "pm.test('name matches', () => pm.expect(j.name).to.eql(pm.environment.get('mtName')));",
                         "pm.test('family STANDARD', () => pm.expect(j.family).to.eql('STANDARD'));",
                         "pm.test('effectiveResources vCpu=2', () => pm.expect(String((j.effectiveResources||{}).vCpu)).to.eql('2'));",
                         "pm.test('effectiveResources memoryMib=8192 (MiB not bytes)', () => pm.expect(String((j.effectiveResources||{}).memoryMib)).to.eql('8192'));",
                         "pm.test('gpus 0 for STANDARD', () => pm.expect(String((j.effectiveResources||{}).gpus||0)).to.eql('0'));",
                         "pm.test('availableZones contains existingZoneId', () => pm.expect(j.availableZones||[]).to.include(pm.environment.get('existingZoneId')));",
                         "pm.test('status AVAILABLE', () => pm.expect(j.status).to.eql('AVAILABLE'));",
                         *assert_created_at_seconds()])),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="MT-GET-VAL-MALFORMED-ID",
    title="COMP-1-20: MachineType.Get с malformed id 'bad!!id' → 400 INVALID_ARGUMENT "
          "'invalid machine type id ...' первым стейтментом (corevalidate.ResourceID до repo). "
          "[class:VAL,NEG priority:P0 verifies COMP-1-20 · malformed-first]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[Step(name="get-malformed", method="GET", path=f"{MT}/bad!!id",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: invalid machine type id', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('invalid machine type id'));"])],
))

CASES.append(Case(
    id="MT-GET-NEG-NOTFOUND",
    title="COMP-1-20: MachineType.Get well-formed-но-нет (mt-doesnotexist...) → 404 NOT_FOUND "
          "'<Resource> <id> not found' (через repo.Get; тон-контракт). "
          "[class:NEG,CONF priority:P1 verifies COMP-1-20 · error-guessing absent-id]",
    classes=["NEG", "CONF"], priority="P1",
    steps=[Step(name="get-absent", method="GET", path=f"{MT}/{{{{garbageMachineTypeId}}}}",
                test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                             "pm.test('text mentions not found', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('not found'));"])],
))


# ===========================================================================
# MT-LST — public list + filter (COMP-1-19)
# ===========================================================================

CASES.append(Case(
    id="MT-LST-CRUD-OK",
    title="COMP-1-19: MachineType.List → 200 массив machineTypes, содержит засеянный mt- (ambient). "
          "[class:CRUD priority:P1 verifies COMP-1-19 · use-case]",
    classes=["CRUD"], priority="P1",
    steps=[
        *_seed_mt("lst"),
        retry_until_present(Step(name="list", method="GET", path=f"{MT}?pageSize=1000",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('machineTypes is array', () => pm.expect(j.machineTypes||[]).to.be.an('array'));",
                         "pm.test('contains seeded mt id', () => pm.expect((j.machineTypes||[]).map(x=>x.id)).to.include(pm.environment.get('mtId')));"]),
            "mtId"),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="MT-LST-FILTER-FAMILY-MINGPUS-NAME",
    title="COMP-1-19: List?family=GPU дискаверит GPU-flavor'ы (не STANDARD); &minGpus=4 отсекает gpus<4; "
          "?name= точное имя. GPU-count = гранулярность каталога (gpu-a100-1/-8), не поле запроса. "
          "[class:FILTER priority:P1 verifies COMP-1-19 · decision-table family×minGpus]",
    classes=["FILTER", "CRUD"], priority="P1",
    steps=[
        *_seed_mt("std0", family="STANDARD", gpus=0, id_var="mtStdId"),
        *_seed_mt("gpu1", family="GPU", vcpu=8, mem=98304, gpus=1, gputype="a100-80g", id_var="mtGpu1Id"),
        *_seed_mt("gpu8", family="GPU", vcpu=64, mem=786432, gpus=8, gputype="a100-80g",
                  id_var="mtGpu8Id", name_var="mtGpu8Name"),
        retry_until_present(Step(name="list-gpu", method="GET", path=f"{MT}?family=GPU&pageSize=1000",
            test_script=[*assert_status(200),
                         "const ids = (pm.response.json().machineTypes||[]).map(x=>x.id);",
                         "pm.test('family=GPU contains gpu1', () => pm.expect(ids).to.include(pm.environment.get('mtGpu1Id')));",
                         "pm.test('family=GPU contains gpu8', () => pm.expect(ids).to.include(pm.environment.get('mtGpu8Id')));",
                         "pm.test('family=GPU excludes STANDARD', () => pm.expect(ids).to.not.include(pm.environment.get('mtStdId')));"]),
            "mtGpu8Id"),
        Step(name="list-gpu-min4", method="GET", path=f"{MT}?family=GPU&minGpus=4&pageSize=1000",
             test_script=[*assert_status(200),
                          "const ids = (pm.response.json().machineTypes||[]).map(x=>x.id);",
                          "pm.test('minGpus=4 contains gpu8 (gpus=8>=4)', () => pm.expect(ids).to.include(pm.environment.get('mtGpu8Id')));",
                          "pm.test('minGpus=4 excludes gpu1 (gpus=1<4)', () => pm.expect(ids).to.not.include(pm.environment.get('mtGpu1Id')));"]),
        Step(name="list-by-name", method="GET", path=f"{MT}?name={{{{mtGpu8Name}}}}&pageSize=1000",
             test_script=[*assert_status(200),
                          "const arr = pm.response.json().machineTypes||[];",
                          "pm.test('name= exact returns only gpu8', () => { pm.expect(arr.length).to.eql(1); pm.expect(arr[0].id).to.eql(pm.environment.get('mtGpu8Id')); });"]),
        *_cleanup_mt("mtStdId", name="cleanup-std0"),
        *_cleanup_mt("mtGpu1Id", name="cleanup-gpu1"),
        *_cleanup_mt("mtGpu8Id", name="cleanup-gpu8"),
    ],
))

# pageSize BVA (0/1/1000/1001) + garbage token — MachineType.List валидирует pagination
# (validate.PageSize + decodePageToken → InvalidArgument); ambient каталог → folder_param=False.
CASES.extend(list_page_block("MT", MT, folder_param=False))
