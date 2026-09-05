// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// reportprosematchestemplate_test.go — ПРОЗА ОТЧЁТА СВЕРЯЕТСЯ С ЛИТЕРАЛОМ,
// КОТОРЫЙ ЕЁ ПЕЧАТАЕТ.
//
// ПРЕДМЕТ (#877). Числа отчёта замера защищены отпечатком: гейт свежести сверяет
// их с деревом и краснеет, когда дерево ушло вперёд. Утверждения отчёта не
// защищены НИЧЕМ. Отчёт прочности дословно говорил «решение о доступе принимает
// движок отношений, реляционный вердикт — теневой» на дереве, где движка нет
// вовсе, и гейт свежести этого не ловил by construction: он сторожит отпечаток
// ПРЕДМЕТА ЧИСЕЛ, а проза живёт в проверочном файле, который под отпечаток не
// попадает.
//
// Устаревшее утверждение опаснее устаревшего числа: число читают с оглядкой на
// провенанс, а прозу принимают на веру.
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ — ТОЖДЕСТВО, А НЕ ЛЕКСИКОН. Заголовок отчёта печатается
// из строкового литерала в исходнике прибора. Литерал обязан присутствовать в
// каком-нибудь отчёте дерева ДОСЛОВНО. Правка литерала без пересъёмки отчёта
// делает вхождение ненаходимым — и это красное; правка обоих вместе оставляет
// вхождение на месте — и это молчание.
//
// Детектор по словарю («принимает», «отвечает», «источник») здесь уже проваливал
// контроль в обе стороны — это записано в корпусе правил, и повторять не нужно.
// Тождество строки такого недостатка не имеет: оно не судит смысл, оно сверяет
// байты.
//
// ГРАНИЦА ПРЕДМЕТА, названная, чтобы «зелено» не читалось шире:
//
//   - вызов, которому заголовок приходит ПЕРЕМЕННОЙ, литерала не имеет и в
//     предмет не входит. Такие вызовы считаются и печатаются отдельной строкой
//     переписи: ноль в ней означает «нечего было проверить», а не «всё сошлось».
//   - литерал ищется по ВСЕМ отчётам дерева, а не по одному, привязанному к
//     этому прибору. Привязка потребовала бы разрешать путь отчёта, который
//     вычисляется в рантайме; вместо этого проверка утверждает более слабое, но
//     достаточное: заголовок, которого нет НИ В ОДНОМ отчёте, не пережил своей
//     пересъёмки ни у одного прибора.
//   - куски короче порога не проверяются: строка в десяток символов совпадает
//     случайно, и находка на ней была бы ложной.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// tmpl — заголовок, напечатанный из литерала: где объявлен и что печатает.
type tmpl struct {
	where   string
	literal string
}

// minProseChunk — короче этого куски не сверяются: случайное совпадение.
const minProseChunk = 24

// reportGlobs — где лежат отчёты приборов. Перечень ЗДЕСЬ, потому что имя
// каталога — свойство прибора, а не дерева, и вывести его неоткуда.
var reportGlobs = []string{
	"services/iam/internal/repo/kacho/pg/scalegrid/REPORT-*.txt",
	"services/iam/tools/authzformbench/REPORT-*.txt",
	"services/iam/tests/k6/results/*.md",
	"services/vpc/tests/k6/results/*.md",
}

func TestReportProseMatchesTheTemplateThatPrintsIt(t *testing.T) {
	root := repoRoot(t)

	// ── отчёты ──
	var reports []string
	corpus := map[string]string{}
	for _, g := range reportGlobs {
		matches, err := treecorpus.Glob(filepath.Join(root, g))
		// Отсутствующая база НЕ смягчается: перечень reportGlobs объявлен руками,
		// и запись, под которой в индексе нет ничего, — это запись без предмета.
		// Смолчав здесь, гейт унаследовал бы её как слепое пятно.
		if err != nil {
			t.Fatalf("перебор %s: %v", g, err)
		}
		for _, m := range matches {
			b, err := os.ReadFile(m)
			if err != nil {
				t.Fatalf("чтение отчёта %s: %v", m, err)
			}
			rel, _ := filepath.Rel(root, m)
			reports = append(reports, rel)
			corpus[rel] = string(b)
		}
	}

	// ── шаблоны ──
	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	var templates []tmpl
	withoutLiteral := 0
	scanned := 0

	fset := token.NewFileSet()
	for _, abs := range files {
		src, err := os.ReadFile(abs)
		if err != nil || !strings.Contains(string(src), ".Header(") {
			continue
		}
		f, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			f = abs
		}
		file, err := parser.ParseFile(fset, abs, src, 0)
		if err != nil {
			continue
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Header" || len(call.Args) == 0 {
				return true
			}
			// Получатель обязан быть провенансом, а не http-заголовком.
			ident, ok := sel.X.(*ast.Ident)
			if !ok || !strings.Contains(strings.ToLower(ident.Name), "prov") {
				return true
			}
			lits := stringLiteralsOf(call.Args[0])
			if len(lits) == 0 {
				withoutLiteral++
				return true
			}
			pos := fset.Position(call.Pos())
			for _, l := range lits {
				if len([]rune(l)) < minProseChunk {
					continue
				}
				templates = append(templates, tmpl{
					where:   f + ":" + strconv.Itoa(pos.Line),
					literal: l,
				})
			}
			return true
		})
	}

	t.Logf("осмотрено: файлов с вызовом заголовка %d · шаблонов с литералом %d · "+
		"вызовов без литерала %d (вне предмета) · отчётов прочитано %d",
		scanned, len(templates), withoutLiteral, len(reports))

	// Предпосылка: пустой обход обязан падать, иначе «ноль находок» неотличим
	// от «ноль прочитанного».
	if len(reports) == 0 {
		t.Fatalf("отчётов не найдено ни по одному образцу — проверять нечего; "+
			"либо каталоги переехали, либо образцы устарели: %v", reportGlobs)
	}
	if len(templates) == 0 {
		t.Fatalf("шаблонов с литералом не найдено при %d осмотренных файлах — "+
			"разбор перестал узнавать вызов заголовка", scanned)
	}

	missing := proseMissingFromReports(templates, corpus)

	if len(missing) > 0 {
		t.Fatalf("заголовок шаблона не найден ни в одном отчёте (%d из %d):\n  %s\n\n"+
			"Это значит, что шаблон правили, а отчёт не пересняли: проза отчёта "+
			"утверждает не то, что печатает прибор. Пересними отчёт прогоном прибора "+
			"либо, если заголовок изменён осознанно, — тем же изменением.",
			len(missing), len(templates), strings.Join(missing, "\n  "))
	}
}

// stringLiteralsOf собирает строковые литералы выражения, включая конкатенацию:
// заголовок часто склеен из строки и переменных, и статическая часть у него всё
// равно есть.
func stringLiteralsOf(e ast.Expr) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		// РАСКАВЫЧИВАТЬ ОБЯЗАТЕЛЬНО, а не срезать кавычки.
		//
		// Срез оставляет escape-последовательности как есть: `\n` в исходнике —
		// два символа, а в отчёте на его месте стоит один, настоящий перевод
		// строки. Сверка сырого литерала давала ЛОЖНУЮ находку на первом же
		// многострочном заголовке — то есть гейт краснел бы на исправном дереве,
		// а такой гейт снимают первым.
		s, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return true
		}
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
		return true
	})
	return out
}

func shortenProse(s string) string {
	r := []rune(s)
	if len(r) <= 60 {
		return s
	}
	return string(r[:57]) + "…"
}

// proseMissingFromReports — РЕШАЮЩАЯ ЧАСТЬ, вынесенная отдельно ради инъекции.
//
// Внутри теста её нельзя было бы проверить на синтетике: чтобы доказать, что
// сверка ЗАМЕЧАЕТ расхождение, нужен вход, которого в дереве нет и не должно
// быть. Функция принимает корпус параметром, поэтому инъекция подаёт свой.
func proseMissingFromReports(templates []tmpl, corpus map[string]string) []string {
	var missing []string
	for _, tm := range templates {
		found := false
		for _, body := range corpus {
			if strings.Contains(body, tm.literal) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, tm.where+" — заголовок «"+shortenProse(tm.literal)+"»")
		}
	}
	return missing
}
