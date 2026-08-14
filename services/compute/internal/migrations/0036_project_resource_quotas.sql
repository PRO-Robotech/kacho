-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Учёт числа ресурсов арендатора у kacho-compute: СВЕДЕНИЕ, а не второй механизм.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), V2-9 и DoD S4 п.1. Величину назначает kacho-iam; учёт и
-- отказ живут здесь, у владельца типа ресурса.
--
-- ЧТО ЗДЕСЬ СВОДИТСЯ. В дереве жили ДВЕ реализации одного предмета: канонический
-- механизм (vpc, миграции 0040/0041) и более ранний прецедент compute
-- (`project_instance_quotas`, 0031). Второй снимается ЦЕЛИКОМ, и снятие не
-- уменьшает защиту — потому что защиты не было: строку предела не вставлял ни
-- один путь прод-кода (ноль производителей при контроле два в пробах), поэтому
-- списание ВСЕГДА получало ноль строк, трактовало это как «предел не назначен» и
-- пропускало. Механизм был провязан, покрыт пробой гонки и не отказал ни разу за
-- всю свою жизнь — форма без содержания.
--
-- ПОЧЕМУ СНЯТИЕ, А НЕ ПЕРЕИМЕНОВАНИЕ И НЕ ПЕРЕНОС ДАННЫХ. Переносить нечего:
-- строк в таблице нет by construction — их некому было создать. Переименование
-- сохранило бы форму ключа `PRIMARY KEY (project_id)`, у которой нет ни вида, ни
-- носителя: она выражает «одна квота на проект», тогда как считать надо тройкой
-- (носитель, id носителя, вид). То есть переименование пришлось бы немедленно
-- переписать целиком, оставив в истории лишний шаг.
--
-- ПОЧЕМУ ОБЕ ЧАСТИ ОДНОЙ МИГРАЦИЕЙ. Пока в дереве две реализации, следующая
-- работа копирует ту, что ближе, — и скопирует `CHECK (used <= limit_value)`,
-- делающий понижение предела невыразимым. Снятие прецедента и заведение
-- канонического обязаны быть одним событием, а не двумя, между которыми есть
-- окно.
--
-- ПРИМЕНЁННУЮ МИГРАЦИЮ НЕ ПРАВИМ (ban #5). 0031 остаётся как есть; таблица
-- снимается ЭТОЙ, новой миграцией.
--
-- СОСЕДНЯЯ РАБОТА, О КОТОРОЙ НАДО ЗНАТЬ. Ветка `issue-352` (PR #354, открыт на
-- `main`) несёт миграцию 0035, снимающую с `project_instance_quotas` ограничение
-- `CHECK (used <= limit_value)`. Её предмет эта миграция поглощает: таблицы не
-- остаётся вовсе. Номера выбраны так, что порядок применения детерминирован при
-- любом порядке вливания (0035 < 0036), но если 0036 уже применена к базе, а 0035
-- приезжает позже, goose увидит пропущенную версию — поэтому #354 обязан быть
-- влит или закрыт как поглощённый ДО этой работы. Это координация, а не дефект
-- схемы, и она названа здесь, чтобы не выясняться на стенде.
--
-- ПОЧЕМУ УЧЁТ ВЕДЁТ ТРИГГЕР, А НЕ USE-CASE. Списание обязано быть верным для
-- КАЖДОГО писателя — будущего пути, каскада, реконсайлера, административного SQL.
-- Соглашение «каждый писатель зовёт списание» держится ровно до первого нового
-- пути записи, и именно «появился второй писатель» есть механизм, которым
-- счётчик разъезжается с реальностью. Прецедент это и показывал: списание жило в
-- Go-функции репозитория, и её вызовы приходилось расставлять руками. Триггер
-- отвечает всем писателям и стоит в ТОЙ ЖЕ транзакции, что сама мутация:
-- «строка ресурса закоммичена» и «место списано» суть одно событие.
--
-- ПОЧЕМУ ПОТОЛОК НЕ СТОИТ `CHECK`-ОМ. `CHECK (used <= limit_value)` запрещает
-- состояние «занято больше, чем разрешено» — и тем самым запрещает САМО ПОНИЖЕНИЕ
-- предела: администратор, желающий ограничить проект, получал бы 23514, пока
-- проект сам не освободит место, то есть административное действие становилось бы
-- заложником того, кого оно ограничивает. Потолок держит предикат
-- `used < limit_value` условного `UPDATE` — того же единственного оператора, что
-- берёт блокировку строки, поэтому свойство не слабее: ограничение схемы ловило бы
-- превышение ПОСЛЕ записи, предикат не даёт его записать.
--
-- ЦЕНА, НАЗВАННАЯ ЧЕСТНО. Раз инвариант держит предикат, а не схема, то путь,
-- который запишет `used` мимо триггера, потолок обойдёт. Отсутствие такого пути
-- доказывается гейтом по дереву (`internal/repohygiene`,
-- TestQuotaUsedIsWrittenOnlyByItsTrigger), а не вниманием.

-- +goose Up
-- +goose StatementBegin

-- -----------------------------------------------------------------------------
-- Снятие прецедента.
-- -----------------------------------------------------------------------------
-- Сначала писатель, потом таблица: обратный порядок оставил бы функцию, которая
-- ссылается на несуществующее.
DROP TABLE IF EXISTS project_instance_quotas;

-- -----------------------------------------------------------------------------
-- Строка учёта — на тройку (носитель, id носителя, вид).
-- -----------------------------------------------------------------------------
-- Носитель, а не просто проект: та же таблица понесёт предел вложенности, где
-- носителем будет сам родитель. Ключ выбран тройкой сразу, чтобы вторая ось не
-- потребовала второй таблицы и второго разрешения старшинства — двух мест об
-- одном предмете, из которых верным окажется одно. Форма совпадает с той, что
-- уже живёт у vpc: расхождение формы между владельцами и есть механизм, которым
-- «общий контракт квот» перестаёт быть общим.
CREATE TABLE IF NOT EXISTS project_resource_quotas (
    carrier_type    text   NOT NULL,
    carrier_id      text   NOT NULL,
    kind            text   NOT NULL,

    -- Потребление. Неотрицательность — единственное, что стережёт схема.
    used            bigint NOT NULL DEFAULT 0,

    -- Снимок разрешённой величины вместе с её источником и ревизией. Это НЕ
    -- второй авторитет: снимок обновляется дельтой по ревизии, а на промахе
    -- пересчитывается резолвом у kacho-iam. Авторитет один — `kacho_iam.limits`.
    limit_value     bigint NOT NULL,
    source_scope    text   NOT NULL,
    source_scope_id text   NOT NULL DEFAULT '',
    limit_revision  bigint NOT NULL,

    -- Зеркало аккаунта проекта. Нужно, чтобы изменение АККАУНТНОЙ области
    -- адресовалось строкам этого аккаунта, а не пересчётом всех строк вида —
    -- всплеском вызовов к соседу на каждое административное действие.
    --
    -- Пустым быть не может by construction: строка без зеркала невидима
    -- аккаунтной дельте и жила бы со старой величиной, а снаружи это неотличимо
    -- от исправной работы — дельта отчитывается успехом, просто не тронув её.
    account_id      text   NOT NULL,

    synced_at       timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (carrier_type, carrier_id, kind),

    CONSTRAINT project_resource_quotas_used_check
        CHECK (used >= 0),
    CONSTRAINT project_resource_quotas_limit_check
        CHECK (limit_value >= 0),
    CONSTRAINT project_resource_quotas_account_id_check
        CHECK (account_id <> ''),
    CONSTRAINT project_resource_quotas_carrier_type_check
        CHECK (carrier_type <> ''),
    CONSTRAINT project_resource_quotas_source_scope_check
        CHECK (source_scope IN ('DEFAULT', 'ACCOUNT', 'PROJECT'))
    -- CHECK (used <= limit_value) НЕ СТАВИТСЯ. Причина — в шапке файла; она не
    -- про вкус, а про то, что понижение предела перестало бы быть выразимым.
    -- Ровно это ограничение и было снято с прецедента отдельной работой,
    -- прежде чем прецедент снялся целиком.
);

COMMENT ON TABLE project_resource_quotas IS
    'per-carrier resource-count accounting; the ceiling lives in the WHERE of the '
    'charging statement, never in a CHECK, because a CHECK would make lowering a '
    'limit below current usage inexpressible';

-- Адресация аккаунтной дельты: строки одного аккаунта.
CREATE INDEX IF NOT EXISTS project_resource_quotas_account_idx
    ON project_resource_quotas (account_id, kind);

-- Возраст старейшего несинхронизированного снимка — наблюдаемая величина.
CREATE INDEX IF NOT EXISTS project_resource_quotas_synced_at_idx
    ON project_resource_quotas (synced_at);

-- -----------------------------------------------------------------------------
-- Отказ — ЕДИНСТВЕННЫЙ производитель.
-- -----------------------------------------------------------------------------
-- Совещательная полоса (ранний отказ до создания операции) и авторитетная
-- (условный UPDATE внутри writer-TX) обязаны отвечать БАЙТ-ИДЕНТИЧНЫМ текстом и
-- признаком. Достигается это не сверкой двух согласных мест, а тем, что место
-- одно: обе полосы зовут эту функцию.
CREATE OR REPLACE FUNCTION kacho_quota_refuse(
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
      FROM project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    -- Строка есть ⇒ место кончилось. Администратору требуется ПОДНЯТЬ предел.
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

    -- Строки нет ⇒ потолок не назван ни на одной области. Администратору
    -- требуется ЗАВЕСТИ предел. Отдельный признак, а не оттенок предыдущего:
    -- сведи их в один, и читающий «место кончилось» пойдёт искать, что понизить,
    -- там, где ничего не назначено.
    RAISE EXCEPTION 'project % has no ceiling stated for %', v_carrier_id, v_kind
        USING ERRCODE = 'KQ002',
              DETAIL  = jsonb_build_object(
                            'carrier_type', v_carrier_type,
                            'carrier_id',   v_carrier_id,
                            'kind',         v_kind)::text;
END;
$$;

COMMENT ON FUNCTION kacho_quota_refuse(text, text, text) IS
    'the ONLY producer of a quota refusal: both the advisory read and the '
    'authoritative charge call it, so their text, SQLSTATE and details cannot '
    'drift apart — there is one place, not two agreeing ones';

-- -----------------------------------------------------------------------------
-- Совещательная полоса: говорит о наличии места, НЕ занимая его.
-- -----------------------------------------------------------------------------
-- Никогда не решение — решение принимает условный UPDATE триггера. Существует
-- затем, чтобы арендатор получил отказ рано и в ТЕХ ЖЕ словах.
CREATE OR REPLACE FUNCTION kacho_quota_admit(
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
      FROM project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF COALESCE(v_ok, false) THEN
        RETURN;
    END IF;

    PERFORM kacho_quota_refuse(v_carrier_type, v_carrier_id, v_kind);
END;
$$;

COMMENT ON FUNCTION kacho_quota_admit(text, text, text) IS
    'advisory band: says whether a slot is available WITHOUT taking it. Never a '
    'decision — the decision is the conditional UPDATE of the charging trigger; '
    'this exists so the tenant is refused early and in the same words';

-- -----------------------------------------------------------------------------
-- Списание и возврат — ОДНА функция, параметризованная видом.
-- -----------------------------------------------------------------------------
-- Одна, а не три: «завели четвёртый вид и забыли скопировать логику» должно быть
-- невыразимо.
--
-- TG_ARGV[0] — вид (`compute.instance`); TG_ARGV[1] — имя булева столбца
-- происхождения, либо пусто, если у вида системных детей не бывает. У compute
-- системных детей сегодня нет ни у одного из трёх видов, поэтому второй аргумент
-- не передаёт ни один триггер. Параметр тем не менее объявлен — это
-- параметризация одного механизма на всех владельцев, а не мёртвая ветка: у vpc
-- она несёт группы правил и таблицы маршрутизации, и расхождение сигнатур
-- сделало бы последующее сведение функции в общий фундамент правкой поведения,
-- а не переносом.
CREATE OR REPLACE FUNCTION kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind       text := TG_ARGV[0];
    v_system_col text := COALESCE(TG_ARGV[1], '');
    v_row        jsonb;
    v_project    text;
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
        -- Возврат — в той же транзакции, что удаление строки ресурса. Возврат вне
        -- её оставил бы счётчик завышенным при откате: проект платил бы местом за
        -- ресурс, которого нет. GREATEST не даёт уйти ниже нуля, если строка учёта
        -- была заведена позже самих ресурсов.
        UPDATE project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй писатель
    -- ждёт коммита первого и видит его результат. Чтение с последующим сравнением
    -- этого не даёт — между ними помещается чужая запись, и оба создателя пройдут
    -- проверку, увидев одно и то же свободное место (ban #10).
    UPDATE project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    -- Ноль строк — два разных состояния, и разбирает их единственный
    -- производитель отказа. Это не check-then-act: решение о списании уже принято
    -- атомарным оператором выше, и последующее чтение ничего не решает — оно
    -- только классифицирует уже случившийся отказ.
    PERFORM kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL;
END;
$$;

COMMENT ON FUNCTION kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. Refusals are produced by kacho_quota_refuse: KQ001 = '
    'ceiling reached, KQ002 = no ceiling stated for this project and kind';

-- -----------------------------------------------------------------------------
-- Три тенантных вида домена.
-- -----------------------------------------------------------------------------
-- Перечень — из закрытой таблицы грантуемых пар модели прав (`compute.instance`,
-- `compute.guestAccessKey`, `compute.placementGroup`), а не из имён таблиц:
-- токен вида — координата, и выписывается он оттуда, где объявлен.
DROP TRIGGER IF EXISTS instances_quota_count ON instances;
CREATE TRIGGER instances_quota_count
    AFTER INSERT OR DELETE ON instances
    FOR EACH ROW EXECUTE FUNCTION kacho_quota_count('compute.instance');

DROP TRIGGER IF EXISTS guest_access_keys_quota_count ON guest_access_keys;
CREATE TRIGGER guest_access_keys_quota_count
    AFTER INSERT OR DELETE ON guest_access_keys
    FOR EACH ROW EXECUTE FUNCTION kacho_quota_count('compute.guestAccessKey');

DROP TRIGGER IF EXISTS placement_groups_quota_count ON placement_groups;
CREATE TRIGGER placement_groups_quota_count
    AFTER INSERT OR DELETE ON placement_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_quota_count('compute.placementGroup');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS placement_groups_quota_count ON placement_groups;
DROP TRIGGER IF EXISTS guest_access_keys_quota_count ON guest_access_keys;
DROP TRIGGER IF EXISTS instances_quota_count ON instances;

DROP FUNCTION IF EXISTS kacho_quota_count();
DROP FUNCTION IF EXISTS kacho_quota_admit(text, text, text);
DROP FUNCTION IF EXISTS kacho_quota_refuse(text, text, text);

DROP TABLE IF EXISTS project_resource_quotas;

-- Прецедент НЕ воссоздаётся. Откат возвращает состояние «квота не считается»,
-- каким оно и было по существу: таблица прецедента существовала, но строк в ней
-- не создавал никто. Воссоздать её пустой значило бы вернуть форму без
-- содержания — ровно то, ради снятия чего эта миграция и написана.
-- +goose StatementEnd
