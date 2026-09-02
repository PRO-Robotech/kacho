// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestverbclassrule_test.go — два свойства ДЕРЕВА, которых проба пакета
// утверждать не может (приёмка §2.4, §2.5; сценарии MOD-MR-03 и MOD-MR-06,
// третий отрицательный).
//
//  1. правило «класс действия из его ИМЕНИ» объявлено в дереве РОВНО ОДИН РАЗ.
//     Проба пакета о числе объявлений не утверждает ничего: она зелена при
//     любом. Тождество импорта («генератор зовёт ту же функцию») сегодня
//     непроверяемо — генератора нет, — а единственность объявления делает
//     второе место НЕПРЕДСТАВИМЫМ, и это сильнее: появится генератор —
//     импортировать ему будет нечего, кроме одной функции, by construction;
//
//  2. правило вывода `objectType ← <module>_<resource>` СНЯТО и не возвращено
//     тихо. Приёмка сняла его замером (не действует у 10 записей закрытой
//     таблицы из 27), и без держателя его можно вернуть молча: ключ перестал бы
//     быть обязательным, а восстанавливающее правило поселилось бы в двух
//     местах — в генераторе (когда опускать) и в загрузчике (как восстановить).
//
// # Как распознаётся правило «класс из имени» — и почему НЕ по набору литералов
//
// Наивный распознаватель («объявление, чей набор строковых литералов равен пяти
// каноническим») даёт ЛОЖНУЮ находку: в дереве живёт `verbDisplayPrecedence` —
// ПОРЯДОК ПОКАЗА глаголов, набор у которого тот же, а референт другой. Гейт,
// краснеющий на верном коде, отключают первым.
//
// Поэтому распознаётся ПАРА: функция, отвечающая «класс и получилось ли»
// (результаты `(string, bool)`), и набор ровно пяти канонических токенов, до
// которого она дотягивается — своим телом либо объявлением того же файла.
// Порядок показа под это не подпадает: его читает функция с ОДНИМ результатом.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// canonicalClassTokens — те же пять токенов, что объявляет контракт манифеста.
// Здесь они ОБРАЗЕЦ распознавателя, а не второе объявление правила: гейт по ним
// ищет, а не выводит из них класс.
var canonicalClassTokens = map[string]bool{
	"get": true, "list": true, "create": true, "update": true, "delete": true,
}

// classRuleStringLiterals — множество НЕПУСТЫХ строковых литералов узла.
//
// Пустая строка исключается намеренно, и это не украшение: правило, написанное
// не общим набором, а собственным `switch`, возвращает `return "", false` на
// неканоническом имени — и наивный распознаватель, считая `""` шестым токеном,
// такую копию НЕ НАШЁЛ БЫ. Форма законна и распространена; распознаватель,
// который её не знает, МОЛЧИТ, а не краснеет (`testing.md` §«Гейт на класс»,
// п. 7). Поймано инъекцией, а не чтением.
func classRuleStringLiterals(n ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if b, ok := x.(*ast.BasicLit); ok && b.Kind == token.STRING {
			if s, err := strconv.Unquote(b.Value); err == nil && s != "" {
				out[s] = true
			}
		}
		return true
	})
	return out
}

// isCanonicalClassSet — множество литералов РАВНО пяти каноническим.
//
// Равенство, а не включение: включение поймало бы расширенный классификатор
// яруса (30 токенов), который классом действия не занимается вовсе.
func isCanonicalClassSet(set map[string]bool) bool {
	if len(set) != len(canonicalClassTokens) {
		return false
	}
	for k := range set {
		if !canonicalClassTokens[k] {
			return false
		}
	}
	return true
}

// answersClassAndOK — результаты функции суть «класс и получилось ли».
func answersClassAndOK(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
		return false
	}
	first, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || first.Name != "string" {
		return false
	}
	second, ok := fn.Type.Results.List[1].Type.(*ast.Ident)
	return ok && second.Name == "bool"
}

// classRuleDeclarationsIn — объявления правила «класс из имени» в одном файле,
// с координатой каждого.
func classRuleDeclarationsIn(fset *token.FileSet, file *ast.File, rel string) []string {
	// Наборы ровно пяти канонических токенов, объявленные на уровне файла.
	fileSets := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) && isCanonicalClassSet(classRuleStringLiterals(vs.Values[i])) {
					fileSets[name.Name] = true
				}
			}
		}
	}

	var out []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !answersClassAndOK(fn) {
			continue
		}
		reaches := isCanonicalClassSet(classRuleStringLiterals(fn.Body))
		if !reaches {
			ast.Inspect(fn.Body, func(x ast.Node) bool {
				if id, ok := x.(*ast.Ident); ok && fileSets[id.Name] {
					reaches = true
				}
				return !reaches
			})
		}
		if reaches {
			out = append(out, rel+":"+itoa(fset.Position(fn.Pos()).Line)+" "+fn.Name.Name)
		}
	}
	return out
}

// TestVerbClassRuleIsDeclaredOnce — правило объявлено РОВНО ОДИН РАЗ.
//
// Ноль объявлений — тоже находка, и она о распознавателе: «объявлений ноль»
// означало бы, что гейт ослеп, а не что правило исчезло.
func TestVerbClassRuleIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	fset := token.NewFileSet()
	var declarations []string
	scanned := 0
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") ||
			strings.HasPrefix(rel, "pkg/api/") || strings.HasPrefix(rel, "vendor/") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if err != nil {
			continue
		}
		scanned++
		declarations = append(declarations, classRuleDeclarationsIn(fset, file, rel)...)
	}

	if scanned == 0 {
		t.Fatal("обход не прочитал ни одного не-тестового файла Go — вердикт был бы " +
			"свойством обхода, а не дерева")
	}
	switch {
	case len(declarations) == 0:
		t.Errorf("объявлений правила «класс действия из его имени» НОЛЬ при %d прочитанных "+
			"файлах — либо правило сняли, либо распознаватель ослеп; в обоих случаях "+
			"молчание про второе объявление не значит ничего", scanned)
	case len(declarations) > 1:
		t.Errorf("правило «класс действия из его имени» объявлено %d раза:\n  %s\n\n"+
			"Второе объявление разойдётся с первым МОЛЧА: обе стороны отвечают одинаково "+
			"на входе, где правило совпадает. Оставь одно — `manifest.ClassOfCanonicalVerb` — "+
			"и зови его; остальные снимай вместе с их вызывающими.",
			len(declarations), strings.Join(declarations, "\n  "))
	}
	t.Logf("перепись: прочитано не-тестовых файлов Go %d · объявлений правила %d (%s)",
		scanned, len(declarations), strings.Join(declarations, ", "))
}

// manifestLoaderDir — прод-файлы загрузчика манифеста. Гейт читает их как ТЕКСТ
// дерева: импортировать пакет отсюда нельзя (правило видимости `internal`).
const manifestLoaderDir = "services/iam/internal/manifest"

// TestObjectTypeIsNeverDerivedFromTheResourceName — правило вывода снято, и
// возвращено тихо быть не может.
//
// Признак прямой: поле `ObjectType` в прод-коде загрузчика только ЧИТАЕТСЯ. Как
// только оно начнёт куда-то ПРИСВАИВАТЬСЯ, значит появился путь, который его
// восстанавливает, — а восстанавливать нечем: правило не действует у 10 записей
// закрытой таблицы из 27, и автор всё равно обязан знать, попал ли его ресурс в
// исключение.
func TestObjectTypeIsNeverDerivedFromTheResourceName(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	fset := token.NewFileSet()
	files, reads := 0, 0
	var writes []string
	for _, rel := range rels {
		if !strings.HasPrefix(rel, manifestLoaderDir+"/") ||
			!strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if err != nil {
			t.Fatalf("%s не разбирается: %v", rel, err)
		}
		files++
		r, w := objectTypeReadsAndWrites(fset, file, rel)
		reads += r
		writes = append(writes, w...)
	}

	if files == 0 {
		t.Fatalf("прод-файлов загрузчика по пути %s не прочитано ни одного — "+
			"«присваиваний ноль» было бы свойством обхода, а не дерева", manifestLoaderDir)
	}
	// Положительный контроль: поле обязано хотя бы ЧИТАТЬСЯ. Ноль чтений
	// означал бы, что распознаватель не находит поле вовсе, и тогда «ноль
	// присваиваний» ничего не значит.
	if reads == 0 {
		t.Fatalf("поле ObjectType не читается ни в одном из %d прод-файлов — "+
			"распознаватель ослеп", files)
	}
	for _, w := range writes {
		t.Errorf("поле ObjectType ПРИСВАИВАЕТСЯ в %s — значит завёлся путь, который его "+
			"восстанавливает. Правило вывода «тип = <модуль>_<ресурс>» снято замером: оно "+
			"не действует у 10 записей закрытой таблицы из 27, и живёт такое правило сразу "+
			"в двух местах — в генераторе (когда опускать ключ) и здесь (как восстановить).", w)
	}
	t.Logf("перепись: прод-файлов загрузчика %d · чтений ObjectType %d · присваиваний %d",
		files, reads, len(writes))
}

// objectTypeReadsAndWrites — сколько раз поле читается и где присваивается.
func objectTypeReadsAndWrites(fset *token.FileSet, file *ast.File, rel string) (reads int, writes []string) {
	isObjectType := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		return ok && sel.Sel != nil && sel.Sel.Name == "ObjectType"
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if isObjectType(lhs) {
					writes = append(writes, rel+":"+itoa(fset.Position(lhs.Pos()).Line))
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := node.Key.(*ast.Ident); ok && id.Name == "ObjectType" {
				writes = append(writes, rel+":"+itoa(fset.Position(node.Pos()).Line))
			}
		case *ast.SelectorExpr:
			if isObjectType(node) {
				reads++
			}
		}
		return true
	})
	return reads, writes
}
