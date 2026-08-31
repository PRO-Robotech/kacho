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

АКТОР. Дефолт коллекции — ПРОЕКТНЫЙ (`jwtProjectAdminA1`, editor @ project-A1/A2), то есть
жизненный цикл Instance проверяется под тем принципалом, для которого он и предназначен:
`InstanceService.Create` гейтится `editor` на `project:<projectId>`, а Get/Update/Delete —
пообъектными `v_get`/`v_update`/`v_delete`, материализуемыми из привязки роли (миграция
0053 seeds селекторы `edit` на `compute.instance`). Cluster-admin остаётся ровно на
админ-CRUD каталога размерностей (`auth=ADMIN_AUTH`) — он гейтится `system_admin` на
cluster-singleton и проектному актору не принадлежит.
"""

CASES = []

INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"      # admin seed (:8081, ban #6)
MT = "/compute/v1/machineTypes"                   # public read (:8080)

# well-formed mt- (родовой prefix валиден), НИКОГДА не резолвится каталогом — sync-негативы
# падают в ValidateCreateInstanceReq ДО doCreate, поэтому реальный mt не нужен.
_PLACEHOLDER_MT = "mt-placeholder0000000"
_BOOT_STORAGE = {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"}
_BOOT_REGISTRY = {"type": "registry.image", "id": "ml/bert-trainer:cu121"}
# sshPublicKeys больше НЕ приём (compute не доставляет ключи в гостя) — значение
# живёт только в негативах, доказывающих отказ.
_SSH = ["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexampledeadbeefkey ml@team"]
_SA_WELLFORMED = "svate85k1x8bphdnn0wp"           # well-formed sva- (existence НЕ проверяется в COMP-1)


# --- self-seed helpers -----------------------------------------------------

def _seed_mt(suffix, family="STANDARD", vcpu=2, mem=8192, gpus=0, id_var="mtId", name_var=None):
    """Seed a MachineType via InternalMachineTypeService.Create (:8081) → sets id_var to mt- id
    (checked !error via assert_op_success). {{runId}}-уникальное имя (UNIQUE(name) cluster-wide).

    Актор — cluster-admin (`ADMIN_AUTH`), в отличие от остальных шагов кейса: админ-CRUD
    каталога размерностей гейтится `system_admin` на cluster-singleton, а дефолт коллекции
    проектный. Опрос Operation несёт ТОГО ЖЕ актора — владелец операции есть создавший её
    принципал, и чужому `OperationService.Get` отвечает NotFound (no-leak), а не отказом."""
    nm = f"mt{suffix}{{{{runId}}}}"
    body = {"name": nm, "family": family,
            "effectiveResources": {"vCpu": vcpu, "memoryMib": mem, "gpus": gpus},
            "availableZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"], "status": "AVAILABLE"}
    ts = [*assert_status(200), *save_from_response("j.id", "opId"),
          *save_from_response("j.metadata && j.metadata.machineTypeId", id_var)]
    if name_var:
        ts.append(f"pm.environment.set('{name_var}', 'mt{suffix}' + pm.environment.get('runId'));")
    return [Step(name=f"seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
                 auth=ADMIN_AUTH, test_script=ts),
            poll_operation_until_done(auth=ADMIN_AUTH), assert_op_success(auth=ADMIN_AUTH)]


def _cleanup_mt(id_var="mtId", name="cleanup-mt"):
    return [Step(name=name, method="DELETE", path=MT_INT + "/{{" + id_var + "}}", internal=True,
                 auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(auth=ADMIN_AUTH)]


def _vm_body(suffix, mt="{{mtId}}", name=None, ack=True, boot=None, nic=True, extra=None):
    b = {"projectId": "{{_suiteProjectId}}",
         "name": name if name is not None else f"insvm{suffix}{{{{runId}}}}",
         "zoneId": "{{existingZoneId}}", "instanceKind": "VM", "machineTypeId": mt,
         "bootSource": dict(boot) if boot is not None else dict(_BOOT_STORAGE),
         "vmSpec": {"userData": "#cloud-config\n{}",
                    "metadataOptions": {"metadataEndpoint": "ENABLED"}}}
    if nic:
        b["networkInterfaceSpecs"] = [{"subnetId": "{{existingSubnetId}}", "securityGroupIds": ["{{existingSgId}}"]}]
    if ack:
        # Страж достижимости снимается ПРИЗНАНИЕМ. Раньше его снимал sshPublicKeys —
        # список ключей, который compute никуда не доставляет: страж отпускал ровно
        # тот случай, ради которого заведён.
        b["acknowledgeUnreachable"] = True
    if extra:
        b.update(extra)
    return b


def _container_body(suffix, mt="{{mtId}}", name=None):
    return {"projectId": "{{_suiteProjectId}}",
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


# Зона, которой у geo нет: код полосы контрактом ещё не зафиксирован (3 либо 9),
# а ТЕКСТ владельца зафиксирован — `Zone %s not found`.
#
# `msg_substr` здесь непригоден by construction: он приводит к нижнему регистру
# ОБЕ стороны, поэтому заглавную `Z` не различает ни при каком ответе, а
# `assert_op_error_oneof` формы `msg_regex` не несёт. Поэтому утверждение о
# тексте дописывается к шагу — тем же `j`, который шаг уже разобрал.
def _zone_unknown_op_error():
    step = assert_op_error_oneof([3, 9], "INVALID_ARGUMENT|FAILED_PRECONDITION")
    step.test_script.append(
        "pm.test('текст владельца дословно (Zone с заглавной)', () => "
        "pm.expect(j.error && j.error.message || '', JSON.stringify(j))"
        ".to.eql('Zone no-such-zone not found'));")
    return step


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
                         "pm.test('vmSpec present, metadataOptions ENABLED (vendor-agnostic F9)', () => { pm.expect(j.vmSpec, 'vmSpec').to.be.an('object'); pm.expect(j.vmSpec.metadataOptions.metadataEndpoint).to.eql('ENABLED'); pm.expect(j.vmSpec.metadataOptions.metadataTokenRequired, 'ручка обязательности токена снята с контракта — её возврат означал бы, что защиту снова можно отключить').to.be.oneOf([undefined, null]); });",
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
    id="INST-RD-CR-VAL-CONTAINER-KIND-REFUSED",
    title="Create CONTAINER → sync 400 INVALID_ARGUMENT ПО ВИДУ: поле 'instance_kind', текст "
          "'instanceKind CONTAINER is not creatable yet: a registry image has no durable address today'. "
          "СЧАСТЛИВЫЙ ПУТЬ КОНТЕЙНЕРА СЕГОДНЯ НЕ КОНСТРУИРУЕТСЯ, и отказ висит на ВИДЕ, а не на источнике "
          "ОС: пока отказывал источник, пара «вид CONTAINER + образ ХРАНИЛИЩА» проходила проверку целиком "
          "и создавала машину, не описываемую ни одной ветвью модели. Кейс утверждает ДЕЙСТВУЮЩИЙ контракт "
          "вместо счастливого пути — и обязан покраснеть в тот день, когда ветвь откроется. "
          "[verifies COMP-1-02/15 частично · decision-table вид×источник]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[
        *_seed_mt("ctok", family="GPU", vcpu=8, mem=98304, gpus=8),
        Step(name="create-container-refused", method="POST", path=INSTANCES,
             body=_container_body("ok"),
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('текст называет ВИД и причину, а не общий отказ', () => { const m=(pm.response.json().message||''); pm.expect(m).to.include('instanceKind CONTAINER is not creatable yet'); pm.expect(m).to.include('durable address'); });",
                          "pm.test('деталь несёт имя поля', () => { const d=(pm.response.json().details||[])[0]||{}; const v=(d.fieldViolations||[])[0]||{}; pm.expect(v.field).to.eql('instance_kind'); });"]),
        # ВТОРОЙ ПРИЗНАК, РАЗВЕДЁННЫЙ С ПЕРВЫМ. Пока отказывал только источник,
        # фикстура несла оба признака сразу и зеленела бы при любом из двух; здесь
        # вид тот же, а источник — образ ХРАНИЛИЩА, и отказ обязан остаться прежним.
        # Без этого шага «отвергается по виду» неотличимо от «отвергается по источнику».
        Step(name="create-container-storage-source-refused", method="POST", path=INSTANCES,
             body={**_container_body("st"), "bootSource": dict(_BOOT_STORAGE)},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('вид отвергнут и при образе ХРАНИЛИЩА', () => { const m=(pm.response.json().message||''); pm.expect(m).to.include('instanceKind CONTAINER is not creatable yet'); });",
                          "pm.test('поле по-прежнему вид, а не источник', () => { const d=(pm.response.json().details||[])[0]||{}; const v=(d.fieldViolations||[])[0]||{}; pm.expect(v.field).to.eql('instance_kind'); });"]),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-VM-REGISTRY-SOURCE-REFUSED",
    title="Create VM с bootSource registry.image → sync 400 INVALID_ARGUMENT по имени поля "
          "'boot_source.type': 'bootSource.type registry.image is not accepted yet: a registry image has "
          "no durable address today'. ОТКАЗ ПО ИСТОЧНИКУ ЖИВ И ДОСТИЖИМ — просто не через вид CONTAINER, "
          "который короткозамыкается раньше. Кейс держит эту ветвь под утверждением: без него отказ по "
          "источнику перестал бы проверяться сквозной пробой вовсе, а он остаётся частью контракта. "
          "[verifies COMP-1-10 · ECP формы источника]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[Step(name="cr-vm-registry-source", method="POST", path=INSTANCES,
                body=_vm_body("regsrc", mt=_PLACEHOLDER_MT, boot=_BOOT_REGISTRY),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('текст называет ПОЛЕ и причину, а не общий отказ', () => { const m=(pm.response.json().message||''); pm.expect(m).to.include('bootSource.type registry.image is not accepted yet'); pm.expect(m).to.include('durable address'); });",
                             "pm.test('деталь несёт имя поля', () => { const d=(pm.response.json().details||[])[0]||{}; const v=(d.fieldViolations||[])[0]||{}; pm.expect(v.field).to.eql('boot_source.type'); });"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-REQUIRED",
    title="COMP-1-03: Create без instanceKind → sync 400 INVALID_ARGUMENT 'instanceKind is required' "
          "(сильный первый required-дискриминатор). [verifies COMP-1-03 · ECP required-field]",
    classes=["VAL", "NEG"], priority="P0",
    steps=[Step(name="cr-no-kind", method="POST", path=INSTANCES,
                body={k: v for k, v in _vm_body("nk", mt=_PLACEHOLDER_MT).items() if k != "instanceKind"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: instanceKind is required', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('instanceKind is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-VM-WITH-CONTAINERSPEC",
    title="COMP-1-03: Create instanceKind=VM с ТОЛЬКО containerSpec (wrong-arm, vmSpec опущен) → sync 400 "
          "'containerSpec is not allowed when instanceKind is VM' (spoken-exclusion в ValidateCreateInstanceReq). "
          "vmSpec СНЯТ из тела: proto `spec`-oneof допускает лишь одну ветку, оба arm'а разом → protojson "
          "'oneof already set' (generic-parse), а не app-контракт — поэтому шлём только неверную ветку. "
          "[verifies COMP-1-03 · decision-table kind×spec]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-vm-ct", method="POST", path=INSTANCES,
                body={**{k: v for k, v in _vm_body("vmct", mt=_PLACEHOLDER_MT).items() if k != "vmSpec"},
                      "containerSpec": {"command": ["x"]}},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: containerSpec not allowed when VM', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('containerSpec is not allowed when instanceKind is VM'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-KIND-CONTAINER-WITH-VMSPEC",
    title="COMP-1-03: Create instanceKind=CONTAINER с чужой ветвью spec (vmSpec, containerSpec опущен) → sync "
          "400 ПО ВИДУ: 'instanceKind CONTAINER is not creatable yet'. Клетка «вид CONTAINER × чужая ветвь» "
          "решается ВИДОМ: отказ по виду короткозамыкает раньше сверки ветви, и проверки «vmSpec не разрешён "
          "при CONTAINER» в коде НЕТ — ветвь, до которой нет достижимого пути, была бы документацией "
          "несуществующего поведения. Живая клетка зеркала (VM × containerSpec) проверяется своим кейсом "
          "INST-RD-CR-VAL-KIND-VM-WITH-CONTAINERSPEC. containerSpec СНЯТ из тела (иначе оба spec-arm'а → "
          "protojson 'oneof already set', а не app-контракт). [verifies COMP-1-03 · decision-table kind×spec]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-ct-vm", method="POST", path=INSTANCES,
                body={**{k: v for k, v in _container_body("ctvm", mt=_PLACEHOLDER_MT).items() if k != "containerSpec"},
                      "vmSpec": {"userData": "x"}},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: kind CONTAINER refused before the spec-arm check', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('instanceKind CONTAINER is not creatable yet: a registry image has no durable address today'));",
                             "pm.test('деталь несёт имя поля вида', () => { const d=(pm.response.json().details||[])[0]||{}; const v=(d.fieldViolations||[])[0]||{}; pm.expect(v.field).to.eql('instance_kind'); });"])],
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
                             "pm.test('text: machineTypeId is required', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('machineTypeId is required'));"])],
))

# INST-RD-CR-VAL-RAW-SIZING-RETIRED — СНЯТ (предмет исчерпан).
#
# Кейс слал снятые редизайном `platformId`/`resourcesSpec` и ожидал
# `400 machineTypeId is required`. Его собственным предметом была СНИСХОДИТЕЛЬНОСТЬ
# КРАЯ: он существовал потому, что acceptance-формулировка «легаси-поле → 400 unknown
# field» через край не наблюдаема (тело разбирается с DiscardUnknown, ключ вне контракта
# молча выбрасывается), и «ретайр» приходилось локать окольно — через требование другого
# поля. То есть единственное, что кейс добавлял сверх соседей, он мог показать, только
# сам совершив нарушение, которое теперь меряется статически.
#
# Что покрывает предмет после снятия:
#   * `machineTypeId` — единственный канал sizing и он обязателен:
#     INST-RD-CR-VAL-MACHINETYPE-REQUIRED (то же 400 + тот же текст; `machineTypeId: ""` и
#     полностью отсутствующий ключ на wire неразличимы — protojson даёт скаляру
#     нулевое значение, так что отдельного класса ввода тут нет);
#   * `platformId`/`resourcesSpec`/`bootDiskSpec` не вернутся в контракт:
#     CreateInstanceRequest резервирует их по номеру И ИМЕНИ (`reserved`) — protobuf
#     не даст переиспользовать имя, это сильнее любого чёрного ящика;
#   * поле, на которое сервис не смотрит, отвергается явно и с именем поля:
#     семейство INST-RD-CR-VAL-UNSUPPORTED-* (6 полей + «все шесть разом»,
#     400 + fieldViolation) — нормативный исход по api-conventions
#     §«Принято-и-проигнорировано — ЗАПРЕЩЕНО»;
#   * ни одна фикстура не шлёт краю ключ вне контракта: гейт
#     gateway/internal/restmux/newman_body_contract_test.go, по всем suite'ам сразу.
#
# Чего покрытие ещё НЕ содержит и почему это не восстанавливается здесь: «старый клиент
# прислал снятое имя → край НАЗЫВАЕТ ему поле», то есть тот самый нормативный исход, но
# на границе. Сегодня край такой запрос принимает молча, поэтому написать этот кейс
# нечем: ожидание пришлось бы выдумать. Он должен появиться, когда край начнёт отвергать
# неизвестные ключи, — и тогда ему понадобится способ жить рядом с гейтом (осознанная
# проба обязана слать «плохое» тело). Симметричная возможность у гейта уже есть для
# маршрутов: пробы `*-METHOD-PUT-NOT-ALLOWED` он относит в отдельную корзину
# `unattributed` и вердикта по ним не выносит. Предложение — в отчёте ветки.

CASES.append(Case(
    id="INST-RD-CR-VAL-CPU-GUARANTEE-OVER",
    title="COMP-1-08: Create с cpuGuaranteePercent=101 (вне {0..100}) → sync 400 "
          "'cpuGuaranteePercent must be between 0 and 100' (CHECK; отвергается, не clamp). "
          "[verifies COMP-1-08 · BVA max+1]",
    classes=["VAL", "BVA", "NEG"], priority="P1",
    steps=[Step(name="cr-cpu-over", method="POST", path=INSTANCES,
                body=_vm_body("cpu", mt=_PLACEHOLDER_MT, extra={"cpuGuaranteePercent": 101}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: cpuGuaranteePercent 0..100', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('cpuGuaranteePercent must be between 0 and 100'));"])],
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
                             "pm.test('text: bootSource is required', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('bootSource is required'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-STORAGE-ID-FORM",
    title="Форму идентификатора источника решает ЕГО ВЛАДЕЛЕЦ: образ хранилища адресуется своим "
          "неизменяемым идентификатором, и голый идентификатор БЕЗ тега — законный вход (положительный "
          "контроль), а явно-не-идентификатор отвергается синхронно по имени поля. Здесь стоял кейс, "
          "требовавший «tag or digest» от образа хранилища: требование было неисполнимо by construction — "
          "у его контракта нет ни поля тега, ни поля дайджеста, — и кейс закреплял дефект. "
          "[verifies COMP-1-10 · ECP формы идентификатора]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        Step(name="cr-boot-malformed", method="POST", path=INSTANCES,
             body=_vm_body("mal", mt=_PLACEHOLDER_MT, boot={"type": "storage.image", "id": "не идентификатор"}),
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          # Дословно и без приведения регистра: владелец пишет ресурс с
                          # ЗАГЛАВНОЙ (`invalid Image id`), и приведение прятало ровно это.
                          '''pm.test('текст называет ресурс и присланную строку', () => { const m=(pm.response.json().message||''); pm.expect(m, m).to.eql("invalid Image id 'не идентификатор'"); });''']),
        # Положительный контроль в паре: без него отрицание выше зеленело бы на
        # проверке, отвергающей ЛЮБОЙ идентификатор образа.
        #
        # Тип машины СЕЯТЬСЯ обязан: без него `{{mtId}}` берётся от соседнего
        # кейса либо не задан вовсе, и «прошло» означало бы «прошло на чужой
        # фикстуре». Созданная машина снимается — утёкший ресурс сдвигает
        # списочные контракты соседних кейсов.
        *_seed_mt("bareid"),
        *_create_inst_steps("cr-boot-bare-id-ok",
                            _vm_body("bare", boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p"})),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-UNKNOWN-TYPE",
    title="COMP-1-10: Create с bootSource.type='vm.image' (вне whitelist) → sync 400 "
          "'bootSource.type must be one of storage.image, registry.image'. [verifies COMP-1-10 · ECP type-whitelist]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-boot-badtype", method="POST", path=INSTANCES,
                body=_vm_body("bt", mt=_PLACEHOLDER_MT, boot={"type": "vm.image", "id": "img-x:tag"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: bootSource.type must be one of', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('bootSource.type must be one of storage.image, registry.image'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-VAL-BOOTSOURCE-OUTPUT-FIELDS",
    title="COMP-1-11: Create с output-only подполем bootSource (name; imageKind; оба сразу) → sync 400, "
          "и отказ называет ИМЕННО присланное подполе — текстом и путём в fieldViolations, "
          "а НЕ обязательного родителя bootSource. [verifies COMP-1-11 · #1625 · #1724 · "
          "error-guessing output-field-reject]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-boot-out", method="POST", path=INSTANCES,
                body=_vm_body("out", mt=_PLACEHOLDER_MT,
                              boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts", "name": "ubuntu"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: назван присланный name, и только он', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('bootSource.name is output-only and must not be set on input'));",
                             "pm.test('деталь несёт путь ПОДПОЛЯ, а не родителя', () => { const d=(pm.response.json().details||[])[0]||{}; const got=(d.fieldViolations||[]).map(v=>v.field); pm.expect(got).to.eql(['boot_source.name']); });"]),
           # imageKind — четвёртое отвергаемое поле, и до #1625 отказ о нём МОЛЧАЛ:
           # условие включало его, текст перечислял три. До #1724 отказ называл все
           # четыре ВСЕГДА, поэтому «назвал присланное» было неотличимо от «назвал
           # все» — шаг утверждает, что три чужих поля НЕ названы.
           Step(name="cr-boot-out-imagekind", method="POST", path=INSTANCES,
                body=_vm_body("outik", mt=_PLACEHOLDER_MT,
                              boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts",
                                    "imageKind": "STORAGE_IMAGE"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('text: назван присланный imageKind, и только он', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('bootSource.imageKind is output-only and must not be set on input'));",
                             "pm.test('поля, которых клиент не слал, НЕ названы', () => { const m=pm.response.json().message||''; ['name','resolvedDigest','materializedVolume'].forEach(f => pm.expect(m, f).to.not.include('bootSource.'+f)); });",
                             "pm.test('деталь несёт путь ПОДПОЛЯ, а не родителя', () => { const d=(pm.response.json().details||[])[0]||{}; const got=(d.fieldViolations||[]).map(v=>v.field); pm.expect(got).to.eql(['boot_source.image_kind']); });"]),
           # Два подполя сразу — один заход вместо круга запроса на каждое.
           Step(name="cr-boot-out-two", method="POST", path=INSTANCES,
                body=_vm_body("outtwo", mt=_PLACEHOLDER_MT,
                              boot={"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts",
                                    "name": "ubuntu", "imageKind": "STORAGE_IMAGE"}),
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('оба присланных подполя названы за один заход', () => {",
                             "  const det = (pm.response.json().details||[]).find(d => (d['@type']||'').includes('BadRequest'));",
                             "  pm.expect(det, 'BadRequest detail').to.be.an('object');",
                             "  pm.expect((det.fieldViolations||[]).map(v=>v.field)).to.eql(['boot_source.name','boot_source.image_kind']);",
                             "});",
                             "pm.test('родитель bootSource нарушителем не назван', () => { const det=(pm.response.json().details||[]).find(d => (d['@type']||'').includes('BadRequest'))||{}; pm.expect((det.fieldViolations||[]).map(v=>v.field)).to.not.include('boot_source'); });"])],
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
    title="COMP-1-14: Create VM БЕЗ external, БЕЗ acknowledgeUnreachable → sync 400 "
          "FAILED_PRECONDITION 'VM will be RUNNING but unreachable ...'. [verifies COMP-1-14 · error-guessing 'boots≠usable']",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-unreachable", method="POST", path=INSTANCES,
                body=_vm_body("unr", mt=_PLACEHOLDER_MT, ack=False),
                test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                             "pm.test('text mentions unreachable', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('VM will be RUNNING but unreachable (no external address); set acknowledgeUnreachable:true to proceed'));"])],
))

CASES.append(Case(
    id="INST-RD-CR-CRUD-UNREACHABLE-ACK",
    title="COMP-1-14: Create VM БЕЗ external + acknowledgeUnreachable=true → guard снят → done "
          "(bastion-only легальный кейс). [verifies COMP-1-14 · state negative→positive]",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_seed_mt("ack"),
        *_create_inst_steps("create", _vm_body("ack")),
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
                             "pm.test('text: needs an existing subnet+SG', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('needs an existing subnet+SG in zone ' + pm.environment.get('existingZoneId') + '; discover via SubnetService.List / SecurityGroupService.List, create via SubnetService.Create \u2014 or set useDefaultNetwork:true'));"])],
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
                             "pm.test('text: sizeGiB must be > 0', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('secondaryVolumeSpecs[].sizeGiB must be > 0'));"])],
))


# ===========================================================================
# Непринимаемые поля Create — отказ вместо молчаливого выбрасывания
#
# Шесть легаси-полей CreateInstanceRequest не имеют в compute ни одного читателя
# (проверено grep по services/compute вне тестов). Раньше они принимались и молча
# выбрасывались: клиент получал 200 + Operation и был уверен, что параметр применён.
# «Принято-и-проигнорировано» — не исход (api-conventions.md); поля остаются в
# контракте, но отвергаются явно, как docs/engineering/architecture/07-known-divergences.md уже
# обещал по filesystemSpecs.
#
# Контракт отказа: 400 / code 3, сообщение ОБОБЩЁННОЕ («invalid argument»), имя поля
# — в google.rpc.BadRequest.fieldViolations[].field (snake_case). Кейсы проверяют ОБЕ
# половины: деталь (без неё клиент не узнает, что именно отвергли) и точный текст
# (он поля не называет — прикладному парсеру не за что цепляться в прозе).
# ===========================================================================

def _unsupported_field_case(case_id, suffix, json_key, proto_field, value, why):
    """Один непринимаемый ключ поверх ВАЛИДНОГО тела VM → sync 400 + field violation.

    Тело валидно во всём остальном (placeholder-mt достаточно: отказ срабатывает
    ДО резолва каталога), поэтому единственная причина 400 — сам ключ.
    """
    return Case(
        id=case_id,
        title=f"Create с {json_key} → sync 400 INVALID_ARGUMENT + fieldViolation '{proto_field}'; "
              f"сообщение обобщённое (имя поля живёт в details, не в тексте). {why} "
              "[class:NEG · accepted-and-ignored ban]",
        classes=["VAL", "NEG"], priority="P1",
        steps=[Step(name=f"cr-{proto_field.replace('_', '-')}", method="POST", path=INSTANCES,
                    body=_vm_body(suffix, mt=_PLACEHOLDER_MT, extra={json_key: value}),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                                 *assert_field_violation(proto_field),
                                 "pm.test('message stays generic (field name lives in details)', () => "
                                 "pm.expect(pm.response.json().message).to.eql('invalid argument'));"])],
    )


CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-NETWORK-SETTINGS", "netset",
    "networkSettings", "network_settings", {"type": "SOFTWARE_ACCELERATED"},
    "compute не конфигурирует сетевое ускорение."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-FILESYSTEM-SPECS", "fsspec",
    "filesystemSpecs", "filesystem_specs",
    [{"mode": "READ_WRITE", "deviceName": "fs0", "filesystemId": "fs-1"}],
    "домена Filesystem в compute нет (контракт FilesystemService удалён)."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-LOCAL-DISK-SPECS", "lclspec",
    "localDiskSpecs", "local_disk_specs", [{"size": "107374182400"}],
    "compute не провижнит host-local диски."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-MAINTENANCE-POLICY", "mntpol",
    "maintenancePolicy", "maintenance_policy", "MIGRATE",
    "обслуживание хоста — не предмет control-plane."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-MAINTENANCE-GRACE-PERIOD", "mntgrc",
    "maintenanceGracePeriod", "maintenance_grace_period", "60s",
    "обслуживание хоста — не предмет control-plane."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-SERIAL-PORT-SETTINGS", "serprt",
    "serialPortSettings", "serial_port_settings", {"sshAuthorization": "OS_LOGIN"},
    "compute не конфигурирует доступ к последовательному порту."))

CASES.append(_unsupported_field_case(
    "INST-RD-CR-VAL-UNSUPPORTED-SSH-PUBLIC-KEYS", "sshkey",
    "sshPublicKeys", "ssh_public_keys", list(_SSH),
    "compute не доставляет ключи в гостя: ни колонки, ни потребителя — а страж достижимости "
    "поле раньше УДОВЛЕТВОРЯЛО, то есть отпускал ровно тот случай, ради которого заведён."))

CASES.append(Case(
    id="INST-RD-CR-VAL-UNSUPPORTED-ALL-SIX-AT-ONCE",
    title="Create со ВСЕМИ семью непринимаемыми полями → одна 400 с семью fieldViolations "
          "(легаси-клиент узнаёт про все за один заход, а не по одному на запрос); "
          "проверка идёт ПЕРВОЙ — instanceKind снят, но отвечают всё равно непринимаемые поля, "
          "не instance_kind. [class:NEG · accepted-and-ignored ban]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="cr-all-six", method="POST", path=INSTANCES,
                body={k: v for k, v in _vm_body("all6", mt=_PLACEHOLDER_MT, extra={
                    "networkSettings": {"type": "SOFTWARE_ACCELERATED"},
                    "filesystemSpecs": [{"mode": "READ_WRITE", "deviceName": "fs0", "filesystemId": "fs-1"}],
                    "localDiskSpecs": [{"size": "107374182400"}],
                    "maintenancePolicy": "MIGRATE",
                    "maintenanceGracePeriod": "60s",
                    "serialPortSettings": {"sshAuthorization": "OS_LOGIN"},
                    "sshPublicKeys": list(_SSH),
                }).items() if k != "instanceKind"},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             "pm.test('all seven unsupported fields reported at once, and nothing else', () => {",
                             "  const j = pm.response.json();",
                             "  const det = (j.details || []).find(d => (d['@type']||'').includes('BadRequest'));",
                             "  pm.expect(det, 'BadRequest detail').to.be.an('object');",
                             "  const got = (det.fieldViolations || []).map(v => v.field).sort();",
                             "  pm.expect(got).to.eql(['filesystem_specs','local_disk_specs','maintenance_grace_period',"
                             "'maintenance_policy','network_settings','serial_port_settings','ssh_public_keys']);",
                             "});"])],
))


# ===========================================================================
# F8 — ins- prefix + malformed-first (COMP-1-22)
# ===========================================================================

CASES.append(Case(
    id="INST-RD-GET-VAL-MALFORMED-ID",
    title="COMP-1-22: Get с malformed instanceId 'bad_instance_id' → 400 INVALID_ARGUMENT 'invalid resource id ...' "
          "первым стейтментом (gateway prefix-router corevalidate.ResourceID — family-agnostic, поэтому generic "
          "'resource', не 'instance'; либо authz-first 403 — scope_extractor на compute_instance/instance_id). "
          "[verifies COMP-1-22 · malformed-first + authz-first tolerance]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="get-malformed", method="GET", path=f"{INSTANCES}/bad_instance_id",
                test_script=["pm.test('rejected 400 or authz-first 403', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
                             "if (pm.response.code === 400) { pm.test('code 3 + invalid resource id', () => { const j=pm.response.json(); pm.expect(j.code).to.eql(3); pm.expect((j.message||'').toLowerCase()).to.include('invalid resource id'); }); }"])],
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
           _zone_unknown_op_error()],
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
    title="COMP-1-04/26/27: Update immutable/unknown/STOPPED-gate матрица на живом инстансе — "
          "instance_kind→400 immutable · zone_id→400 immutable · boot_source→400 immutable · "
          "fqdn→400 unknown-mask · machine_type_id→400 FAILED_PRECONDITION STOPPED-gate (недостижимо ⇒ "
          "always-reject). [verifies COMP-1-04/26/27 · state-transition immutable/gate]",
    classes=["STATE", "VAL", "NEG"], priority="P0",
    steps=[
        *_seed_instance("imm"),
        # updateMask ДОЛЖЕН быть camelCase в JSON: google.protobuf.FieldMask сериализуется
        # lowerCamelCase — grpc-gateway/protojson отвергает snake_case путь ('instance_kind')
        # как "FieldMask.paths contains invalid path" ДО backend'а. camelCase → gateway
        # конвертирует в snake-путь, и compute Update-хендлер уже кейсует на нём
        # (instanceUpdateKnown/immutable-switch — instance.go:388/490).
        # Утверждение в каждом шаге ведёт МАСКА. Одноимённые ключи в теле
        # (`instanceKind`/`zoneId`/`bootSource`) полями UpdateInstanceRequest не являются —
        # редизайн оставил в сообщении только мутабельные, — поэтому до сервиса они не
        # доходили и на исход не влияли. Держать их значило документировать контракт,
        # которого нет: сегодня безвредно, а при смене порядка проверок превратилось бы
        # в тихий успех. `machineTypeId` в последнем шаге — наоборот, НАСТОЯЩЕЕ поле
        # запроса, и оно там нужно: предмет шага — STOPPED-gate на реальном изменении.
        Step(name="upd-kind-immutable", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "instanceKind"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: instanceKind is immutable', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('instanceKind is immutable after Instance.Create'));"]),
        Step(name="upd-zone-immutable", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "zoneId"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: zoneId is immutable', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('zoneId is immutable after Instance.Create'));"]),
        Step(name="upd-bootsource-reinstall", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "bootSource"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('text: bootSource is immutable after Instance.Create', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('bootSource is immutable after Instance.Create'));"]),
        Step(name="upd-unknown-mask", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "fqdn", "description": "x"},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        Step(name="upd-stopped-gate", method="PATCH", path=INSTANCES + "/{{instanceId}}",
             body={"updateMask": "machineTypeId", "machineTypeId": "{{mtId}}"},
             test_script=[*assert_status(400), *assert_grpc_code(9, "FAILED_PRECONDITION"),
                          # Владелец пишет режим ЗАГЛАВНЫМИ (`must be STOPPED`) — приведение
                          # регистра не различало бы его ни при каком ответе.
                          "pm.test('text: must be STOPPED to change sizing or placement', () => pm.expect(pm.response.json().message||'', pm.response.text()).to.eql('instance must be STOPPED to change sizing or placement'));"]),
        *_delete_inst(),
        *_cleanup_mt(),
    ],
))

CASES.append(Case(
    id="INST-RD-UPD-CRUD-NEXTBOOT-DEFERRAL",
    title="COMP-1-27: Update updateMask=vmSpec → Operation done (принято с deferral, НЕ reject) → "
          "Get.statusReason содержит 'takes effect on next boot' (next-boot deferred class). "
          "[verifies COMP-1-27 · state-transition deferral]",
    classes=["CRUD", "STATE"], priority="P1",
    steps=[
        *_seed_instance("nb"),
        retry_until_authorized(Step(name="patch-vmspec", method="PATCH", path=INSTANCES + "/{{instanceId}}",
            # camelCase mask (см. immutable-matrix): snake-путь protojson отвергнет.
            body={"updateMask": "vmSpec", "vmSpec": {"userData": "#cloud-config\n{}"}},
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
# Принято-и-проигнорировано на Update — тот же запрет, вторая половина
# ===========================================================================
# Прикрытие маской было ЧАСТИЧНЫМ и потому обманчивым: known-set этих полей не
# содержит, поэтому ЯВНОЕ упоминание в маске давало generic «unknown field», — но
# при ПУСТОЙ маске (full-object PATCH) тело снова принималось и выбрасывалось.
# Один и тот же параметр отвечал по-разному в зависимости от маски, а в «тихой»
# ветке — успехом.
#
# sshPublicKeys вдобавок штамповал statusReason «takes effect on next boot»:
# продукт не просто игнорировал параметр, он ПОДТВЕРЖДАЛ его приём.

def _unsupported_update_field_case(case_id, json_key, proto_field, value, why):
    """Непринимаемый ключ в теле Update при ПУСТОЙ маске → sync 400 + fieldViolation."""
    return Case(
        id=case_id,
        title=f"Update с {json_key} (маска пустая — full-object PATCH) → sync 400 INVALID_ARGUMENT + "
              f"fieldViolation '{proto_field}'; Operation НЕ создана. {why} "
              "[class:NEG · accepted-and-ignored ban]",
        classes=["VAL", "NEG"], priority="P1",
        steps=[
            *_seed_instance(proto_field.replace("_", "")[:6]),
            retry_until_authorized(Step(name=f"upd-{proto_field.replace('_', '-')}", method="PATCH",
                path=INSTANCES + "/{{instanceId}}", body={json_key: value},
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                             *assert_field_violation(proto_field),
                             "pm.test('message stays generic (field name lives in details)', () => "
                             "pm.expect(pm.response.json().message).to.eql('invalid argument'));"])),
            *_delete_inst(),
            *_cleanup_mt(),
        ],
    )


CASES.append(_unsupported_update_field_case(
    "INST-RD-UPD-VAL-UNSUPPORTED-SSH-PUBLIC-KEYS",
    "sshPublicKeys", "ssh_public_keys", list(_SSH),
    "ключи не доставляются в гостя; прежняя метка «вступит в силу при следующей загрузке» "
    "подтверждала приём того, чего не будет."))

# Кейс про непринимаемое `metadata` СНЯТ вместе со своим предметом: поле снято с
# контракта целиком (номер и имя зарезервированы), а прежнее обоснование отсылало
# к RPC `:updateMetadata`, которого в контракте нет. Кейс, утверждающий отказ по
# полю, которого сообщение не несёт, неконструируем by construction.

CASES.append(_unsupported_update_field_case(
    "INST-RD-UPD-VAL-UNSUPPORTED-NETWORK-SETTINGS",
    "networkSettings", "network_settings", {"type": "SOFTWARE_ACCELERATED"},
    "compute не конфигурирует сетевое ускорение."))

CASES.append(_unsupported_update_field_case(
    "INST-RD-UPD-VAL-UNSUPPORTED-SERIAL-PORT-SETTINGS",
    "serialPortSettings", "serial_port_settings", {"sshAuthorization": "OS_LOGIN"},
    "compute не конфигурирует доступ к последовательному порту."))

CASES.append(Case(
    id="INST-RD-GET-VAL-UNSUPPORTED-SERIAL-PORT",
    title="GET :serialPortOutput?port=2 → sync 400 INVALID_ARGUMENT + fieldViolation 'port': ответ "
          "синтетический и от порта не зависит, поэтому принятый номер порта — обещание выбора, "
          "которого нет. [class:NEG · accepted-and-ignored ban]",
    classes=["VAL", "NEG"], priority="P2",
    steps=[
        *_seed_instance("serp"),
        retry_until_authorized(Step(name="serial-port", method="GET",
            path=INSTANCES + "/{{instanceId}}:serialPortOutput?port=2",
            test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         *assert_field_violation("port")])),
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
            path=INSTANCES + "?projectId={{_suiteProjectId}}&pageSize=1000",
            test_script=[*assert_status(200),
                         "pm.test('instances is array', () => pm.expect(pm.response.json().instances||[]).to.be.an('array'));",
                         "pm.test('contains own fresh instance', () => pm.expect((pm.response.json().instances||[]).map(x=>x.id)).to.include(pm.environment.get('instanceId')));"]),
            "instanceId"),
        retry_until_present(Step(name="list-filter-name", method="GET",
            path=INSTANCES + "?projectId={{_suiteProjectId}}&filter=name%3D%22inslst{{runId}}%22",
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
    steps=[Step(name="ps-over", method="GET", path=INSTANCES + "?projectId={{_suiteProjectId}}&pageSize=1001",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

CASES.append(Case(
    id="INST-RD-LST-PAGE-TOKEN-GARBAGE",
    title="COMP-1-35: List с garbage pageToken → 400 INVALID_ARGUMENT (DecodePageToken; ДО authz-short-circuit). "
          "[verifies COMP-1-35 · error-guessing garbage-token]",
    classes=["PAGE", "VAL"], priority="P1",
    steps=[Step(name="tok-garbage", method="GET",
                path=INSTANCES + "?projectId={{_suiteProjectId}}&pageSize=10&pageToken=!!!not-base64!!!",
                test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
))

# Фаза фильтра — `name=` и только он (acceptance F14 сведён к коду 2026-07-27,
# см. docs/engineering/architecture/07-known-divergences.md §12). Контракт наблюдаемый и СТРОГИЙ:
# не-whitelisted поле отвергается 400 с ИМЕНЕМ поля в сообщении. Прежний oneOf([200,400])
# был «формой без содержания» — он проходил и при молчаливом игнорировании фильтра,
# то есть ровно на том дефекте, ради которого писался.
CASES.append(Case(
    id="INST-RD-LST-FILTER-UNKNOWN-FIELD-REJECTED",
    title="COMP-1-36: List filter по не-whitelisted полю (instanceKind / placementGroupId) → строго 400 "
          "INVALID_ARGUMENT с именем поля в сообщении. Фаза whitelist'ит ТОЛЬКО name= (api-conventions "
          "§pagination/filter; acceptance F14 сведён к коду). Неподдерживаемое поле обязано отвергаться "
          "явно, НИКОГДА не игнорироваться молча — иначе caller получает нефильтрованную страницу под "
          "фильтром, который считает применённым. [verifies COMP-1-36 · negative filter-whitelist]",
    classes=["FILTER", "VAL", "NEG"], priority="P1",
    steps=[
        Step(name="flt-kind-rejected", method="GET",
             path=INSTANCES + "?projectId={{_suiteProjectId}}&filter=instanceKind%3D%22CONTAINER%22",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('message names the offending field', () => pm.expect(String((pm.response.json()||{}).message||'')).to.eql('Bad expression at column 1. Unknown field: \"instanceKind\"'));",
                          "pm.test('no leak', () => { const b = JSON.stringify(pm.response.json()||{}).toLowerCase(); ['sqlstate','panic','goroutine','pgx'].forEach(t => pm.expect(b).to.not.include(t)); });"]),
        Step(name="flt-pg-rejected", method="GET",
             path=INSTANCES + "?projectId={{_suiteProjectId}}&filter=placementGroupId%3D%22plg-x%22",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('message names the offending field', () => pm.expect(String((pm.response.json()||{}).message||'')).to.eql('Bad expression at column 1. Unknown field: \"placementGroupId\"'));"]),
        Step(name="flt-snake-kind-rejected", method="GET",
             path=INSTANCES + "?projectId={{_suiteProjectId}}&filter=instance_kind%3D%22CONTAINER%22",
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('snake_case spelling rejected too', () => pm.expect(String((pm.response.json()||{}).message||'')).to.eql('Bad expression at column 1. Unknown field: \"instance_kind\"'));"]),
    ],
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
    title="COMP-1-38: Delete с malformed instanceId 'bad_instance_id' → 400 'invalid resource id ...' первым "
          "стейтментом (gateway prefix-router, generic 'resource'; либо authz-first 403). "
          "[verifies COMP-1-38 · malformed-first + authz-first tolerance]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[Step(name="del-malformed", method="DELETE", path=f"{INSTANCES}/bad_instance_id",
                test_script=["pm.test('rejected 400 or authz-first 403', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
                             "if (pm.response.code === 400) { pm.test('code 3 + invalid resource id', () => { const j=pm.response.json(); pm.expect(j.code).to.eql(3); pm.expect((j.message||'').toLowerCase()).to.include('invalid resource id'); }); }"])],
))

CASES.append(Case(
    id="INST-RD-DEL-NEG-ABSENT",
    title="COMP-1-38: Delete well-formed-но-нет 'ins-doesnotexist000' → oneOf([403,404]) (authz-first tolerant); "
          "НИКОГДА 200. [verifies COMP-1-38 · authz-first tolerance]",
    classes=["NEG"], priority="P1",
    steps=[Step(name="del-absent", method="DELETE", path=f"{INSTANCES}/ins-doesnotexist000",
                test_script=["pm.test('403 or 404, never success', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));"])],
))

CASES.append(Case(
    id="MT-DEL-NEG-INUSE-RESTRICTED",
    title="Hard-DELETE machine-type, на который ссылаются живые инстансы → op-error FAILED_PRECONDITION "
          "'machine type ... is in use' (within-service FK RESTRICT instances.machine_type_id → machine_types(id); "
          "вывод из эксплуатации — status=RETIRED, не DELETE). После удаления инстанса тип освобождается. "
          "[class:NEG priority:P0 · data-integrity within-service п.1]",
    classes=["NEG", "STATE", "CONF"], priority="P0",
    steps=[
        *_seed_instance("mtfk", name="insmtfk{{runId}}"),
        # Тип занят живым инстансом — DELETE обязан быть отвергнут (не молча удалить,
        # оставив инстансы с dangling machineTypeId).
        Step(name="del-mt-inuse", method="DELETE", path=MT_INT + "/{{mtId}}", internal=True,
             auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
        assert_op_error(9, "FAILED_PRECONDITION", msg_substr="is in use", auth=ADMIN_AUTH),
        # Каталожная запись на месте — public read по-прежнему резолвит sizing инстанса.
        Step(name="mt-still-readable", method="GET", path=MT + "/{{mtId}}",
             test_script=[*assert_status(200),
                          "pm.test('machine type survived the rejected delete', () => "
                          "pm.expect(pm.response.json().id).to.eql(pm.environment.get('mtId')));"]),
        # Снимаем ссылку → тип освобождается и удаляется штатно.
        *_delete_inst(name="del-inst-mtfk"),
        Step(name="del-mt-freed", method="DELETE", path=MT_INT + "/{{mtId}}", internal=True,
             auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
        assert_op_success(auth=ADMIN_AUTH),
    ],
))
