-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- =============================================================================
-- Адрес и его подсеть принадлежат ОДНОМУ проекту — конструкцией базы.
-- =============================================================================
-- Инвариант держался единственным местом в коде: `assertSubnetOwned`
-- (`apps/kacho/api/address/create.go`) читал подсеть запросом
-- `SELECT … FROM subnets WHERE id=$1` через ЧИТАЮЩЕЕ соединение и сравнивал
-- `sub.ProjectID != projectID` в Go. Это «прочитал → сравнил → записал», причём
-- чтение и запись лежат в РАЗНЫХ транзакциях: обе точки вызова стоят до открытия
-- writer-TX, а между синхронной проверкой и асинхронной записью проходит ещё и
-- граница Operation — окно произвольной длины.
--
-- На уровне базы равенства не было выражено ничем. Существовавший внешний ключ
-- (`internal_subnet_id → subnets(id)`, миграция 0001) знал только о
-- СУЩЕСТВОВАНИИ подсети и о проекте не спрашивал, поэтому любой писатель, минующий
-- этот один use-case, вставлял адрес со ссылкой на подсеть чужого проекта, и база
-- принимала строку. Замер до этой миграции (проба
-- `repo/address_subnet_project_pair_integration_test.go`, вставка через порт
-- репозитория): из восьми параллельных транзакций, из которых проект совпадал
-- ровно у одной, прошли ВСЕ ВОСЕМЬ.
--
-- Почему это не «мелочь на будущее»: проект — граница арендатора. Строка адреса,
-- чей проект отличается от проекта подсети, — это ссылка через границу арендатора
-- внутри одной базы, и она же основание для решения о доступе (владелец
-- резолвится по `project_id` строки). Такой инвариант обязан держаться
-- конструкцией, а не порядком операторов в одном вызывающем (правило #10).
--
-- Форма — составной внешний ключ на ПАРУ. Пара выбрана, а не проверка внутри
-- обмена: обе строки лежат в одной базе и одной схеме, поэтому у ссылки есть
-- прямое выражение, и оно действует на КАЖДОГО писателя — включая тот путь,
-- которого ещё нет.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- 1. Цель ссылки — пара (проект, идентификатор) подсети.
--
-- `id` уже первичный ключ, поэтому пара уникальна by construction; ограничение
-- заводится не ради уникальности, а потому что цель составного внешнего ключа
-- обязана быть объявлена уникальной. Побочный и намеренный эффект: `project_id`
-- подсети становится неизменяемым, пока на неё ссылается хоть один адрес
-- (`ON UPDATE NO ACTION` — умолчание), то есть «перенести подсеть в другой
-- проект» перестаёт быть выразимым мимо адресов, которые в ней живут.
ALTER TABLE kacho_vpc.subnets
    ADD CONSTRAINT subnets_project_id_id_key UNIQUE (project_id, id);
-- +goose StatementEnd

-- +goose StatementBegin
-- 2. Строки, которые прежнее соглашение пропустило, — НАЗВАТЬ, а не вычистить
--    молча.
--
-- Ключ ниже отверг бы их и сам, но его сообщение называет одну строку и не
-- говорит оператору ни сколько их, ни что с ними делать. Здесь предмет другой —
-- диагностика для того, кто применяет миграцию; решение остаётся за ключом.
-- Молчаливое удаление недопустимо: строка адреса принадлежит арендатору, и
-- миграция не вправе распорядиться ею вместо владельца.
DO $$
DECLARE
    violating bigint;
BEGIN
    SELECT count(*) INTO violating
      FROM kacho_vpc.addresses a
      JOIN kacho_vpc.subnets  s ON s.id = a.internal_subnet_id
     WHERE s.project_id <> a.project_id;
    IF violating > 0 THEN
        RAISE EXCEPTION
            'kacho_vpc: % address row(s) reference a subnet of another project; '
            'resolve them (delete or re-create in the owning project) before applying 0033. '
            'Predicate: SELECT a.id, a.project_id, s.id, s.project_id FROM kacho_vpc.addresses a '
            'JOIN kacho_vpc.subnets s ON s.id = a.internal_subnet_id WHERE s.project_id <> a.project_id',
            violating;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- 3. Сам ключ.
--
-- `MATCH SIMPLE` (умолчание) — здесь это несущее свойство, а не деталь: у внешнего
-- адреса `internal_subnet_id` равен NULL, и на таких строках ключ НЕ проверяется
-- вовсе. Именно поэтому пара выражает ровно то, что нужно, и ничего сверх:
-- «есть ссылка на подсеть ⇒ подсеть того же проекта», а не «у каждого адреса есть
-- подсеть».
--
-- `ON DELETE RESTRICT` повторяет поведение снимаемого ниже одностолбцового ключа:
-- живой адрес по-прежнему не даёт удалить свою подсеть.
ALTER TABLE kacho_vpc.addresses
    ADD CONSTRAINT addresses_subnet_project_fk
    FOREIGN KEY (project_id, internal_subnet_id)
    REFERENCES kacho_vpc.subnets (project_id, id)
    ON DELETE RESTRICT;
-- +goose StatementEnd

-- +goose StatementBegin
-- 4. Снятие прежнего одностолбцового ключа — он поглощён парой ЦЕЛИКОМ.
--
-- `project_id` объявлен NOT NULL, поэтому пара проверяется всякий раз, когда
-- проверялся бы прежний ключ, и её выполнение влечёт его выполнение. Оставить оба
-- значило бы держать две конструкции об одном предмете; хуже того — какой из двух
-- назовётся в SQLSTATE 23503, решает порядок в каталоге, и классификация отказа
-- в коде стала бы зависеть от него.
--
-- Имя ключа порождено Postgres автоматически (`<таблица>_<колонка>_fkey`), и
-- полагаться на эту форму нельзя — она свойство версии, а не наша. Ключ ищется по
-- каталогу: внешний ключ таблицы адресов на таблицу подсетей ровно по одной
-- колонке `internal_subnet_id`. Пара под этот предикат не подпадает (у неё две
-- колонки), поэтому только что заведённое ограничение не может быть снято этим же
-- блоком.
DO $$
DECLARE
    victim text;
BEGIN
    SELECT c.conname INTO victim
      FROM pg_constraint c
      JOIN pg_class      t ON t.oid = c.conrelid
      JOIN pg_class      f ON f.oid = c.confrelid
      JOIN pg_namespace  n ON n.oid = t.relnamespace
     WHERE c.contype = 'f'
       AND n.nspname = 'kacho_vpc'
       AND t.relname = 'addresses'
       AND f.relname = 'subnets'
       AND cardinality(c.conkey) = 1
       AND c.conkey[1] = (SELECT a.attnum FROM pg_attribute a
                           WHERE a.attrelid = t.oid AND a.attname = 'internal_subnet_id');
    IF victim IS NULL THEN
        RAISE EXCEPTION
            'kacho_vpc: single-column FK addresses.internal_subnet_id -> subnets(id) not found; '
            '0033 expected to find and replace it — the schema is not the one this migration was written against';
    END IF;
    EXECUTE format('ALTER TABLE kacho_vpc.addresses DROP CONSTRAINT %I', victim);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
COMMENT ON CONSTRAINT addresses_subnet_project_fk ON kacho_vpc.addresses IS
    'address and the subnet it references belong to the same project; '
    'NULL internal_subnet_id (external address) is out of scope by MATCH SIMPLE';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_vpc, public;

-- Возврат ровно к состоянию 0001: существование подсети без вопроса о проекте.
-- Имя восстанавливаемого ключа задаётся явно и совпадает с тем, которое Postgres
-- породил бы сам, — чтобы форма схемы после Down была той же, а не «похожей».
ALTER TABLE kacho_vpc.addresses
    DROP CONSTRAINT IF EXISTS addresses_subnet_project_fk;

ALTER TABLE kacho_vpc.addresses
    ADD CONSTRAINT addresses_internal_subnet_id_fkey
    FOREIGN KEY (internal_subnet_id) REFERENCES kacho_vpc.subnets (id)
    ON DELETE RESTRICT;

ALTER TABLE kacho_vpc.subnets
    DROP CONSTRAINT IF EXISTS subnets_project_id_id_key;
-- +goose StatementEnd
