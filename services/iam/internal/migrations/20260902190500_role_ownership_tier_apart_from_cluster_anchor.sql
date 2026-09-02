-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- role_ownership_tier_apart_from_cluster_anchor — ВЛАДЕНИЕ ролью получает свой
-- носитель, и послабление подстановки перестаёт быть следствием кластерного
-- якоря.
--
-- Задача продукта #1032 (P0). Приёмка — APPROVED круга 2,
-- services/iam/docs/engineering/acceptance/role-ownership-tier-apart-from-cluster-anchor.md:
-- §2.1 (носитель и форма ключа), §2.3 (проверка подстановки), §2.4 (составление
-- имени), сценарии IAM-OM-1-06…12.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО СЕГОДНЯ
--
-- Признак «этой роли можно подставлять звёздочку» и признак «эту роль не правит
-- арендатор» несёт ОДНА колонка — вычисляемый `is_system`
-- (`0056_role_definition_tier.sql`, GENERATED ALWAYS AS (cluster_id IS NOT NULL)).
-- Базовая роль модуля системна во втором смысле и обязана быть системной: иначе
-- её правил арендатор. Но вместе со вторым смыслом она получает первый — прямой
-- путь к `*.*.*`: правило проходит и БД (её проверка судит только ФОРМУ правил,
-- `iam_rules_valid`, 0033), и домен (`Rules.Validate(IsSystem)`), и каталожный
-- гейт (короткое замыкание на системном контексте первой строкой).
--
-- В диффе это выглядит обычной строкой роли, и ни один обзор её не поймает.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО ЭТА МИГРАЦИЯ ДЕЛАЕТ
--
-- 1. `roles.owner_module` — модуль, которому роль принадлежит. NULL означает
--    ПЛАТФОРМЕННУЮ роль (admin, edit, view, owner, kacho-system.*), непустое
--    значение — роль, объявленную манифестом этого модуля. Обратного заполнения
--    не требуется: все существующие строки становятся платформенными без единого
--    UPDATE, и это свойство ключа, а не удача.
--
-- 2. Ключ на ПЕРВИЧНЫЙ ключ каталога модулей — `catalog_module (module)`, а не
--    на пару `(module, live)`. Форма выбрана прогоном, а не вкусом (§2.1):
--    ключ живости требует, чтобы держатель умел отпустить референт своим
--    снятием, а у роли модуля снятия НЕТ НИ ОДНОГО (удаление в role_repo идёт
--    с `AND is_system = false`; применителю удаление запрещено гейтом дерева
--    `internal/repohygiene/applierneverdeletes.go`). С ключом живости модуль с
--    ролью не снимался бы НИКОГДА — а это ровно тот вход, на котором сценарий
--    IAM-MW-1-08 соседней APPROVED-приёмки module-withdrawal-is-described.md
--    требует прохождения.
--
--    ЧТО ЭТОТ КЛЮЧ НЕ ДЕРЖИТ — названо, а не умолчано: состояние «модуль снят,
--    а его роли живы» остаётся ПРЕДСТАВИМЫМ, и роль снятого модуля продолжает
--    давать права. Предмет живой и заведён — #1913. Прецедент двух шагов в этом
--    же дереве: уровень глаголов каталога стоял ровно в этом состоянии и закрыт
--    своим изменением (20260902174501), когда у ребёнка появилась живость.
--
-- 3. `roles_rule_wildcards_confined` — подстановка роли с владельцем не выходит
--    за её модуль. ОДНО правило, а не три исключения: `module: "*"` не находится
--    ни в одном модуле, поэтому отвергается всегда; ресурс `*` находится в
--    модуле своего правила, поэтому законен ровно тогда, когда этот модуль и
--    есть владелец.
--
--    Глагол `*` НЕ затрагивается — решение, а не пропуск: он разрешён и в
--    арендаторской роли безусловно (`validateVerbs`), потому что он не сегмент
--    пространства имён, а «все действия названного типа». Сузить его здесь
--    значило бы отобрать уже выданное под видом починки.
--
-- 4. `roles_owner_module_name_prefix` — имя роли СОСТАВЛЕНО из владельца.
--    Пространство имён системных ролей плоское и глобальное
--    (`roles_system_unique UNIQUE (cluster_id, name) WHERE is_system`), поэтому
--    без составления второй поставщик со своим `viewer` получал бы 23505 по
--    причине, от него не зависящей.
--
--    `left(...)`, а НЕ `LIKE`: образец LIKE, собранный из колонки, истолковал бы
--    `%` и `_` в значении как подстановку. Сегодня имя модуля их не содержит, но
--    это свойство ДРУГОГО правила, и опираться на него значило бы завести третье
--    место об одном предмете.
--
-- ФОРМА `NOT VALID` + `VALIDATE CONSTRAINT` — у ключа и обеих проверок. `roles`
-- каталогом не является и растёт с арендаторами, поэтому довод «на каталоге из
-- тридцати строк это ничто» сюда не переносится. `ADD CONSTRAINT … NOT VALID`
-- берёт краткий ACCESS EXCLUSIVE без прохода по таблице, `VALIDATE CONSTRAINT`
-- перепроверяет строки под более слабым SHARE UPDATE EXCLUSIVE. Форма и оба
-- уровня взяты у применённой 0062_access_binding_target_cardinality.sql, а не
-- выведены.
--
-- ОБЕ ПРОВЕРКИ — ЗАЩИТА ПОСЛЕДНЕГО РУБЕЖА. Обе величины сервис проверяет сам,
-- до вставки: политику — домен, составление — загрузчик и применитель. Значит
-- срабатывание любой из них по существу означает «мы пропустили негодное
-- значение» — наш дефект, а не ввод вызывающего. Полосу они получают всё же
-- ВВОДА (общий текст «Illegal argument: value violates a constraint», текст СУБД
-- наружу не эхается); перевод в полосу дефекта сервиса — предмет #1903.

-- +goose Up

ALTER TABLE kacho_iam.roles
  ADD COLUMN owner_module text;

COMMENT ON COLUMN kacho_iam.roles.owner_module IS
  'Модуль, которому принадлежит роль. NULL — платформенная роль (admin/edit/view/owner, '
  'kacho-system.*): её объявляет платформа, и послабление подстановки у неё полное. '
  'Непустое значение — роль, объявленная манифестом этого модуля: подстановка законна '
  'ровно в пределах названного модуля (roles_rule_wildcards_confined), а имя составлено '
  'из владельца (roles_owner_module_name_prefix). Признак is_system при этом НЕ меняется '
  'ни на одну строку: роль модуля остаётся системной, и арендатор её по-прежнему не правит. '
  'Задача продукта #1032.';

ALTER TABLE kacho_iam.roles
  ADD CONSTRAINT roles_owner_module_fk
    FOREIGN KEY (owner_module)
    REFERENCES kacho_iam.catalog_module (module)
    ON DELETE NO ACTION ON UPDATE NO ACTION NOT VALID;

ALTER TABLE kacho_iam.roles VALIDATE CONSTRAINT roles_owner_module_fk;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_iam.iam_rule_wildcards_confined(rules jsonb, owner_module text)
    RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
AS $$
DECLARE
    rule jsonb;
BEGIN
    -- Платформенная роль: политика не менялась ни на йоту, послабление полное.
    IF owner_module IS NULL THEN RETURN true; END IF;

    -- Форму правил судит iam_rules_valid (0033) — второго кодека здесь не
    -- заводится. Негодная форма до этой функции доезжает только у строки,
    -- которую та проверка уже отвергла бы; поэтому неизвестное читается
    -- защитно, а не додумывается.
    IF rules IS NULL THEN RETURN true; END IF;
    IF jsonb_typeof(rules) <> 'array' THEN RETURN true; END IF;

    FOR rule IN SELECT value FROM jsonb_array_elements(rules) LOOP
        IF jsonb_typeof(rule) <> 'object' THEN CONTINUE; END IF;

        -- module: "*" не находится НИ В ОДНОМ модуле — отвергается всегда.
        IF rule ->> 'module' = '*' THEN RETURN false; END IF;

        -- ресурс "*" находится в модуле СВОЕГО правила: законен ровно тогда,
        -- когда этот модуль и есть владелец роли.
        IF jsonb_typeof(rule -> 'resources') = 'array'
           AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(rule -> 'resources') e WHERE e = '*')
           AND rule ->> 'module' IS DISTINCT FROM owner_module
        THEN
            RETURN false;
        END IF;
    END LOOP;

    RETURN true;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION kacho_iam.iam_rule_wildcards_confined(jsonb, text) IS
  'Подстановка роли с владельцем не выходит за её модуль. Глагол * не судится намеренно: '
  'он разрешён и в арендаторской роли безусловно, потому что он не сегмент пространства '
  'имён, а «все действия названного типа». Задача продукта #1032.';

ALTER TABLE kacho_iam.roles
  ADD CONSTRAINT roles_rule_wildcards_confined
    CHECK (kacho_iam.iam_rule_wildcards_confined(rules, owner_module)) NOT VALID;

ALTER TABLE kacho_iam.roles VALIDATE CONSTRAINT roles_rule_wildcards_confined;

ALTER TABLE kacho_iam.roles
  ADD CONSTRAINT roles_owner_module_name_prefix
    CHECK (owner_module IS NULL
           OR left(name, length(owner_module) + 1) = owner_module || '.') NOT VALID;

ALTER TABLE kacho_iam.roles VALIDATE CONSTRAINT roles_owner_module_name_prefix;

-- +goose StatementBegin
DO $$
DECLARE
    owned    bigint;
    platform bigint;
    modules  bigint;
    keys     int;
BEGIN
    SELECT count(*) FILTER (WHERE owner_module IS NOT NULL),
           count(*) FILTER (WHERE owner_module IS NULL)
      INTO owned, platform
      FROM kacho_iam.roles;

    SELECT count(*) INTO modules FROM kacho_iam.catalog_module WHERE live;

    SELECT count(*) INTO keys
      FROM pg_constraint
     WHERE conrelid = 'kacho_iam.roles'::regclass
       AND conname IN ('roles_owner_module_fk',
                       'roles_rule_wildcards_confined',
                       'roles_owner_module_name_prefix')
       AND convalidated;

    -- Перепись печатается ВСЕГДА: «ноль ролей с владельцем» — ожидаемое
    -- состояние на день применения, и оно обязано быть отличимо от «строк не
    -- читали». Три проверенных ограничения — то, что делает вставку негодной
    -- строки невозможной, а не объявленной.
    RAISE NOTICE
        'ярус владения роли: ролей с владельцем %, платформенных %, живых модулей каталога %; '
        'проверенных ограничений владения % из 3',
        owned, platform, modules, keys;

    IF keys <> 3 THEN
        RAISE EXCEPTION 'ярус владения роли: проверенных ограничений % из 3 — '
            'невалидированное ограничение планировщик доказанным не считает', keys;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат снимает НОСИТЕЛЬ владения вместе с обеими проверками. После него
-- послабление подстановки снова становится следствием кластерного якоря, и
-- манифест модуля снова получает прямой путь к `*.*.*` — это надо знать тому,
-- кто откат применяет.
ALTER TABLE kacho_iam.roles
  DROP CONSTRAINT roles_owner_module_name_prefix;

ALTER TABLE kacho_iam.roles
  DROP CONSTRAINT roles_rule_wildcards_confined;

ALTER TABLE kacho_iam.roles
  DROP CONSTRAINT roles_owner_module_fk;

DROP FUNCTION kacho_iam.iam_rule_wildcards_confined(jsonb, text);

ALTER TABLE kacho_iam.roles
  DROP COLUMN owner_module;
