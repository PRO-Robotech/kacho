// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
)

// journalPrincipalCtx — контекст пробы с принципалом пользователя.
//
// Пишущие транзакции модуля открывает помощник записи журнала
// (`journaltx.Begin`), и инициатора изменения он берёт только из принципала
// контекста (NTF-3, замысел issue-2918 З4); контекст без принципала — отказ до
// обращения к базе. Проба, пишущая через писателя модуля, несёт принципал так
// же, как его несёт запрос.
func journalPrincipalCtx(ctx context.Context) context.Context {
	return operations.WithPrincipal(ctx, operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)})
}
