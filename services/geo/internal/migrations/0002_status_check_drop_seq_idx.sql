-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- (a) CHECK на zones.status — оживляет маппинг 23514→ErrInvalidArg (без CHECK'ов
--     он был мёртв). Допустимые значения совпадают с domain ZoneStatus
--     (UP / DOWN / STATUS_UNSPECIFIED).
-- (b) DROP избыточного индекса geo_outbox_seq_idx: sequence_no — BIGSERIAL
--     PRIMARY KEY, уже проиндексирован PK (отдельный btree-индекс дублирует его).
SET search_path TO kacho_geo, public;

ALTER TABLE zones
  ADD CONSTRAINT zones_status_check CHECK (status IN ('UP','DOWN','STATUS_UNSPECIFIED'));

DROP INDEX IF EXISTS kacho_geo.geo_outbox_seq_idx;

-- +goose Down
SET search_path TO kacho_geo, public;

CREATE INDEX IF NOT EXISTS geo_outbox_seq_idx ON geo_outbox (sequence_no);

ALTER TABLE zones
  DROP CONSTRAINT IF EXISTS zones_status_check;
