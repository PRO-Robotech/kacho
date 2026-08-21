// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package grpcsrv_test

// principal_display_codec_test.go — обе стороны кодека отображаемого имени.
//
// Утверждаются ТРИ разных свойства, и ни одно не выводится из другого:
// обратимость (что закодировали — то и расшифровали), тождественность на
// печатаемом ASCII (совместимость при выкатке) и снисходительность к чужому
// входу (производитель прежней сборки шлёт сырую строку).

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

func TestPrincipalDisplayCodec_RoundTrip(t *testing.T) {
	for _, s := range []string{
		"",
		"Demo User",
		"Демо Пользователь",
		"Ünïcodé Nâme",
		"名前",
		"100% хлопок",
		"a%20b",           // выглядит закодированным, но введено человеком
		"%%%",             //
		"tab\tи\nперевод", // управляющие символы
		strings.Repeat("я", 64),
		"emoji 🙂 внутри",
	} {
		enc := grpcsrv.EncodePrincipalDisplayName(s)
		require.True(t, printableASCIIOnly(enc),
			"кодированное значение обязано быть печатаемым ASCII: %q → %q", s, enc)
		require.Equal(t, s, grpcsrv.DecodePrincipalDisplayName(enc),
			"кодек обязан быть обратимым на %q", s)
	}
}

// Тождественность на печатаемом ASCII без знака процента — это и есть
// свойство, на которое опирается выкатка: читатель прежней сборки видит
// обычные имена без изменений.
func TestPrincipalDisplayCodec_IdentityOnPlainASCII(t *testing.T) {
	for _, s := range []string{"", "Demo User", "alice@example.com", "usr-abc123", "A B~!#$&'()*+,-./:;<=>?@[]^_`{|}"} {
		require.Equal(t, s, grpcsrv.EncodePrincipalDisplayName(s),
			"обычное имя обязано ехать байт в байт")
	}
}

// Знак процента экранируется — без этого кодек не обратим: человеческое
// «a%20b» стало бы неотличимо от кодированного «a b».
func TestPrincipalDisplayCodec_PercentIsEscaped(t *testing.T) {
	require.Equal(t, "a%2520b", grpcsrv.EncodePrincipalDisplayName("a%20b"))
	require.Equal(t, "a%20b", grpcsrv.DecodePrincipalDisplayName("a%2520b"))
}

// Вход, кодеком не производившийся, возвращается БЕЗ ИЗМЕНЕНИЙ. Это требование
// выкатки, а не снисходительность: пока на другом конце стоит производитель
// прежней сборки, по ключу приезжает сырая строка.
func TestPrincipalDisplayCodec_ForeignInputPassesThrough(t *testing.T) {
	for _, s := range []string{
		"100% Cotton", // сырой процент от прежнего производителя
		"50%",         // усечённая последовательность
		"%zz",         // не шестнадцатеричная пара
		"%D0",         // расшифровалось бы в не-UTF-8
	} {
		require.Equal(t, s, grpcsrv.DecodePrincipalDisplayName(s),
			"чужой вход обязан пройти насквозь, а не превратиться в мусор: %q", s)
	}
}

func printableASCIIOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return false
		}
	}
	return true
}

// Страж пересылки идемпотентен — иначе он портил бы уже закодированное
// значение на каждом промежуточном участке пути.
func TestPrincipalDisplayWireGuard_IsIdempotent(t *testing.T) {
	for _, s := range []string{"Demo User", "Демо Пользователь", "100% хлопок", "", "名前"} {
		once := grpcsrv.EnsurePrincipalDisplayNameWireSafe(grpcsrv.EncodePrincipalDisplayName(s))
		twice := grpcsrv.EnsurePrincipalDisplayNameWireSafe(once)
		require.Equal(t, once, twice, "повторная пересылка обязана не менять значение: %q", s)
		require.Equal(t, s, grpcsrv.DecodePrincipalDisplayName(twice),
			"после двух пересылок имя обязано расшифроваться в исходное: %q", s)
	}
}

// Страж обязан спасать и сырое значение: производитель прежней сборки на другом
// конце выкатки шлёт незакодированную строку, и уронить на ней вызов нельзя.
func TestPrincipalDisplayWireGuard_RescuesRawInput(t *testing.T) {
	got := grpcsrv.EnsurePrincipalDisplayNameWireSafe("Демо")
	require.True(t, printableASCIIOnly(got), "страж обязан сделать значение пригодным транспорту")
	require.Equal(t, "Демо", grpcsrv.DecodePrincipalDisplayName(got))
}
