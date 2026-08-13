-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0016: класс диска несёт политику; выдуманный посев снят.
--
-- WHY (посев). Миграция 0004 засеяла пять классов с описаниями, обещающими
-- избыточность и производительность («high-IOPS low-latency», «single-replica
-- non-redundant», «maximum IOPS premium»). Ни одно из этих обещаний облако выполнить
-- не может: их выполняет команда, эксплуатирующая хранилище. Класс — РЕГИСТРАЦИЯ
-- того, что провайдер действительно даёт, поэтому заранее выдуманного каталога быть
-- не должно. Пустой каталог — законное состояние: пока класс не зарегистрирован,
-- том не создаётся, и это правильная закрытая посадка, а не поломка.
--
-- Снятие УСЛОВНОЕ. Класс, на который ссылается хоть один том, остаётся: безусловное
-- удаление упёрлось бы в ограничительную внешнюю связь и уронило бы миграцию, то
-- есть заблокировало старт сервиса. И по существу: класс, на котором лежат чужие
-- данные, «не обещать» уже нельзя.
--
-- WHY (ярус словарём). Ярус был свободной строкой. Публичную поверхность от
-- инфра-лексики стережёт проба, перечисляющая ИМЕНА полей, — значение вида
-- «nvme-fast» или «pool-b-replicated» проходит сквозь неё. Словарь закрывает канал
-- по значениям. Прежние строчные значения приводятся к словарю тем же изменением:
-- оставить их значило бы уронить ограничение на собственных данных.
--
-- WHY (состояние обращения). Вывести класс из обращения было НЕЧЕМ: единственной
-- защитой была ограничительная связь, поэтому класс с томами не выводился никогда,
-- а класс без томов сносился мгновенно и молча. Состояние отвечает на единственный
-- вопрос — принимает ли класс НОВЫЕ тома; существующие живут при любом значении.
--
-- Способности здесь НЕ заводятся: они свойство бэкенда и живут на ревизии привязки
-- (0015). Публичный класс выводит их пересечением по действующим ревизиям — иначе
-- завелись бы два источника истины об одном факте, и они разошлись бы.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DELETE FROM kacho_storage.disk_types d
 WHERE d.id IN ('block-standard','block-balanced','block-fast','block-single','block-io-max')
   AND NOT EXISTS (SELECT 1 FROM kacho_storage.volumes v WHERE v.disk_type_id = d.id);

ALTER TABLE kacho_storage.disk_types
    ADD COLUMN lifecycle       text   NOT NULL DEFAULT 'ACTIVE',
    ADD COLUMN min_size_bytes  bigint NOT NULL DEFAULT 0,
    ADD COLUMN max_size_bytes  bigint NOT NULL DEFAULT 0,
    ADD COLUMN size_step_bytes bigint NOT NULL DEFAULT 0;

UPDATE kacho_storage.disk_types
   SET performance_tier = CASE lower(performance_tier)
        WHEN 'standard' THEN 'CAPACITY'
        WHEN 'balanced' THEN 'BALANCED'
        WHEN 'fast'     THEN 'FAST'
        WHEN 'single'   THEN 'SINGLE'
        WHEN 'io-max'   THEN 'IO_MAX'
        WHEN 'io_max'   THEN 'IO_MAX'
        WHEN 'capacity' THEN 'CAPACITY'
        ELSE ''
       END
 WHERE performance_tier <> '';

ALTER TABLE kacho_storage.disk_types
    ADD CONSTRAINT disk_types_tier_known
        CHECK (performance_tier IN ('','CAPACITY','BALANCED','FAST','SINGLE','IO_MAX')),
    ADD CONSTRAINT disk_types_lifecycle_known
        CHECK (lifecycle IN ('ACTIVE','DEPRECATED','RETIRED')),
    ADD CONSTRAINT disk_types_size_bounds_not_negative
        CHECK (min_size_bytes >= 0 AND max_size_bytes >= 0 AND size_step_bytes >= 0),
    ADD CONSTRAINT disk_types_size_bounds_ordered
        CHECK (min_size_bytes = 0 OR max_size_bytes = 0 OR min_size_bytes <= max_size_bytes),
    ADD CONSTRAINT disk_types_size_bounds_on_step
        CHECK (size_step_bytes = 0
               OR (min_size_bytes % size_step_bytes = 0 AND max_size_bytes % size_step_bytes = 0));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

ALTER TABLE kacho_storage.disk_types
    DROP CONSTRAINT IF EXISTS disk_types_size_bounds_on_step,
    DROP CONSTRAINT IF EXISTS disk_types_size_bounds_ordered,
    DROP CONSTRAINT IF EXISTS disk_types_size_bounds_not_negative,
    DROP CONSTRAINT IF EXISTS disk_types_lifecycle_known,
    DROP CONSTRAINT IF EXISTS disk_types_tier_known,
    DROP COLUMN IF EXISTS size_step_bytes,
    DROP COLUMN IF EXISTS max_size_bytes,
    DROP COLUMN IF EXISTS min_size_bytes,
    DROP COLUMN IF EXISTS lifecycle;

UPDATE kacho_storage.disk_types
   SET performance_tier = lower(performance_tier)
 WHERE performance_tier <> '';
-- +goose StatementEnd
