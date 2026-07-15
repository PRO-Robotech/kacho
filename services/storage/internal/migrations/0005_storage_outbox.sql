-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- storage_outbox — транзакционный outbox мутаций Volume (конвенция <domain>_outbox,
-- parity с geo_outbox / vpc_outbox / compute_outbox). Структура совпадает с
-- corelib-хелпером outbox.Emit: (sequence_no, resource_kind, resource_id,
-- event_type, payload, created_at). Строка пишется АТОМАРНО в той же writer-tx,
-- что и доменный INSERT/UPDATE/DELETE volumes — событие не может разойтись с
-- состоянием (ban #16). Downstream-консюмер (fgaproxy RegisterResource/
-- UnregisterResource owner-tuple) читает backlog по sequence_no; trigger pg_notify
-- будит подписчиков.

SET search_path TO kacho_storage, public;

CREATE TABLE kacho_storage.storage_outbox (
    sequence_no   BIGSERIAL    PRIMARY KEY,
    resource_kind TEXT         NOT NULL,        -- Volume | Snapshot | DiskType
    resource_id   TEXT         NOT NULL,
    event_type    TEXT         NOT NULL,        -- CREATED | UPDATED | DELETED
    payload       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ
);
CREATE INDEX storage_outbox_seq_idx  ON kacho_storage.storage_outbox (sequence_no);
CREATE INDEX storage_outbox_kind_idx ON kacho_storage.storage_outbox (resource_kind, sequence_no);

-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION kacho_storage.storage_outbox_notify() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('storage_outbox', NEW.sequence_no::text);
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER storage_outbox_notify_trg AFTER INSERT ON kacho_storage.storage_outbox
  FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_notify();

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;
DROP TRIGGER IF EXISTS storage_outbox_notify_trg ON kacho_storage.storage_outbox;
DROP FUNCTION IF EXISTS kacho_storage.storage_outbox_notify();
DROP TABLE IF EXISTS kacho_storage.storage_outbox;
-- +goose StatementEnd
