// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Инъекция гейта «отображаемое имя не кладут на провод сырым» — В ОБЕ СТОРОНЫ.
//
// Гейт обходит дерево, поэтому его признак здесь предъявляется синтетическому
// исходнику. Доказывается не «признак срабатывает», а что он различает ДЕФЕКТ и
// ЗАКОННОГО БЛИЗНЕЦА той же формы: без второй половины он ловил бы форму вызова
// и был бы снят первым же ложным срабатыванием.

import (
	"strings"
	"testing"
)

const injHead = `package p

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"google.golang.org/grpc/metadata"
)

var _ = http.Header{}
var _ = principalmeta.MetaPrincipalDisplay
var _ = grpcsrv.EncodePrincipalDisplayName
var _ = metadata.MD{}

`

func injFind(t *testing.T, body string) ([]RawDisplayNameWrite, int) {
	t.Helper()
	finds, writes, err := findRawDisplayNameWrites("inj.go", []byte(injHead+body))
	if err != nil {
		t.Fatalf("синтетический исходник не разобрался: %v", err)
	}
	return finds, writes
}

// --- КРАСНОЕ: сырое значение под ключом — в каждой из форм записи.
func TestInjection_RawDisplayNameWriteIsFound(t *testing.T) {
	cases := map[string]string{
		"заголовок": `func f(h http.Header, name string) { h.Set(principalmeta.HeaderPrincipalDisplay, name) }`,
		"мостовой":  `func f(h http.Header, name string) { h.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, name) }`,
		"metadata":  `func f(md metadata.MD, name string) { md.Append(principalmeta.MetaPrincipalDisplay, name) }`,
		"исходящий": `func f(ctx context.Context, name string) { metadata.AppendToOutgoingContext(ctx, principalmeta.MetaPrincipalDisplay, name) }`,
		"литерал":   `func f(h http.Header) { h.Set("x-kacho-principal-display-name", userName()) }`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			finds, writes := injFind(t, body)
			if writes == 0 {
				t.Fatalf("запись под ключ не распознана вовсе — признак ослеп")
			}
			if len(finds) == 0 {
				t.Fatalf("сырая запись НЕ найдена: гейт не покраснеет на дефекте")
			}
			if !strings.Contains(strings.ToLower(finds[0].Expr), "display") {
				t.Errorf("находка не называет координату по существу: %q", finds[0].Expr)
			}
		})
	}
}

// --- МОЛЧАНИЕ: законные близнецы той же формы.
func TestInjection_EncodedWriteIsSilent(t *testing.T) {
	cases := map[string]string{
		"кодировщик": `func f(h http.Header, name string) {
	h.Set(principalmeta.HeaderPrincipalDisplay, grpcsrv.EncodePrincipalDisplayName(name))
}`,
		"страж пересылки": `func f(md metadata.MD, pd string) {
	md.Append(principalmeta.MetaPrincipalDisplay, grpcsrv.EnsurePrincipalDisplayNameWireSafe(pd))
}`,
		"константа ASCII": `func f(h http.Header) { h.Set(principalmeta.HeaderPrincipalDisplay, "") }`,
		"другой ключ":     `func f(h http.Header, v string) { h.Set(principalmeta.HeaderPrincipalID, v) }`,
		"чтение, не запись": `func f(md metadata.MD) string {
	return md.Get(principalmeta.MetaPrincipalDisplay)[0]
}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			finds, _ := injFind(t, body)
			if len(finds) != 0 {
				t.Errorf("законная форма помечена находкой (%q) — первый же ложный "+
					"срабат снимет гейт целиком", finds[0].Expr)
			}
		})
	}
}

// --- признак читает РАЗОБРАННЫЙ исходник: имя ключа в комментарии и в строке
// не является записью. Без этого гейт краснел бы на собственном объяснении.
func TestInjection_CommentAndStringAreNotWrites(t *testing.T) {
	body := "// h.Set(principalmeta.HeaderPrincipalDisplay, name) — так делать нельзя\n" +
		"var doc = `под ключ x-kacho-principal-display-name кладут закодированное`\n"
	finds, writes := injFind(t, body)
	if len(finds) != 0 || writes != 0 {
		t.Errorf("проза о ключе принята за запись (находок %d, записей %d)", len(finds), writes)
	}
}
