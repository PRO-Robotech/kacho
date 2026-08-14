-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Отказ учёта получает ЕДИНСТВЕННОГО производителя, и совещательная полоса
-- начинает пользоваться им же.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), DoD S2 п.3 и §7.4.
--
-- ЧТО ЗДЕСЬ ПОЯВЛЯЕТСЯ И ЗАЧЕМ. 0040 завела авторитетную полосу — списание
-- триггером в той же транзакции, что вставка строки ресурса. Приёмка требует
-- ВТОРУЮ, совещательную: отказ обязан приезжать арендатору синхронно, до
-- создания операции, иначе исчерпание предела наблюдается как «200 и операция,
-- упавшая через секунду». Полосы две по построению — и ровно поэтому они
-- обязаны говорить одно и то же.
--
-- ПОЧЕМУ ПРОИЗВОДИТЕЛЬ ОДИН, А НЕ ДВА СОГЛАСОВАННЫХ. Написанные порознь, тексты
-- расходятся на первой правке, и расхождение НЕ ВИДНО ни одной из сторон:
-- каждая по отдельности верна, а арендатор получает на один отказ два разных
-- сообщения — в зависимости от того, какая полоса сработала первой. Требование
-- приёмки «байт-идентичны» удовлетворяется здесь ПОСТРОЕНИЕМ: текст и SQLSTATE
-- рождаются в одной функции `kacho_quota_refuse`, которую зовут обе полосы.
-- Согласовывать нечего — согласовывать можно только два места, а место одно.
--
-- ПОЧЕМУ СОВЕЩАТЕЛЬНАЯ ПОЛОСА НЕ ЯВЛЯЕТСЯ РЕШЕНИЕМ. Она читает и ничего не
-- пишет, поэтому между её ответом и вставкой помещается чужая запись. Это не
-- дефект, а её роль: решение принимает атомарный `UPDATE … WHERE used <
-- limit_value` авторитетной полосы (ban #10). Совещательная существует ради
-- РАННЕГО и понятного отказа, и приёмка прямо запрещает считать её решением
-- (§7.4). Отсюда же следует, что её промах безопасен в обе стороны: пропустила
-- лишнего — отвергнет триггер; отвергла — арендатор увидит тот же отказ, что
-- увидел бы секундой позже.
--
-- ЧТО ОСТАЁТСЯ ОТ 0040 БЕЗ ИЗМЕНЕНИЙ. Таблица, ключ, ограничения, набор
-- триггеров и правило «потолок живёт в предикате списания, а не в CHECK».
-- Применённая миграция не правится (ban #5): тело функции списания заменяется
-- `CREATE OR REPLACE`, её сигнатура и все восемь триггеров остаются теми же,
-- поэтому пересоздавать их не требуется и порядок применения не важен.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- -----------------------------------------------------------------------------
-- ЕДИНСТВЕННЫЙ производитель отказа учёта.
-- -----------------------------------------------------------------------------
-- Зовётся ТОЛЬКО тогда, когда списание или совещательная проверка уже
-- установили, что места нет. Сама ничего не решает — она классифицирует уже
-- случившийся отказ и облекает его в контракт: текст, SQLSTATE и машинные
-- подробности.
--
-- Различие двух исходов несущее, а не косметическое: «строки нет» требует от
-- администратора ЗАВЕСТИ потолок, «строка полна» — ПОДНЯТЬ его. Один код на оба
-- послал бы его искать, что понизить, там, где ничего не назначено.
--
-- DETAIL несёт те же факты машинно. Он не говорит больше сообщения (иначе
-- скрытое из текста возвращалось бы деталью — то же раскрытие, только незаметное
-- глазом при чтении диффа), и разбирается ровно одним местом Go-стороны.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_refuse(
    v_carrier_type text,
    v_carrier_id   text,
    v_kind         text
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_limit bigint;
    v_used  bigint;
BEGIN
    SELECT limit_value, used INTO v_limit, v_used
      FROM kacho_vpc.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF FOUND THEN
        RAISE EXCEPTION 'project % has reached its limit of % %', v_carrier_id, v_limit, v_kind
            USING ERRCODE = 'KQ001',
                  DETAIL  = jsonb_build_object(
                                'carrier_type', v_carrier_type,
                                'carrier_id',   v_carrier_id,
                                'kind',         v_kind,
                                'limit',        v_limit,
                                'used',         v_used)::text;
    END IF;

    RAISE EXCEPTION 'project % has no ceiling stated for %', v_carrier_id, v_kind
        USING ERRCODE = 'KQ002',
              DETAIL  = jsonb_build_object(
                            'carrier_type', v_carrier_type,
                            'carrier_id',   v_carrier_id,
                            'kind',         v_kind)::text;
END;
$$;

COMMENT ON FUNCTION kacho_vpc.kacho_quota_refuse(text, text, text) IS
    'the ONLY producer of a quota refusal: both the advisory read and the '
    'authoritative charge call it, so their text, SQLSTATE and details cannot '
    'drift apart — there is one place, not two agreeing ones';

-- -----------------------------------------------------------------------------
-- Совещательная полоса.
-- -----------------------------------------------------------------------------
-- Возвращает void, когда место есть, и отказывает через единственного
-- производителя, когда его нет. Ничего не пишет: чтение остаётся чтением.
--
-- Отдельная функция, а не запрос из Go, ровно ради байт-идентичности: запрос из
-- Go пришлось бы снабдить СВОИМ текстом отказа, и второе место завелось бы
-- обратно.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_admit(
    v_carrier_type text,
    v_carrier_id   text,
    v_kind         text
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_ok boolean;
BEGIN
    SELECT used < limit_value INTO v_ok
      FROM kacho_vpc.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF COALESCE(v_ok, false) THEN
        RETURN;
    END IF;

    PERFORM kacho_vpc.kacho_quota_refuse(v_carrier_type, v_carrier_id, v_kind);
END;
$$;

COMMENT ON FUNCTION kacho_vpc.kacho_quota_admit(text, text, text) IS
    'advisory band: says whether a slot is available WITHOUT taking it. Never a '
    'decision — the decision is the conditional UPDATE of the charging trigger; '
    'this exists so the tenant is refused early and in the same words';

-- -----------------------------------------------------------------------------
-- Авторитетная полоса переводится на того же производителя.
-- -----------------------------------------------------------------------------
-- Сигнатура и имя те же, что в 0040, поэтому восемь триггеров продолжают
-- указывать сюда и пересоздания не требуют. Изменилось ровно одно: классификацию
-- и текст отказа функция больше не строит сама, а поручает
-- `kacho_quota_refuse` — вместе с ним ушёл и повторный SELECT предела, который
-- 0040 делала прямо в аргументе RAISE.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind        text := TG_ARGV[0];
    v_system_col  text := COALESCE(TG_ARGV[1], '');
    v_row         jsonb;
    v_project     text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_row := to_jsonb(OLD);
    ELSE
        v_row := to_jsonb(NEW);
    END IF;

    -- Системный ребёнок не тратит предел арендатора: его число привязано к числу
    -- родителей один к одному, а родители ограничены — значит он ограничен
    -- транзитивно.
    IF v_system_col <> '' AND COALESCE((v_row ->> v_system_col)::boolean, false) THEN
        RETURN NULL;
    END IF;

    v_project := v_row ->> 'project_id';
    IF v_project IS NULL OR v_project = '' THEN
        RAISE EXCEPTION 'quota: row of % carries no project_id', TG_TABLE_NAME
            USING ERRCODE = 'KQ003';
    END IF;

    IF TG_OP = 'DELETE' THEN
        -- Возврат — в той же транзакции, что удаление строки ресурса. GREATEST не
        -- даёт уйти ниже нуля, если строка учёта заведена позже самих ресурсов.
        UPDATE kacho_vpc.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй писатель
    -- ждёт коммита первого и видит его результат (ban #10).
    UPDATE kacho_vpc.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_vpc.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;

COMMENT ON FUNCTION kacho_vpc.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. The refusal itself is produced by kacho_quota_refuse, '
    'shared with the advisory band, so the two bands cannot word it differently';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Совещательная полоса снимается целиком: у неё нет второго потребителя.
DROP FUNCTION IF EXISTS kacho_vpc.kacho_quota_admit(text, text, text);

-- Авторитетная возвращается к форме 0040 — самостоятельной классификации.
-- Восстанавливается ДОСЛОВНО, а не «примерно»: откат, оставляющий другой текст,
-- нарушил бы тот самый контракт, ради которого миграция и написана.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind        text := TG_ARGV[0];
    v_system_col  text := COALESCE(TG_ARGV[1], '');
    v_row         jsonb;
    v_project     text;
    v_charged     bigint;
    v_exists      boolean;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_row := to_jsonb(OLD);
    ELSE
        v_row := to_jsonb(NEW);
    END IF;

    IF v_system_col <> '' AND COALESCE((v_row ->> v_system_col)::boolean, false) THEN
        RETURN NULL;
    END IF;

    v_project := v_row ->> 'project_id';
    IF v_project IS NULL OR v_project = '' THEN
        RAISE EXCEPTION 'quota: row of % carries no project_id', TG_TABLE_NAME
            USING ERRCODE = 'KQ003';
    END IF;

    IF TG_OP = 'DELETE' THEN
        UPDATE kacho_vpc.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    UPDATE kacho_vpc.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value
    RETURNING used INTO v_charged;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    SELECT true INTO v_exists
      FROM kacho_vpc.project_resource_quotas
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;

    IF COALESCE(v_exists, false) THEN
        RAISE EXCEPTION 'project % has reached its limit of % %',
                        v_project,
                        (SELECT limit_value FROM kacho_vpc.project_resource_quotas
                          WHERE carrier_type = 'project' AND carrier_id = v_project
                            AND kind = v_kind),
                        v_kind
            USING ERRCODE = 'KQ001';
    END IF;

    RAISE EXCEPTION 'project % has no ceiling stated for %', v_project, v_kind
        USING ERRCODE = 'KQ002';
END;
$$;

DROP FUNCTION IF EXISTS kacho_vpc.kacho_quota_refuse(text, text, text);
-- +goose StatementEnd
