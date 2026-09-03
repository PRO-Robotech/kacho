-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- 20260903170000_selector_trigger_local_name_stops_shadowing_a_column — судья
-- живости селектора перестаёт называть свою переменную именем колонки той
-- таблицы, которую сам же читает.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО СЕГОДНЯ
--
-- `kacho_iam.role_rule_selector_types_live()` (миграция 20260902174500) объявляет
-- локальную переменную цикла `object_type` и читает ею `kacho_iam.catalog_resource`.
-- Пока у той таблицы колонки с таким именем не было, имя разрешалось однозначно.
-- Миграция 20260903112400 завела `catalog_resource.object_type` — и с этого
-- момента `WHERE cr.dotted = object_type` разрешается ДВУМЯ способами: локальной
-- переменной plpgsql и колонкой читаемой таблицы. Умолчание plpgsql на такой
-- неоднозначности — отказ, а не выбор: `ERROR: column reference "object_type" is
-- ambiguous (SQLSTATE 42702)`.
--
-- Отказ приходит НА КАЖДУЮ запись `role_rule_selectors`, потому что триггер стоит
-- `BEFORE INSERT OR UPDATE` без сужения по колонкам. Писателей у таблицы два, и
-- падают оба: путь пользовательской роли (`ReplaceRuleSelectors`) и досев
-- системных селекторов на старте (`seed.SyncAllSystemRoleSelectors`). То есть
-- арендатор не может завести роль, а служба не может подняться.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЭТО КЛАСС «СТОЛКНОВЕНИЕ ПОЛОС», А НЕ ОШИБКА ОДНОГО АВТОРА
--
-- Ни одна из двух миграций не неверна. Первая объявила переменную по предмету
-- («точечный тип из массива»), вторая завела колонку по своему предмету («строка
-- каталога несёт имя типа модели»). Каждая полоса зелена сама по себе: у первой
-- на её дереве колонки ещё нет, у второй триггер не звался. Неверна их РАЗНИЦА, и
-- увидеть её можно только на сведённом дереве.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ ПЕРЕИМЕНОВАНИЕ, А НЕ `#variable_conflict`
--
-- Директива `#variable_conflict use_variable` сняла бы отказ, объявив, что при
-- любом будущем совпадении имён выигрывает переменная. Это правило на ВСЕ имена
-- функции сразу, включая те, что появятся позже, — то есть замена отказа молчанием
-- ровно в том классе, который здесь и сработал. Переименование локальной
-- переменной снимает неоднозначность в одном названном месте и оставляет отказ
-- действующим для всякого следующего совпадения.
--
-- Имя `declared_type` выбрано потому, что называет предмет цикла — ОБЪЯВЛЕННЫЙ
-- правилом точечный тип, — а колонки с таким именем нет ни в одной таблице
-- каталога.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ТЕКСТ ОТКАЗА НЕ МЕНЯЕТСЯ
--
-- Сообщение, SQLSTATE и имя ограничения остаются побайтово прежними: они часть
-- контракта отказа, их читают пробы и разбирает оператор. Меняется РОВНО имя
-- локальной переменной.
--
-- ДАННЫХ МИГРАЦИЯ НЕ ТРОГАЕТ: заменяется тело функции, ни одна строка не
-- читается и не пишется. Обратный путь полный — откатная половина возвращает
-- прежнее тело дословно.

-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_iam.role_rule_selector_types_live() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    declared_type text;
BEGIN
    FOREACH declared_type IN ARRAY NEW.object_types LOOP
        IF NOT EXISTS (
            SELECT 1 FROM kacho_iam.catalog_resource cr
             WHERE cr.dotted = declared_type AND cr.live
        ) THEN
            RAISE EXCEPTION
                'object_types: % is not a live platform resource (role %)',
                declared_type, NEW.role_id
                USING ERRCODE = '23514',
                      CONSTRAINT = 'role_rule_selectors_types_live';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION kacho_iam.role_rule_selector_types_live() IS
  'Референт ТРЕТЬЕЙ поверхности проекции правила: каждый элемент role_rule_selectors.object_types обязан называть живую строку kacho_iam.catalog_resource. Внешний ключ на элемент массива невыразим, проверка в коде запрещена (ban #10) — поэтому триггер. Проверяется КАЖДЫЙ элемент; отказ 23514 называет элемент и роль. Локальная переменная названа declared_type, а не именем колонки catalog_resource: совпадение имён даёт 42702 на КАЖДОЙ записи селектора.';

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_iam.role_rule_selector_types_live() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    object_type text;
BEGIN
    FOREACH object_type IN ARRAY NEW.object_types LOOP
        IF NOT EXISTS (
            SELECT 1 FROM kacho_iam.catalog_resource cr
             WHERE cr.dotted = object_type AND cr.live
        ) THEN
            RAISE EXCEPTION
                'object_types: % is not a live platform resource (role %)',
                object_type, NEW.role_id
                USING ERRCODE = '23514',
                      CONSTRAINT = 'role_rule_selectors_types_live';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
