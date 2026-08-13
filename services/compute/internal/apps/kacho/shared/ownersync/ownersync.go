// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package ownersync — синхронная половина регистрации прав на свежий ресурс.
//
// Пакет отдельный ради единственности: то же самое делает теперь не только
// машина, а вторая копия разошлась бы с первой молча — и разошлась бы именно в
// той ветке, где расхождение не видно, потому что обе на здоровом пути молчат.
package ownersync

import (
	"context"
	"log/slog"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// Register синхронно (post-commit, best-effort) регистрирует owner-tuple
// свеже-созданного ресурса через registrar — чисто window-оптимизация: owner-tuple
// становится эффективен в FGA СРАЗУ, не дожидаясь poll'а async register-drainer'а
// (сужает eventual-consistency-окно, в котором немедленная мутация создателя могла
// бы кратко получить 403/404). Ошибка НЕ проваливает Create: durable outbox-intent
// (эмитится в writer-tx repo.Insert) + register-drainer остаются at-least-once
// backstop'ом (та же идемпотентная регистрация повторно безопасна). registrar==nil →
// no-op (полагаемся на drainer).
func Register(ctx context.Context, registrar ports.OwnerRegistrar, regs []ownerregister.Registration) {
	if registrar == nil || len(regs) == 0 {
		return
	}
	if err := registrar.Register(ctx, regs); err != nil {
		slog.WarnContext(ctx, "owner-tuple sync register failed; register-drainer will backstop at-least-once",
			"err", err, "resource", regs[0].TraceID)
	}
}
