// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Отказ sync-регистрации owner-tuple приходит ПОСЛЕ `w.Commit()` — на адресе,
// который уже создан и, для external-семейства, уже держит lease из пула.
// Проброс наверх отдаёт вызывающему чужой код узла прав (status.FromError
// забирает вложенный статус) и служебную обёртку `rpc error: …` в тексте, а сам
// адрес остаётся фантомом: повтор ловит AlreadyExists по занятому имени.
// Intent на тот же tuple durable в fga_register_outbox той же writer-TX.
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndAddressExists(t *testing.T) {
	ctx := context.Background()
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil).WithRegistrar(reg)
	listUC := NewListAddressesUseCase(kr, nil)

	op, err := uc.Execute(ctx, CreateInput{
		ProjectID: "f1",
		Name:      "addr-reg-denied",
		ExternalSpec: &ExternalAddrSpec{
			Address: "203.0.113.10",
			ZoneID:  "zone-a",
		},
	})
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)

	require.Equal(t, 1, reg.Calls(), "registrar must have been invoked on the create path")

	detail := "no terminal error"
	if saved.Error != nil {
		detail = fmt.Sprintf("code=%d message=%q", saved.Error.Code, saved.Error.Message)
		assert.NotContains(t, saved.Error.Message, "rpc error:",
			"the caller must never see the internal wrapper chain")
	}
	require.Nil(t, saved.Error,
		"owner-tuple registration runs after the durable commit — its refusal must not fail the mutation; got %s",
		detail)

	addrs, _, lerr := listUC.Execute(ctx, "", AddressFilter{ProjectID: "f1"}, Pagination{})
	require.NoError(t, lerr)
	require.Len(t, addrs, 1, "resource is durable: commit happened before registration")

	require.NotEmpty(t, kr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
