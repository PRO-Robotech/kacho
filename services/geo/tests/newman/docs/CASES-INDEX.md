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
| `GEO-ZON-GT-CONF-OK` | CONF/CRUD | P0 | GET /geo/v1/zones → 200, zones[] non-empty + well-formed (id+status), not 503/code14 (migrated from iam) |
| `ZON-GET-CRUD-OK` | CRUD/CONF | P0 | Get zone (resolved from List) → 200, flat shape {id,regionId,name,status,createdAt}, createdAt truncated |
| `ZON-GET-CONF-NO-INFRA` | CONF | P1 | public Zone carries NO infra fields (two-projection invariant; status present AS-IS) |
| `ZON-GET-VAL-MALFORMED` | VAL/NEG | P1 | malformed (non-slug) id → 400 INVALID_ARGUMENT |
| `ZON-GET-NEG-NOTFOUND` | NEG/CONF | P1 | well-formed-absent id → 404 verbatim "Zone <id> not found" |
| `ZON-LST-BVA-PAGESIZE-ZERO` | BVA/PAGE | P2 | pageSize=0 → 200 |
| `ZON-LST-BVA-PAGESIZE-OVER-MAX` | BVA/VAL | P1 | pageSize=10000 → 400 INVALID_ARGUMENT |
| `ZON-LST-PAGE-TOKEN-GARBAGE` | PAGE/VAL | P1 | garbage page_token → 400 INVALID_ARGUMENT |
| `ZON-LST-PAGE-ROUNDTRIP` | PAGE/CRUD | P2 | pagination round-trip |

## OperationService (op-poll) — `operation.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GOP-GET-VAL-MALFORMED` | VAL/NEG | P1 | malformed op-id → 400 "invalid operation id" (OpsProxy validation, GREEN) |
| `GEO-IOP-POLL-ROUTING-OK` | STATE/NEG | P0 | **RED #55** — geo Operation must be pollable to done (gateway OpsProxy 'geo' prefix routing) |
| `GEO-IOP-GET-AUTHZ-BOLA` | AUTHZ/NEG | P1 | **RED #55** — foreign principal polling another's geo op → PermissionDenied (owner-scoping) |

## Admin-not-on-public (ban #6 guard) — `admin-not-on-public.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `ANP-REG-CR-NOT-PUBLIC` | NEG/AUTHZ | P0 | InternalRegion.Create rejected on public endpoint, accepted on internal |
| `ANP-ZON-CR-NOT-PUBLIC` | NEG/AUTHZ | P0 | InternalZone.Create rejected on public endpoint, accepted on internal |

## Authz matrix — `authz-deny.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `GEO-REG-GT-AUTHZ-ANON-DENY` | AUTHZ/NEG | P1 | anonymous GET /geo/v1/regions → 401 UNAUTHENTICATED (migrated from iam) |
| `GEO-ZON-GT-AUTHZ-ANON-DENY` | AUTHZ/NEG | P1 | anonymous GET /geo/v1/zones → 401 UNAUTHENTICATED (migrated from iam) |
| `GEO-REG-GT-AUTHZ-NOVIEWER-DENY` | AUTHZ/NEG | P1 | authenticated-no-viewer GET → 403 PERMISSION_DENIED (AS-IS viewer-gate) |
| `GEO-REG-CR-AUTHZ-NONADMIN-DENY` | AUTHZ/NEG | P0 | non-admin InternalRegion.Create → 403 PERMISSION_DENIED (system_admin required) |
| `GEO-ZON-CR-AUTHZ-NONADMIN-DENY` | AUTHZ/NEG | P0 | non-admin InternalZone.Create → 403 PERMISSION_DENIED |

---

## Admin CRUD (заметка) — `internal-region.py` / `internal-zone.py`

Case-файлы `internal-*.py` каталогизируются этой заметкой (а не таблицей паттернов —
`validate-cases.py` их от индекс-покрытия освобождает; dup-id-проверка работает).
Все они бьют в cluster-internal REST listener (`{{internalBaseUrl}}`) как `system_admin`.
Async op-poll заблокирован #55 → happy-path подтверждается публичным Get; async
op.error асертится по observable side-effect.

**`internal-region.py`** (InternalRegionService): `IRG-CR-CRUD-OK` (create→public-Get),
`IRG-CR-VAL-MALFORMED-ID` (sync 400), `IRG-CR-VAL-EMPTY-ID` (sync 400 "region id is
required"), `IRG-CR-NEG-DUP-INVARIANT` (no-phantom; ALREADY_EXISTS deferred #55),
`IRG-DEL-NEG-HASZONES-INVARIANT` (FK RESTRICT keeps region; FAILED_PRECONDITION
deferred #55), `IRG-UPD-CRUD-OK` (name update→public-Get).

**`internal-zone.py`** (InternalZoneService): `IZN-CR-CRUD-OK` (create→public-Get),
`IZN-CR-VAL-MALFORMED-ID` (sync 400), `IZN-CR-NEG-GHOST-REGION-INVARIANT` (FK reject;
zone not created; FAILED_PRECONDITION deferred #55), `IZN-CR-CONF-STATUS-DOWN`
(explicit DOWN persists), `IZN-CR-CONF-STATUS-DEFAULT-UP` (omitted→UP, AS-IS default),
`IZN-UPD-CRUD-STATUS` (status UP→DOWN).

---

## Что НЕ покрыто (и почему) — GEO-1 redesign surface (не существует)

`docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md` — APPROVED **дизайн**,
но merge-gated (Phase-0 governance) и **НЕ приземлён**. Его поверхность
(`GetInternal`, `/geo/v1/internal/…`, `openForPlacement°`, `placementBlockedReason°`,
`countryCode°`, `infra°`/`numericInfraId°`, `warnings°`, update_mask discipline,
zone.id↔regionId coupling, immutable regionId, fresh-DOWN default, `UNIQUE(name)`,
ambient-exempt read, `?regionId`/`?openForPlacement` list filters) **отсутствует** в
проде — кейсы на неё НЕ писались (ban «не тестировать несуществующие RPC/поля»). Этот
suite лочит AS-IS-контракт; при приземлении GEO-1 (rpc-implementer) кейсы обновляются.
Детали — `docs/RESULTS.md`.
