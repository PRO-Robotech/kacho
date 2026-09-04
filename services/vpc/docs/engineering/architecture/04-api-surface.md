# 04 — API Surface

Полный список RPC kacho-vpc + соответствующие REST endpoints. Public — **7** доменных
сервисов, internal — **4**. Оба числа выводятся из контрактов, а не выписываются:
`grep -h '^service ' proto/kacho/cloud/vpc/v1/*.proto | sort`. Прежняя редакция считала
`NetworkInterfaceService` **сверх** семи ресурсов и получала восемь — интерфейс является
одним из семи, а не восьмым.

## Сводка

| Категория | Listener | REST exposed |
|---|---|---|
| Public домены (7) | `:9090` (public gRPC) | ✅ да, через api-gateway (оба listener'а) |
| Internal admin (kacho-only, 4) | `:9091` (internal gRPC) | ✅ выборочно — только cluster-internal listener (CRUD + admin actions) |

## Public сервисы (`:9090`)

| Сервис | RPC | Что делает |
|---|---|---|
| `NetworkService` | CRUD + AddCidrBlocks + RemoveCidrBlocks + ListOperations | публичная проекция `Network` инфра-полей не несёт; `vrf_id` отдаётся только `InternalNetworkService`. `Network` объявляет супернет (`ipv4_cidr_blocks`/`ipv6_cidr_blocks`) и системный `default_route_table_id` |
| `SubnetService` | CRUD + AddCidrBlocks + RemoveCidrBlocks + ListUsedAddresses + ListOperations | `placement_type` обязателен (`ZONAL`→`zone_id` / `REGIONAL`→`region_id`), immutable; CIDR на Create — **якорь** `ipv4_cidr_primary`/`ipv6_cidr_primary` (immutable), дополнительные диапазоны — только через `:add/:remove-cidr-blocks` (`ipv4_cidr_blocks`/`ipv6_cidr_blocks`); в `UpdateSubnet` CIDR-полей нет вовсе — номера зарезервированы |
| `AddressService` | CRUD + ListOperations | `CreateAddressRequest` получил `internal_ipv6_address_spec`; `ListAddressesRequest.subnet_id` матчит `internal_ipv4`/`internal_ipv6`; `Delete` адреса в использовании у NIC → `FailedPrecondition` |
| `RouteTableService` | CRUD + ListOperations. Три verb-RPC — AddRoutes / RemoveRoutes / UpdateRoute — **объявлены контрактом, но не обслуживаются**: вызов получает `UNIMPLEMENTED`. Гранулярной правкой маршрутов пользоваться нельзя; рабочий путь — полная замена набора через `Update` с `update_mask: ["static_routes"]` | Почему так и при каком условии запись снимается — [07-known-divergences.md](07-known-divergences.md) §26. Здесь намеренно назван **исход** (что получит вызывающий), а не механизм отказа: механизм у записи один владелец, и два места об одном предмете разошлись бы на первой же правке |
| `SecurityGroupService` | CRUD + UpdateRules + UpdateRule + ListOperations | `network_id` **обязателен** на Create (пустой → `InvalidArgument "network_id required"`, синхронно в use-case) и immutable после него; `List?filter=network_id="<id>"` |
| `GatewayService` | CRUD + ListOperations | |
| `NetworkInterfaceService` | Get + List + Create + Update + Delete + ListOperations | REST `/vpc/v1/networkInterfaces`; NIC принадлежит `Subnet` (`subnet_id`), ссылается на `Address` по id (`v4_address_ids[]`/`v6_address_ids[]`), `security_group_ids[]`, `used_by` (денормализованное зеркало — кто использует NIC); проекция чисто control-plane (lean) — инфра-полей у kacho-vpc нет |

> `ListOperations` для Network/Subnet/Address/NetworkInterface не требует существования ресурса
> (precondition `repo.Get` убран — handler best-effort: жив → project-ownership; NotFound → пропуск).
> Для route_table/SG/gateway `ListOperations` по-прежнему гейтит на `repo.Get`.

REST mapping — `google.api.http` аннотации в proto, см. `proto/kacho/cloud/vpc/v1/<resource>_service.proto`.

## Internal admin сервисы (`:9091`, kacho-only)

| Сервис | RPC | Что делает |
|---|---|---|
| `InternalAddressPoolService` | CRUD пулов + binding (BindAsNetworkDefault / UnbindNetworkDefault) + observability (ListAddresses, GetUtilization) | |
| `InternalNetworkService` | GetNetwork (internal-only `vrf_id`) + SetDefaultSecurityGroupId (admin-only computed-field setter) | публичная проекция `Network` инфра-полей не содержит |
| `InternalAddressService` | AllocateInternalIP / **AllocateInternalIPv6** / AllocateExternalIP + SetAddressReference / ClearAddressReference / GetAddressReference + MarkAddressEphemeralInUse (referrer-tracking «кто использует адрес» — отражается в `Address.used` и `SubnetService.ListUsedAddresses.references[]`; referrer'ы: `compute_instance`, `network_interface`) | |
| `InternalNetworkInterfaceService` | Attach / Detach / ListByInstance — привязка NIC↔Instance, инициируется kacho-compute | Attach/Detach — атомарный CAS на `network_interfaces.used_by_id`, идемпотентны на повторе; vpc валидирует **свою** строку и compute обратно не зовёт. `ListByInstance` — единственный RPC vpc с `ScopeFiltered` (см. [07-known-divergences.md](07-known-divergences.md) §21) |
| ~~`InternalRegionService` / `InternalZoneService`~~ | — | Geography (Region/Zone) живет в leaf-домене `kacho-geo`; в kacho-vpc этих сервисов нет |

## REST endpoints (через api-gateway)

### Public (exposed на оба listener'а)

```
# Network
GET    /vpc/v1/networks?projectId=
POST   /vpc/v1/networks                              → Operation   # body: {ipv4CidrBlocks?, ipv6CidrBlocks?} — объявленный супернет
GET    /vpc/v1/networks/{network_id}
PATCH  /vpc/v1/networks/{network_id}                 → Operation
DELETE /vpc/v1/networks/{network_id}                 → Operation
POST   /vpc/v1/networks/{network_id}:add-cidr-blocks    → Operation  # расширение супернета
POST   /vpc/v1/networks/{network_id}:remove-cidr-blocks → Operation
GET    /vpc/v1/networks/{network_id}/operations
#   дочерних списков у сети НЕТ: подсети, группы безопасности и таблицы маршрутов
#   перечисляются своими коллекционными списками с сужением filter=network_id="<id>"

# Subnet
GET/POST/PATCH/DELETE /vpc/v1/subnets[/{id}]
#   POST body: {placementType, zoneId|regionId, ipv4CidrPrimary?, ipv6CidrPrimary?}
#   PATCH: CIDR-полей нет — их номера в контракте зарезервированы
GET    /vpc/v1/subnets/{subnet_id}/addresses         (UsedAddress[])
GET    /vpc/v1/subnets/{subnet_id}/operations        # переживает удаление подсети
POST   /vpc/v1/subnets/{subnet_id}:add-cidr-blocks    # body: {ipv4CidrBlocks?, ipv6CidrBlocks?}
POST   /vpc/v1/subnets/{subnet_id}:remove-cidr-blocks # body: {ipv4CidrBlocks?, ipv6CidrBlocks?}

# Address
GET/POST/PATCH/DELETE /vpc/v1/addresses[/{id}]   # POST принимает internalIpv6AddressSpec
GET    /vpc/v1/addresses?subnetId=<id>           # фильтр по internal_ipv4 ИЛИ internal_ipv6
GET    /vpc/v1/addresses?ipAddress=<ip>          # «чей это адрес» — обе семьи, замена снятому GetByValue

# NetworkInterface (top-level camelCase networkInterfaces)
GET/POST/PATCH/DELETE /vpc/v1/networkInterfaces[/{id}]   # POST: subnet_id; v4_address_ids/v6_address_ids/security_group_ids опциональны
GET    /vpc/v1/networkInterfaces/{network_interface_id}/operations   # переживает удаление NIC

# RouteTable (top-level — camelCase routeTables)
GET/POST/PATCH/DELETE /vpc/v1/routeTables[/{id}]

# SecurityGroup
GET/POST/PATCH/DELETE /vpc/v1/securityGroups[/{id}]   # POST: networkId ОБЯЗАТЕЛЕН; GET?filter=network_id="<id>"
PATCH  /vpc/v1/securityGroups/{security_group_id}/rules             # UpdateRules — PATCH на /rules
PATCH  /vpc/v1/securityGroups/{security_group_id}/rules/{rule_id}   # UpdateRule

# Gateway
GET/POST/PATCH/DELETE /vpc/v1/gateways[/{id}]
```

> ⚠️ REST-пути неоднородны (наследие proto-аннотаций, proto-decided; см.
> [`07-known-divergences.md`](07-known-divergences.md)): top-level
> `routeTables`/`securityGroups`/`addressPools` — camelCase,
> custom-методы — kebab с двоеточием (`:add-cidr-blocks`),
> `OperationService.Get` — `/operations/{id}` (без `/vpc/v1/`).

### Admin (kacho-only, **только cluster-internal listener**)

```
# (Region/Zone admin — домен kacho-geo: /geo/v1/{regions,zones}; в kacho-vpc их нет)

# AddressPool
GET    /vpc/v1/addressPools?zoneId=&kind=
POST   /vpc/v1/addressPools
GET/PATCH/DELETE /vpc/v1/addressPools/{pool_id}

# AddressPool admin actions
GET    /vpc/v1/addressPools/{pool_id}/utilization
GET    /vpc/v1/addressPools/{pool_id}/addresses?projectId=

# AddressPool binding
POST   /vpc/v1/networks/{network_id}/addressPoolBinding   {poolId}
DELETE /vpc/v1/networks/{network_id}/addressPoolBinding
```

⚠️ Все admin paths **не должны** быть доступны на external TLS endpoint
(`api.kacho.local:443`, advertised для внешних клиентов). См. [`06-conventions.md`](06-conventions.md#admin-boundary).

### Internal-only (НЕ через apiGW REST, gRPC server-to-server)

```
InternalAddressService.AllocateInternalIP / AllocateInternalIPv6
                       AllocateExternalIP / AllocateExternalIPv6
                       SetAddressReference / ClearAddressReference / GetAddressReference
                       MarkAddressEphemeralInUse / CreateOwnedAddress
InternalNetworkService.GetNetwork / SetDefaultSecurityGroupId
InternalNetworkInterfaceService.Attach / Detach / ListByInstance
```

Эти RPC дергают только сервисы (kacho-vpc сам себя через wiring, kacho-compute —
через gRPC). Не зарегистрированы в apiGW restmux: `gateway/internal/allowlist/list.go`
это прямо оговаривает для `InternalNetworkInterfaceService`.

## Operations (LRO)

Все мутации (Create/Update/Delete/AddCidrBlocks/...) возвращают
`Operation`. Шаблон:

```protobuf
service NetworkService {
  rpc Get (GetNetworkRequest) returns (Network);                     // sync read
  rpc List (...) returns (ListNetworksResponse);                     // sync read
  rpc Create (CreateNetworkRequest) returns (operation.Operation);   // async
  rpc Update (UpdateNetworkRequest) returns (operation.Operation);   // async
  rpc Delete (DeleteNetworkRequest) returns (operation.Operation);   // async
}
```

Клиент полит `OperationService.Get(operation_id)` до `done=true` (REST: `GET /operations/{id}`,
**без** `/vpc/v1/` префикса). api-gateway имеет in-process `opsproxy` — один URL
`/operations/{id}` маршрутизируется по 3-char prefix ID на нужный backend
(`enp...` → kacho-vpc). Operation.id несет отдельный per-domain prefix
`PrefixOperationVPC = "enp"` (декаплен от ресурсных prefix'ов вроде `net`).
Неизвестный prefix → `400 INVALID_ARGUMENT "unknown prefix"` (intentional fail-fast
перед роутингом; см. [`07-known-divergences.md`](07-known-divergences.md)).

Delete-RPC в домене **восемь** — по одному на каждый из семи публичных ресурсов плюс
`InternalAddressPoolService.Delete` (предикат: `grep -c '  rpc Delete'
proto/kacho/cloud/vpc/v1/*.proto | awk -F: '{s+=$2} END{print s}'`). Каждый возвращает
`google.protobuf.Empty` в `response`, а `Delete<Resource>Metadata` лежит в
`Operation.metadata`. Прежняя редакция называла шесть — число отстало от домена на два
ресурса и не было ничем удержано.

## Где смотреть proto

```
proto/kacho/cloud/vpc/v1/
├── network.proto                       Network message
├── network_service.proto               NetworkService RPC
├── subnet.proto / subnet_service.proto
├── address.proto / address_service.proto
├── route_table.proto / route_table_service.proto
├── security_group.proto / security_group_service.proto
├── gateway.proto / gateway_service.proto
├── network_interface.proto / network_interface_service.proto   NetworkInterface
│
├── internal_address_pool_service.proto AddressPool admin + observability
├── internal_network_service.proto      GetNetwork (internal-only vrf_id) + SetDefaultSecurityGroupId
├── internal_address_service.proto      Allocate*IP (v4/v6/ext), {Set,Clear,Get}AddressReference
├── internal_network_interface_service.proto  Attach / Detach / ListByInstance (NIC↔Instance)
└── package_options.proto
# (Region/Zone — домен kacho-geo: proto/kacho/cloud/geo/v1/)
```

Сгенерённые стабы — `pkg/api/kacho/cloud/vpc/v1/` (руками не править). Импорт:

```go
vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
```
