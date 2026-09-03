-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- new_grant_on_a_retired_role_is_refused — НОВАЯ ВЫДАЧА на снятую роль
-- отвергается, а пережившая — остаётся правимой.
--
-- Задача продукта #1913. Приёмка
-- services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md
-- (APPROVED круга 4), §2.4; сценарии IAM-RW-1-16, IAM-RW-1-26, IAM-RW-1-31.
-- Форма стража и разбор четырёх случаев — запись решения
-- services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ПОЧЕМУ ТРИГГЕР, А НЕ КЛЮЧ — ключом это НЕВЫРАЗИМО by construction
--
-- Единственный ключ на роль у выдачи — `access_bindings_role_fk … ON DELETE
-- RESTRICT`, и он стоит на `id`, без живости. Перевести его на `(id, live)`
-- НЕЛЬЗЯ: ключ судит ОБЕ стороны сразу — и вставку строки выдачи, и правку
-- строки роли. Тогда либо снятие роли с живой выдачей отвергалось бы (а §2.4
-- требует обратного — выдачи ПЕРЕЖИВАЮТ снятие, на этом стоит обратимость),
-- либо снятие уносило бы выдачи (что §2.4 отвергает как уничтожение
-- обратимости). Одним ключом два противоположных требования не выражаются.
--
-- Проверка в коде запрещена (ban #10): у выдачи больше одного писателя —
-- публичный путь и посев, — и инвариант, стоящий на одном из них, инвариантом не
-- является.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- СУДИТСЯ ПОЯВЛЕНИЕ ССЫЛКИ, А НЕ СУЩЕСТВОВАНИЕ СТРОКИ
--
-- Пережившая выдача — ФАКТ ПРОШЛОГО, который платформа объясняет; новая была бы
-- ОБЕЩАНИЕМ, которого продукт не держит. Различие выражается ранним выходом:
--
--   INSERT                                 → ссылка появляется   → судится
--   UPDATE, меняющий role_id               → ссылка появляется   → судится
--   UPDATE, НЕ меняющий role_id            → ссылки не появилось → проходит
--
-- Без раннего выхода пережившая выдача на снятой роли перестала бы принимать
-- ЛЮБОЙ `UPDATE` — её нельзя было бы ни отозвать, ни переметить, — то есть §2.4
-- противоречила бы собственной следующей строке. Перевод выдачи НА снятую роль
-- при этом отвергается законно: это новая ссылка.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ОТКАЗ — 23000, А НЕ 23514, И ЭТО ПЕРЕМЕРЕНО, А НЕ ВЫБРАНО
--
-- `23514` уходит в ветвь проверки значения и отвечает `INVALID_ARGUMENT` — то
-- есть буквальное исполнение «отвергается проверкой» дало бы клиенту «негоден
-- ввод» там, где негодно СОСТОЯНИЕ платформы, и опровергло бы сценарий -16.
-- Класс `23000` (`integrity_constraint_violation`) отвечает
-- `FAILED_PRECONDITION` целиком, а незнакомая связь получает общий текст без
-- утечки сообщения сервера наружу.
--
-- Имя связи названо явно (`access_bindings_role_is_live`), потому что по нему
-- маппер выбирает текст: без имени отказ пришёл бы общим «resource state does not
-- permit this operation» и не назвал бы роль.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЗАМОК — `FOR SHARE`, А НЕ `FOR KEY SHARE`
--
-- Довод записи решения гласил: снятие правит НЕКЛЮЧЕВОЙ столбец и берёт
-- `FOR NO KEY UPDATE`, с которым `FOR KEY SHARE` не конфликтует. НА СЕГОДНЯШНЕЙ
-- СХЕМЕ ЭТО НЕВЕРНО, и сказать надо прямо: референт живости
-- `roles_id_live_uk UNIQUE (id, live)` — единственный индекс, содержащий `live`,
-- — делает её КЛЮЧЕВОЙ, поэтому правка берёт `FOR UPDATE`, и `FOR KEY SHARE`
-- конфликтует с ним тоже. Перемерено на применённом дереве (`mode
-- ExclusiveLock`), инъекция подтвердила: страж с `FOR KEY SHARE` отверг все
-- восемь конкурентных вставок, ровно как с `FOR SHARE`.
--
-- ВЫВОД ОТ ЭТОГО НЕ МЕНЯЕТСЯ, и доводов у него теперь ДВА: `FOR SHARE` верен и
-- когда живость ключевая (сегодня), и когда она перестанет ею быть — референт
-- снят, индекс перестроен, — а `FOR KEY SHARE` верен только в первом случае.
-- Замок выбран по более слабой посылке, и это дороже, чем совпасть с
-- сегодняшней схемой. Без замка вовсе вставка проходит: это замерено записью
-- решения и остаётся верным.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- ЧЕГО ЭТА МИГРАЦИЯ НЕ ДЕЛАЕТ
--
-- Уже существующие выдачи она НЕ трогает: ни одной строки не помечает и не
-- удаляет. Выдачи переживают снятие роли — это условие обратимости, а не
-- послабление: снеся их, мы сделали бы оживление бессмысленным, потому что
-- кому роль была выдана, не знал бы никто.

-- +goose Up

CREATE TEMP TABLE _bindings_before ON COMMIT DROP AS
SELECT count(*) AS rows_total FROM kacho_iam.access_bindings;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_iam.access_bindings_role_is_live() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    role_live boolean;
BEGIN
    -- РАННИЙ ВЫХОД. Правка, не меняющая ссылку, новой ссылки не создаёт, и
    -- судить её нечем: пережившая выдача обязана оставаться отзываемой и
    -- перемечаемой.
    IF TG_OP = 'UPDATE' AND NEW.role_id IS NOT DISTINCT FROM OLD.role_id THEN
        RETURN NEW;
    END IF;

    -- Замок `FOR SHARE`, а не `FOR KEY SHARE`. На сегодняшней схеме конфликтуют
    -- оба (живость ключевая из-за `roles_id_live_uk`), и различает их только
    -- будущее: `FOR SHARE` останется верным, если референт когда-нибудь снимут.
    -- Разбор и замер — шапка файла.
    SELECT r.live INTO role_live
      FROM kacho_iam.roles r
     WHERE r.id = NEW.role_id
       FOR SHARE;

    -- Строки нет вовсе — это предмет ключа `access_bindings_role_fk`, а не
    -- стража: сказать здесь своё значило бы завести второе место об одном
    -- предмете, и разошлись бы они молча.
    IF role_live IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT role_live THEN
        RAISE EXCEPTION
            'role % is retired and cannot receive a new access binding', NEW.role_id
            USING ERRCODE = '23000',
                  CONSTRAINT = 'access_bindings_role_is_live';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION kacho_iam.access_bindings_role_is_live() IS
  'Новая ССЫЛКА на снятую роль отвергается 23000 с именем связи access_bindings_role_is_live. Судится ПОЯВЛЕНИЕ ссылки, а не существование строки: UPDATE, не меняющий role_id, выходит рано — иначе пережившая выдача перестала бы приниматься к отзыву и переметке. Ключом это невыразимо: ключ судит обе стороны сразу, а снятие роли обязано выдачи ПЕРЕЖИВАТЬ (kacho#1913).';

DROP TRIGGER IF EXISTS access_bindings_role_is_live_trg ON kacho_iam.access_bindings;

CREATE TRIGGER access_bindings_role_is_live_trg
    BEFORE INSERT OR UPDATE ON kacho_iam.access_bindings
    FOR EACH ROW EXECUTE FUNCTION kacho_iam.access_bindings_role_is_live();

-- ── САМОПРОВЕРКА ИСХОДА И ПЕРЕПИСЬ ───────────────────────────────────────────

-- +goose StatementBegin
DO $$
DECLARE
    before_rows int;
    after_rows  int;
    trg_present int;
    on_retired  int;
BEGIN
    SELECT rows_total INTO before_rows FROM _bindings_before;
    SELECT count(*)   INTO after_rows  FROM kacho_iam.access_bindings;

    IF after_rows <> before_rows THEN
        RAISE EXCEPTION
            'миграция изменила число выдач: было %, стало % — страж обязан менять '
            'ПРЕДСТАВИМОЕ, а не строки (kacho#1913)', before_rows, after_rows;
    END IF;

    SELECT count(*) INTO trg_present
      FROM pg_trigger
     WHERE tgrelid = 'kacho_iam.access_bindings'::regclass
       AND tgname  = 'access_bindings_role_is_live_trg'
       AND NOT tgisinternal;
    IF trg_present <> 1 THEN
        RAISE EXCEPTION
            'стража новой выдачи нет (найдено %): выдача на снятую роль осталась бы '
            'обещанием, которого продукт не держит (kacho#1913)', trg_present;
    END IF;

    -- Перепись обязана называть ОБЕ величины: «нарушений 0» при «прочитано 0»
    -- неотличимо от исправной работы. Существующие выдачи на снятых ролях
    -- ЗАКОННЫ и стражем не трогаются — они факт прошлого.
    SELECT count(*) INTO on_retired
      FROM kacho_iam.access_bindings b
      JOIN kacho_iam.roles r ON r.id = b.role_id
     WHERE NOT r.live;

    RAISE NOTICE
        'страж новой выдачи: осмотрено выдач %, из них на снятых ролях % '
        '(они ЗАКОННЫ и не трогаются — судится появление ссылки, а не строка)',
        after_rows, on_retired;
END;
$$;
-- +goose StatementEnd

-- +goose Down
--
-- Откат снимает ЗАПРЕТ, а не данные: ни одна выдача не меняется. После него
-- новая выдача на снятую роль снова становится представимой — то есть продукт
-- снова начнёт обещать право, которого не даёт.
DROP TRIGGER IF EXISTS access_bindings_role_is_live_trg ON kacho_iam.access_bindings;
DROP FUNCTION IF EXISTS kacho_iam.access_bindings_role_is_live();
