# 05 — Database

`kacho_vpc` (`pg-vpc` StatefulSet в helm umbrella). Database-per-service —
никаких JOIN'ов с чужими БД или внешними источниками. Все объекты (таблицы,
constraint, индексы, триггеры, helper-функции) живут в схеме `kacho_vpc`;
search_path устанавливается через libpq-параметр `options=-c search_path=kacho_vpc,public`.

## Используемые продвинутые Postgres-фичи

| Фича | Где используется | Зачем |
|---|---|---|
| `EXCLUDE USING gist` | `subnets_no_overlap_v4/v6`, `address_pool_cidrs` | CIDR overlap rejection на DB-level (race-free) |
| `inet/cidr` operators (`<<`, `>>=`) | utilization counts | "сколько Address с IP внутри CIDR пула" |
| Partial UNIQUE index | `addresses_external_ip_uniq` WHERE address `<>` `''` | дубль external IP запретить, но empty allocate-pending разрешить |
| Partial UNIQUE index | `<resource>_project_id_name_key` WHERE name `<>` `''` | дубль непустого `name` в project запретить, пустой — разрешить |
| Partial UNIQUE index | `address_pools_zone_kind_default_uniq` WHERE is_default | один is_default=true на (zone, kind) |
| Partial UNIQUE index | `security_groups_one_default_per_network` | один default-SG на сеть |
| Computed column | `subnets.v4_cidr_primary` / `v6_cidr_primary`, `addresses.internal_subnet_id` | для использования в EXCLUDE / UNIQUE / FK |
| `jsonb_path_ops` GIN index | `address_pools_selector_labels_gin` | быстрые `@>` запросы |
| `LISTEN/NOTIFY` | `vpc_outbox_notify_trg`, `fga_register_outbox_notify_trg` | in-cluster канал доменного outbox-журнала (он же журнал подписки, проецируемый наружу краем) + FGA register-drainer |
| `xmin::text` | optimistic locking (SecurityGroup.UpdateRules) | zero-overhead version-check |
| `FOR UPDATE SKIP LOCKED` | IPv4 freelist / IPv6 released-offsets pop | contention-free аллокация из пула |

## Миграции

`internal/migrations/*.sql`, embed.FS (объявлено в `migrations.go`), goose-стиль up/down.
`0001_initial.sql` — консолидированный baseline (вся базовая схема: все таблицы,
constraint inline в `CREATE TABLE`, индексы, EXCLUDE/UNIQUE, generated-колонки, триггеры,
helper-функции). Дальше — обычные инкрементные миграции:

Номер следующей миграции здесь **не называется**: он выводится из дерева
(`git ls-files services/vpc/internal/migrations/*.sql | tail -1`) и устаревает от любой
чужой правки. Прежняя редакция обрывала таблицу на 0009 и объявляла следующей `0010_*`,
тогда как в дереве уже лежало вдвое больше — причём соседний абзац этого же файла ссылался
на 0017 и 0019. Два места об одном предмете в одном документе.

| # | Файл | Что |
|---|---|---|
| 0001 | `0001_initial.sql` | базовая схема — 19 таблиц: `operations`, `networks`, `route_tables`, `subnets`, `addresses`, `address_references`, `security_groups`, `gateways`, `network_interfaces`, `address_pools`, `address_pool_network_default`, `address_pool_free_ips`, `ipv6_pool_cursors`, `ipv6_allocated_ips`, `ipv6_released_offsets`, `vpc_outbox`, `vpc_watch_cursors` (снята миграцией `20260828114800_drop_watch_cursors.sql`, kacho#1148 — позиция подписки принадлежит клиенту, серверных курсоров нет) и две, дропнутые следующей же миграцией (см. 0002); CHECK/FK/UNIQUE/EXCLUDE, generated columns, триггеры, `kacho_labels_valid`. Все id-колонки — `TEXT` |
| 0002 | `0002_drop_override_and_cloud_pool_selector.sql` | DROP `address_pool_address_override` + `cloud_pool_selector` — per-address override RPC и cloud-selector-шаг IPAM cascade упразднены |
| 0003 | `0003_drop_security_group_status.sql` | DROP `security_groups.status` — у SG нет provisioning-lifecycle, статус никем не наблюдался |
| 0004 | `0004_address_pool_cidrs.sql` | нормализованная child-таблица `address_pool_cidrs` + EXCLUDE gist — CIDR пулов не пересекаются per `kind` (declarative, race-free) |
| 0005 | `0005_default_sg_fk_and_unique.sql` | `networks.default_security_group_id` → nullable + FK ON DELETE SET NULL; partial UNIQUE `security_groups_one_default_per_network` |
| 0006 | `0006_fga_register_outbox.sql` | таблица `fga_register_outbox` (transactional-outbox для регистрации owner-tuple в FGA через kaname) + LISTEN/NOTIFY-триггер |
| 0007 | `0007_network_vrf_id.sql` | `networks.vrf_id bigint` — sequence-backed уникальный per-network VRF id (инфра-чувствительное поле data-plane, отдается только через `InternalNetworkService.GetNetwork`) |
| 0008 | `0008_fga_register_outbox_resource_cols.sql` | additive `resource_kind` / `resource_id` на `fga_register_outbox` (нужны reconciler'у для адресации intent по ресурсу) |
| 0009 | `0009_operations_account_id.sql` | additive nullable `operations.account_id` (общий LRO-writer из `pkg/operations` INSERT'ит колонку безусловно) + partial index |
| 0010 | `0010_subnet_cidr_blocks.sql` | child-таблица `subnet_cidr_blocks` + EXCLUDE gist: неперекрытие покрывает **все** блоки подсетей сети, а не только якорь (baseline-EXCLUDE смотрел лишь на `*_cidr_primary`) |
| 0011 | `0011_address_pool_checks.sql` | DB CHECK-parity формы `address_pools` (name/description/kind/selector_priority) — инвариант на DB-уровне, не только в use-case |
| 0012 | `0012_subnet_placement.sql` | `subnets.placement_type` (`ZONAL`\|`REGIONAL`) + `region_id`; CHECK `subnets_placement_type_chk` и `subnets_placement_payload_chk` держат взаимоисключение пары зона/регион |
| 0013 | `0013_address_reference_owned.sql` | `address_references.owned` — владеет ли referrer адресом или только ссылается (lifecycle эфемерного адреса) |
| 0014 | `0014_network_interface_used_by_index.sql` | `network_interfaces.used_by_index` — слот привязки NIC↔Instance (device-index, `eth0`=0) |
| 0015 | `0015_network_supernet.sql` | `networks.ipv4_cidr_blocks` / `ipv6_cidr_blocks` (объявленный супернет) + `default_route_table_id` |
| 0016 | `0016_network_cidr_blocks_cardinality.sql` | потолок кардинальности супернета — наборы тенант-управляемые и аддитивные |
| 0017 | `0017_network_default_route_table.sql` | `default_route_table_id` становится **действующим** источником истины о RT подсети; снят триггер выбора RT на вставке подсети |
| 0018 | `0018_fga_register_outbox_partition_head_idx.sql` | partial-index под partition-head-only CLAIM дренажа: в одной партиции живут и WRITE, и DELETE одного ключа — порядок обязан держаться cross-batch |
| 0019 | `0019_drop_rt_auto_assoc_trigger.sql` | снят второй (последний) DB-механизм выбора RouteTable — двух конкурирующих механизмов быть не должно |
| 0020 | `0020_fga_register_outbox_claim_order_idx.sql` | индекс внешнего упорядоченного скана claim'а; 0018 дала индекс только под коррелированный `NOT EXISTS` |
| 0021 | `0021_fga_register_outbox_autoanalyze.sql` | свежая статистика планировщика на таблице очереди — вторая половина фикса пропускной способности claim'а: без неё 0020 не срабатывает |
| 0022 | `0022_drop_decoy_pending_idx.sql` | DROP `fga_register_outbox_pending_idx` — само его существование позволяло планировщику строить O(backlog)-claim, ради предотвращения которого добавлялись 0018/0020 |
| 0023 | `0023_free_ips_host_form.sql` | ключ книги учёта свободных адресов приводится к host-форме: один адрес мог лежать в двух разных ключах |
| 0024 | `0024_caller_supplied_cardinality.sql` | DB-backstop на потолки наборов, которые задаёт вызывающий и которые накапливаются между вызовами |
| 0025 | `0025_addresses_read_path.sql` | чтения по адресам стоят столько, сколько отдают: страница адресов подсети читалась дизъюнкцией по jsonb |
| 0026 | `0026_addresses_external_v6_global_uniq.sql` | внешний IPv6 глобально уникален, как и внешний IPv4 (ban #10 — инвариант на DB-уровне) |
| 0027 | `0027_security_group_rules_domain.sql` | область значений правила SG (протокол, диапазон портов) — конструкцией базы, а не только синхронной проверкой |
| 0029 | `0029_retired_contract_columns_and_rule_targets.sql` | снятое с контракта пережило свои строки: DROP `subnets.dhcp_options` и `networks.route_distinguisher` (читателей ноль), плюс нормализация правил SG со снятой ветвью цели — `self_security_group` выражается живой ветвью (цель = сама группа), правило без выразимой цели снимается (оно не разрешало ничего и блокировало правку группы). Числа по каждому виду печатаются в NOTICE |

| 0036 | `0036_network_interface_bandwidth_limit.sql` | верхняя граница полосы интерфейса, заданная арендатором: колонка `bandwidth_limit_mbps` (0 = не задано) + CHECK «ноль либо строго выше обещанного продуктом пола». Верхний край промежутка — объявление посадки, поэтому в схеме его нет: он живёт в профиле возможностей исполнителя и проверяется на пути запроса |
| 0037 | `0037_security_group_used_by_indexes.sql` | обратная ссылка группы правил («кем используется») — запросом, а не таблицей: GIN `jsonb_path_ops` на `network_interfaces.security_group_ids` + частичный btree на `networks.default_security_group_id`. Предикат частичного — `IS NOT NULL`, а не сравнение с пустой строкой: из `col = $1` планировщик выводит первое и не выводит второе, поэтому индекс с `<> ''` не применился бы ни разу |

> [!note] Перечень выше НЕ полон и полнотой не притворяется
> Строки 0028 и 0030…0036 в него не внесены — они появились в дереве, а сюда не
> доехали. Свой ряд добавляю, чужие не сочиняю: восстановить их по памяти значило бы
> завести описания, которых никто не сверял. Предикат, по которому расхождение видно
> одной командой: `ls services/vpc/internal/migrations/*.sql | wc -l` против числа
> строк таблицы (`grep -c '^| 00' 05-database.md`).

⚠️ Запреты:
- НЕ редактировать примененную миграцию. Только новая, со следующим свободным номером.

## Ключевые таблицы

### `networks`

```
id                          TEXT PK (net...)
project_id                   TEXT NOT NULL
name                        TEXT NOT NULL
description, labels         TEXT, JSONB
default_security_group_id   TEXT NULL FK→security_groups ON DELETE SET NULL   -- 0005
ipv4_cidr_blocks            TEXT[] NOT NULL DEFAULT '{}'   -- 0015: объявленный супернет
ipv6_cidr_blocks            TEXT[] NOT NULL DEFAULT '{}'   -- 0015; кардинальность — CHECK из 0016
default_route_table_id      TEXT NULL FK→route_tables      -- 0015 объявила, 0017 сделала действующей
vrf_id                      BIGINT UNIQUE NOT NULL    -- 0007; internal-only (InternalNetworkService)
created_at                  TIMESTAMPTZ

networks_project_id_name_key  UNIQUE (project_id, name)                   -- полный (715001)
INDEX project_idx
```

Супернет — **ограничение на подсети**: CIDR подсети обязан быть подмножеством одного из
блоков сети. `default_route_table_id` — единственный источник истины о том, какую RT
наследует подсеть без явного `route_table_id`; конкурирующих DB-механизмов выбора не
осталось (см. 0017 и 0019).

`vrf_id` — инфра-чувствительный per-network идентификатор data-plane: не публикуется на
public-поверхности, отдается только через `InternalNetworkService.GetNetwork`.

> Та же форма — у всех остальных пользовательских ресурсов (`subnets`,
> `route_tables`, `security_groups`, `gateways`, `addresses`,
> `network_interfaces`, `cidr_groups`): UNIQUE на `(project_id, name)` —
> **полный**, дубль имени → `23505` → `ALREADY_EXISTS`. Исключений нет ни одного.
> Частичная форма `WHERE name <> ''` снята миграцией `715001` вместе со своим
> предметом: пустого имени не существует — на создании сервер подставляет имя,
> производное от `id` (задача #715).

### `subnets`

```
id, project_id, network_id (FK)   TEXT NOT NULL, immutable
placement_type                 TEXT NOT NULL   -- 0012: 'ZONAL' | 'REGIONAL', CHECK, immutable
zone_id                        TEXT NOT NULL DEFAULT ''   -- непуст ⇔ placement_type='ZONAL'
region_id                      TEXT NOT NULL DEFAULT ''   -- 0012; непуст ⇔ 'REGIONAL'
                               -- оба — plain TEXT, no FK (geography → kacho-geo)
name, description, labels
v4_cidr_blocks                 TEXT[] NOT NULL DEFAULT '{}'   -- [1] = якорь ipv4_cidr_primary контракта
v6_cidr_blocks                 TEXT[] NOT NULL DEFAULT '{}'   -- [1] = якорь ipv6_cidr_primary
v4_cidr_primary                CIDR GENERATED ALWAYS AS (v4_cidr_blocks[1]) STORED
v6_cidr_primary                CIDR GENERATED STORED
route_table_id                 TEXT NULL FK ON DELETE SET NULL

subnets_project_id_name_key      UNIQUE (project_id, name) WHERE name <> ''
subnets_placement_type_chk       CHECK (placement_type IN ('ZONAL','REGIONAL'))          -- 0012
subnets_placement_payload_chk    CHECK — ровно одно из zone_id/region_id непусто         -- 0012
EXCLUDE USING gist (network_id WITH =, v4_cidr_primary inet_ops WITH &&)   -- subnets_no_overlap_v4, ТОЛЬКО якорь
EXCLUDE USING gist (network_id WITH =, v6_cidr_primary inet_ops WITH &&)   -- subnets_no_overlap_v6, ТОЛЬКО якорь
```

Неперекрытие **всех** блоков (а не только якорей) держит нормализованная child-таблица
`subnet_cidr_blocks` из 0010 — `EXCLUDE USING gist (network_id WITH =, block inet_ops WITH &&)`.
Baseline-EXCLUDE выше смотрит только на `*_cidr_primary`, поэтому сам по себе вторичные
диапазоны не закрывает; это и было предметом 0010.

CIDR задаётся якорем на Create (immutable); дополнительные блоки обеих семей — через verbs
`:add-cidr-blocks` / `:remove-cidr-blocks`. Удаление подсети
блокируется, если у нее есть внутренние Address (v4 ИЛИ v6) или `NetworkInterface` — sync-precheck
в сервисе + DB-backstops `addresses_internal_subnet_fkey` / `network_interfaces_subnet_id_fkey`.

**Привязка к RouteTable** — выбор RT живёт в service-слое, не в БД (VPC-1 F8):
- `Subnet.Create` подставляет `network.default_route_table_id` (system-created на `Network.Create`), если тенант не задал `route_table_id` явно; строка сети читается `FOR SHARE` в той же writer-TX.
- Оба legacy-триггера DB-выбора **сняты**: `subnet_auto_pick_rt_trg` (BEFORE INSERT ON subnets, «самая ранняя RT сети») — миграцией 0017; `rt_auto_assoc_subnets_trg` (AFTER INSERT ON route_tables, «усыновить subnets с `route_table_id IS NULL`») — миграцией 0019. Двух конкурирующих механизмов выбора RT быть не должно.
- `subnets_outbox_emit_route_table_change_trg` (AFTER UPDATE OF route_table_id) — остаётся: эмитит `Subnet.UPDATED` в `vpc_outbox`, когда `route_table_id` меняет сама БД (после 0019 это только FK ON DELETE SET NULL при `RouteTable.Delete`).

### `addresses`

```
id, project_id                  TEXT NOT NULL
addr_type                      smallint  (1=ext, 2=int)
ip_version                     smallint
external_ipv4                  JSONB     (address, zone_id, address_pool_id) — блок требований снят с контракта
external_ipv6                  JSONB
internal_ipv4                  JSONB     (address, subnet_id)
internal_ipv6                  JSONB     (address, subnet_id)
internal_subnet_id             TEXT GENERATED (из internal_ipv4->>'subnet_id' ИЛИ internal_ipv6->>'subnet_id')
reserved, used                 BOOLEAN                                           -- used=true ⇔ есть referrer-row
used_by                        (flat used_by_type/used_by_id/used_by_name)
deletion_protection            BOOLEAN

addresses_project_id_name_key           UNIQUE (project_id, name) WHERE name <> ''
addresses_external_ip_uniq             UNIQUE (external_ipv4 ->> 'address') WHERE address <> ''
addresses_external_pool_ip_uniq        UNIQUE (external_ipv4 ->> 'address_pool_id', address)
addresses_external_v6_pool_ip_uniq     UNIQUE (external_ipv6 ->> 'address_pool_id', address)
addresses_internal_subnet_ip_uniq      UNIQUE (internal_subnet_id, internal_ipv4 ->> 'address')
addresses_internal_subnet_ipv6_uniq    UNIQUE ((internal_ipv6 ->> 'subnet_id'), (internal_ipv6 ->> 'address'))
addresses_internal_subnet_fkey         FK (internal_subnet_id) → subnets(id) ON DELETE RESTRICT  -- generated col покрывает v4+v6
```

`Address.Delete` блокируется, если адрес `used` (referrer = `NetworkInterface`) →
`FailedPrecondition "address ... is in use by network interface ...; detach it before deleting the address"`.

### `network_interfaces`

First-class самостоятельный сетевой интерфейс (NIC). Project-level, принадлежит `Subnet`.

```
id                  TEXT PK (nic...)
project_id           TEXT NOT NULL
name, labels
subnet_id           TEXT NOT NULL FK→subnets(id) ON DELETE RESTRICT
v4_address_ids      JSONB   -- ссылки на Address по id; один Address ≤ на одном NIC; CHECK jsonb_array_length<=1
v6_address_ids      JSONB   -- CHECK jsonb_array_length<=1
security_group_ids  JSONB   -- подставляемого умолчания НЕТ: пустой массив остаётся пустым.
                            -- Network.default_security_group_id в этот путь не читается
used_by_index       INT     -- 0014: слот привязки NIC↔Instance (device-index, eth0=0)
used_by_type / used_by_id / used_by_name   TEXT   -- denormalised Reference «кто использует NIC» — устанавливается атомарным CAS на смену владельца
mac_address         TEXT UNIQUE cloud-wide, NOT NULL    -- output-only, аллоцируется при Create
status              TEXT  -- 0039: ACTIVE/AVAILABLE/STATUS_UNSPECIFIED — состояние ПРИВЯЗКИ
bandwidth_limit_mbps BIGINT NOT NULL DEFAULT 0  -- 0036: верхняя граница полосы, заданная арендатором.
                            -- 0 — «не задано» (единственное представление отсутствия; ограничение в
                            -- ноль мегабит не выражает ни одна законная просьба). CHECK: 0 ИЛИ строго
                            -- выше опубликованного пола продукта. Верхний край — объявление ПОСАДКИ,
                            -- в схему не вморожен: он разошёлся бы с настройкой при первой её правке
created_at          TIMESTAMPTZ
```

Может быть создан без адресов. Проекция чисто control-plane (lean) — инфра-полей у
kacho-vpc нет.

### `security_groups`

`security_groups.network_id` — **обязателен на Create и immutable после него**: комментарий
самой колонки в `0001_initial.sql` объявляет её `mandatory and immutable after Create`,
use-case отвергает пустое значение синхронно
(`InvalidArgument "network_id required"`). Колонка объявлена NULLABLE только ради FK
(ON DELETE RESTRICT): пустая строка сломала бы ссылку, а use-case пустую и не пропускает.
Прежняя редакция читала NULLABLE-объявление как разрешение на network-less SG — форма
объявления колонки не равна контракту.

`List?filter=network_id="<id>"` работает (`network_id` в whitelist фильтра). Один default-SG
на сеть гарантируется partial UNIQUE `security_groups_one_default_per_network`. Колонки
`status` у таблицы нет — она дропнута миграцией 0003. Область значений правила (протокол,
диапазон портов) держится CHECK'ами из 0027.

### `address_pools`

```
id                      TEXT PK (apl...)
name, description, labels
v4_cidr_blocks          TEXT[]
v6_cidr_blocks          TEXT[]
kind                    smallint
zone_id                 TEXT NULL                                 -- plain TEXT, no FK (zones → kacho-geo); NULL = глобальный fallback
is_default              BOOLEAN
selector_labels         JSONB
selector_priority       INT

address_pools_zone_kind_default_uniq    UNIQUE (COALESCE(zone_id, ''), kind) WHERE is_default
GIN INDEX address_pools_selector_labels_gin (selector_labels jsonb_path_ops) WHERE selector_labels <> '{}'
```

CIDR пулов не пересекаются per `kind` — через нормализованную child-таблицу
`address_pool_cidrs` + EXCLUDE gist (миграция 0004).

### Geography (Region/Zone) — не в kacho-vpc

Geography (Region/Zone) — домен leaf-сервиса `kacho-geo`.
В `kacho-vpc` этих таблиц нет; `subnets.zone_id` / `address_pools.zone_id` /
`addresses.external_ipv4->>'zone_id'` — просто `TEXT`-id без FK, существование валидируется на
request-path через `geo.v1.ZoneService.Get`; dangling-ref (зона удалена в kacho-geo) переживается
грациозно на чтении.

### `address_pool_network_default`

```
address_pool_network_default(network_id PK FK→networks ON DELETE CASCADE, pool_id FK→address_pools ON DELETE RESTRICT)
```

### `address_references`

Referrer-tracking «кто использует адрес». Один referrer на адрес.

```
address_id     TEXT  PK  FK→addresses ON DELETE CASCADE
referrer_type  TEXT      ("compute_instance" | "network_interface" — расширяемо)
referrer_id    TEXT      (id ресурса-владельца — id ВМ / id NIC)
referrer_name  TEXT      ('' если не задано; best-effort на момент привязки)
attached_at    TIMESTAMPTZ DEFAULT now

index address_references_referrer_idx (referrer_type, referrer_id)
```

`addresses.used` поддерживается сервис-слоем синхронно: `true` ⇔ существует
referrer-row (SetReference выставляет, ClearReference снимает; FK CASCADE
убирает row при удалении адреса). Управляется через
`InternalAddressService.{Set,Clear,Get}AddressReference`; surfaced через
`SubnetService.ListUsedAddresses` (`UsedAddress.references[]`). `NetworkInterface.Create`
с `v4_address_ids[]`/`v6_address_ids[]` ставит referrer-rows `referrer_type="network_interface"`
(один Address ≤ на одном NIC); `Address.Delete` для `used`-адреса → `FailedPrecondition`.
kacho-compute привязывает эфемерные NIC-адреса ВМ через эти RPC.

### `vpc_outbox`

```
sequence_no       BIGINT PK  DEFAULT nextval(vpc_outbox_sequence_no_seq)
resource_kind     TEXT
resource_id       TEXT
event_type        TEXT  (CREATED|UPDATED|DELETED)
payload           JSONB
created_at        TIMESTAMPTZ DEFAULT now
processed_at      TIMESTAMPTZ

trigger vpc_outbox_notify_trg AFTER INSERT
  EXECUTE PROCEDURE pg_notify('vpc_outbox', NEW.sequence_no::text)
```

### `fga_register_outbox` (миграция 0006/0008)

Transactional-outbox для регистрации владения через `kaname`. Намерение
«register/unregister» пишется строкой в той же writer-TX, что вставляет/удаляет
ресурс (один commit, без dual-write); отдельный register-drainer применяет каждое намерение
через `InternalIAMService.RegisterResource`/`Unregister`. LISTEN/NOTIFY-канал
`kacho_vpc_fga_register_outbox` будит drainer на INSERT.

### `operations`

Объявлена **в собственном** `0001_initial.sql` сервиса, а не подтягивается из общего
набора: `git grep migrations/common -- services/vpc` даёт пусто, и таблица описана в той
же миграции, что и остальные. Общей остаётся **логика** worker'а (`pkg/operations`), а не
DDL. PK `id` (`enp...`). Без FK на ресурсы (resource может быть удален до завершения op).
`account_id` — nullable денормализация (миграция 0009; для vpc остается NULL).

## Connection / pooling

- `pkg/db`.`NewPool(cfg)` — pgxpool с retry + lifecycle.
- `KACHO_VPC_DB_MAX_CONNS` прокидывается в DSN (`pool_max_conns`) **только** для pgxpool;
  `migrate` использует отдельный `MigrateDSN` без этого параметра (иначе `database/sql`
  шлет серверу неизвестный PG-параметр → `FATAL`).
- Init container `migrate up` прокатывает миграции до старта основного.

## psql быстрый доступ

```bash
# из каталога deploy монорепо
make -C deploy psql SVC=vpc

# Эквивалент (ровно то, что делает цель):
kubectl exec -it -n kacho statefulset/pg-vpc -- psql -U vpc -d kacho_vpc
```

> Цель несёт страж контекста (`guard-kind-context`): интерактивная сессия — это
> произвольная запись в базу **активного** контекста, а не чтение.

Полезные команды:

```sql
-- Список всех миграций
SELECT * FROM goose_db_version ORDER BY version_id DESC LIMIT 10;

-- Все индексы по таблице
\d address_pools
\d addresses

-- Pool utilization вручную
SELECT
  ap.name, ap.zone_id,
  unnest(ap.v4_cidr_blocks) AS cidr,
  count(*) FILTER (WHERE a.external_ipv4 IS NOT NULL) AS used
FROM address_pools ap
LEFT JOIN addresses a
  ON a.external_ipv4 ->> 'address_pool_id' = ap.id
GROUP BY ap.id, ap.name, ap.zone_id, ap.v4_cidr_blocks;

-- Найти dangling Address (no allocated IP старше 5 минут)
SELECT id, project_id, name, external_ipv4, created_at
FROM addresses
WHERE external_ipv4 IS NOT NULL
  AND coalesce(external_ipv4 ->> 'address', '') = ''
  AND created_at < now() - interval '5 minutes';
```
