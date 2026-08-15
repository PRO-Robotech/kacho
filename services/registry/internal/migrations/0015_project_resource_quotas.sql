-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Учёт числа ресурсов арендатора у kacho-registry: проектная ось и ось
-- вложенности «репозиториев в одном реестре».
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), DoD S4 п.1. Величину назначает kacho-iam; учёт и отказ
-- живут здесь, у владельца типа ресурса.
--
-- ФОРМА ПОВТОРЯЕТ УЖЕ СДАННУЮ У kacho-vpc (0040/0041) и у kacho-nlb (0032) —
-- те же столбцы, те же имена функций, те же SQLSTATE'ы, тот же тон отказа.
-- Разойдясь, владельцы одного механизма дали бы арендатору два разных поведения
-- на один и тот же отказ.
--
-- ПОЧЕМУ УЧЁТ ВЕДЁТ ТРИГГЕР, А НЕ USE-CASE. Списание обязано быть верным для
-- КАЖДОГО писателя. У этого домена довод сильнее, чем у соседей, и он измерен:
-- репозиторий заводят ДВА пути — control-plane (`CreateRepository`) и data-plane
-- (первый push), — и оба идут через одну функцию `applyRepoRegistration`. Плюс
-- третий писатель, о котором легко забыть: каскад `ON DELETE CASCADE` от
-- реестра. Соглашение «каждый писатель зовёт списание» здесь не удержалось бы
-- даже сегодня, не то что после следующей правки.
--
-- ПОЧЕМУ ПОТОЛОК НЕ СТОИТ `CHECK`-ОМ. `CHECK (used <= limit_value)` сделал бы
-- невыразимым САМО ПОНИЖЕНИЕ предела — штатное административное действие. Потолок
-- держит предикат `used < limit_value` условного `UPDATE`.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_registry, public;

-- -----------------------------------------------------------------------------
-- Строка учёта — на тройку (носитель, id носителя, вид).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kacho_registry.project_resource_quotas (
    carrier_type    text   NOT NULL,
    carrier_id      text   NOT NULL,
    kind            text   NOT NULL,

    used            bigint NOT NULL DEFAULT 0,

    limit_value     bigint NOT NULL,
    source_scope    text   NOT NULL,
    source_scope_id text   NOT NULL DEFAULT '',
    limit_revision  bigint NOT NULL,

    -- Зеркало аккаунта проекта: без него изменение АККАУНТНОЙ области
    -- адресовалось бы пересчётом всех строк вида. Пустым быть не может —
    -- строка без зеркала невидима аккаунтной дельте, а снаружи это неотличимо
    -- от исправной работы.
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
    -- CHECK (used <= limit_value) НЕ СТАВИТСЯ — см. шапку файла.
);

COMMENT ON TABLE kacho_registry.project_resource_quotas IS
    'per-carrier resource-count accounting; the ceiling lives in the WHERE of the '
    'charging statement, never in a CHECK, because a CHECK would make lowering a '
    'limit below current usage inexpressible';

CREATE INDEX IF NOT EXISTS project_resource_quotas_account_idx
    ON kacho_registry.project_resource_quotas (account_id, kind);

CREATE INDEX IF NOT EXISTS project_resource_quotas_synced_at_idx
    ON kacho_registry.project_resource_quotas (synced_at);

-- -----------------------------------------------------------------------------
-- Разрешённая величина для ОСИ ВЛОЖЕННОСТИ — отдельная таблица.
-- -----------------------------------------------------------------------------
-- Величина вложенного вида резолвится ПО ПРОЕКТУ («в этом проекте каждый реестр
-- держит не более N репозиториев»), а считается ПО РОДИТЕЛЮ. Строка учёта с
-- носителем-проектом несла бы здесь `used`, которого никто не производит, и
-- читалась бы как «занято 0 из N» — утверждение о потреблении, которого владелец
-- не делал (`api-conventions.md` §«Пустое значение обязано означать „пусто“»).
CREATE TABLE IF NOT EXISTS kacho_registry.nested_quota_defaults (
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

COMMENT ON TABLE kacho_registry.nested_quota_defaults IS
    'per-project resolved value of a NESTED kind ("how many children fit in one '
    'parent of this project"). Deliberately carries no `used`: the value is '
    'resolved per project but consumed per parent';

CREATE INDEX IF NOT EXISTS nested_quota_defaults_account_idx
    ON kacho_registry.nested_quota_defaults (account_id, kind);

-- -----------------------------------------------------------------------------
-- Проект репозитория — НА САМОЙ СТРОКЕ, а не запросом к реестру.
-- -----------------------------------------------------------------------------
-- Строка `registry_repository_registration` несёт только `(registry_id, repo)` и
-- добирается до проекта join'ом со своим реестром (что каталог видов и
-- фиксирует). Для СПИСАНИЯ этого достаточно, для ВОЗВРАТА — нет, и разница здесь
-- не теоретическая:
--
-- реестр ссылается на репозитории `ON DELETE CASCADE`, поэтому при удалении
-- реестра строки детей удаляет FK-действие — уже ПОСЛЕ того, как строка родителя
-- исчезла. Триггер возврата, спрашивающий проект у реестра, в этот момент не
-- нашёл бы ничего и либо промолчал (счётчик проекта остался бы завышенным
-- навсегда), либо упал бы, уронив штатное удаление реестра.
--
-- Поэтому проект денормализуется НА СТРОКУ ребёнка и заполняется BEFORE INSERT —
-- в момент, когда родитель заведомо жив (его существование обеспечено тем же FK).
-- Это ровно тот приём, которым vpc решил свой признак происхождения: значение
-- читается из САМОЙ строки, а не спрашивается у родителя, потому что в момент,
-- когда оно нужно, родителя может уже не быть.
ALTER TABLE kacho_registry.registry_repository_registration
    ADD COLUMN IF NOT EXISTS project_id text NOT NULL DEFAULT '';

COMMENT ON COLUMN kacho_registry.registry_repository_registration.project_id IS
    'denormalised from the parent registry at insert time. Read from the row '
    'itself because on ON DELETE CASCADE the parent is already gone when the '
    'refund needs it';

-- Обратное заполнение ДЕТЕРМИНИРОВАНО: проект берётся у своего реестра. Число
-- переписанных строк печатается — иначе «ноль обновлённых» неотличимо от «ноль
-- прочитанных».
DO $$
DECLARE
    v_rows bigint;
BEGIN
    UPDATE kacho_registry.registry_repository_registration g
       SET project_id = r.project_id
      FROM kacho_registry.registries r
     WHERE r.id = g.registry_id
       AND g.project_id = '';
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RAISE NOTICE 'registry_repository_registration.project_id backfill: % row(s)', v_rows;
END $$;

CREATE OR REPLACE FUNCTION kacho_registry.kacho_repo_registration_project()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.project_id IS NULL OR NEW.project_id = '' THEN
        SELECT r.project_id INTO NEW.project_id
          FROM kacho_registry.registries r
         WHERE r.id = NEW.registry_id;
    END IF;
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION kacho_registry.kacho_repo_registration_project() IS
    'fills the child row project from its registry at insert time, so the refund '
    'path never has to ask a parent that ON DELETE CASCADE has already removed';

DROP TRIGGER IF EXISTS repo_registration_project
    ON kacho_registry.registry_repository_registration;
CREATE TRIGGER repo_registration_project
    BEFORE INSERT ON kacho_registry.registry_repository_registration
    FOR EACH ROW EXECUTE FUNCTION kacho_registry.kacho_repo_registration_project();

-- -----------------------------------------------------------------------------
-- ЕДИНСТВЕННЫЙ производитель отказа учёта.
-- -----------------------------------------------------------------------------
-- Формат `'% % has reached its limit of % %'` при носителе `project` даёт
-- ДОСЛОВНО ту же строку, что производят vpc и nlb; при носителе-родителе он
-- называет носителя, чего требует QV2-30.
CREATE OR REPLACE FUNCTION kacho_registry.kacho_quota_refuse(
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
      FROM kacho_registry.project_resource_quotas
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

COMMENT ON FUNCTION kacho_registry.kacho_quota_refuse(text, text, text) IS
    'the ONLY producer of a quota refusal: both the advisory read and the '
    'authoritative charge call it, so their text, SQLSTATE and details cannot '
    'drift apart';

-- -----------------------------------------------------------------------------
-- Совещательная полоса.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION kacho_registry.kacho_quota_admit(
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
      FROM kacho_registry.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF COALESCE(v_ok, false) THEN
        RETURN;
    END IF;

    PERFORM kacho_registry.kacho_quota_refuse(v_carrier_type, v_carrier_id, v_kind);
END;
$$;

COMMENT ON FUNCTION kacho_registry.kacho_quota_admit(text, text, text) IS
    'advisory band: says whether a slot is available WITHOUT taking it. Never a '
    'decision — the decision is the conditional UPDATE of the charging trigger';

-- -----------------------------------------------------------------------------
-- Списание и возврат — ОДНА функция на обе оси.
-- -----------------------------------------------------------------------------
-- TG_ARGV[0] — проектный вид; TG_ARGV[1] — столбец с id родителя (пусто, если
-- оси вложенности нет); TG_ARGV[2] — вложенный вид.
--
-- Порядок списания (V2-6: родитель, затем проект) записан последовательностью
-- операторов, а не именами двух триггеров: имя — не то место, где о порядке
-- вспоминают, а взаимная блокировка — цена, которую платят за то, что не
-- вспомнили.
CREATE OR REPLACE FUNCTION kacho_registry.kacho_quota_count()
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
        -- Возврат в той же транзакции, что снятие строки ресурса, и в том же
        -- порядке, что списание.
        IF v_parent_col <> '' THEN
            UPDATE kacho_registry.project_resource_quotas
               SET used = GREATEST(used - 1, 0), updated_at = now()
             WHERE carrier_type = v_nested_kind AND carrier_id = v_parent AND kind = v_nested_kind;
        END IF;

        UPDATE kacho_registry.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки (ban #10).
    IF v_parent_col <> '' THEN
        UPDATE kacho_registry.project_resource_quotas
           SET used = used + 1, updated_at = now()
         WHERE carrier_type = v_nested_kind AND carrier_id = v_parent AND kind = v_nested_kind
           AND used < limit_value;

        IF NOT FOUND THEN
            PERFORM kacho_registry.kacho_quota_refuse(v_nested_kind, v_parent, v_nested_kind);
        END IF;
    END IF;

    UPDATE kacho_registry.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    PERFORM kacho_registry.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL; -- недостижимо: производитель отказа всегда возбуждает исключение
END;
$$;

COMMENT ON FUNCTION kacho_registry.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. Charges the PARENT carrier before the project carrier '
    '(acceptance V2-6)';

-- -----------------------------------------------------------------------------
-- Жизненный цикл строки учёта РОДИТЕЛЯ.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION kacho_registry.kacho_quota_carrier_lifecycle()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_nested_kind text := TG_ARGV[0];
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM kacho_registry.project_resource_quotas
         WHERE carrier_type = v_nested_kind AND carrier_id = OLD.id;
        RETURN NULL;
    END IF;

    INSERT INTO kacho_registry.project_resource_quotas
        (carrier_type, carrier_id, kind, used, limit_value,
         source_scope, source_scope_id, limit_revision, account_id)
    SELECT v_nested_kind, NEW.id, v_nested_kind, 0, d.limit_value,
           d.source_scope, d.source_scope_id, d.limit_revision, d.account_id
      FROM kacho_registry.nested_quota_defaults d
     WHERE d.project_id = NEW.project_id AND d.kind = v_nested_kind
    ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING;

    RETURN NULL;
END;
$$;

COMMENT ON FUNCTION kacho_registry.kacho_quota_carrier_lifecycle() IS
    'creates and removes the accounting row of a PARENT carrier in the same '
    'transaction as the parent itself, so the row always has a producer';

-- -----------------------------------------------------------------------------
-- Два тенантных вида домена плюс одна ось вложенности.
-- -----------------------------------------------------------------------------
-- Токены выписаны из закрытой таблицы грантуемых пар: у этого домена вторая
-- часть во МНОЖЕСТВЕННОМ числе (`registry.repositories`, не
-- `registry.repository`).
DROP TRIGGER IF EXISTS registries_quota_count ON kacho_registry.registries;
CREATE TRIGGER registries_quota_count
    AFTER INSERT OR DELETE ON kacho_registry.registries
    FOR EACH ROW EXECUTE FUNCTION kacho_registry.kacho_quota_count('registry.registries');

DROP TRIGGER IF EXISTS repo_registration_quota_count
    ON kacho_registry.registry_repository_registration;
CREATE TRIGGER repo_registration_quota_count
    AFTER INSERT OR DELETE ON kacho_registry.registry_repository_registration
    FOR EACH ROW EXECUTE FUNCTION kacho_registry.kacho_quota_count(
        'registry.repositories',
        'registry_id',
        'registry.registries.repositories');

DROP TRIGGER IF EXISTS registries_quota_carrier ON kacho_registry.registries;
CREATE TRIGGER registries_quota_carrier
    AFTER INSERT OR DELETE ON kacho_registry.registries
    FOR EACH ROW EXECUTE FUNCTION kacho_registry.kacho_quota_carrier_lifecycle(
        'registry.registries.repositories');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_registry, public;

DROP TRIGGER IF EXISTS registries_quota_carrier ON kacho_registry.registries;
DROP TRIGGER IF EXISTS repo_registration_quota_count
    ON kacho_registry.registry_repository_registration;
DROP TRIGGER IF EXISTS registries_quota_count ON kacho_registry.registries;
DROP TRIGGER IF EXISTS repo_registration_project
    ON kacho_registry.registry_repository_registration;

DROP FUNCTION IF EXISTS kacho_registry.kacho_quota_carrier_lifecycle();
DROP FUNCTION IF EXISTS kacho_registry.kacho_quota_count();
DROP FUNCTION IF EXISTS kacho_registry.kacho_quota_admit(text, text, text);
DROP FUNCTION IF EXISTS kacho_registry.kacho_quota_refuse(text, text, text);
DROP FUNCTION IF EXISTS kacho_registry.kacho_repo_registration_project();

-- Столбец снимается вместе с механизмом, которому он служил: второго
-- потребителя у него нет.
ALTER TABLE kacho_registry.registry_repository_registration
    DROP COLUMN IF EXISTS project_id;

DROP TABLE IF EXISTS kacho_registry.nested_quota_defaults;
DROP TABLE IF EXISTS kacho_registry.project_resource_quotas;
-- +goose StatementEnd
