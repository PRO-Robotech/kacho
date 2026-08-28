// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations — embedded SQL миграции kacho-nlb (схема `kacho_nlb`).
//
// Baseline: `0001_initial.sql` — squashed schema, helper-функции
// (`kacho_labels_valid`, `nlb_outbox_notify`, `lb_status_recompute`), tables
// (operations, load_balancers, listeners, target_groups, targets, nlb_outbox),
// sequences, FK/CHECK/UNIQUE/partial-UNIQUE-NULLS-NOT-DISTINCT + triggers.
// (Baseline also created the attached_target_groups M:N pivot; `0022` drops it —
// a listener wires directly to one target group, and `0023` widens that FK to
// (target_group, project) so the wiring can never cross a project boundary. And
// it created nlb_watch_cursors, a per-subscriber stream cursor that never had a
// reader; `20260828114800` drops it — the position of a subscription belongs to
// the client, so the server holds none.)
//
// FS потребляется `cmd/migrator/main.go` (goose up/down/status) и
// `cmd/kacho-loadbalancer/main.go` (на serve startup — health-check / version).
package migrations

import "embed"

// FS — embedded набор миграций (`*.sql`).
//
//go:embed *.sql
var FS embed.FS
