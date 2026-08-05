# kacho-geo newman — RESULTS

Прогон-статус geo regression-suite. Source-of-truth кейсов — `cases/*.py`;
каталог — `docs/CASES-INDEX.md`.

## Статус прогона

**`gen.py` + `validate-cases.py` + `selftest-assertions.js` — GREEN** (локально, без сети):
- `python3 scripts/gen.py` → 7 коллекций сгенерированы.
- `python3 scripts/validate-cases.py` → OK, уникальные case-id, все каталогизированы.
- `node scripts/selftest-assertions.js` → 36 шагов-мутаций с ожидаемым успехом и 3 с
  ожидаемым отказом: каждый краснеет на подставленной `Operation{error}` и молчит на
  успешной. Гейт запускается сам из `run.sh` перед newman: до него шаг-мутация
  проверял только форму 200-конверта, а отказ мутации приезжает в `result.error`,
  поэтому провалившаяся мутация зачитывалась как принятая.

**Newman-прогон против стенда — PENDING clean-seed CI.** Локальный newman-замер против
уже-развёрнутого стенда даёт ложные падения, если pod'ы geo/gateway СТАРЕЕ ветки
`redesign/integration` (см. §Известное расхождение стенда ниже); арбитр зелёности —
CI-раннер (`deploy/scripts/newman-parallel.sh`, шард `edge`) на СВЕЖЕ собранном
и посиженном стенде (`tests/authz-fixtures/setup.sh` минтит fresh-JWT'ы и патчит geo env;
`--env-var baseUrl/internalBaseUrl` инжектятся раннером на public :18080 / internal :18081).

**Ожидаемый результат прогона (GEO-1 landed): все коллекции GREEN, 0 RED.** Прежние 2
RED-lock'а (#55, public OpsProxy geo-routing) сняты как неконтрактные под GEO-1 (см. ниже).

## Реконсиляция под GEO-1 (что было починено — 2026-07-21)

Suite был написан на **опровергнутой** премиссе, что GEO-1 «merge-gated и НЕ приземлён».
Ground-truth (CI-артефакт + live-probe + source `redesign/integration`) показал: **GEO-1
ПРИЗЕМЛЁН** — public read отдаёт GEO-1-shape (`openForPlacement°`/`placementBlockedReason°`,
без сырого `status`), admin CRUD живёт на `/geo/v1/internal/…`, мутации возвращают
`Operation{done:true}` синхронно, public read — ambient (project-scope EXEMPT). Каждое из
176 падений артефакта — следствие этой одной премиссы:

| root-cause | падений (каскад) | вердикт | фикс |
|---|---|---|---|
| Internal admin бился в public path `/geo/v1/regions` вместо `/geo/v1/internal/regions` → gateway authz «catalog: no entry for method» 403 → нет opId → op-poll/Update/Delete каскад | ~85+ | **test-bug (wrong-path)** | внутренние Step'ы → `/geo/v1/internal/…` (internal-region/zone, admin-not-on-public, authz-deny, operation) |
| public Zone assert `status` present | zone/internal-zone | **test-bug (stale shape)** | assert `openForPlacement°` (derived); `status` НЕ на public (two-projection) |
| authenticated zero-binding public read ждал 403 (viewer-gate) | authz-deny | **test-bug (stale authz premise)** | GEO-1-20 ambient → 200 (`GEO-REG-GT-AUTHZ-AMBIENT-OK`) |
| zone.id не coupling-valid (`qa-zon-…` под `qa-zreg-…`) | internal-zone/region | **test-bug** | zone.id = `<regionId>-<suffix>` (GEO-1-29 coupling) |
| omitted zone status ждал UP (AS-IS) | internal-zone | **test-bug (stale default)** | fresh → DOWN (GEO-1-12 fail-safe) |
| malformed-id ждал 400, стенд отдал 404 | region/zone | **stale deploy (НЕ баг)** | source `domain.ValidateID` отдаёт `INVALID_ARGUMENT` первым стейтментом (GEO-1-31) → тест корректен, зеленеет на свежесобранном geo |

Прод-код НЕ трогался (test-only PR, ban #13). Реальных прод-багов против GEO-1-acceptance
не найдено — все падения объясняются test-staleness ЛИБО устаревшим (несобранным из ветки)
geo-pod'ом на локальном стенде.

## Снято: #55 RED-lock (неконтрактно под GEO-1)

Прежние `GEO-IOP-POLL-ROUTING-OK` / `GEO-IOP-GET-AUTHZ-BOLA` RED-локали
PRO-Robotech/kacho#55 (gateway OpsProxy `prefixToBackend` без prefix `geo` → geo op-id не
проксируется через ПУБЛИЧНЫЙ `/operations/{id}`). Под GEO-1 geo admin-мутация —
`Operation{done:true}` синхронно + admin/internal-only; клиент разворачивает `.response`,
op-poll не нужен, а публичный OpsProxy-роутинг geo-op **НЕ является контрактом GEO-1
acceptance** (GEO-1-16 поллит опционально через internal `/geo/v1/internal/operations/{id}`).
RED-lock неконтрактной поверхности = suite красный впустую → заменён позитивом
`GEO-IOP-SYNC-DONE-OK` (Operation{done:true, metadata.regionId, response=public Region}).
`prefixToBackend`-фикс geo остаётся отдельным (не-GEO-1) улучшением, если публичный
OpsProxy-poll geo-op когда-либо понадобится — но это НЕ блокирует зелёность suite.

## Отложенные GEO-1-сценарии дописаны (2026-07-27) — что прогнано, что нет

Набор принимался forward-compatible ДО посадки GEO-1 и покрывал контракт только по
самым широким точкам: anonymous **List**, один ambient **List**, один internal
**Create**-deny. Дописаны 15 кейсов ровно там, где контракт мог сломаться молча:

* **authN на публичном read** (exempt снимает authZ, не authN): anonymous **Get-by-id**
  region/zone — `Get` несёт СВОЮ `<exempt>`-аннотацию, покрытие `List` за него не говорит.
* **ambient-200**: zones-List (раньше был только regions) + Get-by-id обоих ресурсов.
  Субъект — `jwtPureNoBindings` (выделенный never-granted), НЕ `jwtNoBindings`: последний
  является grant-ТАРГЕТОМ в iam-суитах, и под параллельным прогоном может транзиентно
  иметь привязки — тогда «zero-binding читает каталог» доказывался бы принципалом,
  который в тот момент zero-binding не был.
* **two-projection на публичной поверхности**: сырой `status` на Region (был проверен
  только у Zone) + отсутствие infra/`status` на **List**-проекции обоих (List — отдельный
  путь сериализации, покрытие `Get` за него не отвечает).
* **internal listener целиком**: anonymous → 401 (internal НЕ освобождён от authN) +
  Update/Delete/**GetInternal** deny. `GetInternal` — та самая дверь к сырому `status` и
  `infra`, и у неё не было ни одного deny-кейса.

**Прогнано против живого стенда (read-only, стенд не поднимался и не перекатывался):**
3 anonymous-кейса — `GEO-REG-GET-AUTHZ-ANON-DENY`, `GEO-ZON-GET-AUTHZ-ANON-DENY`,
`GEO-REG-CR-AUTHZ-ANON-DENY` — **GREEN** (по 1 выполненному запросу, 3/3/2 ассерта).
Замок проверен инъекцией: ожидание «anonymous internal Create → 200» → **RED**
(`expected 401 to deeply equal 200`, тело — `AUTHN_REQUIRED`), возврат → **GREEN**.

**НЕ прогнано здесь: 12 кейсов, требующих токена.** Живой env-файл суиты gitignored, а
его посев (`tests/authz-fixtures/setup.sh`) — запись на общем стенде; единственный
доступный сохранённый env просрочен на ~10 суток. Эти кейсы **не заявляются зелёными** —
их арбитр CI-раннер на свежепосеянном стенде. Отсутствие токена они переживают
громко, а не тихо: `gen.py` хардфейлит шаг с пустой auth-переменной
(`harness config: <var> is set`), поэтому «прогон без фикстур» даёт RED, а не ложный GREEN.

**Гейты:** `validate-cases.py` → OK (57 уникальных id, все каталогизированы; до внесения
строк в `CASES-INDEX.md` гейт был RED по всем 15 — это и есть его red→green пара);
`gen.py` → 7 коллекций, число сгенерированных кейсов пофайлово совпадает с числом
объявленных `CASES.append` (2/17/6/7/2/13/10).

Побочно исправлен неверный комментарий в `cases/zone.py`: он утверждал «raw status IS
present AS-IS» рядом с ассертом, который проверяет его ОТСУТСТВИЕ. Написан до GEO-1 и с
тех пор ложен; такой комментарий провоцирует «починить» ассерт под себя.

## Известное расхождение стенда (stale pod) — НЕ баг теста

На момент реконсиляции локально-развёрнутый `kacho-geo` pod (11h old) СТАРЕЕ ветки:
`GetInternal` → `code 12 unknown method`, Internal Create op-id == resourceId (не
`geo…`), malformed-id → 404 (нет format-check). Source `redesign/integration` (6df8537)
ВСЁ это реализует (GetInternal, `NewID("geo")` op-id, `domain.ValidateID` первым
стейтментом, `Operation{done:true}` via `syncop`). Тесты нацелены на **source/acceptance**
контракт — CI, пересобирающий geo из ветки, даёт зелёный; локальный прогон против stale
pod'а даст ложные падения GEO-1-полей до пересборки/redeploy geo.

## Расширяемая поверхность (следующий инкремент)

Net-new GEO-1-сценарии, ещё не покрытые кейсами (integration-tester/qa follow-up).
Из прежнего списка снято то, что дописано 2026-07-27 (см. выше): deny-полоса
`GetInternal`/Update/Delete, anonymous на internal, ambient-Get-by-id, two-projection на
List. Остаётся:
`GetInternal` **positive** full projection под admin (status+infra°, GEO-1-01), `warnings°` fresh-DOWN loud no-op
(GEO-1-12/13), `?regionId`/`?openForPlacement` list-фильтры (GEO-1-24/26), immutable regionId
reject (GEO-1-32), `countryCode` ISO-3166 формат (GEO-1-39), `UNIQUE(name)` dup op.error
(GEO-1-36), coupling strict-startsWith counter-пример `ru-central10-a` (GEO-1-30),
INTERNAL-opaque на write-ошибке (GEO-1-37).
