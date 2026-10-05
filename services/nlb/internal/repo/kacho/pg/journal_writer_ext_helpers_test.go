// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import "github.com/PRO-Robotech/corelib/journaltx"

// probeJournalOptions — Options писателей журнала модуля в пробах этого
// пакета: построены (journaltx.NewOptions), лента выключена. Флаг ленты в
// транзакции проб, не судящих флаг, — false, как у модуля с выключенной лентой;
// пробы флага строят свои Options сами (УК3-61, NTF3-65/67).
var probeJournalOptions = journaltx.NewOptions(false)

// mustJournalWriter — сборка писателя журнала в пробах: конструктор отвергает
// только нулевые Options (journaltx.ErrOptionsUnset), а probeJournalOptions
// построены, поэтому отказ здесь — ошибка программы пробы, а не исход.
func mustJournalWriter[T any](w T, err error) T {
	if err != nil {
		panic("сборка писателя журнала на построенных Options: " + err.Error())
	}
	return w
}

// droppedName — исход снятия строки без снимка имени: пробам, чей предмет не
// имя на снятии, нужен только отказ (Delete возвращает имя из `RETURNING`).
func droppedName(_ string, err error) error { return err }
