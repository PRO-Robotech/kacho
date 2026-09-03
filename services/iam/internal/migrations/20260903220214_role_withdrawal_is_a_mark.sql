-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- role_withdrawal_is_a_mark — ПОМЕТКА СНЯТИЯ у `kacho_iam.roles`.
--
-- Задача продукта #1913. Приёмка
-- services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md
-- (APPROVED круга 4), §2.1; сценарии IAM-RW-1-01, IAM-RW-1-02, IAM-RW-1-03,
-- IAM-RW-1-04. Форма отзыва выбрана записью решения
-- services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО СЕГОДНЯ
--
-- Роль, объявленная манифестом модуля и потом из него убранная, остаётся живой
-- НАВСЕГДА: колонок живости у `roles` ноль, поэтому снять её нечем ничему, кроме
-- разовой миграции, — то есть выкатки образа iam, ровно того, ради устранения
-- чего манифест и заведён. Право, выданное через такую роль, продолжает
-- действовать после того, как модуль перестал её объявлять, и со стороны это
-- неотличимо от исправного состояния.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ФОРМА БЕРЁТСЯ У КАТАЛОГА ДОСЛОВНО, И ЭТО РЕШЕНИЕ, А НЕ СОВПАДЕНИЕ
--
-- Три таблицы каталога прав (`catalog_module`, `catalog_resource`,
-- `catalog_verb`) несут ровно эти элементы, и второй формы в домене не
-- заводится:
--
--   момент      retired_at timestamptz
--   причина     retired_reason text        — без неё «отобрали» неотличимо от «сломалось»
--   живость     live boolean NOT NULL DEFAULT true
--   согласие    CHECK (live = (retired_at IS NULL))
--   референт    UNIQUE (id, live)          — на нём висят ключи живости проекций
--
-- Колонка ОБЫЧНАЯ, а не генерируемая: на ней стоят ключи, а выражение референтом
-- быть не может. Цена названа: `ADD COLUMN … boolean NOT NULL DEFAULT true` в
-- PostgreSQL 11+ кучу НЕ переписывает (умолчание хранится в каталоге), поэтому
-- обратного заполнения не требуется ни одной строкой — все существующие роли
-- становятся живыми свойством умолчания, а не удачей.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- АВТОР СНЯТИЯ ЗАПИСЫВАЕТСЯ ТОЖЕ
--
-- Довод не изобретается здесь: `20260903215500_ledgers_name_the_author.sql` уже
-- завёл `applied_by` двум ведомостям ровно потому, что на вопрос «кто у меня
-- отобрал» ответ «iam» ответом не является. Третий носитель снятия без автора
-- был бы ЗНАЮЩЕЙ регрессией. Пустая строка означает «строка помечена до
-- заведения колонки», а не «автора потеряли», — та же семантика, что у соседей.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧЕГО ЭТА МИГРАЦИЯ НЕ ДЕЛАЕТ
--
-- 1. `roles_system_unique (cluster_id, name) WHERE is_system` НЕ трогается, и
--    `live` в неё не входит. `id` системной роли ВЫВОДИТСЯ из имени
--    (`domain.SystemRoleID`), поэтому повторное объявление того же имени
--    приходит по тому же первичному ключу и ОЖИВЛЯЕТ ту же строку: второй строки
--    с тем же именем не бывает by construction. Включить `live` в индекс значило
--    бы РАЗРЕШИТЬ вторую живую роль рядом со снятой одноимённой — завести
--    коллизию, которой сегодня нет.
--
-- 2. Право пометка НЕ отбирает. Вердикт читает `role_verb` и
--    `role_rule_selectors`, а `roles` — только как ось меток. Отбор права есть
--    снятие ПРОЕКЦИЙ, и порядок между ними держат ключи живости — они заводятся
--    СЛЕДУЮЩЕЙ миграцией: предмет другой, и вердикт слитного изменения был бы
--    непрослеживаем.
--
-- 3. Ни одного производителя пометки эта миграция не заводит. Писателем роли
--    модуля миграция быть не вправе — это отдельный гейт дерева
--    (`internal/repohygiene/migrationnotawriterofmodulerole.go`), и предмет
--    #1913 в том и состоит, что отзыв не должен требовать выкатки образа.

-- +goose Up

-- Перепись ДО — чтобы самопроверка ниже утверждала «эта миграция ни одной роли
-- не тронула» ЗАМЕРОМ, а не выписанным числом. Выписанное устарело бы молча:
-- число ролей растёт с каждым манифестом.
CREATE TEMP TABLE _roles_before ON COMMIT DROP AS
SELECT (SELECT count(*) FROM kacho_iam.roles)                        AS total,
       (SELECT count(*) FROM kacho_iam.roles WHERE is_system)        AS system_rows,
       (SELECT count(*) FROM kacho_iam.roles WHERE owner_module IS NOT NULL) AS module_rows;

ALTER TABLE kacho_iam.roles
  ADD COLUMN retired_at     timestamptz,
  ADD COLUMN retired_reason text,
  ADD COLUMN retired_by     text,
  ADD COLUMN live           boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN kacho_iam.roles.retired_at IS
  'Момент снятия роли. NULL — роль объявлена. Согласие с live держит проверка roles_live_matches_retired, а не писатель.';

COMMENT ON COLUMN kacho_iam.roles.retired_reason IS
  'Причина снятия — то, что арендатор читает у отобранного права. Без неё «отобрали» неотличимо от «сломалось».';

COMMENT ON COLUMN kacho_iam.roles.retired_by IS
  'Кто снял: сегодня — процессный актор пути старта; глагол применения назовёт проверенную личность вызывающего. Колонка NULLABLE, и это отличие от applied_by двух ведомостей (20260903215500, там NOT NULL DEFAULT '') названо намеренно: здесь пустого значения у снятой строки НЕ БЫВАЕТ — производитель заводится тем же изменением, что и колонка, и пишет автора всегда. NULL означает «роль не снята», а не «автора потеряли», и согласие с этим держит roles_live_matches_retired.';

COMMENT ON COLUMN kacho_iam.roles.live IS
  'Живость строки роли. Колонка ОБЫЧНАЯ, а не выражение: на ней стоит референт uniqueness (id, live), а выражение референтом быть не может. Умолчание true делает существующие строки живыми БЕЗ обратного заполнения.';

ALTER TABLE kacho_iam.roles
  ADD CONSTRAINT roles_live_matches_retired CHECK (live = (retired_at IS NULL));

-- Референт живости проекций. Пока на него не ссылается ни один ключ, он
-- проверяет тривиальность (`id` и так первичный ключ) — ссылающихся заводит
-- следующая миграция, и её самопроверка это утверждает.
ALTER TABLE kacho_iam.roles
  ADD CONSTRAINT roles_id_live_uk UNIQUE (id, live);

-- ── САМОПРОВЕРКА ИСХОДА И ПЕРЕПИСЬ ───────────────────────────────────────────
--
-- Утверждается ровно то, что миграция обещает: форма объявлена целиком, ни одна
-- строка не снята, и состояние «снята и жива» отсутствует. Перепись печатается
-- ВСЕГДА — «ноль расхождений» обязано быть отличимо от «ноль прочитанного».

-- +goose StatementBegin
DO $$
DECLARE
    before_total  int;
    before_system int;
    before_module int;
    after_total   int;
    after_live    int;
    cols_present  int;
    check_present int;
    uk_present    int;
    contradicted  int;
BEGIN
    SELECT total, system_rows, module_rows
      INTO before_total, before_system, before_module
      FROM _roles_before;

    SELECT count(*) INTO cols_present
      FROM information_schema.columns
     WHERE table_schema = 'kacho_iam' AND table_name = 'roles'
       AND column_name IN ('retired_at', 'retired_reason', 'retired_by', 'live');
    IF cols_present <> 4 THEN
        RAISE EXCEPTION
            'форма пометки объявлена не целиком: колонок % из 4 (kacho#1913)', cols_present;
    END IF;

    SELECT count(*) INTO check_present
      FROM pg_constraint
     WHERE conrelid = 'kacho_iam.roles'::regclass
       AND conname  = 'roles_live_matches_retired';
    IF check_present <> 1 THEN
        RAISE EXCEPTION
            'проверки roles_live_matches_retired нет (найдено %): состояние «снята и жива» '
            'остаётся ПРЕДСТАВИМЫМ, а не «не производится ни одним входом» (kacho#1913)',
            check_present;
    END IF;

    SELECT count(*) INTO uk_present
      FROM pg_constraint
     WHERE conrelid = 'kacho_iam.roles'::regclass
       AND conname  = 'roles_id_live_uk';
    IF uk_present <> 1 THEN
        RAISE EXCEPTION
            'референта живости (id, live) нет (найдено %): ключам живости проекций '
            'сослаться будет не на что (kacho#1913)', uk_present;
    END IF;

    SELECT count(*) INTO after_total FROM kacho_iam.roles;
    SELECT count(*) INTO after_live  FROM kacho_iam.roles WHERE live;

    IF after_total <> before_total THEN
        RAISE EXCEPTION
            'миграция изменила число ролей: было %, стало % — форма обязана менять '
            'ПРЕДСТАВИМОЕ, а не строки (kacho#1913)', before_total, after_total;
    END IF;
    IF after_live <> after_total THEN
        RAISE EXCEPTION
            'после заведения пометки живых ролей % из % — умолчание не сделало '
            'существующие строки живыми (kacho#1913)', after_live, after_total;
    END IF;

    -- Проверка только что провалидировала каждую строку, поэтому противоречие
    -- здесь невозможно. Она стоит не ради него, а ради того, чтобы перепись
    -- называла ОБЕ величины: «нарушений 0» при «прочитано 0» неотличимо от
    -- исправной работы.
    SELECT count(*) INTO contradicted
      FROM kacho_iam.roles
     WHERE live <> (retired_at IS NULL);
    IF contradicted <> 0 THEN
        RAISE EXCEPTION
            'ролей в состоянии «снята и жива»: % — проверка не удержала согласие (kacho#1913)',
            contradicted;
    END IF;

    RAISE NOTICE
        'пометка снятия роли: осмотрено ролей %, из них системных %, с владельцем-модулем %; '
        'живых после заведения % — снято 0, противоречий %',
        after_total, before_system, before_module, after_live, contradicted;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат снимает ФОРМУ, и это НЕ безобидно на поставляемом дереве.
--
-- Здесь стояло «ни одна роль не оживляется и не снимается, потому что снятых нет
-- — производителя пометки эта миграция не заводит». Верно для этого ФАЙЛА и
-- ложно для ИЗМЕНЕНИЯ: производитель едет тем же изменением, поэтому к моменту
-- отката снятые роли уже есть.
--
-- Что откат делает на самом деле: снятые роли становятся НЕОТЛИЧИМЫ от живых
-- (колонок пометки больше нет), их проекции при этом сняты и не возвращаются
-- ничем, а объяснение отобранного уносит откат миграции ведомости. То есть роль
-- возвращается «живой» и не дающей НИЧЕГО — ровно то состояние, ради устранения
-- которого заведён #1913. Плюс состояние «объявление убрано, роль жива навсегда»
-- снова становится единственно возможным.
--
-- Откат применим на дереве, где производитель ещё не работал; на прочих он
-- требует своего изменения, а не этой строки.
ALTER TABLE kacho_iam.roles
  DROP CONSTRAINT roles_id_live_uk;

ALTER TABLE kacho_iam.roles
  DROP CONSTRAINT roles_live_matches_retired;

ALTER TABLE kacho_iam.roles
  DROP COLUMN live,
  DROP COLUMN retired_by,
  DROP COLUMN retired_reason,
  DROP COLUMN retired_at;
