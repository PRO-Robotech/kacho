// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_token_checks_test.go — проверки предъявленного токена, каждая из которых
// невидима на положительном пути: токен выпускается, проверяется, запрос
// проходит — и при снятой проверке всё выглядит ровно так же.
//
// У каждого отрицания здесь стоит ПОЛОЖИТЕЛЬНЫЙ контроль в той же пробе:
// набор утверждений, целиком состоящий из ожиданий отказа, зеленеет на
// проверяющем, отвергающем всё, — то есть не отличает исправную защиту от
// сломанного приёма.
package jwks

import (
	"context"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/stretchr/testify/require"
)

// TestF1_13_TokenTypeIsRequiredAndPinned — F1-13.
//
// Тип объявлен и равен ожидаемому для записи издателя. Три утверждения и
// положительный контроль: без последнего «чужой тип отвергнут» и «типа нет —
// отвергнут» верны и на проверяющем, который не принимает ничего.
func TestF1_13_TokenTypeIsRequiredAndPinned(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	v := newVerifier(t, ourPair(ks))
	now := time.Now()

	t.Run("ожидаемый тип принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", now, time.Minute)))
		require.NoError(t, err, "положительный контроль: токен объявленного типа обязан приниматься")
		require.Equal(t, "sva-1", sub)
	})

	t.Run("чужой тип отвергается", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", typJWT, platformClaims("sva-1", now, time.Minute)))
		require.ErrorIs(t, err, ErrInvalidToken,
			"тип другого контура на этой поверхности не принимается: один подписант, обслуживающий два контура, делает путаницу настоящей возможностью")
	})

	t.Run("отсутствие типа отвергается", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", "", platformClaims("sva-1", now, time.Minute)))
		require.ErrorIs(t, err, ErrInvalidToken,
			"тип ОБЯЗАТЕЛЕН: разбор, не встретив типа, сам не возразит")
	})
}

// TestF1_17_SurfaceRejectsForeignTokensAndAcceptsItsOwn — F1-17.
//
// Три отрицания и одно принятие в ОДНОЙ пробе: подменённая подпись · неизвестный
// издатель · законный токен ЧУЖОГО адресата. Отказ по адресату назван отдельно —
// это самостоятельная возможность, появляющаяся ровно тогда, когда один
// подписант обслуживает два контура.
func TestF1_17_SurfaceRejectsForeignTokensAndAcceptsItsOwn(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	v := newVerifier(t, ourPair(ks))
	now := time.Now()

	t.Run("законный токен ЭТОЙ поверхности принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", now, time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})

	t.Run("подменённая подпись отвергается", func(t *testing.T) {
		tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", now, time.Minute))
		repl := "AA"
		if tok[len(tok)-2:] == repl {
			repl = "BB"
		}
		tampered := tok[:len(tok)-2] + repl
		require.NotEqual(t, tok, tampered, "предпосылка: подменённый токен обязан отличаться от исходного")
		_, err := v.Verify(context.Background(), tampered)
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("неизвестный издатель отвергается", func(t *testing.T) {
		claims := platformClaims("sva-1", now, time.Minute)
		claims["iss"] = "https://someone-else.example"
		_, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("законный токен ДРУГОГО адресата отвергается", func(t *testing.T) {
		claims := platformClaims("sva-1", now, time.Minute)
		claims["aud"] = "some-other-service"
		_, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.ErrorIs(t, err, ErrInvalidToken,
			"адресат чужого контура: токен законен, но адресован не сюда")
	})
}

// TestF1_18_AlgorithmDictionaryIsClosedAndCheckedBeforeKeyResolution — F1-18.
//
// Алгоритм вне закрытого словаря (включая «без подписи») отвергается ДО
// разрешения ключа: это утверждается счётчиком обращений к источнику, а не
// прочтением порядка строк. Положительный контроль — законный токен той же
// формы; без него отрицание зелено на проверяющем, отвергающем всё.
func TestF1_18_AlgorithmDictionaryIsClosedAndCheckedBeforeKeyResolution(t *testing.T) {
	for _, alg := range []string{"none", "HS256", "RS512", ""} {
		t.Run("вне словаря: "+alg, func(t *testing.T) {
			ks := newKeySet(t)
			ks.addRSA(t, "our-1")
			v := newVerifier(t, ourPair(ks))

			tok := unsigned(alg, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute))
			_, err := v.Verify(context.Background(), tok)
			require.ErrorIs(t, err, ErrInvalidToken)
			require.Zerof(t, ks.fetches.Load(),
				"алгоритм %q вне словаря обязан отвергаться ДО разрешения ключа — обращение к источнику за него не платится", alg)
		})
	}

	t.Run("положительный контроль: законный токен той же формы", func(t *testing.T) {
		ks := newKeySet(t)
		ks.addRSA(t, "our-1")
		v := newVerifier(t, ourPair(ks))
		sub, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})
}

// TestF1_18_EveryDeclaredAlgorithmVerifies — вторая половина F1-18: словарь ОДИН.
//
// Алгоритм, объявленный политикой, обязан проверяться этой поверхностью. Иначе
// «закрытый словарь» существует только в политике, а на поверхности он свой, и
// расхождение между ними не выражено — то есть не может покраснеть.
func TestF1_18_EveryDeclaredAlgorithmVerifies(t *testing.T) {
	for _, alg := range tokenpolicy.Algorithms() {
		t.Run(alg, func(t *testing.T) {
			ks := newKeySet(t)
			v := newVerifier(t, ourPair(ks))
			claims := platformClaims("sva-1", time.Now(), time.Minute)

			var tok string
			switch alg {
			case tokenpolicy.AlgRS256:
				ks.addRSA(t, "k")
				tok = ks.mintRS(t, "k", typAccessJWT, claims)
			case tokenpolicy.AlgES256:
				ks.addEC(t, "k")
				tok = ks.mintES(t, "k", typAccessJWT, claims)
			case tokenpolicy.AlgEdDSA:
				ks.addEd(t, "k")
				tok = ks.mintEd(t, "k", typAccessJWT, claims)
			default:
				t.Fatalf("словарь политики назвал алгоритм %q, для которого у пробы нет чеканки — "+
					"проба обязана расти вместе со словарём, иначе новый алгоритм остаётся непроверенным", alg)
			}

			sub, err := v.Verify(context.Background(), tok)
			require.NoErrorf(t, err, "алгоритм %q объявлен словарём платформы и обязан приниматься", alg)
			require.Equal(t, "sva-1", sub)
		})
	}
}

// TestF1_19_ExpiryIsRequiredExplicitly — F1-19.
//
// Пара доказывает, что отвергается именно ОТСУТСТВИЕ срока: тот же токен со
// сроком в будущем принимается. Проба, подающая только токен со сроком, это
// свойство не измеряет вовсе.
func TestF1_19_ExpiryIsRequiredExplicitly(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	v := newVerifier(t, ourPair(ks))
	now := time.Now()

	withExp := platformClaims("sva-1", now, time.Minute)
	sub, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, withExp))
	require.NoError(t, err, "положительный контроль: со сроком в будущем токен принимается")
	require.Equal(t, "sva-1", sub)

	noExp := platformClaims("sva-1", now, time.Minute)
	delete(noExp, "exp")
	_, err = v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, noExp))
	require.ErrorIs(t, err, ErrInvalidToken,
		"токен без срока живёт вечно, и на положительном пути это не видно ничем")
}

// TestF1_24_ClockSkewAllowanceHoldsOnBothSides — F1-24.
//
// Допуск объявлен числом (`tokenpolicy.ClockSkew`) и действует в обе стороны:
// за пределом — отказ, ВНУТРИ — проход. Без второй половины проба измеряла бы
// не допуск, а его отсутствие.
func TestF1_24_ClockSkewAllowanceHoldsOnBothSides(t *testing.T) {
	skew := tokenpolicy.ClockSkew
	require.Positive(t, skew, "предпосылка: допуск объявлен числом")

	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	v := newVerifier(t, ourPair(ks))
	now := time.Now()
	at := now
	atClock(v, &at)

	inside := skew / 2
	outside := skew * 3

	t.Run("срок истёк ВНУТРИ допуска — принимается", func(t *testing.T) {
		claims := platformClaims("sva-1", now.Add(-time.Hour), time.Hour-inside)
		sub, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})

	t.Run("срок истёк ЗА пределом допуска — отвергается", func(t *testing.T) {
		claims := platformClaims("sva-1", now.Add(-time.Hour), time.Hour-outside)
		_, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("выпущен из будущего ВНУТРИ допуска — принимается", func(t *testing.T) {
		claims := platformClaims("sva-1", now.Add(inside), time.Hour)
		sub, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})

	t.Run("выпущен из будущего ЗА пределом допуска — отвергается", func(t *testing.T) {
		claims := platformClaims("sva-1", now.Add(outside), time.Hour)
		_, err := v.Verify(context.Background(), ks.mintRS(t, "our-1", typAccessJWT, claims))
		require.ErrorIs(t, err, ErrInvalidToken)
	})
}

// TestF1_26_UnknownKeyInTheSetIsSkippedAndTheSetIsAccepted — F1-26.
//
// Это НАМЕРЕННО противоположно правилу публикатора («не можешь отдать набор
// целиком — не отдавай ничего»), и предметы у сторон разные: там неполнота
// выдаётся за полноту, здесь один незнакомый ключ — новый вид, будущий
// алгоритм — обвалил бы проверку ВСЕХ токенов сразу.
func TestF1_26_UnknownKeyInTheSetIsSkippedAndTheSetIsAccepted(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	ks.extra = []map[string]any{
		{"kty": "FUTURE-KTY", "kid": "k-future", "alg": "XX999", "x": "AAAA"},
		{"kty": "EC", "crv": "P-521", "kid": "k-other-curve", "x": "AAAA", "y": "AAAA"},
	}
	v := newVerifier(t, ourPair(ks))

	sub, err := v.Verify(context.Background(),
		ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
	require.NoError(t, err,
		"непонятный ключ пропускается, набор принимается: иначе один незнакомый ключ обваливает проверку всех токенов")
	require.Equal(t, "sva-1", sub)
}

// TestF1_33_MissingKeyIDRejectedIdenticallyAcrossRotation — F1-33.
//
// Несущая половина здесь — принятие: без неё «исход одинаков» верно и на
// проверяющем, отвергающем ВСЁ, то есть проба не отличает работающую ротацию от
// мёртвого приёма.
func TestF1_33_MissingKeyIDRejectedIdenticallyAcrossRotation(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "k-before")
	v := newVerifier(t, ourPair(ks))
	at := time.Now()
	atClock(v, &at)

	noKID := func() string {
		return unsigned(tokenpolicy.AlgRS256, "", typAccessJWT, platformClaims("sva-1", at, time.Minute))
	}

	// До ротации: законный идентификатор принимается, отсутствующий — нет.
	sub, err := v.Verify(context.Background(), ks.mintRS(t, "k-before", typAccessJWT, platformClaims("sva-1", at, time.Minute)))
	require.NoError(t, err, "несущая половина: до ротации законный идентификатор ключа резолвится")
	require.Equal(t, "sva-1", sub)

	_, errBefore := v.Verify(context.Background(), noKID())
	require.ErrorIs(t, errBefore, ErrInvalidToken)

	// Ротация: прежний ключ снят, новый заведён; время сдвинуто за окно
	// вынужденного перезапроса.
	delete(ks.rsaKeys, "k-before")
	ks.addRSA(t, "k-after")
	at = at.Add(defaultMinRefresh + time.Second)

	subAfter, err := v.Verify(context.Background(), ks.mintRS(t, "k-after", typAccessJWT, platformClaims("sva-2", at, time.Minute)))
	require.NoError(t, err, "несущая половина: после ротации законный идентификатор ключа резолвится")
	require.Equal(t, "sva-2", subAfter)

	_, errAfter := v.Verify(context.Background(), noKID())
	require.ErrorIs(t, errAfter, ErrInvalidToken)
	require.Equal(t, errBefore.Error(), errAfter.Error(),
		"исход обязан быть ОДИНАКОВ до и после ротации: иначе смена ключа тихо меняет полосу допуска")
}
