# Test-Plan — kacho-geo newman (public read surface)

## Scope

Black-box regression через api-gateway public REST ({{baseUrl}}) для публичной
read-поверхности каталога оси размещения:

- `RegionService.Get`  — `GET /geo/v1/regions/{id}`
- `RegionService.List` — `GET /geo/v1/regions`
- `ZoneService.Get`    — `GET /geo/v1/zones/{id}`
- `ZoneService.List`   — `GET /geo/v1/zones`

Плюс чёрно-ящичный guard Internal-vs-external split (admin write-verb на публичном
endpoint не мутирует).

Источник истины — APPROVED `docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md`
+ `.claude/rules/api-conventions.md`. 22 кейса (`cases/region.py` × 11, `cases/zone.py` × 11).

## Deployed-contract probe (важно — читать перед правкой ассертов)

Suite авторена против **фактически задеплоенного** контракта ветки
`redesign/integration @ 8f3dca1` (это то, что гоняет CI), сверенного с proto/gateway
кодом в дереве (локальный прогон newman env-blocked — авторинг + syntax-validate, исполнение в CI):

| Аспект | Deployed AS-IS (8f3dca1) | GEO-1 target (redesign) |
|---|---|---|
| public `Region` | `{id, name, createdAt}` | `+ countryCode°, openForPlacement°, openZoneCountHint°` |
| public `Zone` | `{id, regionId, status, name, createdAt}` | `status`/`infra°` → **Internal-only**; `+ openForPlacement°, placementBlockedReason°` |
| public read authz | `required_relation=viewer`, `scope=cluster`, `acr_min=2` | **EXEMPT** (снят relation+extractor, authN-only) |
| admin-CRUD REST | `POST/PATCH/DELETE /geo/v1/{regions,zones}` (InternalRegion/ZoneService, internal-mux) | `/geo/v1/internal/…` |
| malformed-id текст | `"<field> must be a lowercase slug (…)"` | `"invalid <res> id '<X>'"` |
| absent read | `NOT_FOUND "Region\|Zone <id> not found"` | без изменений (ungated/landed) |

**Стратегия ассертов — forward-compatible:** там, где контракт стабилен через границу
редизайна (grpc-код malformed/notfound/pagesize/authN, verbatim not-found текст,
field-absence инфра/host-class/placement), ассертим точно. Там, где контракт mid-flight
(exact malformed текст, проекция `status`), ассертим толерантно/не ассертим. Итог: suite
зелёная и на текущем CI-стеке, и после приземления GEO-1 (кроме Deferred ниже, которые
добавит GEO-1 PR).

## Traceability — покрытые GEO-1-сценарии (ungated / уже-landed части)

| GEO-1 | Кейсы |
|---|---|
| GEO-1-21 (unauthenticated → UNAUTHENTICATED) | `REG-LST-AUTHZ-ANON-DENY`, `ZON-LST-AUTHZ-ANON-DENY` |
| GEO-1-24/25 (List item = полная public-проекция) | `ZON-LST-CRUD-OK`, `REG-LST-CRUD-OK` |
| GEO-1-27 (pagination-validate: pageSize>max / garbage token → INVALID_ARGUMENT) | `*-LST-BVA-PAGESIZE-OVER-MAX`, `*-LST-PAGE-BADTOKEN` |
| GEO-1-31 (malformed slug → INVALID_ARGUMENT первым стейтментом; код) | `*-GET-VAL-MALFORMED` |
| GEO-1-35 (geo-direct absent → NOT_FOUND "…not found"; ungated) | `*-GET-NEG-NOTFOUND` |
| GEO-1-05 (host-class физически не на public) + GEO-1-33 (нет placementType/scope) | `*-GET-CONF-NO-INFRA` |
| GEO-1-17/22 (Internal admin-CRUD не на external; system_admin gate) — black-box | `*-CR-AUTHZ-ADMIN-NOT-PUBLIC` |

## Deferred — GEO-1 redesign (добавляется ЭТОЙ suite вместе с GEO-1 PR по DoD)

Сценарии, чьё целевое поведение ещё НЕ приземлено в `redesign/integration @ 8f3dca1`.
Не авторены сейчас как зелёные (упали бы на текущем стеке; локальный RED-verify недоступен —
env-blocked). Каждый — прямая единица работы GEO-1-PR (DoD §newman-кейс `# verifies GEO-1-NN`):

| GEO-1 | Что добавить | Блокер |
|---|---|---|
| GEO-1-20 | zero-binding аутентиф. tenant читает Region/Zone → **200** (EXEMPT ambient) | authz сейчас `viewer`@cluster (не EXEMPT) → zero-binding = 403; ждёт снятия relation+scope_extractor у 4 read-RPC + permission-catalog regen |
| GEO-1-02 | public `Zone.Get` НЕ содержит `status` (field-absence) | `status` сейчас на public `Zone`-message; ждёт two-projection (отдельный `InternalZone`-message, breaking proto) |
| GEO-1-03 | public `Region.Get` содержит `countryCode°`, НЕ содержит `infra°` | `countryCode°` ещё нет на `Region` |
| GEO-1-06..09 | `openForPlacement°` формула во всех 4 состояниях zone×region | `openForPlacement°`/`placementBlockedReason°` ещё нет на public |
| GEO-1-16..19 | admin Create/Delete через `/geo/v1/internal/…` (done:true, unwrap .response, delete-non-empty) | internal REST на `/geo/v1/regions` (verb-based), не `/geo/v1/internal/…`; env стенда без `internalBaseUrl` проброса |
| GEO-1-31 (текст) | malformed текст `"invalid <res> id '<X>'"` (точный) | сейчас `"… must be a lowercase slug …"`; наш ассерт толерантен к обоим (флип не сломает) |
| GEO-1-34/36/37 | admin Create absent-parent → NOT_FOUND+reason; dup-name → ALREADY_EXISTS; INTERNAL opaque | требует internal-mux доступа; часть `[PHASE-0-GATED]` (by-lane reason-token) |

## Прогон

```
python3 scripts/gen.py            # регенерация коллекций
python3 scripts/validate-cases.py # dup-id + CASES-INDEX (hard-fail в CI до newman)
./scripts/run.sh --service region # одна коллекция
./scripts/run.sh                  # полный прогон (region + zone)
```

Локальный прогон в этом окружении заблокирован (харнесс убивает port-forward);
исполнение — CI-раннер против задеплоенного `kacho-deploy`-стека.
