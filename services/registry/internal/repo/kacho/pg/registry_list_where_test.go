// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

// registry_list_where_test.go — #460. `pkg/filter` grammar has two operators, `=`
// and `CONTAINS`. This repo parsed the expression and then took only ast.Value,
// building `name = $N` by hand — so `name CONTAINS "prod"` was answered as
// `name = "prod"`: the console asked for every registry whose name contains
// "prod" and got back the one registry called exactly that, with a 200 and
// nothing to distinguish it from a real result.
//
// `name` is what the console searches on and substring is exactly what it needs,
// so the outcome here is IMPLEMENT, not refuse. The predicate is emitted by
// ast.ToSQL, which keeps the operator with the value instead of dropping it at the
// call site.
//
// These assertions are on the SQL, not on rows: the defect lived in the fragment,
// the repo holds a *pgxpool.Pool, and the only proof that existed before was an
// integration test — one that does not run under -short at all.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
)

// TestRegistryListWhere_460_ContainsBecomesLike — a substring request must produce
// a substring predicate, with the value wrapped in wildcards.
func TestRegistryListWhere_460_ContainsBecomesLike(t *testing.T) {
	conds, args, err := registryListWhere(registry.ListQuery{Filter: `name CONTAINS "prod"`})
	require.NoError(t, err)

	require.Len(t, conds, 1)
	assert.Equal(t, "name LIKE $1", conds[0],
		"CONTAINS must build a substring predicate, never equality")
	assert.Equal(t, []any{"%prod%"}, args)
}

// TestRegistryListWhere_460_EqualsStaysEquals — the PAIRED positive control.
// Without it the assertion above stays green on a repo that turned EVERY filter
// into LIKE, which would silently widen every exact lookup in the product.
func TestRegistryListWhere_460_EqualsStaysEquals(t *testing.T) {
	conds, args, err := registryListWhere(registry.ListQuery{Filter: `name="prod"`})
	require.NoError(t, err)

	require.Len(t, conds, 1)
	assert.Equal(t, "name = $1", conds[0], "= must stay exact equality")
	assert.Equal(t, []any{"prod"}, args)
}

// TestRegistryListWhere_460_PlaceholdersStayInStep — the filter predicate is not
// the first condition in a real request, and its placeholder number is derived, not
// constant. A fix that emitted `$1` unconditionally would pass both tests above and
// mis-bind every scoped list.
func TestRegistryListWhere_460_PlaceholdersStayInStep(t *testing.T) {
	for _, tc := range []struct {
		name      string
		expr      string
		wantCond  string
		wantValue any
	}{
		{"contains after project scope", `name CONTAINS "prod"`, "name LIKE $2", "%prod%"},
		{"equals after project scope", `name="prod"`, "name = $2", "prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conds, args, err := registryListWhere(registry.ListQuery{
				ProjectID: "prj-1",
				Filter:    tc.expr,
			})
			require.NoError(t, err)

			require.Len(t, conds, 2)
			assert.Equal(t, "project_id = $1", conds[0])
			assert.Equal(t, tc.wantCond, conds[1])
			assert.Equal(t, []any{"prj-1", tc.wantValue}, args)
			// List numbers the LIMIT placeholder as len(args)+1; if the filter block
			// ever consumed a number without appending its arg, the LIMIT would bind
			// to the filter value instead.
			assert.Len(t, args, len(conds), "one placeholder per argument")
		})
	}
}

// TestRegistryListWhere_PlaceholdersStayInStepAcrossAllThreeConditions — the
// cursor condition is the only one that consumes TWO placeholder numbers, and it
// is also the last, so a number it fails to account for is invisible until some
// later condition is added. The scoped+filtered+paged request is the one a console
// actually issues past the first page; here every number is pinned literally.
//
// Placeholder numbers are derived from len(args) rather than carried in a counter:
// the counter had to be advanced in every branch, its final advance was read by
// nobody, and a branch that forgot to advance it would have mis-bound every
// argument after itself with no failing assertion anywhere.
func TestRegistryListWhere_PlaceholdersStayInStepAcrossAllThreeConditions(t *testing.T) {
	cursorAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	conds, args, err := registryListWhere(registry.ListQuery{
		ProjectID: "prj-1",
		Filter:    `name CONTAINS "prod"`,
		PageToken: encodePageToken(cursorAt, "reg-9"),
	})
	require.NoError(t, err)

	require.Len(t, conds, 3)
	assert.Equal(t, "project_id = $1", conds[0])
	assert.Equal(t, "name LIKE $2", conds[1])
	assert.Equal(t, "(created_at, id) > ($3, $4)", conds[2])
	assert.Equal(t, []any{"prj-1", "%prod%", cursorAt, "reg-9"}, args)

	// List binds LIMIT as len(args)+1. Every placeholder above must therefore have
	// its own argument, or LIMIT collides with one of them.
	assert.Len(t, args, 4, "one argument per placeholder, LIMIT takes $5")
}

// TestRegistryListWhere_460_WildcardsInTheValueAreEscaped — `%` typed by the caller
// is a literal it is searching FOR, not a wildcard it is granting. Without escaping,
// a search for "50%" matches every registry and the caller reads that as "no filter
// applied".
func TestRegistryListWhere_460_WildcardsInTheValueAreEscaped(t *testing.T) {
	_, args, err := registryListWhere(registry.ListQuery{Filter: `name CONTAINS "50%_x"`})
	require.NoError(t, err)
	assert.Equal(t, []any{`%50\%\_x%`}, args)
}

// TestRegistryListWhere_460_NoFilterAddsNoCondition — the empty expression means
// "no filter", not "match nothing" and not "match everything by accident".
func TestRegistryListWhere_460_NoFilterAddsNoCondition(t *testing.T) {
	conds, args, err := registryListWhere(registry.ListQuery{ProjectID: "prj-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"project_id = $1"}, conds)
	assert.Equal(t, []any{"prj-1"}, args)
}

// TestRegistryListWhere_460_UnknownFieldStillRefused — the whitelist did not move.
// A fix that reached for the operator and lost the field check would be a wider
// hole than the one being closed.
func TestRegistryListWhere_460_UnknownFieldStillRefused(t *testing.T) {
	_, _, err := registryListWhere(registry.ListQuery{Filter: `bogus CONTAINS "x"`})
	require.Error(t, err)
}
