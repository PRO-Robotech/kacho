# kacho-geo newman — PRODUCT-REQUIREMENTS

Требования, покрываемые geo regression-suite (AS-IS-контракт). `REQ-*` → case-id
traceability. Источник: proto/kacho/cloud/geo/v1/* + services/geo/internal/* +
`.claude/rules/{api-conventions,data-integrity,security}.md`.

| REQ | требование | покрыто case-id |
|---|---|---|
| REQ-GEO-01 | Public read gateway→geo достижим (mTLS/DNS), не 503/code14 | GEO-REG-GT-CONF-OK, GEO-ZON-GT-CONF-OK |
| REQ-GEO-02 | Region/Zone — плоский flat-resource (camelCase, domain-поля верхнего уровня) | REG-GET-CRUD-OK, ZON-GET-CRUD-OK |
| REQ-GEO-03 | `createdAt` усечён до секунд на wire | REG-GET-CRUD-OK, ZON-GET-CRUD-OK |
| REQ-GEO-04 | Two-projection: инфра-полей на public НЕТ | REG-GET-CONF-NO-INFRA, ZON-GET-CONF-NO-INFRA |
| REQ-GEO-05 | malformed id → sync INVALID_ARGUMENT первым стейтментом | REG-GET-VAL-MALFORMED, ZON-GET-VAL-MALFORMED, IRG-CR-VAL-MALFORMED-ID, IZN-CR-VAL-MALFORMED-ID |
| REQ-GEO-06 | well-formed-absent id → NOT_FOUND, verbatim "<Resource> <id> not found" | REG-GET-NEG-NOTFOUND, ZON-GET-NEG-NOTFOUND |
| REQ-GEO-07 | pageSize вне [0..1000] отвергается (не clamp); garbage token → INVALID_ARGUMENT | REG-LST-BVA-PAGESIZE-OVER-MAX, REG-LST-PAGE-TOKEN-GARBAGE, ZON-LST-* |
| REQ-GEO-08 | pagination round-trip корректен | REG-LST-PAGE-ROUNDTRIP, ZON-LST-PAGE-ROUNDTRIP |
| REQ-GEO-09 | required id обязателен на Create → sync INVALID_ARGUMENT "region id is required" | IRG-CR-VAL-EMPTY-ID |
| REQ-GEO-10 | admin Create/Update/Delete → Operation envelope (async), ресурс материализуется | IRG-CR-CRUD-OK, IRG-UPD-CRUD-OK, IZN-CR-CRUD-OK, IZN-UPD-CRUD-STATUS |
| REQ-GEO-11 | within-service FK RESTRICT: регион с зонами не удаляется | IRG-DEL-NEG-HASZONES-INVARIANT |
| REQ-GEO-12 | zone с несуществующим regionId не создаётся (FK reject) | IZN-CR-NEG-GHOST-REGION-INVARIANT |
| REQ-GEO-13 | zone status: explicit DOWN persists; omitted → UP (AS-IS default) | IZN-CR-CONF-STATUS-DOWN, IZN-CR-CONF-STATUS-DEFAULT-UP |
| REQ-GEO-14 | dup id (PK) не создаёт phantom | IRG-CR-NEG-DUP-INVARIANT |
| REQ-GEO-15 | ban #6: Internal* admin не на публичном endpoint | ANP-REG-CR-NOT-PUBLIC, ANP-ZON-CR-NOT-PUBLIC |
| REQ-GEO-16 | public read: authN обязателен (anonymous → UNAUTHENTICATED) | GEO-REG-GT-AUTHZ-ANON-DENY, GEO-ZON-GT-AUTHZ-ANON-DENY |
| REQ-GEO-17 | public read: authZ viewer@cluster (no-viewer → PERMISSION_DENIED, AS-IS) | GEO-REG-GT-AUTHZ-NOVIEWER-DENY |
| REQ-GEO-18 | admin CRUD: system_admin@cluster (non-admin → PERMISSION_DENIED) | GEO-REG-CR-AUTHZ-NONADMIN-DENY, GEO-ZON-CR-AUTHZ-NONADMIN-DENY |
| REQ-GEO-19 | Operation op-poll маршрутизируется + owner-scoping (BOLA) | GEO-IOP-POLL-ROUTING-OK*, GEO-IOP-GET-AUTHZ-BOLA*, GOP-GET-VAL-MALFORMED |

`*` — RED, заблокировано product-багом PRO-Robotech/kacho#55 (см. RESULTS.md).

## Осознанно НЕ покрыто (GEO-1 redesign — не реализован)

Требования GEO-1 (openForPlacement°, two-projection-move-of-status, GetInternal,
coupling, immutable regionId, fresh-DOWN, UNIQUE(name), countryCode°, warnings°,
update_mask discipline, ambient-exempt read, list filters) — merge-gated, surface
не существует. Traceability добавляется при приземлении GEO-1. См. RESULTS.md.
