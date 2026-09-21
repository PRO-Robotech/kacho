// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sessioncarriers.go — разбор ОБЪЯВЛЕНИЯ о том, ЧЬЁ ПЕЧЕНЬЕ край читает:
// множество читателей носителя браузерной сессии.
//
// # Почему множество, а не значение посадки
//
// Читателя носителя выбирала посадка личности
// (`KACHO_API_GATEWAY_IDENTITY_PROVIDER`), у которой два взаимоисключающих
// значения. Состояния «наш носитель читается, и чужой ЕЩЁ читается» в такой
// форме не существует, и следствие наблюдаемо: перевод стенда с людьми со
// чужой чеканки на нашу АТОМАРЕН — в момент правки профиля каждый, чья чужая
// сессия жива, теряет вход, потому что читателя её печенья в новом процессе
// не остаётся.
//
// У приёма ТОКЕНА этого дефекта нет: издателей край принимает множеством
// (`tokenissuers.go`), и переходное состояние профиль выражает с первого дня.
// Носитель получает ту же форму, потому что предмет тот же — переезд между
// двумя источниками личности.
//
// # Ручка РАЗВЕДЕНА с посадкой, и это ответ на «что чем управляется»
//
// Посадка отвечает на «чья чеканка выдаёт личность» и продолжает решать всё
// остальное: нужны ли адреса чужого поставщика, существует ли полоса формы
// входа, обязателен ли наш авторитет отзыва. Она НЕ отвечает на «чьё печенье
// мы ещё согласны прочитать», потому что во время переезда ответы на эти два
// вопроса расходятся — ровно в этом расхождении переезд и состоит.
//
// # Порядок В МНОЖЕСТВЕ НЕПРЕДСТАВИМ — намеренно
//
// Кто выигрывает при двух предъявленных носителях есть решение о том, ЧЬЯ
// ЛИЧНОСТЬ ДЕЙСТВУЕТ. Оператору оно не принадлежит: перечень позволил бы
// переставить стороны местами и сделал бы старшинство величиной профиля.
// Множество отвечает только на «читаем ли мы эту сторону»; старшинство стоит
// в коде и держится `middleware/session_carrier_precedence_test.go`.
//
// # Три состояния, а не два — и четвёртого нет
//
//   - `external` — только чужой: сегодняшний стенд до переезда;
//   - `own,external` — оба: наш носитель уже читается, чужой ещё читается;
//   - `own` — только наш: состояние, к которому платформа приходит.
//
// Пустое множество («объявлено и даёт ноль элементов») состоянием НЕ является
// и отвергается: стенд без единого читателя поднимается, выглядит исправным и
// отвечает анонимом каждому браузеру. Отказ виден оператору сразу, а вход,
// пропавший молча, — только по жалобе человека.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/PRO-Robotech/corelib/identityposture"
)

// SessionCarriersKnob — имя ручки множества читателей носителя. Объявлено один
// раз: его называют текст отказа разбора, страж старта и профиль.
const SessionCarriersKnob = "KACHO_API_GATEWAY_SESSION_CARRIERS"

// SessionCarrierWindowOpenedAtKnob — момент ОТКРЫТИЯ переходного окна носителя,
// RFC 3339. Объявляется вместе с состоянием «оба» и только с ним.
const SessionCarrierWindowOpenedAtKnob = "KACHO_API_GATEWAY_SESSION_CARRIER_WINDOW_OPENED_AT"

// SessionCarrierSet — МНОЖЕСТВО читателей носителя браузерной сессии.
//
// Поля неэкспортируемые, и это не косметика: сравнение двух значений на
// равенство обязано означать «одно и то же состояние», а собрать значение
// иначе, чем разбором объявления, нечем.
type SessionCarrierSet struct {
	own      bool
	provider bool
	// declared — НАЗВАЛ ли множество профиль, или оно выведено из посадки.
	//
	// Различие несущее, и оно то же, что у приёма издателей токена: «ручка не
	// задана» — сегодняшнее, работающее и повсеместное состояние, а «задано» —
	// утверждение оператора. Требовать от утверждения исполнимости можно, от
	// сегодняшнего состояния — значит сломать профиль, который ничего не
	// объявлял и ни в чём не виноват.
	declared bool
}

// Declared — назвал ли множество профиль (в отличие от выведенного из посадки).
func (s SessionCarrierSet) Declared() bool { return s.declared }

// ReadsOwn — читает ли край НАШ носитель (`kaname_session`).
func (s SessionCarrierSet) ReadsOwn() bool { return s.own }

// ReadsProvider — читает ли край носитель ЧУЖОГО поставщика
// (`ory_kratos_session`).
func (s SessionCarrierSet) ReadsProvider() bool { return s.provider }

// String — состояние словами, для самоотчёта при старте. Имена сторон берутся
// из ОБЩЕГО словаря посадки: второй словарь разошёлся бы с первым молча.
func (s SessionCarrierSet) String() string {
	var names []string
	if s.own {
		names = append(names, identityposture.Own.String())
	}
	if s.provider {
		names = append(names, identityposture.External.String())
	}
	if len(names) == 0 {
		return "<ни одного>"
	}
	return strings.Join(names, ",")
}

// ResolvedSessionCarriers разбирает объявленное множество читателей.
//
// Ручка НЕ ОБЪЯВЛЕНА — состояние выводится из посадки ровно так, как край вёл
// себя до заведения ручки: `own` даёт наш носитель, `external` — чужой. Ни один
// существующий профиль от появления ручки не меняет поведения, и умолчания в
// коде у самого множества поэтому нет — оно наследует умолчание посадки, у
// которой его тоже нет намеренно.
func (c Config) ResolvedSessionCarriers() (SessionCarrierSet, error) {
	raw := strings.TrimSpace(c.SessionCarriers)
	if c.SessionCarriers == "" {
		return c.sessionCarriersFromPosture()
	}
	return c.sessionCarriersFromDeclaration(raw)
}

// sessionCarriersFromPosture — сегодняшнее поведение, выведенное из посадки.
func (c Config) sessionCarriersFromPosture() (SessionCarrierSet, error) {
	provider, err := c.ResolvedIdentityProvider()
	if err != nil {
		// Обёртка называет, ЧТО спрашивали: отказ разбора посадки, пришедший
		// голым, читается как отказ самой посадки, и оператор правит её, не
		// зная, что вопрос задало множество читателей, выводящее из неё себя.
		return SessionCarrierSet{}, fmt.Errorf(
			"%s is not declared, so the browser-session carrier readers are derived from %s: %w",
			SessionCarriersKnob, IdentityProviderKnob, err)
	}
	switch provider {
	case identityposture.Own:
		return SessionCarrierSet{own: true}, nil
	case identityposture.External:
		return SessionCarrierSet{provider: true}, nil
	}
	// Выводить не из чего. Отказ называет ОБЕ ручки: оператору решать, какую
	// объявить, и отказ, назвавший одну, отправил бы его править не ту.
	return SessionCarrierSet{}, fmt.Errorf(
		"%s is not declared and %s is not declared either — there is nothing to derive the "+
			"browser-session carrier readers from. Declare the posture (the set then follows it, "+
			"as before), or declare the set explicitly (legal elements, verbatim: %s). A process "+
			"that wires no carrier reader still boots and answers every browser anonymously, "+
			"which is indistinguishable from a healthy stand",
		SessionCarriersKnob, IdentityProviderKnob, strings.Join(identityposture.Names(), ", "))
}

// sessionCarriersFromDeclaration — разбор объявленного значения.
//
// Считаются ЭЛЕМЕНТЫ, а не длина строки: значение «,» непусто как строка и
// пусто как множество, и именно на таком входе предикат по длине молчит.
func (c Config) sessionCarriersFromDeclaration(raw string) (SessionCarrierSet, error) {
	out := SessionCarrierSet{declared: true}
	seen := map[string]bool{}
	elements := 0
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		// Словарь ОБЩИЙ с посадкой: стороны носителя и значения посадки — одни и
		// те же две стороны, названные одними и теми же именами. Второй разбор
		// тех же имён разошёлся бы с первым на первом же новом значении.
		//
		// Опознаёт чужой разбор, а ОБЪЯСНЯЕТ свой: текст отказа посадки говорит
		// про адреса чужого поставщика, а здесь предмет другой — чьё печенье
		// читается. Отказ, объясняющий не тот предмет, отправляет оператора
		// править не ту ручку.
		side, perr := identityposture.Parse(SessionCarriersKnob, name)
		if perr != nil {
			return SessionCarrierSet{}, fmt.Errorf(
				"%s names %q, which is not a side of the browser-session carrier "+
					"(legal elements, verbatim: %s). Separating whitespace around an element is "+
					"trimmed — the value is a list, and «own, external» is one list written with a "+
					"space — but nothing else is forgiven: case is not folded, and a look-alike "+
					"letter from another alphabet is not the same letter. This knob decides WHOSE "+
					"cookie the edge still reads, so an unrecognised element is refused rather than "+
					"dropped: a dropped element silently narrows the set and takes someone's "+
					"sign-in away",
				SessionCarriersKnob, name, strings.Join(identityposture.Names(), ", "))
		}
		if seen[name] {
			return SessionCarrierSet{}, fmt.Errorf(
				"%s names side %q twice — one side, one record", SessionCarriersKnob, name)
		}
		seen[name] = true
		elements++
		switch side {
		case identityposture.Own:
			out.own = true
		case identityposture.External:
			out.provider = true
		}
	}
	if elements == 0 {
		return SessionCarrierSet{}, fmt.Errorf(
			"%s declares no carrier element (value %q has %d characters and %d elements); "+
				"a declared set that names no side leaves the edge with no reader of the browser "+
				"session at all — every browser is answered anonymously and the stand still looks "+
				"healthy. Legal elements, verbatim: %s",
			SessionCarriersKnob, c.SessionCarriers, len(c.SessionCarriers), elements,
			strings.Join(identityposture.Names(), ", "))
	}
	return out, nil
}

// IsTransitionalWindow — названы ли ОБЕ стороны, то есть открыто ли переходное
// окно. Вопрос задаётся в одном месте: два его вычисления разошлись бы молча.
func (s SessionCarrierSet) IsTransitionalWindow() bool { return s.own && s.provider }

// ResolvedSessionCarrierWindowOpenedAt разбирает момент открытия окна.
//
// Момент несёт ОБА следствия окна (граница приёма чужой сессии и пол второго
// фактора), и разбор у него один: второе прочтение той же величины разошлось бы
// с первым на первом же вырожденном значении.
//
// Незаданное значение возвращается НУЛЁМ без ошибки: обязательность решает
// страж старта, который видит ещё и множество. Отказ здесь назвал бы то же
// самое вторым текстом и не смог бы назвать, ПОЧЕМУ момент обязателен.
func (c Config) ResolvedSessionCarrierWindowOpenedAt() (time.Time, error) {
	raw := strings.TrimSpace(c.SessionCarrierWindowOpenedAt)
	if raw == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s=%q is not an RFC 3339 instant (%w). This instant is the boundary between the "+
				"foreign sessions the edge reads out and the ones it refuses to admit, so it is "+
				"refused rather than read as «no boundary»: an unparsed boundary would admit every "+
				"foreign sign-in, including the ones made after a revocation",
			SessionCarrierWindowOpenedAtKnob, c.SessionCarrierWindowOpenedAt, err)
	}
	return at, nil
}
