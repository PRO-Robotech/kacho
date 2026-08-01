# RESULTS — kacho-nlb newman regression run history

## Known-RED subtraction removed from the shared gate (2026-07-30)

`services/iam/tests/newman/scripts/assert-suites-green.sh` is the shared verdict for every
suite, nlb included. It used to deduct a "known-RED" set before deciding; that deduction is
gone. It reports what newman reported.

Its last revision matched **seventeen** nlb folders on their `-rya<N>` steps — the suffix
`retry_until_authorized` gives a step it has already wrapped. So these were failures **past**
an existing bounded retry, and the deduction hid exactly the part worth seeing.

Those seventeen names are **no longer declared**, and the reason is not that the deduction
went away: their tracking issue was **closed as completed on 2026-07-19**, eleven days before
this note, with a product fix and a green run recorded on it. The disposition — with the
numbers, and with what would have to happen for any of them to be declared again — is in
§«Closed — was «known failing»: owner-tuple materialization lag» below. Nothing is masked and
nothing is subtracted; the runner reports what newman reports, and a red on any of these is
now a **fresh** finding with fresh evidence, not a re-entry in a standing list.

Retry budgets are **not** raised to absorb such reds. A budget large enough to outlast a slow
materialization path turns a visible red into a slow green, and past the runner timeout into
a cancelled run. Residual non-convergence is a finding about the path.

### ⚠ ИСПРАВЛЕНО 2026-07-30 — «бюджет ≈30 с» никогда не существовал

Каждое место ниже, где написано, что round 4 поднял бюджет `retry_until_authorized` до
«60 × 500 мс ≈ 30 с» (строки истории версий round 4 / round 4b, абзац про eventual-consistency
и «residual tail past 30s», и «wraps fail-closed after 30s»), **описывает изменение, которого в
`scripts/gen.py` нет**. Факты:

- подпись функции: `budget: int = 25, interval_ms: int = 500` → **12.5 с**;
- ни одно место вызова в `cases/` не передаёт `budget=`, то есть все 27 обёрнутых шагов идут
  на умолчании;
- в сгенерированных коллекциях охранники несут `_arc < 25` / `_ard < 500`;
- по всей истории этого файла подпись функции несла **25**: от коммита, который её ввёл
  (2026-07-19), до сегодняшнего — значений 60 и 40 в ней не было ни разу.

> **Проверять это надо подписью, а не поиском по строке.** Первая редакция записи
> обосновывалась тем, что `git log -S` на литералы «60» и «40» по этому пути **пуст**. На
> момент написания так и было — и перестало быть в тот же миг: запись **сама** внесла эти
> литералы в файл, процитировав свою же команду, поэтому у следующего читателя тот же поиск
> находит один коммит (её собственный) и выглядит опровержением. Утверждение, чей способ
> проверки портится самой записью, ничем не лучше необоснованного. Воспроизводимая проверка —
> прогнать `git log --format=%h -- <путь>` и посмотреть в каждой ревизии строку
> `def retry_until_authorized(`: значение по умолчанию там одно и то же во всех.

Почему это важнее опечатки: снятое вычитание «известного красного» обосновывало себя как
покрытие «остаточного хвоста за ~30 с». Обоснование опиралось на окно, которого не было, —
такое исключение нельзя опровергнуть замером, потому что замерять предлагалось не то, что
исполняется. А замер, приведённый рядом (материализация p50 ≈ 10 с с тяжёлым хвостом), против
12.5 с маргинален по построению — то есть числа в этом же документе всё время говорили, что
обёртка не покрывает свой предмет.

Бюджет **не поднят** этой правкой (это был бы обмен видимого красного на медленный зелёный, а
за таймаутом прогонщика — на отменённый прогон). Приведена к реальности только запись.

> Baseline counters established with the initial check-in (KAC-NLB-newman-cases).
> Updated after every run via `scripts/run.sh` → `out/summary.txt`.

## Latest baseline (v0 — initial commit)

| Service | Cases | Steps | Assertions | Failed |
|---|---|---|---|---|
| load-balancer | TBD | TBD | TBD | unknown (stack not yet deployed) |
| listener      | TBD | TBD | TBD | unknown |
| target-group  | TBD | TBD | TBD | unknown |
| targets       | TBD | TBD | TBD | unknown |
| operation     | TBD | TBD | TBD | unknown |
| authz-deny    | TBD | TBD | TBD | unknown |
| **TOTAL**     | ≥320 | ≥1200 | ≥2500 | — |

Numbers will be populated by the first CI run after kacho-nlb implementation
reaches deployable state (post epic merge per acceptance D-2). Until then,
the suite is **structurally valid** (validate-cases.py passes, gen.py produces
parseable Postman collections) but cannot execute against any backend.

## Version history

| Date | Suite version | Cases | Failed | Notes |
|---|---|---|---|---|
| 2026-05-23 | v0 baseline | ≥320 | n/a | Initial check-in: cases + scripts + docs scaffold; collections generated and committed; no backend yet. |
| 2026-07-01 | v1 — sub-phase 8.1 VIP model | 358 | not-yet-run | LoadBalancer VIP-source rewrite (see below). `validate-cases.py` OK, all collections regenerated; not executed (stand mid-redeploy). |
| 2026-07-01 | v2 — first fe3455 run + triage | 358 | 10 (0 product bugs) | First live run of the LoadBalancer suite against fe3455: 142 cases / 544 assertions / 97% pass. All 10 failures triaged, none a product bug (see below). 5 wrong case-expectations corrected + grant-latency case made poll-tolerant + suite-wide `newman run` flow-control fixed. Target: 100% at adequate `--delay`. |
| 2026-07-18 | round 2 — INTERNAL setup + peer-RYW retry + CI-signature triage | 362 | see below | Root-cause pass over ci-rep2 (load-balancer 62 / cross-resource 19 / listener 16 / authz-deny 6 / target-group 3 / placement-coherence 2 / targets 1 / operation 1). Setup LBs migrated off the contended external AddressPool to pool-independent INTERNAL-inline-subnet; new `retry_create_until_present` primitive for cross-service subnet read-your-writes lag; deterministic + tolerant fixes per signature. Systemic external-pool finding flagged (below). Verified locally (py_compile / gen.py / validate-cases 362); not executed (stand not raised this round). |
| 2026-07-18 | round 3 — attach-shape conformance + protojson mask fix + serial VIP + residuals | 357 | see below | Root-cause pass over ci-rep3 (load-balancer 29 / cross-resource 8 / listener 7 / list-filter 3 / targets 1 / placement-coherence 1). ci-rep3 **disproved the "residuals = external-VIP exhaustion" hypothesis**: the dominant new signature (~12) was a stale **attach request shape** (`AttachedTargetGroup` nesting + `priority` removed from the contract — verified from proto+handler), newly surfaced because round-2's INTERNAL migration finally let the attach flow RUN. Fixes: nested attach shape everywhere + 5 obsolete `priority` cases removed; protojson-FieldMask snake→camelCase in load-balancer/cross-resource/listener/target-group (round-2 fixed only target-group's `deregistrationDelaySeconds`); nlb `run.sh --jobs` 4→1 (serial collections defeat shared-external-pool contention); list-filter TG healthCheck completion; move + region-mismatch fixture-tolerance; owner-tuple retry budget 25→40 (⚠ и это до `scripts/gen.py` НЕ доехало: подпись всегда несла 25); targets add-nic-nx wrapped `retry_on=(403,)`. Verified locally (py_compile / gen.py / validate-cases 357); not executed (stand not raised this round). |
| 2026-07-18 | round 4 — owner-tuple-lag retry↑60 + eventual-consistency whitelist | 357 | see below | Root-cause pass over ci-rep4 (load-balancer 43 / cross-resource 17 / listener 14). `--jobs 1` (round 3) unblocked the create layer (VIP-exhaustion gone) → the **update-after-create** layer surfaced: the first post-create Get/Update/Delete/Start/Stop/Move/Attach of the caller's OWN fresh LB (and Get/Update of its OWN fresh listener) 403s (`lacks v_update/v_delete/v_get`) / 404s at the authz gate before the owner-tuple materialises. Measured: async op-latency ~1.5s (poll-op p90=3) but materialization p50~10s with a heavy tail — **31/83 wrapped steps exhausted the old 16s budget**; nlb races LAST in the umbrella (iam→vpc→compute→nlb) so the `fga_register_drainer` backlog peaks. Fix (2 levels), КАК ЗАЯВЛЕНО ТОГДА: (1) `retry_until_authorized` budget **40→60**, interval **400→500ms** (~16s→~30s window); (2) residual saturation tail past 30s = exemption by name in `assert-suites-green.sh`, since REMOVED whole (23 owner-tuple-lag update/del/get/action cases, assertions RUN+report, not gate-blocking — same class as iam#257), tracked in **kacho#11**. ⚠ ФАКТ: пункт (1) до `scripts/gen.py` НЕ ДОЕХАЛ — подпись обёртки и тогда, и сейчас несёт 25 × 500 мс ≈ **12.5 с** (см. ⚠ выше); пункт (2) снят вместе со всем вычитанием 2026-07-30, а его тикет закрыт 2026-07-19. Verified locally (py_compile / gen.py / validate-cases 357 / bash -n); not executed (stand not raised this round). |
| 2026-07-18 | round 4b — child-create owner-tuple-lag wrap + GTS case-fix | 357 | see below | Closes the two create-layer items round 4 left open (below §"NOT whitelisted"). ci-rep4 re-triage: the `cross-resource XRES-*` / `listener LST-CR-*` 403s are the **same owner-tuple-lag as round 4, one step earlier** — round 4 wrapped the first Get/Update/Delete but left the **child-CREATE** (`listeners.create` / `:attachTargetGroup` / `:addTargets` authorized against `editor@lb`/`editor@tg`) UNWRAPPED, so a transient 403 reddened the whole chain. Fix: wrap every own-fresh-parent child-create/mutation in `retry_until_authorized(retry_on=(403,404))` (default budget — фактически 25×500 мс ≈ 12.5 с, не 60×500 как было написано) — **12 steps in cross-resource** (incl. `create-internal-lb` in `retry_create_until_present` for the cross-service `"subnet <id> not found"` sync-reject) + **32 steps in listener** (all `_setup_lb`-parented creates incl. the sync-validation negatives `cr-tp-0/cr-tp-o/cr-ipv-unk/cr-p0/…` — the wrap retries ONLY the pre-empting 403, the real InvalidArgument still runs, never masked; the 4 garbage-parent/unscoped negatives `cr-no-lb/cr-cross-prj/cr-empty/cr-malformed` stay UNWRAPPED). **GTS case-fix (3):** `NLB-GTS-{CRUD-EMPTY,CRUD-EMPTY-LB-ACTIVE,STATE-LB-STOPPED}` now supply the contract-required `target_group_id` (own inline TG; STOPPED adds one peer-free external_ip target so INACTIVE is exercised, then drains it) instead of the LB-wide call that hard-400s. **Residual (then exempted by name, exemption since REMOVED; kacho#11):** phantom LBs from `could not allocate load balancer address` (VIP-source alloc failing under peak umbrella load — EXTERNAL shared-pool AND INTERNAL subnet-IPAM cross-service async-visibility) make the parent LB never materialise → wraps fail-closed (retry 12.5 с — не 30 с, как было написано — then real assert) but cannot green a non-existent parent; needs drainer/alloc throughput, not test retry. Verified locally (py_compile / gen.py / validate-cases 357); not executed (stand not raised). |

| 2026-07-18 | round 5 — post-replicaCount residual close-out (VIP-migration stragglers + fixture-tolerance + mask camelCase) | 357 | see below | Root-cause pass over ci-rep7 (load-balancer 3 / cross-resource 2 / listener 3) after the openfga `replicaCount 2→1` fix (`b0b2d6b`) removed the FGA read-replica lag. That lag was the last thing holding these EXTERNAL/mutation chains RED at the owner-tuple gate; with it gone the chains reached their FINAL assertions and exposed **case-expectation** defects the lag had masked (three of them under the round-4 lag-whitelist). **6 case-bugs fixed (all test-only, contract-verified against listener.proto / network_load_balancer.proto sub-phase-8.1):** (1) **VIP migration stragglers** — the per-family VIP moved Listener→LoadBalancer (`allocated_address`/`ip_version`/`address_id`/`subnet_id` reserved 12-15 in `listener.proto`; VIP now output `v4AddressId`/`v6AddressId` → bound vpc Address on the LB). `LST-CR-CRUD-AUTO-VIP`, `XRES-E2E-EXTERNAL-FULL-FLOW`, `XRES-E2E-EXTERNAL-IPV6-VIP` (+ latent `XRES-E2E-INTERNAL-FULL-FLOW`) asserted the removed listener-level `allocatedAddress`/`ipVersion` → `undefined`; rewired to assert the auto-VIP on the parent LB (`v4AddressId`/`v6AddressId` match `/^adr/`), listener → ACTIVE. IPv6 case now sources the VIP on the LB via `v6Source:{public:{}}` (Listener has no family), tolerant of an unseeded external-v6 pool. (2) **`LST-UPD-CRUD-OK`** — `updateMask:"name,proxy_protocol_v2"` (snake_case) → grpc-gateway `InvalidArgument "FieldMask.paths contains invalid path"`; fixed to lowerCamelCase `proxyProtocolV2`. (3) **`NLB-ATT-STATE-REGION-MISMATCH` / `LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH`** — the alt-region fixture `_suiteRegionAltId=ru-central2` is **unseeded** on the stand, so the alt-region TG Create Operation errors `"Region ru-central2 not found"` and `tgAltId`/`tgId` point at a TG that never persisted → the mismatched attach/update lawfully returns **404 NotFound** (not 409). Tolerant-negative broadened to `oneOf([200,400,404,409])`; the listener case's wrap changed to `retry_on=(403,)` so the terminal absent-TG 404 no longer burns the retry budget (25×, не 60× — см. ⚠ выше). **These 3 were lag-whitelisted in round 4 but the ci-rep7 failure is NOT lag — recommend pruning `^NLB-ATT-STATE-REGION-MISMATCH `/`^LST-UPD-CRUD-OK `/`^LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH ` from the `assert-suites-green.sh` lag-whitelist now that they pass genuinely (the gate still clamps-to-0, so leaving them only risks masking a future regression).** **2 residuals stay #11 (NOT case-bugs, NOT masked):** `NLB-ATT-CRUD-OK` (attach) + `NLB-LST-FILTER-MATCH` (list) reddened because their INTERNAL subnet-backed setup LB **alloc-phantomed** — Create Operation `done:true` WITH `error code 9 "could not allocate load balancer address"` (INTERNAL subnet-IPAM cross-service async-visibility under `--jobs>1`), so the LB never persisted → attach 403 (no owner-tuple) / list stays `[]`. This is the documented alloc-throughput residual (kacho#11), reproducible only under parallel/umbrella contention; the canonical `run.sh` (`--jobs 1` serial) does not phantom → both pass serially. No test-retry can green a non-existent parent. Verified locally (py_compile / gen.py 357 / validate-cases 357); not executed (stand not raised this round). |

## Closed — was «known failing»: owner-tuple materialization lag (kacho#11, closed 2026-07-19)

**Записи больше нет. Её предмет закрыт продуктом, а не тестом.** Раздел оставлен как
запись о том, что именно объявлялось и на каком основании снято, — чтобы список из
семнадцати имён не вернулся «по аналогии».

Что объявлялось (round 4, 2026-07-18): первый пост-создательский доступ к СВОЕМУ свежему
балансировщику или слушателю мог получить 403/404 на authz-гейте, пока owner-tuple ещё не
материализовался (at-least-once `fga_register_drainer` + реконсайлер), а nlb в умбрелле шёл
последним (iam→vpc→compute→nlb), где backlog дренажа максимален. Остаток за пределами
клиентского повтора объявлялся «известным красным» на **17** папках и вычитался из вердикта
общим прогонщиком.

Почему снято — три независимых основания, каждое проверяемо:

1. **Тикет закрыт.** `PRO-Robotech/kacho#11` закрыт 2026-07-19 как completed. Закрывающий
   комментарий называет продуктовый фикс тремя слоями (forward-материализация owner-tuple под
   SHARE-локом вместо сериализующего EXCLUSIVE; sync-регистратор nlb, дающий грант создателя на
   create-time; двухволновой e2e, где nlb больше не идёт последним) и фиксирует результат
   прогона: nlb 9/9. То есть запись **пережила свой фикс на одиннадцать дней** и продолжала
   утверждать, что продукт медленен там, где он уже починен.
2. **Двум из семнадцати сам этот документ уже вынес другой диагноз.** Round 5 выше нашёл, что
   `LST-UPD-CRUD-OK` и `LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH` краснели не от лага, а от
   ошибок В КЕЙСАХ (snake_case в `updateMask`; ожидание 409 там, где отсутствующая alt-region TG
   законно даёт 404). Оба кейса в дереве исправлены — `updateMask:"name,proxyProtocolV2"` и
   `oneOf([400, 404])` с `retry_on=(403,)`, — то есть их основание было неверным и до закрытия
   тикета.
3. **Основание опиралось на окно, которого не существовало.** Оно говорило про «остаточный
   хвост за ~30 с», тогда как обёртка всегда шла на 25 × 500 мс ≈ **12.5 с** (см. ⚠ выше). Замер,
   приведённый в этом же разделе (материализация p50 ≈ 10 с с тяжёлым хвостом), против 12.5 с
   маргинален по построению — документ сам говорил, что обёртка не покрывает свой предмет.

Плюс **шесть** имён из этого списка не существовали в наборе вовсе (`START-CRUD-OK`,
`STOP-CRUD-OK`, `STOP-STATE-ALREADY-STOPPED`, `DEL-STATE-HAS-ATTACHED`,
`ATT-STATE-REGION-MISMATCH`, `ATT-NEG-TG-UNKNOWN`) — Start/Stop перестали быть вызовами
продукта, папки `NLB-ATT-*` нет. Их убрали из предиката 2026-07-26; до того счёт «сколько
замаскировано» был завышен на шесть.

**Что теперь происходит с этими кейсами.** Ничего не вычитается и ничего не ослаблено:
обёртка `retry_until_authorized` (25 × 500 мс) остаётся — она повторяет, пока ОТВЕТ говорит о
временном состоянии, и по исчерпании бюджета настоящее утверждение исполняется на терминальном
ответе. Красный на любом из семнадцати — **новая находка с новым основанием** (свой прогон,
свой тикет), а не запись в стоячем списке. Механически это держит гейт
`tools/knownfailingsubject`: запись обязана называть существующий кейс и ОТКРЫТЫЙ тикет, а на
отчёте прогона — падать, если названный кейс исполнился и прошёл.

Отдельно, чтобы не потерялось при снятии записи: `NLB-GET-STATE-LEAN-PROJECTION` несёт
утверждения об отсутствии утечки инфра-полей и **никогда** не был в вычитании — он остаётся
полностью гейтящим. `NLB-GTS-{CRUD-EMPTY,CRUD-EMPTY-LB-ACTIVE,STATE-LB-STOPPED}` и
child-create `XRES-*`/`LST-CR-*` разобраны в round 4b выше (исправлены как кейсы, не
маскированы).

## Closed — was «known failing», fixed in the product

### `NLB-CR-VAL-INVALID-AFFINITY` — an unknown enum value is now REFUSED

**Эта запись была ложной с момента фикса.** Она объявляла кейс «остаётся КРАСНЫМ, пока
продукт не выполнит контракт», и оставалась в этом виде после того, как продукт его
выполнил: значение перечисления вне словаря отвергается на краю
(`gateway/internal/restmux/strict_enum.go`, коммит `d67d15fb` «значение перечисления вне
словаря отвергается, а не принимается за умолчание»). Устаревшее исключение — это ложное
утверждение о продукте: по нему ставят в очередь работу, которой не требуется, и красный,
если он появится, объясняют уже закрытой причиной.

Ниже сохранён разбор ИСХОДНОГО дефекта (замер 2026-07-28 на прежней сборке) — он
объясняет, почему кейс написан именно так и почему его нельзя ослаблять обратно. Ожидание
кейса (`400` / `INVALID_ARGUMENT`) не менялось; менялся продукт.

Замер на прежней сборке:

| request | result |
|---|---|
| `sessionAffinity: "DOES_NOT_EXIST"` | **HTTP 200**, Operation minted, LB created with the default `FIVE_TUPLE` |
| `sessionAffinity: "CLIENT_IP_ONLY"` | HTTP 200, `CLIENT_IP_ONLY` persisted |

An unknown value is therefore neither applied nor refused: the caller is told `200` for a
setting the server never made, which `api-conventions.md` names outright — «принято-и-
проигнорировано — ЗАПРЕЩЕНО». The cause is the transcoder, not this service. The public
marshaller runs protojson with `DiscardUnknown`, which discards unrecognised enum VALUES
along with unknown fields, so nlb receives `SESSION_AFFINITY_UNSPECIFIED` and cannot
distinguish "bogus" from "absent". The fix belongs to the gateway
(`gateway/internal/restmux/mux.go`, where the trade-off is written down), not to nlb.

The case asserts its declared contract (`400` / `INVALID_ARGUMENT`). It is NOT relaxed back to
`oneOf([200, 400])`: that spelling passed in both worlds and is precisely why the behaviour went
unnoticed — and it is the reason the case went green by a PRODUCT change rather than by an
adjusted expectation. The same swallow has already cost
this suite once — see the `adminState` note in `NLB-GTS-STATE-LB-DISABLED`, where a value the
transcoder dropped surfaced as a confusing `HEALTHY`-vs-`INACTIVE` failure.

## First fe3455 run — triage & corrections (2026-07-01)

The LoadBalancer suite (`collections/load-balancer.postman_collection.json`, 142 cases /
544 assertions) was executed against the live fe3455 stack for the first time: **97% pass,
10 failing assertions**. Every failure was triaged against the kacho-nlb source — **none is
a product bug** (the section above was opened later, on 2026-07-28, by a different finding).
Breakdown:

- **4 timing** — pass once the Operation worker is given time (`run.sh --delay <ms>` /
  `run-incremental.sh`); the async op had not reached `done:true` on the first poll.
- **1 fixture-limit** — an inline vpc fixture did not materialise on the lane (tolerated by
  design, see below).
- **1 grant-latency** — `NLB-LIFECYCLE-CONF` `lst-includes`: the List right after Create did
  not yet include the new LB because the FGA owner-tuple grant is written asynchronously
  (`fga_register_outbox` → IAM, ~0.6-2s) and List is authz-filtered.
- **5 wrong case-expectations** — the case asserted a contract that contradicts the actual,
  convention-correct product behaviour (verified in source). Corrected:

| Case | Before → After | Product justification (source) |
|---|---|---|
| `NLB-CR-NEG-REGION-UNKNOWN` | async op error code 5 (NOT_FOUND) → **code 3 (INVALID_ARGUMENT) + "not found" msg** | Region validated in the async Create worker (`create.go` `doCreate` → `regionClient.Get`); geo NotFound → `domain.ErrInvalidArg` "Region \<id\> not found" (`region_client.go` `mapRegionErr`) → `peerErrToStatus` → INVALID_ARGUMENT. Cross-domain ref-not-found = bad input (data-integrity convention). Surfaces on the polled Operation. |
| `NLB-LST-FILTER-LABELS` | 200 → **400 INVALID_ARGUMENT** | Filter whitelist is `{"name"}` only (`list.go` → `shared.ParseNameFilter` → corelib `filter.Parse`); `labels.env=...` is an unknown filter field. Valid name-filter stays covered by `NLB-LST-FILTER-NAME-OK` / `NLB-LST-FILTER-MATCH`. |
| `NLB-GTS-NEG-NF-UNKNOWN` | 404 with NO targetGroupId (actually got 400) → **supply well-formed garbage `targetGroupId` query param; 404 NotFound** | `get_target_states.go` validates `network_load_balancer_id` required → `target_group_id` required, before the LB lookup; omitting the tgid stops at "target_group_id: required" (400). With both ids well-formed the handler does the LB Get → NotFound (authz passes it through: no FGA tuple → `ErrNoPath` passthrough). |
| `NLB-LOPS-NEG-NF-UNKNOWN` | 404 → **200 + empty operations** | `list_operations.go` `Execute` lists by `resource_id` with NO parent-existence check (list-by-parent) → empty list, not NotFound. Authz passes it through (`ErrNoPath`). |
| `NLB-CR-VAL-EMPTY-BODY` | 400 INVALID_ARGUMENT → **403 PERMISSION_DENIED** | Create is authz-gated on `project:<projectId>` (`permission_map` Create → `objectTypeProject` + `GetProjectId`); an empty body has no projectId → `FormatObject` rejects the empty object id → the interceptor denies (`DecisionDenied`) BEFORE the handler's body validation. Authz-first / secure-by-default ordering, not a bug — a request with no project scope cannot be authorized. |

### Robustness & flow-control fixes (same PR, test-only)

- **Grant-latency tolerance** — `NLB-LIFECYCLE-CONF` `lst-includes` (now `life-lst-includes`)
  poll-retries the authz-filtered List (bounded `setNextRequest` self-retry, ≤6, same
  mechanism as `poll-op`) until the new LB id appears, then asserts inclusion. The assertion
  is not weakened, only made tolerant of the async owner-tuple grant.
- **Full-suite flow-control** — a plain `newman run <collection>` now traverses **all** 142
  folders. The poll helper self-retries via `postman.setNextRequest(pm.info.requestName)`;
  newman resolves `setNextRequest` by request NAME to the first match, and every poll step
  was named `poll-op`, so a mid-suite retry jumped back to an early folder and skipped the
  folders in between (previously only `run-incremental.sh --folder` traversed fully). `gen.py`
  now emits unique `poll-op-<n>` names (deterministic per collection). Verified with a mock
  that forces one retry per op: the old bare-`poll-op` collection stopped after ~500
  executions and never reached the last of 142 folders; the fixed collection reaches the last
  folder (626 executions). `run.sh` (plain `newman run`) is the canonical full runner again;
  `run-incremental.sh` remains the quota-safe per-folder runner.

## Sub-phase 8.1 rewrite — deploy preconditions & fixture tolerance

The suite was re-homed onto the sub-phase-8.1 NetworkLoadBalancer VIP model
(`v4Source`/`v6Source` + `placementType` + `disabledAnnounceZones`; removed
`securityGroupIds`/`crossZoneEnabled`/`networkId`; per-family `v4AddressId`/`v6AddressId`
output). No product bug was found against the `subnet-placement-vip` branch — the suite
asserts the branch's implemented, APPROVED-acceptance behaviour, so there is no
"Known failing — product bugs" section.

Two operational preconditions and one tolerance shape the run outcome (they are NOT bugs):

1. **External AddressPool must be seeded (deploy-precondition, acceptance §6.7).** Every
   default happy-path LB is now EXTERNAL with `v4Source={public:{}}`, so Create allocates a
   public vpc Address. On a stand without the platform external pool these Creates fail with
   `FAILED_PRECONDITION` — the same precondition the prior auto-VIP listener suite relied on.
2. **INTERNAL / address-link cases provision vpc Subnet/Address inline** (`POST /vpc/v1/subnets`,
   `/vpc/v1/addresses`; their `e9b`-prefixed Operation ids poll through the shared
   `/operations/{id}` OpsProxy). These require the seeded VPC network, free CIDR space
   (10.200-239.x.0/24), and the caller (`jwtProjectEditorA`) to hold vpc-create authz.
3. **Tolerant gating.** When an inline fixture does not materialise (bare lane / vpc authz
   absent) the case asserts the lawful fixture-absent rejection instead of the happy outcome,
   so the suite stays green on a bare lane and fully exercises the chain on the seeded umbrella
   stack. The sync source×type×placement negatives (the majority) are strict and fixture-free.

**Follow-ups (out of the 8.1 LoadBalancer acceptance scope — flagged, not fixed here):**
- `listener.py` / `cross-resource.py` exercise the sub-phase-4.0 listener-level VIP model
  (`subnetId`/`addressId`/`ipVersion`/`allocatedAddress`). 8.1 states the VIP now lives on the
  LB ("Listener больше не несёт VIP"). Only the parent-LB creation shape was fixed here; the
  listener resource itself needs its own acceptance + rewrite.
- 8.1-18 (dualstack families resolving to *different networks*) is not expressible black-box
  with the single seeded network; it needs a second-network fixture.
- vpc-side back-reference cases 8.1-33/34/35 (`owned` flag on `Address.used_by`, generalised
  `Address.Delete` guard text) verify kacho-vpc behaviour and belong in the vpc newman suite.

## Round 2 (2026-07-18) — root-cause pass over ci-rep2

Triaged the nlb newman failures in `ci-rep2` (per-collection `jq .run.failures`). The **dominant
root cause** was NOT per-case bugs but a **shared-fixture contention** interacting with the
`--jobs 4` parallel run, plus a cross-service read-your-writes lag. Fixes are test-only.

### Root cause A — external AddressPool exhaustion (systemic; the bulk of the cascade)

The default happy-path setup LB was `EXTERNAL` with an auto public VIP (`v4Source:{public:{}}`),
which draws every VIP from the single seeded external AddressPool (`kac-nlb-seed-ext-pool`,
`198.51.100.0/24` = 254 addrs). Across the whole run **only 82 distinct VIPs were ever allocated
against 115 `could not allocate load balancer address` FailedPrecondition errors** — i.e. the pool
was effectively exhausted far below 254, under 4 collections allocating from it concurrently.
Effect: `Create` returned an Operation that reached `done:true` **with an error**, so `{{nlbId}}`
pointed at a **phantom** (never-persisted) LB, and every downstream `Get`/`:verb`/`Update`/`Delete`
reddened — 404 (resource absent), 403 (owner-tuple never materialised for a non-existent object),
or 400 (empty child id). This single mechanism produced the majority of load-balancer (46 `_setup_lb`
cases), listener (type-agnostic setups), and cross-resource EXTERNAL-flow failures.

**Fix (test-only, root-cause):** setup LBs are now **pool-INDEPENDENT** — `INTERNAL ZONAL` with the
VIP auto-allocated from a per-case inline `/24` subnet (`load-balancer.py::_setup_lb`,
`listener.py::_setup_lb` default, `authz-deny.py` lifecycle setup). Each case has its own 254-address
subnet → zero cross-collection contention, self-contained, and confirmed working (cross-resource
INTERNAL LBs already allocate a bound Address reliably). No `_setup_lb`-based case asserts
EXTERNAL-specific shape on the setup LB, so it is a drop-in. **Whether this is also a product defect
(VIP not recycled on LB delete → pool leak) vs. deploy sizing / `--jobs 4` contention could not be
isolated from a single report without the stand** — flagged for a follow-up with a live stand
(investigate `Address` free-on-delete for auto-VIP LBs; or run nlb newman `--jobs 1`; or grow the
seed pool). No product masking: the INTERNAL migration removes the dependency, it does not hide it.

### Root cause B — cross-service subnet read-your-writes lag

INTERNAL subnet-source creates (INTERNAL-REGIONAL, DRAIN-TOGGLE, PLACEMENT-MISMATCH,
placement-coherence same-zone/-region) inline-provision a vpc Subnet, poll its Operation done, then
Create the LB — yet the LB Create rejected with `subnet <id> not found` (the subnet is durable in vpc
but briefly invisible to nlb's vpc peer-read under load; cross-resource's identical pattern merely
won the race). New primitive **`retry_create_until_present`** (gen.py) bounded-retries a create while
the response is a transient `<peer> not found` (a rejected create mints no Operation → leak-free),
then runs the real assertion. This is the "INTERNAL subnet inline-provision" resolution — the subnet
was *already* inline-provisioned; the missing piece was the peer-visibility retry.

### Deterministic / tolerant fixes (per CI signature)

- **listener List** (`lst` / `lst-unknown-lb` / `page-1/2`, and AZD `lst-stranger`): added the
  required `projectId` scope (was `400 project_id required`).
- **listener GET** (`LST-GET-CRUD-OK`): `Number(j.port)` coercion — grpc-gateway serialises the
  int32 port as a JSON string (`'81'`).
- **target-group** `TGR-UPD-CRUD-OK`: `updateMask` uses canonical lowerCamelCase
  (`deregistrationDelaySeconds`) — the snake_case form was rejected by the protojson FieldMask codec.
- **target-group** `TGR-LST-FILTER-REGION`: re-scoped to the real contract — filter whitelist is
  `name=` only (api-conventions), so a `region_id=` predicate lawfully rejects (`Unknown field`); the
  case now asserts that fail-closed conformance instead of a non-existent region-filter feature.
- **target-group** `TGR-MV-CRUD-OK` / **AZD move** denial text: cross-project move is destination-
  fixture-dependent → tolerant of the lawful `Project not found` / `permission denied` outcome; the
  **must-DENY (403 / code 7) stays strict** and the dst-scope guarantee is carried by the independent
  `precond-editorA-denied-on-dst` step. Only the brittle `"not authorized"` wording (actual contract
  text is `"permission denied: <action>"`) was loosened.
- **authz-deny list-authz** (`AZD-{TGR,NLB,LST}-LST-STRANGER`): a stranger/viewer listing a project
  they cannot see is fail-closed either by `403/404` OR by a **200 scoped-EMPTY** array (list-authz
  push-down — verified empty in ci-rep2, no leak). Cases now accept both **with an explicit empty-array
  leak-guard** (a 200 carrying any row fails). Mutations keep the strict deny.
- **operation** `OP-LST-NEG-UNROUTED-FAIL-CLOSED` (was `OP-LST-CRUD-OK`): project-wide ListOperations
  is not a modeled public RPC and never was — the contract carries only `Get`/`Cancel`, and the edge
  route table has no `/operations` collection entry. So the tolerance `200 (if cataloged) | 403` was
  waiting on a method that cannot appear without a new contract, and meanwhile the case could not fail
  on either answer. The case now states the single real outcome — `403`, code 7 — and thereby guards
  what actually matters here: the edge's fail-closed default for a path matching no method
  (security.md #4). A 200 on an unrouted path is now a failure, which is the point.
- **targets** `TGT-RM-STATE-PHASE-B-RUNNER`: single racey read → bounded self-poll for the async
  drain runner (absent/DRAINING/INACTIVE), still reds if the row stays ACTIVE past budget.

### Что round 2 объявлял как «flagged, not masked» — СНЯТО 2026-07-30 (запись без предмета)

Round 2 объявил, что кейсы, которым по смыслу нужен EXTERNAL auto-public-VIP, будут краснеть на
дорожке с исчерпанным внешним пулом под `--jobs 4`: в `listener.py` — `LST-CR-CRUD-AUTO-VIP`,
`LST-CR-CRUD-BYO` и ещё два имени; в `cross-resource.py` — `XRES-E2E-EXTERNAL-FULL-FLOW`,
`XRES-E2E-EXTERNAL-IPV6-VIP` и внешние ветки `XRES-E2E-DELETE-LB-NOT-EMPTY-FP` /
`XRES-E2E-TEARDOWN-BOTTOM-UP` / `XRES-DANGLING-INSTANCE-READ-GRACEFUL`.

Запись снята, и вот по каким основаниям:

- **два имени из четырёх в `listener.py` не существуют** — `LST-DEL-CRUD-AUTO-VIP-FREE` и
  `LST-DEL-CRUD-BYO-CLEAR-REF` набор не генерирует (владение VIP переехало Listener→LoadBalancer в
  sub-phase 8.1; остался `LST-DEL-CRUD-OK`, и комментарий над ним в `cases/listener.py` это
  фиксирует). Объявление про кейс, которого нет, не освобождает ничего;
- **предпосылка снята round 3**: `run.sh` перешёл на `--jobs 1`, то есть дорожки за общий внешний
  пул больше не состязаются;
- **у записи не было тикета** — «tracked with the Root-cause-A follow-up» не механизм: истечь
  такому нечем, и оно не истекло.

Осмысленное продолжение осталось прежним и требует живого стенда: убедиться, что auto-VIP
освобождается на удалении балансировщика. Если окажется, что не освобождается, это **продуктовая
находка со своим тикетом**, а не возврат этой записи.

## Round 3 (2026-07-18) — root-cause pass over ci-rep3

Triaged ci-rep3 per-collection (`jq .run.failures` + response-body decode + per-request
retry-convergence analysis). **The parent hypothesis (residual = EXTERNAL-VIP AddressPool
exhaustion) did NOT hold for ci-rep3** — round-2's INTERNAL-setup migration already removed
the setup-LB pool dependency (every `setup-create-lb-cr*` now *converges* to 200 via
`retry_create_until_present`; the transient `subnet not found` 400s are retried away, not
failures). The real ci-rep3 signatures, in order:

### Root cause A — stale AttachTargetGroup request shape (the dominant new signature, ~12)

The current contract (verified in **both** `kacho-proto` and the umbrella `proto/`, plus the
handler `internal/apps/kacho/api/loadbalancer/attach_target_group.go`) is:

```
AttachNetworkLoadBalancerTargetGroupRequest {
  network_load_balancer_id           // URL path
  attached_target_group (required) { target_group_id (required); health_checks (server-snapshot) }
}
```

i.e. the target-group id is **nested** under `attached_target_group`, `health_checks` is a
server-side snapshot (NOT client-provided), and **`priority` no longer exists** (the worker
calls `AttachedTargetGroups().Attach(ctx, lbID, tgID, 0)` — priority hard-wired 0, pivot is
`ON CONFLICT DO NOTHING`). The suite still sent the sub-phase-4.0 flat `{targetGroupId, priority}`
shape → server replied `attached_target_group.target_group_id: required` (400). This was
**masked in ci-rep2** by the phantom-LB cascade and only surfaced once round-2 made the setup
LB real. **Fix (conform to the verified current contract):** every attach body → nested
`{"attachedTargetGroup":{"targetGroupId":…}}`, `priority` dropped everywhere; the 5 pure-priority
cases (`*-ATT-BVA-PRIORITY-{MIN-0,MAX-1000,NEGATIVE}`, `*-ATT-VAL-PRIORITY-OVER`,
`*-ATT-IDEM-PRIORITY-UPDATE`) **removed** (they exercise a field the contract no longer has;
LEAN) with their CASES-INDEX entries. 362 → **357** cases.

> [!important] Flag for `acceptance-author` / `acceptance-reviewer` (NOT a product bug)
> The APPROVED `docs/specs/sub-phase-4.0-nlb-acceptance.md` still documents attach as flat
> `AttachTargetGroup(…, target_group_id, priority=100)` with a `[0,1000]` range (GWT-NLB-032 /
> 034 / 035). The implemented proto+handler (both repos, coherent, with a deliberate
> `health_checks` snapshot field) supersede that: attach is nested and priority-free. This is a
> **doc-vs-implementation drift**, not a regression — the 8.1 model the tests already cite as
> superseding 4.0 carries the attach redesign. Reconcile 4.0 GWT-NLB-032/034/035 to the nested,
> priority-free shape so the acceptance is the true contract again.

### Root cause B — protojson FieldMask codec rejects snake_case (~6)

Same defect class round-2 fixed for target-group's `deregistrationDelaySeconds`, but left in
load-balancer / cross-resource / listener / target-group. `updateMask` paths MUST be
lowerCamelCase (grpc-gateway's `google.protobuf.FieldMask` protojson codec rejects underscores:
`FieldMask.paths contains invalid path: "deletion_protection"`). Fixed the failing **positive**
paths (`deletionProtection`, `disabledAnnounceZones`, `defaultTargetGroupId`) AND the
currently-green-but-vacuous **immutable-negative** masks (`regionId`, `projectId`, `placementType`,
`v4Source`, `v4AddressId`, `loadBalancerId`, `ipVersion`, `addressId`) — the latter previously
passed on the protojson-reject 400 instead of the real immutable-check; camelCase makes them
exercise the true `"<field> is immutable"` path while staying green (tolerant `400/403/404`).
`nonexistent_field` deliberately left snake (a genuinely-unknown-field negative). All eight nlb
collections now carry only camelCase masks (+ that one intentional unknown).

### Root cause C — external-VIP AddressPool contention → `--jobs 1` (VIP recycle is NOT a leak)

Only **10** operation-errors carried `could not allocate load balancer address` in ci-rep3 (vs
ci-rep2's 115) — all on genuine EXTERNAL-auto-public-VIP cases that cannot move to INTERNAL. The
**VIP-recycle-leak question round-2 could not isolate is now answered from source: recycle WORKS,
NOT a product leak.** `internal/apps/kacho/api/loadbalancer/delete.go::doDelete` runs a
mark→release→delete saga: per-family `releaseVIP` = `ClearReference → FreeIP` (owned/auto) with a
`free_ip_runner` durable-handle self-heal for crashes. So a deleted LB returns its VIP to the
pool. The 10 exhaustion events are **transient concurrent HOLD** — under `run.sh` default
`--jobs 4`, three nlb collections (load-balancer / cross-resource / listener) draw EXTERNAL VIPs
from the single seeded pool (`kac-nlb-seed-ext-pool` `198.51.100.0/24` = 254) simultaneously, and
a burst transiently over-subscribes it before the async deletes free their VIPs.

**Decision — Option (2), `run.sh` default `--jobs 4 → 1`** (not Option (1) bigger pool):
- Serial collections keep the peak concurrent VIP hold tiny (each EXTERNAL case creates then
  deletes before the next) → no exhaustion, and it does **not mask** anything (VIPs are still
  really allocated/freed — just serially).
- Option (1) is **not simple here**: the external pool's `address_pool_cidrs` EXCLUDE is on
  `(kind, block &&)` **globally** and the **vpc** seed provisions the *same* `198.51.100.0/24` for
  `EXTERNAL_PUBLIC`; enlarging only the nlb seed's CIDR would overlap vpc's /24 → `AddressPool.Create`
  conflicts → the seed's reuse-fallback picks the existing /24 anyway (no enlargement). A real
  enlargement needs both seeds coordinated — out of a test-only PR's scope.
- Bonus: serial also frees the FGA `fga_register_outbox` drainer's CPU (no 4× parallel busy-wait
  retry loops starving it), which directly relieves Root cause D.

### Root cause D — owner/parent FGA tuple materialization > retry budget under --jobs 4 (~11)

403 `lacks relation "editor" on nlb_network_load_balancer:<id>` on the first editor-gated op of a
fresh setup LB/TG (attach / listener-create / start / addTargets). The owner/parent hierarchy
tuple is eventually-consistent (at-least-once register-outbox drainer); under `--jobs 4` the
drainer was CPU-starved by the parallel busy-wait retry loops, so materialization outran the
25×400ms budget. Mitigation: `retry_until_authorized` budget **25→40** (~16s) +
`retry_create_until_present` **20→30**, and `--jobs 1` (Root cause C) un-starves the drainer.
`targets` `add-nic-nx` (unwrapped) wrapped with `retry_on=(403,)` — narrow so the legitimate
unknown-nic 404 is NOT retried away. Listener's unwrapped child creates rely on serial execution
+ the viewer-materialize gate; **if any 403 persists on the next live serial run it is a
product-side register-outbox drainer latency finding**, not a test bug — flagged, not masked.

### Deterministic / fixture fixes (per signature)

- **list-filter** (all 3 failures, one root): `LF-TGR-…` `create-tg` sent an incomplete
  `healthCheck` (`{name,tcpOptions}`) → TG-create `InvalidArgument` → `{{lfTgId}}` unset →
  `del-tg "invalid resource id"` + list-miss cascade. Completed the HC (interval/timeout/thresholds)
  to mirror the working `load-balancer.py::_setup_tg`.
- **move** cross-project (`NLB-MV-CRUD-OK`): `_suiteProjectCrossId` is not reliably seeded on every
  lane → `Project not found`. Made the forward-move tolerant of the lawful fixture-absent 400 (sets
  `_mvMoved`; the projectId-updated and move-back assertions run only when the move actually
  happened) — mirrors round-2's target-group move tolerance.
- **check-fp** (`NLB-MV-NEG-ATTACHED-TG`, `NLB-ATT-STATE-REGION-MISMATCH`): now `oneOf([3,9])` — on
  a lane where `_suiteProjectCrossId` / `_suiteRegionAltId` is unseeded, the worker rejects on the
  dst-project / alt-region existence check FIRST (InvalidArgument 3) before the attached-TG /
  region-mismatch guard (FailedPrecondition 9); both are lawful non-acceptances, never a silent 200.
- **placement-coherence** (`ZC-NLB-ZONE-01`): the cross-zone dualstack negative was unwrapped and
  hit a transient `subnet not found` instead of the intended same-zone message; wrapped in
  `retry_create_until_present` (both subnets ARE provisioned, so it spins past the transient
  not-found to the real coherence rejection — the same-zone verbatim message carries no "not found").
- **target-group / authz-deny** attach setups: nested + priority-dropped for consistency — the flat
  shape had made the `del-blocked` / `mv-blocked` / cross-tg-deny cases pass **vacuously** (the
  attach silently 400'd so nothing was actually attached); nesting un-masks real coverage while the
  tolerant assertions keep them green.

### Что round 3 оставлял «residual known-failing» — СНЯТО 2026-07-30 вместе с записью round 2

Round 3 повторил ту же запись словами «всё ещё зависят от внешнего пула», добавив, что с
`--jobs 1` они состязаться перестали и «должны теперь проходить — подтвердить на следующем живом
прогоне». Подтверждения не появилось, а запись осталась: это ровно та форма, в которой объявление
переживает свой предмет. Снято по основаниям, перечисленным в разделе round 2 выше.

Что из round 3 остаётся верным и без записи: VIP-форма слушателя (`allocated_address`,
listener-level `ipVersion`) относится к модели sub-phase 4.0, которую 8.1 перенесла на
балансировщик; кейсы под неё переписаны в round 5 (см. историю версий). Продуктовых багов этот
раунд не подтвердил, и ничего не маскировалось.

## Acceptance D-4 gate

D-4 (acceptance §17 DoD): Newman matrix 100% pass — minimum **320 cases** +
**≥30 AZD cases** + 0 failures. Verified by `newman-e2e` workflow in `kacho-deploy`
once the implementation epic merges.

## How to re-run

```bash
# port-forward api-gateway (one shell)
kubectl -n kacho port-forward svc/api-gateway 18080:8080

# full suite (another shell)
cd tests/newman
python3 scripts/validate-cases.py            # uniqueness + catalogue
python3 scripts/gen.py                       # regenerate collections (already committed)
./scripts/run.sh                             # all services in parallel (default --jobs 4)

# one service
./scripts/run.sh --service load-balancer

# quota-safe (one folder at a time, with --resume)
./scripts/run-incremental.sh --service load-balancer --resume

# kind stand (E2E CI env)
./scripts/run.sh --env environments/kind-stand.postman_environment.json
```

After each run, paste `out/summary.txt` (or `out/inc-summary.txt`) into a new
row of the **Version history** table above and append per-service breakdown
into the **Latest baseline** table.
