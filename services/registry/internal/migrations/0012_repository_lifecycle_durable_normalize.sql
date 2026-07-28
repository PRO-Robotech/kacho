-- Copyright (c) PRO-Robotech
-- SPDX-License-Identifier: BUSL-1.1

-- +goose Up
-- Normalise overlay rows that claim EPHEMERAL to what they actually are: DURABLE.
--
-- 0007 introduced the column so that a durable overlay could state its own class,
-- and CreateRepository accepted EPHEMERAL as an input. Nothing ever read the stored
-- value: an overlay row is durable BY CONSTRUCTION, because visibility is decided by
-- the PRESENCE of the row (mergeRepository), not by this column. A row marked
-- EPHEMERAL therefore survived an empty repository, appeared in ListRepositories and
-- answered GetRepository exactly like a DURABLE one — only the echoed enum differed.
-- The input is now refused (validateRepoLifecycle), so no new such row can appear;
-- this statement retires the ones already stored, which are a persisted claim the
-- service never honoured. It is precisely what the first overlay-set on such a row
-- would have done anyway (auto-promote EPHEMERAL→DURABLE, 0007 / UpdateConfig) —
-- brought forward, so the value stops asserting a capability that does not exist.
--
-- Read paths are untouched: LifecycleFromString still parses 'EPHEMERAL', so a row
-- written by a pod of the previous version mid-rollout, or a replica that has not
-- taken this migration yet, keeps reading correctly. The CHECK is deliberately NOT
-- narrowed for the same reason — narrowing it would turn such a write into a
-- constraint violation during a rolling upgrade, and would strand the parse branch.
--
-- Idempotent (a repeat matches no rows) and a verified no-op where the table is
-- empty. EPHEMERAL as an OUTPUT value is unaffected: it is derived for repositories
-- that have NO overlay row at all, where disappearing-when-empty is real.
SET search_path TO kacho_registry, public;

UPDATE repository_configs SET lifecycle = 'DURABLE' WHERE lifecycle <> 'DURABLE';

-- +goose Down
-- Irreversible by nature: the pre-migration value said EPHEMERAL while the row
-- behaved DURABLE, and which rows carried the claim is not recorded anywhere. Down
-- restores the schema, not the discarded claim — there is nothing truthful to
-- restore it to.
SELECT 1;
