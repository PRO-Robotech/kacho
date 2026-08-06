# kacho-compute — Architecture

> [!warning] Главы ниже описывают ресурсную модель ШИРЕ той, что сервис несёт сегодня
> Замер на 2026-08-06, предикаты названы, чтобы их можно было повторить:
> `grep -h '^service ' proto/kacho/cloud/compute/v1/*.proto` даёт **четыре** сервиса —
> инстанс, тип машины и два внутренних (каталог типов и поток изменений);
> `grep -hE '^message (Disk|Image|Snapshot|DiskType|Region|Zone)\b' proto/kacho/cloud/compute/v1/*.proto`
> даёт **ноль**. Блочное хранение принадлежит `services/storage/`, ось размещения —
> `services/geo/`; карта владельцев — правило целостности данных в воркспейсе.
>
> При этом главы всё ещё пишут об этих ресурсах как о своих. Замер на 2026-08-06,
> из каталога `services/compute/docs/architecture/`, предикат целиком, чтобы его
> можно было повторить, а не поверить:
>
> ```
> grep -rhcE '\b(Disk|Image|Snapshot|DiskType|Region|Zone)s?\b' \
>      --include='*.md' --exclude=README.md . | paste -sd+ | bc
> ```
>
> **155 строк в 12 главах из 12** — то есть в каждой главе каталога. Индекс
> (`README.md`, этот файл) из счёта исключён намеренно: он не глава, и без
> исключения предикат считал бы сам себя — первая же правка этой врезки сдвигала
> бы число, которое врезка называет.
>
> Прежняя редакция называла здесь другое число и отсылала за предикатом «в историю
> правки». Повторить по такой отсылке нельзя: истории у читателя может не быть, а
> у числа без предиката нет способа состариться заметно. Приведение глав к дереву —
> отдельная работа со своей приёмкой; до неё **предметом главы считай proto и код,
> а не главу**. Оговорка стоит в индексе потому, что индекс читают первым, и
> молчание в нём означало бы, что расхождения нет.
>
> Что уже приведено к дереву: координаты кода (слои, клиенты, маппер ошибок, резолв типа
> машины), ссылки на соседний сервис и на контракты. Что нет: перечни ресурсов и разделы
> про них — они не «устарели в имени», у них нет предмета.

Архитектурная документация именно по Compute-сервису. Workspace-уровень (как
он связан с другими сервисами, общий стек, общие правила) живёт в **другом
репозитории** — `PRO-Robotech/kacho-workspace`: его корневой индекс и книга
спецификаций. Пути отсюда не даются: репозитории разные, и относительная ссылка
не резолвилась бы ни в одном клоне.

> **Итоговый самодостаточный документ** — [`../ARCHITECTURE.md`](../ARCHITECTURE.md).
> Документы ниже — детализация по конкретным темам.
>
> Происхождение сервиса: написан заново на проверенных паттернах `kacho-vpc`
> (flat resources + Operations LRO + Clean Architecture + собственный
> опубликованный контракт Kachō).
> Где видишь «как в VPC» — буквально смотри одноимённый файл в `../../../vpc/`.

## Содержание

| # | Документ | О чём |
|---|---|---|
| 00 | [Overview](00-overview.md) | Что делает kacho-compute, какие ресурсы owns, что вне скоупа, 6 принципов проекта, Clean Architecture, цель — соответствие опубликованному контракту Kachō |
| 01 | [Resources](01-resources.md) | Детально по каждому ресурсу: Disk, Image, Snapshot, Instance (+`nic_id`→kacho-vpc NIC), DiskType, Region, Zone (Geography, owner kacho-compute) — proto-поля, ID-префиксы, status-enum'ы, полный список RPC с пометкой implemented/blocked/Unimplemented, инварианты, cross-resource links |
| 02 | [Data Flows](02-data-flows.md) | Sequence-диаграммы compute-сценариев: Operations LRO worker, Disk.Create, Image.Create (source oneof), Snapshot.Create, Instance.Create (boot-disk validation, без авто-NIC — KAC-266), AttachDisk/DetachDisk, outbox + LISTEN/NOTIFY + InternalWatchService |
| 03 | [Instance Lifecycle](03-instance-lifecycle.md) | State-машина `Instance.Status` (PROVISIONING/RUNNING/STOPPING/STOPPED/STARTING/RESTARTING/UPDATING/ERROR/CRASHED/DELETING), transition-таблица (RPC × precondition × end-status × Operation.response), control-plane имитация, AttachDisk/DetachDisk/NAT инварианты |
| 04 | [API Surface](04-api-surface.md) | Таблица всех публичных RPC (REST path, method, request/response, Operation metadata/response, sync-vs-async, implemented/blocked) + internal RPC (InternalWatchService / InternalDiskTypeService / InternalRegionService / InternalZoneService на :9091) |
| 05 | [Database](05-database.md) | Схема `kacho_compute`, миграции (`0003_geography_owner.sql` — regions+zones owned by compute; `0005_instance_nic_id.sql` — `instance_network_interfaces.nic_id`): все таблицы, колонки, индексы, FK, partial UNIQUE, outbox trigger, seed, flat-схема, xmin OCC |
| 06 | [Conventions & Gotchas](06-conventions.md) | Compute-specific правила: naming, error mapping, timestamp truncation, UpdateMask discipline, pagination, filter, hard-delete, Operation prefix `epd`, cross-service ref-validation, `nic_id`-on-Instance, Geography owner=compute, `KACHO_COMPUTE_SKIP_PEER_VALIDATION` |
| 07 | [Намеренные решения / отступления от конвенций](07-known-divergences.md) | Реестр осознанных by-design решений (НЕ баги) — id-syntax validation, name-policy probe, Instance precondition тексты, control-plane имитация, DiskType/Region/Zone admin-CRUD, Geography owner=kacho-compute (KAC-15), Instance NIC ↔ kacho-vpc NetworkInterface (KAC-9), blocked-on-missing-service |
| 08 | [UI](08-ui.md) | Интеграция с `kacho-ui` (Vite + React SPA): compute-views (Instances/Disks/Images/Snapshots), generic CRUD-страницы, polling Operation, attach/detach disk, Start/Stop/Restart actions, DiskType/Zone dropdowns — forward-looking design |
| 09 | [Go skills applied](09-go-skills-applied.md) | Какие практики `golang-*` скилов применены: clean architecture / DI, error handling, context propagation, graceful shutdown, slog, testing pyramid, naming, grpc, pgx-database |

## TL;DR — что это за сервис

Sub-phase 0.4 продукта Kachō. gRPC-сервис управления вычислительными ресурсами:
**Instance, Disk, Image, Snapshot** + read-only справочники **DiskType, Zone**.
Цель — стабильный замороженный публичный контракт продукта в пакете
`kacho.cloud.compute.v1`: proto-форма, error texts, status codes,
timestamp precision, regex'ы, behavioural semantics, state-машина Instance.

Owns:
- 4 мутируемых project-level ресурса: Disk, Image, Snapshot, Instance (NIC бэкуется
  kacho-vpc `NetworkInterface` через `nic_id` — эпик `KAC-9`).
- read-only справочники: DiskType; **Region, Zone** (Geography — owner kacho-compute,
  эпик `KAC-15`: перенесено из kacho-vpc, нет proxy / `skipPeer`-fallback).
- `operations` таблица (per-сервисная, prefix `epd`).
- in-process outbox + LISTEN/NOTIFY → `InternalWatchService`.
- `InternalDiskTypeService` / `InternalRegionService` / `InternalZoneService` — kacho-only
  admin CRUD справочников.

Control plane only: реального data-plane нет — `Instance.status` переходит
детерминированной state-машиной внутри worker'а соответствующей операции;
disk data не существует; serial-port output синтетический; image download
(uri-source) мгновенный.

## Связь с другими репо

```
       kacho-ui (SPA, REST/JSON)
              |
              v
       kacho-api-gateway
       /      |          \
      v       v public    v internal :9091
   kacho-iam vpc       kacho-compute
             :9090     ┌─────────────────┐
   (Account/ (subnet/  │  service layer  │
    Project) SG/addr)  └─┬───────┬───────┘
        ^         ^      │       │ projectClient
        └─────────┼──────┘       └──→ kacho-iam.ProjectService.Get
                  │ vpcClient (Subnet/SecurityGroup/Address .Get)
                  v
            pg-compute (своя БД kacho_compute)
```

Внешние зависимости:
- `kacho-iam.ProjectService.Get` (`projectClient.Exists`) — existence-check
  владельца-проекта в Create; колонка-владелец в схеме — `project_id`.
- `kacho-vpc.{SubnetService.Get, SecurityGroupService.Get, AddressService.Get,
  NetworkInterfaceService.*}` — IPAM эфемерных Address + delete kacho-vpc
  `NetworkInterface` при `Instance.Delete`. ⚠️ авто-создание/привязка NIC при
  `Instance.Create` удалены в `KAC-266` (инстанс создаётся без сетевых
  интерфейсов; правильная сетевая модель — будущая переделка).
- `kacho-corelib` — `ids`, `operations`, `db`, `grpcsrv`, `grpcclient`, `outbox`,
  `validate`, `filter`, `retry`, `shutdown`, `observability`, `config`, `errors`.
- `kacho-proto` — все `.proto`, generated stubs (`gen/go/kacho/cloud/compute/v1`).

kacho-compute **не знает** про:
- api-gateway (просто слушает 9090/9091).
- UI/CLI (это REST/gRPC потребители).
- другие kacho-* (DB-per-service, общение только по API).

## Ссылки в репо

- GitHub Issues — репозиторий `PRO-Robotech/kacho` (долги, баги, planned). Отдельного репозитория сервиса не существует с переходом на монорепо.
- [07-known-divergences.md](07-known-divergences.md) — реестр by-design отступлений от конвенций Kachō.
- `../../tests/newman/` — e2e regression suite (`cases/*.py` → `scripts/gen.py` → Postman-коллекции).
- Эталон-сервис (паттерны) — `../../../vpc/` (буквально смотри одноимённые файлы).
- Proto — `../../../../proto/kacho/cloud/compute/v1/` (единственный дом контрактов в монорепо).
