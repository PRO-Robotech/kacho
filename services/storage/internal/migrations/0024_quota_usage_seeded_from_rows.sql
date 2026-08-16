-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Потребление перестаёт начинаться с нуля: оно СЧИТАЕТСЯ по строкам ресурса.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#419`.
--
-- ЧТО БЫЛО НЕВЕРНО. Строка учёта заводилась с `used = 0` безусловно, а
-- сопроводительный комментарий объяснял это тем, что потребление создаёт
-- триггер. Утверждение верно ровно для проекта, у которого на момент заведения
-- строки ресурсов НЕТ, — и ложно для всякого другого. На боевом стенде это дало
-- расхождение по каждому виду сразу у vpc: проект с четырьмя группами правил числился
-- за одну, то есть предел в 64 разрешал создать ещё 64 сверх фактических
-- четырёх. Механизм присутствовал, был провязан, исполнялся на каждой мутации и
-- не отказал ни разу за всю свою жизнь.
--
-- ПОЧЕМУ ЭТО НЕ РАЗОВАЯ БЕДА, И ПОЧЕМУ ОДНОГО ПЕРЕСЧЁТА МАЛО. Состояние
-- «ресурсы есть, строки учёта нет» воспроизводится КАЖДЫЙ раз, когда учёт
-- приходит на непустую базу: так было здесь, так будет у всякого владельца, чью
-- миграцию учёта применят к живому проекту, и так будет у всякого вида,
-- добавленного в каталог позже своих ресурсов. Разовый пересчёт чинит прошлое и
-- ничего не обещает про будущее — поэтому здесь заводится ФУНКЦИЯ, которой
-- пользуется и материализация на живом пути, и пересчёт ниже, и проверка
-- расхождения. Инвариант «потребление равно числу строк» после этого держится
-- ПОСТРОЕНИЕМ, а не тем, что его однажды восстановили.
--
-- ПОЧЕМУ СЧЁТ НЕ МОЖЕТ РАЗЪЕХАТЬСЯ С ТРИГГЕРОМ. Пока строки учёта нет, вставка
-- строки ресурса ОТВЕРГАЕТСЯ («не сказано» = отказ). Значит между счётом и
-- вставкой строки учёта никакая чужая транзакция не может добавить ресурс:
-- множество, которое считают, заморожено by construction, а не блокировкой.
-- Как только строка появилась, дальнейшее ведёт триггер условным `UPDATE`.
-- Отсюда «прочитал → записал» здесь НЕ возникает (ban #10): читать и писать
-- нечего одновременно.
--
-- ПОЧЕМУ ОТОБРАЖЕНИЕ «ВИД → ТАБЛИЦА» ВЫВОДИТСЯ ИЗ ТРИГГЕРОВ, А НЕ ВЫПИСЫВАЕТСЯ.
-- Выписанный перечень — второе место об одном предмете: он разойдётся с набором
-- триггеров молча, и разойдётся именно там, где расхождение не видно — в счёте,
-- который «выглядит правдоподобно». Здесь отображение ЧИТАЕТСЯ у тех самых
-- триггеров, что ведут списание, поэтому пересчёт и списание не могут говорить о
-- разных множествах строк. Побочное следствие, ради которого это и сделано:
-- владелец, заводящий учёт следующим, копирует ЭТОТ файл, поменяв одно имя
-- схемы, и не пишет ни одной своей строки отображения.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- -----------------------------------------------------------------------------
-- Сколько строк ресурса стоит за этой строкой учёта — по факту, а не по счётчику.
-- -----------------------------------------------------------------------------
-- Возвращает NULL, когда вид не списывается ни одним триггером: у такого вида
-- потребления не существует, и это ОТЛИЧАЕТСЯ от «потребление равно нулю».
-- Разница несущая — ноль означает «считали и не нашли», NULL означает «считать
-- нечем», и второе есть находка (вид объявлен в каталоге, а производителя
-- списания у него нет).
--
-- РАЗБОР АРГУМЕНТОВ ТРИГГЕРА. Первый — всегда вид. Второй в этом дереве бывает
-- двух смыслов, и различает их не имя, а ТИП столбца, на который он указывает:
-- булев столбец — признак системного происхождения (такой ребёнок предела
-- арендатора не тратит), текстовый — идентификатор родителя вложенного вида.
-- Тип столбца — факт схемы, а не догадка о названии; деривация смысла из имени
-- в этом дереве запрещена прецедентом, и здесь её нет.
CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_used_actual(
    v_carrier_type text,
    v_carrier_id   text,
    v_kind         text
)
RETURNS bigint
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_schema  constant text := 'kacho_storage';
    v_table   text;
    v_arg1    text;
    v_arg2    text;
    v_syscol  text;
    v_keycol  text;
    v_pred    text := '';
    v_count   bigint;
BEGIN
    SELECT c.relname, g.a[2], g.a[3]
      INTO v_table, v_arg1, v_arg2
      FROM pg_trigger t
      JOIN pg_class     c ON c.oid = t.tgrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_proc      p ON p.oid = t.tgfoid
      CROSS JOIN LATERAL (
          SELECT regexp_match(
                     pg_get_triggerdef(t.oid),
                     'kacho_quota_count\(''([^'']*)''(?:,\s*''([^'']*)'')?(?:,\s*''([^'']*)'')?\)'
                 ) AS a
      ) g
     WHERE NOT t.tgisinternal
       AND n.nspname  = v_schema
       AND p.proname  = 'kacho_quota_count'
       AND g.a IS NOT NULL
       AND (
             (v_carrier_type = 'project' AND g.a[1] = v_kind)
          OR (v_carrier_type <> 'project' AND g.a[3] = v_kind)
           )
     LIMIT 1;

    IF v_table IS NULL THEN
        RETURN NULL; -- вид не списывается ничем: потребления у него нет
    END IF;

    -- Ось носителя. Проектная считает по проекту; родительская — по столбцу,
    -- которым триггер связывает ребёнка с родителем.
    IF v_carrier_type = 'project' THEN
        v_keycol := 'project_id';
    ELSE
        v_keycol := v_arg1;
        IF v_keycol IS NULL OR v_keycol = '' THEN
            RETURN NULL;
        END IF;
    END IF;

    -- Системный ребёнок предела арендатора не тратит — значит и в счёт не идёт.
    -- Признак опознаётся по типу столбца (см. шапку), а не по его имени.
    IF v_carrier_type = 'project' AND v_arg1 IS NOT NULL AND v_arg1 <> '' THEN
        SELECT a.attname
          INTO v_syscol
          FROM pg_attribute a
          JOIN pg_class     c ON c.oid = a.attrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = v_schema
           AND c.relname = v_table
           AND a.attname = v_arg1
           AND a.atttypid = 'boolean'::regtype
           AND a.attnum > 0
           AND NOT a.attisdropped;

        IF v_syscol IS NOT NULL THEN
            v_pred := format(' AND COALESCE(%I, false) = false', v_syscol);
        END IF;
    END IF;

    EXECUTE format('SELECT count(*) FROM %I.%I WHERE %I = $1%s',
                   v_schema, v_table, v_keycol, v_pred)
       INTO v_count
      USING v_carrier_id;

    RETURN v_count;
END;
$$;

COMMENT ON FUNCTION kacho_storage.kacho_quota_used_actual(text, text, text) IS
    'how many resource rows actually stand behind this accounting row. The '
    'kind-to-table mapping is READ FROM THE CHARGING TRIGGERS themselves, so a '
    'recount and a charge can never disagree about which rows they mean. NULL '
    '(not zero) when no trigger charges the kind — "nothing counts it" is a '
    'finding, "it counts to zero" is not';

-- -----------------------------------------------------------------------------
-- Сверка и пересчёт — одна функция, два режима.
-- -----------------------------------------------------------------------------
-- Режим сверки (`v_apply => false`) ничего не пишет и возвращает строки, чей
-- счётчик разошёлся с фактом. Пустой результат — это и есть утверждение
-- «расхождение невозможно ни для одного вида», и он проверяем в той же
-- транзакции, что и любая мутация.
--
-- ПОЧЕМУ ПУСТОЙ РЕЗУЛЬТАТ — НЕ «НИЧЕГО НЕ ПРОВЕРИЛИ». Число осмотренных строк
-- печатается отдельно (NOTICE), поэтому «ноль расхождений» отличимо от «ноль
-- прочитанного» — без этого сверка на пустой таблице выглядела бы как
-- доказательство исправности.
--
-- ПОЧЕМУ ПОСТРОЧНО, А НЕ ОДНИМ ОПЕРАТОРОМ НА ТАБЛИЦУ. Строк учёта немного (число
-- проектов × число видов), а счёт по каждой опирается на индекс по проекту,
-- который есть у каждой считаемой таблицы. Обновляются ТОЛЬКО разошедшиеся строки,
-- поэтому блокировка не берётся на верные — пересчёт на большой базе не держит
-- таблицу целиком.
CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_recount(v_apply boolean DEFAULT true)
RETURNS TABLE (
    carrier_type text,
    carrier_id   text,
    kind         text,
    used_before  bigint,
    used_actual  bigint
)
LANGUAGE plpgsql
AS $$
DECLARE
    r        record;
    v_actual bigint;
    v_seen   bigint := 0;
    v_diff   bigint := 0;
BEGIN
    FOR r IN
        SELECT q.carrier_type AS ct, q.carrier_id AS ci, q.kind AS k, q.used AS u
          FROM kacho_storage.project_resource_quotas q
         ORDER BY q.carrier_type, q.carrier_id, q.kind
    LOOP
        v_seen := v_seen + 1;
        v_actual := kacho_storage.kacho_quota_used_actual(r.ct, r.ci, r.k);

        CONTINUE WHEN v_actual IS NULL OR v_actual = r.u;

        v_diff := v_diff + 1;

        IF v_apply THEN
            UPDATE kacho_storage.project_resource_quotas q
               SET used = v_actual, updated_at = now()
             WHERE q.carrier_type = r.ct AND q.carrier_id = r.ci AND q.kind = r.k;
        END IF;

        carrier_type := r.ct;
        carrier_id   := r.ci;
        kind         := r.k;
        used_before  := r.u;
        used_actual  := v_actual;
        RETURN NEXT;
    END LOOP;

    RAISE NOTICE 'quota recount: % accounting row(s) inspected, % diverged', v_seen, v_diff;
END;
$$;

COMMENT ON FUNCTION kacho_storage.kacho_quota_recount(boolean) IS
    'verify (false) or repair (true) the invariant "used equals the number of '
    'resource rows". Verify mode writes nothing and returns the diverging rows, '
    'so an empty result IS the assertion; the count of rows inspected is raised '
    'as a notice so that "no divergence" stays distinguishable from "nothing read"';

-- -----------------------------------------------------------------------------
-- Прошлое приводится в согласие с фактом.
-- -----------------------------------------------------------------------------
-- Единственное место, где `used` пишется не триггером, — и оно ограничено этой
-- миграцией и функцией выше. Дальше инвариант держит материализация, которая с
-- этого момента затравляет строку фактом, а не нулём.
DO $$
DECLARE
    v_fixed bigint;
BEGIN
    SELECT count(*) INTO v_fixed FROM kacho_storage.kacho_quota_recount(true);
    RAISE NOTICE 'quota usage backfill: % accounting row(s) corrected', v_fixed;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- Счётчики назад не «раз-считываются»: вернуть им заниженное значение значило бы
-- воспроизвести дефект, ради которого миграция написана. Откат снимает ТОЛЬКО
-- функции; данные остаются верными, и это осознанная асимметрия.
DROP FUNCTION IF EXISTS kacho_storage.kacho_quota_recount(boolean);
DROP FUNCTION IF EXISTS kacho_storage.kacho_quota_used_actual(text, text, text);
-- +goose StatementEnd
