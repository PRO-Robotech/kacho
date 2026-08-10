// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fgaRegisterEmitter — реализация kacho.FGARegisterEmitter. INSERT в
// `fga_register_outbox` в той же TX, что и DML ресурса; trigger
// `fga_register_outbox_notify_trg` шлёт `pg_notify('kacho_nlb_fga_register_outbox',
// id::text)` после commit'а, будя register-drainer.
type fgaRegisterEmitter struct {
	tx pgx.Tx
	// seq — счётчик уже записанных intent'ов ЭТОЙ writer-tx (принадлежит
	// writerImpl, эмиттер держит на него указатель — он создаётся заново на
	// каждый вызов FGARegisterOutbox()). Порядковый номер попадает в
	// source_version, см. Emit.
	seq *int64
}

// Emit добавляет register-intent строку в текущей TX writer'а. Пустой набор
// tuple → no-op (нечего регистрировать — не пишем пустую строку).
//
// payload — JSON-сериализованный набор tuple-намерений (project-hierarchy +
// creator + parent-link),: весь набор ресурса одной строкой.
// CHECK на event_type / jsonb_typeof заложены в миграции 0002 — typo в caller'е
// → SQLSTATE 23514 → ErrInvalidArg в mapPgErr.
func (e *fgaRegisterEmitter) Emit(ctx context.Context, eventType string, intent domain.FGARegisterIntent) (time.Time, error) {
	if len(intent.Tuples) == 0 {
		return time.Time{}, nil
	}
	payload, err := intent.Marshal()
	if err != nil {
		return time.Time{}, mapPgErr(err, "fga_register_outbox", "")
	}
	// Stamp the monotonic source_version into the payload AT INSERT TIME, inside
	// this writer-tx — the exact instant the source-state is recorded. jsonb_set
	// merges it into the encoded payload so the register-drainer forwards it to
	// RegisterResourceRequest.source_version (last-source-state-wins: a reordered
	// stale intent → no-op in IAM, not an overwrite).
	//
	// The stamp is `now() + <ordinal> µs`, and the ordinal is what makes it
	// correct in BOTH directions:
	//
	//   - ACROSS transactions — `now()` is transaction_timestamp, constant for the
	//     whole tx. Sequential mutations of one object serialise on its row-lock,
	//     so a later writer-tx begins after the earlier committed and its now() is
	//     strictly greater; the ordinal offset is microseconds against a gap of at
	//     least a commit round-trip, so it cannot invert that.
	//
	//   - WITHIN one transaction — now() alone is IDENTICAL for every statement,
	//     so two intents about the SAME object emitted in one tx would be
	//     indistinguishable to IAM's last-source-state-wins comparison, and
	//     whichever applied last would decide. That is not hypothetical: a
	//     cross-project Move emits both an unregister of the old scope and a
	//     register of the new one, and IAM's unregister is a hard DELETE gated
	//     `source_version <= tombstone` — equal versions make it match and wipe the
	//     projection the register had just written. The ordinal advances on every
	//     Emit of this writer-tx, so intent N+1 is strictly newer than intent N.
	//
	// The discipline this establishes for callers: emit in semantic order,
	// NEWEST STATE LAST. The drainer's per-resource FIFO (PartitionColumn =
	// resource_id) then applies them in that same order, and the version ordering
	// keeps the outcome correct even if they were ever applied out of order.
	ordinal := *e.seq
	const q = `INSERT INTO kacho_nlb.fga_register_outbox
        (event_type, payload, resource_kind, resource_id)
        VALUES ($1, jsonb_set($2::jsonb, '{source_version}',
                to_jsonb(now() + ($5::bigint * interval '1 microsecond'))), $3, $4)
        RETURNING (payload->>'source_version')::timestamptz`
	var stamped time.Time
	if err := e.tx.QueryRow(ctx, q, eventType, payload, intent.Kind, intent.ResourceID, ordinal).Scan(&stamped); err != nil {
		return time.Time{}, mapPgErr(err, "fga_register_outbox", "")
	}
	*e.seq = ordinal + 1
	return stamped, nil
}
