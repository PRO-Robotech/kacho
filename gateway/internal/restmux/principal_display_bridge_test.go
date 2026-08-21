// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// principal_display_bridge_test.go — мост REST→gRPC переносит отображаемое имя
// в форме, которую транспорт примет при ЛЮБОМ алфавите (#873).
//
// Проба утверждает не «функция вызвана», а СВОЙСТВО значения, доехавшего до
// metadata: оно печатаемо-ASCII (иначе транспорт отвергнет весь вызов) и
// расшифровывается обратно в исходную строку.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return false
		}
	}
	return true
}

func TestIssue873_BridgeCarriesCyrillicDisplayName(t *testing.T) {
	const cyrillic = "Демо Пользователь"

	r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-demo")
	principalmeta.SetPrincipalDisplay(r.Header, cyrillic)

	md := annotate(t, r)

	got := md.Get(principalmeta.MetaPrincipalDisplay)
	if len(got) == 0 {
		t.Fatalf("отображаемое имя потеряно мостом")
	}
	// Мост кладёт значение НЕСКОЛЬКО раз (голая и мостовая формы заголовка плюс
	// сборка metadata) — кратность здесь не предмет и измерена отдельно, #930.
	// Утверждается свойство КАЖДОЙ копии: транспорт отвергает вызов по любой
	// из них, а потребитель читает первую.
	for i, v := range got {
		if !printableASCII(v) {
			t.Errorf("копия %d непечатаема (%q) — транспорт отвергнет ВЕСЬ вызов", i, v)
		}
		if back := grpcsrv.DecodePrincipalDisplayName(v); back != cyrillic {
			t.Errorf("копия %d доехала искажённой: %q, ожидалось %q", i, back, cyrillic)
		}
	}
}

// Положительный контроль: латиница пересекает мост БАЙТ В БАЙТ. Без него
// предыдущая проба зеленела бы на кодеке, экранирующем всё подряд.
func TestIssue873_BridgeLeavesASCIIDisplayNameByteIdentical(t *testing.T) {
	const ascii = "Demo User"

	r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-demo")
	principalmeta.SetPrincipalDisplay(r.Header, ascii)

	md := annotate(t, r)
	got := md.Get(principalmeta.MetaPrincipalDisplay)
	if len(got) == 0 {
		t.Fatalf("отображаемое имя потеряно мостом")
	}
	for i, v := range got {
		if v != ascii {
			t.Errorf("копия %d изменена (%q) — выкатка испортила бы поле контракта", i, v)
		}
	}
}
