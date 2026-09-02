// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestverbclassrule_injection_test.go — доказательство способности обоих
// гейтов УПАСТЬ и СМОЛЧАТЬ, по каждой оси, с законным близнецом рядом.
//
// Вход синтетический и подаётся распознавателю напрямую: дерево продукта при
// этом не трогается, и прогон не роняется (`testing.md` §«Гейт на класс», п. 2).
//
// Законный близнец здесь — НЕ выдумка, а форма, которая в дереве ЖИВЁТ:
// `verbDisplayPrecedence` (порядок показа глаголов) несёт тот же набор из пяти
// токенов и правилом «класс из имени» не является. Первая редакция
// распознавателя ловила бы его — и гейт, краснеющий на верном коде, отключили
// бы первым.
package repohygiene

import (
	"go/parser"
	"go/token"
	"testing"
)

// classRuleDeclarationsInSource — распознаватель над синтетическим исходником.
func classRuleDeclarationsInSource(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatalf("синтетический исходник не разбирается: %v — вход НЕ ПРОИЗВЕДЁН", err)
	}
	return classRuleDeclarationsIn(fset, file, "synthetic.go")
}

const classRuleOwner = `package p

var canonicalVerbClasses = []string{"get", "list", "create", "update", "delete"}

func ClassOfCanonicalVerb(name string) (string, bool) {
	for _, c := range canonicalVerbClasses {
		if c == name {
			return name, true
		}
	}
	return "", false
}
`

// TestClassRuleRecognizerFindsTheOwner — положительный контроль: без него
// «второго объявления нет» зеленело бы и на распознавателе, не находящем
// НИЧЕГО.
func TestClassRuleRecognizerFindsTheOwner(t *testing.T) {
	if got := classRuleDeclarationsInSource(t, classRuleOwner); len(got) != 1 {
		t.Fatalf("распознаватель нашёл %d объявлений в файле, где оно одно: %v", len(got), got)
	}
}

// TestClassRuleRecognizerFindsASecondDeclaration — инъекция: вторая копия
// правила, написанная В ДРУГОЙ ФОРМЕ (свой switch вместо общего набора), —
// находка с координатой и именем функции.
func TestClassRuleRecognizerFindsASecondDeclaration(t *testing.T) {
	got := classRuleDeclarationsInSource(t, classRuleOwner+`
func classOf(verb string) (string, bool) {
	switch verb {
	case "get", "list", "create", "update", "delete":
		return verb, true
	}
	return "", false
}
`)
	if len(got) != 2 {
		t.Fatalf("вторая копия правила не найдена: объявлений %d (%v)", len(got), got)
	}
	if got[1] != "synthetic.go:14 classOf" {
		t.Errorf("находка не называет координату и имя: %q", got[1])
	}
}

// TestClassRuleRecognizerIsSilentOnTheDisplayPrecedenceTwin — законный близнец:
// ПОРЯДОК ПОКАЗА глаголов несёт тот же набор токенов и правилом не является.
//
// Он живёт в дереве (`domain.verbDisplayPrecedence`), поэтому это не выдуманная
// форма, а тот самый вход, на котором наивный распознаватель дал бы ложную
// находку.
func TestClassRuleRecognizerIsSilentOnTheDisplayPrecedenceTwin(t *testing.T) {
	got := classRuleDeclarationsInSource(t, `package p

var verbDisplayPrecedence = []string{"get", "list", "create", "update", "delete"}

func orderVerbs(set map[string]bool) []string {
	var out []string
	for _, c := range verbDisplayPrecedence {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}
`)
	if len(got) != 0 {
		t.Fatalf("порядок показа принят за правило «класс из имени»: %v", got)
	}
}

// TestClassRuleRecognizerIsSilentOnAMereCaller — второй законный близнец: тот,
// кто ПРАВИЛО ЗОВЁТ, объявлением его не является. Иначе гейт краснел бы на
// каждом потребителе и запрещал бы пользоваться единственным объявлением.
func TestClassRuleRecognizerIsSilentOnAMereCaller(t *testing.T) {
	got := classRuleDeclarationsInSource(t, `package p

func classOfVerb(name string) (string, bool) {
	return manifest.ClassOfCanonicalVerb(name)
}
`)
	if len(got) != 0 {
		t.Fatalf("вызывающий принят за объявление: %v", got)
	}
}

// TestClassRuleRecognizerIsSilentOnTheTierClassifier — третий законный близнец:
// расширенный классификатор ЯРУСА (30 токенов) классом действия не занимается.
// Распознаватель требует РАВЕНСТВА множеств, а не включения, — иначе он поймал
// бы его.
func TestClassRuleRecognizerIsSilentOnTheTierClassifier(t *testing.T) {
	got := classRuleDeclarationsInSource(t, `package p

func verbTier(verb string) (string, bool) {
	switch verb {
	case "get", "list", "view", "watch", "describe", "read":
		return "viewer", true
	case "create", "update", "delete", "write", "patch", "put":
		return "editor", true
	}
	return "", false
}
`)
	if len(got) != 0 {
		t.Fatalf("классификатор яруса принят за правило «класс из имени»: %v", got)
	}
}

// objectTypeWritesInSource — распознаватель присваиваний над синтетикой.
func objectTypeWritesInSource(t *testing.T, src string) (reads int, writes []string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, 0)
	if err != nil {
		t.Fatalf("синтетический исходник не разбирается: %v — вход НЕ ПРОИЗВЕДЁН", err)
	}
	return objectTypeReadsAndWrites(fset, file, "synthetic.go")
}

// TestObjectTypeRecognizerFindsARestoredDerivation — инъекция: правило вывода
// вернулось присваиванием.
func TestObjectTypeRecognizerFindsARestoredDerivation(t *testing.T) {
	reads, writes := objectTypeWritesInSource(t, `package p

func fill(r *Resource, module string) {
	if r.ObjectType == "" {
		r.ObjectType = module + "_" + r.Name
	}
}
`)
	if len(writes) != 1 {
		t.Fatalf("восстановленный вывод не найден: присваиваний %d (%v)", len(writes), writes)
	}
	if writes[0] != "synthetic.go:5" {
		t.Errorf("находка не называет координату: %q", writes[0])
	}
	if reads == 0 {
		t.Errorf("чтений ноль — распознаватель не видит поля вовсе, и вердикт о " +
			"присваиваниях ничего не значил бы")
	}
}

// TestObjectTypeRecognizerIsSilentWhenTheFieldIsOnlyRead — законный близнец:
// поле только читается, и это ровно то, чего требует снятие правила.
func TestObjectTypeRecognizerIsSilentWhenTheFieldIsOnlyRead(t *testing.T) {
	reads, writes := objectTypeWritesInSource(t, `package p

func check(r *Resource) error {
	if r.ObjectType == "" {
		return errObjectTypeRequired
	}
	if _, ok := lookup(r.ObjectType); !ok {
		return errObjectTypeUnknown
	}
	return nil
}
`)
	if len(writes) != 0 {
		t.Fatalf("чтение принято за присваивание: %v", writes)
	}
	if reads != 2 {
		t.Errorf("чтений %d при двух написанных — распознаватель считает не то", reads)
	}
}
