// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestfilename_test.go — имя файла манифеста модуля объявлено ОДИН раз
// (задача продукта #1934).
//
// # Предмет
//
// Модуль кладёт своё объявление под именем `manifest.yaml`, а доставка адресует
// тот же документ ключом `<модуль>.manifest.yaml`: ключ ConfigMap не принимает
// `/`, поэтому раскладку дерева доставка воспроизвести не может (замер kubectl —
// в шапке `services/iam/internal/manifest/delivery.go`). Форм ДВЕ, и это
// решение — но объявлений имени было ТРИ, и ни одно из трёх о второй форме не
// знало.
//
// Повторённое соглашение расходится МОЛЧА, и цена у него та же, что у любой
// потерянной части обхода: часть манифестов перестаёт находиться, не дав ни
// одной находки. «Ноль находок» становится неотличимо от «ноль прочитанного» —
// ровно тот класс, который этот корпус и ловит.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ, а что прозой
//
//	const manifestFileName = "manifest.yaml"   ← объявление: имя как значение
//	"под ним ищется services/*/manifest.yaml"  ← ПРОЗА: текст оператору
//	// error: cannot add key "manifest.yaml"   ← комментарий: не исполняется
//
// Гейт судит УЗЕЛ-ЛИТЕРАЛ разобранного исходника и сравнивает его значение
// ДОСЛОВНО. Поэтому подстрока внутри фразы находкой не является: она не несёт
// имени как величины, её не подставишь в путь, и она не разойдётся с обходом —
// разойдётся с читателем, а это другой предмет и другой держатель. Комментарии
// разбор не видит by construction: гейт по подстроке краснел бы на собственном
// объяснении.
//
// # Чего гейт НЕ закрывает — названо, а не спрятано
//
// Он не судит, ВЕРНО ли имя: это вопрос к владельцу объявления, а не к числу
// объявлений. И он не видит имени, собранного из частей
// (`"manifest" + "." + ext`); такой формы в дереве ноль, и разбор потока значений
// был бы вторым механизмом об одном предмете.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// moduleManifestOwner — каталог единственного объявления.
	moduleManifestOwner = "pkg/modulemanifest/"
	// moduleManifestCensusFloor — порог переписи: ниже него «ноль находок»
	// означало бы «ноль прочитанного».
	moduleManifestCensusFloor = 1000
)

// moduleManifestSpellings — формы имени, каждая из которых обязана приезжать
// читателю ИЗ объявления, а не из его собственного исходника.
var moduleManifestSpellings = map[string]string{
	"manifest.yaml":  "имя файла В ДЕРЕВЕ",
	".manifest.yaml": "окончание ключа В ДОСТАВКЕ",
}

// TestModuleManifestFileNameIsDeclaredExactlyOnce — сам гейт.
func TestModuleManifestFileNameIsDeclaredExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, literals int
		owner, findings  []string
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		parsed++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			literals++
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			what, named := moduleManifestSpellings[value]
			if !named {
				return true
			}
			site := fmt.Sprintf("%s:%d  %q — %s", rel, fset.Position(lit.Pos()).Line, value, what)
			if strings.HasPrefix(rel, moduleManifestOwner) {
				owner = append(owner, site)
				return true
			}
			findings = append(findings, site)
			return true
		})
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, строковых литералов осмотрено %d, "+
		"форм имени объявлено %d, объявлений у владельца (%s) найдено %d, вне владельца %d",
		parsed, literals, len(moduleManifestSpellings), moduleManifestOwner, len(owner), len(findings))

	if parsed < moduleManifestCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", parsed, moduleManifestCensusFloor)
	}
	if literals == 0 {
		t.Fatalf("осмотрено ноль строковых литералов на %d файлах — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", parsed)
	}
	// Предпосылка: объявление вообще ЕСТЬ. Ноль у владельца означает, что имя не
	// объявлено нигде, — и тогда гейт молчал бы и при исчезнувшем предмете.
	if len(owner) != len(moduleManifestSpellings) {
		t.Fatalf("у владельца %s объявлено %d форм(ы) из %d:\n  %s\n\n"+
			"Гейт беспредметен: он молчит и тогда, когда объявления не стало.",
			moduleManifestOwner, len(owner), len(moduleManifestSpellings),
			strings.Join(owner, "\n  "))
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("имя файла манифеста объявлено ВНЕ %s — %d место(а):\n  %s\n\n"+
			"Каждое такое место есть второе объявление одного соглашения. Расходятся они "+
			"МОЛЧА: часть манифестов перестаёт находиться, не дав ни одной находки, и «ноль "+
			"находок» становится неотличимо от «ноль прочитанного». Форм у имени ДВЕ — в "+
			"дереве и в доставке, — и объявление, знающее только одну, обещает читателю "+
			"полноту, которой у него нет.\n"+
			"Снятие: брать значение у %s, а не писать его своей рукой.",
			moduleManifestOwner, len(findings), strings.Join(findings, "\n  "), moduleManifestOwner)
	}

	for _, s := range owner {
		t.Logf("единственное объявление: %s", s)
	}
}
