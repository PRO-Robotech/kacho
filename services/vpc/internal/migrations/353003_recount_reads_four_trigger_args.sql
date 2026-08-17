-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Пересчёт умеет читать ЧЕТЫРЁХаргументный триггер списания.
-- =============================================================================
-- Задача `PRO-Robotech/kacho#353`.
--
-- ЧТО СЛОМАЛОСЬ БЫ БЕЗ ЭТОГО ФАЙЛА. `kacho_quota_used_actual` читает отображение
-- «вид → таблица» ИЗ САМИХ ТРИГГЕРОВ СПИСАНИЯ — именно затем, чтобы пересчёт и
-- списание не могли разойтись в том, какие строки они считают. Разбор опирался на
-- выражение, допускавшее не более ТРЁХ аргументов; вложенная ось добавила
-- четвёртый, и триггеры трёх таблиц перестали опознаваться вовсе.
--
-- Отказ при этом ТИХИЙ и потому опасный: функция возвращает NULL («вид не
-- списывается ничем»), вызывающий приводит его к нулю, и материализация заводит
-- строку учёта с потреблением 0 при существующих ресурсах. Проект, чьи сети
-- старше учёта, получил бы право завести столько же СВЕРХ фактического — то есть
-- ровно тот дефект, ради которого миграция 0042 и написана.
--
-- Поймано пробой `TestQuotaMaterialise_SystemChildrenAreNotSeeded`, а не обзором:
-- со стороны кода вложенная ось и пересчёт не связаны ничем, кроме текста
-- определения триггера.
--
-- ЧТО ИМЕННО ИЗМЕНЕНО. Выражение принимает до четырёх аргументов; вложенный вид
-- ищется в ЧЕТВЁРТОМ, а ключ родителя берётся из ТРЕТЬЕГО. Прежние триггеры с
-- одним-двумя аргументами продолжают опознаваться — необязательные группы этого
-- и не требуют.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_used_actual(
    v_carrier_type text,
    v_carrier_id   text,
    v_kind         text
)
RETURNS bigint
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_schema  constant text := 'kacho_vpc';
    v_table   text;
    v_arg1    text;
    v_arg2    text;
    v_arg3    text;
    v_syscol  text;
    v_keycol  text;
    v_pred    text := '';
    v_count   bigint;
BEGIN
    SELECT c.relname, g.a[2], g.a[3], g.a[4]
      INTO v_table, v_arg1, v_arg2, v_arg3
      FROM pg_trigger t
      JOIN pg_class     c ON c.oid = t.tgrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_proc      p ON p.oid = t.tgfoid
      CROSS JOIN LATERAL (
          SELECT regexp_match(
                     pg_get_triggerdef(t.oid),
                     'kacho_quota_count\(\s*''([^'']*)''(?:,\s*''([^'']*)'')?(?:,\s*''([^'']*)'')?(?:,\s*''([^'']*)'')?\s*\)'
                 ) AS a
      ) g
     WHERE NOT t.tgisinternal
       AND n.nspname  = v_schema
       AND p.proname  = 'kacho_quota_count'
       AND g.a IS NOT NULL
       AND (
             (v_carrier_type = 'project' AND g.a[1] = v_kind)
          OR (v_carrier_type <> 'project' AND g.a[4] = v_kind)
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
        v_keycol := v_arg2;
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


COMMENT ON FUNCTION kacho_vpc.kacho_quota_used_actual(text, text, text) IS
    'how many resource rows actually stand behind this accounting row. The '
    'kind-to-table mapping is READ FROM THE CHARGING TRIGGERS themselves, so a '
    'recount and a charge can never disagree about which rows they mean. Reads up '
    'to FOUR trigger arguments: kind, system-origin column, parent key column and '
    'nested kind. NULL (not zero) when no trigger charges the kind — "nothing '
    'counts it" is a finding, "it counts to zero" is not';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Отката к трёхаргументному разбору НЕТ намеренно: он тихо перестал бы опознавать
-- триггеры вложенной оси, и пересчёт снова начал бы возвращать ноль там, где
-- ресурсы есть. Откат, восстанавливающий дефект, хуже его отсутствия; снимать эту
-- миграцию имеет смысл только вместе с самой осью.
SELECT 1;
-- +goose StatementEnd
