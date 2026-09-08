# 03 — IPAM Model (kacho-vpc)

Главная нетривиальная фича VPC. **Полностью внутренняя** — собственная
admin-управляемая модель пулов адресов, отдельная от клиентской иерархии.

## Сущности

```mermaid
erDiagram
  ADDRESS_POOLS ||--o{ ADDRESS_POOL_NETWORK_DEFAULT : "explicit network bind"

  ADDRESS_POOLS {
    text id PK "apl..."
    text name
    text kind "EXTERNAL_PUBLIC|EXTERNAL_TEST|RESERVED_INTERNAL"
    text zone_id "nullable = зоне-независимый (anycast) пул (TEXT-id домена geo, без FK)"
    text_array v4_cidr_blocks
    text_array v6_cidr_blocks "split-by-family: семьи хранятся раздельно"
    bool is_default
  }
  ADDRESS_POOL_NETWORK_DEFAULT {
    text network_id PK
    text pool_id FK
  }
```

## Две иерархии (важный концепт)

```
КЛИЕНТСКАЯ                            СИСТЕМНАЯ
───────────                            ─────────
Account  ─ kaname                   (no parent)
   └─ Project ◄────────┐               Zone (домен kacho-geo, admin)
        └─ Network     │                   └─ AddressPool (admin, kacho-vpc)
            └─ Subnet  │                        │
                 └─ Address (internal)          │
                                                │
        └─ Address (external) ◄─────────────────┘
            external_ipv4.address_pool_id
```

- **Клиентская** — `kaname` (Account/Project) + публичная VPC API.
- **Системная** — admin-managed. AddressPool не принадлежит клиенту, но
  external IP клиента берется оттуда. Region/Zone — отдельный leaf-домен
  `kacho-geo`; в kacho-vpc `zone_id` хранится как `TEXT`-id без FK.
- **Точка пересечения** — `AddressPool`: external IP клиента аллоцируется из пула,
  выбранного cascade-резолвом (network-default → zone-default → global-default).

## Zone

- Геогрфический leaf-ресурс домена `kacho-geo`, не VPC-ресурс.
- В kacho-vpc `subnet.zone_id` / `address_pool.zone_id` /
  `address.external_ipv4.zone_id` — `TEXT`-id без FK; существование `zone_id`
  валидируется на request-path вызовом `geo.v1.ZoneService.Get`.

## AddressPool

- Глобальный admin-only ресурс. **Нет** `project_id` — pool глобальный.
- `v4_cidr_blocks TEXT[]` / `v6_cidr_blocks TEXT[]` — CIDR-блоки **раздельно по семьям**
  (split-by-family). Колонки `cidr_blocks` не существует; объединять семьи нельзя и по
  существу — резолв каждой семьи смотрит только на свой массив (`poolHasFamily`).
  Неперекрытие блоков внутри `kind` держит child-таблица `address_pool_cidrs` + EXCLUDE
  gist (миграция 0004).
- `kind` — `EXTERNAL_PUBLIC | EXTERNAL_TEST | RESERVED_INTERNAL`.
- `zone_id` — `TEXT`-id домена geo, **nullable**. Объявляет, ГДЕ живут префиксы пула:
  непустая зона = **ZONAL**-пул; NULL = **зоне-независимый (REGIONAL/anycast)** пул
  (cascade Step 3). Это разные полосы, а не fallback-цепочка: зональный запрос из
  anycast-пула не обслуживается (см. «Cascade resolve»).
- `is_default` — partial UNIQUE: один `is_default=true` на `(COALESCE(zone_id,''), kind)`.
- `selector_labels JSONB`, `selector_priority INT` — зарезервированы (в текущем cascade не участвуют).
- `addresses_external_pool_ip_uniq` — partial UNIQUE на `(address_pool_id, address)` в `addresses` — гарантия что один IP не выделится дважды.

## Binding

`address_pool_network_default(network_id PK, pool_id)`:
- Cascade Step 1 (network-default).
- API: `BindAsNetworkDefault / UnbindNetworkDefault`.

## Cascade resolve

Используется в `AddressAllocator.AllocateExternalIP`.
Вход: `address_id`. Выход: `pool` (или `FailedPrecondition`). Family-aware, и —
**две взаимоисключающие полосы по placement запроса** (data-integrity.md
§Placement-coherence): после общего Step 1 (network-binding) полоса выбирается по зоне
адреса — задана → зональная (Step 2); пуста → anycast (Step 3). Зональный запрос **не** проваливается в зоне-независимый
пул: выкроить «адрес зоны A» из anycast-префикса — placement-lie (адрес объявляет
зону, которой у его префикса нет, и не защищён её failure-domain'ом). Симметрично,
anycast-запрос зональные шаги пропускает by construction (зоны нет — сравнивать не с чем).

Следствие для эксплуатации: зоне-независимый default — **cluster-wide singleton**
(partial UNIQUE `(COALESCE(zone_id,''), kind) WHERE is_default`). Пока он подрабатывал
catch-all fallback'ом, завести его было нельзя, не изменив молча ответ каждой зоны,
которая намеренно не обслуживает какое-то семейство. Потребитель anycast-полосы —
VIP REGIONAL-балансировщика (`nlb`, EXTERNAL всегда REGIONAL).

```mermaid
flowchart TD
  Start([AllocateExternalIP address_id]) --> Fetch[Get Address →<br/>zone_id, kind, family<br/>+ network_id для external]
  Fetch --> S1

  S1[Step 1<br/>network_default WHERE network_id=$nid] -->|hit| Return1[matched_via:<br/>network_default]
  S1 -->|miss| Lane{zone_id задан?}

  Lane -->|да — зональная полоса| S2
  Lane -->|нет — anycast-полоса| S3

  S2[Step 2<br/>GetDefaultForZone WHERE zone_id=$zid<br/>AND kind=$kind AND is_default] -->|hit| Return2[matched_via:<br/>zone_default]
  S2 -->|miss| Fail

  S3[Step 3<br/>GetDefaultForZone WHERE zone_id IS NULL<br/>AND kind=$kind AND is_default] -->|hit| Return3[matched_via:<br/>global_default]
  S3 -->|miss| Fail[FailedPrecondition<br/>'no address pool resolved']
```

На каждом шаге pool пропускается, если его CIDR-список для запрошенного family пуст
(`poolHasFamily`): v4-резолв берет pool только с непустым `v4_cidr_blocks`, симметрично для v6.

## IP picker — книга учёта, а не перебор по CIDR

Внешний IPv4 выделяется **не** случайным подбором с повтором, а popом из книги учёта
свободных адресов пула (`address_pool_free_ips`). Один SQL-стейтмент на попытку —
`SELECT … LIMIT 1 FOR UPDATE SKIP LOCKED` → `DELETE FROM address_pool_free_ips` →
`UPDATE addresses` с target-guard (`internal/repo/helpers/freelist_sql.go`,
`AllocateUseCase.AllocateExternalIP` в `internal/apps/kacho/api/address/allocate.go`).
Contention нулевая by construction: два конкурента не могут забрать одну строку.
Ключ книги — **host-форма** адреса, без маски (миграция 0023: до неё один адрес мог
лежать в двух разных ключах).

Внешний IPv6 — зеркало для v6, но sparse counter-based (`AllocateExternalIPv6`):
материализовать книгу на /64 нереально.

Случайный подбор остался там, где книги нет — internal-адрес в подсети
(`domain.PickRandomIPv4` / `PickRandomIPv6`, см. ниже). Там же осмысленна и
повторная попытка на конфликте уникальности: `isUniqueViolation`
(`internal/apps/kacho/api/address/helpers.go`) распознаёт **обе** формы —
сырую pg-ошибку и уже обёрнутый sentinel «уже существует» через `errors.Is`.
Без второй ветки петля повтора рвалась и наружу шёл сырой «already exists» вместо
`ResourceExhausted`.

> Прежняя редакция описывала здесь перебор случайного адреса по перечню CIDR пула с
> записью через отдельный сеттер спецификации адреса. Ни одно из трёх имён, которые она
> называла, в дереве не резолвится, и механизм другой: перечисление CIDR заменено книгой
> учёта. Мёртвые имена здесь намеренно не воспроизводятся — в обратных кавычках они
> читаются как живая координата.

## Internal IP allocate (v4 + v6)

`InternalAddressService.AllocateInternalIP` и `AllocateInternalIPv6` — internal-only, вызываются
in-process из `AddressService.doCreate` при `internal_ipv4_address_spec` / `internal_ipv6_address_spec`
(а также admin-tooling). Pool'а нет — IP берется из CIDR-блоков самой подсети:

- **v4** — random-pick + retry в `subnet.v4_cidr_blocks` (двухфазный sweep, см. ниже); conflict-target
  `addresses_internal_subnet_ip_uniq`. CIDR-less подсеть → `FailedPrecondition "subnet ... has no IPv4 CIDR"`
  (то же — explicit `internal_ipv4_address_spec.address` в CIDR-less подсеть).
- **v6** — `AllocateInternalIPv6`: random-pick + retry в `subnet.v6_cidr_blocks`;
  conflict-target `addresses_internal_subnet_ipv6_uniq` (partial UNIQUE на `(subnet_id, address)` из `internal_ipv6`).

`ListAddressesRequest.subnet_id` — фильтр; матчит `internal_ipv4->>'subnet_id'` **ИЛИ**
`internal_ipv6->>'subnet_id'` (т.е. возвращает оба семейства внутренних адресов подсети).

И v4-, и v6-внутренний адрес блокирует удаление своей подсети — sync-precheck `AddressesBySubnet`
(смотрит обе jsonb-колонки) + DB-backstop `addresses_internal_subnet_fkey` на generated-колонке
`addresses.internal_subnet_id` (выводится из `internal_ipv4` ИЛИ `internal_ipv6`).

## Utilization (admin observability)

`InternalAddressPoolService.GetUtilization(pool_id)`:

```json
{
  "poolId": "apl...",
  "totalIps": "510",
  "usedIps": "127",
  "freeIps": "383",
  "usedPercent": 24,
  "cidrs": [
    {"cidr":"198.51.100.0/24", "total":254, "used":120},
    {"cidr":"203.0.113.0/24",  "total":254, "used":7}
  ]
}
```

- `total` per CIDR = `2^(32-bits) - 2` (исключая network/broadcast). Для /31 = 2 (RFC 3021), /32 = 1.
- `used` per CIDR — Postgres `address::inet << cidr` подсчет.

REST: `GET /vpc/v1/addressPools/{pool_id}/utilization` (через apiGW, на cluster-internal listener).

## Управление (через api-gateway internal mux — нет CLI)

Отдельного `kachoctl-ipam` CLI **нет** (удален) — все admin-операции делаются
REST-запросами на cluster-internal listener api-gateway (локально — port-forward
на `localhost:18080`) либо из web-UI. Эти пути не публикуются на external TLS endpoint.

```bash
BASE=http://localhost:18080   # port-forward api-gateway

# Region / Zone — домен kacho-geo (не kacho-vpc): /geo/v1/{regions,zones}

# AddressPool (глобальный — без project_id) — InternalAddressPoolService.Create
curl -XPOST $BASE/vpc/v1/addressPools -d \
  '{"name":"default-zone-a","kind":"EXTERNAL_PUBLIC","zoneId":"zone-a","cidrBlocks":["198.51.100.0/24"],"isDefault":true}'

# Привязка пула к сети (cascade Step 1) — InternalAddressPoolService.BindAsNetworkDefault
curl -XPOST $BASE/vpc/v1/networks/net.../addressPoolBinding -d '{"poolId":"apl..."}'

# Observability — InternalAddressPoolService.{ListAddresses,GetUtilization}
curl "$BASE/vpc/v1/addressPools/apl.../utilization"
```

## Ошибки

| Ситуация | gRPC code | Текст |
|---|---|---|
| Все 3 шага не дали pool | `FailedPrecondition` | `"no address pool resolved for address X (network Y)"` |
| Pool найден, но все CIDR исчерпаны | `ResourceExhausted` | `"address pool X exhausted (no free IP in any cidr_block)"` |
| `zone_id` не существует (peer-валидация через `geo.v1.ZoneService.Get`) | `FailedPrecondition` | (общая обертка от mapPoolErr) |

Подробно про ошибки — в [`06-conventions.md`](06-conventions.md).
