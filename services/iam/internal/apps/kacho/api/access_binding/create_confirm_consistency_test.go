// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package access_binding

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/iam/internal/clients"
)

// create_confirm_consistency_test.go — the iam in-process owner-tuple confirm probe
// (AccessBinding.Create) must read with HIGHER_CONSISTENCY (Koren-1 tail fix): the
// per-object owner tuple is materialized+written synchronously to the same OpenFGA
// store on the create path, so a default read could be served a stale-replica
// negative under the multi-replica deployment.

// consistencyRelStore — clients.RelationStore that records whether the confirm probe
// used the default Check or the HIGHER_CONSISTENCY CheckConsistent path.
type consistencyRelStore struct {
	consistentCalls int
	plainCalls      int
	allow           bool
	lastRelation    string
	lastObject      string
}

func (s *consistencyRelStore) Check(_ context.Context, _, relation, object string) (bool, error) {
	s.plainCalls++
	s.lastRelation, s.lastObject = relation, object
	return s.allow, nil
}

func (s *consistencyRelStore) CheckConsistent(_ context.Context, _, relation, object string) (bool, error) {
	s.consistentCalls++
	s.lastRelation, s.lastObject = relation, object
	return s.allow, nil
}

func (*consistencyRelStore) WriteTuples(context.Context, []clients.RelationTuple) error  { return nil }
func (*consistencyRelStore) DeleteTuples(context.Context, []clients.RelationTuple) error { return nil }

var _ clients.RelationStore = (*consistencyRelStore)(nil)

func TestOwnerTupleConfirm_UsesHigherConsistency(t *testing.T) {
	store := &consistencyRelStore{allow: true}
	u := &CreateAccessBindingUseCase{relations: store}
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_owner"})

	confirm := u.ownerTupleConfirm(ctx, "acb_123")
	require.NotNil(t, confirm)

	ok, err := confirm(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 1, store.consistentCalls, "in-process confirm probe must read with HIGHER_CONSISTENCY")
	assert.Equal(t, 0, store.plainCalls, "in-process confirm probe must NOT use the default (cache-eligible) read")
	assert.Equal(t, "v_update", store.lastRelation)
	assert.Equal(t, "iam_access_binding:acb_123", store.lastObject)
}
