-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Объявленное отсутствие домена величин доезжает до ПУТИ ЗАПРОСА.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#2216`; приёмка
-- `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
-- решение `Д2` («что означает домен величин не развёрнут»), решение `Д7`
-- (предмет действует уже сегодня), сценарии `KAN-Q4-03` и `KAN-Q4-05`.
--
-- ЧТО БЫЛО НЕВЕРНО. Ручка домена величин принимает два законных значения: адрес
-- соседа либо слово `not-deployed`. На втором процесс поднимается, тянущий не
-- заводится, и в журнале появляется одна строка — про тянущего. Путь запроса при
-- этом не менялся ничем: списывающий триггер по-прежнему требовал строки учёта, а
-- пишет их только проекция дельты, которую зовут лишь изнутри фундамента. Значит
-- КАЖДАЯ вставка считаемого вида в проекте без строк отвергалась `KQ002` —
-- «потолок не назван». Оператор читал обещание про тянущего и получал
-- неработающие мутации.
--
-- ЧТО ВЫБРАНО. При объявленном отсутствии домена величин потолок НЕ ДЕЙСТВУЕТ:
-- отказа нет ни `KQ001`, ни `KQ002`. Довод — `Д2` п. 1: потолок здесь не «не
-- назначен администратором», а НЕ НАЗНАЧАЕМ НИКЕМ, и отказ обвинял бы арендатора
-- в том, чего он исправить не может.
--
-- ПОЧЕМУ СПИСАНИЕ ОСТАЁТСЯ, И ОСТАЁТСЯ БЕЗУСЛОВНЫМ. Соблазн выглядит проще:
-- пусть производитель отказа молча возвращается. Он ломает счётчик. Условный
-- `UPDATE … AND used < limit_value` даёт ноль строк И когда строки учёта нет, И
-- когда она полна; производителя зовут уже ПОСЛЕ этого. Вернувшись молча, он
-- оставил бы вставку без списания — и на строке, заведённой прежде (когда
-- авторитет был развёрнут), потребление начало бы отставать от числа строк,
-- которые оно считает. Пока домен отсутствует, отставание невидимо, а становится
-- видимым ровно в день, когда авторитет вернут. Поэтому предикат потолка снимается
-- ЗДЕСЬ, в самом списывающем операторе, и счёт продолжается.
--
-- Состояние `used > limit_value` этим не заводится впервые и схемой не запрещено:
-- `CHECK (used <= limit_value)` не ставится намеренно — иначе понижение предела
-- ниже потребления перестало бы быть выразимым (шапка миграции учёта).
--
-- ПОЧЕМУ ПРАВИТСЯ ТРИГГЕР, А НЕ ОБЩИЙ ПРОИЗВОДИТЕЛЬ ОТКАЗА. Производитель
-- (`kacho_quota_refuse`) рендерится ОДНИМ шаблоном шести владельцам, и один из них
-- — служба доступа: она сама авторитет, таблицы курсора у неё нет и быть не
-- должно. Чтение курсора в шаблоне сломало бы ей отказ во время исполнения, а не
-- при применении миграции. Плюс её цепь сведена в применённую первичную, править
-- которую нельзя (ban #5), — рендер для неё обязан остаться побайтово прежним.
-- Списывающий же оператор шаблоном не порождается: он свой у каждого владельца,
-- и различие между владельцами здесь ОБЪЯВЛЕНО, а не выведено во время работы.
--
-- ПОЧЕМУ ПОДПИСЬ ВЕТВИ ОСТАЁТСЯ ВЕРНОЙ. Производитель отказа по-прежнему всегда
-- возбуждает исключение — при отсутствующем домене его просто НЕ ЗОВУТ. Наивная
-- починка сделала бы подпись ложью сразу у пяти владельцев.
--
-- РЕЖИМ ЧИТАЕТСЯ В ТОЙ ЖЕ ТРАНЗАКЦИИ, что и вставка строки ресурса (`Д2` п. 3,
-- ban #10): решение «отвергать или нет» и само списание — один оператор одной
-- транзакции. Два места об одном режиме разошлись бы молча ровно там, где
-- расхождение означает снятый контроль.
--
-- «НЕИЗВЕСТНО» — НЕ «ОТСУТСТВУЕТ». Словарь состояния курсора закрыт тремя
-- значениями, и умолчание `unknown` означает «объявления ещё не было ни от одного
-- подъёма». Послабление наступает ТОЛЬКО на явно объявленном отсутствии: иначе
-- база, на которую миграции применены, а процесс ещё не поднимался, вела бы себя
-- как установка без потолков.
--
-- Тело заменяется целиком (`CREATE OR REPLACE`): применённую миграцию править
-- нельзя (ban #5). Всё, кроме двух названных мест, побайтово равно телу из
-- `0023_project_resource_quotas.sql`.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind    text := TG_ARGV[0];
    v_row     jsonb;
    v_project text;
    v_absent  boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_row := to_jsonb(OLD);
    ELSE
        v_row := to_jsonb(NEW);
    END IF;

    v_project := v_row ->> 'project_id';
    IF v_project IS NULL OR v_project = '' THEN
        RAISE EXCEPTION 'quota: row of % carries no project_id', TG_TABLE_NAME
            USING ERRCODE = 'KQ003';
    END IF;

    IF TG_OP = 'DELETE' THEN
        -- Возврат — в той же транзакции, что удаление строки ресурса. Возврат
        -- вне её оставил бы счётчик завышенным при откате: проект платил бы
        -- местом за ресурс, которого нет. GREATEST не даёт уйти ниже нуля, если
        -- строка учёта была заведена позже самих ресурсов.
        --
        -- Объявление домена величин здесь не читается намеренно: возврат места не
        -- отвергает никогда, поэтому режим на него не влияет.
        UPDATE kacho_storage.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Объявленное состояние домена величин, прочитанное в ТОЙ ЖЕ транзакции, что
    -- и вставка строки ресурса. Отсутствие строки курсора даёт NULL и ведёт себя
    -- как «домен развёрнут»: послабление не наступает от того, что чего-то не
    -- нашлось.
    SELECT authority_state = 'not-deployed' INTO v_absent
      FROM kacho_storage.quota_sync_cursor
     WHERE id = 'limits';

    -- Списание. Единственный оператор, берущий блокировку строки: второй
    -- писатель ждёт коммита первого и видит его результат. Чтение с последующим
    -- сравнением этого не даёт — между ними помещается чужая запись, и оба
    -- создателя прошли бы проверку, увидев одно и то же свободное место (ban #10).
    UPDATE kacho_storage.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND (COALESCE(v_absent, false) OR used < limit_value);

    IF FOUND THEN
        RETURN NULL;
    END IF;

    -- Домен величин объявлен отсутствующим: потолок не назначаем никем, поэтому
    -- отказа нет. Строки учёта при этом может не быть вовсе — тогда и списывать
    -- было нечего, и счётчик не лжёт: его не существует.
    IF COALESCE(v_absent, false) THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_storage.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;

COMMENT ON FUNCTION kacho_storage.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. When the limit authority is DECLARED absent no ceiling '
    'applies and nothing is refused, yet the charge still happens unconditionally '
    'so that usage does not silently fall behind the rows it counts; "unknown" is '
    'not "absent". The refusal itself is produced by kacho_quota_refuse, shared '
    'with the advisory band, so the two bands cannot word it differently';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- Откат возвращает тело, не знающее объявления домена величин: потолок снова
-- действует при любом объявлении, и вставка в проекте без строк учёта снова
-- отвергается. Это ровно то состояние, ради снятия которого файл заведён.
CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind    text := TG_ARGV[0];
    v_row     jsonb;
    v_project text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_row := to_jsonb(OLD);
    ELSE
        v_row := to_jsonb(NEW);
    END IF;

    v_project := v_row ->> 'project_id';
    IF v_project IS NULL OR v_project = '' THEN
        RAISE EXCEPTION 'quota: row of % carries no project_id', TG_TABLE_NAME
            USING ERRCODE = 'KQ003';
    END IF;

    IF TG_OP = 'DELETE' THEN
        UPDATE kacho_storage.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    UPDATE kacho_storage.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_storage.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;
-- +goose StatementEnd
