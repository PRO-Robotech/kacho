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
// Не всякая пара осмысленна, и правило одно, двустороннее: НА СТЕНДЕ ЧИТАЕТСЯ
// РОВНО ТО, ЧТО НА НЁМ ЧЕКАНИТСЯ. Обе его половины уже стоили бы дефекта, и
// каждая отказывает при СТАРТЕ — отказ при старте виден оператору, а перекос
// между чеканкой и чтением виден только по жалобе человека, который не смог
// войти:
//
//   - ЧИТАТЕЛЬ БЕЗ ПРОИЗВОДИТЕЛЯ: наш читатель под чужой посадкой. Печенье
//     нашей сессии чеканит наша полоса входа, а её край поднимает только под
//     `own`. Провязанный впустую, он выглядел бы включённым контролем;
//   - ПРОИЗВОДИТЕЛЬ БЕЗ ЧИТАТЕЛЯ, и это ВХОД, КОТОРЫЙ НЕ ВЕДЁТ ВНУТРЬ: посадка
//     `own` с множеством БЕЗ нашей стороны. Край поднимает нашу полосу формы
//     входа, служба чеканит `kaname_session`, и читателя у этого печенья в
//     процессе нет. Человек проходит форму, получает носитель и остаётся
//     АНОНИМОМ — отказа при этом никто не видит, потому что отказывать нечему.
//
// Третья половина о другом предмете — об адресе, без которого объявленный
// читатель не заводится:
//
//   - ЧУЖОЙ читатель ОБЪЯВЛЕН, а адреса чужой стороны нет — объявление принято
//     и не действует. Профиль назвал переходное состояние, оно молча не
//     наступило, и вход потерял ровно тот, ради кого состояние заводилось.
//
// # Почему правило спрашивает ПАРУ, а не каждую ручку по отдельности
//
// Пар при двух посадках и трёх состояниях носителя ШЕСТЬ, законных ТРИ.
// Перечень удобных пар судит только то, что в нём названо, и четвёртая —
// «вход, который не ведёт внутрь» — ровно так и прожила один круг ревью:
// правило о ней не написали, и проба её не называла. Страж поэтому формулирует
// СВОЙСТВО пары, а его проба выводит ПРОИЗВЕДЕНИЕ из словаря посадок.
//
// # Почему правило об адресе спрашивает, ОБЪЯВЛЕНО ли множество
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
	"time"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
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
	// WindowOpenedAt — момент открытия переходного окна; нулевой = не объявлен.
	WindowOpenedAt time.Time
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

	// ПРОИЗВОДИТЕЛЬ БЕЗ ЧИТАТЕЛЯ. Проверяется ПЕРВЫМ из двух половин, потому что
	// это единственная пара, на которой край поднимается, выглядит исправным и
	// отдаёт человеку носитель, который сам же не читает.
	if cfg.Posture == identityposture.Own && !cfg.Carriers.ReadsOwn() {
		return fmt.Errorf("%s=%q while %s=%q does not name %q: under the %q posture the edge raises "+
			"OUR sign-in lane and our authority mints the %q cookie — but no reader of it would be "+
			"wired. A person would pass the sign-in form, receive the carrier and stay ANONYMOUS, "+
			"and nothing would refuse: there is nothing left to refuse with. A stand reads exactly "+
			"what it mints. Add %q to the carrier set, or declare the %q posture",
			config.IdentityProviderKnob, cfg.Posture, config.SessionCarriersKnob,
			cfg.Carriers.String(), identityposture.Own, identityposture.Own,
			middleware.OurSessionCarrierName, identityposture.Own, identityposture.External)
	}

	// ЧИТАТЕЛЬ БЕЗ ПРОИЗВОДИТЕЛЯ — та же несогласованность, зеркально.
	if cfg.Carriers.ReadsOwn() && cfg.Posture != identityposture.Own {
		return fmt.Errorf("%s names %q while %s=%q: the cookie of OUR session is minted by our own "+
			"sign-in lane, and the edge raises that lane only under the %q posture. Under %q nothing "+
			"on this stand can issue it, so the reader would be wired to an input no one can produce "+
			"— a control that looks enabled and never acts. Declare the posture %q, or drop %q from "+
			"the carrier set",
			config.SessionCarriersKnob, identityposture.Own, config.IdentityProviderKnob,
			cfg.Posture, identityposture.Own, cfg.Posture, identityposture.Own, identityposture.Own)
	}

	// МОМЕНТ ОТКРЫТИЯ ОКНА — ровно там, где есть окно, и нигде больше.
	//
	// Без момента окно не имеет границы, и «дочитываем живые» превращается в
	// «принимаем любые»: отзыв на чужой полосе снимался бы входом заново, и
	// переходное состояние стало бы постоянной второй дверью.
	//
	// Обратная половина — объявление вне окна — снимается тем же правилом, и
	// поэтому оно ИСТЕКАЕТ САМО: закрыв окно и забыв убрать момент, оператор
	// получит отказ, а не переживший свой предмет остаток.
	switch {
	case cfg.Carriers.IsTransitionalWindow() && cfg.WindowOpenedAt.IsZero():
		return fmt.Errorf("%s names both sides, so %s is required: the window means the edge READS "+
			"OUT the foreign sessions that are already live and admits no new ones, and that instant "+
			"is the boundary between the two. Without it every foreign sign-in is admitted — "+
			"including one made after a revocation, which would lift the revocation",
			config.SessionCarriersKnob, config.SessionCarrierWindowOpenedAtKnob)
	case !cfg.Carriers.IsTransitionalWindow() && !cfg.WindowOpenedAt.IsZero():
		return fmt.Errorf("%s is declared while %s=%q names a single side: there is no window for it "+
			"to bound, and a declaration with nothing to bound outlives its subject silently. "+
			"Remove it, or name both sides",
			config.SessionCarrierWindowOpenedAtKnob, config.SessionCarriersKnob, cfg.Carriers.String())
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

// mustCarrierWindowOpenedAt — момент открытия окна для стража.
//
// Неразбираемое значение отдаётся НУЛЁМ намеренно: о нём отказывает разбор в
// композиционном корне, своим текстом и своей ручкой. Страж же обязан судить
// ПАРУ, и подмена здесь ошибки разбора на «не объявлено» дала бы ему второй
// текст об одном предмете.
func mustCarrierWindowOpenedAt(cfg config.Config) time.Time {
	at, err := cfg.ResolvedSessionCarrierWindowOpenedAt()
	if err != nil {
		return time.Time{}
	}
	return at
}
