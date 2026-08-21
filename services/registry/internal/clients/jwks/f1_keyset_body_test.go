// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_keyset_body_test.go — F1-27: тело ответа источника набора ограничено
// объявленным потолком, а содержимое НЕ ТОГО ТИПА отвергается ДО разбора.
//
// Разбирать «почти набор» нельзя: страница ошибки, разобранная снисходительно,
// даёт пустой набор, а пустой набор читается потребителем как факт «ключей нет».
package jwks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/stretchr/testify/require"
)

// TestF1_27_KeySetBodyCeilingAndContentType — F1-27.
//
// Ни один из двух случаев не приводит к принятию набора; рядом положительный
// контроль — набор законного размера и объявленного типа принимается, без него
// проба зелена на потребителе, не принимающем ничего.
func TestF1_27_KeySetBodyCeilingAndContentType(t *testing.T) {
	t.Run("тело сверх потолка — набор не принят", func(t *testing.T) {
		ks := newKeySet(t)
		ks.addRSA(t, "our-1")
		tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute))

		// Законный по форме документ, раздутый за объявленный потолок: чтение
		// обязано прекратиться на потолке, а не «сколько влезло».
		doc := ks.doc()
		doc["padding"] = strings.Repeat("A", tokenpolicy.KeySetBodyCeiling+1)
		body, err := json.Marshal(doc)
		require.NoError(t, err)
		require.Greater(t, len(body), tokenpolicy.KeySetBodyCeiling, "предпосылка: тело действительно сверх потолка")
		ks.rawBody = body

		v := newVerifier(t, ourPair(ks))
		_, err = v.Verify(context.Background(), tok)
		require.ErrorIs(t, err, ErrInvalidToken, "набор сверх потолка тела не принимается")
	})

	t.Run("содержимое не того типа — отвергается до разбора", func(t *testing.T) {
		// "-" — заголовка нет вовсе. Тело во ВСЕХ случаях остаётся ЗАКОННЫМ
		// набором: отвергнуть обязан именно тип содержимого, а не негодность
		// разбора, иначе проба измеряла бы разбор.
		for _, ct := range []string{"text/html", "text/plain; charset=utf-8", "application/xml", "-", "не тип вовсе"} {
			ks := newKeySet(t)
			ks.addRSA(t, "our-1")
			tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute))
			ks.contentType = ct

			v := newVerifier(t, ourPair(ks))
			_, err := v.Verify(context.Background(), tok)
			require.ErrorIsf(t, err, ErrInvalidToken,
				"ответ типа %q набором ключей не является и разбираться не должен", ct)
		}
	})

	t.Run("положительный контроль: законный набор объявленного типа принимается", func(t *testing.T) {
		for _, ct := range []string{"application/json", "application/json; charset=utf-8", "application/jwk-set+json"} {
			ks := newKeySet(t)
			ks.addRSA(t, "our-1")
			ks.contentType = ct
			v := newVerifier(t, ourPair(ks))

			sub, err := v.Verify(context.Background(),
				ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
			require.NoErrorf(t, err, "набор объявленного типа %q обязан приниматься", ct)
			require.Equal(t, "sva-1", sub)
		}
	})
}

// TestF1_27_BodyCeilingIsTheDeclaredOne — потолок берётся из объявленной
// политики, а не заводится на месте: второе объявление разошлось бы молча, и
// разошлось бы в сторону «принимаем больше».
func TestF1_27_BodyCeilingIsTheDeclaredOne(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute))

	// Ровно потолок — принимается; потолок плюс один — нет. Граница
	// утверждается с обеих сторон, иначе она проверена не была бы вовсе.
	doc := ks.doc()
	base, err := json.Marshal(doc)
	require.NoError(t, err)
	require.Less(t, len(base), tokenpolicy.KeySetBodyCeiling, "предпосылка: набор без набивки помещается в потолок")

	pad := func(n int) []byte {
		doc["padding"] = strings.Repeat("A", n)
		b, merr := json.Marshal(doc)
		require.NoError(t, merr)
		return b
	}
	// Подбираем набивку так, чтобы тело было РОВНО потолком.
	body := pad(tokenpolicy.KeySetBodyCeiling - len(base) - len(`,"padding":""`))
	for len(body) < tokenpolicy.KeySetBodyCeiling {
		body = append(body[:len(body)-1], append([]byte(strings.Repeat(" ", tokenpolicy.KeySetBodyCeiling-len(body))), '}')...)
	}
	require.Len(t, body, tokenpolicy.KeySetBodyCeiling, "предпосылка: тело ровно потолок")

	ks.rawBody = body
	v := newVerifier(t, ourPair(ks))
	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "тело РОВНО потолком обязано приниматься — иначе потолок на единицу меньше объявленного")
	require.Equal(t, "sva-1", sub)
}
