# kacho-geo newman — TAXONOMY

Case-id: `<DOMAIN>-<METHOD>-<CLASS>-<DETAIL>`. DOMAIN и METHOD выдают ресурс и RPC,
CLASS — тип проверки, DETAIL — конкретику.

## DOMAIN-prefix

| prefix | ресурс / поверхность |
|---|---|
| `REG` | public RegionService (Get/List) |
| `ZON` | public ZoneService (Get/List) |
| `IRG` | InternalRegionService (admin CRUD, :9091) |
| `IZN` | InternalZoneService (admin CRUD, :9091) |
| `GOP` | OperationService (op-poll, generic) |
| `ANP` | admin-not-on-public (ban #6 guard) |
| `GEO-REG` / `GEO-ZON` | geo authz-матрица + migrated iam geo-read cases |
| `GEO-IOP` | geo Operation op-poll bug-lock (RED #55) |

## METHOD

`GET` (Get) · `LST` (List) · `CR` (Create) · `UPD` (Update) · `DEL` (Delete) ·
`GT` (мигрированные iam read — Get/List) · `POLL` / `IOP` (op-poll).

## CLASS (тип проверки)

| class | смысл |
|---|---|
| `CRUD` | happy-path CRUD/read |
| `CONF` | conformance — стабильный контракт Kachō (форма, verbatim-текст, timestamp-truncate, two-projection) |
| `VAL` | field-validation rejection (malformed id, длина) |
| `NEG` | negative path (NotFound, FK-reject, no-phantom, deny) |
| `BVA` | boundary value (pageSize 0/1/>max, длина id) |
| `PAGE` | pagination (token garbage/roundtrip) |
| `STATE` | state-transition (Operation done, status flip, FK RESTRICT) |
| `AUTHZ` | authN/authZ (anon/no-viewer/non-admin deny, ban #6, BOLA) |
| `IDM` | идемпотентность / duplicate |

## PRIORITY

`P0` (critical — read reachability, admin CRUD happy, authz deny, ban #6, FK) ·
`P1` (важные негативы/conf) · `P2` (BVA/edge) · `P3` (низкий).

## Классы техник test-design (применены)

ECP (auth-class, id-class), BVA (pageSize/id-length boundaries), decision-table
(status × default), state-transition (Operation, status flip, FK RESTRICT),
conformance (verbatim NotFound, createdAt truncate, two-projection no-infra),
error-guessing (503/code14 dial, authN-vs-authZ ordering, phantom under async-fail).
