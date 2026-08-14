-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Намерение для исполнителя датаплейна: ревизия объекта, снятие намерения,
-- горизонт продолжения и подтверждение применения.
-- =============================================================================
-- Control plane объявляет НАМЕРЕНИЕ (сеть, подсеть, интерфейс, адрес, правило,
-- шлюз, маршрут), исполнитель датаплейна его применяет. Доставка намерения —
-- поток `InternalDataplaneService.WatchIntent` (:9091). Эта миграция строит то,
-- на чём поток стоит, и строит это КОНСТРУКЦИЕЙ БАЗЫ, а не соглашением кода.
--
-- ПОЧЕМУ ПРОЕКЦИЯ ВЕДЁТСЯ ТРИГГЕРАМИ, А НЕ ВЫЗОВАМИ ИЗ USE-CASE'ОВ.
-- Проекция обязана быть полной: объект, изменённый мимо неё, исполнителю не
-- доедет НИКОГДА — не «с задержкой», а никогда, потому что поток идёт по
-- ревизиям, и невыданная ревизия не появится задним числом. Соглашение «каждый
-- писатель зовёт эмиттер» держится ровно до первого нового пути записи:
-- воркера, административной правки, восстановления из дампа, чужой writer-TX.
-- Триггер отвечает КАЖДОМУ писателю и стоит в ТОЙ ЖЕ транзакции, что и сама
-- мутация, — то есть «строка закоммичена» и «ревизия выдана» суть одно событие,
-- а не два, которые можно рассогласовать.
--
-- ПОЧЕМУ РЕВИЗИЯ БЕРЁТСЯ ПОД БЛОКИРОВКОЙ, А НЕ ПРОСТО ИЗ ПОСЛЕДОВАТЕЛЬНОСТИ.
-- Последовательность выдаёт номера в порядке ОБРАЩЕНИЯ, а видимость строк
-- определяется порядком ФИКСАЦИИ. Без связи между ними возможно ровно то, ради
-- чего весь этот механизм и не нужен: транзакция A взяла ревизию 5 и держит
-- открытой, транзакция B взяла 6 и зафиксировалась; исполнитель читает, видит 6,
-- двигает курсор на 6; затем фиксируется A. Изменение с ревизией 5 не будет
-- отдано НИКОГДА — курсор его уже прошёл. Потерянное изменение неотличимо от
-- непришедшего, и заметить это нечем.
--
-- `pg_advisory_xact_lock` держится до конца транзакции, поэтому взять номер и не
-- зафиксироваться раньше того, кто взял номер до тебя, — невозможно: следующий
-- писатель не получит номер, пока предыдущий не отпустит блокировку фиксацией
-- или откатом. Порядок выдачи становится порядком фиксации, и утверждение
-- «всё, что ≤ N, я уже видел» делается верным, а не правдоподобным.
--
-- Цена названа честно: транзакции, меняющие намерение, сериализуются между
-- собой на хвосте (блокировка берётся AFTER-триггером, то есть в конце
-- мутации). Для control plane это допустимо — частота мутаций низка, а
-- альтернатива есть молчаливая потеря доставки.
--
-- ЧТО ЗДЕСЬ НАМЕРЕННО НЕ ДУБЛИРУЕТСЯ. Проекция не хранит тела объекта. Тело
-- читается из самой таблицы ресурса в ОДНОМ снимке с курсором (поток открывает
-- REPEATABLE READ), поэтому расходиться телу с источником истины нечем. Копия
-- тела в журнале была бы вторым местом об одном предмете — и первым же, которое
-- отстанет при правке схемы ресурса.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Последовательность ревизий — ОДНА на весь домен.
--
-- Одна, а не по одной на вид объекта: исполнитель продолжает поток с единой
-- позиции, и сравнивать позиции из разных последовательностей было бы нечем.
-- `START WITH 1` — ноль остаётся выразимым как «состояния у меня нет».
CREATE SEQUENCE IF NOT EXISTS kacho_vpc.dataplane_intent_revision_seq
    AS bigint START WITH 1 INCREMENT BY 1 NO CYCLE;
-- +goose StatementEnd

-- +goose StatementBegin
-- Проекция намерения: одна строка на объект, живой или снятый.
--
-- Ключ — ТОЛЬКО `resource_id`, без вида объекта. Идентификаторы ресурсов
-- глобально уникальны by construction (запрет #15), поэтому вид в ключе был бы
-- вторым способом сказать то же самое, а подтверждение применения смогло бы
-- назвать объект «не тем видом» и промахнуться мимо существующей строки.
-- Уникальность ключа заодно ПРОВЕРЯЕТ это допущение: совпади идентификаторы
-- двух ресурсов — вставка упадёт громко, а не разъедется молча.
CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_intent (
    resource_id text        PRIMARY KEY,
    kind        text        NOT NULL,
    revision    bigint      NOT NULL,
    withdrawn   boolean     NOT NULL DEFAULT false,
    stamped_at  timestamptz NOT NULL DEFAULT now(),

    -- Закрытый словарь видов. Вид вне словаря означает, что кто-то навесил
    -- триггер на таблицу, для которой у потока нет ветви контракта: такой объект
    -- доехал бы до исполнителя ничем. Отказ вставки лучше молчаливой доставки
    -- пустоты.
    CONSTRAINT dataplane_intent_kind_known CHECK (kind IN (
        'network', 'subnet', 'network_interface', 'security_group',
        'route_table', 'gateway', 'address')),

    -- Ревизия выдаётся последовательностью и всегда положительна; ноль занят
    -- доменным «состояния нет» на стороне исполнителя.
    CONSTRAINT dataplane_intent_revision_positive CHECK (revision > 0)
);

-- Ревизия уникальна ГЛОБАЛЬНО: она же — позиция потока. Две строки с одной
-- ревизией сделали бы курсор неоднозначным (продолжив «после 7», нельзя было бы
-- сказать, какая из двух семёрок уже отдана). Индекс одновременно обслуживает
-- единственный горячий запрос потока — `revision > $1 ORDER BY revision`.
CREATE UNIQUE INDEX IF NOT EXISTS dataplane_intent_revision_key
    ON kacho_vpc.dataplane_intent (revision);
-- +goose StatementEnd

-- +goose StatementBegin
-- Горизонт продолжения: ревизия, ниже которой поток продолжать НЕЛЬЗЯ.
--
-- Снятия намерений хранятся не вечно — иначе таблица растёт на каждый удалённый
-- ресурс и не убывает никогда. Уплотнение удаляет старые снятия и поднимает
-- горизонт до наибольшей удалённой ревизии. Исполнитель, чья позиция ниже
-- горизонта, мог пропустить снятие, след которого уже стёрт, — и единственный
-- честный ответ ему «начни с полной выдачи», а не «изменений нет».
--
-- Одна строка обеспечена конструкцией: первичный ключ по константному столбцу
-- плюс CHECK на его значение. Второй строке в этой таблице неоткуда взяться.
CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_intent_horizon (
    only_row boolean PRIMARY KEY DEFAULT true,
    revision bigint  NOT NULL DEFAULT 0,

    CONSTRAINT dataplane_intent_horizon_single CHECK (only_row),
    CONSTRAINT dataplane_intent_horizon_nonnegative CHECK (revision >= 0)
);

INSERT INTO kacho_vpc.dataplane_intent_horizon (only_row, revision)
VALUES (true, 0)
ON CONFLICT (only_row) DO NOTHING;
-- +goose StatementEnd

-- +goose StatementBegin
-- Подтверждение применения: что исполнитель доложил по объекту.
--
-- Строка на объект, а не журнал докладов: предмет — СОСТОЯНИЕ («на какой
-- ревизии датаплейн»), а не история попыток. Историю пишет журнал процесса;
-- держать её здесь значило бы завести вторую растущую таблицу без читателя.
--
-- Внешний ключ на проекцию с каскадом: подтверждение про объект, чьё намерение
-- уплотнено, не имеет предмета. Каскад — ВНУТРИ одной базы одного сервиса,
-- то есть ровно тот случай, когда каскад разрешён (запрет #4 про границу
-- сервиса, а не про свою схему).
CREATE TABLE IF NOT EXISTS kacho_vpc.dataplane_apply (
    resource_id      text        PRIMARY KEY
        REFERENCES kacho_vpc.dataplane_intent(resource_id) ON DELETE CASCADE,
    applied_revision bigint      NOT NULL,
    outcome          text        NOT NULL,
    reason           text        NOT NULL DEFAULT '',
    reported_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT dataplane_apply_outcome_known CHECK (outcome IN ('APPLIED', 'FAILED')),
    CONSTRAINT dataplane_apply_revision_positive CHECK (applied_revision > 0),

    -- Класс причины ЗАКРЫТ и согласован с исходом: у успеха причины неуспеха
    -- нет, у отказа она обязательна. Свободного текста в этом столбце нет
    -- вовсе — и это решение о безопасности: имени интерфейса на узле,
    -- идентификатору туннеля или номеру порта коммутатора сюда физически негде
    -- оказаться, поэтому проекция состояния арендатору безопасна by
    -- construction, а не по внимательности пишущего.
    CONSTRAINT dataplane_apply_reason_matches_outcome CHECK (
        (outcome = 'APPLIED' AND reason = '')
        OR (outcome = 'FAILED' AND reason IN (
            'CAPACITY', 'CONFLICT', 'UNSUPPORTED',
            'DEPENDENCY_NOT_READY', 'TRANSIENT', 'EXECUTOR_INTERNAL'))
    )
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Выдача ревизии. Одна функция на все виды: вид приезжает аргументом триггера,
-- поэтому «завели восьмую таблицу и забыли скопировать логику» невыразимо.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp()
RETURNS trigger
LANGUAGE plpgsql AS $fn$
DECLARE
    v_kind text := TG_ARGV[0];
    v_id   text;
    v_rev  bigint;
BEGIN
    -- Правка, ничего не изменившая, ревизию НЕ двигает: иначе `UPDATE … SET
    -- name = name` заставлял бы исполнителя переприменять неизменённое, а на
    -- потоке это неотличимо от настоящего изменения.
    IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN
        RETURN NULL;
    END IF;

    -- Ключ консультативной блокировки — ЛИТЕРАЛ, выбранный один раз под этот
    -- журнал, а не `hashtext` от имени таблицы: хэш зависит от версии базы и
    -- может совпасть с ключом постороннего модуля, а совпадение здесь означало
    -- бы взаимную сериализацию двух несвязанных подсистем без единого следа в
    -- коде обеих. Значение менять нельзя: сменив его, мы на время смешанного
    -- развёртывания получим два непересекающихся порядка выдачи.
    PERFORM pg_advisory_xact_lock(1289161342);

    v_rev := nextval('kacho_vpc.dataplane_intent_revision_seq');

    IF TG_OP = 'DELETE' THEN
        v_id := OLD.id;
        INSERT INTO kacho_vpc.dataplane_intent AS d (resource_id, kind, revision, withdrawn, stamped_at)
        VALUES (v_id, v_kind, v_rev, true, now())
        ON CONFLICT (resource_id) DO UPDATE
            SET revision = EXCLUDED.revision,
                withdrawn = true,
                stamped_at = EXCLUDED.stamped_at;
        RETURN NULL;
    END IF;

    v_id := NEW.id;
    INSERT INTO kacho_vpc.dataplane_intent AS d (resource_id, kind, revision, withdrawn, stamped_at)
    VALUES (v_id, v_kind, v_rev, false, now())
    ON CONFLICT (resource_id) DO UPDATE
        SET kind = EXCLUDED.kind,
            revision = EXCLUDED.revision,
            withdrawn = false,
            stamped_at = EXCLUDED.stamped_at;
    RETURN NULL;
END;
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Триггеры на все семь видов. AFTER — чтобы ревизия выдавалась только тому, что
-- реально прошло все ограничения строки: BEFORE-триггер выдал бы номер и
-- отказавшейся вставке, а дыра в последовательности читается как потерянное
-- изменение.
DROP TRIGGER IF EXISTS networks_dataplane_intent ON kacho_vpc.networks;
CREATE TRIGGER networks_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('network');

DROP TRIGGER IF EXISTS subnets_dataplane_intent ON kacho_vpc.subnets;
CREATE TRIGGER subnets_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('subnet');

DROP TRIGGER IF EXISTS network_interfaces_dataplane_intent ON kacho_vpc.network_interfaces;
CREATE TRIGGER network_interfaces_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.network_interfaces
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('network_interface');

DROP TRIGGER IF EXISTS security_groups_dataplane_intent ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('security_group');

DROP TRIGGER IF EXISTS route_tables_dataplane_intent ON kacho_vpc.route_tables;
CREATE TRIGGER route_tables_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.route_tables
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('route_table');

DROP TRIGGER IF EXISTS gateways_dataplane_intent ON kacho_vpc.gateways;
CREATE TRIGGER gateways_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.gateways
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('gateway');

DROP TRIGGER IF EXISTS addresses_dataplane_intent ON kacho_vpc.addresses;
CREATE TRIGGER addresses_dataplane_intent
    AFTER INSERT OR UPDATE OR DELETE ON kacho_vpc.addresses
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_dataplane_intent_stamp('address');
-- +goose StatementEnd

-- +goose StatementBegin
-- Обратное заполнение: у ресурсов, созданных ДО этой миграции, ревизии нет, и
-- без заполнения они не попали бы даже в полную выдачу — то есть исполнитель
-- считал бы их несуществующими и снёс бы применённое.
--
-- Порядок внутри заполнения безразличен: все строки заполняются одной
-- транзакцией миграции, а значит становятся видимы одновременно; никакой
-- исполнитель не может читать поток «в середине» этой транзакции.
INSERT INTO kacho_vpc.dataplane_intent (resource_id, kind, revision, withdrawn)
SELECT id, kind, nextval('kacho_vpc.dataplane_intent_revision_seq'), false
FROM (
    SELECT id, 'network'           AS kind FROM kacho_vpc.networks
    UNION ALL
    SELECT id, 'subnet'            AS kind FROM kacho_vpc.subnets
    UNION ALL
    SELECT id, 'network_interface' AS kind FROM kacho_vpc.network_interfaces
    UNION ALL
    SELECT id, 'security_group'    AS kind FROM kacho_vpc.security_groups
    UNION ALL
    SELECT id, 'route_table'       AS kind FROM kacho_vpc.route_tables
    UNION ALL
    SELECT id, 'gateway'           AS kind FROM kacho_vpc.gateways
    UNION ALL
    SELECT id, 'address'           AS kind FROM kacho_vpc.addresses
) AS existing
ON CONFLICT (resource_id) DO NOTHING;

COMMENT ON TABLE kacho_vpc.dataplane_intent IS
    'Desired-state projection for the data-plane executor: one row per object, '
    'revision issued in commit order, withdrawn=true is a tombstone';
COMMENT ON TABLE kacho_vpc.dataplane_apply IS
    'What the data-plane executor reported as applied; reason is a closed class, never free text';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

DROP TRIGGER IF EXISTS addresses_dataplane_intent ON kacho_vpc.addresses;
DROP TRIGGER IF EXISTS gateways_dataplane_intent ON kacho_vpc.gateways;
DROP TRIGGER IF EXISTS route_tables_dataplane_intent ON kacho_vpc.route_tables;
DROP TRIGGER IF EXISTS security_groups_dataplane_intent ON kacho_vpc.security_groups;
DROP TRIGGER IF EXISTS network_interfaces_dataplane_intent ON kacho_vpc.network_interfaces;
DROP TRIGGER IF EXISTS subnets_dataplane_intent ON kacho_vpc.subnets;
DROP TRIGGER IF EXISTS networks_dataplane_intent ON kacho_vpc.networks;
DROP FUNCTION IF EXISTS kacho_vpc.kacho_dataplane_intent_stamp();

DROP TABLE IF EXISTS kacho_vpc.dataplane_apply;
DROP TABLE IF EXISTS kacho_vpc.dataplane_intent_horizon;
DROP TABLE IF EXISTS kacho_vpc.dataplane_intent;
DROP SEQUENCE IF EXISTS kacho_vpc.dataplane_intent_revision_seq;
-- +goose StatementEnd
