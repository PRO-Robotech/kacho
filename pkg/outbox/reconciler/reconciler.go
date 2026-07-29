// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package reconciler is the backstop layer over the outbox/drainer. The drainer
// delivers intents at-least-once; the reconciler repairs the cases the drainer
// alone cannot:
//
//   - RedrivePoisoned — re-drives poisoned/exhausted rows (sent_at IS NULL AND
//     attempt_count >= MaxAttempts) back to claimable so the drainer retries
//     them (e.g. after the permanent cause was fixed, or it was misclassified).
//     Never past a delivered successor of the same resource: replaying an intent
//     over a later one that already landed undoes it, which on a register-outbox
//     resurrects access that was revoked. See the method doc.
//   - BackfillFromState — derive-from-state safety-net: a per-service
//     ResourceEnumerator lists resource rows that lack an applied owner-tuple
//     intent; the reconciler re-emits a project-hierarchy register-intent
//     through the SAME transactional register-outbox table (the CAS-claim path
//     governs delivery — never a direct FGA/IAM call). It synthesises ONLY the
//     project-hierarchy tuple (derivable from the stored project_id); the
//     owner-self-grant subject is NOT reconstructible from resource state
//     so it is never guessed.
//   - GCOrphans — inverse-orphan GC with the anti-race invariant: emit
//     fga.unregister for a tuple whose resource is gone ONLY when (a) the
//     resource is not "intended-registered" (no register-intent that is the most
//     recent intent for the id) AND (b) the resource has been absent for a grace
//     window. A concurrent re-Create that co-commits a register-intent therefore
//     always wins — its durable intent blocks the unregister, so a legitimate
//     re-create never suffers self-inflicted owner access-loss.
//
// The domain knowledge — how to enumerate resource rows / project_id / check
// existence / list registered tuples — is a per-service adapter
// (ResourceEnumerator + TupleRegistry), NOT corelib logic. corelib orchestrates
// the passes and the emit; the service injects the domain enumeration.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox"
)

// Event-type literals on the register-outbox table.
const (
	eventRegister   = "fga.register"
	eventUnregister = "fga.unregister"
)

// supersededSampleLimit bounds how many resource ids RedrivePoisoned names in its
// WARN. Attribution needs examples, not the whole set; an unbounded list would
// turn one stuck resource into an unreadable log line.
const supersededSampleLimit = 20

// ResourceRow is one live resource as seen by the per-service enumerator. Kind +
// ID identify the resource; ProjectID is the hierarchy parent ("" when there is
// no derivable project hierarchy — e.g. an account — in which case the
// reconciler will NOT backfill).
type ResourceRow struct {
	Kind      string
	ID        string
	ProjectID string
}

// RegisteredTuple is one currently-registered owner-tuple (Kind + resource ID)
// as seen by the per-service TupleRegistry — the candidate set for orphan GC.
type RegisteredTuple struct {
	Kind string
	ID   string
}

// ResourceEnumerator is the per-service adapter that knows the domain resource
// tables. It is injected by the service (NOT implemented in corelib).
type ResourceEnumerator interface {
	// ListResources enumerates the live resource rows (id + project hierarchy).
	ListResources(ctx context.Context) ([]ResourceRow, error)
	// ResourceExists reports whether (kind,id) still exists — used by GC to
	// confirm a registered tuple's resource is truly gone.
	ResourceExists(ctx context.Context, kind, id string) (bool, error)
}

// TupleRegistry is the per-service adapter that lists currently-registered
// owner-tuples (the orphan-GC candidate set). It is injected by the service.
type TupleRegistry interface {
	ListRegistered(ctx context.Context) ([]RegisteredTuple, error)
}

// Adapters bundles the per-service domain adapters.
type Adapters struct {
	Enumerator ResourceEnumerator
	Registry   TupleRegistry
}

// Config parameterises a Reconciler.
type Config struct {
	// Table — full register-outbox table name (`<schema>.<table>`). Must be the
	// SAME table the drainer drains (intents are re-emitted here so the CAS-claim
	// path governs delivery).
	Table string
	// Channel — LISTEN/NOTIFY channel of the table (for parity / future use).
	Channel string
	// MaxAttempts — poison threshold (default 10) used by RedrivePoisoned.
	MaxAttempts int
	// GraceWindow — a resource must be continuously absent (and not
	// intended-registered) for at least this long before GCOrphans emits an
	// unregister (anti-race deferral). 0 → emit on the first confirmed
	// pass (no deferral). Production sets a window (e.g. a minute) so any
	// in-flight re-Create lands its durable register-intent first.
	GraceWindow time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 10
	}
	return c
}

// Reconciler orchestrates the backstop passes over one register-outbox table.
type Reconciler struct {
	pool *pgxpool.Pool
	cfg  Config
	ad   Adapters
	log  *slog.Logger

	// firstSeenAbsent tracks when each orphan candidate was first observed absent
	// (the GraceWindow tombstone). Reset when the resource reappears or becomes
	// intended-registered, and pruned at the end of each GCOrphans pass to the
	// current registered-tuple set (so an id that leaves ListRegistered by any
	// other path cannot leak an entry for process lifetime). Guarded by mu
	// (GCOrphans may run concurrently).
	mu              sync.Mutex
	firstSeenAbsent map[string]time.Time
}

// New constructs a Reconciler. pool + Table + both adapters are required.
func New(pool *pgxpool.Pool, cfg Config, ad Adapters, logger *slog.Logger) (*Reconciler, error) {
	if pool == nil {
		return nil, errors.New("reconciler.New: pool is nil")
	}
	if cfg.Table == "" {
		return nil, errors.New("reconciler.New: Config.Table required")
	}
	if ad.Enumerator == nil {
		return nil, errors.New("reconciler.New: Adapters.Enumerator required")
	}
	if ad.Registry == nil {
		return nil, errors.New("reconciler.New: Adapters.Registry required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		pool:            pool,
		cfg:             cfg.withDefaults(),
		ad:              ad,
		log:             logger.With(slog.String("component", "outbox_reconciler"), slog.String("table", cfg.Table)),
		firstSeenAbsent: map[string]time.Time{},
	}, nil
}

// NewRedriveOnly constructs a Reconciler that can run ONLY RedrivePoisoned.
//
// RedrivePoisoned is pure SQL over the outbox table — it consults neither the
// resource enumerator nor the tuple registry — so a service that wants just the
// poison backstop should not have to invent two domain adapters it will never call.
// Requiring them anyway pushed services into one of two bad shapes: stub adapters
// that exist only to satisfy a constructor (dead code that reads as if a reconcile
// loop were running), or no backstop at all.
//
// The backstop is not optional in any service whose register-outbox can poison.
// A poisoned row is excluded from the claim query's blocking set, so its partition
// unblocks immediately — that is what stops one refused intent from silencing every
// later intent for the same resource. But the row itself then stays undelivered
// forever unless something re-drives it, and an undelivered registration means the
// resource has no mirror row in kacho-iam, hence no owner tuple and no materialized
// verbs: invisible to authz until someone edits the database by hand. The periodic
// redrive is what makes poisoning a bounded pause rather than a permanent loss.
//
// BackfillFromState and GCOrphans return an error on a redrive-only Reconciler:
// the narrowing is enforced where it matters, not left to a nil dereference.
func NewRedriveOnly(pool *pgxpool.Pool, cfg Config, logger *slog.Logger) (*Reconciler, error) {
	if pool == nil {
		return nil, errors.New("reconciler.NewRedriveOnly: pool is nil")
	}
	if cfg.Table == "" {
		return nil, errors.New("reconciler.NewRedriveOnly: Config.Table required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		pool:            pool,
		cfg:             cfg.withDefaults(),
		log:             logger.With(slog.String("component", "outbox_reconciler"), slog.String("table", cfg.Table)),
		firstSeenAbsent: map[string]time.Time{},
	}, nil
}

// RedrivePoisoned resets poisoned/exhausted rows (sent_at IS NULL AND
// attempt_count >= MaxAttempts) back to claimable (attempt_count = 0, last_error
// = NULL) so the drainer retries them. Returns the number re-driven.
//
// # Reviving respects the order of the partition
//
// An intent is NOT revived once a LATER intent for the SAME resource has already
// been delivered. Reviving past a delivered successor replays an outdated intent
// on top of the current state — and for a register-outbox that is precisely the
// over-grant the partition ordering exists to prevent: the target (kacho-iam's
// resource_mirror) versions only its update branch, deregistration is a hard
// delete that keeps no tombstone, so a replayed registration finds nothing to
// compare against, takes the insert branch and resurrects the mirror row of a
// deleted resource. iam's reconciler is level-triggered off that mirror, so the
// owner tuple is then re-materialised forever — revoked access returns and stays.
//
// The drainer's claim already refuses to take a row ahead of a DELIVERABLE
// same-partition predecessor (drainer Config.PartitionColumn). That guard says
// nothing about rows already delivered, because a delivered row leaves the
// deliverable set — so the backstop has to carry the other half of the same rule
// itself, or it re-opens through repair exactly what the claim closed.
//
// The partition is `resource_id`, the same key every production register-outbox
// gives the drainer and the same one this package already keys lockResource and
// intendedRegistered on: every emitter writes one row per FGA object and stamps
// resource_id with that object's globally-unique id, so "same partition" is
// exactly "same target object". There is no knob for it — a knob left unset here
// would read as "no ordering" and silently restore the defect. The table MUST
// therefore carry a resource_id column; the one outbox in the platform that does
// not (iam's fga_outbox, partitioned on tuple_key) is not a register-outbox and
// has no reconciler.
//
// # Two honest limits of this rule
//
// A row with an EMPTY resource_id names no resource, so "a later intent for the
// same resource" cannot be established for it. Such rows group together — the
// drainer's claim already groups them the same way and the two must agree — and
// the conservative reading is deliberate: reviving one risks replaying it past a
// delivered intent, which is the defect this guard exists for. The cost is real
// and is NOT merely a delay: one delivered empty-key row parks every earlier
// poisoned empty-key row permanently. No live emitter writes an empty
// resource_id, so this is reachable only for rows predating the column
// (vpc migration 0008 added it without a backfill) — which is why the report
// below names the rows rather than counting them.
//
// The check is evaluated against ONE statement snapshot, so it is "no delivered
// successor as of this pass", not a serialised guarantee: a successor the drainer
// commits WHILE the statement runs is invisible to it. This narrows the original
// window from "every pass, forever" to "a delivery landing inside one statement",
// and the pass is periodic (minutes) while the statement is milliseconds. Closing
// it fully would mean taking lockResource per row and giving up the set-based
// pass; that trade is not obviously worth it and is written down here rather than
// papered over.
//
// A superseded row stays poisoned: its intent is void, and it keeps its original
// last_error for diagnosis. Note what that means operationally — the row remains
// in the pending set, so it keeps contributing to the table's backlog and
// oldest-pending-age gauges, which will not fall back to zero on their own. That
// is why the report below is a WARN carrying the resource ids and not a bare
// count: the condition needs a human to retire the rows, and an alarm nobody can
// clear stops being an alarm.
//
// Cost: the supersession check is a correlated anti-join over the table, which is
// unindexed for delivered rows (the partition-head index is partial on
// sent_at IS NULL). It runs only for rows in the outer set, i.e. only while
// something is poisoned — an alarm condition in itself — and not at all on a
// healthy queue. A service whose outbox both grows large and poisons routinely
// should add a plain btree on (resource_id, id) to turn it into a lookup.
func (r *Reconciler) RedrivePoisoned(ctx context.Context) (int, error) {
	table := outbox.SanitizeTable(r.cfg.Table)
	q := fmt.Sprintf(`
		WITH poisoned AS (
		    SELECT t.id, t.resource_id,
		           EXISTS (
		               SELECT 1 FROM %[1]s s
		                WHERE s.resource_id = t.resource_id
		                  AND s.id > t.id
		                  AND s.sent_at IS NOT NULL
		           ) AS superseded
		      FROM %[1]s t
		     WHERE t.sent_at IS NULL AND t.attempt_count >= $1
		),
		revived AS (
		    UPDATE %[1]s
		       SET attempt_count = 0, last_error = NULL
		     WHERE id IN (SELECT id FROM poisoned WHERE NOT superseded)
		    RETURNING 1
		)
		SELECT (SELECT count(*) FROM revived),
		       (SELECT count(*) FROM poisoned WHERE superseded),
		       (SELECT coalesce(array_agg(resource_id ORDER BY id), '{}')
		          FROM (SELECT resource_id, id FROM poisoned
		                 WHERE superseded ORDER BY id LIMIT %[2]d) sample)`,
		table, supersededSampleLimit)

	var revived, superseded int64
	var sample []string
	if err := r.pool.QueryRow(ctx, q, r.cfg.MaxAttempts).Scan(&revived, &superseded, &sample); err != nil {
		return 0, fmt.Errorf("reconciler.RedrivePoisoned %s: %w", r.cfg.Table, err)
	}
	if superseded > 0 {
		// Named, not counted: these rows stay in the pending set and keep the
		// table's backlog/age gauges off zero, so an operator has to retire them
		// by hand and needs to know WHICH. Resource ids are id-based handles, not
		// PII or infrastructure detail — the same call the drainer's wedge
		// reporter makes.
		r.log.Warn("poisoned intents left un-revived: a later intent for the same "+
			"resource has already been delivered, so replaying them would undo it; "+
			"they remain pending and must be retired by hand",
			slog.Int64("superseded", superseded),
			slog.Int("sampled", len(sample)),
			slog.String("resources", strings.Join(sample, ",")))
	}
	return int(revived), nil
}

// BackfillFromState synthesises a project-hierarchy register-intent for each live
// resource that is NOT currently intended-registered (no register-intent that is
// the latest intent for the id) AND has a derivable project_id. It emits through
// the same register-outbox table (CAS-claim path delivers). Returns the count
// emitted. Resources without a project_id are skipped (owner-self-grant is not
// backfillable).
func (r *Reconciler) BackfillFromState(ctx context.Context) (int, error) {
	if r.ad.Enumerator == nil {
		return 0, errors.New("reconciler.BackfillFromState: no resource enumerator " +
			"(this Reconciler was built by NewRedriveOnly)")
	}
	rows, err := r.ad.Enumerator.ListResources(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconciler.BackfillFromState list: %w", err)
	}
	emitted := 0
	for _, res := range rows {
		if res.ProjectID == "" {
			// No derivable project-hierarchy → not backfillable.
			continue
		}
		n, err := r.backfillOne(ctx, res)
		if err != nil {
			return emitted, err
		}
		emitted += n
	}
	return emitted, nil
}

// backfillOne emits a register-intent for one resource iff it is not already
// intended-registered. The check + insert run in one advisory-locked tx so a
// concurrent path serialises on the resource id (no duplicate backfill).
func (r *Reconciler) backfillOne(ctx context.Context, res ResourceRow) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("reconciler.backfillOne begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockResource(ctx, tx, res.ID); err != nil {
		return 0, err
	}
	registered, err := intendedRegistered(ctx, tx, r.cfg.Table, res.ID)
	if err != nil {
		return 0, err
	}
	if registered {
		return 0, nil // already has a live register-intent → no backfill
	}

	payload := fmt.Sprintf(`{"project_id":%q}`, res.ProjectID)
	if err := insertIntent(ctx, tx, r.cfg.Table, eventRegister, res.Kind, res.ID, payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("reconciler.backfillOne commit: %w", err)
	}
	return 1, nil
}

// GCOrphans emits fga.unregister for each registered tuple whose resource is gone
// and whose absence has been confirmed for >= GraceWindow with NO live
// register-intent (anti-race). Returns the count of unregister-intents
// emitted. Safe to call concurrently.
func (r *Reconciler) GCOrphans(ctx context.Context) (int, error) {
	if r.ad.Registry == nil {
		return 0, errors.New("reconciler.GCOrphans: no tuple registry " +
			"(this Reconciler was built by NewRedriveOnly)")
	}
	tuples, err := r.ad.Registry.ListRegistered(ctx)
	if err != nil {
		return 0, fmt.Errorf("reconciler.GCOrphans list: %w", err)
	}

	emitted := 0
	candidates := make(map[string]struct{}, len(tuples))
	for _, tup := range tuples {
		candidates[tup.ID] = struct{}{}
		exists, err := r.ad.Enumerator.ResourceExists(ctx, tup.Kind, tup.ID)
		if err != nil {
			// Bound the tombstone map even on an early error return: prune what we
			// have observed as the candidate set so a partial pass cannot leak.
			r.pruneTombstones(candidates)
			return emitted, fmt.Errorf("reconciler.GCOrphans exists %s: %w", tup.ID, err)
		}
		if exists {
			r.clearTombstone(tup.ID)
			continue
		}
		ok, err := r.gcOne(ctx, tup)
		if err != nil {
			r.pruneTombstones(candidates)
			return emitted, err
		}
		if ok {
			emitted++
		}
	}
	// Bound firstSeenAbsent to the current candidate set: any tombstone whose id
	// left ListRegistered by a path other than corelib GC (e.g. an out-of-band
	// unregister) is dropped here, so the map can never leak entries for lifetime
	// of the process (CWE-401). Grace is defense-in-depth only — correctness of the
	// anti-race is DB-enforced (pg_advisory_xact_lock + intendedRegistered), so a
	// pruned/reset grace clock (also on restart) never causes an incorrect GC.
	r.pruneTombstones(candidates)
	return emitted, nil
}

// pruneTombstones drops every firstSeenAbsent entry whose id is not in the given
// candidate set (the ids observed in this GCOrphans pass), bounding the map to the
// current registered-tuple set.
func (r *Reconciler) pruneTombstones(candidates map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.firstSeenAbsent {
		if _, ok := candidates[id]; !ok {
			delete(r.firstSeenAbsent, id)
		}
	}
}

// gcOne attempts to unregister one orphan candidate under the anti-race
// invariant. Returns true if an unregister-intent was emitted.
func (r *Reconciler) gcOne(ctx context.Context, tup RegisteredTuple) (bool, error) {
	// GraceWindow deferral: first sighting only records the tombstone (a
	// concurrent re-Create gets a full window to land its durable register-intent
	// which then blocks GC). Eligible once absent for >= GraceWindow.
	if !r.graceElapsed(tup.ID) {
		return false, nil
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("reconciler.gcOne begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise register-vs-unregister emits on the resource id.
	if err := lockResource(ctx, tx, tup.ID); err != nil {
		return false, err
	}

	// Anti-race condition (a): a live register-intent (the latest intent is a
	// register) means the resource is intended-registered → a concurrent
	// re-Create won; NEVER unregister.
	registered, err := intendedRegistered(ctx, tx, r.cfg.Table, tup.ID)
	if err != nil {
		return false, err
	}
	if registered {
		r.clearTombstone(tup.ID)
		return false, nil
	}

	if err := insertIntent(ctx, tx, r.cfg.Table, eventUnregister, tup.Kind, tup.ID, "{}"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("reconciler.gcOne commit: %w", err)
	}
	r.clearTombstone(tup.ID)
	return true, nil
}

// graceElapsed reports whether the candidate has been continuously absent for at
// least GraceWindow. The first call records the tombstone and (for GraceWindow>0)
// returns false; GraceWindow==0 → eligible on the first confirmed pass.
func (r *Reconciler) graceElapsed(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	first, seen := r.firstSeenAbsent[id]
	now := time.Now()
	if !seen {
		r.firstSeenAbsent[id] = now
		return r.cfg.GraceWindow == 0
	}
	return now.Sub(first) >= r.cfg.GraceWindow
}

func (r *Reconciler) clearTombstone(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.firstSeenAbsent, id)
}

// EmitRegister co-commits an fga.register intent in the caller's writer-tx (the
// same tx that inserts the resource — atomic). It takes the per-resource
// advisory lock so it serialises against GCOrphans' unregister-emit on the same
// id: this is the anti-race contract — a re-Create that co-commits its
// register-intent always wins over a concurrent GC. Services call this from
// their resource Create writer-tx or the reconciler uses it for backfill.
//
// table/kind are trusted literals; payload is a JSON object string (owner-tuple
// data incl. project_id).
func EmitRegister(ctx context.Context, tx pgx.Tx, table, kind, id, payload string) error {
	if err := lockResource(ctx, tx, id); err != nil {
		return err
	}
	return insertIntent(ctx, tx, table, eventRegister, kind, id, payload)
}

// EmitUnregister co-commits an fga.unregister intent in the caller's writer-tx
// (the same tx that deletes the resource). Like EmitRegister it takes the
// per-resource advisory lock so register/unregister emits for the same id
// serialise.
func EmitUnregister(ctx context.Context, tx pgx.Tx, table, kind, id, payload string) error {
	if err := lockResource(ctx, tx, id); err != nil {
		return err
	}
	return insertIntent(ctx, tx, table, eventUnregister, kind, id, payload)
}

// lockResource takes a transaction-scoped advisory lock keyed by the resource id
// so register-emit and unregister-emit for the same id serialise.
func lockResource(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, id); err != nil {
		return fmt.Errorf("reconciler: advisory lock %s: %w", id, err)
	}
	return nil
}

// intendedRegistered reports whether the resource id is currently intended to be
// registered: there exists a register-intent that is more recent than any
// unregister-intent for the id (covers both pending and already-sent rows — a
// re-Create's durable register-intent blocks GC even after delivery).
func intendedRegistered(ctx context.Context, tx pgx.Tx, table, id string) (bool, error) {
	q := fmt.Sprintf(`
		SELECT COALESCE(
		    (SELECT event_type FROM %s
		      WHERE resource_id = $1 AND event_type IN ($2, $3)
		      ORDER BY id DESC LIMIT 1),
		    '') = $2
	`, outbox.SanitizeTable(table))
	var registered bool
	if err := tx.QueryRow(ctx, q, id, eventRegister, eventUnregister).Scan(&registered); err != nil {
		return false, fmt.Errorf("reconciler: intended-registered %s: %w", id, err)
	}
	return registered, nil
}

// insertIntent inserts one intent row into the register-outbox table (the NOTIFY
// trigger wakes the drainer). table/eventType are trusted literals.
func insertIntent(ctx context.Context, tx pgx.Tx, table, eventType, kind, id, payload string) error {
	q := fmt.Sprintf(
		`INSERT INTO %s (event_type, resource_kind, resource_id, payload)
		 VALUES ($1, $2, $3, $4::jsonb)`,
		outbox.SanitizeTable(table),
	)
	if _, err := tx.Exec(ctx, q, eventType, kind, id, payload); err != nil {
		return fmt.Errorf("reconciler: insert %s intent %s: %w", eventType, id, err)
	}
	return nil
}
