# 00 — Overview

## Роль сервиса

`kacho-vpc` — один из двух доменных сервисов Kachō (control-plane only).
Самый объемный сервис в системе.

Owns:
- 7 публичных VPC-ресурсов: Network, Subnet, Address, `NetworkInterface`
  (first-class NIC-ресурс домена), RouteTable, SecurityGroup, Gateway.
  У `Network` есть internal-only инфра-идентификатор (`vrf_id`), не на публичной поверхности.
- AddressPool + network-default binding-таблица (kacho-only, admin).
  (Region/Zone — leaf-домен `kacho-geo`, в kacho-vpc только `zone_id`-ссылка без FK.)
- inline IPAM allocation в request-path — internal/external IPv4 + internal IPv6.
- inline default-SG creation на Network.Create.
- in-process outbox + LISTEN/NOTIFY для подписки на изменения.

## Что делает (логически)

```
       ┌──────────────────────────────────────────────────────────┐
       │                        kacho-vpc                         │
       │                                                          │
  public  ──►  │ публичный API — 7 доменных сервисов:             │
       │       │  Network, Subnet, Address, NetworkInterface,     │
       │       │  RouteTable, SecurityGroup, Gateway              │
       │                                                          │
  admin   ──►  │ kacho-only API (AddressPool + binding)           │
       │       │  AddressPool (глобальный admin) + network/default│
       │                                                          │
  internal ──► │ InternalAddressService (allocate v4/v6/ext)      │
       │       │ InternalNetworkService (vrf_id / default-SG)     │
       │       │ InternalAddressPoolService (admin пулы)          │
       │       │ InternalNetworkInterfaceService (attach/detach)  │
       └──────────────────────────────────────────────────────────┘
```

> Оба счёта — **7** публичных и **4** internal — выводятся из контрактов, а не
> выписываются: `grep -h '^service ' proto/kacho/cloud/vpc/v1/*.proto`. Прежняя
> редакция рисовала «6 ресурсов» и три internal-сервиса; расходилась она не с
> кодом, а с соседней таблицей этого же файла.

> Region/Zone admin (`InternalRegionService`/`InternalZoneService`) — **не в kacho-vpc**:
> Geography — отдельный leaf-домен `kacho-geo`. VPC только ссылается на `zone_id` (TEXT, без FK).

## Ресурсы — две группы

**Клиентская (project-scoped)** — то что видит конечный клиент:

| Ресурс | Назначение | ID prefix |
|---|---|---|
| Network | VPC-сеть; несёт объявленный супернет (`ipv4_cidr_blocks`/`ipv6_cidr_blocks`) и `default_route_table_id` | `net` |
| Subnet | подсеть в Network; `placement_type` (ZONAL→`zone_id` / REGIONAL→`region_id`) обязателен и immutable; CIDR задаётся якорем `ipv4_cidr_primary`/`ipv6_cidr_primary` | `sub` |
| Address | external (publicIP) или internal (IPv4/IPv6 в Subnet) | `adr` |
| NetworkInterface | first-class NIC: принадлежит Subnet, ссылается на Address по id | `nic` |
| RouteTable | static routes для Network | `rtb` |
| SecurityGroup | firewall rules; `network_id` **обязателен** на Create и immutable после него | `sgr` |
| Gateway | выход наружу: `NAT` (IPv4) либо `EGRESS_ONLY` (IPv6); якорь — подсеть | `gtw` |

> Префиксы — из `pkg/ids` (3-char + 17 crockford-base32), у каждого ресурса свой.
> Operation у VPC несет **отдельный** prefix `PrefixOperationVPC = "enp"` (декаплен от
> ресурсных prefix'ов): api-gateway маршрутизирует `OperationService.Get` по первым 3
> символам id на нужный backend.

**Системная (kacho-only, admin, глобальная)** — то что админ управляет
для обеспечения IP allocation:

| Ресурс | Назначение | ID format |
|---|---|---|
| AddressPool | пул external IP с CIDR-блоками | `apl` |

> Region/Zone (`zone` / `zone-a`) — **не в kacho-vpc**, а в leaf-домене
> `kacho-geo`. `subnet.zone_id` / `address_pool.zone_id` хранятся как `TEXT`-id без FK,
> валидируются через `geo.v1.ZoneService.Get`.

**Bindings** (внутренние таблицы для cascade resolve):

| Binding | PK | Связывает |
|---|---|---|
| `address_pool_network_default` | network_id | Network → AddressPool (override на zone-default) |

## Layered architecture

Стандартная Clean Architecture:

Раскладка **выведена из дерева** (`git ls-files services/vpc/internal | cut -d/ -f4 | sort -u`),
а не выписана: прежняя редакция называла каталог `internal/service/`, которого в дереве нет
ни одного дня — use-case'ы живут по одному пакету на ресурс под `internal/apps/`.

```
cmd/vpc/main.go            composition root (runServe): pgxpool, репозитории,
                           use-case'ы, handler'ы, два gRPC-сервера (9090 + 9091).
cmd/migrator/main.go       отдельный бинарь миграций.

internal/
  domain/                  чистые сущности (только stdlib): Network, Subnet,
                           Address, NetworkInterface, RouteTable, SecurityGroup,
                           Gateway, AddressPool, CIDR-хелперы, newtypes.

  apps/kacho/api/<res>/    use-case на ресурс — по одному пакету на каждый из
                           восьми: address, addresspool, gateway, network,
                           networkinterface, routetable, securitygroup, subnet.
                           В каждом: файл на RPC + `iface.go` (порты этого
                           пакета) + `handler.go` (тонкий transport) + `helpers.go`.
  apps/kacho/services/     не-ресурсные use-case'ы: addressref, networkinternal,
                           nicinternal.
  apps/kacho/shared/       общее внутри сервиса: serviceerr (sentinel→gRPC),
                           listpage, macutil, pbconv.
  apps/kacho/config/       Config + Load + Validate (boot-guard посадки).
  apps/migrator/           обвязка goose (Dialect/Runner).

  repo/kacho/pg/           pgx-адаптер, реализация портов + outbox-emit.
  repo/helpers/            SQL-хелперы: paging, jsonb, unique, freelist_sql, …
  clients/                 gRPC-адаптеры: iam (project/authz/fga-register), geo
                           (zone/region), кеши существования и проекта.
  check/                   per-RPC authz-gate: permission_map, scope_filtered_rpcs.
  authzfilter/             словарь сервиса для общего сужателя списков (pkg/listnarrow).
  dto/toproto/             запись репозитория → proto-сообщение.
  handler/                 cross-cutting и internal-only transport (интерсепторы,
                           InternalAddressService, InternalNetworkService, …).
  observability/           метрики (Prometheus) и readiness.
  fgaboot/, fuzz/          загрузочная сверка модели прав; fuzz-корпус.
  migrations/              *.sql, embed.FS, goose-стиль up/down.
```

## Зависимости

**Inbound** (кто дергает kacho-vpc):
- `gateway/` — proxy для REST/gRPC клиентов.
- admin-tooling (curl/REST через api-gateway internal mux) / web-UI на :9091 RPC.
- `kacho-compute` — валидация NIC-spec (Subnet/SecurityGroup) + IPAM-аллокация Address.

**Outbound** (кого дергает kacho-vpc):
- `kaname.ProjectService.Get` — existence check владельца-проекта
  (`project_id` — id владельца-проекта) в Create-мутациях (канонический error
  `"Project X not found"`); `InternalIAMService.Check` — per-RPC authz-gate;
  `RegisterResource`/`UnregisterResource` — регистрация владения через IAM.
- `kacho-geo.ZoneService.Get` — валидация `zone_id` Subnet/AddressPool на request-path.

## База данных

`kacho_vpc` (`pg-vpc` StatefulSet в helm umbrella). Database-per-service —
никаких JOIN'ов с rm-БД или внешними источниками.

Особенности:
- Миграции в `internal/migrations/*.sql` (embed.FS) — `0001_initial.sql`
  (baseline-схема со всеми таблицами/индексами/constraints) + инкрементные.
  Число и последний номер здесь **не выписаны намеренно** — их место одно, и оно
  выводится из дерева: `git ls-files services/vpc/internal/migrations/*.sql`
  (см. [`05-database.md`](05-database.md)).
- Используем продвинутые Postgres-фичи: `EXCLUDE USING gist` (CIDR
  no-overlap), partial UNIQUE indices, computed columns, `inet/cidr`
  типы и операторы (`<<`, `>>=`), `JSONB` containment с GIN индексом
  (`jsonb_path_ops`), `LISTEN/NOTIFY` для outbox stream, `xmin::text` для
  optimistic locking.

См. [`05-database.md`](05-database.md).

## Что НЕ owns kacho-vpc

- Account/Project — это `kaname`. VPC только проверяет существование
  владельца-проекта через ProjectClient.
- Region/Zone — это `kacho-geo` (leaf-домен Geography). VPC ссылается на `zone_id`
  по TEXT-id без FK, валидирует через `geo.v1.ZoneService.Get`.
- Operations storage — таблица `operations` в схеме `kacho_vpc` (объявлена в `0001_initial.sql`),
  логика worker'а — в `pkg/operations`.
- Instance / MachineType — `kacho-compute`.
- Volume / Snapshot / Image / DiskType (блочное хранение) — `kacho-storage`.
  Здесь стояло «Compute/instances/disks — kacho-compute»: раскол блочного хранения
  завершён, контрактов диска, образа и снимка у вычислений нет вовсе.

## Quick links

- [Resources детально](01-resources.md)
- [Data flows / sequence](02-data-flows.md)
- [IPAM (главное)](03-ipam.md)
- [API surface (RPC список)](04-api-surface.md)
- [DB schema + миграции](05-database.md)
- [Conventions + gotchas](06-conventions.md)

Дополнительно:
- GitHub Issues монорепо (`github.com/PRO-Robotech/kacho/issues`) — долги, баги, задачи.
  Разработка ведётся в монорепо; прежний отдельный репозиторий сервиса адресом для
  заведения задач больше не является.
- [07-known-divergences.md](07-known-divergences.md) — реестр намеренных дизайн-решений Kachō VPC.
- `tests/newman/` — e2e regression suite.
