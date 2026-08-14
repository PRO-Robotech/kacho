// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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

// Sync-регистрация owner-tuple выполняется ПОСЛЕ `w.Commit()`, то есть на уже
// созданном ресурсе. Пробрасывать её отказ наверх нельзя по двум причинам.
//
// Во-первых, worker-fn прогоняет ошибку через status.FromError, который через
// errors.As достаёт вложенный статус узла прав, забирает ЕГО код как терминальный
// код операции и подменяет сообщение текстом всей цепочки — вызывающий получает
// чужой код и служебную обёртку `rpc error: …` в тексте.
//
// Во-вторых, ресурс к этому моменту durable: строка закоммичена, имя занято
// уникальностью. Отказ операции делает его фантомом — клиент видит ошибку,
// повторяет и ловит AlreadyExists. Доступ доедет сам: intent на тот же tuple
// лежит в fga_register_outbox той же writer-TX (at-least-once дренаж).
func TestCreateUseCase_OwnerTupleRegisterDenied_OperationSucceedsAndGatewayExists(t *testing.T) {
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &repomock.DenyingRegistrar{}

	uc := NewCreateGatewayUseCase(kr, &repomock.ProjectClient{OK: true}, or).WithRegistrar(reg)

	op, err := uc.Execute(ctx, domain.Gateway{
		ProjectID:   "f1",
		Name:        domain.RcNameVPC("gw-reg-denied"),
		GatewayType: domain.GatewayTypeEgressOnly,
		// Ветвь «только исход» — она не требует пула и аренды; предмет пробы
		// здесь отказ узла прав, а не выделение адреса.
		SubnetID: seedSubnetID,
	})
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)

	// Тест обязан реально пройти по спорной ветке, иначе проверки ниже вакуумны.
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

	require.Len(t, kr.Gateways(), 1, "resource is durable: commit happened before registration")
	require.NotEmpty(t, kr.FGARegisterEvents(),
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}
