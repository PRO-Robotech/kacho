// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Имя типа ресурса и пространство ключа повторной подачи — ОДНО значение, а не два похожих.
//
// # Что здесь проверяется и почему это не косметика
//
// Провайдер называет своё имя типа в `Metadata`, а на край шлёт ключ повторной подачи,
// первым слагаемым которого стоит ТО ЖЕ имя. Пока это два написания, они могут разойтись
// молча: обе стороны по отдельности защитимы, компилятор различия не видит, а обзор
// изменения видит одну строку из двух. Разойдясь, они дают не отказ, а тихую потерю защиты
// от дубля: повтор запроса после потерянного ответа приносит ДРУГОЙ ключ, край считает его
// новым намерением и заводит второй ресурс.
//
// # Почему сверка, а не «мы же свели к одной константе»
//
// Свести к одной константе — правильно, и для типов доступа это сделано. Но константа
// сводит только ТЕ места, которые её читают: литерал, вписанный на месте, остаётся законной
// формой языка и появится снова при следующем ресурсе, написанном по образцу соседа.
// Поэтому проверяется СВОЙСТВО — «оба написания дают одно значение», — а не наличие
// константы: свойство переживает смену формы записи, а требование константы её запрещало бы
// без нужды.
//
// # Почему разбор, а не поиск по образцу
//
// Имя типа встречается в этом пакете в прозе комментариев, в текстах отказов и в примерах
// документации ресурса. Поиск по образцу не отличает объявление от рассказа о нём и краснел
// бы на собственном объяснении. Судится узел синтаксического дерева: присваивание
// `resp.TypeName` и аргумент вызова.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// createCallSites — вызовы, первым содержательным аргументом которых идёт пространство
// ключа повторной подачи, и позиция этого аргумента.
//
// Оба пути настоящие: общий (`awaitCreate` доводит создание до операции) и прямой (ресурсы,
// чьё создание операцией не заканчивается). Предикат, знающий один, молчал бы о половине
// ресурсов.
var createCallSites = map[string]int{
	"client.IdempotencyKey": 0,
	"awaitCreate":           4,
}

func TestIdempotencyNamespaceAgreesWithTheDeclaredTypeName(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог пакета не прочитан: %v", err)
	}

	// Константы строк собираются ПО ВСЕМУ пакету: объявление имени и его использование
	// лежат в разных файлах by construction — в том и смысл единого словаря.
	consts := map[string]string{}
	files := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("разбор %s: %v", name, err)
		}
		files[name] = f
		collectStringConsts(f, consts)
	}
	if len(files) == 0 {
		t.Fatal("в пакете не прочитано ни одного не-тестового файла — обход пуст, вердикт беспредметен")
	}

	var (
		declaredCount int
		checkedCount  int
		derivedCount  int
		throughCount  int
		names         []string
		withoutSite   []string
	)

	for _, name := range sortedFileNames(files) {
		f := files[name]
		declared, declaredPos, ok := declaredTypeNameOf(f, consts)
		sites, unresolved := createNamespaceArgs(f, consts)

		if ok {
			declaredCount++
			names = append(names, display(declared))
			if len(sites) == 0 && unresolved == 0 {
				// Ресурс, чьё создание в этом файле не найдено, сверке недоступен.
				// Названный числом, он виден; спрятанный в «сошлось» — нет.
				withoutSite = append(withoutSite, name)
			}
		}
		// Аргумент, сводимый только к параметру функции, — общий путь: имя типа приходит
		// туда вызывающим и сверено у него. Названо отдельным числом, а не пропущено молча.
		throughCount += unresolved

		for _, site := range sites {
			if !ok {
				throughCount++
				continue
			}
			checkedCount++
			if site.symbolic {
				derivedCount++
			}
			if site.value == declared {
				continue
			}
			t.Errorf("%s: имя типа и пространство ключа повторной подачи разошлись.\n"+
				"  объявлено в Metadata (%s): %s\n"+
				"  отправляется на край (%s): %s\n"+
				"Ключ повторной подачи считается от имени типа: разойдясь, два написания "+
				"тихо снимают защиту от дубля — повтор после потерянного ответа приносит "+
				"другой ключ, и край заводит второй ресурс.",
				name,
				fset.Position(declaredPos).String(), display(declared),
				fset.Position(site.pos).String(), display(site.value))
		}
	}

	if checkedCount == 0 {
		t.Fatal("сверено ноль вызовов создания — предикат обхода устарел, и «расхождений нет» " +
			"означало бы «ничего не прочитано»")
	}
	if declaredCount == 0 {
		t.Fatal("объявлений имени типа не найдено — сверять не с чем")
	}

	sort.Strings(names)
	sort.Strings(withoutSite)
	t.Logf("осмотрено: файлов %d, объявлений имени типа %d, сверено вызовов создания %d "+
		"(из них по выражению-источнику %d), на общем пути %d",
		len(files), declaredCount, checkedCount, derivedCount, throughCount)
	t.Logf("имена типов: %s", strings.Join(names, ", "))
	if len(withoutSite) > 0 {
		t.Logf("объявляют имя типа, но создания в своём файле не содержат (сверке недоступны): %s",
			strings.Join(withoutSite, ", "))
	}
}

// resourceMetadataFunc — метод `Metadata` РЕСУРСА, если файл его объявляет.
func resourceMetadataFunc(f *ast.File) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Metadata" || fn.Type.Params == nil {
			continue
		}
		params := fn.Type.Params.List
		if len(params) == 0 {
			continue
		}
		star, ok := params[len(params)-1].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if exprText(star.X) == "resource.MetadataResponse" {
			return fn
		}
	}
	return nil
}

// namespaceArg — аргумент вызова, задающий пространство ключа.
type namespaceArg struct {
	value string
	pos   token.Pos
	// symbolic означает «значение не строка, а само выражение»: имя типа приходит из
	// таблицы описаний в момент работы, и статически его не вычислить. Сверка тогда идёт
	// по ВЫРАЖЕНИЮ — то же самое выражение с обеих сторон означает одно значение
	// by construction, а разные выражения означают два независимых пути.
	symbolic bool
}

// declaredTypeNameOf — имя типа, объявляемое файлом в его Metadata.
func declaredTypeNameOf(f *ast.File, consts map[string]string) (string, token.Pos, bool) {
	var (
		value string
		pos   token.Pos
		found bool
	)
	// Метод `Metadata` есть и у САМОГО провайдера — он называет своё имя тем же полем.
	// Различает не имя переменной (у обоих `resp`), а тип ответа: ресурс отвечает
	// `*resource.MetadataResponse`, провайдер — `*provider.MetadataResponse`. Спутать их
	// значило бы сверять имя провайдера с пространством ключа ресурса.
	decl := resourceMetadataFunc(f)
	if decl == nil {
		return "", 0, false
	}
	ast.Inspect(decl, func(n ast.Node) bool {
		asn, ok := n.(*ast.AssignStmt)
		if !ok || len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
			return true
		}
		sel, ok := asn.Lhs[0].(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TypeName" {
			return true
		}
		// Провайдер называет СЕБЯ тем же полем; его объявление к типам ресурсов
		// отношения не имеет и сверке не подлежит.
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "resp" {
			if v, symbolic := resolveTypeExpr(asn.Rhs[0], consts); v != "" || symbolic {
				value, pos, found = v, asn.Pos(), true
				return false
			}
		}
		return true
	})
	return value, pos, found
}

// createNamespaceArgs — аргументы пространства ключа во всех вызовах создания файла.
//
// Второе значение — число аргументов, не сводимых ни к значению, ни к выражению-источнику:
// это общий путь, куда имя приходит параметром. Возвращается числом, а не пропускается
// молча: иначе «сверено N» не отличалось бы от «прочитано N».
func createNamespaceArgs(f *ast.File, consts map[string]string) ([]namespaceArg, int) {
	var out []namespaceArg
	unresolved := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		idx, ok := createCallSites[calleeName(call.Fun)]
		if !ok || idx >= len(call.Args) {
			return true
		}
		v, symbolic := resolveTypeExpr(call.Args[idx], consts)
		if v == "" && !symbolic {
			unresolved++
			return true
		}
		out = append(out, namespaceArg{value: v, pos: call.Args[idx].Pos(), symbolic: symbolic})
		return true
	})
	return out, unresolved
}

// resolveTypeExpr сводит выражение к значению имени типа либо к самому выражению.
//
// Форм ЧЕТЫРЕ, и все четыре настоящие: строковый литерал, склейка с именем провайдера,
// константа пакета и поле описания ресурса. Предикат, знающий три из четырёх, объявил бы
// часть ресурсов несверяемыми — и молчал бы там, где расхождение как раз и опасно.
func resolveTypeExpr(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, false
	case *ast.Ident:
		if s, ok := consts[v.Name]; ok {
			return s, false
		}
		return "", false
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left := exprText(v.X)
		right, _ := resolveTypeExpr(v.Y, consts)
		if left == "req.ProviderTypeName" && right != "" {
			return providerTypeName + right, false
		}
		return "", false
	case *ast.SelectorExpr:
		// `r.spec.tfName` и `r.kind.tfName` — имя приходит из таблицы описаний и
		// вычисляется в момент работы. Сверяется само выражение: одинаковое с обеих
		// сторон означает одно значение, разное — два независимых пути.
		if v.Sel.Name != "tfName" {
			return "", false
		}
		return exprText(v), true
	}
	return "", false
}

// calleeName — имя вызываемого: `f`, `pkg.F` либо `x.f`.
func calleeName(e ast.Expr) string { return exprText(e) }

// exprText — выражение в виде текста, достаточного для сравнения его с самим собой.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	}
	return ""
}

// collectStringConsts — строковые константы пакета.
func collectStringConsts(f *ast.File, out map[string]string) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil {
					out[name.Name] = s
				}
			}
		}
	}
}

func sortedFileNames(m map[string]*ast.File) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// display — значение в тексте отказа; пустое означает «выражение, а не строка».
func display(v string) string {
	if v == "" {
		return "(выражение)"
	}
	return strconv.Quote(v)
}
