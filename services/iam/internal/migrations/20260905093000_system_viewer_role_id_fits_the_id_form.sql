-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: AGPL-3.0-or-later
--
-- 20260905093000_system_viewer_role_id_fits_the_id_form — системная роль viewer
-- становится ДОСТИЖИМОЙ ПО СВОЕМУ id.
--
-- Задача продукта #1808.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО СЕГОДНЯ
--
-- Идентификатор `rol000000000sysviewer`, посеянный `0001_initial.sql`, имеет
-- длину 21, тогда как собственная строгая проверка сервиса
-- (`shared.ValidateResourceID`) требует ровно 20 (`domain.ShortIDLen`).
-- Проверка стоит ПЕРВЫМ стейтментом каждого глагола роли, поэтому всякое
-- обращение к этой роли по id отвергается `INVALID_ARGUMENT` ещё до чтения:
-- `RoleService.Get`, `GetRoleCompiled`, `Update`, `Delete`,
-- `ListAccessBindingsByRole`.
--
-- Арендатор получает роль в ответе `List` и не может прочитать её НИ ОДНИМ
-- путём. Соседняя полоса того же механизма ведёт себя иначе, и различие никем
-- не решалось: у роли admin длина 20, и она проходит.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ СМЕНА id ЗДЕСЬ НЕ НАРУШАЕТ НЕИЗМЕНЯЕМОСТЬ (ban #15)
--
-- Запрет защищает ВНЕШНЕ-АДРЕСУЕМУЮ координату: клиент держит id в ссылке, в
-- гранте, в URL — и смена id ломает то, что он держит. Здесь держать нечего
-- BY CONSTRUCTION: ни один путь чтения этой роли по id никогда не отвечал
-- успехом, поэтому клиент, получивший её из `List`, не мог ни разу
-- воспользоваться идентификатором. Меняется не адрес живой координаты, а
-- строка, адресом никогда не бывшая.
--
-- Обратная сторона названа честно: привязки, СОЗДАННЫЕ на эту роль (их путь
-- лежит через `AccessBinding`, а не через глаголы роли), существовать могут —
-- поэтому они ПЕРЕЦЕЛИВАЮТСЯ, а не бросаются.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ КОПИЯ И ПЕРЕЦЕЛИВАНИЕ, А НЕ ОДИН `UPDATE roles SET id = …`
--
-- Ключи детей объявлены без `ON UPDATE CASCADE` (`ON DELETE {CASCADE|RESTRICT}`),
-- то есть правило обновления — `NO ACTION`, проверяемое немедленно. Простой
-- `UPDATE` родителя оставил бы детей висеть на несуществующей строке в тот же
-- стейтмент и был бы отвергнут. Поэтому порядок: завести годную строку →
-- перецелить детей → снять старую.
--
-- Имя освобождается первым: `roles_system_unique (cluster_id, name)` не даёт
-- двум системным строкам носить одно имя, а временное имя обязано пройти
-- `roles_system_name_check` — отсюда подчёркивания, а не дефисы, после точки.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ИДЕМПОТЕНТНОСТЬ
--
-- Все стейтменты сужены по СТАРОМУ идентификатору. На дереве, где его уже нет
-- (свежая установка после этой миграции), каждый затрагивает ноль строк, и
-- повторный накат безопасен.

-- +goose Up

-- 1. Освободить каноническое имя: два системных имени `kacho-system.viewer`
--    одновременно существовать не могут.
UPDATE kacho_iam.roles
   SET name = 'kacho-system.viewer_legacy_id'
 WHERE id = 'rol000000000sysviewer';

-- 2. Завести строку с ГОДНЫМ идентификатором, дословно копируя содержание.
--    `is_system` не перечисляется: колонка ВЫЧИСЛЯЕМАЯ (`cluster_id IS NOT NULL`).
INSERT INTO kacho_iam.roles
       (id, account_id, name, description, permissions, created_at, cluster_id,
        project_id, rules, labels, owner_module, retired_at, retired_reason,
        retired_by, live)
SELECT 'rol00000000sysviewer', account_id, 'kacho-system.viewer', description,
       permissions, created_at, cluster_id, project_id, rules, labels,
       owner_module, retired_at, retired_reason, retired_by, live
  FROM kacho_iam.roles
 WHERE id = 'rol000000000sysviewer'
    ON CONFLICT (id) DO NOTHING;

-- 3. Перецелить ДЕТЕЙ. Перечень выведен из ключей на `kacho_iam.roles(id)`
--    в `0001_initial.sql`; порядок между ними безразличен — все указывают на
--    одного родителя, и обе строки родителя в этот момент существуют.
UPDATE kacho_iam.access_bindings
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.access_binding_target_members
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.role_grant_orphan
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.role_rule_ref
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.role_rule_selectors
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.role_selector_prune
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';
UPDATE kacho_iam.role_verb
   SET role_id = 'rol00000000sysviewer' WHERE role_id = 'rol000000000sysviewer';

-- 4. Снять строку с негодным идентификатором.
DELETE FROM kacho_iam.roles WHERE id = 'rol000000000sysviewer';

-- +goose Down

UPDATE kacho_iam.roles
   SET name = 'kacho-system.viewer_legacy_id'
 WHERE id = 'rol00000000sysviewer';

INSERT INTO kacho_iam.roles
       (id, account_id, name, description, permissions, created_at, cluster_id,
        project_id, rules, labels, owner_module, retired_at, retired_reason,
        retired_by, live)
SELECT 'rol000000000sysviewer', account_id, 'kacho-system.viewer', description,
       permissions, created_at, cluster_id, project_id, rules, labels,
       owner_module, retired_at, retired_reason, retired_by, live
  FROM kacho_iam.roles
 WHERE id = 'rol00000000sysviewer'
    ON CONFLICT (id) DO NOTHING;

UPDATE kacho_iam.access_bindings
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.access_binding_target_members
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.role_grant_orphan
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.role_rule_ref
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.role_rule_selectors
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.role_selector_prune
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';
UPDATE kacho_iam.role_verb
   SET role_id = 'rol000000000sysviewer' WHERE role_id = 'rol00000000sysviewer';

DELETE FROM kacho_iam.roles WHERE id = 'rol00000000sysviewer';
