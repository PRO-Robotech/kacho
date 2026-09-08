// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// fga_register_outbox_intra_tx_order_integration_test.go — the emitter's
// ordering contract, at the primitive that produces it.
//
// The outcome this protects is locked end-to-end in
// apps/kacho/api/{loadbalancer,targetgroup}/move_mirror_projection_integration_test.go.
// This test pins the mechanism those depend on, so a refactor of the emitter
// fails HERE — where the cause is — instead of only in a Move scenario.

// TestFGARegisterOutbox_IntraTxOrder_VersionsAreStrictlyIncreasing — several
// intents about the SAME object emitted in ONE writer-tx must be distinguishable
// AND correctly ordered by source_version.
//
// kaname resolves competing intents by comparing that version: a register
// applies only when strictly newer than the stored one, and an unregister deletes
// only when the stored one is not newer than its tombstone. Equal versions
// therefore mean "whichever is applied last wins", which turns emission order
// into an unstated dependency and lets a later unregister erase an earlier
// register of the same object.
func TestFGARegisterOutbox_IntraTxOrder_VersionsAreStrictlyIncreasing(t *testing.T) {
	tc := newTestCtx(t)
	ctx := context.Background()

	const srcProject = "prj-order-src-aaaaa"
	const dstProject = "prj-order-dst-aaaaa"
	lb := newLB(srcProject, "lb-order")
	id := string(lb.ID)

	unreg := domain.FGARegisterIntent{
		Kind:       "NetworkLoadBalancer",
		ResourceID: id,
		Tuples:     []domain.FGATuple{domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, id, srcProject)},
	}
	reg := domain.FGARegisterIntent{
		Kind:            "NetworkLoadBalancer",
		ResourceID:      id,
		Tuples:          []domain.FGATuple{domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, id, dstProject)},
		ParentProjectID: dstProject,
	}

	commitWriter(t, tc.Repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		// Штампы ДВУХ намерений одной writer-транзакции обязаны РАЗЛИЧАТЬСЯ:
		// иначе снятие и постановка на одном объекте неотличимы по версии, и
		// принимающая сторона применила бы их в произвольном порядке.
		unregStamp, uerr := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventUnregister, unreg)
		require.NoError(t, uerr)
		regStamp, rerr := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, reg)
		require.NoError(t, rerr)
		require.True(t, regStamp.After(unregStamp),
			"второе намерение той же транзакции обязано быть строго новее первого: %s не позже %s",
			regStamp, unregStamp)
	})

	rows := queryRegisterRows(t, ctx, tc)
	require.Len(t, rows, 2, "both intents persisted by the one writer-tx")

	// Rows come back ordered by id — the same order the drainer's per-resource
	// FIFO applies them in, since both carry the same resource_id.
	first, err := decodeIntent(rows[0].payload)
	require.NoError(t, err)
	second, err := decodeIntent(rows[1].payload)
	require.NoError(t, err)

	require.Equal(t, domain.FGAEventUnregister, rows[0].eventType)
	require.Equal(t, domain.FGAEventRegister, rows[1].eventType)
	require.Equal(t, rows[0].resourceID, rows[1].resourceID,
		"same object ⇒ same drainer partition ⇒ applied in id order")

	require.Falsef(t, first.SourceVersion.IsZero(), "first intent must carry a source_version")
	require.Falsef(t, second.SourceVersion.IsZero(), "second intent must carry a source_version")
	require.Truef(t, second.SourceVersion.After(first.SourceVersion),
		"intent #2 of one writer-tx must be STRICTLY newer than intent #1 "+
			"(#1=%s, #2=%s) — equal versions leave the outcome to apply order alone",
		first.SourceVersion.UTC(), second.SourceVersion.UTC())
}
