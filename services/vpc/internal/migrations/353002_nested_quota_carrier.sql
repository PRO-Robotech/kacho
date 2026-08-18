-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Вложенный потолок: сколько детей помещается в ОДНОЙ сети.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#353`.
--
-- ЧЕГО НЕ БЫЛО. Три вложенных вида этого домена — `vpc.network.subnet`,
-- `vpc.network.securityGroup`, `vpc.network.routeTable` — объявлены в каталоге и
-- посеяны величинами (16, 16, 8; миграция iam `0094`). Администратор облака их
-- видит, вправе изменить и получает успешный ответ — а предел не применяется ни
-- при каких условиях. Это класс «принято-и-проигнорировано» на уровне подсистемы.
--
-- ЗАЧЕМ ВТОРАЯ ОСЬ, КОГДА ЕСТЬ ПРОЕКТНАЯ. Проектная отвечает «сколько подсетей у
-- арендатора», вложенная — «сколько их в ОДНОЙ сети». Опасна именно вложенность:
-- состав сети уезжает исполнителю целиком и резолвится при каждой правке
-- интерфейса, поэтому одна сеть с тысячей подсетей и тысяча подсетей, разложенных
-- по сетям, — разные нагрузки, и вторую платит не только владелец сети.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- -----------------------------------------------------------------------------
-- Проектный резолв вложенных величин.
-- -----------------------------------------------------------------------------
-- Отдельная таблица, а не строка учёта: у вложенного вида есть проектная
-- ВЕЛИЧИНА («по скольку детей на родителя разрешено в этом проекте») и нет
-- проектного ПОТРЕБЛЕНИЯ — детей считают в каждом родителе отдельно. Столбец
-- `used` здесь объявлял бы расход, которого никто не производит.
CREATE TABLE IF NOT EXISTS kacho_vpc.nested_quota_defaults (
    project_id      text   NOT NULL,
    kind            text   NOT NULL,

    limit_value     bigint NOT NULL,
    source_scope    text   NOT NULL,
    source_scope_id text   NOT NULL DEFAULT '',
    limit_revision  bigint NOT NULL DEFAULT 0,
    account_id      text   NOT NULL,

    synced_at       timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT nested_quota_defaults_pkey PRIMARY KEY (project_id, kind),
    CONSTRAINT nested_quota_defaults_limit_check   CHECK (limit_value >= 0),
    CONSTRAINT nested_quota_defaults_account_check CHECK (account_id <> ''),
    CONSTRAINT nested_quota_defaults_scope_check
        CHECK (source_scope IN ('DEFAULT', 'ACCOUNT', 'PROJECT'))
);

COMMENT ON TABLE kacho_vpc.nested_quota_defaults IS
    'per-project resolved value of a NESTED kind ("how many children fit in one '
    'parent of this project"). Deliberately carries no `used`: the value is '
    'resolved per project but consumed per parent, and a `used` column here would '
    'state a consumption nobody produces';

CREATE INDEX IF NOT EXISTS nested_quota_defaults_account_idx
    ON kacho_vpc.nested_quota_defaults (account_id, kind);

-- -----------------------------------------------------------------------------
-- Строка учёта РОДИТЕЛЯ заводится вместе с ним.
-- -----------------------------------------------------------------------------
-- В той же транзакции, что сама сеть, — поэтому у строки всегда есть
-- производитель, и её отсутствие означает не «ещё не спросили соседа», а «сеть
-- заведена до появления этой оси либо до резолва вложенной величины».
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_nested_kind text := TG_ARGV[0];
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM kacho_vpc.project_resource_quotas
         WHERE carrier_type = v_nested_kind AND carrier_id = OLD.id;
        RETURN NULL;
    END IF;

    INSERT INTO kacho_vpc.project_resource_quotas
        (carrier_type, carrier_id, kind, used, limit_value,
         source_scope, source_scope_id, limit_revision, account_id)
    SELECT v_nested_kind, NEW.id, v_nested_kind, 0, d.limit_value,
           d.source_scope, d.source_scope_id, d.limit_revision, d.account_id
      FROM kacho_vpc.nested_quota_defaults d
     WHERE d.project_id = NEW.project_id AND d.kind = v_nested_kind
    ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING;

    RETURN NULL;
END;
$$;

COMMENT ON FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle() IS
    'creates and removes the accounting row of a PARENT carrier in the same '
    'transaction as the parent itself, so the row always has a producer';

-- -----------------------------------------------------------------------------
-- Списание — теперь по ДВУМ осям.
-- -----------------------------------------------------------------------------
-- Тело заменяется целиком (`CREATE OR REPLACE`): применённую миграцию править
-- нельзя (ban #5). Прежние восемь триггеров зовут функцию с одним-двумя
-- аргументами, и вложенная ветвь у них остаётся ИНЕРТНОЙ — третий аргумент пуст.
--
-- ПОЗИЦИИ АРГУМЕНТОВ ЗДЕСЬ СВОИ, и это не расхождение с соседями по недосмотру: у
-- vpc вторая позиция занята булевым столбцом системного ребёнка, которого у nlb
-- нет, поэтому столбец родителя и вложенный вид встали третьим и четвёртым. Гейт
-- дерева («объявленный вложенный вид обязан иметь списание») арности НЕ ЗНАЕТ: он
-- спрашивает, называет ли триггер этот вид, — иначе мерил бы форму записи.
--
-- ОТСУТСТВИЕ СТРОКИ УЧЁТА РОДИТЕЛЯ — ПРОПУСК ВЛОЖЕННОЙ ОСИ, А НЕ ОТКАЗ, и это
-- решение, а не послабление. Сеть, созданная ДО появления этой оси, строки не
-- имеет и получить её задним числом не может: триггер жизненного цикла
-- срабатывает на вставке сети, а она уже случилась. Отказ здесь отверг бы
-- создание подсети в КАЖДОЙ такой сети — то есть у всех существующих арендаторов
-- сразу, и ровно в тот момент, когда предел вводится ради их защиты.
--
-- Пропуск при этом ТОЧЕН, а не удобен: строки нет тогда и только тогда, когда
-- проектный резолв вложенной величины для этого проекта отсутствует, то есть
-- вложенный потолок и вправду НЕ НАЗВАН. Проектная ось при этом продолжает
-- действовать в полную силу и остаётся fail-closed.
--
-- Догоняющее заведение строк для уже живущих сетей делает полоса учёта при
-- материализации проектного резолва — там, где известен проект и его сети.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind        text := TG_ARGV[0];
    v_system_col  text := COALESCE(TG_ARGV[1], '');
    v_parent_col  text := COALESCE(TG_ARGV[2], '');
    v_nested_kind text := COALESCE(TG_ARGV[3], '');
    v_row         jsonb;
    v_project     text;
    v_parent      text;
    v_nested_row  boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_row := to_jsonb(OLD);
    ELSE
        v_row := to_jsonb(NEW);
    END IF;

    -- Системный ребёнок не тратит предел арендатора: его число привязано к числу
    -- родителей один к одному, а родители ограничены — значит он ограничен
    -- транзитивно. Верно и для вложенной оси.
    IF v_system_col <> '' AND COALESCE((v_row ->> v_system_col)::boolean, false) THEN
        RETURN NULL;
    END IF;

    v_project := v_row ->> 'project_id';
    IF v_project IS NULL OR v_project = '' THEN
        RAISE EXCEPTION 'quota: row of % carries no project_id', TG_TABLE_NAME
            USING ERRCODE = 'KQ003';
    END IF;

    IF v_parent_col <> '' THEN
        v_parent := v_row ->> v_parent_col;
        IF v_parent IS NULL OR v_parent = '' THEN
            RAISE EXCEPTION 'quota: row of % carries no %', TG_TABLE_NAME, v_parent_col
                USING ERRCODE = 'KQ003';
        END IF;

        SELECT true INTO v_nested_row
          FROM kacho_vpc.project_resource_quotas
         WHERE carrier_type = v_nested_kind AND carrier_id = v_parent
           AND kind = v_nested_kind;
    END IF;

    IF TG_OP = 'DELETE' THEN
        -- Возврат — в той же транзакции, что удаление строки ресурса. GREATEST не
        -- даёт уйти ниже нуля, если строка учёта заведена позже самих ресурсов.
        IF COALESCE(v_nested_row, false) THEN
            UPDATE kacho_vpc.project_resource_quotas
               SET used = GREATEST(used - 1, 0), updated_at = now()
             WHERE carrier_type = v_nested_kind AND carrier_id = v_parent
               AND kind = v_nested_kind;
        END IF;

        UPDATE kacho_vpc.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй писатель
    -- ждёт коммита первого и видит его результат (ban #10).
    --
    -- СНАЧАЛА РОДИТЕЛЬ. Цена порядка названа честно: арендатор, упёршийся в оба
    -- предела, узнаёт о родительском — поэтому носитель и назван в тексте отказа,
    -- а не оставлен на догадку.
    IF COALESCE(v_nested_row, false) THEN
        UPDATE kacho_vpc.project_resource_quotas
           SET used = used + 1, updated_at = now()
         WHERE carrier_type = v_nested_kind AND carrier_id = v_parent
           AND kind = v_nested_kind
           AND used < limit_value;

        IF NOT FOUND THEN
            PERFORM kacho_vpc.kacho_quota_refuse(v_nested_kind, v_parent, v_nested_kind);
        END IF;
    END IF;

    UPDATE kacho_vpc.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_vpc.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;

COMMENT ON FUNCTION kacho_vpc.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row, on BOTH axes: the project and — when the trigger names a '
    'parent column and the parent has an accounting row — the parent. A parent '
    'without an accounting row is one whose project has no resolved nested value, '
    'so the nested axis is SKIPPED rather than refused: refusing would deny every '
    'child of every network that predates this axis. The project axis stays '
    'fail-closed. Refusals come from kacho_quota_refuse, shared with the advisory '
    'band, so the two cannot word them differently';

-- -----------------------------------------------------------------------------
-- Сеть — носитель учёта для трёх видов своих детей.
-- -----------------------------------------------------------------------------
DROP TRIGGER IF EXISTS networks_quota_carrier_subnet ON kacho_vpc.networks;
CREATE TRIGGER networks_quota_carrier_subnet
    AFTER INSERT OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle(
        'vpc.network.subnet');

DROP TRIGGER IF EXISTS networks_quota_carrier_security_group ON kacho_vpc.networks;
CREATE TRIGGER networks_quota_carrier_security_group
    AFTER INSERT OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle(
        'vpc.network.securityGroup');

DROP TRIGGER IF EXISTS networks_quota_carrier_route_table ON kacho_vpc.networks;
CREATE TRIGGER networks_quota_carrier_route_table
    AFTER INSERT OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle(
        'vpc.network.routeTable');

-- -----------------------------------------------------------------------------
-- Дети сети считаются В СЕТИ, а не только в проекте.
-- -----------------------------------------------------------------------------
-- Второй аргумент — булев столбец системного происхождения — сохраняется ровно
-- тем, чем он был у каждого из этих триггеров: смена значения здесь означала бы,
-- что системный ребёнок начал тратить предел арендатора, а это другая правка.
DROP TRIGGER IF EXISTS subnets_quota_count ON kacho_vpc.subnets;
CREATE TRIGGER subnets_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.subnet', '', 'network_id', 'vpc.network.subnet');

DROP TRIGGER IF EXISTS security_groups_quota_count ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.securityGroup', 'default_for_network', 'network_id',
        'vpc.network.securityGroup');

DROP TRIGGER IF EXISTS route_tables_quota_count ON kacho_vpc.route_tables;
CREATE TRIGGER route_tables_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.routeTable', 'system_owned', 'network_id', 'vpc.network.routeTable');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

DROP TRIGGER IF EXISTS networks_quota_carrier_subnet ON kacho_vpc.networks;
DROP TRIGGER IF EXISTS networks_quota_carrier_security_group ON kacho_vpc.networks;
DROP TRIGGER IF EXISTS networks_quota_carrier_route_table ON kacho_vpc.networks;
DROP FUNCTION IF EXISTS kacho_vpc.kacho_quota_carrier_lifecycle();

-- Триггеры детей возвращаются к ТОЙ ЖЕ арности, какая стояла до этой миграции:
-- откат, оставляющий другой набор аргументов, тихо сменил бы поведение вместо
-- того, чтобы его вернуть.
DROP TRIGGER IF EXISTS subnets_quota_count ON kacho_vpc.subnets;
CREATE TRIGGER subnets_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.subnet');

DROP TRIGGER IF EXISTS security_groups_quota_count ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.securityGroup', 'default_for_network');

DROP TRIGGER IF EXISTS route_tables_quota_count ON kacho_vpc.route_tables;
CREATE TRIGGER route_tables_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.routeTable', 'system_owned');

DROP TABLE IF EXISTS kacho_vpc.nested_quota_defaults;
-- +goose StatementEnd
