// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_declared_checks_test.go — F1-28, объявительная половина на стороне
// ПОТРЕБИТЕЛЯ: состав обязательных проверок объявлен один раз, в
// `pkg/tokenpolicy`, и эта реализация им пользуется.
//
// Пока состав живёт у каждой поверхности свой, различие между поверхностями не
// выражено и потому не может покраснеть: одна перестанет требовать срок, другая
// тип, и об этом не узнает никто.
package jwks

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/stretchr/testify/require"
)

// TestF1_28_VerifierDeclaresTheWholeMandatorySet — F1-28.
//
// Утверждается ПАРА, и вторая половина несущая: у полностью провязанного
// проверяющего недостающих проверок нет, а у провязанного без чтения отзыва —
// ровно отзыв и назван. Без второй половины «пусто» верно и для объявления,
// которое просто перечисляет весь перечень, ничего не исполняя.
func TestF1_28_VerifierDeclaresTheWholeMandatorySet(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	a := newAuthority(t)
	at := time.Now()

	full := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))
	require.Nil(t, tokenpolicy.MissingChecks(full.DeclaredChecks()),
		"полностью провязанный проверяющий обязан исполнять ВЕСЬ обязательный перечень: %v",
		tokenpolicy.MandatoryChecks())

	withoutRevocation := newVerifier(t, ourPair(ks))
	require.Equal(t,
		[]tokenpolicy.Check{tokenpolicy.CheckRevocation},
		tokenpolicy.MissingChecks(withoutRevocation.DeclaredChecks()),
		"объявление ПРАВДИВО: без читателя отзыва проверяющий отзыв не исполняет и не объявляет")
}

// TestF1_28_DeclarationHasNoDuplicatesAndNoStrangers — объявление сверх
// перечня обязано быть объяснимым: молчаливое расхождение — находка. Здесь
// утверждается более узкое и машинно проверяемое: в объявлении нет ни повторов,
// ни проверок, которых перечень политики не знает.
func TestF1_28_DeclarationHasNoDuplicatesAndNoStrangers(t *testing.T) {
	ks := newKeySet(t)
	a := newAuthority(t)
	at := time.Now()
	v := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))

	known := map[tokenpolicy.Check]bool{}
	for _, c := range tokenpolicy.MandatoryChecks() {
		known[c] = true
	}
	seen := map[tokenpolicy.Check]bool{}
	for _, c := range v.DeclaredChecks() {
		require.Falsef(t, seen[c], "проверка %q объявлена дважды", c)
		seen[c] = true
		require.Truef(t, known[c],
			"проверка %q объявлена сверх перечня политики и без причины — молчаливое расхождение является находкой", c)
	}
}

// TestF1_28_PolicyNumbersAreTheOnesThisSurfaceUses — числа, из которых
// вычисляется отсрочка снятия ключа, берутся из объявленной политики, а не
// заводятся здесь.
//
// Пока каждая сторона объявляет своё число, арифметика отсрочки невыразима:
// её нечем проверить, и она угадывается.
func TestF1_28_PolicyNumbersAreTheOnesThisSurfaceUses(t *testing.T) {
	require.Equal(t, tokenpolicy.ConsumerKeySetCacheCeiling, maxTTL,
		"потолок срока годности снимка — второе слагаемое арифметики отсрочки; источник у него один")
	require.Equal(t, tokenpolicy.UnknownKeyIDRefetchInterval, defaultMinRefresh,
		"минимальный интервал вынужденного перезапроса объявлен политикой")
	require.Equal(t, tokenpolicy.ClockSkew, New1Skew(t),
		"допуск на расхождение часов объявлен политикой")

	r, err := NewIntrospectionReader("http://127.0.0.1:9097/internal/tokens/introspect", RevocationTransport{})
	require.NoError(t, err)
	require.LessOrEqual(t, r.Window(), authz.RevocationPolicy.Ceiling,
		"окно отзыва укладывается в объявленный потолок политики доступа")
}

// New1Skew возвращает допуск построенного проверяющего — предмет утверждения
// выше. Через конструктор, а не через константу пакета: проба обязана читать то
// значение, которым проверяющий пользуется, а не то, которое рядом объявлено.
func New1Skew(t *testing.T) time.Duration {
	t.Helper()
	ks := newKeySet(t)
	return newVerifier(t, ourPair(ks)).skew
}
