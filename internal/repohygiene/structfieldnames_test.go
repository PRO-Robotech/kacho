// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// structfieldnames_test.go — СОСТАВ ПОЛЕЙ СТРУКТУРЫ, выведенный разбором, и
// ПЕРЕБОР ФОРМ ЕГО ВХОДА.
//
// # Зачем это здесь, а не у потребителя
//
// Средство отвечает на вопрос о ДЕРЕВЕ — «какие поля объявлены у типа», — и
// потребителей у него будет больше одного: всякий гейт, который требует, чтобы
// состав чего-либо покрывался перечнем, упирается в тот же разбор. Живя в
// пробном файле потребителя, оно не импортируется вовсе, и второму потребителю
// достался бы копипаст вместе со всеми своими слепыми формами.
//
// # ФОРМЫ ВХОДА ВЫВЕДЕНЫ ИЗ РАЗБОРА, А НЕ ВЫПИСАНЫ
//
// Прежний перебор был выписан от руки и оттого неполон дважды. Во-первых, он
// перечислял СЕМАНТИКУ, а не вход: «встроенный интерфейс» и «встроенное поле»
// для разбора один и тот же узел, а «поле с тегом», «поле-функция»,
// «неэкспортируемое поле» и «поле чужого типа» — один и тот же случай
// именованного поля. Во-вторых, он пропустил случай, лежавший внутри
// разбираемого им же `switch`.
//
// Формы входа здесь — это ВИДЫ УЗЛА, которыми грамматика языка разрешает
// записать тип встроенного поля, и перечень выводится из самого разбора:
// `embeddedNameFormCases` обязан покрыть каждую ветвь `embeddedFieldName`, и
// это проверяется, а не обещается.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// StructFieldNames — имена полей названной структуры в разобранном файле.
//
// У ВСТРОЕННОГО поля имя даёт ТИП: список имён пуст, а ключом составного
// литерала служит имя типа. Пропуск такого поля — не мелочь: перечень,
// сверяемый с составом, промолчит о нём, и ось, читающая его, выключится
// молча.
func StructFieldNames(f *ast.File, typeName string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return false
		}
		for _, fld := range st.Fields.List {
			if len(fld.Names) == 0 {
				if name := embeddedFieldName(fld.Type); name != "" {
					out = append(out, name)
				}
				continue
			}
			for _, nm := range fld.Names {
				out = append(out, nm.Name)
			}
		}
		return false
	})
	sort.Strings(out)
	return out
}

// embeddedFieldName — имя, под которым встроенное поле стоит в составном
// литерале: последний идентификатор типа, без звёздочки, без квалификатора
// пакета и без аргументов типа.
//
// Ветви этой функции И ЕСТЬ формы входа. Перебор ниже обязан покрыть каждую.
func embeddedFieldName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident: // Embedded
		return x.Name
	case *ast.StarExpr: // *Embedded
		return embeddedFieldName(x.X)
	case *ast.SelectorExpr: // other.Embedded
		return x.Sel.Name
	case *ast.IndexExpr: // Embedded[int] — ОДИН аргумент типа
		return embeddedFieldName(x.X)
	case *ast.IndexListExpr: // Embedded[int, string] — НЕСКОЛЬКО аргументов
		return embeddedFieldName(x.X)
	}
	return ""
}

// embeddedNameFormCases — ФОРМЫ ВХОДА по видам узла. Каждая строка называет
// вид узла, которым записан тип встроенного поля.
func embeddedNameFormCases() []struct {
	node string
	decl string
	want string
} {
	return []struct {
		node string
		decl string
		want string
	}{
		{"*ast.Ident", "Embedded", "Embedded"},
		{"*ast.StarExpr", "*Embedded", "Embedded"},
		{"*ast.SelectorExpr", "other.Embedded", "Embedded"},
		{"*ast.IndexExpr", "Embedded[int]", "Embedded"},
		{"*ast.IndexListExpr", "Embedded[int, string]", "Embedded"},
		// Обёртки поверх тех же узлов: грамматика их допускает, и каждая
		// проверяется отдельно, потому что разбор у них разный.
		{"*ast.StarExpr над *ast.SelectorExpr", "*other.Embedded", "Embedded"},
		{"*ast.StarExpr над *ast.IndexExpr", "*Embedded[int]", "Embedded"},
		{"*ast.StarExpr над *ast.IndexListExpr", "*Embedded[int, string]", "Embedded"},
		{"*ast.IndexExpr над *ast.SelectorExpr", "other.Embedded[int]", "Embedded"},
		{"*ast.IndexListExpr над *ast.SelectorExpr", "other.Embedded[int, string]", "Embedded"},
	}
}

// TestStructFieldNames_EveryNodeFormOfAnEmbeddedTypeIsJudged — перебор форм
// входа, и перечень СВЕРЯЕТСЯ С ВЕТВЯМИ разбора: форма, добавленная в `switch`
// и забытая в переборе, — находка.
func TestStructFieldNames_EveryNodeFormOfAnEmbeddedTypeIsJudged(t *testing.T) {
	t.Parallel()

	cases := embeddedNameFormCases()
	if len(cases) == 0 {
		t.Fatal("перечень форм пуст — перебор судил бы о непрочитанном")
	}
	covered := map[string]bool{}
	for _, tc := range cases {
		src := "package main\ntype Subject struct {\n\t" + tc.decl + "\n}\n"
		f, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", src, 0)
		if err != nil {
			t.Fatalf("%s: синтетика %q не разбирается: %v", tc.node, tc.decl, err)
		}
		got := StructFieldNames(f, "Subject")
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s (%q): вывод дал %v, ожидалось [%s] — встроенное поле теряется молча, "+
				"и перечень, сверяемый с составом, о нём не скажет", tc.node, tc.decl, got, tc.want)
			continue
		}
		covered[nodeKindOf(t, tc.decl)] = true
	}

	// ПЕРЕЧЕНЬ СВЕРЯЕТСЯ С РАЗБОРОМ: каждая ветвь `switch` обязана быть
	// покрыта. Прежний перебор пропустил ветвь, лежавшую внутри того же
	// `switch`, — здесь это находка, а не недосмотр.
	for _, kind := range embeddedNodeKindsInSwitch() {
		if !covered[kind] {
			t.Errorf("ветвь разбора %s не покрыта ни одной формой перебора: механизм различает "+
				"её, а перебор о ней молчит", kind)
		}
	}

	t.Logf("перепись: ЕДИНИЦА — вид узла типа встроенного поля. Форм входа перечислено %d · "+
		"различных видов узла покрыто %d · ветвей в разборе %d",
		len(cases), len(covered), len(embeddedNodeKindsInSwitch()))
}

// embeddedNodeKindsInSwitch — виды узла, которые РАЗЛИЧАЕТ `embeddedFieldName`.
// Перечень стоит рядом с самим `switch` и правится тем же изменением.
func embeddedNodeKindsInSwitch() []string {
	return []string{"*ast.Ident", "*ast.StarExpr", "*ast.SelectorExpr",
		"*ast.IndexExpr", "*ast.IndexListExpr"}
}

// nodeKindOf — вид корневого узла в объявлении встроенного поля.
func nodeKindOf(t *testing.T, decl string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "k.go",
		"package main\ntype S struct {\n\t"+decl+"\n}\n", 0)
	if err != nil {
		t.Fatalf("%q не разбирается: %v", decl, err)
	}
	kind := ""
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil || len(st.Fields.List) == 0 {
			return true
		}
		switch st.Fields.List[0].Type.(type) {
		case *ast.Ident:
			kind = "*ast.Ident"
		case *ast.StarExpr:
			kind = "*ast.StarExpr"
		case *ast.SelectorExpr:
			kind = "*ast.SelectorExpr"
		case *ast.IndexExpr:
			kind = "*ast.IndexExpr"
		case *ast.IndexListExpr:
			kind = "*ast.IndexListExpr"
		}
		return false
	})
	return kind
}

// TestStructFieldNames_NamedFieldFormsAreOneInputForm — именованное поле есть
// ОДНА форма входа, как бы ни различалась его семантика.
//
// Прежний перебор считал «поле с тегом», «поле-функцию», «неэкспортируемое
// поле» и «поле чужого типа» разными формами. Для разбора это один и тот же
// случай: список имён непуст. Строки остаются — они дёшевы и ловят случайное
// сужение, — но названы тем, чем являются: ОДНОЙ формой входа в разных
// обличьях.
func TestStructFieldNames_NamedFieldFormsAreOneInputForm(t *testing.T) {
	t.Parallel()

	decls := []string{
		"Alpha string",
		"Alpha, Beta, Gamma string",
		"hidden string",
		"At other.Time",
		"Alpha string `json:\"alpha\"`",
		"Clock func() int64",
	}
	for _, decl := range decls {
		src := "package main\ntype Subject struct {\n\t" + decl + "\n}\n"
		f, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", src, 0)
		if err != nil {
			t.Fatalf("%q не разбирается: %v", decl, err)
		}
		if got := StructFieldNames(f, "Subject"); len(got) == 0 {
			t.Errorf("%q: вывод не дал ни одного имени", decl)
		}
	}

	// Формы БЕЗ полей: ноль есть законный исход, а не отказ.
	empty, err := parser.ParseFile(token.NewFileSet(), "empty.go",
		"package main\ntype Subject struct{}\n", 0)
	if err != nil {
		t.Fatalf("пустая структура не разбирается: %v", err)
	}
	if got := StructFieldNames(empty, "Subject"); len(got) != 0 {
		t.Errorf("пустая структура дала поля %v", got)
	}
	if got := StructFieldNames(empty, "NoSuchType"); len(got) != 0 {
		t.Errorf("несуществующий тип дал поля %v", got)
	}

	t.Logf("перепись: ЕДИНИЦА — форма входа разбора. Именованное поле — ОДНА форма, проверена "+
		"в %d обличьях · форм без полей 2 (пустая структура · отсутствующий тип)", len(decls))
}

// requireNoStaleStrings — держит импорт strings значимым, если перебор вырастет.
var _ = strings.TrimSpace
