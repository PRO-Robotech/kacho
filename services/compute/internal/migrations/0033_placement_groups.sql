-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- Группа размещения — правило взаимного расположения машин.
--
-- ДО ЭТОЙ МИГРАЦИИ `instances.placement_group_id` был непрозрачным слагом: он
-- принимался, проверялся на форму, хранился и не значил НИЧЕГО. Продукт обещал
-- возможность, которой нет, и обещал её успехом, а не отказом.

-- +goose Up

CREATE TABLE placement_groups (
    id             text PRIMARY KEY,
    project_id     text NOT NULL,
    name           text NOT NULL,
    description    text NOT NULL DEFAULT '',
    labels         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),

    -- Что группа делает с машинами: разнести либо сблизить. Ровно два намерения,
    -- и оба выразимы без единого числа. Числового параметра разнесения здесь нет
    -- и не будет: он описывает нашу раскладку железа, а не намерение арендатора,
    -- и, опубликованный, запретил бы нам эту раскладку менять.
    strategy       text NOT NULL,

    -- Якорь размещения — ДИСКРИМИНАТОР, а не пара необязательных полей.
    placement_type text NOT NULL,
    zone_id        text NOT NULL DEFAULT '',
    region_id      text NOT NULL DEFAULT '',

    CONSTRAINT placement_groups_id_check
        CHECK (id ~ '^plg-[0-9a-hjkmnp-tv-z]{17}$'),
    CONSTRAINT placement_groups_name_check
        CHECK (length(name) BETWEEN 1 AND 63),
    CONSTRAINT placement_groups_description_check
        CHECK (length(description) <= 1024),
    CONSTRAINT placement_groups_labels_object_check
        CHECK (jsonb_typeof(labels) = 'object'),
    CONSTRAINT placement_groups_strategy_check
        CHECK (strategy IN ('SPREAD', 'PACK')),

    -- Взаимоисключающий якорь: зональная группа несёт зону и не несёт региона,
    -- региональная — наоборот. Строка, где заполнены оба (или ни одного),
    -- описывает размещение, которого не бывает, и её не должно существовать —
    -- это свойство схемы, а не дисциплины вызывающего.
    CONSTRAINT placement_groups_anchor_check
        CHECK (
            (placement_type = 'ZONAL'    AND zone_id <> '' AND region_id = '')
            OR
            (placement_type = 'REGIONAL' AND zone_id = ''  AND region_id <> '')
        )
);

-- Имя различает группы для человека — значит оно обязано быть различающим.
CREATE UNIQUE INDEX placement_groups_project_name_uniq
    ON placement_groups (project_id, name);

-- Курсор списка: та же пара, что у прочих ресурсов продукта.
CREATE INDEX placement_groups_cursor_idx
    ON placement_groups (project_id, created_at, id);

-- ── Ссылка машины на группу становится настоящей ─────────────────────────────
--
-- ОТСУТСТВИЕ ССЫЛКИ ПРЕДСТАВЛЯЕТСЯ NULL-ОМ, а не пустой строкой. Пустая строка
-- как «нет ссылки» — это значение, притворяющееся отсутствием: внешний ключ
-- обязан был бы искать группу с пустым идентификатором и не находить её.
-- Отсутствие обязано быть представимо ОТДЕЛЬНО от значения, и здесь ровно тот
-- случай, ради которого это правило существует.
ALTER TABLE instances ALTER COLUMN placement_group_id DROP DEFAULT;
ALTER TABLE instances ALTER COLUMN placement_group_id DROP NOT NULL;

-- Прежние значения ОБНУЛЯЮТСЯ, и это решение, а не потеря.
--
-- Колонка была непрозрачным слагом без единого читателя: она не влияла ни на
-- размещение, ни на что-либо ещё. Оставить её содержимое значило бы превратить
-- косметическую строку в ссылку, которая никуда не ведёт, — то есть выдать
-- прежнее «ничего не значит» за нынешнее «указывает на группу». Внешний ключ
-- тогда пришлось бы завести непроверенным, и первое же чтение по нему нашло бы
-- висячие ссылки.
UPDATE instances SET placement_group_id = NULL WHERE placement_group_id IS NOT NULL;

-- ON DELETE RESTRICT: снятие группы, в которой стоят машины, означало бы, что
-- правило размещения перестало действовать, а машины остались размещёнными по
-- нему. Держит это ссылочная целостность, а не проверка в коде: проверка
-- защищает ровно тот путь, который через неё проходит, а восстановление
-- оператором идёт мимо.
ALTER TABLE instances
    ADD CONSTRAINT instances_placement_group_fk
    FOREIGN KEY (placement_group_id) REFERENCES placement_groups(id)
    ON DELETE RESTRICT;

CREATE INDEX instances_placement_group_idx
    ON instances (placement_group_id)
    WHERE placement_group_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS instances_placement_group_idx;
ALTER TABLE instances DROP CONSTRAINT IF EXISTS instances_placement_group_fk;
UPDATE instances SET placement_group_id = '' WHERE placement_group_id IS NULL;
ALTER TABLE instances ALTER COLUMN placement_group_id SET NOT NULL;
ALTER TABLE instances ALTER COLUMN placement_group_id SET DEFAULT '';
DROP TABLE IF EXISTS placement_groups;
