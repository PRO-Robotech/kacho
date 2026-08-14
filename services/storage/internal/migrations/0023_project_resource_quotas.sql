-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Учёт числа ресурсов арендатора у kacho-storage: списание и возврат
-- КОНСТРУКЦИЕЙ БАЗЫ.
-- =============================================================================
-- Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
-- (APPROVED, раунд 2), DoD S4 п.1 — механизм стадии S2 повторён у второго
-- владельца. Величину назначает kacho-iam; учёт и отказ живут здесь, у владельца
-- типа ресурса.
--
-- ПОЧЕМУ УЧЁТ ВЕДЁТ ТРИГГЕР, А НЕ USE-CASE — и почему у storage этот довод
-- СИЛЬНЕЕ, чем у соседа. Списание обязано быть верным для КАЖДОГО писателя.
-- У storage путей вставки строки ресурса **шесть**, и они не сводятся к одному
-- «создать»: том вставляется одним путём, снимок — двумя (заведение и КОПИЯ в
-- другую зону), образ — тремя (заведение, РЕГИСТРАЦИЯ уже лежащего у бэкенда
-- объекта и КОПИЯ в другой регион). Замер на ревизии `6762cea2`, предикат
-- воспроизводим:
--
--   for t in volumes snapshots images; do
--     git grep -c "INSERT INTO $t" -- 'services/storage/**/*.go' ':!*_test.go' \
--       | awk -F: '{s+=$2} END{print s+0}'
--   done                                   # → 1, 2, 3
--   # контроль (тот же предикат по пробам, чтобы «ноль» было отличимо от
--   # «предикат слеп»): → 4, 5, 5
--
-- Соглашение «каждый писатель зовёт списание» пришлось бы соблюсти в шести
-- местах сразу и в каждом седьмом, которое появится, — а именно «появился
-- писатель, о котором не вспомнили» и есть механизм, которым счётчик
-- разъезжается с реальностью. Триггер отвечает всем писателям и стоит в ТОЙ ЖЕ
-- транзакции, что сама мутация: «строка ресурса закоммичена» и «место списано»
-- суть одно событие, а не два, которые можно рассогласовать. Возврат на каждом
-- пути высвобождения получается by construction, а не перечислением путей.
--
-- ПОЧЕМУ ПОТОЛОК НЕ СТОИТ `CHECK`-ОМ. `CHECK (used <= limit_value)` запрещает
-- состояние «занято больше, чем разрешено» — и тем самым запрещает САМО
-- ПОНИЖЕНИЕ предела: администратор, желающий ограничить проект, получал бы
-- 23514, пока проект сам не освободит место, то есть административное действие
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
-- вниманием; этот файл назван там как единственный законный писатель `used` в
-- домене storage.
--
-- «НЕ СКАЗАНО» = ОТКАЗ. Отсутствие строки учёта НЕ означает «без предела».
-- Обратная трактовка уже существует в дереве (тот же прецедент compute) и
-- измерена как механизм, не отказавший ни разу за всю свою жизнь. Здесь промах —
-- отказ, и он отличим от исчерпания своим SQLSTATE: одно требует от
-- администратора ЗАВЕСТИ потолок, другое — ПОДНЯТЬ его.
--
-- ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ. (а) Признака СИСТЕМНОГО ребёнка: у storage системных
-- детей не бывает — все шесть путей вставки заводят строку по заявке арендатора
-- и в его проекте, ни одна миграция строк ресурсов не вносит (предикат:
-- `grep -rn 'INSERT INTO \(volumes\|snapshots\|images\)'
-- services/storage/internal/migrations/*.sql` → пусто). Параметр, которому нечего
-- параметризовать, был бы мёртвой ветвью, и снять её потом стоило бы дороже, чем
-- не заводить. (б) Учёта ПРИВЯЗОК тома к машине: `volume_attachments` живёт
-- здесь и несёт свой `project_id`, поэтому выглядит кандидатом на учёт ровно так
-- же, как сами тома, — но привязка НЕ ресурс, а отношение между двумя уже
-- посчитанными. Считай её, и предел на тома молча стал бы пределом на привязки:
-- арендатор, переставивший один том между машинами, платил бы местом дважды.
-- Утверждается пробой (`TestQuota_AttachingAVolumeMovesNoCounter`) с
-- положительным контролем, а не оставлено на прочтение этого абзаца.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

-- Строка учёта — на тройку (носитель, id носителя, вид).
--
-- Носитель, а не просто проект: та же таблица понесёт предел вложенности, если
-- у домена появится ось «детей в одном родителе». Ключ выбран тройкой сразу,
-- чтобы вторая ось не потребовала второй таблицы и второго разрешения
-- старшинства — двух мест об одном предмете, из которых верным окажется одно.
--
-- Имя таблицы совпадает с именем у соседа НАМЕРЕННО: схемы разные
-- (`kacho_storage` против `kacho_vpc`), коллизии нет, а гейт дерева ищет
-- писателей `used` ПО ИМЕНИ таблицы — разойдись имена, домен storage выпал бы
-- из-под его наблюдения молча.
CREATE TABLE IF NOT EXISTS kacho_storage.project_resource_quotas (
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

COMMENT ON TABLE kacho_storage.project_resource_quotas IS
    'per-carrier resource-count accounting for kacho-storage; the ceiling lives '
    'in the WHERE of the charging statement, never in a CHECK, because a CHECK '
    'would make lowering a limit below current usage inexpressible';

-- Адресация аккаунтной дельты: строки одного аккаунта.
CREATE INDEX IF NOT EXISTS project_resource_quotas_account_idx
    ON kacho_storage.project_resource_quotas (account_id, kind);

-- Возраст старейшего несинхронизированного снимка — наблюдаемая величина.
CREATE INDEX IF NOT EXISTS project_resource_quotas_synced_at_idx
    ON kacho_storage.project_resource_quotas (synced_at);

-- -----------------------------------------------------------------------------
-- ЕДИНСТВЕННЫЙ производитель отказа учёта.
-- -----------------------------------------------------------------------------
-- Зовётся ТОЛЬКО тогда, когда списание или совещательная проверка уже
-- установили, что места нет. Сама ничего не решает — она классифицирует уже
-- случившийся отказ и облекает его в контракт: текст, SQLSTATE и машинные
-- подробности.
--
-- Производитель один, а не два согласованных: написанные порознь, тексты
-- расходятся на первой правке, и расхождение НЕ ВИДНО ни одной из сторон —
-- каждая по отдельности верна, а арендатор получает на один отказ два разных
-- сообщения в зависимости от того, какая полоса сработала первой. Требование
-- приёмки «байт-идентичны» удовлетворяется здесь ПОСТРОЕНИЕМ: согласовывать
-- можно только два места, а место одно.
CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_refuse(
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
      FROM kacho_storage.project_resource_quotas
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

COMMENT ON FUNCTION kacho_storage.kacho_quota_refuse(text, text, text) IS
    'the ONLY producer of a quota refusal: both the advisory read and the '
    'authoritative charge call it, so their text, SQLSTATE and details cannot '
    'drift apart — there is one place, not two agreeing ones';

-- -----------------------------------------------------------------------------
-- Совещательная полоса.
-- -----------------------------------------------------------------------------
-- Возвращает void, когда место есть, и отказывает через единственного
-- производителя, когда его нет. Ничего не пишет: чтение остаётся чтением.
--
-- Она НЕ является решением: между её ответом и вставкой помещается чужая запись.
-- Это не дефект, а роль — решение принимает атомарный `UPDATE … WHERE used <
-- limit_value` авторитетной полосы (ban #10). Совещательная существует ради
-- РАННЕГО отказа: без неё исчерпание предела наблюдается арендатором как «200 и
-- операция, упавшая через секунду».
--
-- Отдельная функция, а не запрос из Go, ровно ради байт-идентичности: запрос из
-- Go пришлось бы снабдить СВОИМ текстом отказа, и второе место завелось бы
-- обратно.
CREATE OR REPLACE FUNCTION kacho_storage.kacho_quota_admit(
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
      FROM kacho_storage.project_resource_quotas
     WHERE carrier_type = v_carrier_type AND carrier_id = v_carrier_id AND kind = v_kind;

    IF COALESCE(v_ok, false) THEN
        RETURN;
    END IF;

    PERFORM kacho_storage.kacho_quota_refuse(v_carrier_type, v_carrier_id, v_kind);
END;
$$;

COMMENT ON FUNCTION kacho_storage.kacho_quota_admit(text, text, text) IS
    'advisory band: says whether a slot is available WITHOUT taking it. Never a '
    'decision — the decision is the conditional UPDATE of the charging trigger; '
    'this exists so the tenant is refused early and in the same words';

-- -----------------------------------------------------------------------------
-- Списание и возврат — ОДНА функция, параметризованная видом.
-- -----------------------------------------------------------------------------
-- Одна, а не три: «завели четвёртый вид и забыли скопировать логику» должно быть
-- невыразимо. Вид приезжает `TG_ARGV[0]` — той же формой, что вид приезжает в
-- проекцию намерений соседнего домена.
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
        -- Возврат — в той же транзакции, что удаление строки ресурса. Возврат
        -- вне её оставил бы счётчик завышенным при откате: проект платил бы
        -- местом за ресурс, которого нет. GREATEST не даёт уйти ниже нуля, если
        -- строка учёта была заведена позже самих ресурсов.
        UPDATE kacho_storage.project_resource_quotas
           SET used = GREATEST(used - 1, 0), updated_at = now()
         WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind;
        RETURN NULL;
    END IF;

    -- Списание. Единственный оператор, берущий блокировку строки: второй
    -- писатель ждёт коммита первого и видит его результат. Чтение с последующим
    -- сравнением этого не даёт — между ними помещается чужая запись, и оба
    -- создателя прошли бы проверку, увидев одно и то же свободное место (ban #10).
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

COMMENT ON FUNCTION kacho_storage.kacho_quota_count() IS
    'charges one slot on insert and returns it on delete, in the same transaction '
    'as the resource row. The refusal itself is produced by kacho_quota_refuse, '
    'shared with the advisory band, so the two bands cannot word it differently';

-- Три тенантных вида домена. Перечень — из закрытого каталога видов
-- (`services/iam/internal/domain/limit.go`, записи `storage.*`), а не из имён
-- таблиц: форма второй части токена у этого домена МНОЖЕСТВЕННАЯ
-- (`storage.volumes`, не `storage.volume`), и написанный по памяти токен не
-- резолвился бы каталогом вовсе.
DROP TRIGGER IF EXISTS volumes_quota_count ON kacho_storage.volumes;
CREATE TRIGGER volumes_quota_count
    AFTER INSERT OR DELETE ON kacho_storage.volumes
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.kacho_quota_count('storage.volumes');

DROP TRIGGER IF EXISTS snapshots_quota_count ON kacho_storage.snapshots;
CREATE TRIGGER snapshots_quota_count
    AFTER INSERT OR DELETE ON kacho_storage.snapshots
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.kacho_quota_count('storage.snapshots');

DROP TRIGGER IF EXISTS images_quota_count ON kacho_storage.images;
CREATE TRIGGER images_quota_count
    AFTER INSERT OR DELETE ON kacho_storage.images
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.kacho_quota_count('storage.images');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP TRIGGER IF EXISTS images_quota_count ON kacho_storage.images;
DROP TRIGGER IF EXISTS snapshots_quota_count ON kacho_storage.snapshots;
DROP TRIGGER IF EXISTS volumes_quota_count ON kacho_storage.volumes;

DROP FUNCTION IF EXISTS kacho_storage.kacho_quota_count();
DROP FUNCTION IF EXISTS kacho_storage.kacho_quota_admit(text, text, text);
DROP FUNCTION IF EXISTS kacho_storage.kacho_quota_refuse(text, text, text);

DROP TABLE IF EXISTS kacho_storage.project_resource_quotas;
-- +goose StatementEnd
