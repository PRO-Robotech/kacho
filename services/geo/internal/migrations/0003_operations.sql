-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- operations — Long-Running Operations (LRO) каталога kacho-geo. Admin-мутации
-- Region/Zone (Internal{Region,Zone}Service.Create/Update/Delete) асинхронны:
-- мутация возвращает строку operations (done=false), corelib-worker выполняет
-- доменную запись и финализирует строку (done=true, response=Region/Zone либо
-- error=google.rpc.Status). Клиент поллит OperationService.Get(id) до done.
--
-- Структура и набор колонок совпадают с тем, что читает/пишет corelib
-- operations.Repo (pgRepo): id, description, created_at, created_by, modified_at,
-- done, metadata_*, resource_id, account_id, error_*, response_*, principal_*.
-- account_id остается NULL (geo metadata не несет account_id — каталог
-- cluster-scoped, не привязан к Account); колонка нужна, т.к. corelib
-- CreateWithPrincipal INSERT-ит account_id безусловно.

SET search_path TO kacho_geo, public;

CREATE TABLE operations (
  id            TEXT         PRIMARY KEY,  -- "<geo-prefix><crockford>" для opsproxy-роутинга
  description   TEXT         NOT NULL,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  created_by    TEXT         NOT NULL DEFAULT 'anonymous',
  modified_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
  done          BOOLEAN      NOT NULL DEFAULT false,
  metadata_type TEXT,                       -- type_url из Any
  metadata_data BYTEA,                      -- value из Any
  resource_id   TEXT,                       -- денорм для filter в List
  account_id    TEXT,                       -- денорм (geo: всегда NULL), corelib INSERT-ит безусловно
  error_code    INTEGER,
  error_message TEXT,
  error_details BYTEA,                       -- google.rpc.Status.details (Any[])
  response_type TEXT,
  response_data BYTEA,
  principal_type         TEXT NOT NULL DEFAULT 'system',
  principal_id           TEXT NOT NULL DEFAULT 'bootstrap',
  principal_display_name TEXT NOT NULL DEFAULT 'System'
);

CREATE INDEX operations_resource_idx   ON operations (resource_id);
CREATE INDEX operations_done_idx       ON operations (done);
CREATE INDEX operations_created_at_idx ON operations (created_at);
-- partial cursor-индекс account-scoped List (geo не пишет account_id → не растет).
CREATE INDEX operations_account_id_idx
  ON operations (account_id, created_at, id)
  WHERE account_id IS NOT NULL;

-- +goose Down
SET search_path TO kacho_geo, public;
DROP INDEX IF EXISTS operations_account_id_idx;
DROP TABLE IF EXISTS operations;
