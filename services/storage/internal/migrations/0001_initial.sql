-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Базовая схема kacho-storage. Схема kacho_storage (database-per-service). Плоские
-- ресурсы (без K8s-envelope). Домен Storage: Volume / VolumeAttachment / Snapshot /
-- DiskType.
--
-- СКЕЛЕТ (service-scaffolder): здесь создаётся ТОЛЬКО схема. Доменные таблицы
-- (volumes, volume_attachments, snapshots, disk_types с FK/UNIQUE/CHECK/EXCLUDE/CAS)
-- и общая LRO-таблица operations добавляет rpc-implementer отдельными миграциями
-- при реализации первого RPC (строгий TDD, db-architect-reviewer). НЕ добавлять
-- доменные таблицы в этот placeholder.

CREATE SCHEMA IF NOT EXISTS kacho_storage;
SET search_path TO kacho_storage, public;

-- +goose Down
DROP SCHEMA IF EXISTS kacho_storage CASCADE;
