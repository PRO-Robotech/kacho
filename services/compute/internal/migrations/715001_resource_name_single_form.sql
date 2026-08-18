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
-- ПОЧЕМУ БАЗУ НАДО ТРОГАТЬ, А НЕ ТОЛЬКО КОД. Прежние ограничения расходятся с
-- новой формой В ОБЕ СТОРОНЫ, и одна из сторон — живой отказ: они ШИРЕ канона
-- (принимают пустую строку), но при этом УЖЕ него — требуют, чтобы имя
-- начиналось с БУКВЫ, тогда как RFC 1123 допускает цифру. Без этой миграции имя
-- `9lives` проходит проверку сервиса и умирает на ограничении таблицы:
-- вызывающий получает внутреннюю ошибку там, где контракт обещал успех.
--
-- ЧТО ДЕЛАЕТСЯ СО СТАРЫМИ СТРОКАМИ. Строка, чьё имя новой форме не отвечает,
-- получает `name = id`. Правило одно на оба случая — и на пустое имя, и на
-- `Legacy_Name`, — потому что `id` глобально уникален by construction: замена
-- не может ни столкнуться с чужим именем, ни зависеть от порядка строк.
-- Изобретать «привести к нижнему регистру, заменить подчёркивание на дефис»
-- нельзя: это было бы ВТОРЫМ производством имени, живущим в SQL и расходящимся
-- с тем, что делает сервис.
--
-- Цена названа честно: выбранное арендатором имя, не отвечающее форме, будет
-- заменено идентификатором. Это допустимо, потому что имя косметическое и
-- изменяемое (ban #15 — адресация идёт по `id`), а облако не в проде
-- (директива владельца 2026-07-27).
--
-- ИДЕМПОТЕНТНОСТЬ. Повторный прогон не меняет ничего: строк, не отвечающих
-- форме, после первого прохода не остаётся, ограничения и индексы создаются
-- через DROP IF EXISTS + CREATE.

-- +goose Up
-- +goose StatementBegin

SET search_path TO public;

DO $$
DECLARE
    t    text;
    tbl  text[] := ARRAY['instances', 'machine_types', 'guest_access_keys', 'placement_groups'];
    form text := '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$';
BEGIN
    FOREACH t IN ARRAY tbl LOOP
        EXECUTE format('UPDATE public.%I SET name = id WHERE name IS NULL OR name !~ %L', t, form);
        EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT IF EXISTS %I', t, t || '_name_check');
        EXECUTE format('ALTER TABLE public.%I ADD CONSTRAINT %I CHECK (name ~ %L)',
                       t, t || '_name_check', form);
    END LOOP;
END $$;

-- Единственный частичный индекс имени в этой схеме. Предикат `WHERE name <> ''`
-- существовал ради пустых имён; предмета у него больше нет. Остальные три
-- таблицы уже несут полную форму.
DROP INDEX IF EXISTS instances_project_name_uniq;
CREATE UNIQUE INDEX instances_project_name_uniq ON instances (project_id, name);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Откат возвращает ПРЕЖНИЕ правила, но НЕ прежние имена строк: заменённое имя
-- не сохранено нигде, восстановить его неоткуда.

SET search_path TO public;

ALTER TABLE instances         DROP CONSTRAINT IF EXISTS instances_name_check;
ALTER TABLE machine_types     DROP CONSTRAINT IF EXISTS machine_types_name_check;
ALTER TABLE guest_access_keys DROP CONSTRAINT IF EXISTS guest_access_keys_name_check;
ALTER TABLE placement_groups  DROP CONSTRAINT IF EXISTS placement_groups_name_check;

ALTER TABLE guest_access_keys
    ADD CONSTRAINT guest_access_keys_name_check CHECK (length(name) BETWEEN 1 AND 63);
ALTER TABLE placement_groups
    ADD CONSTRAINT placement_groups_name_check  CHECK (length(name) BETWEEN 1 AND 63);

DROP INDEX IF EXISTS instances_project_name_uniq;
CREATE UNIQUE INDEX instances_project_name_uniq ON instances (project_id, name) WHERE name <> '';

-- +goose StatementEnd
