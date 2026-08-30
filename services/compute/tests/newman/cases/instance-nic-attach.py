# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set привязки/отвязки сетевого интерфейса — ребро compute→vpc :9091.

ЗАЧЕМ ЭТОТ ФАЙЛ. Ребро compute→vpc `InternalNetworkInterfaceService` держали два
ДЕКЛАРАТИВНЫХ гейта: `services/compute/deploy/peer_mtls_producer_test.go` (чарт
объявляет группу переменных клиентского сертификата) и boot-guard конфига (адрес
непуст, клиент не заглушка). Оба утверждают, что ребро СКОНФИГУРИРОВАНО, и ни один —
что оно РАБОТАЕТ: адрес может быть задан, сертификат смонтирован, а привязка не
доезжать. Здесь ребро проверяется исходом: запрос уходит из compute, доходит до vpc
по внутреннему листенеру, проходит там per-RPC проверку прав на СВОЙ объект и
возвращает контракт ВЛАДЕЛЬЦА.

ЧЕМ КЕЙСЫ РАЗЛИЧАЮТ ЖИВОЕ РЕБРО ОТ ЗАГЛУШКИ. Composition root подменяет клиента
`NoopNicClient`, когда адрес пуст; заглушка отвечает на снятие привязки
`UNAVAILABLE "network interface service not configured"`. Поэтому кейсы, чей шаг
доходит до соседа, помечены ниже `[РАЗЛИЧАЕТ РЕБРО]`: на живом ребре они зелёные, на
заглушке краснеют — операция вместо успеха несёт код 14. Доказано инъекцией на
стенде (снять адрес у compute → эти кейсы красные; вернуть → зелёные).

ЧТО СЕГОДНЯ НЕ КОНСТРУИРУЕТСЯ, И ПОЧЕМУ ЭТО СКАЗАНО, А НЕ ОБОЙДЕНО. Счастливый путь
приёмки S4-01 («инстанс в STOPPED, привязать два интерфейса») через публичный API
собрать НЕЛЬЗЯ: `Instance.Create` кладёт машину в PROVISIONING и оставляет её там
(`instance.go`, комментарий OQ1 — переход к RUNNING принадлежит саге запуска COMP-2,
которой в дереве нет), а `Start` требует STOPPED, `Stop` — RUNNING. Ни одного
перехода из PROVISIONING не существует, поэтому гейт состояния в
`AttachNetworkInterface` отвергает КАЖДУЮ машину, созданную через API. Кейс
INST-NIC-ATT-NEG-STATE-GATE утверждает ровно этот, действующий контракт и НЕ
претендует на проверку ребра: гейт состояния стоит ДО вызова соседа, поэтому на
заглушке он остаётся зелёным — это сказано здесь, чтобы его зелёное не читалось шире,
чем есть. Привязка к машине в STOPPED приезжает вместе с COMP-2, а не раньше.

Трассировка приёмки — `docs/specs/sub-phase-compute-storage-volume-attach-acceptance.md`
(APPROVED 2026-07-12), сценарии S4-06 (идемпотентное снятие, malformed-id, пустой
oneof) и S4-01 (в конструируемой сегодня части — гейт состояния).

Техники (testing-product-coach): state-transition (гейт состояния машины),
error-guessing (несуществующий и malformed идентификатор соседа), conformance
(тон и код ошибки — контракт ВЛАДЕЛЬЦА, доехавший через ребро), idempotency
(повтор снятия — тот же исход).

АКТОР. Дефолт коллекции — проектный (`jwtProjectAdminA1`, editor на project-A1/A2):
он создаёт и сеть с подсетью, и сам интерфейс, поэтому на стороне vpc он владелец
объекта и внутренняя проверка прав на `nic` отвечает ему «да». Каталог размерностей
— единственное отступление, оно объявлено шагом (`auth=ADMIN_AUTH`), потому что
админ-CRUD гейтится `system_admin` на cluster-singleton. Операцию всюду читает тот,
кто её создал: `OperationService.Get` энфорсит владение и отвечает чужому отказом.
"""

CASES = []

INSTANCES = "/compute/v1/instances"
MT_INT = "/compute/v1/internal/machineTypes"       # admin seed (:8081, ban #6)
NETS = "/vpc/v1/networks"
SUBNETS = "/vpc/v1/subnets"
NICS = "/vpc/v1/networkInterfaces"

_BOOT_STORAGE = {"type": "storage.image", "id": "img-9k2m4x7q1n8p:22.04-lts"}

# Well-formed идентификатор интерфейса, который НИКОГДА не резолвится: prefix `nic`
# ∈ каталога `ids.KnownPrefixes()`, поэтому синхронная проверка формата его
# ПРОПУСКАЕТ и запрос уходит соседу — ровно то, что и нужно, чтобы увидеть ответ
# владельца. Тело — 17 символов из алфавита crockford-base32.
_ABSENT_NIC = "nicabsent99999999999"

# Явно НЕ идентификатор: дефис есть, но `bad` не значится ни в дефисном каноне, ни в
# слитном, поэтому отказ синхронный и терминальный, до всякого обращения к соседу.
_MALFORMED_NIC = "bad-nic"


# --- посев ------------------------------------------------------------------

def _cidr_prescript(var):
    """Октеты подсети — широкая случайность на ПРОГОН, не производная от runId.

    Общий runId у параллельных коллекций давал один и тот же октет, а порядковый
    номер внутри процесса рестартовал с единицы в каждом — параллельные процессы
    сталкивались на одном блоке, и «блуждающий» отказ читался как рассогласование
    реплик. Два случайных октета дают ~56k блоков /24; сталкиваться по-прежнему
    можно, но это перестало быть детерминированным.
    """
    return [
        f"pm.environment.set('{var}', '10.' + (64 + Math.floor(Math.random() * 128)) + '.' "
        f"+ Math.floor(Math.random() * 256) + '.0/24');",
    ]


def _seed_mt(suffix):
    """Каталожная размерность под инстанс (админ-маршрут :8081, актор объявлен шагом)."""
    body = {"name": f"mtnic{suffix}{{{{runId}}}}", "family": "STANDARD",
            "effectiveResources": {"vCpu": 2, "memoryMib": 8192, "gpus": 0},
            "availableZones": ["{{existingZoneId}}", "{{existingZoneAltId}}"], "status": "AVAILABLE"}
    return [
        Step(name=f"seed-mt-{suffix}", method="POST", path=MT_INT, body=body, internal=True,
             auth=ADMIN_AUTH,
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.machineTypeId", "mtId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
        assert_op_success(auth=ADMIN_AUTH),
    ]


def _seed_nic(suffix):
    """Своя сеть + своя ZONAL-подсеть в зоне инстанса + свой интерфейс в ней.

    Посев per-case (не общий литерал из окружения): общая фикстура, однажды упавшая
    асинхронно, оставляет в окружении идентификатор несозданного ресурса, и дальше
    кейс идёт по фантому. Каждый шаг здесь дожидается операции и утверждает её ИСХОД.
    """
    return [
        # Сеть объявляет адресный план НАМЕРЕННО: сеть без него подсети не
        # принимает — нарезать не из чего (sync 400). Фикстура без плана была бы
        # снисходительнее продукта и обрушила бы весь посев интерфейса: подсети
        # нет → NIC не на чем строить → упал бы не тот шаг, который ошибся.
        # `10.0.0.0/8` покрывает любой блок, который выдаёт _cidr_prescript
        # (10.64…10.191), поэтому план и посев не разъедутся при смене энтропии.
        Step(name=f"seed-net-{suffix}", method="POST", path=NETS,
             pre_script=_cidr_prescript("nicSubCidr"),
             body={"projectId": "{{_suiteProjectId}}", "name": f"nicnet{suffix}{{{{runId}}}}",
                   "ipv4CidrBlocks": ["10.0.0.0/8"]},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId", "netId")]),
        poll_operation_until_done(),
        assert_op_success(),
        Step(name=f"seed-subnet-{suffix}", method="POST", path=SUBNETS,
             body={"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                   "name": f"nicsub{suffix}{{{{runId}}}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "{{nicSubCidr}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId", "subId")]),
        poll_operation_until_done(),
        assert_op_success(),
        # `assert_operation_envelope()` здесь НЕ применяется намеренно: он проверяет
        # форму идентификатора операции COMPUTE (`epd…`), а операцию создаёт vpc и
        # выдаёт свою (`enp…`). Утверждение чужой формы краснело бы на исправном
        # продукте — это была бы проверка не того предмета.
        Step(name=f"seed-nic-{suffix}", method="POST", path=NICS,
             body={"projectId": "{{_suiteProjectId}}", "subnetId": "{{subId}}",
                   "name": f"nicif{suffix}{{{{runId}}}}"},
             test_script=[*assert_status(200),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkInterfaceId", "nicId"),
                          "pm.test('metadata.networkInterfaceId несёт prefix nic', () => "
                          "pm.expect(pm.environment.get('nicId')||'').to.match(/^nic/));"]),
        poll_operation_until_done(),
        assert_op_success(),
        # Прогрев интерфейса — ровно то же, что `_seed_instance` делает для машины, и
        # ровно по той же причине. Без него ПЕРВЫМ обращением к свежему интерфейсу
        # оказывается привязка/отвязка, а она асинхронная: её POST отвечает `200` и
        # `Operation` всегда, поэтому ограниченный ретрай, который generator ставит на
        # шаг, сработать на ней не может — отказ владельца приезжает в терминальной
        # ошибке операции, которую читает уже другой шаг.
        #
        # Отказ в этом окне НЕОТЛИЧИМ от настоящего промаха: цель проверки прав у
        # `InternalNetworkInterfaceService.Detach` — САМ интерфейс (`scope_extractor`
        # `vpc_network_interface` из `nic_id`), а vpc прячет существование и берёт текст
        # из общей таблицы — «Network interface <id> not found», байт-в-байт как у
        # реального отсутствия. Кейс поэтому читал открытое окно как утверждение о
        # продукте.
        #
        # Греется ЧТЕНИЕМ: на нём обёртка работает (403/404 видны кодом ответа), она
        # ограничена бюджетом и ничего не маскирует — постоянный отказ по-прежнему
        # роняет посев, и роняет его НА СВОЁМ шаге, а не через три шага на чужом.
        retry_until_authorized(Step(name=f"warm-nic-{suffix}", method="GET",
                                    path=NICS + "/{{nicId}}",
                                    test_script=[*assert_status(200)])),
    ]


def _seed_instance(suffix):
    """Машина в проекте суиты, зона — та же, что у подсети интерфейса.

    `networkInterfaceSpecs` обязателен и это не украшение: `Instance.Create` без него
    отвергает запрос FAILED_PRECONDITION «needs an existing subnet+SG in zone …».
    Спека описывает НАМЕРЕНИЕ разложить машину в сети (её материализация — сага
    запуска COMP-2) и к привязке существующего интерфейса отношения не имеет:
    привязка идёт отдельным RPC, ради которого и заведён этот файл. Берём
    предпосеянные подсеть и группу безопасности — они принадлежат проекту суиты.
    """
    body = {"projectId": "{{_suiteProjectId}}", "name": f"insnic{suffix}{{{{runId}}}}",
            "zoneId": "{{existingZoneId}}", "instanceKind": "VM", "machineTypeId": "{{mtId}}",
            "bootSource": dict(_BOOT_STORAGE), "acknowledgeUnreachable": True,
            "networkInterfaceSpecs": [{"subnetId": "{{existingSubnetId}}",
                                       "securityGroupIds": ["{{existingSgId}}"]}],
            "vmSpec": {"userData": "#cloud-config\n{}",
                       "metadataOptions": {"metadataEndpoint": "ENABLED"}}}
    return [
        Step(name=f"seed-inst-{suffix}", method="POST", path=INSTANCES, body=body,
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.instanceId", "instanceId")]),
        poll_operation_until_done(),
        assert_op_success(),
        retry_until_authorized(Step(name=f"warm-inst-{suffix}", method="GET",
                                    path=INSTANCES + "/{{instanceId}}",
                                    test_script=[*assert_status(200)])),
    ]


def _cleanup(suffix):
    """Снос своего: интерфейс → подсеть → сеть → машина → размерность.

    Порядок обратный созданию: подсеть с живым интерфейсом не удаляется, и это
    поведение владельца, а не помеха — снос идёт от листа.
    """
    return [
        Step(name=f"cleanup-nic-{suffix}", method="DELETE", path=NICS + "/{{nicId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name=f"cleanup-subnet-{suffix}", method="DELETE", path=SUBNETS + "/{{subId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name=f"cleanup-net-{suffix}", method="DELETE", path=NETS + "/{{netId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name=f"cleanup-inst-{suffix}", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name=f"cleanup-mt-{suffix}", method="DELETE", path=MT_INT + "/{{mtId}}",
             internal=True, auth=ADMIN_AUTH,
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
    ]


def _detach_by_nic(name, nic_var_or_literal):
    return Step(name=name, method="POST",
                path=INSTANCES + "/{{instanceId}}:detachNetworkInterface",
                body={"nicId": nic_var_or_literal},
                test_script=[*assert_status(200), *assert_operation_envelope(),
                             *save_from_response("j.id", "opId")])


# ===========================================================================
# S4-06 — снятие привязки идёт до владельца и возвращается его исходом
# ===========================================================================

CASES.append(Case(
    id="INST-NIC-DET-CRUD-IDEMPOTENT-OK",
    title="S4-06 [РАЗЛИЧАЕТ РЕБРО]: DetachNetworkInterface по nicId для СВОЕГО отвязанного интерфейса → "
          "Operation done+success (владелец отвечает идемпотентным OK); повтор — тот же исход; интерфейс "
          "у vpc цел и не привязан (usedById пуст). Успех здесь означает, что запрос реально ушёл из "
          "compute на внутренний листенер vpc, прошёл там проверку прав на СВОЙ объект и вернулся: "
          "заглушка ребра на этом шаге даёт код 14. [verifies S4-06 · idempotency + conformance]",
    classes=["CRUD", "IDM", "CONF"], priority="P0",
    steps=[
        *_seed_mt("det"),
        *_seed_nic("det"),
        *_seed_instance("det"),
        _detach_by_nic("detach-1", "{{nicId}}"),
        poll_operation_until_done(),
        assert_op_success(),
        _detach_by_nic("detach-2-repeat", "{{nicId}}"),
        poll_operation_until_done(),
        assert_op_success(),
        retry_until_authorized(Step(name="get-nic-after-detach", method="GET", path=NICS + "/{{nicId}}",
            test_script=[*assert_status(200),
                         "const j = pm.response.json();",
                         "pm.test('интерфейс цел и это тот же объект', () => pm.expect(j.id).to.eql(pm.environment.get('nicId')));",
                         "pm.test('привязки нет — usedById пуст', () => pm.expect(j.usedById || '').to.eql(''));"])),
        *_cleanup("det"),
    ],
))

CASES.append(Case(
    id="INST-NIC-DET-NEG-ABSENT-NIC",
    title="S4-06 [РАЗЛИЧАЕТ РЕБРО]: DetachNetworkInterface с well-formed, но НЕ СУЩЕСТВУЮЩИМ nicId → "
          "формат синхронно пропущен (prefix nic ∈ каталога), запрос ушёл владельцу, и операция несёт "
          "ЕГО отказ — не транспортную недоступность. Кейс утверждает контракт ПРИНИМАЮЩЕЙ стороны: на "
          "заглушке тот же шаг даёт код 14. [verifies S4-06 · error-guessing]",
    classes=["NEG", "CONF"], priority="P0",
    steps=[
        *_seed_mt("abs"),
        *_seed_instance("abs"),
        _detach_by_nic("detach-absent", _ABSENT_NIC),
        poll_operation_until_done(),
        Step(name="assert-op-error", method="GET", path="/operations/{{opId}}",
             test_script=[
                 "const j = pm.response.json();",
                 "pm.test('операция завершена', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                 "pm.test('операция отвергнута (есть error, а не response)', () => pm.expect(Boolean(j.error), JSON.stringify(j)).to.eql(true));",
                 # Ровно один ожидаемый код, и он НЕ 14: 14 означал бы, что до владельца
                 # не дошли. Какой именно код здесь контрактный, снят со стенда пробой,
                 # а не угадан — см. RESULTS.md.
                 "pm.test('код — отказ ВЛАДЕЛЬЦА, не транспорт (никогда 14/UNAVAILABLE)', () => { pm.expect(j.error.code, JSON.stringify(j)).to.not.eql(14); pm.expect(j.error.code, JSON.stringify(j)).to.eql(5); });",
                 "pm.test('тон сообщения — контракт отсутствия у владельца', () => pm.expect(j.error.message||'', JSON.stringify(j)).to.eql('Network interface " + _ABSENT_NIC + " not found'));",
                 "pm.test('сообщение называет запрошенный идентификатор', () => pm.expect(j.error.message||'').to.include('" + _ABSENT_NIC + "'));",
             ]),
        Step(name="cleanup-inst-abs", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-mt-abs", method="DELETE", path=MT_INT + "/{{mtId}}",
             internal=True, auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
    ],
))


# ===========================================================================
# S4-06 — синхронные отказы формы (до обращения к соседу)
# ===========================================================================

CASES.append(Case(
    id="INST-NIC-ATT-VAL-MALFORMED-NICID",
    title="S4-06: AttachNetworkInterface с явно-не-идентификатором → СИНХРОННО 400 INVALID_ARGUMENT, "
          "текст дословно «invalid network interface id 'bad-nic'». Терминальный отказ первым "
          "стейтментом: вызывающий не получает ни retryable-недоступности, ни тона отсутствия ресурса "
          "на строку, которая ресурсом быть не может. Ребро здесь НЕ задействовано — проверка формата "
          "стоит до него. [verifies S4-06 · ECP формата]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        *_seed_mt("mal"),
        *_seed_instance("mal"),
        Step(name="attach-malformed", method="POST",
             path=INSTANCES + "/{{instanceId}}:attachNetworkInterface",
             body={"attachedNicSpec": {"nicId": _MALFORMED_NIC}},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test(\"текст: invalid network interface id '" + _MALFORMED_NIC + "'\", () => "
                          "pm.expect(pm.response.json().message||'').to.eql(\"invalid network interface id '" + _MALFORMED_NIC + "'\"));"]),
        Step(name="cleanup-inst-mal", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-mt-mal", method="DELETE", path=MT_INT + "/{{mtId}}",
             internal=True, auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
    ],
))

CASES.append(Case(
    id="INST-NIC-DET-VAL-ONEOF-MISSING",
    title="S4-06: DetachNetworkInterface без nicId и без index → СИНХРОННО 400 INVALID_ARGUMENT "
          "«exactly one of nic_id or index is required». Пустой oneof — не «отвяжи что-нибудь»: "
          "предмет мутации обязан быть назван. [verifies S4-06 · exactly_one]",
    classes=["VAL", "NEG"], priority="P1",
    steps=[
        *_seed_mt("oneof"),
        *_seed_instance("oneof"),
        Step(name="detach-empty-oneof", method="POST",
             path=INSTANCES + "/{{instanceId}}:detachNetworkInterface", body={},
             test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                          "pm.test('текст: exactly one of nic_id or index is required', () => "
                          "pm.expect((pm.response.json().message||'').toLowerCase()).to.include('exactly one of nic_id or index is required'));"]),
        Step(name="cleanup-inst-oneof", method="DELETE", path=INSTANCES + "/{{instanceId}}",
             test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        Step(name="cleanup-mt-oneof", method="DELETE", path=MT_INT + "/{{mtId}}",
             internal=True, auth=ADMIN_AUTH, test_script=[*save_from_response("j.id", "opId")]),
        poll_operation_until_done(auth=ADMIN_AUTH),
    ],
))


# ===========================================================================
# S4-01 в конструируемой сегодня части — гейт состояния машины
# ===========================================================================

CASES.append(Case(
    id="INST-NIC-ATT-NEG-STATE-GATE",
    title="S4-01 (конструируемая часть): AttachNetworkInterface своего СУЩЕСТВУЮЩЕГО интерфейса к "
          "машине сразу после Create → Operation error FAILED_PRECONDITION «Instance is not running or "
          "stopped». Машина покоится в PROVISIONING (переход к RUNNING принадлежит саге запуска COMP-2, "
          "её в дереве нет), а Start требует STOPPED, Stop — RUNNING: перехода из PROVISIONING не "
          "существует, поэтому счастливый путь приёмки сегодня НЕ КОНСТРУИРУЕТСЯ, и кейс утверждает "
          "действующий контракт вместо него. Ребро при этом НЕ задействовано — гейт состояния стоит ДО "
          "вызова соседа, значит зелёное этого кейса о ребре не говорит НИЧЕГО. "
          "[verifies S4-01 частично · state-transition]",
    classes=["NEG", "STATE"], priority="P0",
    steps=[
        *_seed_mt("gate"),
        *_seed_nic("gate"),
        *_seed_instance("gate"),
        retry_until_authorized(Step(name="get-inst-status", method="GET", path=INSTANCES + "/{{instanceId}}",
            test_script=[*assert_status(200),
                         "pm.test('машина покоится в PROVISIONING (предпосылка кейса, а не догадка)', () => "
                         "pm.expect(pm.response.json().status).to.eql('PROVISIONING'));"])),
        Step(name="attach-on-provisioning", method="POST",
             path=INSTANCES + "/{{instanceId}}:attachNetworkInterface",
             body={"attachedNicSpec": {"nicId": "{{nicId}}"}},
             test_script=[*assert_status(200), *assert_operation_envelope(),
                          *save_from_response("j.id", "opId")]),
        poll_operation_until_done(),
        # Тон утверждается ДОСЛОВНО: `msg_substr` приводит обе стороны к нижнему
        # регистру, и расхождение по регистру под ним покраснеть не может.
        assert_op_error(9, "FAILED_PRECONDITION", msg_regex="^Instance is not running or stopped$"),
        *_cleanup("gate"),
    ],
))
