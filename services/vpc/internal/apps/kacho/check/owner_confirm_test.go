// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// owner-tuple opgate P3 — confirmer unit (OTG-08 reuse-Check; subject/relation/object
// корректность; pending-vs-transient семантика). Confirmer — тонкий адаптер поверх
// authz.CheckClient; здесь фиксируем, что он зовёт ИМЕННО `subject #v_update object`
// (тот резолв, что gateway scope_extractor выполнит на немедленной мутации).

// fakeCheckClient — управляемый authz.CheckClient для confirmer-теста.
type fakeCheckClient struct {
	mu      sync.Mutex
	calls   []checkCall
	allowed bool
	err     error
}

type checkCall struct {
	subject  string
	relation string
	object   string
}

func (f *fakeCheckClient) Check(_ context.Context, subject, relation, object string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, checkCall{subject, relation, object})
	return f.allowed, f.err
}

var _ authz.CheckClient = (*fakeCheckClient)(nil)

func creator() operations.Principal {
	return operations.Principal{Type: "user", ID: "usr_owner"}
}

// OTG-08 / D5 — confirmer зовёт существующий Check с корректной тройкой subject
// (creator), relation (v_update), object (<type>:<id>) — та же тройка, что gateway
// scope_extractor резолвит на немедленной мутации.
func TestOwnerConfirmer_Network_CallsCheckWithMutateRelation(t *testing.T) {
	cc := &fakeCheckClient{allowed: true}
	c := NewNetworkOwnerConfirmer(cc)

	confirmed, err := c.Confirm(context.Background(), creator(), "enp_net123")
	require.NoError(t, err)
	assert.True(t, confirmed)

	call := cc.calls[len(cc.calls)-1]
	assert.Equal(t, "user:usr_owner", call.subject, "subject = FGA-форма creator'а")
	assert.Equal(t, relationVUpdate, call.relation, "relation = канонический mutate-relation v_update")
	assert.Equal(t, "vpc_network:enp_net123", call.object, "object = vpc_network:<id>")
}

func TestOwnerConfirmer_SecurityGroup_Object(t *testing.T) {
	cc := &fakeCheckClient{allowed: true}
	c := NewSecurityGroupOwnerConfirmer(cc)
	_, err := c.Confirm(context.Background(), creator(), "ens_sg1")
	require.NoError(t, err)
	assert.Equal(t, "vpc_security_group:ens_sg1", cc.calls[0].object)
	assert.Equal(t, relationVUpdate, cc.calls[0].relation)
}

func TestOwnerConfirmer_Subnet_Object(t *testing.T) {
	cc := &fakeCheckClient{allowed: true}
	c := NewSubnetOwnerConfirmer(cc)
	_, err := c.Confirm(context.Background(), creator(), "eng_sub1")
	require.NoError(t, err)
	assert.Equal(t, "vpc_subnet:eng_sub1", cc.calls[0].object)
	assert.Equal(t, relationVUpdate, cc.calls[0].relation)
}

// Deny (allowed=false, err=nil) → pending: confirmed=false, err=nil (worker ретраит).
func TestOwnerConfirmer_Deny_IsPendingNotError(t *testing.T) {
	cc := &fakeCheckClient{allowed: false}
	c := NewNetworkOwnerConfirmer(cc)
	confirmed, err := c.Confirm(context.Background(), creator(), "enp_x")
	require.NoError(t, err, "plain deny — pending, не transient-сбой")
	assert.False(t, confirmed)
}

// ErrNoPath — owner-tuple ещё не виден → pending (confirmed=false, err=nil), НЕ
// transient. Ключ FIX: worker не должен трактовать «tuple ещё не зареган» как сбой.
func TestOwnerConfirmer_ErrNoPath_IsPendingNotError(t *testing.T) {
	cc := &fakeCheckClient{allowed: false, err: authz.ErrNoPath}
	c := NewNetworkOwnerConfirmer(cc)
	confirmed, err := c.Confirm(context.Background(), creator(), "enp_x")
	require.NoError(t, err, "ErrNoPath = owner-tuple ещё не виден → pending, не transient")
	assert.False(t, confirmed)
}

func TestOwnerConfirmer_ErrHideExistence_IsPending(t *testing.T) {
	cc := &fakeCheckClient{allowed: false, err: authz.ErrHideExistence}
	c := NewNetworkOwnerConfirmer(cc)
	confirmed, err := c.Confirm(context.Background(), creator(), "enp_x")
	require.NoError(t, err)
	assert.False(t, confirmed)
}

// Транспорт/Unavailable (прочий err) → transient: confirmed=false, err!=nil.
func TestOwnerConfirmer_TransportError_IsTransient(t *testing.T) {
	sentinel := errors.New("iam unavailable")
	cc := &fakeCheckClient{allowed: false, err: sentinel}
	c := NewNetworkOwnerConfirmer(cc)
	confirmed, err := c.Confirm(context.Background(), creator(), "enp_x")
	require.Error(t, err, "транспорт-сбой должен пробрасываться как transient (worker ретраит/логирует)")
	assert.ErrorIs(t, err, sentinel)
	assert.False(t, confirmed)
}

// Вырожденный resourceID (пустой) → err (не pending): owner-tuple такого object не
// подтвердится никогда → fail-closed по deadline, а не ложный success.
func TestOwnerConfirmer_EmptyResourceID_ReturnsError(t *testing.T) {
	cc := &fakeCheckClient{allowed: true}
	c := NewNetworkOwnerConfirmer(cc)
	confirmed, err := c.Confirm(context.Background(), creator(), "")
	require.Error(t, err)
	assert.False(t, confirmed)
	assert.Empty(t, cc.calls, "Check не должен вызываться на вырожденном object")
}
