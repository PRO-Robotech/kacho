-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Учёт числа ресурсов арендатора у kacho-nlb: проектная ось и ось вложенности.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), DoD S4 п.1. Величину назначает kacho-iam; учёт и отказ
-- живут здесь, у владельца типа ресурса.
--
-- ФОРМА ПОВТОРЯЕТ УЖЕ СДАННУЮ У kacho-vpc (миграции 0040 и 0041) — не «похожа
-- на неё», а совпадает по составу столбцов, именам функций, SQLSTATE'ам и тону
-- отказа. Разойдясь, два владельца одного механизма дали бы арендатору два
-- разных поведения на один и тот же отказ, а клиенту — второй словарь кодов.
--
-- ПОЧЕМУ УЧЁТ ВЕДЁТ ТРИГГЕР, А НЕ USE-CASE. Списание обязано быть верным для
-- КАЖДОГО писателя — будущего пути, каскада, реконсайлера, административного
-- SQL. Соглашение «каждый писатель зовёт списание» держится ровно до первого
-- нового пути записи, и именно «появился второй писатель» есть механизм, которым
-- счётчик разъезжается с реальностью. Триггер отвечает всем писателям и стоит в
-- ТОЙ ЖЕ транзакции, что сама мутация.
--
-- ПОЧЕМУ ПОТОЛОК НЕ СТОИТ `CHECK`-ОМ. `CHECK (used <= limit_value)` запрещает
-- состояние «занято больше, чем разрешено» — и тем самым запрещает САМО
-- ПОНИЖЕНИЕ предела: администратор получал бы 23514, пока проект сам не
-- освободит место. Потолок держит предикат `used < limit_value` условного
-- `UPDATE` — того же единственного оператора, что берёт блокировку строки.
--
-- ЧТО ЗДЕСЬ ЕСТЬ СВЕРХ ОБРАЗЦА vpc: ОСЬ ВЛОЖЕННОСТИ.
-- «Слушателей в ОДНОМ балансировщике» — предел, чей носитель сам является
-- ресурсом (V2-5). Опасность, названная владельцем («ресурсы, способные положить
-- инфраструктуру»), живёт именно во вложенности: один балансировщик с десятью
-- тысячами слушателей — это другой отказ, чем десять тысяч слушателей,
-- размазанных по проектам.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;

-- -----------------------------------------------------------------------------
-- Строка учёта — на тройку (носитель, id носителя, вид).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kacho_nlb.project_resource_quotas (
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
    -- адресовалось строкам этого аккаунта, а не пересчётом всех строк вида.
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
);

COMMENT ON TABLE kacho_nlb.project_resource_quotas IS
    'per-carrier resource-count accounting; the ceiling lives in the WHERE of the '
    'charging statement, never in a CHECK, because a CHECK would make lowering a '
    'limit below current usage inexpressible';

CREATE INDEX IF NOT EXISTS project_resource_quotas_account_idx
    ON kacho_nlb.project_resource_quotas (account_id, kind);

CREATE INDEX IF NOT EXISTS project_resource_quotas_synced_at_idx
    ON kacho_nlb.project_resource_quotas (synced_at);

-- -----------------------------------------------------------------------------
-- Разрешённая величина для ОСИ ВЛОЖЕННОСТИ — отдельная таблица, а не строка
-- учёта с бессмысленным `used`.
-- -----------------------------------------------------------------------------
-- Величина вложенного вида резолвится ПО ПРОЕКТУ («в этом проекте каждый
-- балансировщик держит не более N слушателей»), а считается ПО РОДИТЕЛЮ. То есть
-- у неё есть проектная величина и нет проектного потребления.
--
-- Положить её строкой в таблицу учёта было бы дешевле на одну таблицу и неверно
-- по существу: такая строка несла бы `used`, которого никто не производит, и
-- читалась бы как «занято 0 из 8» — утверждение о потреблении, которого владелец
-- не делал (`api-conventions.md` §«Пустое значение обязано означать „пусто“»).
-- Столбец, который лжёт, дороже таблицы, которая не лжёт.
--
-- Отсюда триггер берёт снимок, заводя строку учёта нового родителя, — поэтому
-- производитель строки родителя есть BY CONSTRUCTION, а не по соглашению с
-- use-case-слоем. Ровно этим отличается «строка не заведена» (громкий отказ
-- `QUOTA_NOT_PROVISIONED` на первом ребёнке) от «счётчик молча разошёлся».
CREATE TABLE IF NOT EXISTS kacho_nlb.nested_quota_defaults (
    project_id      text   NOT NULL,
    kind            text   NOT NULL,

    limit_value     bigint NOT NULL,
    source_scope    text   NOT NULL,
    source_scope_id text   NOT NULL DEFAULT '',
    limit_revision  bigint NOT NULL,
    account_id      text   NOT NULL,

    synced_at       timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (project_id, kind),

    CONSTRAINT nested_quota_defaults_limit_check
        CHECK (limit_value >= 0),
    CONSTRAINT nested_quota_defaults_account_id_check
        CHECK (account_id <> ''),
    CONSTRAINT nested_quota_defaults_source_scope_check
        CHECK (source_scope IN ('DEFAULT', 'ACCOUNT', 'PROJECT'))
);

COMMENT ON TABLE kacho_nlb.nested_quota_defaults IS
    'per-project resolved value of a NESTED kind ("how many children fit in one '
    'parent of this project"). Deliberately carries no `used`: the value is '
    'resolved per project but consumed per parent, and a `used` column here would '
    'state a consumption nobody produces';

CREATE INDEX IF NOT EXISTS nested_quota_defaults_account_idx
    ON kacho_nlb.nested_quota_defaults (account_id, kind);

-- -----------------------------------------------------------------------------
-- ЕДИНСТВЕННЫЙ производитель отказа учёта.
-- -----------------------------------------------------------------------------
-- Зовётся ТОЛЬКО тогда, когда списание или совещательная проверка уже
-- установили, что места нет. Сама ничего не решает — она классифицирует уже
-- случившийся отказ и облекает его в контракт: текст, SQLSTATE и машинные
-- подробности.
--
-- Различие двух исходов несущее: «строки нет» требует от администратора ЗАВЕСТИ
-- потолок, «строка полна» — ПОДНЯТЬ его. Один код на оба послал бы его искать,
-- что понизить, там, где ничего не назначено.
--
-- ТЕКСТ ОБЩИЙ С kacho-vpc, И ЭТО ПРОВЕРЯЕМО. Формат `'% % has reached its limit
-- of % %'` при носителе `project` даёт ДОСЛОВНО ту же строку, что производит
-- vpc: «project <id> has reached its limit of <N> <вид>». При носителе-родителе
-- та же строка называет носителя — «loadbalancer.networkLoadBalancers <id> has
-- reached its limit of <N> <вид>», — чего требует QV2-30 («сообщение называет
-- носителя, а не только вид»). Один формат на оба случая, а не два согласованных.
CREATE OR REPLACE FUNCTION kacho_nlb.kacho_quota_refuse(
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
      FROM kacho_nlb.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF FOUND THEN
        RAISE EXCEPTION '% % has reached its limit of % %',
                        v_carrier_type, v_carrier_id, v_limit, v_kind
            USING ERRCODE = 'KQ001',
                  DETAIL  = jsonb_build_object(
                                'carrier_type', v_carrier_type,
                                'carrier_id',   v_carrier_id,
                                'kind',         v_kind,
                                'limit',        v_limit,
                                'used',         v_used)::text;
    END IF;

    RAISE EXCEPTION '% % has no ceiling stated for %', v_carrier_type, v_carrier_id, v_kind
        USING ERRCODE = 'KQ002',
              DETAIL  = jsonb_build_object(
                            'carrier_type', v_carrier_type,
                            'carrier_id',   v_carrier_id,
                            'kind',         v_kind)::text;
END;
$$;

COMMENT ON FUNCTION kacho_nlb.kacho_quota_refuse(text, text, text) IS
    'the ONLY producer of a quota refusal: both the advisory read and the '
    'authoritative charge call it, so their text, SQLSTATE and details cannot '
    'drift apart — there is one place, not two agreeing ones';

-- -----------------------------------------------------------------------------
-- Совещательная полоса.
-- -----------------------------------------------------------------------------
-- Возвращает void, когда место есть, и отказывает через единственного
-- производителя, когда его нет. Ничего не пишет: чтение остаётся чтением.
CREATE OR REPLACE FUNCTION kacho_nlb.kacho_quota_admit(
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
      FROM kacho_nlb.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF COALESCE(v_ok, false) THEN
        RETURN;
    END IF;

    PERFORM kacho_nlb.kacho_quota_refuse(v_carrier_type, v_carrier_id, v_kind);
END;
$$;

COMMENT ON FUNCTION kacho_nlb.kacho_quota_admit(text, text, text) IS
    'advisory band: says whether a slot is available WITHOUT taking it. Never a '
    'decision — the decision is the conditional UPDATE of the charging trigger';

-- -----------------------------------------------------------------------------
-- Списание и возврат — ОДНА функция на обе оси.
-- -----------------------------------------------------------------------------
-- TG_ARGV[0] — проектный вид (`loadbalancer.listeners`);
-- TG_ARGV[1] — имя столбца с id родителя, либо пусто, если оси вложенности нет;
-- TG_ARGV[2] — вложенный вид (`loadbalancer.networkLoadBalancers.listeners`).
--
-- ПОЧЕМУ ОДНА ФУНКЦИЯ, А НЕ ДВА ТРИГГЕРА. Порядок списания зафиксирован
-- приёмкой (V2-6): сначала носитель-родитель, потом носитель-проект. Два
-- триггера на одной таблице исполняются в алфавитном порядке ИМЁН — то есть
-- порядок, от которого зависит отсутствие взаимной блокировки, держался бы
-- соглашением об именовании, невидимым в месте, где о нём надо помнить. Здесь он
-- записан последовательностью операторов и иначе быть не может.
--
-- ПОЧЕМУ ПОРЯДОК ВООБЩЕ ВАЖЕН. Две строки учёта, берущиеся в разном порядке
-- разными транзакциями, — это взаимная блокировка на ровном месте.
CREATE OR REPLACE FUNCTION kacho_nlb.kacho_quota_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_kind        text := TG_ARGV[0];
    v_parent_col  text := COALESCE(TG_ARGV[1], '');
    v_nested_kind text := COALESCE(TG_ARGV[2], '');
    v_row         jsonb;
    v_project     text;
    v_parent      text;
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

    IF v_parent_col <> '' THEN
        v_parent := v_row ->> v_parent_col;
        IF v_parent IS NULL OR v_parent = '' THEN
            RAISE EXCEPTION 'quota: row of % carries no %', TG_TABLE_NAME, v_parent_col
                USING ERRCODE = 'KQ003';
        END IF;
    END IF;

    IF TG_OP = 'DELETE' THEN
        -- Возврат — в той же транзакции, что удаление строки ресурса. Порядок тот
        -- же, что у списания: родитель, затем проект. GREATEST не даёт уйти ниже
        -- нуля, если строка учёта заведена позже самих ресурсов.
        IF v_parent_col <> '' THEN
            UPDATE kacho_nlb.project_resource_quotas
               SET used = GREATEST(used - 1, 0), updated_at = now()
             WHERE carrier_type = v_nested_kind AND carrier_id = v_parent AND kind = v_nested_kind;
        END IF;

        UPDATE kacho_nlb.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй писатель
    -- ждёт коммита первого и видит его результат. Чтение с последующим сравнением
    -- этого не даёт — между ними помещается чужая запись, и оба создателя прошли
    -- бы проверку, увидев одно и то же свободное место (ban #10).
    --
    -- СНАЧАЛА РОДИТЕЛЬ (V2-6). Цена порядка названа приёмкой честно: арендатор,
    -- упёршийся в оба предела, узнаёт о родительском — поэтому носитель и назван
    -- в тексте отказа, а не оставлен на догадку.
    IF v_parent_col <> '' THEN
        UPDATE kacho_nlb.project_resource_quotas
           SET used = used + 1, updated_at = now()
         WHERE carrier_type = v_nested_kind AND carrier_id = v_parent AND kind = v_nested_kind
           AND used < limit_value;

        IF NOT FOUND THEN
            PERFORM kacho_nlb.kacho_quota_refuse(v_nested_kind, v_parent, v_nested_kind);
        END IF;
    END IF;

    UPDATE kacho_nlb.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_nlb.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;

COMMENT ON FUNCTION kacho_nlb.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. Charges the PARENT carrier before the project carrier '
    '(acceptance V2-6) — the order lives in the statement sequence, not in a '
    'trigger-naming convention nobody can see from here';

-- -----------------------------------------------------------------------------
-- Жизненный цикл строки учёта РОДИТЕЛЯ.
-- -----------------------------------------------------------------------------
-- Строка учёта родителя заводится ТОЙ ЖЕ транзакцией, что сам родитель, и той же
-- транзакцией снимается (V2-5, QV2-32). Производитель есть by construction —
-- то есть дефект «учёт есть, строку никто не пишет», измеренный на прецеденте
-- compute, в этой оси НЕВЫРАЗИМ.
--
-- Снимок величины берётся из `nested_quota_defaults` — проектного резолва. Его
-- отсутствие НЕ является молчаливым разрешением: строка родителя тогда не
-- заводится, и первый же ребёнок получает `QUOTA_NOT_PROVISIONED` — громкий
-- отказ, называющий предмет.
CREATE OR REPLACE FUNCTION kacho_nlb.kacho_quota_carrier_lifecycle()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_nested_kind text := TG_ARGV[0];
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM kacho_nlb.project_resource_quotas
         WHERE carrier_type = v_nested_kind AND carrier_id = OLD.id;
        RETURN NULL;
    END IF;

    INSERT INTO kacho_nlb.project_resource_quotas
        (carrier_type, carrier_id, kind, used, limit_value,
         source_scope, source_scope_id, limit_revision, account_id)
    SELECT v_nested_kind, NEW.id, v_nested_kind, 0, d.limit_value,
           d.source_scope, d.source_scope_id, d.limit_revision, d.account_id
      FROM kacho_nlb.nested_quota_defaults d
     WHERE d.project_id = NEW.project_id AND d.kind = v_nested_kind
    ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING;

    RETURN NULL;
END;
$$;

COMMENT ON FUNCTION kacho_nlb.kacho_quota_carrier_lifecycle() IS
    'creates and removes the accounting row of a PARENT carrier in the same '
    'transaction as the parent itself, so the row always has a producer';

-- -----------------------------------------------------------------------------
-- Три тенантных вида домена плюс одна ось вложенности.
-- -----------------------------------------------------------------------------
-- Виды выписаны токенами закрытой таблицы грантуемых пар
-- (`services/iam/internal/authzmap/fga_types.go`), а НЕ по памяти о названии
-- ресурса: у этого домена вторая часть токена во МНОЖЕСТВЕННОМ числе
-- (`loadbalancer.listeners`, не `loadbalancer.listener`), и написанное по памяти
-- гейт каталога отвергает.
DROP TRIGGER IF EXISTS load_balancers_quota_count ON kacho_nlb.load_balancers;
CREATE TRIGGER load_balancers_quota_count
    AFTER INSERT OR DELETE ON kacho_nlb.load_balancers
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.kacho_quota_count('loadbalancer.networkLoadBalancers');

DROP TRIGGER IF EXISTS target_groups_quota_count ON kacho_nlb.target_groups;
CREATE TRIGGER target_groups_quota_count
    AFTER INSERT OR DELETE ON kacho_nlb.target_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.kacho_quota_count('loadbalancer.targetGroups');

-- Слушатель считается ДВАЖДЫ: в своём балансировщике и в проекте. Порядок
-- зафиксирован внутри функции, а не именем триггера.
DROP TRIGGER IF EXISTS listeners_quota_count ON kacho_nlb.listeners;
CREATE TRIGGER listeners_quota_count
    AFTER INSERT OR DELETE ON kacho_nlb.listeners
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.kacho_quota_count(
        'loadbalancer.listeners',
        'load_balancer_id',
        'loadbalancer.networkLoadBalancers.listeners');

-- Балансировщик — носитель учёта для своих слушателей.
DROP TRIGGER IF EXISTS load_balancers_quota_carrier ON kacho_nlb.load_balancers;
CREATE TRIGGER load_balancers_quota_carrier
    AFTER INSERT OR DELETE ON kacho_nlb.load_balancers
    FOR EACH ROW EXECUTE FUNCTION kacho_nlb.kacho_quota_carrier_lifecycle(
        'loadbalancer.networkLoadBalancers.listeners');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_nlb, public;

DROP TRIGGER IF EXISTS load_balancers_quota_carrier ON kacho_nlb.load_balancers;
DROP TRIGGER IF EXISTS listeners_quota_count ON kacho_nlb.listeners;
DROP TRIGGER IF EXISTS target_groups_quota_count ON kacho_nlb.target_groups;
DROP TRIGGER IF EXISTS load_balancers_quota_count ON kacho_nlb.load_balancers;

DROP FUNCTION IF EXISTS kacho_nlb.kacho_quota_carrier_lifecycle();
DROP FUNCTION IF EXISTS kacho_nlb.kacho_quota_count();
DROP FUNCTION IF EXISTS kacho_nlb.kacho_quota_admit(text, text, text);
DROP FUNCTION IF EXISTS kacho_nlb.kacho_quota_refuse(text, text, text);

DROP TABLE IF EXISTS kacho_nlb.nested_quota_defaults;
DROP TABLE IF EXISTS kacho_nlb.project_resource_quotas;
-- +goose StatementEnd
