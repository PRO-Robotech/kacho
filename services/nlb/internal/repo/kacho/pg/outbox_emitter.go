// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/subscription"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

// outboxEmitter — реализация kacho.OutboxEmitter: строка `nlb_outbox` в той же
// транзакции writer'а, что и DML; триггер `nlb_outbox_notify_trg` шлёт
// `pg_notify('nlb_outbox', sequence_no::text)` после коммита.
type outboxEmitter struct {
	tx *journaltx.Tx
}

// Emit пишет строку журнала ФУНКЦИЕЙ ФУНДАМЕНТА с дескриптором nlb
// (`subscription.Journal.Emit` объявления `subscriptionjournal.Journal()`),
// а не своей вставкой (замысел issue-2918, З5, З6).
//
// Словарь у записи один — объявление владельца (`Mapping`): вид или род
// изменения вне него, вид без объявленных формы имени и якоря, пустой якорь
// проектного вида, снятие именованного вида без имени — отказ
// `subscription.ErrEntryRefused` ДО оператора. Ограничение базы
// (`nlb_outbox_resource_type_check`) остаётся последним словом, но словарь
// у них разный: база принимает ещё ключ строки сигнала ленты, который пишет
// не этот эмиттер.
//
// Отказ объявления — дефект вызывающего (слова берутся константами
// `kacho.OutboxResource*` / `kacho.OutboxAction*`), а не ошибка ввода
// арендатора: он уходит `kacho.ErrInternal` с сохранённой причиной
// (`errors.Is` видит обе). Отказ базы классифицирует `mapPgErr`.
func (e *outboxEmitter) Emit(ctx context.Context, resourceType, resourceID, projectID, action string, payload map[string]any) error {
	err := subscriptionjournal.Journal().Emit(ctx, e.tx, subscription.Entry{
		Kind:      resourceType,
		ID:        resourceID,
		ProjectID: projectID,
		Change:    action,
		Payload:   payload,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, subscription.ErrEntryRefused), errors.Is(err, subscription.ErrNotHelperTx):
		return fmt.Errorf("%w: outbox: %w", kacho.ErrInternal, err)
	}
	return mapPgErr(err, "outbox", "")
}
