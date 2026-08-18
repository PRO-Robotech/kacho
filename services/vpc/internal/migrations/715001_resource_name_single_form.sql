-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Имя ресурса: одна форма на всё дерево, пустого имени не существует.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#715`.
--
-- ЧТО ИЗМЕНИЛОСЬ В КОНТРАКТЕ. Форма имени сведена к одной — DNS label по
-- RFC 1123, `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`. Пустая строка остаётся
-- законным ВХОДОМ создания, но ресурсом с пустым именем не становится: сервис
-- подставляет имя, производное от `id`, до вставки. Правка на пустое имя
-- отвергается.
--
-- ПОЧЕМУ БАЗУ НАДО ТРОГАТЬ, А НЕ ТОЛЬКО КОД. Прежние ограничения этой схемы
-- расходятся с новой формой В ОБЕ СТОРОНЫ, и одна из сторон — живой отказ:
--
--   * они ШИРЕ канона — принимают заглавные, подчёркивание и пустую строку;
--   * они УЖЕ канона — требуют, чтобы имя начиналось с БУКВЫ, тогда как
--     RFC 1123 допускает цифру. Без этой миграции имя `9lives` проходит
--     проверку сервиса и умирает на ограничении таблицы: вызывающий получает
--     внутреннюю ошибку там, где контракт обещал успех.
--
-- Частичные уникальные индексы `WHERE name <> ''` существовали ровно ради
-- пустых имён. Пустых имён больше не бывает, поэтому предикат теряет предмет и
-- снимается: полный `UNIQUE (project_id, name)` становится верным везде. Сеть
-- уже несла полную форму — она приводится к общей не по индексу, а по
-- ограничению формы.
--
-- ЧТО ДЕЛАЕТСЯ СО СТАРЫМИ СТРОКАМИ. Строка, чьё имя новой форме не отвечает,
-- получает `name = id`. Правило одно на оба случая — и на пустое имя, и на
-- `Legacy_Name`, — потому что `id` глобально уникален by construction: замена
-- не может ни столкнуться с чужим именем, ни зависеть от порядка строк.
-- Изобретать «привести к нижнему регистру, заменить подчёркивание на дефис»
-- здесь нельзя: это было бы ВТОРЫМ производством имени, живущим в SQL и
-- расходящимся с тем, что делает сервис, — ровно тот класс, ради снятия
-- которого задача и заведена.
--
-- Цена названа честно: выбранное арендатором имя, не отвечающее форме, будет
-- заменено идентификатором. Это допустимо, потому что имя косметическое и
-- изменяемое (ban #15 — адресация идёт по `id`), а облако не в проде
-- (директива владельца 2026-07-27). Арендатор переименует ресурс одним вызовом.
--
-- ИДЕМПОТЕНТНОСТЬ. Повторный прогон не меняет ничего: после первого прохода
-- строк, не отвечающих форме, не остаётся, а ограничения и индексы создаются
-- через DROP IF EXISTS + CREATE.

-- +goose Up
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

DO $$
DECLARE
    -- Все таблицы схемы, несущие пользовательское имя.
    t   text;
    tbl text[] := ARRAY[
        'networks', 'route_tables', 'subnets', 'addresses',
        'security_groups', 'gateways', 'network_interfaces',
        'cidr_groups', 'address_pools'
    ];
    -- Единственная форма имени. Держится в одной переменной, чтобы девять
    -- ограничений не могли разойтись между собой внутри одного файла.
    form text := '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$';
BEGIN
    FOREACH t IN ARRAY tbl LOOP
        -- 1. Имя, не отвечающее форме (включая пустое), заменяется на id.
        --    ДО создания ограничения и полного индекса: иначе они не построятся.
        EXECUTE format(
            'UPDATE kacho_vpc.%I SET name = id WHERE name IS NULL OR name !~ %L',
            t, form);

        -- 2. Ограничение формы приводится к канону. Имена ограничений в этой
        --    схеме исторически двух видов (_name_check и _name_chk) — снимаются
        --    оба, создаётся одно.
        EXECUTE format('ALTER TABLE kacho_vpc.%I DROP CONSTRAINT IF EXISTS %I', t, t || '_name_check');
        EXECUTE format('ALTER TABLE kacho_vpc.%I DROP CONSTRAINT IF EXISTS %I', t, t || '_name_chk');
        EXECUTE format(
            'ALTER TABLE kacho_vpc.%I ADD CONSTRAINT %I CHECK (name ~ %L)',
            t, t || '_name_check', form);
    END LOOP;
END $$;

-- 3. Частичные уникальные индексы имени приводятся к полным. Предикат
--    `WHERE name <> ''` существовал ради пустых имён; предмета у него больше
--    нет. address_pools в перечне нет намеренно — уникального индекса по имени
--    у него не было и до этой миграции.
DO $$
DECLARE
    t   text;
    tbl text[] := ARRAY[
        'route_tables', 'subnets', 'addresses', 'security_groups',
        'gateways', 'network_interfaces', 'cidr_groups'
    ];
BEGIN
    FOREACH t IN ARRAY tbl LOOP
        EXECUTE format('DROP INDEX IF EXISTS kacho_vpc.%I', t || '_project_id_name_key');
        EXECUTE format(
            'CREATE UNIQUE INDEX %I ON kacho_vpc.%I (project_id, name)',
            t || '_project_id_name_key', t);
    END LOOP;
END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Откат возвращает ПРЕЖНЮЮ форму ограничений и частичные индексы.
--
-- Он НЕ восстанавливает прежние имена строк: заменённое имя не сохранено нигде,
-- и восстановить его неоткуда. Это свойство отката названо прямо, а не скрыто
-- за «симметричным» текстом: откат вернёт правила, но не данные.

SET search_path TO kacho_vpc, public;

DO $$
DECLARE
    t   text;
    tbl text[] := ARRAY[
        'networks', 'route_tables', 'subnets', 'addresses',
        'security_groups', 'gateways', 'network_interfaces',
        'cidr_groups', 'address_pools'
    ];
    legacy text := '^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$';
BEGIN
    FOREACH t IN ARRAY tbl LOOP
        EXECUTE format('ALTER TABLE kacho_vpc.%I DROP CONSTRAINT IF EXISTS %I', t, t || '_name_check');
        EXECUTE format(
            'ALTER TABLE kacho_vpc.%I ADD CONSTRAINT %I CHECK (name ~ %L)',
            t, t || '_name_check', legacy);
    END LOOP;
END $$;

DO $$
DECLARE
    t   text;
    tbl text[] := ARRAY[
        'route_tables', 'subnets', 'addresses', 'security_groups',
        'gateways', 'network_interfaces', 'cidr_groups'
    ];
BEGIN
    FOREACH t IN ARRAY tbl LOOP
        EXECUTE format('DROP INDEX IF EXISTS kacho_vpc.%I', t || '_project_id_name_key');
        EXECUTE format(
            'CREATE UNIQUE INDEX %I ON kacho_vpc.%I (project_id, name) WHERE name <> ''''',
            t || '_project_id_name_key', t);
    END LOOP;
END $$;

-- +goose StatementEnd
