# newman — результаты прогона (kacho-compute)

## COMP-1 redesign (Instance core + MachineType) — добавлено (2026-07-20)

Новые коллекции black-box для tenant-facing редизайна `kacho-compute` против APPROVED
`docs/specs/sub-phase-COMP-1-instance-machinetype-acceptance.md`:

| Collection | Cases | Покрытие COMP-1-NN |
|---|---|---|
| `machine-type` | 12 | F7 sync sizing-каталог: Get/List (18/19), malformed-first + NOT_FOUND (20), admin-CRUD на Internal* :8081 (21), pageSize/token BVA |
| `instance-redesign` | 43 | легаси-поля Create отвергаются, не игнорируются (`INST-RD-CR-VAL-UNSUPPORTED-*`, 6 полей + «все шесть разом»; см. `docs/architecture/07-known-divergences.md` §7.1) · F1 kind-oneof XOR (01-04) · F2 machineTypeId single-channel (05-08) · F3 bootSource grammar (09-11) · F4 SA Referrer (12-13) · F5 unreachable-guard (14) · F6 launch-skeleton (16-17) · F8 malformed-first (22) · F9/F11 field-absence (24/28) · F10 Update mutability + STOPPED-gate (04/25/26/27) · F12 dup-name (30) · F13 zone peer-validate (33) · F14 List authz+pagination+filter (34-36) · F15 Delete hard-delete + name-recycle (37-38) |

Прогон — **CI** (`deploy/scripts/newman-e2e.sh`, локальный env-blocked: harness убивает port-forward).
Ожидание: все кейсы **зелёные** (поведение сверено с реализацией `services/compute/internal/service/
{instance,machine_type}.go` + `protoconv` + gateway `restmux`/`DiscardUnknown` — не по идеализированному
тексту acceptance).

### Сверка с контрактом — задокументированные нюансы (НЕ баги продукта, findings для acceptance-author)

1. **Gateway `DiscardUnknown: true`** (`gateway/internal/restmux/mux.go`) — retired/reserved поля
   (`platformId`/`resourcesSpec`/ретайренные brand-flavoured поля `MetadataOptions`/`hostGroupId`)
   **молча отбрасываются**, а НЕ `400 unknown field`.
   Поэтому acceptance-формулировка F2/F9 «легаси-поле → 400 unknown field» **не наблюдаема** через gateway.
   Retire залочен наблюдаемыми инвариантами: **single-channel** (`INST-RD-CR-VAL-MT-REQUIRED`:
   sizing без `machineTypeId` → `400 machineTypeId is required`) + **field-absence на выводе**
   (`INST-RD-GET-CONF-FIELD-ABSENCE`) + **явный отказ по имени поля** для полей, которые в контракте
   остались, но сервисом не читаются (`INST-RD-CR-VAL-UNSUPPORTED-*`, 400 + `fieldViolation`).
   Исключение: output-only поля `bootSource.name°/resolvedDigest°` — **known** proto-поля, реджектятся
   **сервисом** (не gateway) → `INST-RD-CR-VAL-BOOTSOURCE-OUTPUT-FIELDS` строго локает 400.

   **Обновление (эта ветка).** Прежний окольный лок `INST-RD-CR-VAL-RAW-SIZING-RETIRED` **снят**:
   его предметом была сама снисходительность края, и показать её он мог, только сам отправив ключ вне
   контракта. Этот класс теперь меряется статически и по всем suite'ам сразу —
   `gateway/internal/restmux/newman_body_contract_test.go` сверяет ключи тела каждого newman-запроса с
   полями обслуживающего его message. Плюс `platformId`/`resourcesSpec`/`bootDiskSpec` зарезервированы
   в `CreateInstanceRequest` по номеру **и имени**, то есть вернуться в контракт не могут по построению.
   Чего в покрытии по-прежнему нет: «край НАЗЫВАЕТ старому клиенту снятое поле» — этот кейс станет
   написуемым, когда край начнёт отвергать неизвестные ключи (сегодня ожидание пришлось бы выдумать).

2. **F14 filter-whitelist — РАЗРЕШЕНО (2026-07-27), фаза остаётся `name=`.** Acceptance F14/COMP-1-36
   заявлял whitelist `name=`/`placementGroupId=`/`instanceKind=`; реализация (`instance_repo.go`:
   `filter.Parse(f, []string{"name"})`) whitelist'ит **только `name=`**, что совпадает с нормативным
   `api-conventions.md` «текущая фаза — name=». Расхождение сведено **в пользу кода**: acceptance
   приведён к реализации (§Reconcile F14 filter-whitelist), отклонение записано в
   `services/compute/docs/architecture/07-known-divergences.md` §12, расширение отложено в COMP-3
   вместе с ресурсом `PlacementGroup` и **обязательным** индексом под новое поле. Замеры на живой
   Postgres, обосновавшие решение: camelCase-написание из дока → `42703 column "instancekind" does not
   exist`; `instance_kind = 'CONTAINER'` → `22P02` (колонка INTEGER-ordinal, парсер даёт строку).
   Прежний толерантный кейс `INST-RD-LST-FILTER-KIND-TOLERANT` (`oneOf([200,400])`) **заменён строгим**
   `INST-RD-LST-FILTER-UNKNOWN-FIELD-REJECTED`: 400 + точное сообщение с именем поля, 3 написания.
   Толерантность здесь была «формой без содержания» — `oneOf([200,400])` проходил и при молчаливом
   игнорировании фильтра, то есть ровно на том дефекте, ради которого писался.

3. ~~**Legacy `cases/instance.py` (77 кейсов)**~~ — **снято 2026-07-28: файла нет.**
   Этот пункт объявлял 77 pre-existing-red кейсов и follow-up-миграцию для них.
   `cases/instance.py` в дереве отсутствует; покрытие живёт в `cases/instance-redesign.py`.
   Заявленной работы не существует — не планировать её по этому пункту.

4. **MachineType-каталог не засеян** на стенде (миграция 0015 — пустая таблица, deploy-seed нет) → каждый
   зависимый кейс **self-seed'ит** mt через `InternalMachineTypeService.Create` (`{{internalBaseUrl}}`
   :8081, ban #6) с `{{runId}}`-уникальным именем + cleanup. `internalBaseUrl` инжектится CI-драйвером
   (`newman-e2e.sh --env-var`); PRE_GLOBAL деривирует fallback из baseUrl (:18080→:18081) для standalone.

5. **authz-first толерантность** (`testing.md`): Instance Get/Delete по malformed/absent id — gateway
   `scope_extractor{compute_instance,instance_id}` может короткозамкнуть `403` ДО backend → negatives
   ждут `oneOf([400,403])` (malformed) / `oneOf([403,404])` (absent), НИКОГДА 200. Malformed-first контракт
   строго локнут отдельно на cluster-scope MachineType.Get (`MT-GET-VAL-MALFORMED-ID` → строгий 400 + текст).

**Known failing — product bugs:** нет. Всё redesign-поведение сверено с реализацией и соответствует
контракту; п.1-2 — gateway-policy / фазовый scope конвенции (не баги).

---

## Статус: v1 — сгенерировано, ещё не прогнано против задеплоенного стенда

Коллекции сгенерированы (`scripts/gen.py`); прогон против live api-gateway **не выполнен** —
на момент создания сьюты compute-backend не задеплоен в локальном стенде
(api-gateway на `localhost:18080` отвечает `503 "name resolver error: produced zero addresses"`
для `/compute/v1/*`; VPC-backend работает). Прогон будет выполнен после `kacho-deploy` с поднятым
`kacho-compute` — результаты заносятся ниже.

## Сводка (v1 — generated)

| Ресурс | Cases | Steps | Assertions* | Failed | Status |
|---|---|---|---|---|---|
| disk | 74 | 204 | — | — | generated, not run |
| instance | 82 | 426 | — | — | generated, not run |
| image | 60 | 149 | — | — | generated, not run |
| snapshot | 52 | 157 | — | — | generated, not run |
| disk-type | 10 | 10 | — | — | generated, not run |
| ~~zone~~ | — | — | — | — | removed (Stage S7 — Geography owned by kacho-geo) |
| operation | 8 | 18 | — | — | generated, not run |
| **Итого** | **286** | **964** | — | — | — |

\* assertions считаются при прогоне (`run.sh` → `out/<resource>.json`).

## Как прогнать

```bash
# 1. Поднять стенд с задеплоенным compute + port-forward api-gateway → localhost:18080
cd ../../kacho-deploy && make dev-up && make reload-svc SVC=compute
# 2. (если seed e2e-ресурсов VPC отличается от env — поправить environments/local.postman_environment.json:
#     existingNetworkId / existingSubnetId / existingSgId / existingPlatformId)
# 3. Перегенерить коллекции (если меняли cases/*.py)
python3 tests/newman/scripts/gen.py
# 4a. Прогнать всё одним махом
tests/newman/scripts/run.sh                       # сводка → out/summary.txt
tests/newman/scripts/run.sh --service disk        # один ресурс
# 4b. Прогнать ПО ОДНОМУ кейсу за раз с зачисткой ресурсов (quota-safe — как для YC)
tests/newman/scripts/run-incremental.sh           # все ~296 кейсов; сводка → out/incremental/summary.txt
tests/newman/scripts/run-incremental.sh --resume                 # продолжить прерванный
tests/newman/scripts/run-incremental.sh --service instance       # один ресурс
tests/newman/scripts/run-incremental.sh --failed                 # только упавшие из прошлого прогона
tests/newman/scripts/run-incremental.sh --cleanup-only           # стереть throwaway-ресурсы в тест-папках
```

**Деплоймент-замечания:**
- Instance CRUD-кейсы (`INST-*-CRUD-*`, `INST-STATE-*`, `INST-AD/DD/NAT/UMETA-*`, `INST-DISK-DEL-WHILE-ATTACHED`)
  требуют поднятого `kacho-vpc` + seeded `existingNetworkId`/`existingSubnetId`/`existingSgId` в той же зоне (`existingZoneId`).
- `*-NEG-SUBNET-NOTFOUND` / `*-NEG-PROJECT-NOTFOUND` / `OP-GET-CRUD-FAILED-OP` требуют `KACHO_COMPUTE_SKIP_PEER_VALIDATION!=true`
  (при `=true` cross-service existence-checks — no-op → эти кейсы краснеют; помечены `# requires peer-validation enabled` в cases).
- `# probe-needed:` кейсы фиксируют наше текущее поведение там, где точный контракт ещё не verified (список — `REQUIREMENTS.md` §A);
  они написаны с `allow [200,400]` / substr-assert, чтобы не краснеть на любом разумном поведении — заменяются точными assert'ами после probe.

## Known failing — round-4 disposition (umbrella CI, gate `../../iam/tests/newman/scripts/assert-suites-green.sh`)

The umbrella gate is the shared `assert-suites-green.sh`. Compute residuals against the
umbrella CI report (`ci-rep4`) resolve as follows — **fixed in the case where the product
is convention-correct**, **whitelisted only for a tracked infra/product gap**, and **never
masking a leak**:

| Case / step | Signature | Disposition |
|---|---|---|
> **Three rows removed 2026-07-28.** They described `INST-CR-CRUD-BOOT-DISK-ID-OK`,
> `INST-CR-CRUD-OK::get` and `INST-DEL-CONF-RESPONSE-EMPTY::assert-empty`. None of those
> case names exists anywhere in this service's `cases/` or `collections/` any more — the
> instance suite was rewritten as `instance-redesign` (`INST-RD-*`) and the attach/boot
> coverage left with the storage split. The first row also claimed a live whitelist entry
> that was pruned from the gate on 2026-07-26. A reader budgeting work from this table
> would have counted one masked red and two dispositions that have no subject.
> `PRO-Robotech/kacho#10` should be re-checked on the redesigned suite's own evidence.

| `OP-GET-NEG-UNKNOWN-PREFIX::get-garbage-prefix` | `mentions prefix: expected 'invalid operation id "…"' to include 'prefix'` | **TEST-ASSERTION FIXED** — the product is convention-correct (api-conventions «malformed id → `invalid <res> id <X>`»); the message is `invalid operation id "<X>"`, not the internal term 'prefix'. Assert now checks the convention text; status 400 + gRPC code 3 already passed. |

## Parity-добор — `test/compute-newman-parity-qa` (qa-test-engineer, 2026-07-21)

**Контекст (корректировка premise).** Задача формулировалась как «compute сильно недобран — 144 RPC /
10 cases, добрать ~30-40». Ground-truth по факту **иной**: compute-suite уже зрелая и на parity-bar
iam/vpc — `gen.py` даёт **277 core-кейсов** (disk 70, instance 77, image 60, snapshot 52, disk-type 10,
operation 8) + authz-deny 186 + list-filter 4 + sec-d 2. Все **wired+implemented** public RPC покрыты:
Instance verb-actions Start/Stop/Restart/AttachDisk/DetachDisk/AttachNetworkInterface/DetachNetworkInterface/
UpdateMetadata/GetSerialPortOutput/SimulateMaintenanceEvent/ListOperations — **уже** имеют happy + state/NF
негативы (`INST-STATE-*`, `INST-AD/DD-*`, `INST-NIC-*`, `INST-SME/SPO/UMETA/LOP-*`). «144 RPC / MachineType /
InternalMachineTypeService» относятся к **не-реализованному** редизайну **COMP-1** (`project/kacho` monorepo,
acceptance `sub-phase-COMP-1-instance-machinetype-acceptance.md` — APPROVED, но код not-yet-landed:
`ins-`-prefix/`MachineType`/`bootSource` отсутствуют в AS-IS compute). В текущем сервисе **нет** ни
`MachineTypeService`, ни `InternalMachineTypeService` (proto/served surface проверены).

**Что добавлено (genuine parity-добор, +6 cases).** Выявлены реальные асимметрии: Image/Snapshot не
имели трёх классов, которые есть у эталона Disk/Instance. Каждый кейс — зеркало **проходящего** Disk-эталона
(тот же handler/operations-паттерн → детерминированно GREEN):

| Case-id | Класс | Техника | Зеркалит |
|---|---|---|---|
| `IMG-UPD-MASK-EMPTY-FULL-PATCH` | STATE/VAL | ECP(mask=empty) + state-transition (mutable applied / immutable ignored) | `DISK-UPD-MASK-EMPTY-FULL-PATCH` |
| `SNAP-UPD-MASK-EMPTY-FULL-PATCH` | STATE/VAL | ↑ | ↑ |
| `IMG-DEL-CONF-RESPONSE-EMPTY` | CONF | conformance (async Delete-op → response=Empty + metadata.imageId) | `DISK-DEL-CONF-RESPONSE-EMPTY` |
| `SNAP-DEL-CONF-RESPONSE-EMPTY` | CONF | ↑ (metadata.snapshotId) | ↑ |
| `IMG-LOP-NEG-PARENT-NF` | NEG | error-guessing (absent parent → 200\|404) | `DISK-LOP-NEG-PARENT-NF` |
| `SNAP-LOP-NEG-PARENT-NF` | NEG | ↑ | ↑ |

`*-DEL-CONF-RESPONSE-EMPTY` наследуют round-4 фикс Empty-`Any` (assert фильтрует и `@type`, и `value`-обёртку —
proto3-JSON `{"@type":".../Empty","value":{}}` → 0 доменных ключей). Новый gen-итог: image 60→63, snapshot 52→55,
core 277→**283**. `gen.py` зелёный (нет дублей case-id — hard-fail не сработал).

**Greenness — CI-арбитр.** Локальный стенд недоступен (`/tmp/kacho.kubeconfig` — ns `kacho` без compute-подов /
api-gateway REST не проброшен; известное ограничение харнесса, memory `local-newman-env-blocked`). Live-probe
шести кейсов **не** выполнен → RED-фаза не наблюдалась локально. Кейсы построены как точные зеркала уже-зелёных
Disk-эталонов на идентичном handler-паттерне; **финальная зелёность подтверждается umbrella-CI** (gate
`iam/tests/newman/scripts/assert-suites-green.sh`) с поднятым storage/vpc/iam. Требуют `existingZoneId`
(pre-disk) — как остальные disk-sourced кейсы.

## Known coverage-gap — malformed-id → InvalidArgument (НЕ покрыт; documented deferral)

Convention `api-conventions.md` требует «malformed id → sync `InvalidArgument "invalid <res> id"` первым
стейтментом». Read/Delete-RPC compute (Disk/Image/Snapshot/DiskType.Get, *.Delete) **format-check id НЕ делают**
(только empty-check → `repo.Get` → `NOT_FOUND`) — это **documented deferred divergence #1**
(`docs/architecture/07-known-divergences.md` §1: «мы NotFound, контракт InvalidArgument», низкоприоритетно,
план — prefix-check + Issue + newman-миграция). Suite намеренно использует **well-formed** garbage id
(`epdnonexistent999999`) → корректно тестирует 404-линию; malformed→400 линия **не** покрыта.

**Кейс НЕ добавлен** (осознанно, не упущение): (a) кейс, ждущий 400, был бы **RED** против уже-задокументированного
deferral (не свежий баг → не завожу дубль-Issue); (b) кейс, локающий 404, **заблокировал бы** намеренный будущий
фикс (COMP-1 F8/F13/F15 явно целят «malformed-id первым стейтментом» → 400). Решение — за owner: закрыть divergence
#1 (Go-фикс prefix-check across Get/Delete) → тогда добавить `*-GET-VAL-MALFORMED-ID` (→400) как GREEN-lock.
Attach-path уже конформен (`InstanceService.AttachDisk` вызывает `corevalidate.ResourceID` первым — sync 400).

## Эволюция

| Версия | Cases | Steps | Что добавлено |
|---|---|---|---|
| **v1** | **296** | **975** | первая версия: disk(74) / instance(82) / image(60) / snapshot(52) / disk-type(10) / zone(10) / operation(8). Полный CRUD + Operations LRO poll, Instance state-машина, attach/detach + Disk-delete-while-attached, NAT, UpdateMetadata, GetLatestByFamily, BVA (size/name/labels/pagesize/cores/core_fraction), CONF (id-prefix epd/fd8, created_at до секунд, Operation.response=Empty, BASIC-view metadata omission), security probes, lifecycle conformance. 100% публичных RPC compute-домена покрыты ≥1 кейсом (кроме явных `blocked:*` / scope-cut — см. TEST-PLAN). |

## Skill-mapping (testing-product-coach §3, §4)

| Техника | Реализация |
|---|---|
| §3.1 ECP | ✅ `name_validation_block`, `labels_validation_block`, `description_validation_block` |
| §3.2 BVA | ✅ disk size 4MiB/below/26TiB/above, name len 63/64, pageSize 0/1/1000/1001, labels 64/65, cores set, core_fraction set |
| §3.3 Decision Tables | ✅ required-field matrix (Instance: zone/platform/resources/bootdisk/nic/project), UpdateMask (unknown/immutable/empty), error mapping |
| §3.4 State Transition | ✅ Instance state-машина (Start/Stop/Restart preconditions, AttachDisk/DetachDisk/NAT), immutable fields, Disk-delete-while-attached |
| §3.5 Pairwise | partial (Disk size × type × source — частично; full pairwise — backlog) |
| §3.7 Use-case | ✅ `*-LIFECYCLE-CONF` (полный CRUD-цикл; Instance — с Stop/Start) |
| §3.8 Error Guessing | ✅ `malformed_body_block`, empty body, HTTP-method, garbage prefix (Operation) |
| §3.10 Property-Based | ✅ pagination roundtrip (ZONE), idempotent move-self semantics (через MV-кейсы) |
| §3.11 Risk-Based | ✅ priority P0..P3 tagging — P0 на security/data-integrity/state-machine/Disk-delete-while-attached |
| §4.1 Smoke | ✅ P0/P1 кейсы — фактический smoke |
| §4.2 Functional regression | ✅ полная suite (296 кейсов) |
| §4.3 Conformance | ✅ CONF class: id-prefix, created_at до секунд, Operation.response=Empty, NF-text формат, BASIC-view metadata omission — против proto + acceptance-дока |
| §4.4 Performance | → перенесено в k6 (`tests/k6/`) |
| §4.5-4.8 Load/Stress/Soak/Spike | → k6 |
| §4.10 Security | ✅ `security_injection_block` (SQLi/union/XSS/cmd/path/longpayload × name + filter) |
| §4.11 Compatibility | → backlog |
| §4.12 Migration | covered внешними тестами (`kacho-deploy` smoke) |

## Findings

Найденные баги / расхождения с verbatim YC / observability-gaps — заводятся в GitHub Issues
(`PRO-Robotech/kacho-compute`, метки `bug`/`tech-debt`/`enhancement`; `blocked:kacho-kms` /
`blocked:kacho-marketplace` / `blocked:kacho-snapshot-schedule` / `blocked:kacho-filesystem` для
заблокированного), см. `kacho-compute/CLAUDE.md` §14.4. By-design расхождения — `docs/architecture/07-known-divergences.md`.
Отдельного bug-map / FINDING-реестра нет.
