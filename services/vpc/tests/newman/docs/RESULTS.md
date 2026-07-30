# newman — финальный прогон (AddressPool DB-level CIDR overlap prevention)

## Сводка (cases-count актуален после gen.py; assertions/requests — ориентир, перепрогнать suite)

| Сервис | Cases | Failed | % к 100/рес |
|---|---|---|---|
| subnet | 137 | 0 | |
| network | 103 | 0 | |
| address | 103 | 0 | |
| security-group | 101 | 0 | |
| route-table | 88 | 0 | |
| gateway | 84 | 0 | |
| internal-pool | 31 | 0 | (admin) |
| network-interface | 14 | 0 | (nic) |
| concurrency | 4 | 0 | |
| operation | 6 | 0 | (n/a) |
| observability | 1 | 0 | (n/a) |
| authz-deny | 324 | 0 | (per-RPC authz-gate matrix) |
| **Итого** | **996** | **0** | — |

> Suite `internal-cloud` (CloudPoolSelector) и RPC `Move`/`Relocate`, NIC
> `AttachToInstance`/`DetachFromInstance`, AddressPool override/Check/ExplainResolution —
> удалены вместе с соответствующими кейсами. IPAM cascade сведен к
> network_default → zone_default → global_default.

> Suite `internal-region-zone` (Region/Zone, prefix `RGN-*`/`ZON-*`) вынесен из этого
> репозитория вместе с доменом Geography; покрытие Region/Zone — в сервисе compute. Цифры
> в таблице выше — без него.

**100% PASS, кроме declared known-failing (rule #13)** — см. «Known failing tests —
product bugs» ниже (2 persistent-RED SG-rule-target; `#27` — **FIXED**, product-side
refcheck реализован). Плюс отдельный
under-investigation кластер IPAM-resolve (см. ниже). Покрыты internal/admin-only IPAM RPC
(`InternalAddressPoolService`) — kacho-only RPC проброшены через api-gateway
cluster-internal mux, возвращают ресурсы напрямую (не Operation).

> **Снято из таблицы 2026-07-30: `ADR-GBV-CONF-EXT-BY-VALUE-UNREACHABLE` → `…-REFUSED`.**
> Продуктовое решение принято и реализовано: поиск по значению отвечает только про
> внутренний адрес, а запрос, назвавший внешний, отвергается синхронно и по имени поля
> (`external_ipv4_address`, `INVALID_ARGUMENT`) — первым стейтментом, до обращения к
> хранилищу. Предмет фикса — не «нет ответа», а **ложный** ответ: прежде такой запрос
> доезжал до выборки, сужение по подсети не совпадало ни с одной строкой, и вызывающему
> отвечали «не найдено» про адрес, который существует. Сужение по подсети НЕ снято
> (снять его значило бы открыть межтенантный оракул по внешним адресам — его сторожит
> соседний P0 `ADR-GBV-CONF-NOLEAK-FOR-EXISTING-OTHER`), и поиск по внешнему значению
> НЕ реализован: ему нужна область, которой у этого RPC нет. Кейс больше не красный —
> он утверждает названный отказ (400 + код 3 + имя поля + «не найдено» в тексте
> запрещено); краснеет он ровно при возврате ложного «не найдено», что проверено
> инъекцией. Остаток #104 — продуктовый: давать ли поиску по внешнему значению область
> проекта отдельной приёмкой.

## Known failing tests — product bugs (rule #13)

Persistent-RED кейсы — тест корректен, но GREEN требует фикса продукта. Допустимое
исключение из «100% pass» с явной декларацией (rule #13). Кейс краснеет до фикса
прод-бага; в case-файле стоит `# verifies <issue-url>`, `pm.test.skip` запрещён.

| Case | Suite | Verifies | Что доказывает | Причина RED |
|---|---|---|---|---|
| `SG-NET-08-RULE-SAME-NETWORK-OK` | security-group | [`kacho#106`](https://github.com/PRO-Robotech/kacho/issues/106) (заведён 2026-07-30 — до этого запись стояла «issue pending», то есть истечь ей было нечем) | Правило с SG-target (`securityGroupId`) обязано отдаваться в `Get/List`-ответе SG (`rule.securityGroupId` == целевой SG) | `dto/toproto/security_group.go::securityGroup.toPb` мапит в `SecurityGroupRule.Target` **только** ветку `CidrBlocks`; ветки `SecurityGroupId` и `PredefinedTarget` (домен несёт `r.SecurityGroupID`/`r.PredefinedTarget`) не сериализуются → `Target=nil` → `rule.securityGroupId=undefined`. Signature: `expected [ undefined ] to include '<sgId>'`. Фикс (не в test-only PR, ban #13): добавить обе ветки в `toPb` + regression. |
| `SG-NET-09-RULE-SAME-NETWORK-UPDATERULES-OK` | security-group | [`kacho#106`](https://github.com/PRO-Robotech/kacho/issues/106) | То же через `UpdateRules` (PATCH `…/rules`): добавленное SG-target-правило видно в `Get` с `securityGroupId` | Та же прод-первопричина — `toPb` роняет `SecurityGroupId`/`PredefinedTarget` target. RED до прод-фикса `toPb`. |
| `SG-URL-VAL-PORT-NEG` | security-group | [#103](https://github.com/PRO-Robotech/kacho/issues/103) | Правило с портом ниже допустимой границы обязано отвергаться на входе | Диапазон портов не проверяется НИГДЕ: ни `validateSGRule` (она смотрит направление/описание/метки/CIDR), ни доменная модель, ни ограничение БД (правила — одно JSONB-поле). Правило сохраняется как есть, ответ 200. RED до продуктового решения (границы, «любой порт», соотношение имени и номера протокола). |
| `SG-URL-VAL-PORT-OVER-65535` | security-group | [#103](https://github.com/PRO-Robotech/kacho/issues/103) | То же для верхней границы порта | Та же первопричина. |
| `SG-URL-VAL-PROTOCOL-UNKNOWN` | security-group | [#103](https://github.com/PRO-Robotech/kacho/issues/103) | Правило с несуществующим именем протокола обязано отвергаться на входе | Имя протокола не проверяется; законный набор имён — продуктовое решение (регистр, протоколы без портов, связь с числовым номером). RED до фикса. |


> **ИСПРАВЛЕНО 2026-07-29 (запись о правке утверждений, не объявление).** Три кейса выше
> стали красными в этот день — и это ожидаемо. До этого все пять
> кейсов набора `SG-URL-VAL-*` стояли под утверждением `oneOf([200, 400])` с меткой
> «rejected sync or async»: оно принимало и приём правила, и отказ в нём, поэтому
> ни один отрицательный кейс не мог упасть — независимо от того, что делает продукт.
> Утверждения сведены к одному исходу. Два из пяти при этом зелёные:
> `SG-URL-VAL-PORT-ANY-MINUS-1` (правило принимается) и `SG-URL-VAL-DIRECTION-UNKNOWN`
> (неизвестное значение перечисления направления отвергается разбором тела на краю).
> Оставшиеся три краснеют на реальном расхождении — это давление на продуктовый фикс,
> а не регресс тестов. Подгонять утверждение под текущее поведение запрещено: тогда
> дефект стал бы задокументированным контрактом.

> **`SG-DEL-NEG-NIC-ATTACHED` ([#27](https://github.com/PRO-Robotech/kacho-vpc/issues/27)) — FIXED.**
> `SG.Delete` SG'а, прилинкованного к NIC через `security_group_ids[]`, теперь отвергается
> `FAILED_PRECONDITION` (code 9). Product-side refcheck реализован в repo
> `securityGroupWriter.Delete`: ВНУТРИ writer-TX `SELECT id … FOR UPDATE` +
> `EXISTS(security_group_ids @> jsonb_build_array($id))` → `ErrFailedPrecondition`
> «security group is in use by network interface(s)» (не TOCTOU — проверка+DELETE в одной TX).
> Покрыто integration-тестами `TestCQRS_SG_Delete_BlockedByNICReference` /
> `…_Concurrent_Referenced_AllBlocked` / `…_Concurrent_Unreferenced_ExactlyOne`
> (`internal/repo/kacho/pg/security_group_refcheck_integration_test.go`, testcontainers, `-race`).
> Кейс перестал быть persistent-RED — ожидаемо GREEN при следующем прогоне suite против стенда.
> (До 2026-07-05 он маскировался условным `pm.test.skip`; SEC-hardening r2 сделал его
> безусловным persistent-RED + issue #27, что и держало давление на прод-фикс.)

> **Под расследованием — [`kacho#107`](https://github.com/PRO-Robotech/kacho/issues/107)** (заведён
> 2026-07-30; до этого запись ссылалась только на «сигнатуры переданы исполнителю», то есть
> истечь ей было нечем). Кластер IPAM-resolve в `internal-pool`:
> `IPL-ALLOC-POOL-EXHAUSTED` (`alloc-1/alloc-2` → Operation error `no address pool resolved
> for address … (network , family=0)`), `IPL-RESOLVE-DUALSTACK-OK` (`get-v4/get-v6` → 404:
> `cr-addr` Operation не резолвит case-local isDefault pool в throwaway-зоне),
> `IPL-RMCIDR-NEG-INUSE` (`remove-inuse` текст `CIDR blocks not found` вместо `has allocated addresses`
> — каскад от того же resolve-fail). Это НЕ read-your-writes / authz-ordering (детерминированная
> Operation-ошибка резолва пула в throwaway-зонах zoneC/zoneD, вероятно зависит от количества
> seeded geo-зон и/или порядка cleanup). Сигнатуры переданы rpc-implementer'у для доменного
> разбора; кейсы не форс-гринятся и не whitelist'ятся масками. `get-v6` обёрнут в
> `retry_until_authorized` (parity с `get-v4`), но GREEN требует резолва пула.

> Деплоймент-замечание: suite требует `KACHO_VPC_DEFAULT_SG_INLINE=true`
> (default) — `*-LSG-CRUD-DEFAULT-SG` / `*-DEL-STATE-DEFAULT-SG` проверяют
> авто-создание default SG. При `=false` (load-test config) эти кейсы краснеют.
> internal-* кейсы используют seeded `zone` region / `zone-{a,b,c,d}`
> zones / `default-zone-a` pool как readonly-фикстуры (не трогают),
> остальное — runId-суффиксованные throwaway-ресурсы с self-cleanup.

## Round-4 disposition — ЗАКРЫТО (было «known failing»; все три строки исправлены)

Запись о разборе vpc-residuals против umbrella-отчёта (`ci-rep4`). Ни одного живого
объявления здесь больше нет: все три строки ниже помечены исправленными, а вычитание
«известного красного» из вердикта прогона снято целиком 2026-07-30 — маски, о которой
говорил исходный заголовок («ACB-fixture-gap whitelist'им с issue-ref»), не существует.
Ни одна из них никогда не скрывала must-DENY-leak.

| Case / step | Signature | Disposition |
|---|---|---|
| `SUBNET-LF-D-VISIBLE` / `SUBNET-LF-D-NOLEAK` / `SUBNET-LF-D-NONE` (list-filter-d) | subset-viewer / no-grant `List /vpc/v1/subnets` → **403**, устойчиво (не EC-окно: повтор через сутки даёт то же). Отказ наступал РАНЬШЕ пообъектной фильтрации, на методном гейте уровня проекта: карта прав vpc требовала на top-level List читательский ЯРУС `viewer` на проекте, а каталог шлюза объявлял глагол `v_list` — и в модели (`type project`) одно из другого **не выводится**, поэтому фактическим требованием было их пересечение, которого не выдавал никто. Пообъектный грант при этом резолвился, что и показывала канарейка `SUBNET-LF-D-GET-OK` (Get видимой подсети → 200 живьём). | **FIXED 2026-07-29** (не whitelist; маска снята 7c44f8d9 и не возвращается). Правой стороной оказалась **карта сервиса**, а занижал требование **каталог**: на той же проектной области `Create` уже гейтится ярусом `editor` (не `v_create`) в ОБОИХ артефактах, а три списочных RPC storage и `iam.ConditionsService/List` давно называли там `viewer` без единого расхождения; `v_list` на проекте — доступ к САМОМУ объекту проекта, про который модель говорит «never to its contents», то есть другой вопрос. Исправлены аннотации семи RPC (шесть vpc + `compute.InstanceService/List`), каталог перегенерирован, обе вшитые копии синхронны; фикстуры выдают ровно объявленное каталогом (`viewer` на проекте) — **утверждения кейсов не менялись**. Класс закрыт репо-широким гейтом `internal/repohygiene/catalogparity_test.go`. До 2026-07-29 три кейса были **зелёными и вакуумными**: утверждение читалось «список доступен (200) ИЛИ закрыт методом (403)», то есть принимало оба взаимоисключающих исхода и упасть не могло. Различающая проба и вскрыла дефект. `kacho-iam#276` |
| `SUB-RCB-CONF-STATE::verify-state` (subnet) | `verify-state` GET своей же subnet → **404** сразу после `RemoveCidrBlocks`, хотя Operation.done вернул `response=Subnet` с `v4CidrBlocks=[10.230.0.0/24]` (primary kept — продукт КОРРЕКТЕН). Первый пост-мутационный Get на read-consistency окне. | **FIXED** (не whitelist) — `verify-state` обёрнут в `retry_until_authorized` (RYW 403/404-ретрай, тот же паттерн, что RemoveCidr add/remove). Genuine non-converging 404 после бюджета всё равно FAIL — не маскируется. |
| `SG-DEL-NEG-NIC-ATTACHED` (security-group, `kacho-vpc#27`) | `SG.Delete` NIC-attached SG проходит вместо `FAILED_PRECONDITION` (dangling ref) | **FIXED** — product-side within-service refcheck реализован в repo `securityGroupWriter.Delete` (writer-TX: `FOR UPDATE` + `EXISTS(security_group_ids @> jsonb_build_array($id))` → FailedPrecondition, не TOCTOU). Persistent-RED снят; кейс ожидаемо GREEN. Integration-покрытие — `security_group_refcheck_integration_test.go`. |

## Эволюция

| Версия | Cases | Assertions | Среднее/рес | % target |
|---|---|---|---|---|
| v1 | 89 | 467 | 11 | 11% |
| v11 | 578 | 2528 | 82 | 82% |
| v12 (FK RESTRICT delete) | 597 | 2616 | 85 | 85% |
| v13 (Req/Immutable matrix + CIDR pack) | 624 | 2744 | 89 | 89% |
| v14 (pairwise + security probes + lifecycle) | 685 | 3107 | 97 | 97% |
| v15 (dup-name fix → SUB-CR-NEG-DUP-NAME) | 686 | 3120 | 97 | 97% |
| v16 (internal IPAM admin RPC: internal-pool/-region-zone/-cloud) | 731 | 3361 | — | — |
| v17 (contract alignment: sync-валидация в мутирующих RPC, Move-в-текущий-project → 400, Subnet CIDR ≤/28, Relocate → 400, error-texts) | ~731 | ~3360 | — | — |
| v18 (NetworkInterface first-class + v6-Subnet / optional-CIDR-Subnet / SG-без-network / NIC↔Subnet-RESTRICT / multi-resource delete-chain / operation-history-survives-delete / Network-public-без-data-plane-id / v6-CIDR-через-verbs; дедуп case-id + mandatory `scripts/validate-cases.py` (dup-id + каталогизация в CASES-INDEX) в CI до newman) | 736 | ~3380 | — | — |
| v19 (`NetworkInterface.mac_address` — output-only, cloud-wide UNIQUE, префикс `0e:` + 40 бит `crypto/rand`, retry-on-collision; новый `NIC-CR-MAC-OK` + `REQ-NIC-08`) | 737 | ~3385 | — | — |
| **v20 (AddressPool `cidr_blocks` → `v4_cidr_blocks` + `v6_cidr_blocks` split. +18 net new case-id (IPL-* / ADR-CR-EXT-FALLTHROUGH-V4/V6). Все остальные IPL-* кейсы — payload обновлен на split-shape. Новые REQ: REQ-IPL-CR-01..06, REQ-IPL-UPD-*, REQ-RESOLVE-*)** | **762** | **~3585** | — | — |
| **v21 (SG `network_id` mandatory+immutable + SG→SG rules same-network + Move guard. +9 net new SG-NET-* case-id. Переписаны под mandatory-контракт. Новые REQ: REQ-SG-RULE-SAME-NETWORK, REQ-SG-MOVE-NETWORK-BOUND; REQ-RES-07 переписан)** | **766** | **~3620** | — | — |
| **v22 (PE-ресурс и его сервис полностью удалены — backend, DB-таблица, proto-импорты, кейсы. Удален suite `private-endpoint` (64 case-id) + блок `define_resource_cases("private-endpoint", ...)` из `authz-deny.py`. CASES-INDEX / TEST-PLAN обновлены)** | **683** | **~3258** | — | — |
| **v24 (AddressPool CIDR-управление как у Subnet. Proto drop `replace_v4/v6_cidr_blocks`; добавлены `InternalAddressPoolService.AddCidrBlocks` / `RemoveCidrBlocks`. internal-pool: −5 replace-кейсов, +3 (`IPL-ADDCIDR-OK`, `IPL-RMCIDR-OK`, `IPL-RMCIDR-NEG-INUSE`). REQ-IPL-UPD-01 переписан, +REQ-IPL-ADDCIDR-01 / REQ-IPL-RMCIDR-01/02)** | **994** | **~3578** | — | — |
| **v25 (AddressPool DB-level CIDR overlap prevention — нормализованная `address_pool_cidrs` + EXCLUDE gist `(kind, block && )` (миграция 0004; within-service инвариант на DB-уровне). Пересечение CIDR per kind внутри/между пулами → `FailedPrecondition` "address pool CIDRs can not overlap" на Create / :addCidrBlocks; sync within-request precheck → InvalidArgument тем же текстом. internal-pool: +2 (`IPL-CR-NEG-OVERLAP`, `IPL-ADDCIDR-NEG-OVERLAP`); +REQ-IPL-OVERLAP-01. Integration `TestIntegration_AddressPoolOverlap_*` (4) зеленые)** | **996** | **~3590** | — | — |

## Покрытие формальных техник test design

| Техника | Реализация |
|---|---|
| ECP | ✅ `ecp_name_block`, `ecp_description_block`, `ecp_labels_block` |
| BVA | ✅ `crud_list_bva_block`, pagesize 0/1/1000/1001/10000 |
| Decision Tables | ✅ `required_fields_matrix`, `immutable_fields_matrix`, `updatemask_decision_table` |
| State Transition | ✅ STATE class, immutable, idempotent move-self |
| Pairwise | ✅ `pairwise_subnet_pack` (zone × prefix × dhcp, 9 кейсов из 18) |
| Cause-Effect | ✅ имплицитно через decision tables |
| Use-case | ✅ `conformance_lifecycle_pack` — full CRUD-цикл |
| Error Guessing | ✅ `malformed_body_block`, `headers_content_type_block`, edge cases |
| Exploratory | manual — не в автомате |
| Property-Based | ✅ `idempotency_block`, `pagination_roundtrip` |
| Risk-Based | ✅ priority P0..P3 tagging |
| Smoke | ✅ P0/P1 кейсы — фактический smoke |
| Functional regression | ✅ полная suite |
| Conformance | ✅ CONF class — тексты ошибок/коды/форматы + lifecycle |
| Performance | ✅ `perf_baseline_block` (response_time < 500ms) |
| Load/Stress/Soak/Spike | → перенесено в k6 (отдельный setup) |
| Chaos | → backlog |
| Security | ✅ `security_injection_block` (SQLi/XSS/cmd/path traversal × 7) |
| Compatibility | → backlog |
| Migration | covered внешними тестами |
| DR | → backlog |

## Findings

Найденные баги / расхождения — заводятся в issue-трекер репозитория; намеренные
особенности контракта — `docs/architecture/07-known-divergences.md`.
