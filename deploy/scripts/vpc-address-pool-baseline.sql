-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- Базовый каталог внешних адресов kacho-vpc для ПОДНЯТОГО стенда: единственный
-- на кластер пул EXTERNAL_PUBLIC, НЕ привязанный к зоне, помеченный «по
-- умолчанию». Применяется скриптом deploy/scripts/seed-vpc-address-pools.sh
-- (цель `make seed-vpc-pools`, вызывается из рецепта `dev-up`).
--
-- ЗАЧЕМ. Внешний балансировщик размещается ТОЛЬКО регионально
-- (`EXTERNAL_REGIONAL` — единственное внешнее размещение), а региональный ресурс
-- зоне-независим by construction (data-integrity.md §Placement-coherence).
-- Поэтому его публичный адрес выделяется БЕЗ зоны, и резолвер берёт пул
-- зоне-независимой полосы — `GetDefaultForZone("")`, то есть
-- `WHERE zone_id IS NULL AND kind = 1 AND is_default`
-- (services/vpc/internal/repo/kacho/pg/address_pool.go). Полос ДВЕ и они
-- ВЗАИМОИСКЛЮЧАЮЩИЕ: зональный запрос обслуживается пулом СВОЕЙ зоны и никогда
-- не проваливается в зоне-независимую, и наоборот.
--
-- Пока эта полоса пуста, КАЖДОЕ создание внешнего балансировщика отвечает
-- `could not allocate load balancer address` (код 9), сага сносит durable-handle
-- компенсацией, и `GET` по названному идентификатору отдаёт НАСТОЯЩИЙ 404. То
-- есть свежеподнятый стенд непригоден для целого продуктового действия, и это
-- не видно ни по одному «под Ready» — ровно тот класс беды, из-за которого
-- заведены посевы geo и каталога хранения.
--
-- ПОЧЕМУ SQL, А НЕ ADMIN-RPC. `InternalAddressPoolService` авторизуется на
-- КЛАСТЕРНОМ СИНГЛТОНЕ (`cluster:cluster_root`), то есть требует
-- cluster-admin RS256. Полоса консоли такого принципала не выпускает вовсе: её
-- фикстура регистрирует свежего арендатора через форму провайдера личности
-- (ui-future/e2e/specs/fixtures.ts) и другого удостоверения не имеет. Завести
-- его там значило бы принести в полосу консоли вторую копию машинерии
-- приёмочных фикстур (бутстрап-токен + Hydra + проброс внутреннего слушателя) —
-- та же причина, по которой SQL выбран для geo и для каталога хранения, и она
-- записана в шапках обоих файлов.
--
-- Предикат равнозначности SQL и RPC для ЭТОГО ресурса: AddressPool не участвует
-- в потоке owner-таплов — `grep -c fgaregister
-- services/vpc/internal/apps/kacho/api/addresspool/*.go` даёт 0 (зеркальная
-- форма на network/create.go даёт непустое, то есть предикат читает то, что
-- должен). Ресурс глобальный и админский (ban #6, только внутренний слушатель),
-- пообъектной выдачи у него нет, поэтому строка, записанная SQL-ом, для каждого
-- читателя тождественна записанной через RPC.
--
-- ПОЧЕМУ НЕ МИГРАЦИЕЙ. Пустой старт каталога — решение (0001_initial.sql не
-- несёт ни одного INSERT в address_pools); адресный план стенда в боевой
-- поставке — бессмыслица, а применённую миграцию не правят (ban #5).
--
-- ЧТО ИМЕННО СЕЕТСЯ И ПОЧЕМУ ИМЕННО ЭТА ЛИЧНОСТЬ.
-- Имя и блоки взяты ДОСЛОВНО из deploy/scripts/seed-nlb-fixtures.sh (§3.6,
-- ANY_POOL_NAME/ANY_POOL_V4/ANY_POOL_V6) — не ради красоты, а чтобы у
-- кластерного слота остался ОДИН предмет. Тогда посев набора nlb, встретив эту
-- строку, уходит в свою уже существующую ветку «reusing … placement verified»
-- (ровно ту, которую он берёт на любом повторном прогоне) и не заводит второго
-- автора одного слота. Имя ИСТОРИЧЕСКОЕ: автор строки теперь — подъём стенда, а
-- не набор nlb; переименование потребовало бы согласованной правки в двух
-- объявлениях и одном комментарии и заведено отдельным предметом. Согласие двух
-- объявлений держит гейт TestStandAnycastPoolBaselineMatchesTheNlbSeeder, а не
-- эта строка.
--
-- Блок 100.103.0.0/22 участвует в ГЛОБАЛЬНОМ на kind ограничении
-- `address_pool_cidrs EXCLUDE (kind WITH =, block WITH &&)`, поэтому он обязан
-- не пересекаться ни с одним другим посевом. Перепись занятых полос на день
-- заведения: 100.64.0.0/24 (зональный посев набора vpc), 100.65.0.0/24 (его же
-- зоне-независимый), 100.100.0.0/16 (набор internal-pool), 100.101.0.0/16
-- (набор address), 100.102.<октет>.0/24 (зональный посев nlb), 100.103.7-9.0/24
-- (набор public-pool). /22 — это 100.103.0.0–100.103.3.255, то есть с
-- 100.103.7-9 не пересекается.
--
-- ИДЕМПОТЕНТНОСТЬ И НЕВМЕШАТЕЛЬСТВО. `ON CONFLICT DO NOTHING` БЕЗ указания цели
-- накрывает и первичный ключ, и партиал-UNIQUE «один is_default на
-- (COALESCE(zone_id,''), kind)», и ограничение исключения по пересечению
-- блоков. Следствие, ради которого выбрана именно эта форма: если слот уже
-- держит ЧУЖОЙ пул (посев nlb на долгоживущем стенде, посев набора vpc), посев
-- не пишет НИЧЕГО и не отбирает слот. Audit-строка привязана к RETURNING
-- вставки, поэтому повторный прогон не пишет ни ресурсной, ни audit-строки.
--
-- ПОЧЕМУ ВСТАВКА ЕЩЁ И ОГРАЖДЕНА `NOT EXISTS`. Одного `ON CONFLICT DO NOTHING`
-- мало: слот мог быть свободен, а блоки — уже заняты чужим пулом. Тогда строка
-- пула вставилась бы, а её блоки и список свободных адресов — нет, и стенд
-- получил бы пул «по умолчанию», из которого нельзя выделить ни одного адреса.
-- Отказ был бы тем же самым «could not allocate», только теперь при видимом
-- пуле. Ограждение делает такой исход невозможным by construction, а скрипт
-- доставки утверждает НЕ существование пула, а способность полосы выделить
-- адрес (непустой список свободных).
--
-- ACTOR в audit-payload — `stand_seed:make-seed-vpc-pools`: строку записал
-- подъём стенда, а не принципал. Форма payload сверяется с той, что пишет
-- репозиторий (helpers.AddressPoolDomainPayload = JSON-снимок доменного
-- объекта), гейтом TestStandVpcPoolBaselineMatchesTheWriterPath — он создаёт
-- равнозначный пул НАСТОЯЩИМ путём записи и сличает все затронутые таблицы.

SET search_path TO kacho_vpc, public;

-- Один оператор: строка пула, её нормализованные блоки, материализованный
-- список свободных адресов, курсор IPv6 и audit-строка обязаны появиться вместе
-- либо не появиться вовсе. Скрипт доставки вдобавок оборачивает файл в одну
-- транзакцию — здесь это дублируется намеренно, чтобы файл был верен и при
-- применении вручную.
WITH RECURSIVE
seed(id, name, description, kind, v4, v6) AS (
    VALUES (
        'aplstandanycastextv4',
        'kac-nlb-seed-anycast-pool',
        'stand baseline: zone-independent (anycast) EXTERNAL_PUBLIC VIP pool',
        1::smallint,
        ARRAY['100.103.0.0/22']::text[],
        ARRAY['2001:db8:e2e:1ac::/64']::text[]
    )
),
-- Перечисление адресов блока — та же рекурсия, что исполняет путь записи
-- (addressPoolWriter.populateFreelistForCidrs): от network+1 до broadcast-1.
-- Читает блоки ИЗ `seed`, а не из второго литерала, — два литерала одного блока
-- в одном файле разошлись бы молча.
ips(ip, stop) AS (
        SELECT (network(b::cidr) + 1)::inet, broadcast(b::cidr)::inet
          FROM seed s, unnest(s.v4) AS b
         WHERE family(b::cidr) = 4
    UNION ALL
        SELECT (ip + 1)::inet, stop FROM ips WHERE ip + 1 < stop
),
ins AS (
    INSERT INTO address_pools (
        id, name, description, labels, v4_cidr_blocks, v6_cidr_blocks,
        kind, is_default, selector_labels, selector_priority, zone_id,
        created_at, modified_at)
    -- МЕТКИ — `'null'::jsonb`, А НЕ `'{}'`, и это не описка.
    -- Столбец объявлен NOT NULL DEFAULT '{}', но путь записи сервиса кладёт в
    -- него JSON-значение `null`: незаданная карта меток приезжает в домен как
    -- nil (`domain.LabelsFromMap(nil)`), а json.Marshal нулевой карты даёт
    -- именно `null`. Каждый читатель разбирает оба написания одинаково — в
    -- пустую карту, — но в столбце это РАЗНЫЕ значения, и посев обязан быть
    -- НЕОТЛИЧИМ от записи сервиса, а не «эквивалентен по смыслу». Расхождение
    -- найдено пробой сличения (TestStandVpcPoolBaselineMatchesTheWriterPath) на
    -- её первом же прогоне; если сервис когда-нибудь станет писать `{}`, та же
    -- проба покраснеет и назовёт оба места.
    SELECT s.id, s.name, s.description, 'null'::jsonb, s.v4, s.v6,
           s.kind, true, 'null'::jsonb, 0, NULL, now(), now()
      FROM seed s
     -- Слот кластера уже кем-то занят → не отбираем.
     WHERE NOT EXISTS (
             SELECT 1 FROM address_pools p
              WHERE p.zone_id IS NULL AND p.kind = s.kind AND p.is_default)
     -- Блоки уже кем-то заняты → не заводим пул, из которого нечего выделить.
       AND NOT EXISTS (
             SELECT 1 FROM address_pool_cidrs c, unnest(s.v4 || s.v6) AS b
              WHERE c.kind = s.kind AND c.block && b::cidr)
    ON CONFLICT DO NOTHING
    RETURNING id, name, description, v4_cidr_blocks, v6_cidr_blocks, kind,
              zone_id, is_default, selector_priority
),
cidrs AS (
    INSERT INTO address_pool_cidrs (pool_id, kind, block)
    SELECT i.id, i.kind, b::cidr
      FROM ins i, unnest(i.v4_cidr_blocks || i.v6_cidr_blocks) AS b
    ON CONFLICT DO NOTHING
    RETURNING pool_id
),
freelist AS (
    INSERT INTO address_pool_free_ips (pool_id, ip)
    SELECT i.id, host(ips.ip)::inet FROM ins i, ips
    ON CONFLICT DO NOTHING
    RETURNING pool_id
),
cursor6 AS (
    -- Пул с блоками IPv6 обслуживается разрежённым счётчиком, а не списком
    -- свободных адресов: курсор ставит путь записи (addressWriter.InitIPv6PoolCursor).
    INSERT INTO ipv6_pool_cursors (pool_id, next_offset)
    SELECT i.id, 1 FROM ins i WHERE array_length(i.v6_cidr_blocks, 1) > 0
    ON CONFLICT (pool_id) DO NOTHING
    RETURNING pool_id
)
INSERT INTO vpc_outbox (resource_kind, resource_id, event_type, payload)
SELECT 'AddressPool', i.id, 'CREATED', jsonb_build_object(
           'ID',               i.id,
           'Name',             i.name,
           'Description',      i.description,
           'Labels',           NULL::jsonb,
           'V4CIDRBlocks',     to_jsonb(i.v4_cidr_blocks),
           'V6CIDRBlocks',     to_jsonb(i.v6_cidr_blocks),
           'Kind',             i.kind,
           'ZoneID',           coalesce(i.zone_id, ''),
           'IsDefault',        i.is_default,
           'SelectorLabels',   NULL::jsonb,
           'SelectorPriority', i.selector_priority,
           'actor',            'stand_seed:make-seed-vpc-pools')
  FROM ins i;
-- Изменяющие CTE `cidrs`/`freelist`/`cursor6` в основном запросе не читаются и
-- читаться не должны: PostgreSQL исполняет изменяющий оператор внутри WITH
-- РОВНО ОДИН РАЗ И ЦЕЛИКОМ независимо от того, читает ли основной запрос его
-- вывод. Приписывать им искусственную ссылку «чтобы не выбросили» значило бы
-- закрепить в файле неверное утверждение о движке.
