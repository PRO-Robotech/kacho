-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- Наблюдаемое состояние машины — то, что видит узел, а не то, что мы просили.
--
-- ДВЕ КОЛОНКИ, А НЕ ОДНА. Существующая `status` — намерение: её пишет путь
-- запроса от арендатора. Эти — наблюдение: их пишет узловой агент отчётом.
-- Держать оба факта одной колонкой значит потерять расхождение между ними, а
-- именно расхождение и есть то, ради чего наблюдаемое собирают: пока их нет,
-- «мы просили запустить» и «оно запущено» неотличимы.
--
-- ПОЧЕМУ НАБЛЮДАЕМОЕ ДОПУСКАЕТ ПУСТОТУ. Машина, о которой узел ещё не
-- отчитался, — законное состояние: агента может не быть вовсе (сегодня его и
-- нет), связь может быть потеряна, отчёт может не успеть. Пустое наблюдаемое
-- означает «не знаем», и это честнее, чем подставить намерение и выдать его за
-- факт.
--
-- ПОЧЕМУ ВРЕМЯ УЗЛА, А НЕ НАШЕ. Узел мог наблюдать факт заметно раньше, чем
-- сумел сообщить. Подмена своим временем стёрла бы эту разницу — единственный
-- след того, что связь была потеряна.

-- +goose Up

ALTER TABLE instances
    -- Наблюдаемое состояние. NULL = узел ещё не отчитался.
    ADD COLUMN observed_state text,

    -- Номер события доставки, по которому агент получил намерение. Служит
    -- признаком свежести: меньший номер отбрасывается как устаревший.
    ADD COLUMN observed_sequence_no bigint,

    -- Момент наблюдения ПО ЧАСАМ УЗЛА.
    ADD COLUMN observed_at timestamptz,

    -- Причина, если наблюдаемое требует пояснения.
    ADD COLUMN observed_reason text NOT NULL DEFAULT '';

-- Набор совпадает с перечислением контракта ПОЭЛЕМЕНТНО.
--
-- Значение, которое код умеет прислать, а база не умеет принять, даёт отказ
-- хранилища без имени поля — сигнал, по которому нельзя понять, что именно не
-- так. Обратное расхождение хуже: база принимает то, чего контракт не
-- объявляет, и наблюдаемое становится свободной строкой, которую никто не
-- сможет ни сгруппировать, ни отфильтровать.
ALTER TABLE instances
    ADD CONSTRAINT instances_observed_state_check
    CHECK (observed_state IS NULL OR observed_state IN (
        'OBSERVED_STARTING',
        'OBSERVED_RUNNING',
        'OBSERVED_STOPPING',
        'OBSERVED_STOPPED',
        'OBSERVED_TERMINATED_UNEXPECTEDLY'
    ));

-- Три поля наблюдения заполняются вместе либо не заполняются вовсе.
--
-- Состояние без номера события не с чем сравнить на свежесть; номер без
-- состояния ничего не описывает. Частично заполненная тройка выглядит
-- осведомлённой и не является ею.
ALTER TABLE instances
    ADD CONSTRAINT instances_observed_triple_check
    CHECK (
        (observed_state IS NULL AND observed_sequence_no IS NULL AND observed_at IS NULL)
        OR
        (observed_state IS NOT NULL AND observed_sequence_no IS NOT NULL AND observed_at IS NOT NULL)
    );

-- Номер события монотонен и неотрицателен: ноль означал бы «намерение нулевой
-- доставки», которого не бывает.
ALTER TABLE instances
    ADD CONSTRAINT instances_observed_seq_check
    CHECK (observed_sequence_no IS NULL OR observed_sequence_no > 0);

-- Поиск расхождений: машины, где наблюдаемое отстало от намерения либо его нет
-- вовсе. Это запрос оператора, а не арендатора, и он частичный — машины с
-- согласованным состоянием в выборке не участвуют.
CREATE INDEX instances_observed_drift_idx
    ON instances (project_id, id)
    WHERE observed_state IS NULL;

-- +goose Down

DROP INDEX IF EXISTS instances_observed_drift_idx;

ALTER TABLE instances
    DROP CONSTRAINT IF EXISTS instances_observed_seq_check,
    DROP CONSTRAINT IF EXISTS instances_observed_triple_check,
    DROP CONSTRAINT IF EXISTS instances_observed_state_check,
    DROP COLUMN IF EXISTS observed_reason,
    DROP COLUMN IF EXISTS observed_at,
    DROP COLUMN IF EXISTS observed_sequence_no,
    DROP COLUMN IF EXISTS observed_state;
