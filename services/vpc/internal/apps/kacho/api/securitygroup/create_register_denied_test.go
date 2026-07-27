// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

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

// Отказ sync-регистрации owner-tuple приходит ПОСЛЕ `w.Commit()`. Проброс наверх
// отдаёт вызывающему чужой код узла прав (status.FromError забирает вложенный
// статус) и служебную обёртку `rpc error: …` в тексте — на группе, которая уже
// создана. Intent на тот же tuple durable в fga_register_outbox той же writer-TX.
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndSecurityGroupExists(t *testing.T) {
	ctx := context.Background()
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	_, nerr := nr.Insert(ctx, &domain.Network{ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	require.NoError(t, nerr)
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or).WithRegistrar(reg)

	op, err := uc.Execute(ctx, domain.SecurityGroup{
		ProjectID: "f1",
		NetworkID: netID,
		Name:      domain.RcNameVPC("sg-reg-denied"),
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

	require.Len(t, sgr.SecurityGroups(), 1, "resource is durable: commit happened before registration")
	require.NotEmpty(t, sgr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
