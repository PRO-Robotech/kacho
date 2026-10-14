// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// callbudget.go — бюджеты вызовов края к соседям (приёмка KA1, Р4; kacho#2713,
// kacho#2738).
//
// # Предмет
//
// На пути запроса край ждёт двух соседей: внутренний слушатель службы доступа
// (вопрос о сессии, об отсечке, об отзыве по идентификатору, о годности
// базового удостоверения, отзыв сессий при выходе) и бэкенд домена за
// REST-мостом. Пока ожидание кончалось ничем, отсутствие предела ничего не
// стоило; когда на другом конце ожидания стоит отказ, величина, ограничивающая
// его, решает судьбу запроса — и обязана быть объявлена, прочитана и судима.
// Иначе зависший (не молчащий, а именно зависший) сосед держит запрос ровно
// столько, сколько его держит клиент.
//
// # Умолчания нет
//
// Величину объявляет профиль. Ручка, которой профиль не назвал, — отказ в старте
// в ЛЮБОЙ посадке: умолчание было бы величиной, которую никто не выбирал и
// которую поэтому некому обсуждать.
//
// # Три состояния, два текста отказа
//
// Не объявлено (переменной нет) и объявлено, но не годится (пусто, не
// разбирается, ноль, меньше нуля) лечатся по-разному — добавить объявление либо
// починить форму объявленного, — и отказ их различает. Поэтому объявленность
// спрашивается у окружения (`LookupEnv`), а не выводится из пустой строки.

import (
	"errors"
	"fmt"
	"time"
)

const (
	// KnobIdentityCallBudget — бюджет КАЖДОГО вызова края к внутреннему
	// слушателю службы доступа на пути запроса и отзыва сессий при выходе.
	KnobIdentityCallBudget = "KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET"
	// KnobBackendCallBudget — бюджет КАЖДОГО унарного вызова REST-моста к бэкенду
	// домена.
	KnobBackendCallBudget = "KACHO_API_GATEWAY_BACKEND_CALL_BUDGET"
)

// ParseCallBudget судит значение ручки бюджета: declared — объявлена ли
// переменная вообще, raw — её значение как есть. Форма значения — длительность Go
// (`1s`, `250ms`); годно только строго положительное.
func ParseCallBudget(knob, raw string, declared bool) (time.Duration, error) {
	if !declared {
		return 0, fmt.Errorf("%s не объявлено: бюджет вызова края к соседу задаёт профиль, "+
			"умолчания в коде нет — объявите длительность Go (например 1s) (отказ в старте)", knob)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s объявлено, но не годится: %q не разбирается как длительность Go "+
			"(например 1s, 250ms) (отказ в старте)", knob, raw)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s объявлено, но не годится: %q — бюджет обязан быть больше нуля: "+
			"нулевой либо отрицательный срок отказывал бы каждому вызову (отказ в старте)", knob, raw)
	}
	return d, nil
}

// loadCallBudgets — обе ручки сразу: отказ называет каждую негодную, а не
// первую, — иначе оператор чинил бы их по одной за перезапуск. Значение берётся
// из поля, заполненного загрузчиком; объявленность — у окружения.
func loadCallBudgets(cfg *Config, lookup func(string) (string, bool)) error {
	declared := func(k string) bool { _, ok := lookup(k); return ok }
	var errs []error
	var err error
	if cfg.IdentityCallBudget, err = ParseCallBudget(KnobIdentityCallBudget,
		cfg.IdentityCallBudgetRaw, declared(KnobIdentityCallBudget)); err != nil {
		errs = append(errs, err)
	}
	if cfg.BackendCallBudget, err = ParseCallBudget(KnobBackendCallBudget,
		cfg.BackendCallBudgetRaw, declared(KnobBackendCallBudget)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
