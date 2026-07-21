# Taxonomy кейсов newman — kacho-geo

Структура воспроизводит эталон `kacho-vpc/tests/newman` (тот же `<DOMAIN>-<METHOD>-<CLASS>-<DETAIL>`),
суженный под read-only leaf-каталог оси размещения (Region/Zone).

## Naming convention

```
<DOMAIN>-<METHOD>-<CLASS>-<DETAIL>
```

| Часть | Значения (geo) |
|---|---|
| DOMAIN | `REG` (Region), `ZON` (Zone) |
| METHOD | `GET`, `LST` (List), `CR` (Create — только admin-not-on-public guard) |
| CLASS | `CRUD`, `VAL`, `NEG`, `BVA`, `CONF`, `AUTHZ`, `PAGE`, `SEC` |
| DETAIL | свободное краткое описание |

Примеры: `REG-GET-CRUD-OK`, `ZON-GET-NEG-NOTFOUND`, `REG-LST-BVA-PAGESIZE-OVER-MAX`,
`ZON-GET-CONF-NO-INFRA`.

## Классы

| Класс | Назначение | Техника |
|---|---|---|
| `CRUD` | Happy read: List / Get существующего каталог-item'а | Use-case |
| `VAL` | Sync-валидация id: malformed slug → `INVALID_ARGUMENT` первым стейтментом | ECP (charset) |
| `NEG` | NotFound / anon-deny / admin-not-on-public | Decision tables + error-guessing |
| `BVA` | Boundary на `pageSize` (0 → default, >max → reject) | BVA |
| `PAGE` | Pagination: token round-trip + garbage token | BVA + property |
| `CONF` | Conformance к контракту: verbatim NotFound-текст, shape response | Approval/snapshot |
| `AUTHZ` | AuthN-обязательность (anonymous → 401); Internal-vs-external split | Permission matrix |
| `SEC` | Two-projection / capacity-anonymization: infra/host-class не на public | Security probe |

## Priority уровни

| Priority | Применение |
|---|---|
| P0 | Security (two-projection NotContains infra/host-class, admin-not-on-public) — must-pass |
| P1 | CRUD happy read, malformed/notfound-контракт, pagination-validate, authN-deny |
| P2 | BVA (pageSize=0), pagination round-trip |

## geo-специфика (чем отличается от project-scoped сервисов)

- **Не project-scoped.** Region/Zone — глобальный cluster-scoped каталог; нет `projectId`,
  нет `_suiteProjectId` / cross-project isolation, нет per-suite project-фикстур.
- **Read-only public surface.** Public — только `RegionService`/`ZoneService` `Get`/`List` (sync).
  Мутации (Create/Update/Delete) — `InternalRegion/ZoneService` (Internal-only :9091, ban #6);
  на публичном endpoint их нет — кейсы `*-CR-AUTHZ-ADMIN-NOT-PUBLIC` это чёрно-ящично сторожат.
- **Admin-seeded каталог.** Region/Zone создаются оператором/umbrella-seed'ом, НЕ tenant'ом на
  лету → `retry_until_present`/`retry_until_authorized` (read-your-writes своего свежего ресурса)
  тут не нужны — читаем стабильный seeded-каталог. Negatives не оборачиваются.
- **Ambient authN.** Public read гейтится аутентификацией (см. env `jwtBootstrap`); anonymous → 401.

## Что НЕ покрываем в этой suite (scope-cut)

| Зона | Причина | Альтернатива |
|---|---|---|
| Internal admin-CRUD (`InternalRegion/ZoneService`, :9091) | Не публичный API; env стенда не пробрасывает internal-mux | Go integration (`services/geo/internal/repo/.../*_integration_test.go`) + отдельная internal-suite при наличии `internalBaseUrl` |
| GEO-1-redesign-only поведение (EXEMPT ambient-read=200 для zero-binding, two-projection status→Internal, `/geo/v1/internal/…`, `countryCode°`/`openForPlacement°`) | Ещё НЕ приземлено в `redesign/integration` (см. TEST-PLAN §Deferred) | Добавляется в эту suite вместе с GEO-1 PR (DoD-трассировка) |
| Performance / load | Не функциональная проверка | k6/ghz |
