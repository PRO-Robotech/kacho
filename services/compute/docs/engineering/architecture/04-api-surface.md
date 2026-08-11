# 04 — API Surface

Полный список RPC compute + соответствующие REST endpoints (из
`google.api.http`-аннотаций в `proto/kacho/cloud/compute/v1/`) и
Operation metadata/response (из `(kacho.cloud.api.operation)` options).

## Сводка

| Категория | Сервисов | RPC (примерно) | Listener | REST exposed |
|---|---:|---:|---|---|
| Public мутируемые | 1 (`InstanceService`) | ~25 | `:9090` public gRPC | ✅ через api-gateway (public mux), на обоих listener'ах |
| Public read-only справочники | 1 (`MachineTypeService`) | 2 | `:9090` public gRPC | ✅ через api-gateway. Geography (Region/Zone) — owner kacho-geo; блочное хранение (Volume/Image/Snapshot/DiskType) — owner kacho-storage, под `/storage/v1` |
| Operations | 1 (`OperationService`) | 2 (`Get`, `Cancel`) | `:9090` public gRPC | ✅ `/operations/{id}` (через api-gateway opsproxy) |
| Internal admin (kacho-only) | 1 (`InternalMachineTypeService`) | 3 | `:9091` internal gRPC | ✅ выборочно (`/compute/v1/internal/machineTypes`) — только cluster-internal listener |
| Outbox stream | 1 (`InternalWatchService`) | 1 (`Watch`) | `:9091` internal gRPC | ❌ только server-to-server |

> ⚠️ REST-пути неоднородны (решено формой proto): top-level camelCase
> (`/compute/v1/machineTypes`, `/compute/v1/instances`), custom-методы с двоеточием
> (`:relocate`, `:serialPortOutput`, `:latestByFamily`, `:listAccessBindings`),
> child-list `.../operations`, action-методы через сегмент пути
> (`/updateMetadata`, `/addOneToOneNat`, `:attachDisk`), `OperationService.Get` —
> `/operations/{id}` (БЕЗ `/compute/v1/`-префикса).
>
> Нормализация — **ломающее изменение уже посаженного контракта** (клиенты и
> сгенерированные коллекции адресуют эти пути), поэтому она делается осознанно и
> отдельным решением, а не попутно. Прежняя редакция запрещала её ссылкой на
> совпадение с чужим API — это не основание: конвенции Kachō свои
> (`api-conventions.md`), и единственный настоящий довод — цена ломки. См.
> [`07-known-divergences.md`](07-known-divergences.md).

## InstanceService (`instance_service.proto`, `:9090`)

| RPC | REST | sync/async | metadata / response | статус |
|---|---|---|---|---|
| `Get` | `GET /compute/v1/instances/{instance_id}?view=` | sync | → `Instance` (FULL включает metadata) | ✅ |
| `List` | `GET /compute/v1/instances?projectId=&...` | sync | → `ListInstancesResponse` (metadata всегда омитится) | ✅ |
| `Create` | `POST /compute/v1/instances` body `*` | async | `CreateInstanceMetadata{instance_id}` / `Instance` | ✅ (`filesystem_specs[]`→`blocked:kacho-filesystem`. ⚠️ **без авто-NIC** — auto-NIC материализация `materializeNICs` удалена в `KAC-266`; инстанс создаётся без сетевых интерфейсов, NIC не создаётся/привязывается на Create; правильная сетевая модель — будущая переделка) |
| `Update` | `PATCH /compute/v1/instances/{instance_id}` body `*` | async | `UpdateInstanceMetadata` / `Instance` | ✅ (`resources_spec`/`platform_id` требуют STOPPED) |
| `Delete` | `DELETE /compute/v1/instances/{instance_id}` | async | `DeleteInstanceMetadata` / `google.protobuf.Empty` | ✅ (для каждого NIC с непустым `nic_id` — delete kacho-vpc `NetworkInterface`) |
| `UpdateMetadata` | `POST /compute/v1/instances/{instance_id}/updateMetadata` body `*` | async | `UpdateInstanceMetadataMetadata` / `Instance` | ✅ |
| `GetSerialPortOutput` | `GET /compute/v1/instances/{instance_id}:serialPortOutput?port=` | **sync** | → `GetInstanceSerialPortOutputResponse{contents}` | ✅ (синтетика, не операция) |
| `Stop` | `POST /compute/v1/instances/{instance_id}:stop` | async | `StopInstanceMetadata` / `google.protobuf.Empty` | ✅ |
| `Start` | `POST /compute/v1/instances/{instance_id}:start` | async | `StartInstanceMetadata` / `Instance` | ✅ |
| `Restart` | `POST /compute/v1/instances/{instance_id}:restart` | async | `RestartInstanceMetadata` / `google.protobuf.Empty` | ✅ |
| `AttachDisk` | `POST /compute/v1/instances/{instance_id}:attachDisk` body `*` | async | `AttachInstanceDiskMetadata{instance_id, disk_id}` / `Instance` | ✅ |
| `DetachDisk` | `POST /compute/v1/instances/{instance_id}:detachDisk` body `*` | async | `DetachInstanceDiskMetadata` / `Instance` | ✅ |
| `AttachNetworkInterface` | `POST /compute/v1/instances/{instance_id}:attachNetworkInterface` body `*` | async | `AttachInstanceNetworkInterfaceMetadata` / `Instance` | ✅ (требует STOPPED) |
| `DetachNetworkInterface` | `POST /compute/v1/instances/{instance_id}:detachNetworkInterface` body `*` | async | `DetachInstanceNetworkInterfaceMetadata` / `Instance` | ✅ (требует STOPPED) |
| `AddOneToOneNat` | `POST /compute/v1/instances/{instance_id}/addOneToOneNat` body `*` | async | `AddInstanceOneToOneNatMetadata` / `Instance` | ✅ |
| `RemoveOneToOneNat` | `POST /compute/v1/instances/{instance_id}/removeOneToOneNat` body `*` | async | `RemoveInstanceOneToOneNatMetadata` / `Instance` | ✅ |
| `UpdateNetworkInterface` | `PATCH /compute/v1/instances/{instance_id}/updateNetworkInterface` body `*` | async | `UpdateInstanceNetworkInterfaceMetadata` / `Instance` | ✅ (OCC через xmin) |
| `ListOperations` | `GET /compute/v1/instances/{instance_id}/operations` | sync | → `ListInstanceOperationsResponse` | ✅ |
| `Relocate` | `POST /compute/v1/instances/{instance_id}:relocate` body `*` | async | `RelocateInstanceMetadata` / `Instance` | 🚫 blocked (cross-zone disk move) |
| `SimulateMaintenanceEvent` | `POST /compute/v1/instances/{instance_id}:simulateMaintenanceEvent` body `*` | async | `SimulateInstanceMaintenanceEventMetadata` / `google.protobuf.Empty` | ⏭️ no-op |
| `ListAccessBindings` / `SetAccessBindings` / `UpdateAccessBindings` | `.../instances/{resource_id}:listAccessBindings` / `:setAccessBindings` / `:updateAccessBindings` | sync / async / async | как у Disk | ⏭️ no-op скелет |

(`GuestStopInstanceMetadata` / `PreemptInstanceMetadata` / `CrashInstanceMetadata`
— metadata-сообщения без RPC; зарезервированы для будущих guest-инициированных
переходов, в Kachō не эмитятся.)

> [!warning] Здесь стояли четыре таблицы оси размещения — ни одного из этих
> адресов край под доменом compute не обслуживает, и владелец другой
> Регион и зона принадлежат сервису geo (эпик KAC-82). В контрактах compute
> ни region-, ни zone-сообщения нет; край под доменом compute обслуживает
> адреса машин, типов машин и внутренней поверхности — и ничего больше.
> Публичное чтение оси размещения живёт под доменом geo и объявлено
> **задокументированным исключением** из проверки прав по проекту
> (аутентификация обязательна, права по проекту сняты): глобальный справочник
> обязан читать каждый арендатор, иначе он не сможет создать ни одного
> размещаемого ресурса. Админский CRUD справочника — на внутреннем листенере
> сервиса-владельца.
>
> Снятые адреса здесь **намеренно не воспроизводятся**: адрес в обратных
> кавычках читается как живое утверждение — и проверкой свежести, и человеком,
> который его скопирует. Сводная таблица в начале документа называет владельцев
> верно уже сейчас; эти четыре таблицы противоречили ей полсотни строк спустя,
> и ложной была та половина, которая подробнее.

## OperationService (`:9090`)

| RPC | REST | sync/async | response | примечание |
|---|---|---|---|---|
| `Get` | `GET /operations/{operation_id}` (БЕЗ `/compute/v1/`) | sync | → `operation.Operation` | api-gateway opsproxy маршрутизирует по первым 3 символам id (`epd...` → kacho-compute). prefix `epd` для всех compute-операций |
| `Cancel` | `POST /operations/{operation_id}:cancel` | sync | → `operation.Operation` | best-effort cancel; в control-plane операции быстрые → обычно уже `done` |

## Internal сервисы (`:9091`, НЕ на external TLS endpoint)

### InternalMachineTypeService (`internal_machine_type_service.proto`) — kacho-only

| RPC | REST (api-gateway internal mux) | response | примечание |
|---|---|---|---|
| `Create` | `POST /compute/v1/internal/machineTypes` | → `MachineType` | admin задаёт `id` явно (PK, immutable) |
| `Update` | `PATCH /compute/v1/internal/machineTypes/{machine_type_id}` | → `MachineType` | |
| `Delete` | `DELETE /compute/v1/internal/machineTypes/{machine_type_id}` | → пусто | блокируется, если тип используется машиной |

> [!warning] Здесь стояли две админ-таблицы оси размещения — предмета у них нет
> Админский CRUD региона и зоны принадлежит сервису geo и живёт на **его**
> внутреннем листенере; у compute внутренняя админ-поверхность одна — типы машин.
> Снятые адреса не воспроизводятся по той же причине, что выше.
>
> Правило границы от этого не меняется и остаётся нормой: admin-RPC, которого нет
> в публичном API ресурса, добавляется **только** в `Internal*`-сервис на `:9091`
> и регистрируется в краевом мультиплексоре под internal-блоком своего домена —
> тогда он попадает на cluster-internal listener и **не** попадает на external TLS
> endpoint, объявленный внешним клиентам (workspace `CLAUDE.md` §запрет 6). См.
> [`06-conventions.md`](06-conventions.md#admin-boundary).

### InternalWatchService (`internal_watch_service.proto`) — server-to-server

| RPC | REST | примечание |
|---|---|---|
| `Watch` | ❌ нет (gRPC server-stream только) | `Watch(WatchRequest{kinds[]?, from_sequence_no})` → `stream Event{sequence_no, resource_kind, resource_id, event_type, payload(Struct), created_at}`. outbox stream через LISTEN/NOTIFY. Не зарегистрирован в api-gateway restmux. См. [`02-data-flows.md`](02-data-flows.md#8-outbox--listennotify--internalwatchservice) |

## Operations (LRO) — общая модель

Все мутации (`Create/Update/Delete/Start/Stop/Restart/Relocate/AttachDisk/
DetachDisk/AddOneToOneNat/RemoveOneToOneNat/UpdateNetworkInterface/UpdateMetadata/
SimulateMaintenanceEvent/Set|UpdateAccessBindings`) возвращают
`operation.Operation`. Клиент полит `OperationService.Get(operation_id)` до
`done=true` (REST: `GET /operations/{id}`, БЕЗ `/compute/v1/`). api-gateway имеет
in-process `opsproxy` — один URL `/operations/{id}` маршрутизируется по 3-char
prefix id на нужный backend (`epd...` → kacho-compute; `enp...`/`e9b...` →
kacho-vpc; `b1g...` → kacho-iam — Account/Project, заменил resource-manager в
KAC-124). `PrefixOperationCompute == PrefixInstance
== "epd"`. Неизвестный prefix → `400 INVALID_ARGUMENT "operation_id has unknown
prefix"` (intentional fail-fast; по конвенции by-lane split
well-formed-но-нерезолвящийся id должен отвечать `404 NotFound`, а `400` полагается
только malformed — сейчас оба схлопнуты, отступление зафиксировано, общий issue в
`kacho-api-gateway`). `response` для Delete/Stop/Restart/
SimulateMaintenanceEvent = `google.protobuf.Empty`; metadata всегда заполнено
(тип `DeleteXxxMetadata` / etc.) и доступно с момента создания операции.

## Где смотреть proto

```
kacho-proto/proto/kacho/cloud/compute/v1/
├── disk.proto / disk_service.proto
├── image.proto / image_service.proto
├── snapshot.proto / snapshot_service.proto
├── instance.proto / instance_service.proto
├── disk_type.proto / disk_type_service.proto
├── region.proto / region_service.proto  Geography (owner kacho-compute, эпик KAC-15)
├── zone.proto / zone_service.proto       Geography (owner kacho-compute, эпик KAC-15)
├── hardware_generation.proto / kek.proto / maintenance.proto / application.proto / package_options.proto
│
├── internal_watch_service.proto         InternalWatchService.Watch (outbox stream)
├── internal_catalog_service.proto       InternalDiskTypeService / InternalRegionService / InternalZoneService (admin CRUD)
│
└── (vendored, реализация отложена) disk_placement_group*.proto, placement_group*.proto,
    host_group*.proto, host_type*.proto, gpu_cluster*.proto, filesystem*.proto,
    snapshot_schedule*.proto, reserved_instance_pool*.proto, maintenance_service.proto
```

Generated stubs: `kacho-proto/gen/go/kacho/cloud/compute/v1/...`. Импорт:

```go
computev1 "github.com/PRO-Robotech/kacho-proto/gen/go/kacho/cloud/compute/v1"
```
