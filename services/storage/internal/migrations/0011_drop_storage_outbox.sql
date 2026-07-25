-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- 0011: снятие доменного storage_outbox (0005) — очередь БЕЗ потребителя.
--
-- WHY. Таблица писалась в КАЖДОЙ writer-tx Volume/Snapshot/Image
-- (outbox.Emit CREATED/UPDATED/DELETED), но её backlog не читал НИКТО:
--   * заголовок 0005 утверждал «downstream-консюмер (fgaproxy RegisterResource/
--     UnregisterResource owner-tuple) читает backlog по sequence_no» — это ЛОЖНОЕ
--     заявление. owner-tuple идёт через ОТДЕЛЬНУЮ таблицу fga_register_outbox
--     (0006), и заголовок самой 0006 прямо фиксирует, что доменный storage_outbox
--     драйнеру НЕ подходит (PK sequence_no, processed_at, нет attempt_count);
--   * единственная подписка сервиса (cmd/storage/register_drainer.go) слушает
--     канал `kacho_storage_fga_register_outbox`, а НЕ `storage_outbox`;
--   * на канал `storage_outbox` не подписан ни один LISTEN во всём репозитории.
--
-- Аналог в compute (compute_outbox) потребителя ИМЕЕТ — InternalWatchService.Watch
-- стримит его через LISTEN/NOTIFY. У storage такого сервиса нет: в
-- proto/kacho/cloud/storage/v1/ нет ни Watch-, ни InternalWatch-контракта, а
-- data-plane storage осознанно out-of-scope (Volume.GetInternal — контрактный
-- Unimplemented). Аналог в geo (geo_outbox) — audit-outbox: он пишет actor'а
-- (CWE-778). storage_outbox actor'а НЕ несёт (payload = {id, project_id, zone_id}),
-- поэтому audit-ценности у него тоже нет.
--
-- Итог: строки копились вечно (processed_at никогда не проставлялся, retention
-- нет), а каждая мутация тома/снапшота/образа платила лишний INSERT + plpgsql-
-- триггер + pg_notify на канал без слушателей. Это утечка, а не резерв под
-- будущее: очередь корректно вводится ВМЕСТЕ со своим драйнером — в той же
-- миграции, что и потребитель (как это сделано для 0006).
--
-- Что теряем: доменный поток событий CREATED/UPDATED/DELETED, который в будущем
-- мог бы кормить storage-аналог InternalWatchService. Он вводится обратно вместе
-- с этим сервисом. Что НЕ теряем: материализацию owner-tuple в FGA (живёт на
-- fga_register_outbox 0006), audit-след (его тут не было) и тестовое покрытие
-- (ни один тест не читал storage_outbox).
--
-- Применённую 0005 не редактируем (ban #5) — снятие идёт отдельной миграцией.

-- +goose Up
-- +goose StatementBegin
SET search_path TO kacho_storage, public;

DROP TRIGGER IF EXISTS storage_outbox_notify_trg ON kacho_storage.storage_outbox;
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_notify();
DROP TABLE IF EXISTS kacho_storage.storage_outbox;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Откат воссоздаёт структуру 0005 один-в-один (без ложного заявления о
-- потребителе). Накопленный backlog не восстанавливается — его никто не читал.
SET search_path TO kacho_storage, public;

CREATE TABLE IF NOT EXISTS kacho_storage.storage_outbox (
    sequence_no   BIGSERIAL    PRIMARY KEY,
    resource_kind TEXT         NOT NULL,        -- Volume | Snapshot | Image
    resource_id   TEXT         NOT NULL,
    event_type    TEXT         NOT NULL,        -- CREATED | UPDATED | DELETED
    payload       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS storage_outbox_seq_idx  ON kacho_storage.storage_outbox (sequence_no);
CREATE INDEX IF NOT EXISTS storage_outbox_kind_idx ON kacho_storage.storage_outbox (resource_kind, sequence_no);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_notify() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('storage_outbox', NEW.sequence_no::text);
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER storage_outbox_notify_trg AFTER INSERT ON kacho_storage.storage_outbox
  FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_notify();
-- +goose StatementEnd
