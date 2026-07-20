# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для InstanceService — COMP-1 REDESIGN (Instance core) black-box coverage.

Покрывает tenant-facing редизайн Instance (docs/specs/sub-phase-COMP-1-instance-
machinetype-acceptance.md), НЕ легаси YC-поверхность (та живёт в cases/instance.py и
retired редизайном — см. RESULTS.md «legacy instance.py»). Все обращения — public
:8080 через api-gateway; MachineType для sizing self-seed'ится через internal admin
(:8081, InternalMachineTypeService) — каталог пуст на стенде (миграция 0015).

Трассировка COMP-1-NN (verifies-аннотация в title каждого кейса):
  F1 instanceKind oneof XOR (01-04) · F2 machineTypeId single sizing channel (05-08) ·
  F3 bootSource{type,id}+grammar (09-11) · F4 serviceAccountId Referrer (12-13) ·
  F5 unreachable-guard (14-15) · F6 launch-specs skeleton (16-17) · F8 ins- malformed-first
  (22) · F9 vendor-agnostic metadataOptions + YC-cruft field-absence (23-24) · F10 Update
  mutability-классы + STOPPED-gate (25-27) · F11 two-projection field-absence (28) · F12
  UNIQUE(project,name) dup (30) · F13 zone peer-validate (33) · F14 List listauthz +
  pagination-validate + filter (34-36) · F15 Delete hard-delete + name-recycle (37-38).

Техники (testing-product-coach): ECP/BVA (kind/sizing/cpu/name классы + границы),
decision-table (kind×spec XOR, family×cpuGuarantee), state-transition (Operation
done→durable; immutable/STOPPED-gate на Update), error-guessing (malformed vs absent id,
bare-untagged bootSource, output-only-field reject), conformance (flat-shape, createdAt
truncate, canonical mt- echo, field-absence retired YC-cruft).

Дисциплина (testing.md): read-your-writes → retry_until_authorized/_present на ПЕРВЫЙ
доступ к своему свежему ресурсу; async op-poll с задержкой; negatives НЕ оборачиваются;
authz-first → oneOf([400,403,404]) где gateway scope_extractor короткозамыкает; per-case
self-seed + cleanup; {{runId}}-уникальные имена.
"""

CASES = []

INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"      # admin seed (:8081, ban #6)

# well-formed mt- (родовой prefix валиден), НИКОГДА не резолвится каталогом — sync-негативы
# падают в ValidateCreateInstanceReq ДО doCreate, поэтому реальный mt не нужен.
_PLACEHOLDER_MT = "mt-placeholder0000000"
_BOOT_STORAGE = {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"}
_BOOT_REGISTRY = {"type": "registry.image", "id": "ml/bert-trainer:cu121"}
_SSH = ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexampledeadbeefkey ml@team"]
_SA_WELLFORMED = "svate85k1x8bphdnn0wp"           # well-formed sva- (existence НЕ проверяется в COMP-1)


# --- self-seed helpers -----------------------------------------------------

def _seed_mt(suffix, family="STANDARD", vcpu=2, mem=8192, gpus=0, id_var="mtId", name_var=None):
    """Seed a MachineType via InternalMachineTypeService.Create (:8081) → sets id_var to mt- id
    (checked !error via assert_op_success). {{runId}}-уникальное имя (UNIQUE(name) cluster-wide)."""
    nm = f"mt{suffix}{{{{runId}}}}"
    body = {"name": nm, "family": family,
            "effectiveResources": {"vCpu": vcpu, "memoryMib": mem, "gpus": gpus},
            "availableZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"], "status": "AVAILABLE"}
    ts = [*assert_status(200), *save_from_response("j.id", "opId"),
          *save_from_response("j.metadata && j.metadata.machineTypeId", id_var)]
    if name_var:
        ts.append(f"pm.environment.set('{name_var}', 'mt{suffix}' + pm.environment.get('runId'));")
    return [Step(name=f"seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
                 test_script=ts),
            poll_operation_until_done(), assert_op_success()]


def _cleanup_mt(id_var="mtId", name="cleanup-mt"):
    return [Step(name=name, method="DELETE", path=MT_INT + "/{{" + id_var + "}}", internal=True,
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done()]


def _vm_body(suffix, mt="{{mtId}}", name=None, ssh=True, boot=None, nic=True, extra=None):
    b = {"projectId": "{{_suiteFolderId}}",
         "name": name if name is not None else f"insvm{suffix}{{{{runId}}}}",
         "zoneId": "{{existingZoneId}}", "instanceKind": "VM", "machineTypeId": mt,
         "bootSource": dict(boot) if boot is not None else dict(_BOOT_STORAGE),
         "vmSpec": {"userData": "#cloud-config\n{}",
                    "metadataOptions": {"metadataEndpoint": "ENABLED", "metadataTokenRequired": True}}}
    if nic:
        b["networkInterfaceSpecs"] = [{"subnetId": "{{existingSubnetId}}", "securityGroupIds": ["{{existingSgId}}"]}]
    if ssh:
        b["sshPublicKeys"] = list(_SSH)
    if extra:
        b.update(extra)
    return b


def _container_body(suffix, mt="{{mtId}}", name=None):
    return {"projectId": "{{_suiteFolderId}}",
            "name": name if name is not None else f"insct{suffix}{{{{runId}}}}",
            "zoneId": "{{existingZoneId}}", "instanceKind": "CONTAINER", "machineTypeId": mt,
            "bootSource": dict(_BOOT_REGISTRY),
            "networkInterfaceSpecs": [{"subnetId": "{{existingSubnetId}}", "securityGroupIds": ["{{existingSgId}}"]}],
            "containerSpec": {"command": ["python", "train.py"], "args": ["--epochs=3"],
                              "env": {"WANDB_MODE": "offline"}, "restartPolicy": "ON_FAILURE"}}


def _create_inst_steps(name, body, save_op=True):
    ts = [*assert_status(200), *assert_operation_envelope(),
          *save_from_response("j.id", "opId"),
          *save_from_response("j.metadata && j.metadata.instanceId", "instanceId"),
          "pm.test('metadata.instanceId is ins- (pre-allocated at Create)', () => pm.expect(pm.environment.get('instanceId')||'').to.match(/^ins-/));"]
    return [Step(name=name, method="POST", path=INSTANCES, body=body, test_script=ts),
            poll_operation_until_done(), assert_op_success()]


def _seed_instance(suffix, kind="VM", name=None):
    """Seed mt + create Instance + poll + warm owner-tuple (retry GET). Sets mtId, instanceId."""
    steps = _seed_mt("i" + suffix)
    body = _vm_body(suffix, name=name) if kind == "VM" else _container_body(suffix, name=name)
    steps += _create_inst_steps(f"seed-inst-{suffix}", body)
    steps.append(retry_until_authorized(Step(name=f"warm-{suffix}", method="GET",
                 path=INSTANCES + "/{{instanceId}}", test_script=[*assert_status(200)])))
    return steps


def _delete_inst(name="del-inst", var="instanceId"):
    return [Step(name=name, method="DELETE", path=INSTANCES + "/{{" + var + "}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done()]


# ===========================================================================
# F1 — instanceKind oneof XOR (COMP-1-01/02/03/04)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-CRUD-VM-OK",
    title="COMP-1-01/05/09/23: Create VM (machineTypeId+bootSource storage.image+vmSpec+nic+ssh) → Operation "
          "(epd) + metadata.instanceId (ins-) сразу → poll done+success → Get: instanceKind==VM, vmSpec "
          "present (metadataOptions ENABLED), containerSpec absent (oneof), machineTypeId==mt- canonical echo, "
          "effectiveResources° mirror {vCpu:2,memoryMib:8192,gpus:0}, bootSource echo, status PROVISIONING, "
          "createdAt° усечён. [verifies COMP-1-01/05/09/23 · use-case + conformance]",
    classes=["CRUD", "CONF", "STATE"], priority="P0",
    steps=[
        *_seed_mt("vmok"),
        *_create_inst_steps("create", _vm_body("ok")),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('id matches & ins- prefix', () => { pm.expect(j.id).to.eql(pm.environment.get('instanceId')); pm.expect(j.id).to.match(/^ins-/); });",
                         "pm.test('instanceKind VM', () => pm.expect(j.instanceKind).to.eql('VM'));",
                         "pm.test('vmSpec present, metadataOptions ENABLED (vendor-agnostic F9)', () => { pm.expect(j.vmSpec, 'vmSpec').to.be.an('object'); pm.expect(j.vmSpec.metadataOptions.metadataEndpoint).to.eql('ENABLED'); pm.expect(j.vmSpec.metadataOptions.metadataTokenRequired).to.eql(true); });",
                         "pm.test('containerSpec absent (oneof XOR)', () => pm.expect(j.containerSpec).to.be.oneOf([undefined, null]));",
                         "pm.test('machineTypeId canonical mt- echo == seeded', () => { pm.expect(j.machineTypeId).to.eql(pm.environment.get('mtId')); pm.expect(j.machineTypeId).to.match(/^mt-/); });",
                         "pm.test('effectiveResources° mirror vCpu=2 memoryMib=8192 gpus=0', () => { const e=j.effectiveResources||{}; pm.expect(String(e.vCpu)).to.eql('2'); pm.expect(String(e.memoryMib)).to.eql('8192'); pm.expect(String(e.gpus||0)).to.eql('0'); });",
                         "pm.test('bootSource echo storage.image + id', () => { pm.expect(j.bootSource.type).to.eql('storage.image'); pm.expect(j.bootSource.id).to.eql('img-9k2m4x7q1n8p:22.04-lts'); });",
                         "pm.test('status PROVISIONING (durable resting, ban #9)', () => pm.expect(j.status).to.eql('PROVISIONING'));",
                         *assert_created_at_seconds()])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-CRUD-CONTAINER-OK",
    title="COMP-1-02/15: Create CONTAINER (containerSpec+bootSource registry.image, БЕЗ ssh/external) → done "
          "→ Get: instanceKind==CONTAINER, containerSpec present (command/restartPolicy ON_FAILURE), vmSpec "
          "absent (oneof), bootSource.materializedVolume absent (ephemeral rootfs); unreachable-guard НЕ "
          "применяется к CONTAINER (F5 exempt). [verifies COMP-1-02/15 · state + decision-table]",
    classes=["CRUD", "STATE"], priority="P0",
    steps=[
        *_seed_mt("ctok", family="GPU", vcpu=8, mem=98304, gpus=8),
        *_create_inst_steps("create", _container_body("ok")),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('instanceKind CONTAINER', () => pm.expect(j.instanceKind).to.eql('CONTAINER'));",
                         "pm.test('containerSpec present (command, restartPolicy)', () => { pm.expect(j.containerSpec, 'containerSpec').to.be.an('object'); pm.expect(j.containerSpec.command).to.eql(['python','train.py']); pm.expect(j.containerSpec.restartPolicy).to.eql('ON_FAILURE'); });",
                         "pm.test('vmSpec absent (oneof XOR)', () => pm.expect(j.vmSpec).to.be.oneOf([undefined, null]));",
                         "pm.test('bootSource registry.image echo', () => pm.expect(j.bootSource.type).to.eql('registry.image'));",
                         "pm.test('bootSource.materializedVolume absent for CONTAINER', () => pm.expect(j.bootSource.materializedVolume).to.be.oneOf([undefined, null]));"])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-REQUIRED",
    title="COMP-1-03: Create без instanceKind → sync 400 INVALID_ARGUMENT 'instanceKind is required' "
          "(сильный первый required-дискриминатор). [verifies COMP-1-03 · ECP required-field]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[Step(name="cr-no-kind", method="POST", path=INSTANCES,
                body={k: v for k, v in _vm_body("nk", mt=_PLACEHOLDER_MT).items() if k != "instanceKind"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: instanceKind is required', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('instancekind is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-VM-WITH-CONTAINERSPEC",
    title="COMP-1-03: Create instanceKind=VM с заполненным containerSpec → sync 400 "
          "'containerSpec is not allowed when instanceKind is VM' (oneof XOR spoken-exclusion). "
          "[verifies COMP-1-03 · decision-table kind×spec]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-vm-ct", method="POST", path=INSTANCES,
                body=_vm_body("vmct", mt=_PLACEHOLDER_MT, extra={"containerSpec": {"command": ["x"]}}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: containerSpec not allowed when VM', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('containerspec is not allowed when instancekind is vm'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-CONTAINER-WITH-VMSPEC",
    title="COMP-1-03: Create instanceKind=CONTAINER с заполненным vmSpec → sync 400 "
          "'vmSpec is not allowed when instanceKind is CONTAINER'. [verifies COMP-1-03 · decision-table kind×spec]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-ct-vm", method="POST", path=INSTANCES,
                body={**_container_body("ctvm", mt=_PLACEHOLDER_MT), "vmSpec": {"userData": "x"}},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: vmSpec not allowed when CONTAINER', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('vmspec is not allowed when instancekind is container'));"])],
))


# ===========================================================================
# F2 — machineTypeId single sizing channel (COMP-1-05/06/07/08)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-CRUD-MACHINETYPE-BYNAME",
    title="COMP-1-06: Create с machineTypeId=<стабильное имя> (не slug) → резолвится в scope, Get."
          "machineTypeId == canonical mt- slug (echo всегда mt-). [verifies COMP-1-06 · ECP alt-ref]",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_seed_mt("byname", name_var="mtName"),
        *_create_inst_steps("create", _vm_body("byname", mt="{{mtName}}")),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200),
                         "pm.test('machineTypeId echoed as canonical mt- slug (not the name)', () => { const j=pm.response.json(); pm.expect(j.machineTypeId).to.eql(pm.environment.get('mtId')); pm.expect(j.machineTypeId).to.match(/^mt-/); });"])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-MACHINETYPE-REQUIRED",
    title="COMP-1-07: Create с machineTypeId='' → sync 400 'machineTypeId is required' "
          "(единственный канал sizing). [verifies COMP-1-07 · ECP required-field]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[Step(name="cr-no-mt", method="POST", path=INSTANCES, body=_vm_body("nomt", mt=""),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: machineTypeId is required', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('machinetypeid is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-RAW-SIZING-RETIRED",
    title="COMP-1-07: Create с легаси raw-sizing (platformId/resourcesSpec/coreFraction) БЕЗ machineTypeId → "
          "sync 400 'machineTypeId is required' — raw channel retired (ban #2); gateway DiscardUnknown "
          "молча отбрасывает легаси-поля, единственный канал sizing = machineTypeId. "
          "[verifies COMP-1-07 · error-guessing retired-field]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-raw-sizing", method="POST", path=INSTANCES,
                body={**{k: v for k, v in _vm_body("raw", mt="").items() if k != "machineTypeId"},
                      "platformId": "standard-v3",
                      "resourcesSpec": {"cores": 2, "memory": 2147483648, "coreFraction": 100}},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: machineTypeId is required (raw sizing has no effect)', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('machinetypeid is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-CPU-GUARANTEE-OVER",
    title="COMP-1-08: Create с cpuGuaranteePercent=101 (вне {0..100}) → sync 400 "
          "'cpuGuaranteePercent must be between 0 and 100' (CHECK; отвергается, не clamp). "
          "[verifies COMP-1-08 · BVA max+1]",
    classes=["VAL", "BVA", "NEG"], priority="P1",
    steps=[Step(name="cr-cpu-over", method="POST", path=INSTANCES,
                body=_vm_body("cpu", mt=_PLACEHOLDER_MT, extra={"cpuGuaranteePercent": 101}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: cpuGuaranteePercent 0..100', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('cpuguaranteepercent must be between 0 and 100'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-NEG-MACHINETYPE-NOTFOUND",
    title="COMP-1-07: Create с machineTypeId=mt-<well-formed-absent> → async Operation.error "
          "FAILED_PRECONDITION 'machine type ... not found' (каталог-резолв в doCreate; peer-класс same-service). "
          "[verifies COMP-1-07 · error-guessing absent-catalog]",
    classes=["NEG"], priority="P1",
    steps=[Step(name="cr-mt-absent", method="POST", path=INSTANCES,
                body=_vm_body("mtabs", mt="{{garbageMachineTypeId}}"),
                test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
           poll_operation_until_done(),
           assert_op_error(9, "FAILED_PRECONDITION", msg_substr="machine type")],
))


# ===========================================================================
# F3 — bootSource {type,id} + grammar (COMP-1-09/10/11)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-REQUIRED",
    title="COMP-1-10: Create без bootSource → sync 400 'bootSource is required'. "
          "[verifies COMP-1-10 · ECP required-field]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-no-boot", method="POST", path=INSTANCES,
                body={k: v for k, v in _vm_body("nb", mt=_PLACEHOLDER_MT).items() if k != "bootSource"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: bootSource is required', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('bootsource is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-BARE-UNTAGGED",
    title="COMP-1-10: Create с bootSource storage.image id БЕЗ tag/digest → sync 400 "
          "'bootSource.id needs a tag or digest ...' (grammar в тексте). [verifies COMP-1-10 · error-guessing grammar]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-boot-untagged", method="POST", path=INSTANCES,
                body=_vm_body("bare", mt=_PLACEHOLDER_MT, boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: needs a tag or digest', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('needs a tag or digest'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-UNKNOWN-TYPE",
    title="COMP-1-10: Create с bootSource.type='vm.image' (вне whitelist) → sync 400 "
          "'bootSource.type must be one of storage.image, registry.image'. [verifies COMP-1-10 · ECP type-whitelist]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-boot-badtype", method="POST", path=INSTANCES,
                body=_vm_body("bt", mt=_PLACEHOLDER_MT, boot={"type": "vm.image", "id": "img-x:tag"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: bootSource.type must be one of', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('bootsource.type must be one of'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-OUTPUT-FIELDS",
    title="COMP-1-11: Create с bootSource output-only полем (name) в теле → sync 400 "
          "'... output-only and must not be set on input' (name°/resolvedDigest°/materializedVolume° "
          "server-derived). [verifies COMP-1-11 · error-guessing output-field-reject]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-boot-out", method="POST", path=INSTANCES,
                body=_vm_body("out", mt=_PLACEHOLDER_MT,
                              boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts", "name": "ubuntu"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: output-only must not be set on input', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('output-only'));"])],
))


# ===========================================================================
# F4 — serviceAccountId = class-C Referrer (COMP-1-12/13)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-CRUD-SERVICEACCOUNT",
    title="COMP-1-12: Create с serviceAccountId=<well-formed sva-> → Get.serviceAccount эхается как class-C "
          "Referrer{type:'iam.service_account', id:<sva>} (write-time snapshot; existence peer-validate → COMP-2). "
          "[verifies COMP-1-12 · use-case + conformance Referrer]",
    classes=["CRUD", "CONF"], priority="P1",
    steps=[
        *_seed_mt("sa"),
        *_create_inst_steps("create", _vm_body("sa", extra={"serviceAccountId": _SA_WELLFORMED})),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         f"pm.test('serviceAccount Referrer echoed', () => {{ pm.expect(j.serviceAccount, 'serviceAccount').to.be.an('object'); pm.expect(j.serviceAccount.id).to.eql('{_SA_WELLFORMED}'); pm.expect(j.serviceAccount.type).to.eql('iam.service_account'); }});"])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-SERVICEACCOUNT-MALFORMED",
    title="COMP-1-13: Create с serviceAccountId='not!!a!!sa!!id' (malformed) → sync 400 "
          "'invalid service account id ...' (own-side format-check). [verifies COMP-1-13 · error-guessing malformed]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-sa-malformed", method="POST", path=INSTANCES,
                body=_vm_body("samf", mt=_PLACEHOLDER_MT, extra={"serviceAccountId": "not!!a!!sa!!id"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: invalid service account id', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('invalid service account id'));"])],
))


# ===========================================================================
# F5 — unreachable-guard (COMP-1-14/15)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-VAL-UNREACHABLE-GUARD",
    title="COMP-1-14: Create VM БЕЗ sshPublicKeys И БЕЗ external, БЕЗ acknowledgeUnreachable → sync 400 "
          "FAILED_PRECONDITION 'VM will be RUNNING but unreachable ...'. [verifies COMP-1-14 · error-guessing 'boots≠usable']",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-unreachable", method="POST", path=INSTANCES,
                body=_vm_body("unr", mt=_PLACEHOLDER_MT, ssh=False),
                test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             "pm.test('text mentions unreachable', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('unreachable'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-CRUD-UNREACHABLE-ACK",
    title="COMP-1-14: Create VM БЕЗ ssh/external + acknowledgeUnreachable=true → guard снят → done "
          "(bastion-only легальный кейс). [verifies COMP-1-14 · state negative→positive]",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_seed_mt("ack"),
        *_create_inst_steps("create", _vm_body("ack", ssh=False, extra={"acknowledgeUnreachable": True})),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200), "pm.test('instanceKind VM', () => pm.expect(pm.response.json().instanceKind).to.eql('VM'));"])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))


# ===========================================================================
# F6 — launch-*Specs skeleton (COMP-1-16/17)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-VAL-NO-NETWORK",
    title="COMP-1-16: Create без networkInterfaceSpecs И без useDefaultNetwork → sync 400 FAILED_PRECONDITION "
          "'needs an existing subnet+SG in zone ...' (actionable runbook; compute НЕ авто-создаёт subnet). "
          "[verifies COMP-1-16 · error-guessing prerequisite]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-no-net", method="POST", path=INSTANCES, body=_vm_body("nonet", mt=_PLACEHOLDER_MT, nic=False),
                test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             "pm.test('text: needs an existing subnet+SG', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('needs an existing subnet'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-CRUD-USE-DEFAULT-NETWORK",
    title="COMP-1-16: Create с useDefaultNetwork=true (без явных nic-specs) → форма принята структурно → done "
          "(резолв project-default subnet+SG — COMP-2). [verifies COMP-1-16 · use-case skeleton]",
    classes=["CRUD"], priority="P1",
    steps=[
        *_seed_mt("udn"),
        *_create_inst_steps("create", _vm_body("udn", nic=False, extra={"useDefaultNetwork": True})),
        retry_until_authorized(Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200), "pm.test('status PROVISIONING', () => pm.expect(pm.response.json().status).to.eql('PROVISIONING'));"])),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-SECONDARY-VOLUME-SIZE",
    title="COMP-1-17: Create с secondaryVolumeSpecs[sizeGib=0] → sync 400 'secondaryVolumeSpecs[].sizeGiB must be > 0' "
          "(structural: human-scale GiB, не байты). [verifies COMP-1-17 · BVA min-1]",
    classes=["VAL", "BVA", "NEG"], priority="P2",
    steps=[Step(name="cr-secvol-zero", method="POST", path=INSTANCES,
                body=_vm_body("sv", mt=_PLACEHOLDER_MT,
                              extra={"secondaryVolumeSpecs": [{"sizeGib": 0, "volumeTypeId": "vt-ssd", "mountPath": "/data"}]}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: sizeGiB must be > 0', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('sizegib must be > 0'));"])],
))


# ===========================================================================
# F8 — ins- prefix + malformed-first (COMP-1-22)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-GET-VAL-MALFORMED-ID",
    title="COMP-1-22: Get с malformed instanceId 'bad_instance_id' → 400 INVALID_ARGUMENT 'invalid instance id ...' "
          "первым стейтментом (либо authz-first 403 — gateway scope_extractor на compute_instance/instance_id; "
          "malformed-first контракт строго локнут в MT-GET-VAL-MALFORMED-ID cluster-scope). "
          "[verifies COMP-1-22 · malformed-first + authz-first tolerance]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="get-malformed", method="GET", path=f"{INSTANCES}/bad_instance_id",
                test_script=["pm.test('rejected 400 or authz-first 403', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
                             "if (pm.response.code === 400) { pm.test('code 3 + invalid instance id', () => { const j=pm.response.json(); pm.expect(j.code).to.eql(3); pm.expect((j.message||'').toLowerCase()).to.include('invalid instance id'); }); }"])],
))

CASES.append(Case(
    id="INST-RD-GET-NEG-ABSENT",
    title="COMP-1-22: Get well-formed-но-нет 'ins-doesnotexist000' → oneOf([403,404]) (authz-first: scope_extractor "
          "не резолвит target→project → 403 ДО backend NOT_FOUND); НИКОГДА 200. [verifies COMP-1-22 · authz-first tolerance]",
    classes=["NEG"], priority="P1",
    steps=[Step(name="get-absent", method="GET", path=f"{INSTANCES}/ins-doesnotexist000",
                test_script=["pm.test('403 or 404, never success', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));"])],
))


# ===========================================================================
# F12 — UNIQUE(project,name) dup (COMP-1-30, public-observable часть; race → integration)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-NEG-DUP-NAME",
    title="COMP-1-30: два Create с одинаковым непустым name в одном проекте → второй async Operation.error "
          "ALREADY_EXISTS (partial UNIQUE(project_id,name) WHERE name<>'' на DB-уровне, 23505). "
          "[verifies COMP-1-30 · state-transition UNIQUE-backstop; concurrent-race → integration]",
    classes=["NEG", "CONC"], priority="P1",
    steps=[
        *_seed_mt("dup"),
        *_create_inst_steps("create-1", _vm_body("dup", name="insdup{{runId}}")),
        Step(name="create-2-dup", method="POST", path=INSTANCES, body=_vm_body("dup2", name="insdup{{runId}}"),
             test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        assert_op_error(6, "ALREADY_EXISTS"),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))


# ===========================================================================
# F13 — zone peer-validate fail-closed (COMP-1-33)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-CR-NEG-ZONE-UNKNOWN",
    title="COMP-1-33: Create c zoneId='no-such-zone' (в geo нет) → async Operation.error — compute→geo "
          "ZoneService.Get не находит зону (code 3 INVALID_ARGUMENT 'Zone ... not found' AS-IS; by-lane "
          "FAILED_PRECONDITION PHASE-0-GATED). [verifies COMP-1-33 · peer-validate fail-closed]",
    classes=["NEG"], priority="P1",
    steps=[Step(name="cr-bad-zone", method="POST", path=INSTANCES,
                body=_vm_body("badzone", mt=_PLACEHOLDER_MT, extra={"zoneId": "no-such-zone"}),
                test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
           poll_operation_until_done(),
           assert_op_error_oneof([3, 9], "INVALID_ARGUMENT|FAILED_PRECONDITION", msg_substr="not found")],
))


# ===========================================================================
# F10 — Update mutability-классы + STOPPED-gate (COMP-1-04/25/26/27)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-UPD-CRUD-LIVE-OK",
    title="COMP-1-25: Update updateMask=name,labels → Operation done → Get: name/labels обновлены "
          "(LIVE-mutable применяются сразу). [verifies COMP-1-25 · use-case LIVE-mutable]",
    classes=["CRUD"], priority="P1",
    steps=[
        *_seed_instance("upd"),
        retry_until_authorized(Step(name="patch", method="PATCH", path=INSTANCES + "/{{instanceId}}",
            body={"updateMask": "name,labels", "name": "insupd{{runId}}b", "labels": {"team": "ml", "run": "42"}},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "pm.test('name updated', () => pm.expect(j.name).to.eql('insupd' + pm.environment.get('runId') + 'b'));",
                          "pm.test('labels updated', () => { pm.expect(j.labels.team).to.eql('ml'); pm.expect(j.labels.run).to.eql('42'); });"]),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-UPD-STATE-IMMUTABLE-MATRIX",
    title="COMP-1-04/26/27: Update immutable/unknown/Reinstall/STOPPED-gate матрица на живом инстансе — "
          "instance_kind→400 immutable · zone_id→400 immutable · boot_source→400 Reinstall-only · "
          "fqdn→400 unknown-mask · machine_type_id→400 FAILED_PRECONDITION STOPPED-gate (недостижимо ⇒ "
          "always-reject). [verifies COMP-1-04/26/27 · state-transition immutable/gate]",
    classes=["STATE", "VAL", "NEG"], priority="P0",
    steps=[
        *_seed_instance("imm"),
        Step(name="upd-kind-immutable", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "instance_kind", "instanceKind": "CONTAINER"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: instanceKind is immutable', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('instancekind is immutable after instance.create'));"]),
        Step(name="upd-zone-immutable", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "zone_id", "zoneId": "ru-central1-c"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: zoneId is immutable', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('zoneid is immutable after instance.create'));"]),
        Step(name="upd-bootsource-reinstall", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "boot_source", "bootSource": {"type": "storage.image", "id": "img-x:v2"}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: bootSource cannot be changed via Update; use Reinstall', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('bootsource cannot be changed via update; use reinstall'));"]),
        Step(name="upd-unknown-mask", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "fqdn", "description": "x"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="upd-stopped-gate", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "machine_type_id", "machineTypeId": "{{mtId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          "pm.test('text: must be STOPPED to change sizing or placement', () => pm.expect((pm.response.json().message||'').toLowerCase()).to.include('instance must be stopped to change sizing or placement'));"]),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-UPD-CRUD-NEXTBOOT-DEFERRAL",
    title="COMP-1-27: Update updateMask=ssh_public_keys → Operation done (принято с deferral, НЕ reject) → "
          "Get.statusReason содержит 'takes effect on next boot' (next-boot deferred class). "
          "[verifies COMP-1-27 · state-transition deferral]",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_seed_instance("nb"),
        retry_until_authorized(Step(name="patch-ssh", method="PATCH", path=INSTANCES + "/{{instanceId}}",
            body={"updateMask": "ssh_public_keys", "sshPublicKeys": ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5newkey nb@team"]},
            test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
        poll_operation_until_done(), assert_op_success(),
        Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200),
                          "pm.test('statusReason: takes effect on next boot', () => pm.expect((pm.response.json().statusReason||'').toLowerCase()).to.include('takes effect on next boot'));"]),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))


# ===========================================================================
# F9/F11 — YC-cruft retire + two-projection field-absence (COMP-1-24/28)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-GET-CONF-FIELD-ABSENCE",
    title="COMP-1-24/28: public Instance.Get НЕ несёт retired YC-cruft (platformId/resources/resourcesSpec/"
          "coreFraction/schedulingPolicy/gpuSettings/reservedInstancePoolId/application) НИ инфра-полей "
          "(hostId/hostGroupId/placementPolicy/nodeId/topologyKey) НИ brand-токенов (yc.host/gce/aws); "
          "vmSpec.metadataOptions vendor-agnostic. [verifies COMP-1-24/28 · conformance field-absence]",
    classes=["CONF", "SEC"], priority="P0",
    steps=[
        *_seed_instance("fa"),
        Step(name="get", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=[*assert_status(200),
                          "const j = pm.response.json();",
                          "const raw = JSON.stringify(j).toLowerCase();",
                          "pm.test('no retired sizing keys', () => { ['platformId','resources','resourcesSpec','coreFraction'].forEach(k => pm.expect(j[k], k).to.be.oneOf([undefined, null])); });",
                          "pm.test('no retired scheduling/gpu/pool/app keys', () => { ['schedulingPolicy','gpuSettings','reservedInstancePoolId','application'].forEach(k => pm.expect(j[k], k).to.be.oneOf([undefined, null])); });",
                          "pm.test('no infra/placement keys (two-projection)', () => { ['hostId','hostGroupId','placementPolicy','hostAffinityRules','nodeId','topologyKey'].forEach(k => pm.expect(j[k], k).to.be.oneOf([undefined, null])); });",
                          "pm.test('no brand/infra tokens in serialized body', () => { ['yc.host','gcehttp','awsv1','awsv2','preemptible','gpuclusterid','reservedinstancepool','platformid'].forEach(t => pm.expect(raw, t).to.not.include(t)); });",
                          "pm.test('vmSpec.metadataOptions vendor-agnostic (no gce_*/aws_*)', () => { const mo = (j.vmSpec && j.vmSpec.metadataOptions) || {}; pm.expect(mo.gceHttpEndpoint).to.be.oneOf([undefined, null]); pm.expect(mo.awsV1HttpEndpoint).to.be.oneOf([undefined, null]); });"]),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))


# ===========================================================================
# F14 — List: listauthz row-filter + pagination-validate + filter (COMP-1-34/35/36)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-LST-CRUD-FILTER-OK",
    title="COMP-1-34/36: List(projectId) → 200, содержит свой свежий Instance (listauthz row-filter, anti-BOLA); "
          "List filter=name=<name> → содержит его же (whitelist name=). [verifies COMP-1-34/36 · use-case + filter]",
    classes=["CRUD", "FILTER", "PAGE"], priority="P1",
    steps=[
        *_seed_instance("lst", name="inslst{{runId}}"),
        retry_until_present(Step(name="list", method="GET",
            path=INSTANCES + "?projectId={{_suiteFolderId}}&pageSize=1000",
            test_script=[*assert_status(200),
                         "pm.test('instances is array', () => pm.expect(pm.response.json().instances||[]).to.be.an('array'));",
                         "pm.test('contains own fresh instance', () => pm.expect((pm.response.json().instances||[]).map(x=>x.id)).to.include(pm.environment.get('instanceId')));"]),
            "instanceId"),
        retry_until_present(Step(name="list-filter-name", method="GET",
            path=INSTANCES + "?projectId={{_suiteFolderId}}&filter=name%3D%22inslst{{runId}}%22",
            test_script=[*assert_status(200),
                         "pm.test('filter name= contains own instance', () => pm.expect((pm.response.json().instances||[]).map(x=>x.id)).to.include(pm.environment.get('instanceId')));"]),
            "instanceId"),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-LST-BVA-PAGESIZE-OVER-1001",
    title="COMP-1-35: List pageSize=1001 (>max 1000) → 400 INVALID_ARGUMENT (pagination-validate ДО listauthz "
          "empty-grant short-circuit; отвергается, не clamp). [verifies COMP-1-35 · BVA max+1]",
    classes=["BVA", "VAL", "PAGE"], priority="P1",
    steps=[Step(name="ps-over", method="GET", path=INSTANCES + "?projectId={{_suiteFolderId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="INST-RD-LST-PAGE-TOKEN-GARBAGE",
    title="COMP-1-35: List с garbage pageToken → 400 INVALID_ARGUMENT (DecodePageToken; ДО authz-short-circuit). "
          "[verifies COMP-1-35 · error-guessing garbage-token]",
    classes=["PAGE", "VAL"], priority="P1",
    steps=[Step(name="tok-garbage", method="GET",
                path=INSTANCES + "?projectId={{_suiteFolderId}}&pageSize=10&pageToken=!!!not-base64!!!",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="INST-RD-LST-FILTER-KIND-TOLERANT",
    title="COMP-1-36 (спорный контракт): List filter=instanceKind=CONTAINER — acceptance F14 заявляет "
          "whitelist name=/placementGroupId=/instanceKind=, но текущая реализация (api-conventions «текущая "
          "фаза — name=») whitelist'ит ТОЛЬКО name → oneOf([200,400]) (400=unknown-filter-field сейчас, "
          "200=когда добавят). Задокументировано в RESULTS.md для acceptance-author reconcile. НЕ маскирует "
          "баг — 500/leak падает. [verifies COMP-1-36 · conformance filter-whitelist gap]",
    classes=["FILTER", "VAL"], priority="P3",
    steps=[Step(name="flt-kind", method="GET",
                path=INSTANCES + "?projectId={{_suiteFolderId}}&filter=instanceKind%3D%22CONTAINER%22",
                test_script=["pm.test('200 (supported) or 400 (name-only whitelist current phase)', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
                             "pm.test('never 500 / no leak', () => { pm.expect(pm.response.code).to.not.eql(500); const b = JSON.stringify(pm.response.json()||{}).toLowerCase(); ['sqlstate','panic','goroutine'].forEach(t => pm.expect(b).to.not.include(t)); });"])],
))


# ===========================================================================
# F15 — Delete hard-delete + name-recycle (COMP-1-37/38)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-DEL-CRUD-NAME-RECYCLE",
    title="COMP-1-37: Delete → Operation done → Get NOT_FOUND (hard-delete, не tombstone); name-recycle — "
          "тот же непустой name снова Create-able в проекте (partial UNIQUE slot освобождён). "
          "[verifies COMP-1-37 · state-transition hard-delete + name-recycle]",
    classes=["CRUD", "STATE"], priority="P0",
    steps=[
        *_seed_instance("del", name="insdel{{runId}}"),
        *_delete_inst(name="delete-1"),
        Step(name="get-after-delete", method="GET", path=INSTANCES + "/{{instanceId}}",
             test_script=["pm.test('403 or 404 (hard-delete; authz-first tolerant), never 200', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));"]),
        # name-recycle: тот же непустой name снова Create-able (partial UNIQUE slot освобождён hard-delete'ом)
        *_create_inst_steps("recreate-same-name", _vm_body("del2", name="insdel{{runId}}")),
        *_delete_inst(name="delete-2"),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-DEL-VAL-MALFORMED-ID",
    title="COMP-1-38: Delete с malformed instanceId 'bad_instance_id' → 400 'invalid instance id ...' первым "
          "стейтментом (либо authz-first 403). [verifies COMP-1-38 · malformed-first + authz-first tolerance]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="del-malformed", method="DELETE", path=f"{INSTANCES}/bad_instance_id",
                test_script=["pm.test('rejected 400 or authz-first 403', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
                             "if (pm.response.code === 400) { pm.test('code 3 + invalid instance id', () => { const j=pm.response.json(); pm.expect(j.code).to.eql(3); pm.expect((j.message||'').toLowerCase()).to.include('invalid instance id'); }); }"])],
))

CASES.append(Case(
    id="INST-RD-DEL-NEG-ABSENT",
    title="COMP-1-38: Delete well-formed-но-нет 'ins-doesnotexist000' → oneOf([403,404]) (authz-first tolerant); "
          "НИКОГДА 200. [verifies COMP-1-38 · authz-first tolerance]",
    classes=["NEG"], priority="P1",
    steps=[Step(name="del-absent", method="DELETE", path=f"{INSTANCES}/ins-doesnotexist000",
                test_script=["pm.test('403 or 404, never success', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));"])],
))
