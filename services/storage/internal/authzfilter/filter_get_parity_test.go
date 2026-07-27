// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// filter_get_parity_test.go — the list page may not name an object the object's
// own Get would refuse.
//
// The page this filter emits is not a set of ids: the handler returns the FULL
// rows behind them. So "visible in the list" and "readable by Get" must be the
// same question, asked once. When the filter's predicate is WIDER than the
// relation the permission catalog gates Get on, the caller receives an object it
// then cannot fetch — and learns the id of a resource it has no access to. That is
// an existence oracle handed out by the read path itself.
//
// The permission catalog is the authority (the api-gateway enforces it, and
// services/storage/internal/check mirrors it). These tests pin the filter to that
// same authority.

// TestVisibilityRelationsMatchCatalogGetRelation locks the filter's predicate to
// the relation the catalog gates the per-object read (Get) on. It reads the
// catalog rather than restating a literal, so a future catalog change cannot leave
// this file quietly describing the old contract.
func TestVisibilityRelationsMatchCatalogGetRelation(t *testing.T) {
	catalog, err := catalogparity.LoadCatalog(".")
	require.NoError(t, err, "load permission catalog")

	// Every per-object read storage publishes. If the catalog ever gates these on
	// different relations, "what the list may show" stops being one answer and this
	// test must be revisited deliberately.
	getMethods := []string{
		"/kacho.cloud.storage.v1.VolumeService/Get",
		"/kacho.cloud.storage.v1.SnapshotService/Get",
		"/kacho.cloud.storage.v1.ImageService/Get",
	}

	want := map[string]bool{}
	for _, m := range getMethods {
		entry, ok := catalog[m]
		require.Truef(t, ok, "catalog has no entry for %s — cannot derive the read relation", m)
		require.NotEmptyf(t, entry.RequiredRelation, "catalog entry for %s carries no required_relation", m)
		want[entry.RequiredRelation] = true
	}
	require.Lenf(t, want, 1,
		"storage Get RPCs are gated on more than one relation (%v); the list predicate can no longer mirror a single read relation", want)

	got := map[string]bool{}
	for _, r := range visibilityRelations {
		got[r] = true
	}
	assert.Equalf(t, want, got,
		"the list visibility predicate must be exactly the relation the catalog gates Get on: "+
			"anything wider puts objects on the page that Get then refuses, disclosing their ids")
}

// TestListNeverShowsWhatGetWouldRefuse is the behavioural lock: an object the
// subject holds ONLY the list-selector grant on (v_list, without the read tier the
// catalog gates Get on) must not reach the page.
//
// This is the observable form of the defect. The stub answers `v_list` yes and
// `viewer` no for one object — precisely the state a partially-materialized or
// partially-revoked grant leaves behind — and the page must come back without it.
func TestListNeverShowsWhatGetWouldRefuse(t *testing.T) {
	const (
		readable   = "vol0000000000000001" // full read tier: Get would serve it
		listOnly   = "vol0000000000000002" // v_list only: Get would refuse it
		unreadable = "vol0000000000000003" // nothing at all
	)

	client := newFakeAuthorizeClient().
		allow("viewer", readable).
		allow("v_list", listOnly)

	f := NewFGAFilter(client, Config{Enabled: true})

	got, err := f.FilterVisibleIDs(context.Background(), "user:usr_alice", "storage_volume",
		"storage.volumes.list", []string{readable, listOnly, unreadable})
	require.NoError(t, err)

	assert.Equal(t, []string{readable}, got,
		"an object granted only v_list must not appear on the page: Get is gated on the read tier and would refuse it, "+
			"so listing it discloses the id of a resource the caller cannot read")
}
