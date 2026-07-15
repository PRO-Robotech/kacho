-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- +goose StatementBegin

-- operations — Long-Running Operations (LRO) каталога kacho-storage. Мутации
-- Volume/Snapshot (Create/Update/Delete) асинхронны: возвращают строку operations
-- (done=false), corelib-worker выполняет доменную запись и финализирует строку
-- (done=true, response=Volume/Snapshot либо error=google.rpc.Status). Клиент
-- поллит OperationService.Get(id) до done. op-id prefix — "sop".
--
-- Набор колонок совпадает с тем, что читает/пишет corelib operations.Repo
-- (id, description, created_at, created_by, modified_at, done, metadata_*,
-- resource_id, account_id, error_*, response_*, principal_*). Консолидированный
-- greenfield-baseline эквивалент corelib common-миграций 0001-0004
-- (operations + principal + account_id + orphan_scan_idx); руками не редактируем
-- (ban #5) — источник истины набора колонок kacho-corelib/migrations/common.
-- account_id остается NULL, пока storage не пишет metadata с account_id; колонка
-- нужна, т.к. corelib CreateWithPrincipal INSERT-ит account_id безусловно.

SET search_path TO kacho_storage, public;

CREATE TABLE kacho_storage.operations (
    id            text         PRIMARY KEY,   -- "sop<crockford>" для opsproxy-роутинга
    description   text         NOT NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    created_by    text         NOT NULL DEFAULT 'anonymous',
    modified_at   timestamptz  NOT NULL DEFAULT now(),
    done          boolean      NOT NULL DEFAULT false,
    metadata_type text,                        -- type_url из Any
    metadata_data bytea,                       -- value из Any
    resource_id   text,                        -- денорм для filter в List
    account_id    text,                        -- денорм (storage: NULL), corelib INSERT-ит безусловно
    error_code    integer,
    error_message text,
    error_details bytea,                       -- google.rpc.Status.details (Any[])
    response_type text,
    response_data bytea,
    principal_type         text NOT NULL DEFAULT 'system',
    principal_id           text NOT NULL DEFAULT 'bootstrap',
    principal_display_name text NOT NULL DEFAULT 'System'
);

CREATE INDEX operations_resource_idx   ON kacho_storage.operations (resource_id);
CREATE INDEX operations_done_idx       ON kacho_storage.operations (done);
CREATE INDEX operations_created_at_idx ON kacho_storage.operations (created_at);
-- partial cursor-индекс account-scoped List (storage не пишет account_id → не растет).
CREATE INDEX operations_account_id_idx
    ON kacho_storage.operations (account_id, created_at, id)
    WHERE account_id IS NOT NULL;
-- partial-индекс под orphan-scan startup-reconciler'а (durable LRO recovery):
-- WHERE done=false AND modified_at < $1 ORDER BY modified_at FOR UPDATE SKIP LOCKED.
CREATE INDEX operations_orphan_scan_idx
    ON kacho_storage.operations (modified_at)
    WHERE NOT done;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO kacho_storage, public;
DROP TABLE IF EXISTS kacho_storage.operations;
-- +goose StatementEnd
