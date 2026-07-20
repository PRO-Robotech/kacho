-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- REG-1 F6 — Repository natural-key rename `registry_id` → `namespace_id`.
--
-- Крупный breaking rename `Registry` → `Namespace` (слово «registry» зарезервировано
-- за serving-host'ом). Ресурсная таблица `registries` НЕ переименовывается (внутренняя,
-- не tenant-facing; FGA-тип тоже заморожен `registry_registry`) — переименовывается
-- лишь натуральный ключ overlay-таблицы `repository_configs`, где `registry_id` был
-- ссылкой на ресурс. Data-plane таблицы (`registry_push_grant`/`registry_pending_blob`)
-- трактуют id как plain string и НЕ затрагиваются (меньший blast).
--
-- RENAME COLUMN в Postgres автоматически перепривязывает PRIMARY KEY, FK и индексы,
-- ссылающиеся на колонку (определения обновляются in-place). Имя FK-констрейнта
-- переименовываем явно под новую колонку (auto-name был `*_registry_id_fkey`).
SET search_path TO kacho_registry, public;

ALTER TABLE repository_configs RENAME COLUMN registry_id TO namespace_id;
ALTER TABLE repository_configs
  RENAME CONSTRAINT repository_configs_registry_id_fkey TO repository_configs_namespace_id_fkey;

-- +goose Down
SET search_path TO kacho_registry, public;
ALTER TABLE repository_configs
  RENAME CONSTRAINT repository_configs_namespace_id_fkey TO repository_configs_registry_id_fkey;
ALTER TABLE repository_configs RENAME COLUMN namespace_id TO registry_id;
