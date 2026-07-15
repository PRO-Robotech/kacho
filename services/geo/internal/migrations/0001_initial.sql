-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Базовая схема kacho-geo. Схема kacho_geo. Плоские ресурсы (без K8s-envelope).
-- Region/Zone — глобальные cluster-scoped read-only справочники; admin CRUD идет
-- через Internal* (:9091). Все id-колонки — TEXT (admin-assigned "region-1" /
-- "region-1-a").

CREATE SCHEMA IF NOT EXISTS kacho_geo;
SET search_path TO kacho_geo, public;

-- ---------------------------------------------------------------------------
-- regions — глобальный справочник регионов (id = "region-1"). PK назначается
-- админом.
-- ---------------------------------------------------------------------------
CREATE TABLE regions (
  id         TEXT         PRIMARY KEY,         -- "region-1"
  name       TEXT         NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- zones — зоны доступности (id = "region-1-a"), принадлежат региону.
-- zones.region_id → regions(id) ON DELETE RESTRICT: регион с зонами удалить
-- нельзя (within-service инвариант на DB-уровне).
-- ---------------------------------------------------------------------------
CREATE TABLE zones (
  id         TEXT         PRIMARY KEY,                  -- "region-1-a"
  region_id  TEXT         NOT NULL
               REFERENCES regions (id) ON DELETE RESTRICT, -- FK RESTRICT: регион с зонами не удалить
  status     TEXT         NOT NULL DEFAULT 'UP',        -- UP | DOWN | STATUS_UNSPECIFIED
  name       TEXT         NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX zones_region_idx ON zones (region_id);

-- ---------------------------------------------------------------------------
-- geo_outbox — audit-outbox для admin-мутаций Region/Zone (конвенция
-- <domain>_outbox, parity с compute_outbox / vpc_outbox). Структура совпадает
-- с corelib-хелпером outbox.Emit: (sequence_no, resource_kind, resource_id,
-- event_type, payload, created_at). Строки пишутся АТОМАРНО в той же admin
-- writer-tx (аудит не теряется).
-- ---------------------------------------------------------------------------
CREATE TABLE geo_outbox (
  sequence_no   BIGSERIAL    PRIMARY KEY,
  resource_kind TEXT         NOT NULL,        -- Region | Zone
  resource_id   TEXT         NOT NULL,
  event_type    TEXT         NOT NULL,        -- CREATED | UPDATED | DELETED
  payload       JSONB        NOT NULL DEFAULT '{}'::jsonb,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  processed_at  TIMESTAMPTZ
);
CREATE INDEX geo_outbox_seq_idx  ON geo_outbox (sequence_no);
CREATE INDEX geo_outbox_kind_idx ON geo_outbox (resource_kind, sequence_no);

-- +goose StatementBegin
CREATE FUNCTION geo_outbox_notify() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('geo_outbox', NEW.sequence_no::text);
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER geo_outbox_notify_trg AFTER INSERT ON geo_outbox
  FOR EACH ROW EXECUTE FUNCTION geo_outbox_notify();

-- Каталог регионов и зон стартует пустым: записи заводит администратор вручную
-- через Internal{Region,Zone}Service.Create — встроенного seed нет.

-- +goose Down
SET search_path TO kacho_geo, public;
DROP TRIGGER IF EXISTS geo_outbox_notify_trg ON geo_outbox;
DROP FUNCTION IF EXISTS geo_outbox_notify();
DROP TABLE IF EXISTS geo_outbox;
DROP TABLE IF EXISTS zones;
DROP TABLE IF EXISTS regions;
DROP SCHEMA IF EXISTS kacho_geo;
