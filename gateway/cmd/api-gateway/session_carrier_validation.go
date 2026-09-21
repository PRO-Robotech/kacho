// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_validation.go — страж старта: объявленное множество читателей
// носителя браузерной сессии обязано БЫТЬ ИСПОЛНИМЫМ на этой посадке.
//
// # Что чем управляется, и почему это не одна ручка
//
// Посадка личности отвечает на «ЧЬЯ ЧЕКАНКА выдаёт личность»: от неё зависят
// адреса чужого поставщика, существование полосы формы входа и обязательность
// нашего авторитета отзыва. Множество читателей отвечает на «ЧЬЁ ПЕЧЕНЬЕ край
// ещё согласен прочитать». Во время переезда ответы расходятся — в этом
// расхождении переезд и состоит, — и одной ручкой их не выразить: у неё два
// взаимоисключающих значения, а состояний требуется три.
//
// # Но разведены они НЕ до независимости
//
// Не всякая пара осмысленна, и бессмысленная обязана отказывать при СТАРТЕ:
//
//   - НАШ читатель под ЧУЖОЙ посадкой — читатель без производителя. Печенье
//     нашей сессии чеканит наша полоса входа, а её край поднимает только под
//     `own`. Провязанный впустую, он выглядел бы включённым контролем;
//   - ЧУЖОЙ читатель ОБЪЯВЛЕН, а адреса чужой стороны нет — объявление принято
//     и не действует. Профиль назвал переходное состояние, оно молча не
//     наступило, и вход потерял ровно тот, ради кого состояние заводилось.
//
// # Почему второе правило спрашивает, ОБЪЯВЛЕНО ли множество
//
// То же различие, что у приёма издателей токена (`config/tokenissuers.go`):
// «ручка не задана» — сегодняшнее, работающее и повсеместное состояние края,
// «задано» — утверждение оператора. Требовать исполнимости от утверждения
// можно; требовать её от выведенного состояния значило бы уронить профиль,
// который ничего не объявлял: посадка `external` с выключенным адресом
// поставщика сегодня поднимается и просто не заводит полосу.
package main

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// providerAddressDisabled — значение ручки адреса чужой стороны, выключающее
// полосу. Названо константой, потому что его называет отказ.
const providerAddressDisabled = "disabled"

// SessionCarrierConfig — то, что страж сверяет между собой.
type SessionCarrierConfig struct {
	// Posture — посадка личности: чья чеканка выдаёт личность.
	Posture identityposture.Provider
	// Carriers — множество читателей носителя: чьё печенье край читает.
	Carriers config.SessionCarrierSet
	// ProviderURL — адрес публичного API чужого поставщика.
	ProviderURL string
}

// validateSessionCarrierConfig отвергает пару (посадка, множество), при которой
// провязка не состоится или состоится впустую.
func validateSessionCarrierConfig(cfg SessionCarrierConfig) error {
	if !cfg.Carriers.ReadsOwn() && !cfg.Carriers.ReadsProvider() {
		// Разбор такого значения уже отказал; страж повторяет вопрос, потому что
		// он последний перед провязкой, а стенд без единого читателя поднимается
		// и выглядит исправным.
		return fmt.Errorf("%s names no side: the edge would wire no reader of the browser session "+
			"at all and answer every browser anonymously", config.SessionCarriersKnob)
	}

	if cfg.Carriers.ReadsOwn() && cfg.Posture != identityposture.Own {
		return fmt.Errorf("%s names %q while %s=%q: the cookie of OUR session is minted by our own "+
			"sign-in lane, and the edge raises that lane only under the %q posture. Under %q nothing "+
			"on this stand can issue it, so the reader would be wired to an input no one can produce "+
			"— a control that looks enabled and never acts. Declare the posture %q, or drop %q from "+
			"the carrier set",
			config.SessionCarriersKnob, identityposture.Own, config.IdentityProviderKnob,
			cfg.Posture, identityposture.Own, cfg.Posture, identityposture.Own, identityposture.Own)
	}

	if cfg.Carriers.Declared() && cfg.Carriers.ReadsProvider() {
		url := strings.TrimSpace(cfg.ProviderURL)
		if url == "" || url == providerAddressDisabled {
			return fmt.Errorf("%s names %q, but %s is %q: the declared reader has no address and "+
				"would not be wired. The transitional state exists so that a stand with live foreign "+
				"sessions does not take anyone's sign-in away — accepted and not in effect is exactly "+
				"the outcome it is meant to prevent",
				config.SessionCarriersKnob, identityposture.External,
				"KACHO_API_GATEWAY_KRATOS_PUBLIC_URL", cfg.ProviderURL)
		}
	}
	return nil
}
