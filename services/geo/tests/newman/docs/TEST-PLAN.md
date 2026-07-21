# kacho-geo newman — TEST-PLAN

Black-box regression-план geo через api-gateway REST. Объект — сервис в целом
(public gRPC/REST :9090 + cluster-internal :9091 через gateway internal mux). Код —
только для понимания scope. Источник контракта — AS-IS implementation + acceptance
(`docs/specs/sub-phase-GEO-1-…`, часть «AS-IS») + `.claude/rules/api-conventions.md`.

## Поверхность (10 implemented RPC)

| RPC | путь | вид | покрытие |
|---|---|---|---|
| RegionService.Get | GET /geo/v1/regions/{id} | sync | region.py |
| RegionService.List | GET /geo/v1/regions | sync | region.py |
| ZoneService.Get | GET /geo/v1/zones/{id} | sync | zone.py |
| ZoneService.List | GET /geo/v1/zones | sync | zone.py |
| InternalRegionService.Create | POST /geo/v1/regions (:9091) | async Op | internal-region.py |
| InternalRegionService.Update | PATCH /geo/v1/regions/{id} (:9091) | async Op | internal-region.py |
| InternalRegionService.Delete | DELETE /geo/v1/regions/{id} (:9091) | async Op | internal-region.py |
| InternalZoneService.Create | POST /geo/v1/zones (:9091) | async Op | internal-zone.py |
| InternalZoneService.Update | PATCH /geo/v1/zones/{id} (:9091) | async Op | internal-zone.py |
| InternalZoneService.Delete | DELETE /geo/v1/zones/{id} (:9091) | async Op | internal-zone.py |

RPC-покрытие: **10/10 implemented**. (OperationService op-poll — cross-cutting,
operation.py; заблокирован #55.)

## Стратегия по рискам (risk-based)

| зона | риск | класс кейсов |
|---|---|---|
| read reachability (gateway→geo dial) | High | CONF/error-guessing (503/code14 regression, migrated) |
| authz (anon/no-viewer/non-admin/BOLA) | Critical | AUTHZ deny matrix + ban #6 |
| within-service invariant (FK RESTRICT zones→regions) | High | STATE/NEG (delete-non-empty, ghost-region) |
| async op lifecycle через gateway | High | STATE (op-poll — RED #55) |
| контракт (verbatim NotFound, timestamp-truncate, two-projection) | High | CONF |
| pagination | Medium | BVA/PAGE |

## Изоляция / идемпотентность

- geo каталог — ГЛОБАЛЬНЫЙ (cluster-scoped), не project-scoped. Кейсы адресуют СВОИ
  `qa-*-{{runId}}` регионы/зоны (slug-safe suffix) + cleanup внутри кейса. Негативы —
  по фиксированным absent id (`{{garbageRegionId}}`/`{{garbageZoneId}}`) и malformed
  (`{{malformedId}}`). Общего мутабельного state между коллекциями нет → параллельно-безопасно.
- read-your-writes: internal Create/Update async; op-poll недоступен (#55) →
  материализация подтверждается публичным Get с bounded-retry (`retry_get_until_found`).

## Прогон

```
python3 scripts/validate-cases.py      # dup-id + CASES-INDEX (hard-fail до newman)
python3 scripts/gen.py                 # cases/*.py → collections/*.json
bash scripts/run.sh                     # весь suite (false-green guard: MISSING/rc)
bash scripts/run.sh --service region    # одна коллекция
```

Umbrella (CI): `deploy/scripts/newman-parallel.sh` (geo — в PHASE2-волне,
изолированной от leaf-resource нагрузки: geo мутирует ГЛОБАЛЬНЫЙ каталог, который
читают vpc/compute/nlb при резолве zone/region — поэтому geo не гонится конкурентно
с ними).

## Gate-стадии

1. `validate-cases.py` (unique + catalogued) — до newman.
2. `gen.py` (регенерация коллекций).
3. `run.sh` — целевая + полная; false-green guard роняет на MISSING/failed/rc!=0.
4. GREEN везде, кроме RED-known-failing (#55, декларированы в RESULTS.md).
