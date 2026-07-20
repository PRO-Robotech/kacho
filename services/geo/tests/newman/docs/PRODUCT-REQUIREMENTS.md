# Product Requirements — kacho-geo public read (нормативный регламент)

`REQ-*` — что продукт ДОЛЖЕН / НЕ ДОЛЖЕН на публичной read-поверхности каталога оси
размещения (Region/Zone). Выведено из APPROVED acceptance GEO-1 + `api-conventions.md`.
Каждый REQ мапится на newman-кейс(ы) (`Validated-by`).

## Read (Get / List)

| REQ | Требование | Validated-by |
|---|---|---|
| REQ-GEO-READ-01 | `List` возвращает `regions[]`/`zones[]` — массив плоских public-ресурсов (camelCase); seeded-каталог непуст | `REG-LST-CRUD-OK`, `ZON-LST-CRUD-OK` |
| REQ-GEO-READ-02 | List-item несёт **полную** public-проекцию (Region: id/name; Zone: id/regionId/name), не reduced-subset | `REG-LST-CRUD-OK`, `ZON-LST-CRUD-OK` |
| REQ-GEO-READ-03 | `Get` существующего id → 200, `id` эхо-возврат, `createdAt` присутствует (усечён до секунд на wire) | `REG-GET-CRUD-OK`, `ZON-GET-CRUD-OK` |
| REQ-GEO-READ-04 | `Get` well-formed-но-отсутствующего id → `NOT_FOUND`, verbatim `"Region\|Zone <id> not found"` | `REG-GET-NEG-NOTFOUND`, `ZON-GET-NEG-NOTFOUND` |
| REQ-GEO-READ-05 | `Get` malformed (non-slug) id → `INVALID_ARGUMENT` первым стейтментом; **без** утечки pgx/SQL-текста | `REG-GET-VAL-MALFORMED`, `ZON-GET-VAL-MALFORMED` |

## Pagination

| REQ | Требование | Validated-by |
|---|---|---|
| REQ-GEO-PAGE-01 | `pageSize=0` → 200 (сервер применяет default) | `REG-LST-BVA-PAGESIZE-ZERO`, `ZON-LST-BVA-PAGESIZE-ZERO` |
| REQ-GEO-PAGE-02 | `pageSize > 1000` → `INVALID_ARGUMENT` (**отвергается**, не clamp'ится), field-violation `page_size` | `REG-LST-BVA-PAGESIZE-OVER-MAX`, `ZON-LST-BVA-PAGESIZE-OVER-MAX` |
| REQ-GEO-PAGE-03 | garbage `pageToken` → `INVALID_ARGUMENT` | `REG-LST-PAGE-BADTOKEN`, `ZON-LST-PAGE-BADTOKEN` |
| REQ-GEO-PAGE-04 | opaque `nextPageToken` round-trip'ится без ошибки | `REG-LST-PAGE-ROUNDTRIP`, `ZON-LST-PAGE-ROUNDTRIP` |

## Security / two-projection / authN

| REQ | Требование | Validated-by |
|---|---|---|
| REQ-GEO-SEC-01 | Публичная проекция Region/Zone **НЕ** несёт инфра-полей (numericInfraId, hostClasses, underlayAnchor, capacityHint, failureDomainCount) — host-class физически не выходит на public (two-projection, `:9091`-only) | `REG-GET-CONF-NO-INFRA`, `ZON-GET-CONF-NO-INFRA` |
| REQ-GEO-SEC-02 | Ни одна public-проекция не несёт ось-дискриминатор `placementType`/`placementScope` (derived-by-service) | `REG-GET-CONF-NO-INFRA`, `ZON-GET-CONF-NO-INFRA` |
| REQ-GEO-SEC-03 | Public read гейтится **authN** (anonymous → `UNAUTHENTICATED`); anonymous-full-access запрещён на любом листенере | `REG-LST-AUTHZ-ANON-DENY`, `ZON-LST-AUTHZ-ANON-DENY` |
| REQ-GEO-SEC-04 | Admin write-verb (`InternalRegion/ZoneService.Create`) на публичном endpoint для не-admin'а **никогда не мутирует** (rejected 401/403/404/501) — Internal-vs-external split (ban #6) | `REG-CR-AUTHZ-ADMIN-NOT-PUBLIC`, `ZON-CR-AUTHZ-ADMIN-NOT-PUBLIC` |

## НЕ ДОЛЖЕН (invariants)

- НЕ отвечать 500 / не течь SQLSTATE/pgx/goroutine на malformed id (REQ-GEO-READ-05).
- НЕ clamp'ить `pageSize` вне `[0..1000]` (REQ-GEO-PAGE-02 — отвергать).
- НЕ раскрывать инфра/host-class/underlay/placement на public (REQ-GEO-SEC-01/02).
- НЕ выполнять admin-мутацию по публичному endpoint (REQ-GEO-SEC-04).
