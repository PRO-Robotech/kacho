// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keysourceheadermembers_injection_test.go — доказательство того, что
// TestKeySourceHeaderMembersAreDeclaredOnce СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanKeySourceHeaderMembers), что и
// гейт, и берёт перечень из ТОГО ЖЕ единственного объявления.
//
// Вторая сторона пары здесь тяжелее первой и без неё гейт был бы вреден: место,
// где ключ ВЫБИРАЕТСЯ из предъявленного материала, в дереве есть и законно —
// это доказательство владения ключом (RFC 9449), где встроенный ключ не
// уязвимость, а предмет. Гейт, считающий всякое упоминание второй копией
// перечня, объявил бы находкой соседний механизм.
package repohygiene

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// keySourceInjectedSecondList — ВТОРАЯ копия перечня, в двух написаниях: срезом
// и разбором заголовка по членам.
//
// Второе написание здесь не украшение. Выписать перечень можно не только
// списком строк: разбор заголовка, перечисляющий члены полями структуры,
// объявляет ровно тот же список — и обойти гейт, читающий только срезы, можно
// было бы одной сменой формы записи.
const keySourceInjectedSecondList = `package clientassertion

var forbiddenKeySources = []string{"jwk", "jku", "x5u", "x5c"}

type joseHeader struct {
	Alg string ` + "`json:\"alg\"`" + `
	JWK string ` + "`json:\"jwk\"`" + `
	JKU string ` + "`json:\"jku\"`" + `
	X5C string ` + "`json:\"x5c\"`" + `
}
`

// keySourceInjectedDeliberateSingleMember — законный близнец: место, где ключ
// берётся ИЗ предъявленного материала намеренно.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - `"x5t#S256"` в подтверждении владения — тот же член в другой роли;
//   - `"kid"`, `"alg"`, `"typ"` — члены заголовка, источниками ключа НЕ
//     являющиеся: разбор, судящий «упомянут член заголовка», объявил бы
//     находкой всякий разбор JOSE;
//   - `"jwks"` — имя, отличающееся от члена перечня одной буквой.
const keySourceInjectedDeliberateSingleMember = `package middleware

type dpopHeader struct {
	Alg string ` + "`json:\"alg\"`" + `
	Typ string ` + "`json:\"typ\"`" + `
	Kid string ` + "`json:\"kid\"`" + `
	JWK string ` + "`json:\"jwk\"`" + `
}

func verifyProof(hdr dpopHeader, cnf map[string]any) error {
	// Ключ приходит В САМОМ доказательстве, и это его ПРЕДМЕТ: проверяющий
	// связывает предъявленный ключ с токеном по отпечатку.
	key, err := parseJWK(hdr.JWK)
	if err != nil {
		return err
	}
	_ = cnf["x5t#S256"]
	_ = fetch("jwks")
	return bind(key)
}
`

// TestKeySourceScannerFindsASecondList — сторона (а): вторая копия перечня
// становится находкой, и находка несёт координату.
func TestKeySourceScannerFindsASecondList(t *testing.T) {
	members := tokenpolicy.KeySourceHeaderMembers()
	sites, census, err := ScanKeySourceHeaderMembers(
		"synthetic/clientassertion/header.go", []byte(keySourceInjectedSecondList), members)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.StringLiterals == 0 && census.TagNames == 0 {
		t.Fatalf("осмотрено ноль литералов и ноль имён из тегов — разбирается не то дерево")
	}

	var lists []KeySourceSite
	for _, s := range sites {
		if len(s.Members) >= keySourceListArity {
			lists = append(lists, s)
		}
	}
	if len(lists) != 2 {
		t.Fatalf("объявлений перечня найдено %d, ожидалось 2 (срезом и полями структуры): %+v",
			len(lists), sites)
	}
	for _, s := range lists {
		if s.File == "" || s.Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", s)
		}
		if s.Decl == "" {
			t.Errorf("находка не называет объявление — по номеру строки читатель не поймёт, "+
				"что именно выписало перечень: %+v", s)
		}
		if len(s.Members) < keySourceListArity {
			t.Errorf("находка названа перечнем, называя %d член: %+v", len(s.Members), s)
		}
	}
	if lists[0].Line == lists[1].Line {
		t.Errorf("обе находки на одной строке (%d) — разбор считает строки, а не объявления: %+v",
			lists[0].Line, lists)
	}

	// Форма записи на исход влиять не должна: перечень, выписанный тегами полей,
	// остаётся перечнем.
	forms := map[string]bool{}
	for _, s := range lists {
		for _, m := range s.Mentions {
			forms[m.Form] = true
		}
	}
	if len(forms) != 2 {
		t.Fatalf("разбор увидел перечень только в одной форме записи (%v) — сменой формы "+
			"вторую копию можно было бы внести мимо гейта", forms)
	}
}

// TestKeySourceScannerIsSilentOnADeliberateSingleMember — сторона (б): место,
// где ключ берётся из предъявленного материала НАМЕРЕННО, находкой не является.
func TestKeySourceScannerIsSilentOnADeliberateSingleMember(t *testing.T) {
	members := tokenpolicy.KeySourceHeaderMembers()
	sites, census, err := ScanKeySourceHeaderMembers(
		"synthetic/middleware/dpop.go", []byte(keySourceInjectedDeliberateSingleMember), members)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.TagNames < 4 {
		t.Fatalf("имён из тегов осмотрено %d — разбирается не то дерево", census.TagNames)
	}

	var lists, singles []KeySourceSite
	for _, s := range sites {
		if len(s.Members) >= keySourceListArity {
			lists = append(lists, s)
			continue
		}
		singles = append(singles, s)
	}
	if len(lists) != 0 {
		t.Fatalf("разбор объявил перечнем законное место (%+v).\n\n"+
			"Тогда гейт запрещал бы механизм, ради которого §5 и написан: в доказательстве "+
			"владения ключом ключ приходит В САМОМ доказательстве, и проверяющий связывает "+
			"его с токеном по отпечатку — там встроенный ключ не уязвимость, а предмет.", lists)
	}
	if len(singles) == 0 {
		t.Fatalf("разбор не увидел НИ ОДНОГО упоминания члена на месте, которое называет " +
			"его дважды (полем и разбором): молчание гейта здесь означало бы не различение, " +
			"а слепоту")
	}

	// Молчание обязано быть взвешиваемым: разбор увидел члены и признал каждый
	// употреблением, а не «не заметил».
	seen := map[string]bool{}
	for _, s := range singles {
		if len(s.Members) != 1 {
			t.Fatalf("вместилище названо употреблением, называя %d членов (%v) — тогда "+
				"разбор считает не вместилища, а что-то другое: %+v",
				len(s.Members), s.Members, s)
		}
		seen[s.Members[0]] = true
	}
	if !seen["jwk"] {
		t.Fatalf("разбор не увидел члена jwk вовсе (увидел: %v) — тогда его молчание сказано "+
			"о слепоте, а не о различении", keySourceSortedMembers(seen))
	}
	// Два члена в РАЗНЫХ ролях, названные одной функцией, перечнем не являются:
	// заголовок доказательства несёт `jwk`, подтверждение владения — `x5t#S256`,
	// и лежат они в разных вместилищах. Единицей «одна функция» разбор объявил
	// бы это находкой — измерено на этой самой синтетике, а не предположено.
	if !seen["x5t#S256"] {
		t.Fatalf("разбор не увидел члена подтверждения владения (увидел: %v) — тогда "+
			"утверждение о РАЗНЫХ ролях в одной функции ничем не подтверждено",
			keySourceSortedMembers(seen))
	}
	if len(singles) < 2 {
		t.Fatalf("два члена в разных ролях легли в %d вместилище — они обязаны остаться "+
			"ДВУМЯ местами по одному члену, иначе законная функция становится находкой",
			len(singles))
	}
	// Члены заголовка, источниками ключа НЕ являющиеся, и имя, отличающееся
	// одной буквой, упоминаниями считаться не должны.
	if len(seen) != 2 {
		t.Fatalf("на законном месте признано %d разных членов (%v), ожидалось 2 — разбор "+
			"принимает за члены перечня то, что в него не входит",
			len(seen), keySourceSortedMembers(seen))
	}
}

// TestKeySourceScannerReadsTheSingleDeclarationItself — предпосылка гейта: он
// берёт имена ИЗ единственного объявления, а не из своей копии.
//
// Без этой пробы гейт мог бы стеречь перечень, разошедшийся с тем, которым
// пользуется проверяющий, — и молчать ровно о том члене, который добавили.
func TestKeySourceScannerReadsTheSingleDeclarationItself(t *testing.T) {
	members := tokenpolicy.KeySourceHeaderMembers()
	if len(members) < keySourceListArity {
		t.Fatalf("единственное объявление называет %d член(а) — перечнем оно быть перестало",
			len(members))
	}
	// Каждый член перечня обязан быть опознан разбором: член, который разбор не
	// видит, останется невыраженным именно там, где его выписали второй раз.
	for _, m := range members {
		src := "package p\n\nvar a = []string{" + strconv.Quote(m) + ", " + strconv.Quote("kid") + "}\n"
		sites, _, err := ScanKeySourceHeaderMembers("synthetic/p.go", []byte(src), members)
		if err != nil {
			t.Fatalf("разбор синтетики для члена %q: %v", m, err)
		}
		if len(sites) != 1 || len(sites[0].Members) != 1 || sites[0].Members[0] != m {
			t.Fatalf("член %q не опознан разбором (получено %+v) — вторая копия перечня, "+
				"содержащая его, прошла бы мимо гейта", m, sites)
		}
	}
	t.Logf("перечень единственного объявления опознаётся разбором целиком: %s",
		strings.Join(members, ", "))
}

// keySourceSortedMembers — имена множества по возрастанию, для читаемого текста отказа.
func keySourceSortedMembers(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
