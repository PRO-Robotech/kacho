-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- REG-1 F7 — Repository lifecycle. Авторитетный output-only enum {DURABLE,EPHEMERAL}
-- заменил протекающий implicit durable-bool (класс выводился неявно из наличия overlay-
-- строки). Теперь durable-overlay несёт явный lifecycle; ephemeral-проекция без overlay
-- → EPHEMERAL (derive в use-case). Within-service инвариант домена — DB CHECK (ban #10).
--
-- DEFAULT 'DURABLE' корректен: явный CreateRepository → DURABLE by default (explicit
-- intent = сохранить каркас); существующие durable overlay-строки (survives-empty) —
-- durable by construction, backfill в DEFAULT безопасен. Overlay-set (Update/Rename)
-- промоутит EPHEMERAL→DURABLE на уровне DML (repository_config.go).
SET search_path TO kacho_registry, public;

ALTER TABLE repository_configs ADD COLUMN lifecycle TEXT NOT NULL DEFAULT 'DURABLE';
ALTER TABLE repository_configs
  ADD CONSTRAINT repository_configs_lifecycle_check
    CHECK (lifecycle IN ('DURABLE', 'EPHEMERAL'));

-- +goose Down
SET search_path TO kacho_registry, public;
ALTER TABLE repository_configs DROP CONSTRAINT IF EXISTS repository_configs_lifecycle_check;
ALTER TABLE repository_configs DROP COLUMN IF EXISTS lifecycle;
