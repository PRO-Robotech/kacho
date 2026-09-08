// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_issuer_binding_test.go — F1-42 и потребительская половина F1-46: издатель
// стал МНОЖЕСТВОМ, и у каждого принимаемого издателя СВОЯ объявленная запись
// источника ключей.
//
// Объединить наборы было бы дешевле и уничтожило бы ровно ту защиту, ради
// которой развязка делается: ключ одного издателя проверял бы токен,
// объявляющий другого.
package jwks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// legacyClaims — тело токена ПРЕЖНЕГО издателя.
func legacyClaims(sub string, now time.Time, ttl time.Duration) map[string]any {
	c := platformClaims(sub, now, ttl)
	c["iss"] = testLegacyIss
	return c
}

// TestF1_42_KeyOfOneIssuerNeverVerifiesTokenOfAnother — F1-42.
//
// Оба законных сочетания проходят (A/A и B/B) и оба перекрёстных отвергаются —
// в обе стороны. Довод тот же, что у адресата, и здесь он сильнее: контуров два
// и подписантов два, поэтому «ключ одного проверяет токен другого» — не теория,
// а конструкция, которая получается сама, если её не запретить.
func TestF1_42_KeyOfOneIssuerNeverVerifiesTokenOfAnother(t *testing.T) {
	// Идентификаторы ключей у записей СОВПАДАЮТ намеренно: если бы они
	// различались, отказ объяснялся бы ненайденным идентификатором, а не
	// разделением ключевого материала.
	const sharedKID = "k-1"

	our := newKeySet(t)
	our.addRSA(t, sharedKID)
	legacy := newKeySet(t)
	legacy.addRSA(t, sharedKID)

	v := newVerifier(t, ourPair(our), legacyPair(legacy))
	now := time.Now()

	t.Run("законное сочетание: наш издатель / наш ключ", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			our.mintRS(t, sharedKID, typAccessJWT, platformClaims("sva-1", now, time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})

	t.Run("законное сочетание: прежний издатель / прежний ключ", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			legacy.mintRS(t, sharedKID, typJWT, legacyClaims("cid-1", now, time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "cid-1", sub)
	})

	t.Run("подписан ключом прежнего, объявляет нашего — отвергается", func(t *testing.T) {
		// Подпись ставится ключом ПРЕЖНЕГО набора, а тело объявляет НАШЕГО
		// издателя и наш тип токена.
		claims := platformClaims("sva-1", now, time.Minute)
		_, err := v.Verify(context.Background(), legacy.mintRS(t, sharedKID, typAccessJWT, claims))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("подписан нашим ключом, объявляет прежнего — отвергается", func(t *testing.T) {
		_, err := v.Verify(context.Background(), our.mintRS(t, sharedKID, typJWT, legacyClaims("cid-1", now, time.Minute)))
		require.ErrorIs(t, err, ErrInvalidToken)
	})
}

// TestF1_46_IssuerWithoutADeclaredRecordIsRejected — F1-46, потребительская
// половина.
//
// Издатель без объявленной записи даёт ОТКАЗ: ни перебора записей подряд, ни
// обращения по адресу, выведенному из самого издателя. Утверждается счётчиком
// обращений к обоим источникам — иначе «отвергнут» верно и для реализации,
// которая перед отказом опросила всех.
func TestF1_46_IssuerWithoutADeclaredRecordIsRejected(t *testing.T) {
	our := newKeySet(t)
	our.addRSA(t, "our-1")
	legacy := newKeySet(t)
	legacy.addRSA(t, "legacy-1")
	v := newVerifier(t, ourPair(our), legacyPair(legacy))

	claims := platformClaims("sva-1", time.Now(), time.Minute)
	claims["iss"] = "https://not-declared.example"
	_, err := v.Verify(context.Background(), our.mintRS(t, "our-1", typAccessJWT, claims))

	require.ErrorIs(t, err, ErrInvalidToken)
	require.Zero(t, our.fetches.Load(), "перебора записей нет: наша запись не опрашивалась")
	require.Zero(t, legacy.fetches.Load(), "перебора записей нет: запись зеркала не опрашивалась")
}

// TestF1_46_DeclaredIssuerIsAnUntrustedInput — F1-46, половина про недоверенный
// вход.
//
// Издатель приходит ОТ ПРЕДЪЯВИТЕЛЯ и читается ДО проверки подписи. Он
// употребляется ТОЛЬКО как ключ поиска в объявленной перечнем таблице: ни
// частью адреса, ни частью ключа кэша без нормализации, ни частью текста,
// уходящего наружу. Положительный контроль — издатель ЗАКОННОЙ формы ИЗ
// ПЕРЕЧНЯ резолвится, иначе отрицание зеленеет на разборе, отвергающем всё.
func TestF1_46_DeclaredIssuerIsAnUntrustedInput(t *testing.T) {
	hostile := map[string]string{
		"разделители пути":       "https://evil.example/../../keys",
		"управляющие символы":    "https://kaname.kacho.local\x00\r\nX-Injected: 1",
		"разметка":               `<script>alert(1)</script>`,
		"схема file":             "file:///etc/passwd",
		"четыре килобайта":       "https://" + strings.Repeat("a", 4096) + ".example",
		"пусто":                  "",
		"пробелы":                "   ",
		"наш издатель с хвостом": testPlatformIss + "/../evil",
	}

	for name, iss := range hostile {
		t.Run(name, func(t *testing.T) {
			our := newKeySet(t)
			our.addRSA(t, "our-1")
			v := newVerifier(t, ourPair(our))

			claims := platformClaims("sva-1", time.Now(), time.Minute)
			claims["iss"] = iss
			_, err := v.Verify(context.Background(), our.mintRS(t, "our-1", typAccessJWT, claims))

			require.ErrorIs(t, err, ErrInvalidToken)
			if iss != "" {
				require.NotContains(t, err.Error(), iss,
					"объявленный издатель не уходит в текст, покидающий процесс")
			}
			require.Zero(t, our.fetches.Load(),
				"издатель без объявленной записи не оплачивается обращением к источнику")
		})
	}

	t.Run("положительный контроль: издатель из перечня резолвится", func(t *testing.T) {
		our := newKeySet(t)
		our.addRSA(t, "our-1")
		v := newVerifier(t, ourPair(our))
		sub, err := v.Verify(context.Background(),
			our.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})
}

// TestF1_46_DegenerateRecordIsRefused — F1-46, половина про вырожденную запись.
//
// Запись, у которой издатель есть, а адрес пуст либо состоит из пробелов, — это
// «источника нет», выданное за «источник объявлен». Проверяющий такой перечень
// не принимает вовсе: иначе отказ наступил бы на первом же запросе в рантайме,
// а не при построении.
func TestF1_46_DegenerateRecordIsRefused(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")

	cases := map[string][]KeySetSource{
		"адрес пуст":             {{Issuer: testPlatformIss, URL: "", TokenType: typAccessJWT}},
		"адрес из пробелов":      {{Issuer: testPlatformIss, URL: "   ", TokenType: typAccessJWT}},
		"издатель пуст":          {{Issuer: "", URL: ks.url(), TokenType: typAccessJWT}},
		"издатель из пробелов":   {{Issuer: "  ", URL: ks.url(), TokenType: typAccessJWT}},
		"тип токена не объявлен": {{Issuer: testPlatformIss, URL: ks.url(), TokenType: ""}},
		"записей нет вовсе":      {},
		"издатель объявлен дважды": {
			{Issuer: testPlatformIss, URL: ks.url(), TokenType: typAccessJWT},
			{Issuer: testPlatformIss, URL: ks.url(), TokenType: typJWT},
		},
	}
	for name, sources := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(sources, testAud)
			require.Error(t, err, "вырожденная запись обязана отвергаться при построении, а не в рантайме")
		})
	}

	t.Run("адресат не объявлен", func(t *testing.T) {
		_, err := New([]KeySetSource{{Issuer: testPlatformIss, URL: ks.url(), TokenType: typAccessJWT}}, "  ")
		require.Error(t, err, "незаданный ожидаемый адресат означает «любой адресат»")
	})

	t.Run("положительный контроль: полная привязка принимается", func(t *testing.T) {
		v, err := New([]KeySetSource{
			{Issuer: testPlatformIss, URL: ks.url(), TokenType: typAccessJWT},
			{Issuer: testLegacyIss, URL: ks.url(), TokenType: typJWT},
		}, testAud)
		require.NoError(t, err)
		require.NotNil(t, v)
	})
}

// TestF1_46_RecordIsChosenByExactIssuerNeverByDerivation — половина про
// ПРОВЕРЯЕМОСТЬ решения, а не только про безопасность.
//
// Пока адрес выводился бы из издателя, состояние «записи нет» не наступало бы
// НИКОГДА — и страж старта остался бы в тексте, не имея возможности упасть.
// Проба подаёт издателя, отличающегося от объявленного одним знаком, и требует
// отказа: производная конструкция дала бы обращение по выведенному адресу.
func TestF1_46_RecordIsChosenByExactIssuerNeverByDerivation(t *testing.T) {
	our := newKeySet(t)
	our.addRSA(t, "our-1")
	v := newVerifier(t, ourPair(our))

	for _, iss := range []string{
		testPlatformIss + "/",
		strings.ToUpper(testPlatformIss),
		" " + testPlatformIss,
		testPlatformIss + "?x=1",
	} {
		claims := platformClaims("sva-1", time.Now(), time.Minute)
		claims["iss"] = iss
		_, err := v.Verify(context.Background(), our.mintRS(t, "our-1", typAccessJWT, claims))
		require.ErrorIsf(t, err, ErrInvalidToken,
			"издатель %q не равен объявленному дословно и записи не имеет", iss)
	}
	require.Zero(t, our.fetches.Load(), "ни один из близких издателей не привёл к обращению к источнику")
}
