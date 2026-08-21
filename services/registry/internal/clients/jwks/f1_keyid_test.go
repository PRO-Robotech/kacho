// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_keyid_test.go — F1-22: идентификатор ключа приходит ОТ ПРЕДЪЯВИТЕЛЯ и
// читается ДО проверки подписи. Значит его форма ограничивается до
// использования, а сам он употребляется ТОЛЬКО для поиска ключа.
package jwks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/stretchr/testify/require"
)

// hostileKeyIDs — виды враждебного входа, названные сценарием: длинная строка,
// разделители пути, управляющие символы, элементы разметки.
func hostileKeyIDs() map[string]string {
	return map[string]string{
		"разделители пути":     "../../../etc/passwd",
		"обратные разделители": `..\..\windows\system32`,
		"схема и адрес":        "https://evil.example/keys.json",
		"управляющие символы":  "kid\x00\r\n\tnext",
		"разметка":             `<script>alert(1)</script>`,
		"подстановка формата":  "%s%n%d",
		"четыре килобайта":     strings.Repeat("A", 4096),
		"пусто":                "",
		"пробелы":              "   ",
	}
}

// TestF1_22_KeyIDIsBoundedBeforeUse — F1-22.
//
// Утверждается ТРИ вещи сразу, и каждая — про отдельный путь утечки: значение не
// доходит до обращения к источнику (счётчик обращений), не уходит в текст,
// покидающий процесс (текст ошибки), и вообще не резолвится. Плюс положительный
// контроль: идентификатор ЗАКОННОЙ формы резолвится — иначе сужение формы
// неотличимо от отказа резолвить что-либо.
func TestF1_22_KeyIDIsBoundedBeforeUse(t *testing.T) {
	for name, kid := range hostileKeyIDs() {
		t.Run(name, func(t *testing.T) {
			ks := newKeySet(t)
			ks.addRSA(t, "our-1")
			v := newVerifier(t, ourPair(ks))

			tok := unsigned(tokenpolicy.AlgRS256, kid, typAccessJWT,
				platformClaims("sva-1", time.Now(), time.Minute))
			_, err := v.Verify(context.Background(), tok)

			require.ErrorIs(t, err, ErrInvalidToken, "идентификатор негодной формы не резолвится")
			if kid != "" {
				require.NotContains(t, err.Error(), kid,
					"значение от предъявителя не уходит в текст, покидающий процесс")
			}
			require.Zero(t, ks.fetches.Load(),
				"форма ограничивается ДО использования: негодный идентификатор не оплачивается обращением к источнику")
		})
	}

	t.Run("положительный контроль: законная форма резолвится", func(t *testing.T) {
		for _, kid := range []string{
			"our-1",
			"a",
			"AZaz09-_.~:",
			strings.Repeat("k", maxKeyIDLen),
		} {
			ks := newKeySet(t)
			ks.addRSA(t, kid)
			v := newVerifier(t, ourPair(ks))

			sub, err := v.Verify(context.Background(),
				ks.mintRS(t, kid, typAccessJWT, platformClaims("sva-1", time.Now(), time.Minute)))
			require.NoErrorf(t, err, "идентификатор законной формы %q обязан резолвиться", kid)
			require.Equal(t, "sva-1", sub)
		}
	})
}

// TestF1_22_KeyIDLengthCeilingIsTheOneDeclared — граница потолка утверждается с
// обеих сторон: ровно потолок годен, потолок плюс один — нет. Односторонняя
// проверка зеленела бы на любом потолке, включая нулевой.
func TestF1_22_KeyIDLengthCeilingIsTheOneDeclared(t *testing.T) {
	require.True(t, keyIDWellFormed(strings.Repeat("k", maxKeyIDLen)),
		"ровно потолок длины обязан быть годен")
	require.False(t, keyIDWellFormed(strings.Repeat("k", maxKeyIDLen+1)),
		"потолок плюс один обязан быть негоден")
}
