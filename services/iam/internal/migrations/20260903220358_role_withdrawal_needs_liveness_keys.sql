-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- role_withdrawal_needs_liveness_keys — ПОРЯДОК ОТЗЫВА держит КЛЮЧ, а не память
-- применителя.
--
-- Задача продукта #1913. Приёмка
-- services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md
-- (APPROVED круга 4), §2.3; сценарии IAM-RW-1-12, IAM-RW-1-28.
--
-- Отдельным файлом от пометки НАМЕРЕННО: предмет другой — там ФОРМА снятия,
-- здесь ПОРЯДОК его постановки, — и вердикт слитного изменения был бы
-- непрослеживаем.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО БЕЗ ЭТИХ КЛЮЧЕЙ
--
-- Пометка на `roles` праву НИЧЕГО не делает: вердикт читает `role_verb` и
-- `role_rule_selectors`, а `roles` открывает только как ось меток. Значит
-- «выдача на снятую роль доступа не даёт» достигается снятием ПРОЕКЦИЙ, а
-- пометка отвечает за другое — за то, что роль не будет переписана обратно и
-- что состояние названо.
--
-- Порядок между ними — «сперва проекции, потом пометка» — без ключа был бы
-- свойством КОДА: применитель, поставивший пометку раньше, оставил бы живую
-- проекцию под снятой ролью, и вердикт продолжал бы отвечать «разрешено».
-- Software check-then-act здесь запрещён (ban #10): у проекций больше одного
-- писателя.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО ЭТА МИГРАЦИЯ ДЕЛАЕТ
--
-- Даёт каждой из четырёх проекций роли вторую ссылку — на «эта роль И она жива»:
--
--   FOREIGN KEY (role_id, live) REFERENCES kacho_iam.roles (id, live)
--
-- Референт заведён предыдущей миграцией (`roles_id_live_uk`). Тогда пометка
-- роли, у которой осталась хоть одна живая проекция, отвергается `23503`, и
-- пометка становится НЕВОЗМОЖНОЙ раньше снятия — порядок перестаёт быть
-- свойством кода и становится свойством схемы.
--
-- Двум из четырёх колонка уже есть и берётся ТА ЖЕ, а не заводится вторая:
--
--   role_verb                     live есть (20260901113757:568-572)
--   role_rule_ref                 live есть (там же, :400-402)
--   role_rule_selectors           заводится здесь
--   access_binding_target_members заводится здесь
--
-- Смысл у колонки ОДИН — «эта строка проекции жива», — и оба родителя (каталог
-- и роль) она отпускает одинаково: удалением строки.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ КОНСТАНТА `true` ЗДЕСЬ ЗАКОННА — И ЧЕМ ОНА НЕЗАКОННА У КАТАЛОГА
--
-- Обе миграции живости каталога предупреждают: константная `true` сделала бы
-- родителя НЕСНИМАЕМЫМ навсегда, потому что снятые строки каталога НЕ удаляются
-- и держали бы ссылку вечно. Предупреждение верно и здесь НЕ нарушается, потому
-- что различитель у него другой, чем кажется:
--
--   строка каталога     ПОМЕЧАЕТСЯ — строка остаётся  ⇒ нужно выражение в NULL
--   проекция роли       СНИМАЕТСЯ  — строки не остаётся ⇒ константа `true` годна
--
-- Правило, годное и для следующего уровня: константа законна ровно тогда, когда
-- ребёнок УДАЛЯЕТСЯ, и запрещена, когда он ПОМЕЧАЕТСЯ. Ошибиться здесь стоит
-- дорого и тихо: неснимаемость проявляется не при заведении ключа, а при первом
-- снятии, то есть месяцами позже.
--
-- Колонка ОБЫЧНАЯ, а не генерируемая, и довод тот же, что у `role_verb.live`:
-- генерируемая переписала бы кучу под ACCESS EXCLUSIVE, а `ADD COLUMN … boolean
-- NOT NULL DEFAULT true` в PostgreSQL 11+ кучу не переписывает.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ВЕДОМОСТИ СОСТАВЛЯЮЩЕЙ ЖИВОСТИ НЕ ПОЛУЧАЮТ — И ЭТО РЕШЕНИЕ
--
-- `role_grant_orphan` и `role_selector_prune` ОБЯЗАНЫ пережить снятие: в них
-- лежит объяснение того, что отобрано, и ключ живости запер бы роль ровно её
-- собственным следом. Асимметрия названа, потому что она выглядит
-- непоследовательностью.
--
-- `access_bindings` живости тоже не получает, и по другому доводу — обратимость:
-- выдачи переживают снятие, иначе оживление роли возвращало бы её, а кому она
-- была выдана, не знал бы никто. Новую выдачу НА снятую роль отвергает триггер,
-- а не ключ: ключ судит обе стороны сразу и потому невыразим здесь by
-- construction (§2.4 приёмки).

-- +goose Up

-- Перепись ДО — самопроверка ниже утверждает «ни одна строка проекции не
-- тронута» ЗАМЕРОМ, а не выписанным числом.
CREATE TEMP TABLE _role_projections_before ON COMMIT DROP AS
SELECT (SELECT count(*) FROM kacho_iam.role_verb)                     AS verbs,
       (SELECT count(*) FROM kacho_iam.role_rule_ref)                 AS refs,
       (SELECT count(*) FROM kacho_iam.role_rule_selectors)           AS selectors,
       (SELECT count(*) FROM kacho_iam.access_binding_target_members) AS members;

-- ── ДВЕ НЕДОСТАЮЩИЕ СОСТАВЛЯЮЩИЕ ЖИВОСТИ ─────────────────────────────────────

ALTER TABLE kacho_iam.role_rule_selectors
  ADD COLUMN live boolean NOT NULL DEFAULT true;

ALTER TABLE kacho_iam.role_rule_selectors
  ADD CONSTRAINT role_rule_selectors_live_true CHECK (live);

COMMENT ON COLUMN kacho_iam.role_rule_selectors.live IS
  'Константа true. Колонка существует ради ключа role_rule_selectors_role_live_fk: сослаться на «эту роль И она жива» без неё нечем. Константа законна потому, что строка селектора СНИМАЕТСЯ, а не помечается.';

ALTER TABLE kacho_iam.access_binding_target_members
  ADD COLUMN live boolean NOT NULL DEFAULT true;

ALTER TABLE kacho_iam.access_binding_target_members
  ADD CONSTRAINT access_binding_target_members_live_true CHECK (live);

COMMENT ON COLUMN kacho_iam.access_binding_target_members.live IS
  'Константа true. Колонка существует ради ключа access_binding_target_members_role_live_fk: сослаться на «эта роль И она жива» без неё нечем. Константа законна потому, что строка состава СНИМАЕТСЯ, а не помечается.';

-- ── ЧЕТЫРЕ КЛЮЧА ЖИВОСТИ ─────────────────────────────────────────────────────
--
-- `NOT VALID` + `VALIDATE` порознь взяты намеренно: первый берёт короткий замок
-- и не проверяет существующие строки, второй проверяет их без ACCESS EXCLUSIVE.
-- На сегодняшних объёмах разница мала; форма выбрана та, что не станет дорогой,
-- когда объёмы вырастут.

ALTER TABLE kacho_iam.role_verb
  ADD CONSTRAINT role_verb_role_live_fk
    FOREIGN KEY (role_id, live) REFERENCES kacho_iam.roles (id, live)
    ON DELETE CASCADE ON UPDATE NO ACTION NOT VALID;
ALTER TABLE kacho_iam.role_verb VALIDATE CONSTRAINT role_verb_role_live_fk;

ALTER TABLE kacho_iam.role_rule_ref
  ADD CONSTRAINT role_rule_ref_role_live_fk
    FOREIGN KEY (role_id, live) REFERENCES kacho_iam.roles (id, live)
    ON DELETE CASCADE ON UPDATE NO ACTION NOT VALID;
ALTER TABLE kacho_iam.role_rule_ref VALIDATE CONSTRAINT role_rule_ref_role_live_fk;

ALTER TABLE kacho_iam.role_rule_selectors
  ADD CONSTRAINT role_rule_selectors_role_live_fk
    FOREIGN KEY (role_id, live) REFERENCES kacho_iam.roles (id, live)
    ON DELETE CASCADE ON UPDATE NO ACTION NOT VALID;
ALTER TABLE kacho_iam.role_rule_selectors VALIDATE CONSTRAINT role_rule_selectors_role_live_fk;

ALTER TABLE kacho_iam.access_binding_target_members
  ADD CONSTRAINT access_binding_target_members_role_live_fk
    FOREIGN KEY (role_id, live) REFERENCES kacho_iam.roles (id, live)
    ON DELETE CASCADE ON UPDATE NO ACTION NOT VALID;
ALTER TABLE kacho_iam.access_binding_target_members
  VALIDATE CONSTRAINT access_binding_target_members_role_live_fk;

-- ── САМОПРОВЕРКА ИСХОДА И ПЕРЕПИСЬ ───────────────────────────────────────────

-- +goose StatementBegin
DO $$
DECLARE
    before_verbs int;
    before_refs  int;
    before_sel   int;
    before_mem   int;
    after_verbs  int;
    after_refs   int;
    after_sel    int;
    after_mem    int;
    keys_present int;
    contradicted int;
BEGIN
    SELECT verbs, refs, selectors, members
      INTO before_verbs, before_refs, before_sel, before_mem
      FROM _role_projections_before;

    SELECT count(*) INTO keys_present
      FROM pg_constraint
     WHERE contype = 'f'
       AND confrelid = 'kacho_iam.roles'::regclass
       AND conname IN ('role_verb_role_live_fk',
                       'role_rule_ref_role_live_fk',
                       'role_rule_selectors_role_live_fk',
                       'access_binding_target_members_role_live_fk');
    IF keys_present <> 4 THEN
        RAISE EXCEPTION
            'ключей живости проекций заведено % из 4: порядок «проекции → пометка» '
            'остался бы свойством кода, а не схемы (kacho#1913)', keys_present;
    END IF;

    SELECT count(*) INTO after_verbs FROM kacho_iam.role_verb;
    SELECT count(*) INTO after_refs  FROM kacho_iam.role_rule_ref;
    SELECT count(*) INTO after_sel   FROM kacho_iam.role_rule_selectors;
    SELECT count(*) INTO after_mem   FROM kacho_iam.access_binding_target_members;

    IF (after_verbs, after_refs, after_sel, after_mem)
       IS DISTINCT FROM (before_verbs, before_refs, before_sel, before_mem) THEN
        RAISE EXCEPTION
            'миграция изменила проекции: было %, %, %, %; стало %, %, %, % — ключ обязан '
            'менять ПРЕДСТАВИМОЕ, а не строки (kacho#1913)',
            before_verbs, before_refs, before_sel, before_mem,
            after_verbs, after_refs, after_sel, after_mem;
    END IF;

    -- Ключи только что провалидировали каждую строку, поэтому противоречие здесь
    -- невозможно. Проверка стоит ради того, чтобы перепись называла ОБЕ
    -- величины: «нарушений 0» при «прочитано 0» неотличимо от исправной работы.
    SELECT count(*) INTO contradicted
      FROM (
        SELECT role_id FROM kacho_iam.role_verb
        UNION ALL SELECT role_id FROM kacho_iam.role_rule_ref
        UNION ALL SELECT role_id FROM kacho_iam.role_rule_selectors
        UNION ALL SELECT role_id FROM kacho_iam.access_binding_target_members
      ) p
      JOIN kacho_iam.roles r ON r.id = p.role_id
     WHERE NOT r.live;
    IF contradicted <> 0 THEN
        RAISE EXCEPTION
            'живых проекций у снятых ролей: % — ключи не удержали порядок (kacho#1913)',
            contradicted;
    END IF;

    RAISE NOTICE
        'ключи живости роли: заведено %, осмотрено строк проекций % + % + % + %; '
        'живых проекций у снятых ролей %',
        keys_present, after_verbs, after_refs, after_sel, after_mem, contradicted;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат снимает ПОРЯДОК, а не данные: ни одна строка проекции не меняется. После
-- него состояние «роль помечена снятой, а её проекция жива и грантует» снова
-- становится представимым — и это надо знать тому, кто откат применяет.
ALTER TABLE kacho_iam.access_binding_target_members
  DROP CONSTRAINT access_binding_target_members_role_live_fk;
ALTER TABLE kacho_iam.role_rule_selectors
  DROP CONSTRAINT role_rule_selectors_role_live_fk;
ALTER TABLE kacho_iam.role_rule_ref
  DROP CONSTRAINT role_rule_ref_role_live_fk;
ALTER TABLE kacho_iam.role_verb
  DROP CONSTRAINT role_verb_role_live_fk;

ALTER TABLE kacho_iam.access_binding_target_members
  DROP CONSTRAINT access_binding_target_members_live_true;
ALTER TABLE kacho_iam.access_binding_target_members DROP COLUMN live;

ALTER TABLE kacho_iam.role_rule_selectors
  DROP CONSTRAINT role_rule_selectors_live_true;
ALTER TABLE kacho_iam.role_rule_selectors DROP COLUMN live;
