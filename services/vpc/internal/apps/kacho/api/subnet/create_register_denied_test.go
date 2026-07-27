// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Subnet.Create отражает исход worker-fn синхронно (operations.RunSync), но
// точка отказа та же: sync-регистрация owner-tuple идёт ПОСЛЕ `w.Commit()`.
// RunSync прогоняет ошибку через тот же status.FromError — вызывающий получает
// чужой код узла прав и служебную обёртку `rpc error: …` в тексте операции,
// притом что подсеть уже создана и её CIDR уже занят EXCLUDE-ограничением.
// Intent на тот же tuple durable в fga_register_outbox той же writer-TX.
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndSubnetExists(t *testing.T) {
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetwork(t, kr, "f1", netID)
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithRegistrar(reg)

	op, err := uc.Execute(ctx, domain.Subnet{
		ProjectID:    "f1",
		NetworkID:    netID,
		ZoneID:       testZone,
		Name:         domain.RcNameVPC("sub-reg-denied"),
		V4CidrBlocks: []string{"10.30.0.0/24"},
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

	require.Len(t, kr.Subnets(), 1, "resource is durable: commit happened before registration")
	require.NotEmpty(t, kr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
