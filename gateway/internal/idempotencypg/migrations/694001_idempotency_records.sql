-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1
--
-- 694001 — общее хранилище однократности края (Idempotency-Key).
--
-- Домен параллелизма гарантии — ФЛОТ подов края, а не один процесс, поэтому
-- запись о погашенном ключе обязана лежать там, где её видят все реплики.
-- Ключ — первичный, и это не украшение: именно уникальность ключа делает
-- допуск атомарным (INSERT … ON CONFLICT), то есть держит инвариант оператором
-- базы, а не программной парой «посмотреть — записать» (правило #10).
--
-- Строка живёт до `expires_at` и убирается сборщиком — иначе хранилище растёт
-- без границы.

-- +goose Up
CREATE SCHEMA IF NOT EXISTS kacho_gateway;

CREATE TABLE IF NOT EXISTS kacho_gateway.idempotency_records (
    -- Отпечаток запроса: principal + метод + путь + Idempotency-Key + sha256 тела.
    key              TEXT        PRIMARY KEY,
    -- Держатель брони. Непрозрачная строка, выданная тем процессом, который
    -- выиграл допуск; он предъявляет её обратно, чтобы записать исход. Пустая —
    -- у законченной записи (держателя больше нет).
    lease_owner      TEXT        NOT NULL,
    -- Срок брони. Держатель, умерший, не оставив исхода (упавший под), отдаёт
    -- ключ по истечении срока, а не навсегда.
    lease_expires_at TIMESTAMPTZ NOT NULL,
    -- Исход записан.
    done             BOOLEAN     NOT NULL DEFAULT FALSE,
    status_code      INTEGER,
    content_type     TEXT        NOT NULL DEFAULT '',
    body             BYTEA,
    -- Срок годности самой записи (TTL ключа).
    expires_at       TIMESTAMPTZ NOT NULL,
    -- Законченная запись обязана нести ответ: иначе повтор отдал бы пустоту,
    -- выдав её за сохранённый ответ.
    CONSTRAINT idempotency_records_done_has_answer
        CHECK (NOT done OR status_code IS NOT NULL)
);

-- Обход сборщика идёт по сроку годности.
CREATE INDEX IF NOT EXISTS idempotency_records_expires_at_idx
    ON kacho_gateway.idempotency_records (expires_at);

-- +goose Down
DROP TABLE IF EXISTS kacho_gateway.idempotency_records;
