<!--
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
-->

# kacho-geo

**Geography control plane платформы Kachō.** Сервис — источник истины о
географической топологии облака: какие есть **регионы** (Region) и **зоны
доступности** (Zone). На эти справочники ссылаются остальные сервисы (compute, vpc,
nlb), размещая ресурсы по зонам, — поэтому модель топологии вынесена в отдельный
небольшой leaf-сервис с одним владельцем.

Управляемые ресурсы:

| Ресурс | Назначение |
|---|---|
| **Region** | географический регион — верхнеуровневый элемент топологии (`id`, `name`) |
| **Zone** | зона доступности внутри региона (`id`, `regionId`, `status` UP/DOWN, `name`) |

`Zone.regionId` ссылается на `Region.id` (FK `ON DELETE RESTRICT`: регион нельзя
удалить, пока в нем есть зоны). Region и Zone — **cluster-scoped** справочники: они
не принадлежат проекту и одинаковы для всего кластера.

## Контракт API

Region и Zone — **admin-каталог**: пользователи только **читают** их (синхронно), а
заводит и правит записи администратор. Поэтому форма API отличается от ресурсов с
жизненным циклом:

| Поверхность | Методы | Листенер |
|---|---|---|
| Публичное чтение | `RegionService.Get/List`, `ZoneService.Get/List` | `9090` |
| Admin-управление (sync) | `InternalRegionService.Create/Update/Delete`, `InternalZoneService.Create/Update/Delete` | `9091` (internal) |

Мутации каталога **синхронны** — возвращают ресурс сразу (это осознанное решение для
admin-managed справочника с админ-назначаемыми неизменяемыми `id`), а не асинхронный
`Operation`. Admin-методы и весь internal-листенер `9091` **не публикуются** на внешнем
endpoint. Каждый RPC обоих листенеров проходит per-RPC авторизацию через kacho-iam.

```bash
# Публичное чтение зоны (REST через api-gateway)
curl http://localhost:18080/geo/v1/zones/zone-a -H 'Authorization: Bearer <JWT>'
# → { "id": "zone-a", "regionId": "region-1", "status": "UP", "name": "Zone A", "createdAt": "..." }
```

## Быстрый старт

Требуется Go 1.25+ и Postgres 16+. Собираются два бинаря — сервис и мигратор схемы:

```bash
make build            # → bin/kacho-geo
make build-migrator   # → bin/kacho-migrator

# Накатить схему БД (создает regions/zones/geo_outbox)
KACHO_GEO_DB_PASSWORD=secret bin/kacho-migrator up
KACHO_GEO_DB_PASSWORD=secret bin/kacho-migrator status

# Запустить сервис
bin/kacho-geo serve
```

Каталог стартует пустым — регионы и зоны заводит администратор через
`InternalRegionService`/`InternalZoneService` (встроенного seed нет).

Конфигурация — через YAML/ENV (префикс `KACHO_GEO_`). По умолчанию сервис **secure
by default**: per-RPC авторизация через kacho-iam и mTLS на обоих листенерах
обязательны, иначе он не стартует. Единственный способ запустить без них —
аварийный `KACHO_GEO_AUTHZ_BREAKGLASS=true` (только для локальной отладки / инцидента).

## Архитектура

Чистая архитектура со строгим правилом зависимостей:

```
handler ─┐
         ├─→ use-case ─→ domain
repo ────┘                ↑ (только структуры)
```

- `internal/domain` — Region/Zone и их `Validate()` (только stdlib + proto-типы);
- `internal/apps/kacho/api/{region,zone}` — use-case'ы и port-интерфейсы;
- `internal/repo/kacho/pg` — адаптер Postgres (handwritten pgx, без ORM);
- `internal/handler` — тонкий transport (public + internal);
- `internal/check` — per-RPC авторизация через kacho-iam;
- `cmd/kacho-geo` — точка сборки; `cmd/migrator` — мигратор схемы.

**Целостность данных — на уровне БД:** связь зон с регионами выражена FK
`ON DELETE RESTRICT`, а конкурентные вставки защищены первичным ключом — не
software-проверками. Admin-мутации атомарно (в той же транзакции) пишут запись в
`geo_outbox` для аудита и публикации событий.

**Место в платформе.** kacho-geo — **leaf-сервис**: по сборке зависит только от
`kacho-proto` и `kacho-corelib`, ни от одного другого сервиса. В runtime обращается
лишь к kacho-iam за авторизацией. Остальные сервисы ссылаются на `zoneId`/`regionId`
по значению (без cross-service FK) и валидируют их через `ZoneService.Get` /
`RegionService.Get`.

## Тестирование

```bash
make test-short   # быстрый прогон (моки, без внешних зависимостей)
make test         # полный прогон, включая integration на testcontainers (нужен Docker)
make vet          # go vet
make lint         # golangci-lint
```

- **unit** — use-case'ы и handler через mock-порты, домен, конфиг;
- **integration** (`internal/repo/kacho/pg/integration_test.go`) — реальный Postgres
  через testcontainers: CRUD, FK RESTRICT, конкурентная вставка, outbox.

## Структура репозитория

```
cmd/            точки входа: kacho-geo (сервис), migrator (схема БД)
internal/       domain, use-case'ы (region/zone), repo, handler, check, config
internal/migrations/   goose SQL-миграции схемы kacho_geo
docs-site/      документация (Docusaurus)
```

## Разработка и вклад

Как завести issue, оформить ветку и PR, требования к коду и тестам — см.
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Лицензия

Распространяется по **Business Source License 1.1** — свободное использование, кроме
случаев, когда продукт прямо или косвенно приносит коммерческую выгоду; такое
использование требует отдельной лицензии. Полный текст — [`LICENSE`](LICENSE).
