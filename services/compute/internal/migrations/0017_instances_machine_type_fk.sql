-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Within-service referential integrity for instances.machine_type_id →
-- machine_types(id) (data-integrity §within-service п.1: "ссылка на ресурс в той
-- же БД → FK REFERENCES … ON DELETE {RESTRICT|…}; никогда software-only").
--
-- 0016 added machine_type_id as a plain TEXT column; existence was only checked
-- softly on the Instance.Create path (resolveMachineType). The release path had
-- no guard at all: InternalMachineTypeService.Delete issued a bare DELETE, so a
-- catalog row referenced by live instances could vanish — leaving instances that
-- echo machineTypeId='mt-…' while GET machineTypes/mt-… answers NOT_FOUND, and
-- with no way to restore the row under the same (server-assigned) id. A
-- delete-side software guard would still leave the Create side racy (INSERT of an
-- instance and DELETE of the catalog row do not conflict under READ COMMITTED);
-- the FK closes both directions because the INSERT takes a row-share lock on the
-- parent. Decommissioning a size stays a status transition (status=RETIRED,
-- Bookable()=false), never a hard DELETE of an in-use row.
--
-- The column becomes NULLable because `NOT NULL DEFAULT ''` is not FK-able ('' is
-- not a machine_types id). NULL means "not set" and is not enforced by the FK;
-- the required-on-Create contract stays a sync service-level check. The Go type
-- remains `string` — the repo reads through COALESCE and writes through NULLIF.
ALTER TABLE instances ALTER COLUMN machine_type_id DROP DEFAULT;
ALTER TABLE instances ALTER COLUMN machine_type_id DROP NOT NULL;
UPDATE instances SET machine_type_id = NULL WHERE machine_type_id = '';

-- Referencing-side index: without it every machine_types DELETE degrades into a
-- seq-scan of instances for the RESTRICT check.
CREATE INDEX IF NOT EXISTS instances_machine_type_idx ON instances (machine_type_id);

ALTER TABLE instances
  ADD CONSTRAINT instances_machine_type_id_fkey
    FOREIGN KEY (machine_type_id) REFERENCES machine_types(id) ON DELETE RESTRICT;

-- +goose Down
ALTER TABLE instances DROP CONSTRAINT IF EXISTS instances_machine_type_id_fkey;
DROP INDEX IF EXISTS instances_machine_type_idx;
UPDATE instances SET machine_type_id = '' WHERE machine_type_id IS NULL;
ALTER TABLE instances ALTER COLUMN machine_type_id SET NOT NULL;
ALTER TABLE instances ALTER COLUMN machine_type_id SET DEFAULT '';
