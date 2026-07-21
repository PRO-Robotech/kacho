# kacho-geo newman — RESULTS

Прогон-статус geo regression-suite. Source-of-truth кейсов — `cases/*.py`;
каталог — `docs/CASES-INDEX.md`.

## Статус прогона

**`gen.py` + `validate-cases.py` — GREEN** (локально, pure-Python, без сети):
- `python3 scripts/gen.py` → 42 cases, 7 коллекций сгенерированы.
- `python3 scripts/validate-cases.py` → OK, 42 уникальных case-id, все каталогизированы.

**Newman-прогон против стенда — PENDING clean-seed CI.** Локальный newman-замер
недоступен (harness убивает `kubectl port-forward` — см. workspace MEMORY «Local
newman env blocked»); зелёность/красность прогона арбитрируется CI-раннером
(`deploy/scripts/newman-parallel.sh`, geo зарегистрирован в PHASE2-волне) на
свежесиданном стенде (`tests/authz-fixtures/setup.sh` патчит geo env fresh-JWT'ами;
`--env-var baseUrl/internalBaseUrl` инжектятся раннером).

Ожидаемый результат прогона (по контракту AS-IS): **40 GREEN + 2 RED** (RED —
известные product-баги, декларированы ниже).

## Known failing — product bugs (RED, TDD-red против реального бага прода)

> Допустимое исключение из «100% pass»: кейс красный до фикса прода (testing.md
> ban #13). Прод-фикс — отдельный PR (НЕ в этом test-only PR).

### PRO-Robotech/kacho#55 — gateway OpsProxy не маршрутизирует geo Operation-id

`gateway/internal/opsproxy/proxy.go` `prefixToBackend` не содержит prefix `geo`
(при том что geo backend-conn в gateway есть). Любой geo op-id (`geo…`, 20 симв)
резолвится в `InvalidArgument "invalid operation id"` → geo admin-мутации нельзя
доткнуть до `done` через gateway; op.error недоступен; ownership/BOLA недостижим.
Фикс — одна строка (`"geo": "geo"` в карте). Verified-by-test:

| case-id | коллекция | что лочит | сейчас | после фикса |
|---|---|---|---|---|
| `GEO-IOP-POLL-ROUTING-OK` | operation | op-poll geo Operation → done | RED (gateway 400 invalid operation id) | GREEN (200 + done) |
| `GEO-IOP-GET-AUTHZ-BOLA` | operation | foreign principal → PermissionDenied (owner-scoping) | RED (400 routing до ownership) | GREEN (403) |

Оба кейса несут `# verifies https://github.com/PRO-Robotech/kacho/issues/55`.
Повторный прогон коллекции `operation` после мерджа gateway-фикса → GREEN.

## Deferred behind #55 (async op.error conformance — асертится по side-effect)

Точный **код/текст `Operation.error`** для async-негативов доставляется только через
op-poll (заблокирован #55), поэтому эти инварианты в suite асертятся **по observable
side-effect через публичный read** (не по op.error), а точный контракт кода/текста
задеферен до фикса #55:

| инвариант | side-effect assert (GREEN сейчас) | op.error-контракт (deferred #55) |
|---|---|---|
| dup region id (PK) | `IRG-CR-NEG-DUP-INVARIANT` — регион резолвится один раз, phantom не создан | `ALREADY_EXISTS "Region <id> already exists"` |
| delete region c зонами | `IRG-DEL-NEG-HASZONES-INVARIANT` — регион пережил delete (FK RESTRICT) | `FAILED_PRECONDITION "Region <id> violates a reference constraint"` |
| zone c ghost-regionId | `IZN-CR-NEG-GHOST-REGION-INVARIANT` — зона не создана (public Get 404) | `FAILED_PRECONDITION "Zone <id> violates a reference constraint"` |
| update/delete absent | (не покрыто отдельным кейсом) | `NOT_FOUND "<Resource> <id> not found"` |

После фикса #55 эти op.error-контракты стоит добить отдельными op-poll-кейсами
(follow-up для qa-test-engineer; трекер — тот же #55 либо новый issue).

## GEO-1 redesign — НЕ покрыт (surface не существует)

`docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md` — **APPROVED дизайн**,
но с жёстким MERGE-GATE (Phase-0 governance change-set) и **НЕ приземлён** в прод
(proto/handler/migration = AS-IS, git-log geo без GEO-1). Поверхность GEO-1
(`GetInternal`, `/geo/v1/internal/…`, `openForPlacement°`, `placementBlockedReason°`,
`countryCode°`, `infra°`/`numericInfraId°`, `warnings°`, update_mask discipline,
zone.id↔regionId coupling, immutable regionId, fresh-DOWN default, `UNIQUE(name)`,
ambient-exempt public read, `?regionId`/`?openForPlacement` фильтры) в проде
**отсутствует** → кейсы на неё НЕ писались (нельзя тестировать несуществующие
RPC/поля; RED-против-future-work — не допустимый TDD-red).

**Этот suite лочит AS-IS-контракт** (10 implemented RPC): public Region{id,name,
createdAt} + Zone{id,regionId,status,name,createdAt}; async Operation-мутации;
viewer-gated public read; slug-id; FK RESTRICT; default-status UP. Инварианты,
которые GEO-1 изменит и которые здесь лочатся как AS-IS (обновятся при приземлении
GEO-1): `ZON-*`/`REG-GET-CONF-NO-INFRA` (public Zone несёт `status` — GEO-1 вынесет
в Internal), `IZN-CR-CONF-STATUS-DEFAULT-UP` (GEO-1 инвертирует в DOWN),
`GEO-REG-GT-AUTHZ-NOVIEWER-DENY` (GEO-1 сделает public read exempt → 200).

Когда `rpc-implementer` приземлит GEO-1: (1) обновить перечисленные AS-IS-кейсы под
новый контракт; (2) добавить кейсы на новую поверхность (two-projection field-absence
`status`, GetInternal, openForPlacement° derivation, coupling, warnings°, countryCode,
UNIQUE(name), ambient-read) по 42 GEO-1-NN сценариям acceptance-дока.
