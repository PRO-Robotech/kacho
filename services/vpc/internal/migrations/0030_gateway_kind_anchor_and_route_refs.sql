-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Шлюз получает ВИД и ЯКОРЬ РАЗМЕЩЕНИЯ; ссылка маршрута на шлюз получает FK.
-- =============================================================================
-- Три предмета, и они неразделимы: якорь нужен, чтобы у когерентности размещения
-- вообще была вторая сторона, вид — чтобы у ссылки был проверяемый смысл, а
-- нормализованная ссылка — чтобы существование шлюза держал внешний ключ, а не
-- проверка перед записью.
--
-- 1. gateways.subnet_id — привязка шлюза и его якорь размещения.
--    Своей зоны/региона шлюз НЕ несёт: он наследует их через подсеть — тот же
--    канон, по которому зону не несут ни сетевой интерфейс, ни адрес
--    (`.claude/rules/data-integrity.md` §Placement-coherence). Следствие: зона
--    подсети УЖЕ проверена у владельца каталога размещения на её создании, и
--    шлюзу не нужно ни своего обращения к geo, ни своей копии дискриминатора.
--    NOT NULL + FK ON DELETE RESTRICT: якорь есть у КАЖДОЙ строки, и подсеть не
--    исчезает из-под живого шлюза.
--
-- 2. gateways.gateway_type ограничивается закрытым набором {NAT, EGRESS_ONLY}.
--    Прежнее значение по умолчанию 'shared_egress' называло ветвь oneof, которая
--    не несла ни одного поля; она снята с контракта с резервированием номера И
--    имени (proto/kacho/cloud/vpc/v1/gateway.proto). DEFAULT снимается: вид
--    выбирается на КАЖДОМ Create явно, silent-fallback'а нет.
--
-- 3. route_table_gateway_refs — нормализованная ссылка «маршрут → шлюз».
--    Прежде следующий узел через шлюз отвергался синхронно, потому что читать его
--    было нечем. Теперь он резолвится, и существование держит внешний ключ, а не
--    предшествующая записи проверка: под READ COMMITTED коррелированный
--    подзапрос на `gateways` строк не лочит, поэтому «прочитал → записал» и
--    параллельное удаление шлюза оба закоммитились бы, оставив висячую ссылку.
--    FK эту гонку закрывает by construction (проверка ссылки берёт разделяемую
--    блокировку на строку-referent), и он же даёт обратное направление: шлюз,
--    названный живым маршрутом, не удаляется (ON DELETE RESTRICT → 23503 →
--    FAILED_PRECONDITION "gateway is in use"). Эта ветвь в коде уже была
--    (`gatewayWriter.Delete`) и до сегодня была НЕДОСТИЖИМА: на `gateways` не
--    ссылался ни один внешний ключ.
--
--    Строка ссылки несёт `destination_family` — семейство адресов назначения
--    маршрута. Оно денормализовано сюда сознательно: сам маршрут живёт в JSONB
--    `route_tables.static_routes`, и семейство назначения — единственное, что
--    оператору записи нужно от маршрута, чтобы сверить его с видом шлюза. Обе
--    строки (JSONB и ссылка) пишутся ОДНИМ вызывающим в одной writer-TX;
--    расхождение между ними ловит интеграционная проба
--    TestRouteTableGatewayRefsMirrorStaticRoutes.
--
-- ЧТО ДЕЛАЕТСЯ С УЖЕ СУЩЕСТВУЮЩИМИ СТРОКАМИ ШЛЮЗОВ — и почему именно это.
-- Легаси-строка не имеет представления в новом контракте: её единственная
-- возможная ветвь снята с контракта, а якоря у неё нет и взять его негде —
-- подсети шлюз никогда не называл. Такую строку API не может ни отдать (ветвь
-- oneof не существует), ни принять на вход, ни сослаться на неё маршрутом. Три
-- исхода были: оставить якорь необязательным (тогда «отсутствие» и «значение»
-- живут в одном поле навсегда — ровно тот класс, который запрещён), уронить
-- миграцию на непустой таблице (мина под накат стенда: на чистой БД она
-- зелёная, поэтому её никто не увидит до боевого наката) либо снять строки.
-- Снято — с печатью числа, чтобы «ноль удалённых» было отличимо от «удалено
-- молча». Основание снятия: облако не в проде (директива владельца 2026-07-27,
-- цитируется в `.claude/rules/data-integrity.md`), на `gateways` не ссылается ни
-- один внешний ключ (проверено по дереву миграций), а сама таблица не несёт ни
-- одного поля, которое нельзя было бы создать заново.
-- ЧЕСТНО О ЦЕНЕ: снятие идёт SQL'ом, поэтому событие в `vpc_outbox` не
-- эмитируется и зеркало ресурса у владельца прав остаётся с записями об
-- исчезнувших шлюзах. Доступ они не открывают (предмета больше нет — Get отвечает
-- NOT_FOUND), реконсайлер их подберёт; но это остаток, а не «чисто».
--
-- Идемпотентно (IF NOT EXISTS / DO-guard по pg_constraint) — повторный или
-- параллельный migrate-init при helm rollout, как 0016/0024/0028.

SET search_path TO kacho_vpc, public;

DO $$
DECLARE
    retired bigint;
BEGIN
    -- Якорь: сначала колонка (nullable), затем снятие непредставимых строк, затем
    -- NOT NULL. Порядок именно такой: SET NOT NULL на непустой таблице без
    -- предварительной очистки упал бы, а очистка по столбцу требует его наличия.
    ALTER TABLE kacho_vpc.gateways ADD COLUMN IF NOT EXISTS subnet_id text;

    DELETE FROM kacho_vpc.gateways WHERE subnet_id IS NULL;
    GET DIAGNOSTICS retired = ROW_COUNT;
    RAISE NOTICE 'kacho_vpc.gateways: снято строк без якоря размещения: %', retired;

    ALTER TABLE kacho_vpc.gateways ALTER COLUMN subnet_id SET NOT NULL;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'gateways_subnet_fk'
           AND conrelid = 'kacho_vpc.gateways'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.gateways
            ADD CONSTRAINT gateways_subnet_fk
            FOREIGN KEY (subnet_id) REFERENCES kacho_vpc.subnets(id) ON DELETE RESTRICT;
    END IF;

    -- Вид шлюза: закрытый набор, без значения по умолчанию.
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'gateways_type_chk'
           AND conrelid = 'kacho_vpc.gateways'::regclass
    ) THEN
        ALTER TABLE kacho_vpc.gateways
            ADD CONSTRAINT gateways_type_chk
            CHECK (gateway_type IN ('NAT', 'EGRESS_ONLY'));
    END IF;
END
$$;

ALTER TABLE kacho_vpc.gateways ALTER COLUMN gateway_type DROP DEFAULT;

CREATE INDEX IF NOT EXISTS gateways_subnet_idx ON kacho_vpc.gateways (subnet_id);

-- Ссылка «маршрут → шлюз».
--
-- Ключ — (route_table_id, route_index): у статического маршрута нет собственной
-- идентичности (именно поэтому у набора нет аддитивного глагола, см. 0028), и
-- единственное, чем маршрут адресуется, — его позиция в наборе. Она же стоит в
-- тексте отказа (`static_routes[<i>].gateway_id`), поэтому позиция здесь — не
-- служебный номер строки, а часть адреса, который видит вызывающий.
CREATE TABLE IF NOT EXISTS kacho_vpc.route_table_gateway_refs (
    route_table_id     text     NOT NULL REFERENCES kacho_vpc.route_tables(id) ON DELETE CASCADE,
    route_index        integer  NOT NULL,
    gateway_id         text     NOT NULL REFERENCES kacho_vpc.gateways(id) ON DELETE RESTRICT,
    destination_family smallint NOT NULL,

    PRIMARY KEY (route_table_id, route_index),
    CONSTRAINT route_table_gateway_refs_family_chk CHECK (destination_family IN (4, 6)),
    CONSTRAINT route_table_gateway_refs_index_chk  CHECK (route_index >= 0)
);

CREATE INDEX IF NOT EXISTS route_table_gateway_refs_gateway_idx
    ON kacho_vpc.route_table_gateway_refs (gateway_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

DROP TABLE IF EXISTS kacho_vpc.route_table_gateway_refs;

DROP INDEX IF EXISTS kacho_vpc.gateways_subnet_idx;

ALTER TABLE kacho_vpc.gateways
    DROP CONSTRAINT IF EXISTS gateways_type_chk,
    DROP CONSTRAINT IF EXISTS gateways_subnet_fk;

ALTER TABLE kacho_vpc.gateways DROP COLUMN IF EXISTS subnet_id;

ALTER TABLE kacho_vpc.gateways ALTER COLUMN gateway_type SET DEFAULT 'shared_egress';

-- +goose StatementEnd
