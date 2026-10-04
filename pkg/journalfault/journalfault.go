// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package journalfault опознаёт отказ журнала модуля ПО ИНИЦИАТОРУ — строку,
// которую база не приняла, потому что у изменения нет инициатора либо он не
// той формы (приёмка NTF-3, Р2, NTF3-62; kacho#2918).
//
// # Чей это дефект
//
// Колонку `initiator` журнала заполняет умолчание, читающее настройку
// транзакции, которую выставляет единственный производитель — помощник
// транзакции записи (`corelib/journaltx`). Вызывающий RPC значения не
// передаёт и исправить его не может. Поэтому оба отказа — 23502 по колонке
// `initiator` и 23514 от ограничения `<таблица>_initiator_form` — дефект записи
// сервиса, и наружу они уходят ВНУТРЕННИМ отказом с фиксированным текстом.
// Общая полоса 23514 (`pgfault.CheckLaneOf`) судит только форму имени ресурса
// и такое ограничение отнесла бы к вводу вызывающего: `INVALID_ARGUMENT`
// обвинил бы его в чужой ошибке.
//
// # Чем опознаётся
//
// Координатами, которые сервер называет сам (они же утверждены интеграционной
// пробой миграции журнала каждого модуля): имя колонки для 23502 и имя
// ограничения, равное имени таблицы с суффиксом [ConstraintSuffix], для 23514.
// Колонка `initiator` в схемах модулей есть только у таблиц журнала;
// соответствие имён миграциям держит гейт `internal/repohygiene`
// (`TestJournalInitiatorFaultIsDecidedBeforeTheCheckClass`).
package journalfault

import (
	"log/slog"

	"github.com/PRO-Robotech/corelib/db/pgfault"
)

const (
	// Column — колонка инициатора журнала модуля.
	Column = "initiator"
	// ConstraintSuffix — суффикс имени ограничения формы инициатора:
	// `<таблица журнала>_initiator_form`.
	ConstraintSuffix = "_initiator_form"
)

// Initiator сообщает, отказала ли база строке журнала по инициатору.
func Initiator(f pgfault.Fault) bool {
	if f.Table == "" {
		return false
	}
	switch f.Class {
	case pgfault.NotNull:
		return f.Column == Column
	case pgfault.Check:
		return f.Constraint == f.Table+ConstraintSuffix
	}
	return false
}

// Report — [Initiator] с записью ERROR оператору: таблица, ограничение и код,
// плюс координаты вызывающего (attrs). Значения строки (DETAIL) не пишутся.
// Близнец не пишет ничего — запись о нём принадлежит полосе вызывающего.
func Report(f pgfault.Fault, attrs ...any) bool {
	if !Initiator(f) {
		return false
	}
	slog.Error("journal refused its row by initiator: writer transaction carried no valid initiator",
		append(append([]any{}, attrs...),
			slog.String("sqlstate", f.SQLState),
			slog.String("table", f.Table),
			slog.String("column", f.Column),
			slog.String("constraint", f.Constraint))...)
	return true
}
