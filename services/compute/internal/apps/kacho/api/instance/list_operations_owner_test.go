// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

// Право на список операций ресурса отдаёт СВОИ операции, а не всю историю.
//
// Проверка — на наблюдаемом: страница, которую вернул use-case. Утверждение
// «вызвана суженная функция» этот класс не ловит: оно останется зелёным, если
// суженную функцию позовут с ключом, который матчит всех.
//
// Строка операции несёт полный ресурс в Response и личность инициатора
// (Principal), поэтому чужая строка на выдаче — это и чужое содержимое, и чужая
// личность одновременно.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

func TestInstance_ListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedRunningInstance(k.repo, domain.InstanceStatusRunning)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}

	bg := context.Background()
	require.NoError(t, k.ops.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-mine", Description: "mine", ResourceID: "epdvm1"}, me))
	require.NoError(t, k.ops.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-foreign", Description: "foreign", ResourceID: "epdvm1"}, other))

	got, _, err := k.svc.ListOperations(operations.WithPrincipal(bg, me), "epdvm1", Pagination{})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, op := range got {
		ids[op.ID] = true
	}
	require.True(t, ids["op-mine"], "своя операция обязана присутствовать")
	require.False(t, ids["op-foreign"],
		"чужая операция попала в список: её Response несёт ресурс целиком, а Principal — email инициатора")
}

// Неопознанный вызывающий не получает несуженную выдачу: ключа владения у него
// нет, значит и своих операций нет. Пустая страница — не «нет данных», а
// «нечего показать именно вам».
func TestInstance_ListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedRunningInstance(k.repo, domain.InstanceStatusRunning)

	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	require.NoError(t, k.ops.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", Description: "foreign", ResourceID: "epdvm1"}, other))

	got, _, err := k.svc.ListOperations(context.Background(), "epdvm1", Pagination{})
	require.NoError(t, err)
	require.Empty(t, got, "без ключа владения выдача обязана быть пустой, а не полной")
}
