// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"

	"github.com/PRO-Robotech/corelib/journaltx"
)

// inJournalTx исполняет fn в транзакции помощника записи журнала: открытие —
// `journaltx.Begin`, отказ fn — откат, иначе фиксация. Форма та же, что у
// `pgx.BeginFunc`, и ошибка fn возвращается как есть.
//
// Помощник выставляет инициатора изменения локально к транзакции и берёт его
// только из принципала контекста; контекст без принципала — отказ до обращения
// к базе, транзакция не открывается (NTF-3, З4). Строки журнала storage пишут
// функции базы на журналируемых таблицах (`volumes`, `snapshots`, `images`,
// `volume_attachments`), поэтому любая их запись идёт этой транзакцией, в том
// числе одиночный оператор.
func inJournalTx(ctx context.Context, src journaltx.TxStarter, opts journaltx.Options, fn func(tx *journaltx.Tx) error) error {
	tx, err := journaltx.Begin(ctx, src, opts)
	if err != nil {
		return err
	}
	// Откат после успешной фиксации — no-op (pgx.ErrTxClosed), поэтому defer
	// снимает транзакцию на всяком пути, кроме фиксации, включая панику fn.
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
