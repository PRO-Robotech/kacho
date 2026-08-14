-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Учёт числа ресурсов арендатора: списание и возврат КОНСТРУКЦИЕЙ БАЗЫ.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), DoD S2 п.1-4. Величину назначает kacho-iam; учёт и отказ
-- живут здесь, у владельца типа ресурса.
--
-- ПОЧЕМУ УЧЁТ ВЕДЁТ ТРИГГЕР, А НЕ USE-CASE. Списание обязано быть верным для
-- КАЖДОГО писателя — будущего пути, каскада, реконсайлера, административного
-- SQL. Соглашение «каждый писатель зовёт списание» держится ровно до первого
-- нового пути записи, и именно «появился второй писатель» есть механизм, которым
-- счётчик разъезжается с реальностью. Триггер отвечает всем писателям и стоит в
-- ТОЙ ЖЕ транзакции, что сама мутация: «строка ресурса закоммичена» и «место
-- списано» суть одно событие, а не два, которые можно рассогласовать. Возврат на
-- каждом пути высвобождения получается by construction, а не перечислением
-- путей. Форма повторяет уже живущую здесь проекцию намерений (0032), где вид
-- приезжает `TG_ARGV[0]`, — а не изобретает вторую.
--
-- ПОЧЕМУ ПОТОЛОК НЕ СТОИТ `CHECK`-ОМ. `CHECK (used <= limit_value)` запрещает
-- состояние «занято больше, чем разрешено» — и тем самым запрещает САМО
-- ПОНИЖЕНИЕ предела: администратор, желающий ограничить проект, получал бы 23514,
-- пока проект сам не освободит место, то есть административное действие
-- становилось бы заложником того, кого оно ограничивает. Понижение штатно, а
-- `used > limit_value` означает ровно то, что после него нужно: новые нельзя,
-- старые живут, удаление работает. Потолок держит предикат `used < limit_value`
-- условного `UPDATE` — того же единственного оператора, что берёт блокировку
-- строки, поэтому свойство не слабее: ограничение схемы ловило бы превышение
-- ПОСЛЕ записи, предикат не даёт его записать. Прецедент, на котором это
-- измерено, — `services/compute/internal/migrations/0031_project_instance_limit.sql`.
--
-- ЦЕНА, НАЗВАННАЯ ЧЕСТНО. Инвариант держит предикат списания, а не ограничение
-- схемы, поэтому путь, который запишет `used` мимо триггера, потолок обойдёт.
-- Отсутствие такого пути доказывается гейтом по дереву
-- (`internal/repohygiene`, TestQuotaUsedIsWrittenOnlyByItsTrigger), а не
-- вниманием.
--
-- ТРИ ИСХОДА, И ОНИ РАЗЛИЧИМЫ. Ноль изменённых строк означает два РАЗНЫХ
-- состояния, и свести их к одному нельзя: «строки нет» — потолок не назван ни на
-- одной области; «строка есть, но полна» — место кончилось. Первое требует от
-- администратора завести потолок, второе — поднять его; один код на оба послал бы
-- его искать, что понизить, там, где ничего не назначено. Поэтому у них разные
-- SQLSTATE, и Go-сторона отображает их в разные gRPC-коды.
--
-- «НЕ СКАЗАНО» = ОТКАЗ. Отсутствие строки учёта НЕ означает «без предела».
-- Обратная трактовка уже существует в дереве (тот же прецедент compute) и
-- измерена как механизм, не отказавший ни разу за всю свою жизнь: строку предела
-- там не пишет ни один путь прод-кода, поэтому списание всегда получало ноль
-- строк, трактовало это как «предел не назначен» и пропускало. Форма без
-- содержания. Здесь промах — отказ, и он отличим от исчерпания.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Строка учёта — на тройку (носитель, id носителя, вид).
--
-- Носитель, а не просто проект: та же таблица понесёт предел вложенности
-- («подсетей в одной сети»), где носителем будет сам родитель. Ключ выбран
-- тройкой сразу, чтобы вторая ось не потребовала второй таблицы и второго
-- разрешения старшинства — двух мест об одном предмете, из которых верным
-- окажется одно.
CREATE TABLE IF NOT EXISTS kacho_vpc.project_resource_quotas (
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
);

COMMENT ON TABLE kacho_vpc.project_resource_quotas IS
    'per-carrier resource-count accounting; the ceiling lives in the WHERE of the '
    'charging statement, never in a CHECK, because a CHECK would make lowering a '
    'limit below current usage inexpressible';

-- Адресация аккаунтной дельты: строки одного аккаунта.
CREATE INDEX IF NOT EXISTS project_resource_quotas_account_idx
    ON kacho_vpc.project_resource_quotas (account_id, kind);

-- Возраст старейшего несинхронизированного снимка — наблюдаемая величина.
CREATE INDEX IF NOT EXISTS project_resource_quotas_synced_at_idx
    ON kacho_vpc.project_resource_quotas (synced_at);

-- -----------------------------------------------------------------------------
-- Признак происхождения у таблиц маршрутизации.
-- -----------------------------------------------------------------------------
-- У групп правил системность читается собственным столбцом
-- (`security_groups.default_for_network`, 0001). У таблиц маршрутизации такого
-- столбца нет: системность выражена ссылкой НА РОДИТЕЛЕ
-- (`networks.default_route_table_id`, 0017) — а на пути вставки ребёнка эта
-- ссылка ещё НЕ ПРОСТАВЛЕНА: `Network.Create` сначала вставляет строку таблицы
-- маршрутизации и лишь потом линкует её (`Insert` → `SetDefaultRouteTableID`).
-- Триггер, спрашивающий родителя, увидел бы «не системная» и списал бы место —
-- то есть предел на таблицы маршрутизации молча стал бы пределом на сети.
ALTER TABLE kacho_vpc.route_tables
    ADD COLUMN IF NOT EXISTS system_owned boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN kacho_vpc.route_tables.system_owned IS
    'true for the table Network.Create makes for its own network; such a child is '
    'not charged against the tenant ceiling. Read from the row itself because at '
    'child-insert time the parent link is not set yet';

-- Обратное заполнение ДЕТЕРМИНИРОВАНО: системны ровно те строки, на которые
-- ссылается своя сеть. Направление объявлено здесь, а число переписанных строк
-- печатается — иначе «ноль обновлённых» неотличимо от «ноль прочитанных».
DO $$
DECLARE
    v_rows bigint;
BEGIN
    UPDATE kacho_vpc.route_tables rt
       SET system_owned = true
      FROM kacho_vpc.networks n
     WHERE n.default_route_table_id = rt.id
       AND rt.system_owned IS DISTINCT FROM true;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RAISE NOTICE 'route_tables.system_owned backfill: % row(s) marked system-owned', v_rows;
END $$;

-- -----------------------------------------------------------------------------
-- Списание и возврат — ОДНА функция, параметризованная видом.
-- -----------------------------------------------------------------------------
-- Одна, а не восемь: «завели девятую таблицу и забыли скопировать логику» должно
-- быть невыразимо. Ровно эту форму держит проекция намерений 0032.
--
-- TG_ARGV[0] — вид (`vpc.network`); TG_ARGV[1] — имя булева столбца
-- происхождения, либо пусто, если у вида системных детей не бывает. Столбец
-- читается из САМОЙ строки через `to_jsonb`, а не запросом к родителю: см.
-- разбор выше.
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

    -- Системный ребёнок не тратит предел арендатора: его число привязано к числу
    -- родителей один к одному, а родители ограничены — значит он ограничен
    -- транзитивно. Счёт его против тенантного предела означал бы, что предел на
    -- группы правил молча становится пределом на сети.
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
        UPDATE kacho_vpc.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй писатель
    -- ждёт коммита первого и видит его результат. Чтение с последующим сравнением
    -- этого не даёт — между ними помещается чужая запись, и оба создателя пройдут
    -- проверку, увидев одно и то же свободное место (ban #10).
    UPDATE kacho_vpc.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND used < limit_value
    RETURNING used INTO v_charged;

    IF FOUND THEN
        RETURN NULL;
    END IF;

    -- Ноль строк — два разных состояния. Различаем их ОДНИМ дополнительным
    -- чтением той же строки. Это не check-then-act: решение о списании уже принято
    -- атомарным оператором выше, и это чтение ничего не решает — оно только
    -- классифицирует уже случившийся отказ.
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

COMMENT ON FUNCTION kacho_vpc.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. KQ001 = ceiling reached, KQ002 = no ceiling stated for '
    'this project and kind; the two are DELIBERATELY different SQLSTATEs because '
    'one asks the administrator to raise a limit and the other to state one';

-- Восемь тенантных видов домена. Перечень — из модели прав, а не из словаря
-- проекции намерений: `vpc.cidrGroup` несёт проект и свой тип модели, но
-- исполнителю не проецируется, и вывод перечня из проекции терял бы ровно его.
DROP TRIGGER IF EXISTS networks_quota_count ON kacho_vpc.networks;
CREATE TRIGGER networks_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.network');

DROP TRIGGER IF EXISTS subnets_quota_count ON kacho_vpc.subnets;
CREATE TRIGGER subnets_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.subnet');

DROP TRIGGER IF EXISTS addresses_quota_count ON kacho_vpc.addresses;
CREATE TRIGGER addresses_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.addresses
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.address');

DROP TRIGGER IF EXISTS network_interfaces_quota_count ON kacho_vpc.network_interfaces;
CREATE TRIGGER network_interfaces_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.network_interfaces
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.networkInterface');

DROP TRIGGER IF EXISTS security_groups_quota_count ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.securityGroup', 'default_for_network');

DROP TRIGGER IF EXISTS route_tables_quota_count ON kacho_vpc.route_tables;
CREATE TRIGGER route_tables_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.routeTable', 'system_owned');

DROP TRIGGER IF EXISTS gateways_quota_count ON kacho_vpc.gateways;
CREATE TRIGGER gateways_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.gateways
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.gateway');

DROP TRIGGER IF EXISTS cidr_groups_quota_count ON kacho_vpc.cidr_groups;
CREATE TRIGGER cidr_groups_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.cidr_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count('vpc.cidrGroup');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

DROP TRIGGER IF EXISTS cidr_groups_quota_count ON kacho_vpc.cidr_groups;
DROP TRIGGER IF EXISTS gateways_quota_count ON kacho_vpc.gateways;
DROP TRIGGER IF EXISTS route_tables_quota_count ON kacho_vpc.route_tables;
DROP TRIGGER IF EXISTS security_groups_quota_count ON kacho_vpc.security_groups;
DROP TRIGGER IF EXISTS network_interfaces_quota_count ON kacho_vpc.network_interfaces;
DROP TRIGGER IF EXISTS addresses_quota_count ON kacho_vpc.addresses;
DROP TRIGGER IF EXISTS subnets_quota_count ON kacho_vpc.subnets;
DROP TRIGGER IF EXISTS networks_quota_count ON kacho_vpc.networks;

DROP FUNCTION IF EXISTS kacho_vpc.kacho_quota_count();

-- Столбец происхождения снимается вместе с механизмом, которому он служил:
-- второго потребителя у него нет.
ALTER TABLE kacho_vpc.route_tables DROP COLUMN IF EXISTS system_owned;

DROP TABLE IF EXISTS kacho_vpc.project_resource_quotas;
-- +goose StatementEnd
