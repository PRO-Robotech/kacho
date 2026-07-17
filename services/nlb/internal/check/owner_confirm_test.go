// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

// owner-tuple opgate — OwnerConfirmer contract (OTG-08). Проба идёт по тому же
// InternalIAMService.Check ребру (reuse iamclient.CheckClient); confirmed=true ⇔
// v_update ALLOW; ErrNoPath/ErrHideExistence → pending (false, nil, НЕ transient);
// прочий err → transient (false, err). HIGHER_CONSISTENCY-путь (CheckConsistent)
// используется, когда client его реализует.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// fakeCheckClient реализует iamclient.CheckClient + consistentChecker. Записывает,
// какой путь дёрнули (Check vs CheckConsistent), и по какому relation/object.
type fakeCheckClient struct {
	allow          bool
	err            error
	consistentUsed bool
	gotRelation    string
	gotObject      string
	gotSubject     string
}

func (f *fakeCheckClient) Check(_ context.Context, subject, relation, object string) (bool, error) {
	f.gotSubject, f.gotRelation, f.gotObject = subject, relation, object
	return f.allow, f.err
}

func (f *fakeCheckClient) CheckConsistent(_ context.Context, subject, relation, object string) (bool, error) {
	f.consistentUsed = true
	f.gotSubject, f.gotRelation, f.gotObject = subject, relation, object
	return f.allow, f.err
}

// checkOnlyClient — реализует ТОЛЬКО Check (нет CheckConsistent) → fallback-путь.
type checkOnlyClient struct {
	allow bool
	err   error
	used  bool
}

func (c *checkOnlyClient) Check(_ context.Context, _, _, _ string) (bool, error) {
	c.used = true
	return c.allow, c.err
}

var creator = operations.Principal{Type: "user", ID: "usr-otg"}

func TestOwnerConfirmer_Allow_ConfirmedAndConsistentPath(t *testing.T) {
	cc := &fakeCheckClient{allow: true}
	c := NewLoadBalancerOwnerConfirmer(cc)

	ok, err := c.Confirm(context.Background(), creator, "nlbtestid00000001")
	require.NoError(t, err)
	assert.True(t, ok, "v_update ALLOW → confirmed")
	assert.True(t, cc.consistentUsed, "HIGHER_CONSISTENCY-путь (CheckConsistent) при read-after-register")
	assert.Equal(t, relationVUpdate, cc.gotRelation, "confirm по v_update")
	assert.Equal(t, "lb_network_load_balancer:nlbtestid00000001", cc.gotObject)
	assert.Equal(t, "user:usr-otg", cc.gotSubject)
}

func TestOwnerConfirmer_NoPath_PendingNotTransient(t *testing.T) {
	cc := &fakeCheckClient{err: authz.ErrNoPath}
	c := NewListenerOwnerConfirmer(cc)

	ok, err := c.Confirm(context.Background(), creator, "lsttestid00000001")
	require.NoError(t, err, "ErrNoPath — pending, НЕ transient (err=nil)")
	assert.False(t, ok)
	assert.Equal(t, "lb_listener:lsttestid00000001", cc.gotObject)
}

func TestOwnerConfirmer_HideExistence_Pending(t *testing.T) {
	cc := &fakeCheckClient{err: authz.ErrHideExistence}
	c := NewTargetGroupOwnerConfirmer(cc)

	ok, err := c.Confirm(context.Background(), creator, "tgrtestid00000001")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "lb_target_group:tgrtestid00000001", cc.gotObject)
}

func TestOwnerConfirmer_TransientErr_Propagated(t *testing.T) {
	boom := errors.New("iam unavailable")
	cc := &fakeCheckClient{err: boom}
	c := NewLoadBalancerOwnerConfirmer(cc)

	ok, err := c.Confirm(context.Background(), creator, "nlbtestid00000001")
	assert.False(t, ok)
	require.Error(t, err, "transient (не sentinel) → err пробрасывается (worker ретраит/fail-closed)")
	assert.ErrorIs(t, err, boom)
}

func TestOwnerConfirmer_FallbackToPlainCheck(t *testing.T) {
	cc := &checkOnlyClient{allow: true}
	c := NewLoadBalancerOwnerConfirmer(cc)

	ok, err := c.Confirm(context.Background(), creator, "nlbtestid00000001")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, cc.used, "client без CheckConsistent → fallback на Check")
}
