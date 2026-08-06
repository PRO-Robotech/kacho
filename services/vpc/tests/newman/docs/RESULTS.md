# newman — прогон vpc-сюиты (числа замерены, не унаследованы)

## Сводка — прогон 2026-08-05, живой стенд kind-kacho, боевая посадка

Числа ниже **сняты с отчётов этого прогона** — JSON-отчёты newman в каталоге `out/`
суиты (он под `.gitignore`: отчёты производит прогон, в дереве их нет), а не перенесены из
предыдущего. Единицы — те же, которыми считает гейт
`services/iam/tests/newman/scripts/assert-suites-green.sh`: упавшие утверждения,
запросы без ответа, падения скрипта до утверждений. «Кейсов» — папок в
сгенерированной коллекции (`gen.py`), то есть счёт заявленного, а не исполненного.

| Коллекция | Кейсов | Запросов | Утверждений | Упало | Без ответа |
|---|---:|---:|---:|---:|---:|
| subnet | 129 | 779 | 1216 | 0 | 0 |
| security-group | 104 | 579 | 845 | 0 | 0 |
| address | 104 | 363 | 592 | 0 | 0 |
| network | 101 | 273 | 479 | 0 | 0 |
| route-table | 87 | 362 | 559 | 0 | 0 |
| gateway | 83 | 249 | 387 | 0 | 0 |
| authz-deny | 324 | 326 | 740 | 0 | 0 |
| network-interface | 14 | 273 | 396 | 0 | 0 |
| internal-pool | 32 | 147 | 228 | 0 | 0 |
| vpc1 | 34 | 237 | 455 | 0 | 0 |
| internal-network | 6 | 24 | 41 | 0 | 0 |
| operation | 6 | 22 | 37 | 0 | 0 |
| list-filter-d | 5 | 7 | 9 | 0 | 0 |
| concurrency | 4 | 175 | 51 | 0 | 0 |
| address-zone-coherence | 4 | 20 | 24 | 0 | 0 |
| observability | 1 | 3 | 2 | 0 | 0 |
| **Итого (16/16 отчиталось)** | **1038** | **3839** | **6061** | **0** | **0** |

> [!warning] Что здесь стояло до 2026-08-05 и почему это было хуже, чем ошибка в числе
> Заголовок объявлял «финальный прогон», таблица перечисляла **12** коллекций из
> **16** и подводила итог **996** кейсов, а следом стояло «100% PASS» — при том, что
> прогон 2026-08-04 дал по этой сюите **172 упавших утверждения в 10 коллекциях из
> 16**. Ошибались не только цифры:
>
> * **четыре коллекции отсутствовали в таблице целиком** (`vpc1`, `internal-network`,
>   `list-filter-d`, `address-zone-coherence` — 49 кейсов). Их не было ни в итоге, ни
>   в перечне, поэтому «Итого» нельзя было сверить с деревом: недостача выглядела как
>   меньший продукт, а не как непрочитанное;
> * **счёт разошёлся и по перечисленным** (subnet 137 против 129, network 103 против
>   101, gateway 84 против 83, route-table 88 против 87 — и в обе стороны:
>   security-group 101 против 104, address 103 против 104, internal-pool 31 против 32);
> * «100% PASS» было утверждением **без прогона за спиной** — оговорка «assertions/
>   requests — ориентир, перепрогнать suite» стояла в заголовке той же таблицы, то
>   есть документ сам сообщал, что его числа не измерены, и тут же выносил вердикт.
>
> Устаревание в сторону «лучше, чем есть» опаснее устаревания в любую другую: красный
> прогон читается как известный, а не как новый. Поэтому шапка теперь **называет дату,
> стенд и источник чисел**, а вердикт держится на отчётах, лежащих рядом.

> Suite `internal-cloud` (CloudPoolSelector) и RPC `Move`/`Relocate`, NIC
> `AttachToInstance`/`DetachFromInstance`, AddressPool override/Check/ExplainResolution —
> удалены вместе с соответствующими кейсами. IPAM cascade сведен к
> network_default → zone_default → global_default.

> Suite `internal-region-zone` (Region/Zone, prefix `RGN-*`/`ZON-*`) вынесен из этого
> репозитория вместе с доменом Geography; покрытие Region/Zone — в сервисе compute. Цифры
> в таблице выше — без него.

**Все 16 коллекций отчитались, упавших утверждений 0, запросов без ответа 0, падений
скрипта 0** (прогон 2026-08-05; это измеренный исход, а не унаследованная строка —
предыдущая редакция объявляла то же самое, не прогоняя сюиту). Объявленных
ожидаемо-красных в этой сюите нет — объявлявший их раздел снят
вместе с последней записью, а история снятых, с доказательством на каждую, лежит ниже
в разделе «Снятые объявления „ожидаемо красного“» (SG-rule-target, `#27` и кластер
IPAM-resolve сняты вместе со своими предметами). Покрыты internal/admin-only IPAM RPC
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

> Деплоймент-замечание: suite требует `KACHO_VPC_DEFAULT_SG_INLINE=true`
> (default) — `*-LSG-CRUD-DEFAULT-SG` / `*-DEL-STATE-DEFAULT-SG` проверяют
> авто-создание default SG. При `=false` (load-test config) эти кейсы краснеют.
> internal-* кейсы используют seeded `zone` region / `zone-{a,b,c,d}`
> zones / `default-zone-a` pool как readonly-фикстуры (не трогают),
> остальное — runId-суффиксованные throwaway-ресурсы с self-cleanup.

## Снятые объявления «ожидаемо красного» (rule #13) — история, доказательство на каждую

Правило #13 разрешает исключение из «100% pass» кейсу, который корректен, но зелёным
станет только после фикса продукта: он краснеет до фикса, в case-файле стоит
`# verifies <issue-url>`, `pm.test.skip` запрещён.

**Живых объявлений у этой сюиты нет — раздел, который их объявлял, снят вместе с
последним из них**, и заголовок здесь намеренно НЕ называет их термином. Причина
механическая: гейт `tools/knownfailingsubject` находит объявляющий раздел ПО
ЗАГОЛОВКУ, поэтому заголовок, который одновременно объявляет («known failing») и
отчитывается о снятии, невидим обеим половинам гейта сразу — проверено инъекцией:
объявление, вписанное под таким заголовком, не даёт ни одной находки, а под нынешним
— даёт. Новое объявление заводится **вместе** со своим объявляющим заголовком;
вписанное сюда останется без надзора. Ниже — снятые.

> **СНЯТО 2026-08-01 — `SG-NET-08-RULE-SAME-NETWORK-OK` и
> `SG-NET-09-RULE-SAME-NETWORK-UPDATERULES-OK` (`kacho#106`): предмет мёртв.**
> Запись утверждала, что `toPb` сериализует **только** ветку `CidrBlocks`, а
> `SecurityGroupId`/`PredefinedTarget` роняет в `Target=nil`, из-за чего
> `rule.securityGroupId` приходит `undefined`. На этой ревизии это неверно: в
> `services/vpc/internal/dto/toproto/security_group.go` сериализуются **все три**
> взаимоисключающие ветки target-oneof, и рядом стоит комментарий, объясняющий, зачем.
>
> Проверено на трёх уровнях, а не перечитыванием кода:
> `TestDTO_SecurityGroupRule_AllTargetOneofBranches` — PASS; против настоящей СУБД
> (testcontainers) `TestIntegration_SGNet_CreateSameNetworkRule_OK`,
> `TestIntegration_SGNet_UpdateRulesSameNetwork_OK` (утверждает round-trip
> `rec.Rules[0].SecurityGroupID == sgA`) и `TestIntegration_SGNet_CidrAndPredefinedRulesUnaffected`
> — 3/3 PASS за 4.0 с (прогон 2026-08-01). Разбор запроса на входе тоже на месте:
> `services/vpc/internal/apps/kacho/api/securitygroup/helpers.go` читает
> `rs.GetSecurityGroupId()`. Цепочка «запрос → хранение → чтение» замкнута целиком.
>
> **Почему запись пережила фикс.** Фикс приехал 2026-07-19 (`33cf29a`), а тикет
> `kacho#106` заведён 2026-07-30 — **на одиннадцать дней позже**. Тикет завели именно
> затем, чтобы записи было чем истечь (прежняя редакция строки это и признаёт: «до
> этого запись стояла „issue pending“, то есть истечь ей было нечем»). Получилась
> обратная зависимость: не строка ждала дефекта, а тикет ждал строку. Никакое
> состояние тикета не было свидетельством о продукте — он открылся, когда чинить
> было уже нечего. Отсюда правило «условие снятия называет дефект, а не тикет»
> (гейт `tools/knownfailingsubject`).


> **ЗАКРЫТО 2026-07-31 — записи снесены вместе с предметом.** Набор `SG-URL-VAL-*`
> прошёл полный цикл. Сначала (2026-07-29) с пяти кейсов сняли утверждение
> `oneOf([200, 400])` под меткой «rejected sync or async»: оно принимало и приём
> правила, и отказ в нём, поэтому ни один отрицательный кейс не мог упасть —
> независимо от того, что делает продукт. Три из пяти сразу покраснели на реальном
> расхождении и были объявлены здесь как known-failing. Замер в боевой посадке
> (прогон 4, 2026-07-30) подтвердил их живьём: шесть упавших утверждений, три
> мутации приняты вместо отказа.
>
> Теперь диапазон портов, имя и номер протокола проверяются синхронно, первым
> стейтментом, с именем поля в отказе, поэтому объявлять больше нечего и три
> строки удалены. Запись, которой нечего исключать, — находка, а не память о
> былом; история остаётся здесь текстом.

> **`SG-DEL-NEG-NIC-ATTACHED` ([#27](https://github.com/PRO-Robotech/kacho-vpc/issues/27)) — FIXED.**
> `SG.Delete` SG'а, прилинкованного к NIC через `security_group_ids[]`, теперь отвергается
> `FAILED_PRECONDITION` (code 9). Product-side refcheck реализован в repo
> `securityGroupWriter.Delete`: ВНУТРИ writer-TX `SELECT id … FOR UPDATE` +
> `EXISTS(security_group_ids @> jsonb_build_array($id))` → `ErrFailedPrecondition`
> «security group is in use by network interface(s)» (не TOCTOU — проверка+DELETE в одной TX).
> Покрыто integration-тестами `TestCQRS_SG_Delete_BlockedByNICReference` /
> `…_Concurrent_Referenced_AllBlocked` / `…_Concurrent_Unreferenced_ExactlyOne`
> (`internal/repo/kacho/pg/security_group_refcheck_integration_test.go`, testcontainers, `-race`).
> Пометка persistent-RED СНЯТА с кейса. Прежняя редакция обещала здесь «ожидаемо GREEN
> при следующем прогоне» — прогон случился (2026-08-04, отчёт коллекции security-group), и
> обещание не сбылось: кейс красный, но **не по предмету `#27`** — шаг удаления получает
> `403` на гейте прав (`expected 403 to be one of [200, 400]`), то есть проходящего
> удаления, ради запрета которого запись стояла, здесь не наблюдается вовсе. Этот `403`
> идёт по всей коллекции (18 упавших утверждений, тот же тон отказа в соседних кейсах) и
> разбирается отдельно от `#27`.
> (До 2026-07-05 кейс маскировался условным `pm.test.skip`; SEC-hardening r2 сделал его
> безусловным persistent-RED — прежнее состояние, снятое вместе с дефектом, — и это
> держало давление на прод-фикс.)
>
> **2026-08-01 — пометка снята с кейса.** Строка `# verifies …/kacho-vpc/issues/27` в
> `cases/security-group.py` пережила свой предмет на месяц: она означает «кейс ожидаемо
> КРАСНЫЙ» и выкупала его из «всё обязано быть зелёным», хотя абзац выше и её собственный
> соседний комментарий оба называли дефект исправленным. Отказ доказан тремя
> интеграционными тестами против настоящей СУБД (прогон 2026-08-01: 3/3 PASS, 61.8 с).
> Тикет `PRO-Robotech/kacho-vpc#27` при этом **открыт** — и в этом суть: гейт
> `auditknownfailing` снимает запись по ЗАКРЫТОМУ тикету, поэтому эту не снял бы никогда.
> Тикет — не дефект. Закрыть его артефактом (коммит фикса) — отдельное действие наружу.

> **СНЯТО 2026-08-04 — кластер IPAM-resolve в `internal-pool` (`kacho#107`): предмет мёртв.**
> Запись объявляла ожидаемо красными три кейса —
> `IPL-ALLOC-POOL-EXHAUSTED`, `IPL-RESOLVE-DUALSTACK-OK`, `IPL-RMCIDR-NEG-INUSE` — и семь
> их шагов; предметом был детерминированный отказ резолва пула в throwaway-зонах.
>
> Условие снятия эта запись назвала сама («прогон `internal-pool` даёт ноль упавших
> утверждений на этих трёх»), и выполнено оно ЗАМЕРОМ, а не перечитыванием: прогон e2e
> 2026-08-04 (отчёт коллекции internal-pool) — 227 утверждений, **0 упавших**, ни одной записи
> в `run.failures`. Отчёт не протух: он описывает коллекцию из 137 запросов — ровно
> столько же, сколько в закоммиченной `collections/internal-pool.postman_collection.json`.
>
> Прошли поимённо ровно те утверждения, которые ловили этот отказ: `alloc-1`/`alloc-2` —
> «success», а третья аллокация — FailedPrecondition «пул исчерпан» (значит резолв не
> просто отвечает, но и отличает исчерпание); `get-v4`/`get-v6` — операция без ошибки и
> адрес из своего блока пула (`100.100.0.` / `2001:db8:ff:`); `cr-addr` — операция без
> ошибки с непустым адресом; `remove-inuse` — 400, код 9 и текст `has allocated addresses`
> вместо прежнего `CIDR blocks not found`. Ни одно утверждение по дороге не ослаблялось.
>
> `kacho#107` при этом **открыт**, и запись снята не по нему: тикет — не дефект, его
> закрытие артефактом остаётся отдельным действием наружу. Так и было записано в условии
> снятия — оно намеренно называло предмет, а не состояние тикета.

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
