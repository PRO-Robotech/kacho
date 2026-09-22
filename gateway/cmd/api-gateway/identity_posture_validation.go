// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_validation.go — страж старта ПОСАДКИ ЛИЧНОСТИ на крае.
//
// # Предмет
//
// Посадка — ответ на вопрос «чем стенд проверяет, что перед платформой именно
// этот человек». Край разводит по ней ТРИ места провязки композиционного корня:
// полосу личности (читатель носителя браузерной сессии), ответ «кто я» и
// ретрансляцию глаголов формы на слушатель службы доступа. Незаданное значение
// — не «безопасное умолчание», а отсутствие ответа: `identityposture.Unset` в
// словарь законных значений не входит.
//
// # Что наблюдалось, пока стража не было
//
// Разрешение посадки возвращает «не задано» БЕЗ ошибки, а «не задано» — это не
// `own`, поэтому все три места молча оставались непровязанными. Край стартовал,
// под становился готовым, а браузерной полосы личности не существовало ни
// одной: ответ «кто я» всегда пуст, глаголы формы не смонтированы и уходят в
// отказ по отсутствию, запрос без предъявителя отвергается. Войти нельзя ни при
// каком вводе — и НИ ОДНОГО отказа старта при этом не произносится.
//
// # Почему отказ БЕЗУСЛОВЕН
//
// Соседний страж полосы отзыва терпит послабление под явными метками
// разработки: на местном стенде может не быть достижимого авторитета. Здесь
// послабление означало бы «на стенде разработки посадку не выбираем» — то есть
// ровно то состояние, ради снятия которого страж и заведён. Образец тот же, что
// у необъявленного перечня издателей: отказ не зависит от метки окружения.
//
// # Почему страж не отменяет гейта ПРОФИЛЯ
//
// Он умеет сказать «не задано». Он НЕ умеет сказать «задано ПРЕЖНЕЕ, а прежнего
// больше нет»: `external` для него — законное значение, и стенд, унаследовавший
// его от умолчания подчарта, поднимется как настроенный. Вторую половину держит
// `gateway/deploy/identity_posture_reaches_every_stand_test.go`: посадку
// объявляет КАЖДЫЙ стенд, а умолчания у чарта края нет.
package main

import (
	"fmt"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// wiredIdentityPostures — посадки, под которые у ЭТОГО процесса есть провязка.
//
// Словарь фундамента шире: посадку читают ДВА процесса, и `external` служба
// прав исполняет. Сужается здесь то, что принимает КРАЙ, — и сужается до того,
// что корень действительно строит. Совпадение множества с провязками корня
// производится ДЕРЕВОМ, а не обещанием:
// `TestGuardAcceptsExactlyWhatTheCompositionRootWires` выводит его разбором
// достижимого от `main()` кода и краснеет, если корень начал ветвиться по
// значению, которого здесь нет, — или перестал ветвиться по тому, которое есть.
func wiredIdentityPostures() []identityposture.Provider {
	return []identityposture.Provider{identityposture.Own}
}

// wiredIdentityPostureNames — то же множество словами, для текста отказа.
func wiredIdentityPostureNames() []string {
	wired := wiredIdentityPostures()
	out := make([]string, 0, len(wired))
	for _, p := range wired {
		out = append(out, p.String())
	}
	return out
}

// validateIdentityPosture отказывает в старте, пока посадка не объявлена.
//
// Негодное значение до этой проверки не доходит: его отвергает разбор
// (`config.ResolvedIdentityProvider`) с именем той же ручки. Здесь судится
// ровно ОТСУТСТВИЕ ответа.
func validateIdentityPosture(provider identityposture.Provider) error {
	if provider == identityposture.Unset {
		return fmt.Errorf(
			"%s is not declared — the posture decides three wirings of this process (the "+
				"identity lane, the answer to «who am I» and the relay of the sign-in form's "+
				"verbs), and an undeclared value wires NONE of them: the edge would come up "+
				"ready with no browser sign-in at all, answering empty to «who am I» and "+
				"refusing every request that carries no presenter, without a single start-up "+
				"refusal. Declare it as one of %v (refuse to start)",
			config.IdentityProviderKnob, wiredIdentityPostureNames())
	}
	for _, wired := range wiredIdentityPostures() {
		if provider == wired {
			return nil
		}
	}
	// ВТОРОЙ отказ, а не тот же. «Не объявлено» чинится объявлением;
	// «объявлено то, чего этот процесс не исполняет» — выбором другого значения
	// либо провязкой. Слитые в один, они предлагали бы чинить не то.
	return fmt.Errorf(
		"%s=%s is declared, but this process wires NOTHING under it: the posture decides "+
			"three wirings of the edge (the identity lane, the answer to «who am I» and the "+
			"relay of the sign-in form's verbs), and under %s not one of the three is "+
			"constructed — the edge would come up READY with no browser sign-in at all, "+
			"answering empty to «who am I» and refusing every request that carries no "+
			"presenter, and it would say nothing about it. The value stays lawful in the "+
			"shared vocabulary for the processes that DO implement it (the identity service "+
			"is one); this edge accepts only %v. Declare one of them (refuse to start)",
		config.IdentityProviderKnob, provider, provider, wiredIdentityPostureNames())
}
