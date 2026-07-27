// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

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

// Network.Create отражает исход worker-fn синхронно (operations.RunSync), но
// точка отказа та же: sync-регистрация owner-tuple идёт ПОСЛЕ `w.Commit()`.
// RunSync прогоняет ошибку через тот же status.FromError — вызывающий получает
// чужой код узла прав и служебную обёртку `rpc error: …` в тексте операции,
// притом что сеть уже создана. Intent на тот же tuple durable в
// fga_register_outbox той же writer-TX.
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndNetworkExists(t *testing.T) {
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithRegistrar(reg)

	op, err := uc.Execute(ctx, domain.Network{
		ProjectID: "f1",
		Name:      domain.RcNameVPC("net-reg-denied"),
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

	require.Len(t, kr.Networks(), 1, "resource is durable: commit happened before registration")
	require.NotEmpty(t, kr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
