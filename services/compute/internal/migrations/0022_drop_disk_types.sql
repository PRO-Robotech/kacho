-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Storage-split contract phase, last of the four: the DiskType catalogue.
--
-- kacho-storage owns DiskType and serves it at /storage/v1/diskTypes, with its own
-- table and its own seed (migration 0004 there). compute served a duplicate of the
-- same catalogue — a public read pair plus admin CRUD over a second copy of the
-- rows. Two catalogues of the same thing is a question with two answers: a volume
-- created against storage's `network-ssd` and one created against compute's were
-- validated by different tables that nothing kept in agreement.
--
-- This drops last of the four because the retired Disk resource read it: a compute
-- Disk validated its `type_id` here. With Disk gone (migration 0021) the table has
-- no reader left.
--
-- The rows here are catalogue, not tenant data — seeded by migration 0001, never
-- written by a tenant. Dropping them loses nothing a tenant created, and storage
-- seeds the same catalogue on its own side.
DROP TABLE IF EXISTS disk_types;

-- +goose Down
-- Recreate the catalogue table in its pre-retire shape (0001 baseline) and restore
-- the seed rows 0001 inserted, so a rollback lands on the state the Up left.
CREATE TABLE disk_types (
  id          TEXT        PRIMARY KEY,
  description TEXT        NOT NULL DEFAULT '',
  zone_ids    JSONB       NOT NULL DEFAULT '[]'::jsonb,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO disk_types (id, description, zone_ids) VALUES
  ('network-hdd',                'Standard network HDD',                 '["ru-central1-a","ru-central1-b","ru-central1-d"]'::jsonb),
  ('network-ssd',                'Fast network SSD (replicated)',        '["ru-central1-a","ru-central1-b","ru-central1-d"]'::jsonb),
  ('network-ssd-nonreplicated',  'High-performance non-replicated SSD',  '["ru-central1-a","ru-central1-b","ru-central1-d"]'::jsonb),
  ('network-ssd-io-m3',          'Ultra high-performance SSD (io-m3)',   '["ru-central1-a","ru-central1-b","ru-central1-d"]'::jsonb);
