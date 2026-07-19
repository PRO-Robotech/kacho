// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"log/slog"
)

// syncRegisterOwner синхронно (post-commit, best-effort) регистрирует owner-tuple
// свеже-созданного ресурса через registrar — чисто window-оптимизация: owner-tuple
// становится эффективен в FGA СРАЗУ, не дожидаясь poll'а async register-drainer'а
// (сужает eventual-consistency-окно, в котором немедленная мутация создателя могла
// бы кратко получить 403/404). Ошибка НЕ проваливает Create: durable outbox-intent
// (эмитится в writer-tx repo.Insert) + register-drainer остаются at-least-once
// backstop'ом (та же идемпотентная регистрация повторно безопасна). registrar==nil →
// no-op (полагаемся на drainer).
func syncRegisterOwner(ctx context.Context, registrar OwnerRegistrar, kind, resourceID, projectID string, labels map[string]string) {
	if registrar == nil {
		return
	}
	if err := registrar.Register(ctx, kind, resourceID, projectID, labels); err != nil {
		slog.WarnContext(ctx, "owner-tuple sync register failed; register-drainer will backstop at-least-once",
			"err", err, "kind", kind, "resource", resourceID)
	}
}
