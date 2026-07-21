# kacho-geo — CASES-INDEX

Единый каталог уникальных проверок geo newman-suite. Source-of-truth — модули
`cases/*.py`; этот файл — производный каталог (гейт `validate-cases.py`: каждый
case-id обязан быть здесь литерально ИЛИ покрыт суффикс-паттерном `*-<SUFFIX>`,
ЛИБО помечен `# index:` в case-файле, ЛИБО жить в `internal-*.py`).

Таксономия case-id — `docs/TAXONOMY.md`. Prefixes: `REG`/`ZON` (public read),
`IRG`/`IZN` (internal admin CRUD — каталогизированы заметкой ниже), `GOP`/`GEO-IOP`
(OperationService), `ANP` (admin-not-on-public), `GEO-REG`/`GEO-ZON` (authz + migrated).

Прогон-статус (GREEN / RED-known-failing) — `docs/RESULTS.md`.

---

## Public RegionService (Get/List) — `region.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GEO-REG-GT-CONF-OK` | CONF/CRUD | P0 | GET /geo/v1/regions → 200, regions[] non-empty + well-formed, not 503/code14 (migrated from iam) |
| `REG-GET-CRUD-OK` | CRUD/CONF | P0 | Get region (resolved from List) → 200, flat shape {id,name,createdAt}, createdAt truncate-to-seconds |
| `REG-GET-CONF-NO-INFRA` | CONF | P1 | public Region carries NO infra fields (two-projection invariant) |
| `REG-GET-VAL-MALFORMED` | VAL/NEG | P1 | malformed (non-slug) id → 400 INVALID_ARGUMENT (first statement) |
| `REG-GET-VAL-ID-TOOLONG` | VAL/BVA/NEG | P2 | over-length id (64 chars) → 400 INVALID_ARGUMENT |
| `REG-GET-NEG-NOTFOUND` | NEG/CONF | P1 | well-formed-absent id → 404 verbatim "Region <id> not found" |
| `REG-LST-BVA-PAGESIZE-ZERO` | BVA/PAGE | P2 | pageSize=0 → 200 (default applied) |
| `REG-LST-BVA-PAGESIZE-OVER-MAX` | BVA/VAL | P1 | pageSize=10000 (>1000) → 400 INVALID_ARGUMENT (rejected, not clamped) |
| `REG-LST-PAGE-TOKEN-GARBAGE` | PAGE/VAL | P1 | garbage page_token → 400 INVALID_ARGUMENT |
| `REG-LST-BVA-PAGESIZE-ONE` | BVA/PAGE | P2 | pageSize=1 → ≤1 item |
| `REG-LST-PAGE-ROUNDTRIP` | PAGE/CRUD | P2 | pageSize=1 → nextPageToken → next page 200 |

## Public ZoneService (Get/List) — `zone.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GEO-ZON-GT-CONF-OK` | CONF/CRUD | P0 | GET /geo/v1/zones → 200, zones[] non-empty + well-formed (id+openForPlacement; NO raw status), not 503/code14 (migrated from iam) |
| `ZON-GET-CRUD-OK` | CRUD/CONF | P0 | Get zone (resolved from List) → 200, flat shape {id,regionId,name,openForPlacement°,placementBlockedReason°,createdAt}, createdAt truncated |
| `ZON-GET-CONF-NO-INFRA` | CONF | P1 | public Zone carries NO infra fields (two-projection invariant; NO raw status) |
| `ZON-GET-VAL-MALFORMED` | VAL/NEG | P1 | malformed (non-slug) id → 400 INVALID_ARGUMENT |
| `ZON-GET-NEG-NOTFOUND` | NEG/CONF | P1 | well-formed-absent id → 404 verbatim "Zone <id> not found" |
| `ZON-LST-BVA-PAGESIZE-ZERO` | BVA/PAGE | P2 | pageSize=0 → 200 |
| `ZON-LST-BVA-PAGESIZE-OVER-MAX` | BVA/VAL | P1 | pageSize=10000 → 400 INVALID_ARGUMENT |
| `ZON-LST-PAGE-TOKEN-GARBAGE` | PAGE/VAL | P1 | garbage page_token → 400 INVALID_ARGUMENT |
| `ZON-LST-PAGE-ROUNDTRIP` | PAGE/CRUD | P2 | pagination round-trip |

## OperationService (envelope) — `operation.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GOP-GET-VAL-MALFORMED` | VAL/NEG | P1 | malformed op-id → 400 "invalid operation id" (OpsProxy validation) |
| `GEO-IOP-SYNC-DONE-OK` | STATE/CRUD | P0 | geo admin mutation → Operation{done:true} synchronous (metadata.regionId + response=public Region); unwrap .response (GEO-1-16) |

## Admin-not-on-public (ban #6 guard) — `admin-not-on-public.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `ANP-REG-CR-NOT-PUBLIC` | NEG/AUTHZ | P0 | InternalRegion.Create rejected on public endpoint, accepted on internal |
| `ANP-ZON-CR-NOT-PUBLIC` | NEG/AUTHZ | P0 | InternalZone.Create rejected on public endpoint, accepted on internal |

## Authz matrix — `authz-deny.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GEO-REG-GT-AUTHZ-ANON-DENY` | AUTHZ/NEG | P1 | anonymous GET /geo/v1/regions → 401 UNAUTHENTICATED (authN required; GEO-1-21) |
| `GEO-ZON-GT-AUTHZ-ANON-DENY` | AUTHZ/NEG | P1 | anonymous GET /geo/v1/zones → 401 UNAUTHENTICATED (GEO-1-21) |
| `GEO-REG-GT-AUTHZ-AMBIENT-OK` | AUTHZ/CONF | P1 | authenticated zero-binding GET /geo/v1/regions → 200 (ambient read; project-scope EXEMPT, GEO-1-20) |
| `GEO-REG-CR-AUTHZ-NONADMIN-DENY` | AUTHZ/NEG | P0 | non-admin InternalRegion.Create (/geo/v1/internal/regions) → 403 PERMISSION_DENIED (system_admin required; GEO-1-22) |
| `GEO-ZON-CR-AUTHZ-NONADMIN-DENY` | AUTHZ/NEG | P0 | non-admin InternalZone.Create (/geo/v1/internal/zones) → 403 PERMISSION_DENIED |

---

## Admin CRUD (заметка) — `internal-region.py` / `internal-zone.py`

Case-файлы `internal-*.py` каталогизируются этой заметкой (а не таблицей паттернов —
`validate-cases.py` их от индекс-покрытия освобождает; dup-id-проверка работает).
Все они бьют в cluster-internal REST listener (`{{internalBaseUrl}}`) под сегментом
`/geo/v1/internal/…` как `system_admin`. Синхронно-завершённая Operation
(`done:true`, GEO-1 F5); happy-path материализацию подтверждаем публичным Get;
DB-detected op.error асертится по observable side-effect (public read).

**`internal-region.py`** (InternalRegionService, `/geo/v1/internal/regions`):
`IRG-CR-CRUD-OK` (create→public-Get, openForPlacement), `IRG-CR-VAL-MALFORMED-ID`
(sync 400 "invalid region id"), `IRG-CR-VAL-EMPTY-ID` (sync 400 "invalid region id
''"), `IRG-CR-NEG-DUP-INVARIANT` (no-phantom; dup→Operation.error ALREADY_EXISTS),
`IRG-DEL-NEG-HASZONES-INVARIANT` (FK RESTRICT keeps region; Operation.error
FAILED_PRECONDITION, GEO-1-18), `IRG-UPD-CRUD-OK` (name update→public-Get).

**`internal-zone.py`** (InternalZoneService, `/geo/v1/internal/zones`):
`IZN-CR-CRUD-OK` (create→public-Get, openForPlacement=true GEO-1-06),
`IZN-CR-VAL-MALFORMED-ID` (sync 400), `IZN-CR-NEG-COUPLING` (sync 400 coupling
GEO-1-29), `IZN-CR-NEG-GHOST-REGION-INVARIANT` (coupling-valid+absent region → FK
reject; zone not created), `IZN-CR-CONF-STATUS-DOWN` (zone DOWN → openForPlacement
false/ZONE_DOWN GEO-1-08), `IZN-CR-CONF-STATUS-DEFAULT-DOWN` (omitted status →
fail-safe DOWN GEO-1-12), `IZN-UPD-CRUD-STATUS` (status UP→DOWN flips openForPlacement).

---

## GEO-1 landed — suite приведён к GEO-1-контракту

`docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md` (APPROVED) **приземлён**
на `redesign/integration` (proto/handler/domain/gateway = GEO-1): two-projection
(`status`/`infra°` только Internal), derived `openForPlacement°`+`placementBlockedReason°`,
`/geo/v1/internal/…` admin-сегмент, синхронный `Operation{done:true}`, ambient public-read
(project-scope EXEMPT), zone.id↔regionId coupling, fresh-DOWN default, `countryCode°`.
Этот suite приведён к landed-контракту (был написан на устаревшей AS-IS-премиссе, что
GEO-1 «не приземлён» — премисса опровергнута ground-truth: public-read отдаёт GEO-1-shape,
admin CRUD живёт на `/geo/v1/internal/…`).

Расширяемая (net-new) поверхность GEO-1, ещё НЕ покрытая отдельными кейсами (следующий
инкремент — integration-tester/qa): `GetInternal` full projection (status+infra°),
`warnings°` в CreateRegionMetadata (fresh-DOWN loud no-op), `?regionId`/`?openForPlacement`
list-фильтры (GEO-1-24/26), immutable regionId reject (GEO-1-32), `countryCode` формат
(GEO-1-39), `UNIQUE(name)` dup (GEO-1-36 op.error), coupling strict-startsWith counter
`ru-central10-a` (GEO-1-30). Детали статуса прогона — `docs/RESULTS.md`.
