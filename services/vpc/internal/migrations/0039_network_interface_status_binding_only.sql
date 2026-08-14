-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- =============================================================================
-- Статус интерфейса выражает ПРИВЯЗКУ — и больше ничего.
-- =============================================================================
-- `NetworkInterface.Status` объявлял шесть значений. Производились два:
-- `AVAILABLE` ставят создание интерфейса и его отвязка, `ACTIVE` — привязка.
-- У `PROVISIONING`, `FAILED` и `DELETING` не было производителя НИ ОДНОГО, при
-- том что их комментарии в контракте заявляли программирование датаплейна — то
-- есть ровно тот вопрос, на который отвечает поле состояния применения.
--
-- Контракт, отвечающий на один вопрос двумя способами, из которых один мёртв,
-- заставляет вызывающего выбирать между ними без правильного ответа. Три
-- значения сняты с контракта; здесь за ними сужается ограничение столбца.
--
-- Почему сужение обязательно, а не «контракта достаточно»
-- -----------------------------------------------------------------------------
-- Снятие с контракта убирает ИМЯ, а не состояние. Пока ограничение принимает
-- шесть значений, строка со снятым значением остаётся ЗАПИСЫВАЕМОЙ — посевом,
-- ручной правкой, будущим путём записи, — и при этом НЕВЫРАЗИМОЙ: перевод в
-- контракт отдаёт её как `STATUS_UNSPECIFIED`, то есть арендатор видит «состояние
-- неизвестно» там, где в базе стоит вполне определённая строка. Инвариант держит
-- база, а не намерение писать правильно (запрет #10).
--
-- Направление обратного заполнения — из АВТОРИТЕТА, а не из вероятности
-- -----------------------------------------------------------------------------
-- Это поле выражает привязку, и авторитет привязки — `used_by_id`. Поэтому:
-- строка со снятым значением получает `ACTIVE`, если потребитель у неё есть, и
-- `AVAILABLE` иначе. Никакого «наиболее вероятного» значения здесь не
-- придумывается: придумать значило бы записать утверждение о состоянии, которого
-- никто не наблюдал.
--
-- Ожидаемое число переписанных строк — НОЛЬ (производителя у снятых значений не
-- было), и именно поэтому оно печатается вместе с числом осмотренных: миграция,
-- ничего не нашедшая, и миграция, ничего не посмотревшая, выглядят одинаково
-- ровно до того дня, когда разница понадобится.

SET search_path TO kacho_vpc, public;

-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    rows_examined  bigint := 0;
    rows_rewritten bigint := 0;
    to_active      bigint := 0;
    to_available   bigint := 0;
BEGIN
    -- Разбивка считается ДО правки: после неё снятых значений в таблице уже нет,
    -- и «сколько куда ушло» стало бы невосстановимым — то есть печатать было бы
    -- нечего именно тогда, когда печатать и нужно.
    SELECT count(*),
           count(*) FILTER (WHERE status IN ('PROVISIONING','FAILED','DELETING') AND used_by_id <> ''),
           count(*) FILTER (WHERE status IN ('PROVISIONING','FAILED','DELETING') AND used_by_id =  '')
      INTO rows_examined, to_active, to_available
      FROM kacho_vpc.network_interfaces;

    UPDATE kacho_vpc.network_interfaces
       SET status = CASE WHEN used_by_id <> '' THEN 'ACTIVE' ELSE 'AVAILABLE' END
     WHERE status IN ('PROVISIONING', 'FAILED', 'DELETING');
    GET DIAGNOSTICS rows_rewritten = ROW_COUNT;

    RAISE NOTICE 'network_interfaces status narrow: examined % row(s); rewritten % row(s) '
                 '(retired PROVISIONING/FAILED/DELETING derived from used_by_id: '
                 '% to ACTIVE, % to AVAILABLE)',
        rows_examined, rows_rewritten, to_active, to_available;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin

-- Ограничение сужается до состава контракта. `NULL` остаётся допустимым по той же
-- причине, по какой он был допустим раньше: колонка объявлена NOT NULL с
-- умолчанием, и условие с `IS NULL` здесь — не послабление, а сохранение формы
-- предиката baseline.
ALTER TABLE kacho_vpc.network_interfaces
    DROP CONSTRAINT IF EXISTS network_interfaces_status_check;

ALTER TABLE kacho_vpc.network_interfaces
    ADD CONSTRAINT network_interfaces_status_check
    CHECK (status IS NULL OR status IN ('ACTIVE', 'AVAILABLE', 'STATUS_UNSPECIFIED'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

SET search_path TO kacho_vpc, public;

-- Откат возвращает ШИРОКОЕ ограничение и НЕ восстанавливает снятые значения:
-- восстанавливать неоткуда — исходное значение строки уничтожено переписью, а не
-- сохранено. Это сказано прямо, чтобы откат не читался как обратимый.
ALTER TABLE kacho_vpc.network_interfaces
    DROP CONSTRAINT IF EXISTS network_interfaces_status_check;

ALTER TABLE kacho_vpc.network_interfaces
    ADD CONSTRAINT network_interfaces_status_check
    CHECK (status IS NULL OR status IN ('PROVISIONING','ACTIVE','AVAILABLE','FAILED','DELETING','STATUS_UNSPECIFIED'));

-- +goose StatementEnd
