# 06 — Conventions & Gotchas

VPC-specific правила, error mapping, уроки из истории фиксов.
Общие конвенции API Kachō — `01-resources.md` и `04-api-surface.md`.

## Validation layering

**Sync** (до создания Operation):
- Required: `project_id`, `network_id` (для дочерних, включая SecurityGroup), `name`
  (где обязательно), `placement_type` + соответствующий ему `zone_id`/`region_id`.
- Format:
  - `corevalidate.NameVPC` — permissive (`^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$`, разрешает empty/uppercase/underscore).
  - `Description` ≤ 256.
  - `Labels` ≤ 64 пар, key regex.
  - `ZoneId` — required-only в `pkg/validate`.
    Existence-проверка `zone_id`/`region_id` — sync, через порты `ZoneRegistry` /
    `RegionRegistry` (вызовы `geo.v1.ZoneService.Get` / `RegionService.Get` в `kacho-geo`);
    неизвестная зона → `FailedPrecondition` + машинный признак `PEER_RESOURCE_MISSING`
    (полоса peer-validate).
- CIDR: host-bits=0 (`netip.Prefix.Masked == prefix`) + подмножество супернета сети
  (`validateSubnetWithinSupernet`, `internal/apps/kacho/api/subnet/helpers.go`).
- UpdateMask: known-set + immutable check.
- DeletionProtection.
- Address spec: oneof external/internal — exactly one.

> Здесь стояла отдельная строка про проверку DHCP-опций (`domain_name` по RFC 1123,
> списки DNS/NTP). Проверять больше нечего: поле снято с контракта `Subnet` целиком —
> номера и имя зарезервированы (VPC-1-43), ни `Create`, ни `Update` его не принимают.

**Async** (внутри Operation worker):
- Project existence через `projectClient.Exists` → `NotFound`.
- Network/Subnet existence для дочерних → `NotFound`.
- Repo Insert/Update — FK violations, EXCLUDE constraint (CIDR overlap),
  UNIQUE violation (name within project, IP collision).
- Все маппятся в gRPC-status единственной трансляцией — `serviceerr.MapRepoErr`
  (см. ниже).

## Error mapping (sentinel → grpc)

Трансляция sentinel → gRPC-status живёт **в одном месте** —
`internal/apps/kacho/shared/serviceerr` (`MapRepoErr`, плюс `MapRepoErrLeakSafe` для
путей, где нужен фиксированный текст вместо чужой ошибки).

> Прежняя редакция объявляла восемь копий — по одной на ресурс. Предикат
> `git grep -l 'func mapRepoErr' -- 'services/vpc/**/*.go'` даёт **пусто**: такой функции
> в дереве нет ни одной, имя `mapRepoErr` встречается только в комментариях, объясняющих
> маршрут ошибки. Это ровно тот случай, когда механизм назван комментарием и потому
> считается существующим, — проверять надо выражением, а не упоминанием.

| Sentinel | gRPC code | Текст сообщения |
|---|---|---|
| `ErrNotFound` | `NOT_FOUND` | `"<Resource> {X} not found"` |
| `ErrAlreadyExists` | `ALREADY_EXISTS` | `"<resource> with name ... exists"` |
| `ErrFailedPrecondition` | `FAILED_PRECONDITION` | varies |
| `ErrInvalidArg` | `INVALID_ARGUMENT` | varies |
| `ErrInternal` | `INTERNAL` | `"internal database error"` (no leak) |

Specific:
- CIDR overlap (PG `23P01` от EXCLUDE) → `FailedPrecondition` `"Subnet CIDRs can not overlap"`.
- Malformed / нераспознанный resource-id (нет известного 3-char prefix `net/sub/adr/rtb/sgr/gtw/nic/apl/enp`) → sync `InvalidArgument "invalid <res> id '<X>'"` (`corevalidate.ResourceID`, вызывается первым стейтментом в каждом id-берущем RPC). Well-formed-но-несуществующий id (известный prefix) → `NotFound` через `repo.Get`. Семантика family-agnostic: `enp...`, переданный как Operation-id, проходит prefix-check → затем `repo.Get` → `NotFound`.
- Duplicate name (UNIQUE `23505`) → `ALREADY_EXISTS`.
- `addresses_external_pool_ip_uniq` violation → распознаётся `isUniqueViolation`
  (`internal/apps/kacho/api/address/helpers.go`), аллокатор ловит её и повторяет попытку.
  Прежняя редакция называла здесь отдельный «повторяемый internal»-sentinel — имени с
  таким смыслом в дереве нет, и оно тут не воспроизводится: в обратных кавычках оно
  читалось бы как живая координата.
- Dependency-chain `FailedPrecondition` (sync-prechecks): `Address.Delete` used-адреса → `"address ... is in use by network interface ...; detach it before deleting the address"`; `Subnet.Delete` с внутренними адресами (v4/v6) → `"Subnet has allocated internal addresses"`, с NIC'ами → `"subnet ... has N network interface(s) (...); delete them first"`; `Network.Delete` непустой → `"Network ... is not empty"`; CIDR-less подсеть при internal-v4-allocate → `"subnet ... has no IPv4 CIDR"`.

## Hard delete

`DELETE FROM <table> WHERE id = $1`. Никаких `deletion_timestamp` для tombstones.

## Flat schemas (без K8s envelope)

Все VPC-таблицы — flat: только domain-specific колонки + id/project_id/name/description/labels/created_at. **Нет** `resource_version`, `generation`, `deletion_timestamp`, `finalizers`, `spec`, `status` (как jsonb).

## Optimistic concurrency

Без отдельной колонки. Используем Postgres `xmin::text`:

```sql
SELECT field, xmin::text FROM t WHERE id = $1;
UPDATE t SET field = $2 WHERE id = $1 AND xmin::text = $3 RETURNING ...;
```

Zero-overhead, миграция не нужна.

## ID format

| Resource | Prefix | Where |
|---|---|---|
| Network | `net` | `ids.PrefixNetwork` |
| Subnet | `sub` | `ids.PrefixSubnet` |
| Address | `adr` | `ids.PrefixAddress` |
| RouteTable | `rtb` | `ids.PrefixRouteTable` |
| SecurityGroup | `sgr` | `ids.PrefixSecurityGroup` |
| Gateway | `gtw` | `ids.PrefixGateway` |
| NetworkInterface | `nic` | `ids.PrefixNetworkInterface` |
| AddressPool | `apl` | `ids.PrefixAddressPool` |
| Operation (VPC) | `enp` | `ids.PrefixOperationVPC` |

3-char prefix + 17-char crockford-base32; тип ресурса читается по prefix.
Operation несет **отдельный** prefix `enp` (`ids.PrefixOperationVPC`): api-gateway
маршрутизирует `OperationService.Get(id)` по первым 3 символам, и все VPC-операции
должны идти в один backend. Все ID — `TEXT`.

## Subnet — размещение и CIDR

`network_id`, `placement_type`, `zone_id`/`region_id` — **hard-immutable** в UpdateMask →
`InvalidArgument "<field> is immutable after Subnet.Create"`.

CIDR на Create задаётся **якорем**: `ipv4_cidr_primary` / `ipv6_cidr_primary` (по одному на
семью, immutable). Дополнительные диапазоны — только через `:add-cidr-blocks` /
`:remove-cidr-blocks` (поля `ipv4_cidr_blocks` / `ipv6_cidr_blocks`). В `UpdateSubnetRequest`
CIDR-полей **нет** — их номера зарезервированы, поэтому «принять и no-op» здесь невозможно
by construction.

Подсеть может быть одной семьи: пустой v4-якорь легален, и тогда internal-v4-аллокация в неё
даёт `FailedPrecondition "subnet %s has no IPv4 CIDR"`.

Каждый блок обязан лежать внутри объявленного супернета сети и не пересекаться с блоками
других подсетей той же сети — второе держит `EXCLUDE USING gist` на child-таблице
`subnet_cidr_blocks` (миграция 0010), покрывая **все** блоки, а не только якорь.

## NetworkInterface ↔ Address referrer-convention

NIC ссылается на `Address`-ресурсы **по id** (`v4_address_ids[]`/`v6_address_ids[]`); один `Address`
может быть привязан **максимум к одному NIC** — enforced сервис-слоем через `addresses.used` + referrer-rows
в `address_references` (`referrer_type="network_interface"`, как `compute_instance`). `Address.Delete` для
`used`-адреса → `FailedPrecondition "address ... is in use by network interface ...; detach it before deleting the address"`.
NIC `used_by` (кто использует NIC) — денормализованное зеркало `Address.used_by`; владелец ставится
атомарным CAS на одной строке (flat-колонки `used_by_type`/`used_by_id`/`used_by_name`). Дерево удаления —
снизу вверх: NIC → Address → Subnet → Network,
все FK RESTRICT (`network_interfaces_subnet_id_fkey`).

## ListOperations переживает удаление ресурса (Network/Subnet/Address/NetworkInterface)

`ListOperations` для этих четырех ресурсов **не требует существования ресурса** — precondition `repo.Get`
убран из сервиса и из хэндлера. Handler best-effort: жив → проверка project-ownership; `NotFound` → пропуск,
отдаем накопленные операции; прочие ошибки пробрасываются. `operations`-строки без FK-каскада — история сохраняется.
(route_table/SG/gateway `ListOperations` по-прежнему гейтит на `repo.Get` — это существующее поведение.)

## Default Security Group (inline, опционально)

Управляется флагом `KACHO_VPC_DEFAULT_SG_INLINE` (default `true`).

При `true` — Network.Create:
1. SYNC создается Operation, возвращается клиенту.
2. ASYNC в worker:
   - `repo.Insert(network)`.
   - **Inline создается SG** `default-sg-{first-8-chars-of-net-id}` с правилами по умолчанию.
   - `UPDATE networks SET default_security_group_id = sg.id`.
3. Outbox emit для всех трех событий (Network.CREATED, SecurityGroup.CREATED, Network.UPDATED).

При `false` — Network.Create НЕ создает SG (композиционный корень передаёт `false` в
`NewCreateNetworkUseCase`), `default_security_group_id` остается пустым; создание
делегируется внешнему reconciler'у.
Убирает 2 INSERT + 1 UPDATE из hot-path (≈ +30-40% write-throughput) — для load-тестов.
В таком режиме newman-кейсы `*-LSG-CRUD-DEFAULT-SG` / `*-DEL-STATE-DEFAULT-SG` ожидаемо падают.

При Network.Delete worker сначала удаляет default SG (если есть), потом Network. Не-default SG / subnets / route tables препятствуют удалению (FK RESTRICT + sync-precheck) → клиент получает `FailedPrecondition "Network ... is not empty"`.

## Admin boundary

⚠️ **Внутренние служебные сущности не публиковать наружу:**

- `Internal*Service`'ы могут быть зарегистрированы через api-gateway REST mux на cluster-internal listener — для UI/admin-tooling.
- На external TLS endpoint (`api.kacho.local:443`, advertised для внешних клиентов) эти paths **не должны** быть доступны.
- Список admin paths (для TLS-middleware фильтра):
  - `/vpc/v1/addressPools*`
  - `/vpc/v1/networks/*/addressPoolBinding`
  - (Region/Zone — домен `kacho-geo`, в kacho-vpc их нет)

При добавлении нового admin-RPC обновлять этот список.

**Правило для новых admin-RPC**: добавлять **только** в `Internal*` сервис на `:9091`, регистрировать через блок адреса `VPCInternalAddr` в `gateway/internal/restmux/mux.go`. **НЕ** расширять публичные сервисы для admin-нужд — это засветит admin-функции на TLS endpoint.

## Gotchas (из истории фиксов)

1. **id sync-валидация** — malformed / нераспознанный resource-id (нет известного 3-char prefix `net/sub/adr/rtb/sgr/gtw/nic/apl/enp`) → sync `InvalidArgument "invalid <res> id '<X>'"` (`corevalidate.ResourceID`, первым стейтментом в каждом id-берущем RPC). Well-formed-но-несуществующий id (известный prefix) → `NotFound` через `repo.Get`. Семантика family-agnostic — `enp...`, переданный как subnet-id, проходит prefix-check, затем `repo.Get` → `NotFound`.
2. **NameVPC permissive, не strict** — empty/uppercase/underscore разрешены для Network/Subnet/Address/RouteTable/SG. Gateway — strict (`corevalidate.NameGateway`: lowercase, без uppercase/underscore).
3. **CIDR overlap** = `FailedPrecondition`, не `InvalidArgument`.
4. **CIDR host-bits=0** обязательно, sync через `netip.Prefix.Masked`.
5. **Subnet immutable**: `network_id`, `placement_type`, `zone_id`/`region_id` — reject в mask, silent ignore в full-PATCH. CIDR-полей в `Update` нет вовсе.
6. **Hard-delete, не soft**.
7. **Default SG создается inline в NetworkService.doCreate** при `KACHO_VPC_DEFAULT_SG_INLINE=true` (default). Флаг `=false` отключает inline-SG (для load-тестов / внешнего reconciler'а).
8. **Timestamp truncate to seconds** в proto-ответе (БД хранит микросекунды).
9. **DeletionProtection sync-check** перед Delete — `FailedPrecondition` `"... deletion_protection enabled"`.
10. **page_size валидируется**, garbage page_token → `InvalidArgument`.

## IPAM-specific gotchas

11. **`isUniqueViolation` распознает обе формы**: raw pgErr + уже обёрнутый sentinel «уже существует» через `errors.Is`. Без второй ветки аллокатор вылетал из retry-петли с сырым «already exists» вместо `ResourceExhausted` — то есть исчерпание пула выглядело бы как конфликт имён.
12. **AddressPool.zone_id NULL = глобальный fallback**, не "ошибка". Cascade Step 3 (global-default) ищет `WHERE zone_id IS NULL`.
13. **Cascade family-aware**: на каждом шаге pool пропускается, если его CIDR-список для запрошенного family пуст (`poolHasFamily`). Cascade — 3 шага: network-default → zone-default → global-default.

## Что нельзя делать

- НЕ редактировать примененные миграции — только новые.
- НЕ добавлять admin-нужное в публичный сервис — только в `Internal*`.
- НЕ возвращать ресурс синхронно из мутирующих RPC — все мутации через Operation.
- НЕ делать каскадное удаление через границу сервиса — только same-DB FK.
- НЕ использовать ORM (gorm/ent/bun) — только pgx + handwritten SQL.

## Ссылки в репо

- GitHub Issues монорепо (`github.com/PRO-Robotech/kacho/issues`) — долги, баги, задачи.
- [07-known-divergences.md](07-known-divergences.md) — осознанные дизайн-решения.
- `tests/newman/docs/TAXONOMY.md` — class taxonomy для regression-кейсов.
