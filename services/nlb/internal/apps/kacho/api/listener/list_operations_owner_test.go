// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

// Список операций ресурса отдаёт операции САМОГО вызывающего.
//
// Утверждение — на наблюдаемом: строки в ответе. Фейк реализует оба пути
// (несуженный List и суженный ListOwned), поэтому тест краснеет ровно тогда,
// когда вызывается несуженный.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

func TestListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	t.Parallel()
	or := newFakeOpsRepo()
	uc := NewListOperationsUseCase(or)
	resID := ids.NewID(ids.PrefixListener)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	bg := context.Background()
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-mine", ResourceID: resID}, me))
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-foreign", ResourceID: resID}, other))

	ctx := operations.WithPrincipal(bg, me)
	resp, err := uc.Run(ctx, &lbv1.ListListenerOperationsRequest{ListenerId: resID})
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, op := range resp.GetOperations() {
		seen[op.GetId()] = true
	}
	require.True(t, seen["op-mine"], "своя операция обязана присутствовать")
	require.False(t, seen["op-foreign"],
		"чужая операция попала в список: её Response несёт ресурс целиком, а Principal — email инициатора")
}

// Без ключа владения выдача пуста: несуженный откат запрещён.
func TestListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	t.Parallel()
	or := newFakeOpsRepo()
	uc := NewListOperationsUseCase(or)
	resID := ids.NewID(ids.PrefixListener)

	require.NoError(t, or.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", ResourceID: resID},
		operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}))

	ctx := context.Background()
	resp, err := uc.Run(ctx, &lbv1.ListListenerOperationsRequest{ListenerId: resID})
	require.NoError(t, err)
	require.Empty(t, resp.GetOperations(), "без ключа владения выдача обязана быть пустой, а не полной")
}
