-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Шов исполнителя датаплейна снимается целиком: проекция намерения, горизонт
-- продолжения, подтверждение применения, выдача ревизий и семь триггеров.
-- =============================================================================
-- Заведено миграцией 0032 под поток `InternalDataplaneService.WatchIntent`.
-- Исполнителя датаплейна не существует и в этом заходе не появится, поэтому
-- механизм снимается вместе со своим предметом, а не остаётся «на будущее».
--
-- ПОЧЕМУ СНИМАЕТСЯ, А НЕ ОСТАВЛЯЕТСЯ ВПРОК. Проекция не бесплатна и не тиха:
-- семь AFTER-триггеров стоят в транзакции КАЖДОЙ мутации каждого ресурса VPC и
-- берут `pg_advisory_xact_lock(1289161342)` — то есть сериализуют между собой
-- все мутирующие транзакции домена. 0032 назвала эту цену честно и оправдала её
-- гарантией доставки: порядок выдачи ревизий равен порядку фиксации. Гарантия
-- имеет смысл ровно пока есть кому её потреблять. Без читателя остаётся одна
-- цена: узкое место на записи, растущая таблица снятий и уплотнение, которое
-- некому запускать. Мёртвый механизм, выглядящий работающим, — тот самый класс,
-- который правила запрещают в прод-коде (LEAN, запрет #11).
--
-- ПОРЯДОК СНЯТИЯ — ОБРАТНЫЙ ЗАВЕДЕНИЮ, И ОН ЗДЕСЬ НЕ КОСМЕТИКА.
-- Триггеры зависят от функции: `DROP FUNCTION` при живом триггере Postgres
-- отвергает, а не игнорирует. `dataplane_apply` ссылается внешним ключом на
-- `dataplane_intent`, поэтому потребитель снимается раньше владельца. Иначе
-- пришлось бы дописывать CASCADE, а он снёс бы заодно и то, чего мы не
-- называли, — снятие обязано быть перечислением, а не широким жестом.
--
-- ЧТО УХОДИТ ВМЕСТЕ С ТАБЛИЦАМИ И ПОЭТОМУ НЕ ПЕРЕЧИСЛЕНО ОТДЕЛЬНО: уникальный
-- индекс `dataplane_intent_revision_key`, ограничения-CHECK всех трёх таблиц,
-- внешний ключ `dataplane_apply.resource_id` и оба COMMENT ON TABLE. Отдельного
-- оператора им не нужно — они принадлежат таблицам. Последовательность
-- `dataplane_intent_revision_seq` таблице НЕ принадлежит (заведена отдельным
-- CREATE SEQUENCE, без OWNED BY), поэтому снимается явно: иначе она пережила бы
-- всё, чему служила, и осталась бы в схеме объектом без предмета.
--
-- ЧТО УНИЧТОЖАЕТСЯ ИЗ ДАННЫХ — измерено, а не предположено; числа и их
-- обоснование лежат в `dropguard.json` рядом с этим файлом, по записи на
-- таблицу, и сверяются с базой равенством.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- 1. Триггеры — первыми: они держат функцию.
DROP TRIGGER IF EXISTS addresses_dataplane_intent ON kacho_vpc.addresses;
DROP TRIGGER IF EXISTS gateways_dataplane_intent ON kacho_vpc.gateways;
DROP TRIGGER IF EXISTS route_tables_dataplane_intent ON kacho_vpc.route_tables;
DROP TRIGGER IF EXISTS security_groups_dataplane_intent ON kacho_vpc.security_groups;
DROP TRIGGER IF EXISTS network_interfaces_dataplane_intent ON kacho_vpc.network_interfaces;
DROP TRIGGER IF EXISTS subnets_dataplane_intent ON kacho_vpc.subnets;
DROP TRIGGER IF EXISTS networks_dataplane_intent ON kacho_vpc.networks;

-- 2. Функция выдачи ревизии — после того, как её перестали звать.
DROP FUNCTION IF EXISTS kacho_vpc.kacho_dataplane_intent_stamp();

-- 3. Таблицы — потребитель раньше владельца (внешний ключ).
DROP TABLE IF EXISTS kacho_vpc.dataplane_apply;
DROP TABLE IF EXISTS kacho_vpc.dataplane_intent_horizon;
DROP TABLE IF EXISTS kacho_vpc.dataplane_intent;

-- 4. Последовательность — она никому не принадлежала, поэтому названа явно.
DROP SEQUENCE IF EXISTS kacho_vpc.dataplane_intent_revision_seq;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Откат восстанавливает КОНСТРУКЦИЮ 0032 дословно — и не восстанавливает
-- историю, которой у неё больше нет. Это сказано здесь прямо, потому что иначе
-- следующий читатель примет обратимость схемы за обратимость состояния.
--
-- Что откат возвращает: последовательность, три таблицы со всеми ограничениями,
-- индекс, функцию выдачи, семь триггеров и обратное заполнение — то есть по
-- строке на КАЖДЫЙ живой ресурс, ровно как это делала 0032.
--
-- Чего откат не возвращает и вернуть не может:
--   * снятия (`withdrawn = true`) по ресурсам, удалённым до наката 0045: их след
--     хранился только здесь, а источника, из которого его можно пересобрать, в
--     базе нет — удалённой строки ресурса не существует;
--   * прежние номера ревизий: последовательность начинается заново с 1, поэтому
--     любой сохранённый курсор исполнителя после отката УКАЗЫВАЕТ НЕ ТУДА. Он
--     обязан начать с полной выдачи, а не продолжить «после N»; горизонт для
--     этого и восстанавливается нулём — «состояния у меня нет» выразимо;
--   * содержимое `dataplane_apply`: восстанавливать нечего, писал его только
--     исполнитель, которого не существовало.
CREATE SEQUENCE IF NOT EXISTS kacho_vpc.dataplane_intent_revision_seq
    AS bigint START WITH 1 INCREMENT BY 1 NO CYCLE;

CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_intent (
    resource_id text        PRIMARY KEY,
    kind        text        NOT NULL,
    revision    bigint      NOT NULL,
    withdrawn   boolean     NOT NULL DEFAULT false,
    stamped_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT dataplane_intent_kind_known CHECK (kind IN (
        'network', 'subnet', 'network_interface', 'security_group',
        'route_table', 'gateway', 'address')),
    CONSTRAINT dataplane_intent_revision_positive CHECK (revision > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS dataplane_intent_revision_key
    ON kacho_vpc.dataplane_intent (revision);

CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_intent_horizon (
    only_row boolean PRIMARY KEY DEFAULT true,
    revision bigint  NOT NULL DEFAULT 0,

    CONSTRAINT dataplane_intent_horizon_single CHECK (only_row),
    CONSTRAINT dataplane_intent_horizon_nonnegative CHECK (revision >= 0)
);

INSERT INTO kacho_vpc.dataplane_intent_horizon (only_row, revision)
VALUES (true, 0)
ON CONFLICT (only_row) DO NOTHING;

CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_apply (
    resource_id      text        PRIMARY KEY
        REFERENCES kacho_vpc.dataplane_intent(resource_id) ON DELETE CASCADE,
    applied_revision bigint      NOT NULL,
    outcome          text        NOT NULL,
    reason           text        NOT NULL DEFAULT '',
    reported_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT dataplane_apply_outcome_known CHECK (outcome IN ('APPLIED', 'FAILED')),
    CONSTRAINT dataplane_apply_revision_positive CHECK (applied_revision > 0),
    CONSTRAINT dataplane_apply_reason_matches_outcome CHECK (
        (outcome = 'APPLIED' AND reason = '')
        OR (outcome = 'FAILED' AND reason IN (
            'CAPACITY', 'CONFLICT', 'UNSUPPORTED',
            'DEPENDENCY_NOT_READY', 'TRANSIENT', 'EXECUTOR_INTERNAL'))
    )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp()
RETURNS trigger
LANGUAGE plpgsql AS $fn$
DECLARE
    v_kind text := TG_ARGV[0];
    v_id   text;
    v_rev  bigint;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN
        RETURN NULL;
    END IF;

    PERFORM pg_advisory_xact_lock(1289161342);

    v_rev := nextval('kacho_vpc.dataplane_intent_revision_seq');

    IF TG_OP = 'DELETE' THEN
        v_id := OLD.id;
        INSERT INTO kacho_vpc.dataplane_intent AS d (resource_id, kind, revision, withdrawn, stamped_at)
        VALUES (v_id, v_kind, v_rev, true, now())
        ON CONFLICT (resource_id) DO UPDATE
            SET revision = EXCLUDED.revision,
                withdrawn = true,
                stamped_at = EXCLUDED.stamped_at;
        RETURN NULL;
    END IF;

    v_id := NEW.id;
    INSERT INTO kacho_vpc.dataplane_intent AS d (resource_id, kind, revision, withdrawn, stamped_at)
    VALUES (v_id, v_kind, v_rev, false, now())
    ON CONFLICT (resource_id) DO UPDATE
        SET kind = EXCLUDED.kind,
            revision = EXCLUDED.revision,
            withdrawn = false,
            stamped_at = EXCLUDED.stamped_at;
    RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS networks_dataplane_intent ON kacho_vpc.networks;
CREATE TRIGGER networks_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('network');

DROP TRIGGER IF EXISTS subnets_dataplane_intent ON kacho_vpc.subnets;
CREATE TRIGGER subnets_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('subnet');

DROP TRIGGER IF EXISTS network_interfaces_dataplane_intent ON kacho_vpc.network_interfaces;
CREATE TRIGGER network_interfaces_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.network_interfaces
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('network_interface');

DROP TRIGGER IF EXISTS security_groups_dataplane_intent ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('security_group');

DROP TRIGGER IF EXISTS route_tables_dataplane_intent ON kacho_vpc.route_tables;
CREATE TRIGGER route_tables_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('route_table');

DROP TRIGGER IF EXISTS gateways_dataplane_intent ON kacho_vpc.gateways;
CREATE TRIGGER gateways_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.gateways
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('gateway');

DROP TRIGGER IF EXISTS addresses_dataplane_intent ON kacho_vpc.addresses;
CREATE TRIGGER addresses_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.addresses
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('address');
-- +goose StatementEnd

-- +goose StatementBegin
-- Обратное заполнение — то же, что в 0032: без него живые ресурсы не попали бы
-- даже в полную выдачу, и восстановленный исполнитель счёл бы их
-- несуществующими.
INSERT INTO kacho_vpc.dataplane_intent (resource_id, kind, revision, withdrawn)
SELECT id, kind, nextval('kacho_vpc.dataplane_intent_revision_seq'), false
FROM (
    SELECT id, 'network'           AS kind FROM kacho_vpc.networks
    UNION ALL
    SELECT id, 'subnet'            AS kind FROM kacho_vpc.subnets
    UNION ALL
    SELECT id, 'network_interface' AS kind FROM kacho_vpc.network_interfaces
    UNION ALL
    SELECT id, 'security_group'    AS kind FROM kacho_vpc.security_groups
    UNION ALL
    SELECT id, 'route_table'       AS kind FROM kacho_vpc.route_tables
    UNION ALL
    SELECT id, 'gateway'           AS kind FROM kacho_vpc.gateways
    UNION ALL
    SELECT id, 'address'           AS kind FROM kacho_vpc.addresses
) AS existing
ON CONFLICT (resource_id) DO NOTHING;

COMMENT ON TABLE kacho_vpc.dataplane_intent IS
    'Desired-state projection for the data-plane executor: one row per object, '
    'revision issued in commit order, withdrawn=true is a tombstone';
COMMENT ON TABLE kacho_vpc.dataplane_apply IS
    'What the data-plane executor reported as applied; reason is a closed class, never free text';
-- +goose StatementEnd
