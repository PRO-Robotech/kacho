// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

// Список операций ресурса отдаёт операции САМОГО вызывающего.
//
// Утверждение — на наблюдаемом: строки, которые вернул use-case. Фейк
// repomock.OpsRepo реализует оба пути (несуженный List и суженный ListOwned),
// поэтому тест краснеет ровно тогда, когда вызывается несуженный.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

func TestListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	or := repomock.NewOpsRepo()
	uc := NewListOperationsUseCase(or)
	resID := ids.NewID(ids.PrefixNetworkInterface)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	bg := context.Background()
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-mine", ResourceID: resID}, me))
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-foreign", ResourceID: resID}, other))

	got, _, err := uc.Execute(operations.WithPrincipal(bg, me), resID, Pagination{})
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, op := range got {
		seen[op.ID] = true
	}
	require.True(t, seen["op-mine"], "своя операция обязана присутствовать")
	require.False(t, seen["op-foreign"],
		"чужая операция попала в список: её Response несёт ресурс целиком, а Principal — email инициатора")
}

// Без ключа владения выдача пуста: несуженный откат запрещён.
func TestListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	or := repomock.NewOpsRepo()
	uc := NewListOperationsUseCase(or)
	resID := ids.NewID(ids.PrefixNetworkInterface)

	require.NoError(t, or.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", ResourceID: resID},
		operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}))

	got, _, err := uc.Execute(context.Background(), resID, Pagination{})
	require.NoError(t, err)
	require.Empty(t, got, "без ключа владения выдача обязана быть пустой, а не полной")
}
