// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// auth_methods_test.go — форма перечня способов на проводе (#1252).
//
// Каждое утверждение здесь парное: годный вход обязан ПРОЙТИ, негодный —
// отсеяться И БЫТЬ НАЗВАННЫМ. Одна половина без другой зеленела бы на кодеке,
// который выбрасывает всё, либо на кодеке, который не проверяет ничего.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// TestEncodeAuthMethods_KeepsWhatTravelsAndNamesWhatDoesNot — обе стороны формы.
func TestEncodeAuthMethods_KeepsWhatTravelsAndNamesWhatDoesNot(t *testing.T) {
	// Положительная сторона: словари ОБОИХ известных источников проходят
	// целиком. Без неё «отбрасываем негодное» верно и для кодека, который
	// отбрасывает всё.
	known := []string{
		// провайдер сессий
		"password", "webauthn", "totp", "lookup_secret", "code", "oidc", "passkey",
		// RFC 8176
		"pwd", "otp", "mfa", "hwk", "swk", "sms", "user", "pin", "sc",
	}
	for _, m := range known {
		got, dropped := principalmeta.EncodeAuthMethods([]string{m})
		assert.Equal(t, m, got, "способ %q обязан пройти на провод как есть", m)
		assert.Empty(t, dropped, "способ %q отброшен, хотя годен", m)
	}

	// Отрицательная сторона: каждое значение негодно по СВОЕЙ причине, и каждое
	// обязано быть НАЗВАНО вызывающему, а не исчезнуть.
	unusable := map[string]string{
		"веб-ключ":  "кириллица роняет метаданные gRPC целиком",
		"web authn": "разделитель внутри значения расколол бы один способ на два",
		"web\nauth": "управляющий знак",
		"":          "пустое имя способа",
		"   ":       "только пробелы",
		strings.Repeat("a", principalmeta.MaxAuthMethodLen+1): "длиннее потолка",
	}
	for v, why := range unusable {
		got, dropped := principalmeta.EncodeAuthMethods([]string{v})
		assert.Empty(t, got, "негодное значение уехало на провод (%s)", why)
		require.Len(t, dropped, 1, "негодное значение отброшено МОЛЧА (%s)", why)
		assert.Equal(t, v, dropped[0], "отброшенное обязано называться как пришло")
	}
}

// TestEncodeAuthMethods_NormalisesCaseAndDeduplicates — написание не различает
// способы, а повтор не растит значение.
func TestEncodeAuthMethods_NormalisesCaseAndDeduplicates(t *testing.T) {
	got, dropped := principalmeta.EncodeAuthMethods([]string{"WebAuthn", " webauthn ", "PASSWORD"})
	assert.Empty(t, dropped)
	assert.Equal(t, "webauthn password", got)
}

// TestEncodeAuthMethods_BoundsTheCountAndNamesTheOverflow — перечень приходит из
// чужого ответа, поэтому его длина ограничена здесь, а не надеждой на источник.
func TestEncodeAuthMethods_BoundsTheCountAndNamesTheOverflow(t *testing.T) {
	in := make([]string, 0, principalmeta.MaxAuthMethods+3)
	for i := 0; i < principalmeta.MaxAuthMethods+3; i++ {
		in = append(in, "m"+string(rune('a'+i)))
	}
	got, dropped := principalmeta.EncodeAuthMethods(in)
	assert.Len(t, strings.Fields(got), principalmeta.MaxAuthMethods)
	assert.Len(t, dropped, 3, "лишнее обязано быть названо, а не срезано молча")
}

// TestDecodeAuthMethods_RoundTripsAndIsNoMoreLenientThanTheWrite — разбор не
// вправе быть снисходительнее записи: второе, более мягкое правило о той же
// форме — это второе правило о том же предмете.
func TestDecodeAuthMethods_RoundTripsAndIsNoMoreLenientThanTheWrite(t *testing.T) {
	value, dropped := principalmeta.EncodeAuthMethods([]string{"webauthn", "password"})
	require.Empty(t, dropped)
	assert.Equal(t, []string{"webauthn", "password"}, principalmeta.DecodeAuthMethods(value))

	// Пустое значение — «способов не назвали», а не «способов ноль».
	assert.Nil(t, principalmeta.DecodeAuthMethods(""))
	assert.Nil(t, principalmeta.DecodeAuthMethods("   "))

	// Значение, которого запись бы не произвела, разбор не принимает.
	assert.Equal(t, []string{"password"}, principalmeta.DecodeAuthMethods("веб password"))
	assert.Nil(t, principalmeta.DecodeAuthMethods("веб-ключ"))
}

// TestEdgeOnlyKeys_HaveASubject — набор «остаются на краю» обязан состоять из
// ключей, которые край ПРОИЗВОДИТ. Запись про ключ вне семейства — исключение,
// потерявшее предмет: оно переживёт то, что им обозначалось.
func TestEdgeOnlyKeys_HaveASubject(t *testing.T) {
	edgeOnly := []string{principalmeta.MetaTokenAMR, principalmeta.MetaTokenMfaAt}
	for _, k := range edgeOnly {
		assert.True(t, principalmeta.IsEdgeOnlyKey(k), "%s объявлен остающимся на краю", k)
		assert.True(t, principalmeta.IsGatewayProducedKey(k),
			"%s остаётся на краю, но краем не производится — у записи нет предмета", k)
		assert.True(t, principalmeta.IsClientForgeableKey(k),
			"%s обязан сниматься со входящего запроса как всё в этом пространстве имён", k)
	}
	// Положительный контроль: соседний ключ того же семейства на краю НЕ
	// остаётся — иначе утверждение выше было бы верно для чего угодно.
	assert.False(t, principalmeta.IsEdgeOnlyKey(principalmeta.MetaTokenJti))
	assert.False(t, principalmeta.IsEdgeOnlyKey(principalmeta.MetaTokenACR))
	t.Logf("перепись: ключей семейства, остающихся на краю — %d", len(edgeOnly))
}
