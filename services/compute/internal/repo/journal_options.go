// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import "github.com/PRO-Robotech/corelib/journaltx"

// journalOptions — Options помощника записи журнала (`journaltx.Begin`), с
// которыми писатели модуля открывают каждую пишущую транзакцию.
//
// Ручки флага ленты у модуля нет, и лента модуля выключена: флаг — `false`.
// Ручку `KACHO_COMPUTE_NOTIFICATIONS_ENABLED` и позиционный аргумент `Options`
// конструкторов писателей вводит полоса S1-A4 issue-2918 (замысел З11, З4 (а));
// тем же изменением эта функция снимается.
//
// Помощник выставляет инициатора изменения локально к транзакции и берёт его
// только из принципала контекста; контекст без принципала — отказ до обращения
// к базе, транзакция не открывается (NTF-3, З4).
func journalOptions() journaltx.Options { return journaltx.NewOptions(false) }
