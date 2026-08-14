-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Именованный набор префиксов (CidrGroup) + ссылка правила на него.
-- =============================================================================
-- Правило группы безопасности несло цель одним из двух: собственный набор блоков
-- либо ссылка на другую группу. Арендатор, у которого один и тот же перечень
-- сетей повторяется в двадцати правилах, правит двадцать мест и рано или поздно
-- правит не все. Именованный набор делает перечень ПРЕДМЕТОМ.
--
-- Каждый инвариант ниже выражен конструкцией базы, и по каждому названо, чем он
-- был бы в коде — потому что «в коде» здесь означает check-then-act, который под
-- конкуренцией перестаёт выполняться ровно тогда, когда он нужен:
--
--   имя уникально в проекте        частичный UNIQUE (project_id, name) WHERE name <> ''
--                                  вместо «прочитал по имени → нет → вставил»: две
--                                  параллельные Create с одним именем проходят ОБЕ.
--   блок не задваивается           PRIMARY KEY (group_id, block)
--                                  вместо «прочитал набор → блока нет → дописал»:
--                                  второй писатель теряет вставку первого.
--   состав уходит с набором        FK ON DELETE CASCADE (та же база)
--                                  вместо второго стейтмента: падение между ними
--                                  оставляет блоки без владельца.
--   потолок на семейство           счётчик на родителе + CHECK; инкремент условным
--                                  UPDATE в той же транзакции, что вставка.
--                                  Строка-счётчик под row-lock сериализует писателей
--                                  ПО ПОСТРОЕНИЮ: второй ждёт коммита, видит новое
--                                  значение и упирается в предикат.
--   набор с живой ссылкой не       FK ON DELETE RESTRICT с проекции ссылок правил
--   удаляется                      вместо «спросил, есть ли ссылки → нет → удалил»:
--                                  правило, созданное между вопросом и удалением,
--                                  осталось бы с висячей ссылкой. FK отвечает В МОМЕНТ
--                                  удаления, потому что вставка ссылки берёт на строке
--                                  набора KEY SHARE-блокировку, конфликтующую с DELETE.
--
-- `EXCLUDE USING gist` на пересечение блоков здесь осознанно НЕ ставится. У
-- именованного набора пересечение — не дефект: набор перечисляет то, что
-- перечисляет вызывающий, и «область непересечения» у него не определена.
-- Исключающее ограничение у пула и у подсети стоит там, где из диапазона
-- ВЫДЕЛЯЕТСЯ адрес и пересечение выдавало бы один адрес дважды. Поставить его
-- здесь значило бы придумать инвариант.
--
-- Идемпотентность (IF NOT EXISTS / DO-guard): защищает от повторного или
-- параллельного migrate-init (helm rollout запускает goose из нескольких подов).

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

CREATE TABLE IF NOT EXISTS kacho_vpc.cidr_groups (
    id          text        PRIMARY KEY,
    -- project_id — cross-service ссылка (владелец — iam), FK невозможен by
    -- construction (database-per-service). Существование проверяет peer-вызов на
    -- пути запроса, здесь — только колонка.
    project_id  text        NOT NULL,
    name        text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    labels      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- v4_count / v6_count — материализованная кардинальность состава ПО
    -- СЕМЕЙСТВАМ. Она существует не ради скорости, а ради потолка: CHECK не умеет
    -- считать строки дочерней таблицы, а условный инкремент этой колонки берёт
    -- row-lock родителя и тем сериализует конкурентные вставки.
    v4_count    integer     NOT NULL DEFAULT 0,
    v6_count    integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cidr_groups_name_check
        CHECK (name ~ '^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$'),
    CONSTRAINT cidr_groups_description_check
        CHECK (length(description) <= 256),
    CONSTRAINT cidr_groups_labels_valid
        CHECK (kacho_vpc.kacho_labels_valid(labels)),
    -- Потолок НА СЕМЕЙСТВО — 64, как у супернета сети и диапазонов подсети.
    -- Зеркало в коде — domain.MaxCidrGroupBlocks; значения обязаны совпадать, и
    -- это утверждает проба TestMaxCidrGroupBlocks_MatchesSiblingSets вместе с
    -- интеграционной пробой на сам CHECK.
    CONSTRAINT cidr_groups_cidr_cardinality
        CHECK (v4_count BETWEEN 0 AND 64 AND v6_count BETWEEN 0 AND 64)
);

-- Пустое имя дублей не образует: оно косметическое и его отсутствие — законное
-- состояние, а не значение, которым можно занять слот.
CREATE UNIQUE INDEX IF NOT EXISTS cidr_groups_project_id_name_key
    ON kacho_vpc.cidr_groups (project_id, name) WHERE name <> '';
CREATE INDEX IF NOT EXISTS cidr_groups_project_idx    ON kacho_vpc.cidr_groups (project_id);
CREATE INDEX IF NOT EXISTS cidr_groups_created_at_idx ON kacho_vpc.cidr_groups (created_at, id);

-- Состав — нормализованная дочерняя таблица, а НЕ два массива text[]: по массиву
-- «этот блок уже в наборе» не выражается ничем, и идемпотентность добавления
-- пришлось бы держать чтением-перезаписью целого массива. Приём тот же, что у
-- диапазонов подсети (0010) и блоков пула (0004).
--
-- Тип `cidr` (не text) сам отвергает запись с ненулевыми host-битами и приводит
-- значение к канонической форме — то есть дедупликация по PRIMARY KEY работает по
-- ЗНАЧЕНИЮ префикса, а не по написанию. Семейство читается функцией family(block)
-- и отдельной колонкой не дублируется.
CREATE TABLE IF NOT EXISTS kacho_vpc.cidr_group_blocks (
    group_id text NOT NULL
        REFERENCES kacho_vpc.cidr_groups(id) ON DELETE CASCADE,
    block    cidr NOT NULL,

    CONSTRAINT cidr_group_blocks_pkey PRIMARY KEY (group_id, block)
);
CREATE INDEX IF NOT EXISTS cidr_group_blocks_group_family_idx
    ON kacho_vpc.cidr_group_blocks (group_id, family(block));

-- Таблица очередного характера: она перекладывается целиком на каждое изменение
-- состава. Без собственных параметров автоочистки последний сбор статистики почти
-- всегда снят на пустой таблице, и планировщик входит во всплеск с оценкой в одну
-- строку.
ALTER TABLE kacho_vpc.cidr_group_blocks SET (
    autovacuum_analyze_scale_factor = 0.0,
    autovacuum_analyze_threshold    = 1000,
    autovacuum_vacuum_scale_factor  = 0.0,
    autovacuum_vacuum_threshold     = 1000
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Проекция ссылок правил на наборы.
--
-- Правила живут в JSONB-колонке `security_groups.rules`, поэтому объявить FK
-- прямо на них нельзя: внешний ключ — свойство КОЛОНКИ, а не элемента документа.
-- Проекция закрывает этот разрыв, не перенося правила в таблицу: строки в ней
-- поддерживает триггер в ТОЙ ЖЕ транзакции, что и запись правил, а FK на
-- `cidr_groups` объявлен RESTRICT.
--
-- Почему это настоящий инвариант, а не проверка в другом месте: вставка строки
-- проекции берёт на строке набора блокировку KEY SHARE, которая конфликтует с
-- удалением этой строки. Значит правило, создаваемое ОДНОВРЕМЕННО с удалением
-- набора, либо дожидается удаления и получает отказ по FK, либо удерживает набор
-- и удаление отвергается. Окна «спросил → удалил» не остаётся.
CREATE TABLE IF NOT EXISTS kacho_vpc.security_group_rule_cidr_group_refs (
    security_group_id text NOT NULL
        REFERENCES kacho_vpc.security_groups(id) ON DELETE CASCADE,
    rule_id           text NOT NULL,
    cidr_group_id     text NOT NULL
        REFERENCES kacho_vpc.cidr_groups(id) ON DELETE RESTRICT,

    CONSTRAINT security_group_rule_cidr_group_refs_pkey
        PRIMARY KEY (security_group_id, rule_id, cidr_group_id)
);
CREATE INDEX IF NOT EXISTS security_group_rule_cidr_group_refs_group_idx
    ON kacho_vpc.security_group_rule_cidr_group_refs (cidr_group_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Синхронизация проекции с правилами группы.
--
-- Ключ документа — `CidrGroupID`: правила сериализуются кодировщиком Go БЕЗ
-- переименования полей, поэтому имена ключей совпадают с именами полей
-- структуры. Тот же приём уже применён миграцией 0029, которая читала
-- `PredefinedTarget`.
--
-- Триггер объявлен на UPDATE ЦЕЛИКОМ, а не на `UPDATE OF rules`. Второе выглядит
-- точнее и сегодня достаточно, но перестанет быть достаточным в день, когда у
-- таблицы появится BEFORE-триггер, выставляющий NEW.rules сам: тогда столбец в
-- SET не назван и сужённый триггер не сработает. Цена общей формы — сравнение
-- двух документов на обновлении группы, и оно же служит ранним выходом.
CREATE OR REPLACE FUNCTION kacho_vpc.kacho_sg_rule_cidr_group_refs_sync()
RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.rules IS NOT DISTINCT FROM OLD.rules THEN
        RETURN NULL;
    END IF;

    DELETE FROM kacho_vpc.security_group_rule_cidr_group_refs
     WHERE security_group_id = NEW.id;

    INSERT INTO kacho_vpc.security_group_rule_cidr_group_refs
        (security_group_id, rule_id, cidr_group_id)
    SELECT DISTINCT
           NEW.id,
           COALESCE(r.value ->> 'ID', ''),
           r.value ->> 'CidrGroupID'
      FROM jsonb_array_elements(
               CASE WHEN jsonb_typeof(NEW.rules) = 'array'
                    THEN NEW.rules ELSE '[]'::jsonb END) AS r(value)
     WHERE COALESCE(r.value ->> 'CidrGroupID', '') <> '';

    RETURN NULL;
END;
$fn$;

-- CREATE OR REPLACE TRIGGER есть с PG 14, но в этом дереве принят DROP+CREATE
-- (идемпотентность без зависимости от версии) — как в 0031.
DROP TRIGGER IF EXISTS security_groups_cidr_group_refs_sync ON kacho_vpc.security_groups;
CREATE TRIGGER security_groups_cidr_group_refs_sync
    AFTER INSERT OR UPDATE ON kacho_vpc.security_groups
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_sg_rule_cidr_group_refs_sync();

COMMENT ON TABLE kacho_vpc.security_group_rule_cidr_group_refs IS
    'derived projection of security_groups.rules[].CidrGroupID, maintained by trigger '
    'in the same transaction as the rules write; the RESTRICT foreign key on it is what '
    'keeps a referenced CidrGroup from being deleted';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

DROP TRIGGER IF EXISTS security_groups_cidr_group_refs_sync ON kacho_vpc.security_groups;
DROP FUNCTION IF EXISTS kacho_vpc.kacho_sg_rule_cidr_group_refs_sync();
DROP TABLE IF EXISTS kacho_vpc.security_group_rule_cidr_group_refs;
DROP TABLE IF EXISTS kacho_vpc.cidr_group_blocks;
DROP TABLE IF EXISTS kacho_vpc.cidr_groups;

-- +goose StatementEnd
