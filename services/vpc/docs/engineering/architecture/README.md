# kacho-vpc — Architecture

Архитектурная документация по VPC-сервису.

> **Итоговый самодостаточный документ** — [`../ARCHITECTURE.md`](../ARCHITECTURE.md).
> Документы ниже — детализация по конкретным темам.

## Содержание

| # | Документ | О чем |
|---|---|---|
| 00 | [Overview](00-overview.md) | Что делает VPC, какие ресурсы owns, его место в общей системе |
| 01 | [Resources](01-resources.md) | Детально по каждому ресурсу: Network (супернет + системная RT), Subnet (дискриминатор размещения + якорь CIDR), Address (v4/v6), NetworkInterface, RouteTable, SecurityGroup, Gateway, AddressPool |
| 02 | [Data Flows](02-data-flows.md) | Sequence-диаграммы VPC-сценариев: Network create + default-SG, Address allocate cascade, Internal alloc (v4/v6), outbox-журнал + polling-наблюдение, Cloud-selector set, NIC create/attach/detach, delete-blocking chain |
| 03 | [IPAM Model](03-ipam.md) | Pool + cascade resolve + internal v4/v6 allocate + utilization (Region/Zone — домен kacho-geo) |
| 04 | [API Surface](04-api-surface.md) | Все RPC: **7** публичных доменных сервисов и **4** internal (оба числа выводятся из `proto/kacho/cloud/vpc/v1/`), REST endpoints, вёрстка путей |
| 05 | [Database](05-database.md) | Схема `kacho_vpc`, все миграции с их предметом, ключевые constraints (EXCLUDE для CIDR — включая child-таблицу всех блоков, partial UNIQUE, generated col, JSONB GIN) |
| 06 | [Conventions & Gotchas](06-conventions.md) | VPC-specific правила, error mapping, уроки из истории фиксов |
| 07 | [Намеренные дизайн-решения](07-known-divergences.md) | Осознанные поведенческие решения, которые могут удивить ревьюера (не баги; баги/задачи — в GitHub Issues монорепо) |
| 08 | [Payload регистрации ресурса](08-rsab-register-payload.md) | Что несёт intent регистрации owner-tuple и почему mirror-feed обязателен |
| 09 | [Принципы Go-стиля](09-go-skills-applied.md) | Инженерные принципы Go-кода и их выражение в репозитории |
| 10 | [Долг соответствия исполнителя](10-executor-conformance-debt.md) | **Открытый долг, а не механизм**: три величины одного интерфейса опубликованы, но контур ни одну не наблюдает и не сверяет при старте; что делает каждая сторона и предикат снятия по каждой величине |
| 11 | [Квоты на число ресурсов](11-resource-count-quotas.md) | **Открытый долг, а не механизм**: потолка на число ресурсов арендатора нет; семь счётчиков, форма учёта и отказа, владелец политики (домен квот) и предикат снятия долга |
| — | [ER-диаграмма](er-diagram.md) | Схема `kacho_vpc` целиком: таблицы, связи, constraint'ы |
| — | [Аудит внутрисервисных ссылок](within-service-refs-audit.md) | Проверка: каждая ссылка и инвариант закрыты DB-уровнем, а не software-check'ом |

## TL;DR — что это за сервис

Один из доменных сервисов Kachō (владелец Account/Project — `kaname`).
Owns два слоя:

- **VPC ресурсы** (7): Network, Subnet, Address (v4/v6),
  `NetworkInterface` (first-class NIC), RouteTable, SecurityGroup,
  Gateway. Public API на gRPC `:9090`, через край → REST
  `/vpc/v1/...`. Project-scoped (ссылка на kaname.Project). Admin-операции
  (default-SG setter, IPAM, привязка NIC↔Instance) — через `Internal*` на `:9091`.
- **IPAM (kacho-only, admin)**: AddressPool + network-default binding.
  Internal-only API на gRPC `:9091`. Глобальные ресурсы — не привязаны к
  аккаунту или проекту. Управляются админом через internal-mux края.
  (Region/Zone — домен `kacho-geo`.)

Cascade IP-allocate работает inline в worker'е создания адреса
(`internal/apps/kacho/api/address/create.go`); тот же выбор пула переиспользуется
internal-RPC аллокации (`allocate.go`) через общий `alloc_shared.go`.

## Связь с другими репо

```
       ┌──────────────────────────────────┐
       │   край (gateway/ монорепо)       │
       └─────┬──────────────────┬─────────┘
             │ public :9090     │ admin internal :9091
             ▼                  ▼
       ┌──────────────────────────────────┐
       │           kacho-vpc              │
       │  ┌──────────────────┐            │
       │  │  service layer   │            │
       │  └─┬────────┬───────┘            │
       │    │        │ ProjectClient      │
       │    │        └──→ kaname        │
       │    │             (gRPC)           │
       │    │             ProjectService.Get
       │    │             project_id → account_id
       │    │                              │
       │    ▼                              │
       │  ┌──────────────────┐            │
       │  │  pg-vpc (own DB) │            │
       │  └──────────────────┘            │
       └──────────────────────────────────┘
```

Внешние зависимости:
- `kaname` — `ProjectService.Get` (existence check владельца-проекта),
  `InternalIAMService.Check` (per-RPC authz-gate на **обоих** листенерах),
  `RegisterResource`/`UnregisterResource` (owner-tuple в модель прав).
- `kacho-geo` — `ZoneService.Get` / `RegionService.Get`: существование
  `zone_id`/`region_id` на request-path, fail-closed.
- `pkg/` монорепо — `ids`, `operations`, `db`, `grpcsrv`, `outbox`, `authz`,
  `listnarrow`, `servicehost`, … (прежде отдельный репозиторий `kacho-corelib`).
- `proto/` + сгенерённые стабы `pkg/api/...` (прежде `kacho-proto`).

VPC **не знает** про:
- api-gateway (просто слушает 9090/9091).
- UI/TUI/CLI (это REST/gRPC потребители).
- compute/loadbalancer (общение только по API).

См. [`02-data-flows.md`](02-data-flows.md#cross-service-project-cloud-id-lookup).
