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

// validateIdentityPosture отказывает в старте, пока посадка не объявлена.
//
// Негодное значение до этой проверки не доходит: его отвергает разбор
// (`config.ResolvedIdentityProvider`) с именем той же ручки. Здесь судится
// ровно ОТСУТСТВИЕ ответа.
func validateIdentityPosture(provider identityposture.Provider) error {
	if provider != identityposture.Unset {
		return nil
	}
	return fmt.Errorf(
		"%s is not declared — the posture decides three wirings of this process (the "+
			"identity lane, the answer to «who am I» and the relay of the sign-in form's "+
			"verbs), and an undeclared value wires NONE of them: the edge would come up "+
			"ready with no browser sign-in at all, answering empty to «who am I» and "+
			"refusing every request that carries no presenter, without a single start-up "+
			"refusal. Declare it as one of %v (refuse to start)",
		config.IdentityProviderKnob, identityposture.Names())
}
