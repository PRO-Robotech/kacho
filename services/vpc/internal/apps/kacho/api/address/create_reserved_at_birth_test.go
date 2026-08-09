// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

// What `reserved` means on a newly created Address, and why it is true.
//
// `reserved` says the address is held by the project in its own right: the
// tenant asked for the address itself, so it outlives every consumer and goes
// away only when the tenant deletes it. Its opposite is an address allocated as
// a side effect of creating something else, whose life is tied to that
// consumer. `AddressService.Create` is the tenant asking for an address, and
// nothing else in this service creates one — so every address born here is a
// reservation, and `doCreate` says so unconditionally.
//
// That is the contract, not an inference from the code: the RPC written to undo
// it says so out loud. `InternalAddressService.MarkAddressEphemeralInUse` exists
// precisely because an address auto-allocated for an interface is *not* a
// reservation, and its own comment records that such addresses "создаются через
// публичный AddressService.Create с `reserved = true`, но для
// авто-аллоцированного NIC-адреса это неверно". A flag that has to be cleared
// afterwards was set beforehand. The founding acceptance for this service says
// the same in the shape the resource had then — sub-phase 0.3-F1: creating an
// Address puts it in `status.state = "RESERVED"` synchronously — and the flat
// redesign split that state into the `reserved`/`used` pair.
//
// The claim these tests replace ran the other way: that `reserved` "belongs to
// Update, and a fresh address is not reserved", inferred from the field being
// absent from `CreateAddressRequest`. Absence from the request means the caller
// cannot choose the value. It says nothing about which value the service picks,
// and the service picks true.
//
// Two tests, because one alone would not separate anything: the first pins the
// value at birth, the second changes it through the only door that opens —
// `Update` — and reads it back. Together they show the assertion tracks the
// field rather than restating a constant.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// TestCreateUseCase_FreshAddressIsReserved pins the birth state: an address
// created through the public use-case is reserved and not yet used.
func TestCreateUseCase_FreshAddressIsReserved(t *testing.T) {
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	uc := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil)
	listUC := NewListAddressesUseCase(kr, narrowtest.AllowingAll())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-born-reserved",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.20"},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	addrs, _, _ := listUC.Execute(narrowtest.Caller(), AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Len(t, addrs, 1)
	require.True(t, addrs[0].Reserved,
		"an address the tenant asked for is held for the project from the moment it exists")
	require.False(t, addrs[0].Used,
		"nothing references it yet — reserved and used are independent")
}

// TestUpdateUseCase_ReservedIsClearedThroughUpdate is the other half: the flag
// is the tenant's to give up, and `Update` is where that happens. It also keeps
// the test above honest — if the assertion were reading a constant instead of
// the field, this one could not move it.
func TestUpdateUseCase_ReservedIsClearedThroughUpdate(t *testing.T) {
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	createUC := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil)
	updateUC := NewUpdateAddressUseCase(kr, or)
	listUC := NewListAddressesUseCase(kr, narrowtest.AllowingAll())

	op, err := createUC.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-unreserve",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.21"},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	addrs, _, _ := listUC.Execute(narrowtest.Caller(), AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Len(t, addrs, 1)
	require.True(t, addrs[0].Reserved, "precondition: born reserved")

	upOp, err := updateUC.Execute(context.Background(), UpdateInput{
		AddressID:  addrs[0].ID,
		Reserved:   false,
		UpdateMask: []string{"reserved"},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, or, upOp.ID).Error)

	addrs, _, _ = listUC.Execute(narrowtest.Caller(), AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Len(t, addrs, 1)
	require.False(t, addrs[0].Reserved,
		"giving up the reservation is a tenant decision, taken through Update")
}
