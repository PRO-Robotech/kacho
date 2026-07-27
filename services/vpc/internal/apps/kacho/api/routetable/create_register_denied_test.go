// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Отказ sync-регистрации owner-tuple приходит ПОСЛЕ `w.Commit()`. Проброс наверх
// даёт вызывающему чужой код узла прав (status.FromError забирает вложенный
// статус) и служебную обёртку `rpc error: …` в тексте — на ресурс, который уже
// создан. Intent на тот же tuple durable в fga_register_outbox той же writer-TX.
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndRouteTableExists(t *testing.T) {
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	net := makeNetwork(t, kr)
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateRouteTableUseCase(kr, &repomock.ProjectClient{OK: true}, or).WithRegistrar(reg)

	op, err := uc.Execute(ctx, domain.RouteTable{
		ProjectID: "f1",
		NetworkID: net.ID,
		Name:      domain.RcNameVPC("rt-reg-denied"),
		StaticRoutes: []domain.StaticRoute{
			{DestinationPrefix: "0.0.0.0/0", NextHopAddress: "192.168.0.1"},
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

	require.Len(t, kr.RouteTables(), 1, "resource is durable: commit happened before registration")
	require.NotEmpty(t, kr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
