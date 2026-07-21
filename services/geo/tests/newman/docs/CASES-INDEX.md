# newman — индекс уникальных кейсов (kacho-geo)

22 кейса / 2 файла (`cases/region.py`, `cases/zone.py`). Каждый case-id обязан
пройти `scripts/validate-cases.py` (дубль case-id и не-каталогизированный кейс →
hard-fail в CI до newman). Новый кейс → добавь его литеральный id (или суффикс-паттерн
`*-<SUFFIX>`) сюда.

> Источник истины сценариев — APPROVED acceptance
> `docs/specs/sub-phase-GEO-1-region-zone-redesign-acceptance.md` (аннотации `# verifies GEO-1-NN`
> в case-файлах) + `.claude/rules/api-conventions.md`. Deferred (redesign-only) кейсы — в `TEST-PLAN.md`.

## Region (`cases/region.py`) — `RegionService.Get`/`List` (public read)

| case-id | class | happy/neg | verifies | что проверяет |
|---|---|---|---|---|
| `REG-LST-CRUD-OK` | CRUD/CONF | happy | GEO-1-25 | List → 200, regions[] non-empty, item well-formed (id/name) |
| `REG-GET-CRUD-OK` | CRUD/CONF | happy | — | List→capture id→Get → 200, id echoes, createdAt present |
| `REG-GET-NEG-NOTFOUND` | NEG/CONF | neg | GEO-1-35 | absent well-formed id → 404 NOT_FOUND, verbatim "Region <id> not found" |
| `REG-GET-VAL-MALFORMED` | VAL/NEG | neg | GEO-1-31 | malformed non-slug id → 400 INVALID_ARGUMENT, no pgx/SQL leak |
| `REG-LST-BVA-PAGESIZE-ZERO` | BVA/PAGE | happy-bound | — | pageSize=0 → 200 (default applied) |
| `REG-LST-BVA-PAGESIZE-OVER-MAX` | BVA/VAL/PAGE | neg | GEO-1-27 | pageSize=10000 → 400 INVALID_ARGUMENT (rejected, not clamped) |
| `REG-LST-PAGE-BADTOKEN` | PAGE/VAL/NEG | neg | GEO-1-27 | garbage page_token → 400 INVALID_ARGUMENT |
| `REG-LST-PAGE-ROUNDTRIP` | PAGE/BVA | happy | — | pageSize=1 → follow nextPageToken → 200 |
| `REG-GET-CONF-NO-INFRA` | CONF/SEC | security | GEO-1-05, GEO-1-33 | public body NotContains infra/host-class/placement fields |
| `REG-LST-AUTHZ-ANON-DENY` | AUTHZ/NEG | neg | GEO-1-21 | anonymous → 401 UNAUTHENTICATED |
| `REG-CR-AUTHZ-ADMIN-NOT-PUBLIC` | AUTHZ/NEG/SEC | security | GEO-1-17, GEO-1-22 | admin write on public endpoint as non-admin → rejected, never 200 |

## Zone (`cases/zone.py`) — `ZoneService.Get`/`List` (public read)

| case-id | class | happy/neg | verifies | что проверяет |
|---|---|---|---|---|
| `ZON-LST-CRUD-OK` | CRUD/CONF | happy | GEO-1-24 | List → 200, zones[] non-empty, item well-formed (id/regionId/name) |
| `ZON-GET-CRUD-OK` | CRUD/CONF | happy | — | List→capture id→Get → 200, id echoes, regionId + createdAt present |
| `ZON-GET-NEG-NOTFOUND` | NEG/CONF | neg | GEO-1-31, GEO-1-35 | absent well-formed id → 404 NOT_FOUND, verbatim "Zone <id> not found" |
| `ZON-GET-VAL-MALFORMED` | VAL/NEG | neg | GEO-1-31 | malformed non-slug id → 400 INVALID_ARGUMENT, no pgx/SQL leak |
| `ZON-LST-BVA-PAGESIZE-ZERO` | BVA/PAGE | happy-bound | — | pageSize=0 → 200 (default applied) |
| `ZON-LST-BVA-PAGESIZE-OVER-MAX` | BVA/VAL/PAGE | neg | GEO-1-27 | pageSize=10000 → 400 INVALID_ARGUMENT (rejected, not clamped) |
| `ZON-LST-PAGE-BADTOKEN` | PAGE/VAL/NEG | neg | GEO-1-27 | garbage page_token → 400 INVALID_ARGUMENT |
| `ZON-LST-PAGE-ROUNDTRIP` | PAGE/BVA | happy | — | pageSize=1 → follow nextPageToken → 200 |
| `ZON-GET-CONF-NO-INFRA` | CONF/SEC | security | GEO-1-05, GEO-1-33 | public body NotContains infra/host-class/placement fields (status NOT asserted — mid-redesign) |
| `ZON-LST-AUTHZ-ANON-DENY` | AUTHZ/NEG | neg | GEO-1-21 | anonymous → 401 UNAUTHENTICATED |
| `ZON-CR-AUTHZ-ADMIN-NOT-PUBLIC` | AUTHZ/NEG/SEC | security | GEO-1-17, GEO-1-22 | admin write on public endpoint as non-admin → rejected, never 200 |
