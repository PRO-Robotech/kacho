-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- orphan_ledger_names_the_cause — ВЕДОМОСТЬ ОТОБРАННОГО различает ПРИЧИНУ
-- переселения.
--
-- Задача продукта #1913. Приёмка
-- services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md
-- (APPROVED круга 4), §2.9; сценарии IAM-RW-1-29, IAM-RW-1-30.
--
-- Третья миграция этой работы, и предмет у неё СВОЙ: там форма пометки и порядок
-- её постановки, здесь — то, что арендатор ЧИТАЕТ у отобранного права. Слить их
-- в один файл значило бы сделать вердикт непрослеживаемым.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧТО НЕВЕРНО БЕЗ РАЗЛИЧИТЕЛЯ
--
-- Ведомость наполняли ДВЕ разные причины, а колонки для них не было:
--
--   строка каталога снята  → правило роли перестало резолвиться
--   САМА РОЛЬ снята        → право отобрано целиком
--
-- Смешав их, мы теряем оба следующих требования §2.9 разом:
--
--  1. ОЖИВЛЕНИЕ роли обязано снять из ведомости строки СВОЕЙ причины и не
--     тронуть чужих: без различителя «очистить свои» невыразимо, и здоровая
--     оживлённая роль несла бы непустой `withdrawn_grants` — при том что
--     контракт поля говорит настоящим временем «что у роли ОТОБРАНО»;
--  2. ПОВТОРНОЕ снятие обязано отвечать ПОСЛЕДНЕЙ причиной. Первичный ключ
--     `(role_id, object_type, verb, source)` причины, автора и момента не несёт,
--     а писатель каталога кладёт строки `ON CONFLICT … DO NOTHING`, — значит
--     цикл «снять → вернуть → снять» оставил бы арендатору причину, автора и
--     момент ПЕРВОГО снятия.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПРИЧИНА ВХОДИТ В ПЕРВИЧНЫЙ КЛЮЧ — И ЭТО РЕШЕНИЕ, А НЕ УДОБСТВО
--
-- Обе причины могут относиться к ОДНОЙ паре «тип объекта × глагол» одной роли:
-- строка каталога снята, а позже снята и сама роль. Оставь причину вне ключа —
-- и вторая запись перезаписала бы первую, то есть объяснение «правило перестало
-- резолвиться» исчезло бы в момент отзыва роли и не вернулось бы при оживлении.
-- Сценарий IAM-RW-1-30 требует ровно обратного: строки чужой причины ОСТАЮТСЯ.
--
-- Умолчание `'catalog_retired'` названо ЯВНО, потому что все существующие строки
-- ведомости произведены полосой каталога: другой полосы до этой работы не было
-- ни одной (`catalog_consequence_sql.go` — единственный писатель).
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧЕГО ЭТА МИГРАЦИЯ НЕ ДЕЛАЕТ
--
-- Полосу каталога она НЕ меняет: `ON CONFLICT … DO NOTHING` там остаётся, и
-- вопрос «обязано ли второе снятие строки каталога отвечать своей причиной»
-- вынесен остатком §9 Р10 приёмки, а не решён здесь молча.

-- +goose Up

CREATE TEMP TABLE _orphan_before ON COMMIT DROP AS
SELECT count(*) AS rows_total FROM kacho_iam.role_grant_orphan;

ALTER TABLE kacho_iam.role_grant_orphan
  ADD COLUMN cause text NOT NULL DEFAULT 'catalog_retired';

ALTER TABLE kacho_iam.role_grant_orphan
  ADD CONSTRAINT role_grant_orphan_cause_known
    CHECK (cause IN ('catalog_retired', 'role_retired'));

COMMENT ON COLUMN kacho_iam.role_grant_orphan.cause IS
  'ПОЧЕМУ строка переселена: catalog_retired — снята строка каталога, на которую ссылалось правило; role_retired — снята сама роль. Причина входит в первичный ключ, потому что обе могут относиться к одной паре «тип × глагол»: оживление роли снимает строки СВОЕЙ причины и не трогает чужих.';

-- Смена первичного ключа. Форма взята у соседа (`0027`), у которого тот же
-- предмет: ключ расширяется, когда прежний перестаёт различать две законные
-- популяции одной таблицы.
ALTER TABLE kacho_iam.role_grant_orphan
  DROP CONSTRAINT role_grant_orphan_pkey;

ALTER TABLE kacho_iam.role_grant_orphan
  ADD CONSTRAINT role_grant_orphan_pkey
    PRIMARY KEY (role_id, object_type, verb, source, cause);

-- ── САМОПРОВЕРКА ИСХОДА И ПЕРЕПИСЬ ───────────────────────────────────────────

-- +goose StatementBegin
DO $$
DECLARE
    before_rows int;
    after_rows  int;
    catalog_rows int;
    role_rows    int;
    pk_cols      int;
BEGIN
    SELECT rows_total INTO before_rows FROM _orphan_before;
    SELECT count(*)   INTO after_rows  FROM kacho_iam.role_grant_orphan;

    IF after_rows <> before_rows THEN
        RAISE EXCEPTION
            'миграция изменила число строк ведомости: было %, стало % — различитель '
            'обязан менять ПРЕДСТАВИМОЕ, а не строки (kacho#1913)', before_rows, after_rows;
    END IF;

    SELECT cardinality(c.conkey) INTO pk_cols
      FROM pg_constraint c
     WHERE c.conrelid = 'kacho_iam.role_grant_orphan'::regclass
       AND c.conname  = 'role_grant_orphan_pkey';
    IF pk_cols <> 5 THEN
        RAISE EXCEPTION
            'первичный ключ ведомости несёт % колонок вместо пяти: причина в него не '
            'вошла, и две законные популяции остались неразличимыми (kacho#1913)', pk_cols;
    END IF;

    SELECT count(*) INTO catalog_rows
      FROM kacho_iam.role_grant_orphan WHERE cause = 'catalog_retired';
    SELECT count(*) INTO role_rows
      FROM kacho_iam.role_grant_orphan WHERE cause = 'role_retired';

    IF role_rows <> 0 THEN
        RAISE EXCEPTION
            'строк причины «роль снята» уже %, при том что производителя отзыва роли '
            'эта миграция не заводит (kacho#1913)', role_rows;
    END IF;

    RAISE NOTICE
        'причина переселения: осмотрено строк ведомости %, из них снят каталог %, '
        'снята роль %; колонок первичного ключа %',
        after_rows, catalog_rows, role_rows, pk_cols;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат СНИМАЕТ строки причины «роль снята» — иначе прежний первичный ключ их
-- не примет: две причины на одной паре сложились бы в дубль. Это единственное
-- место всей работы, где откат теряет данные, и оно названо прямо.
DELETE FROM kacho_iam.role_grant_orphan WHERE cause = 'role_retired';

ALTER TABLE kacho_iam.role_grant_orphan
  DROP CONSTRAINT role_grant_orphan_pkey;

ALTER TABLE kacho_iam.role_grant_orphan
  ADD CONSTRAINT role_grant_orphan_pkey
    PRIMARY KEY (role_id, object_type, verb, source);

ALTER TABLE kacho_iam.role_grant_orphan
  DROP CONSTRAINT role_grant_orphan_cause_known;

ALTER TABLE kacho_iam.role_grant_orphan DROP COLUMN cause;
