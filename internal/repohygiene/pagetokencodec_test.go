// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestPageCursorFormIsDeclaredOnce — форма курсора страницы объявлена ОДИН раз
// на дерево (#652).
//
// # Предмет
//
// Форма была записана трижды: авторитетный кодек в слое репозитория, рукописное
// зеркало в проверке формата — и оно бежит ПЕРВЫМ — и свой декодер в общем
// пакете. Пока значения совпадают, это не расхождение, а заряженная ловушка:
// смена формы вносится в три места, и первое же пропущенное даёт ответ,
// зависящий от того, какой декодер отработал раньше. Оба при этом отвечают
// «валидно» на валидном входе — то есть расходятся ровно там, где расхождение
// не видно.
//
// Зеркало и правда оказалось СЛАБЕЕ авторитетного: пустой идентификатор оно
// принимало, а чтение отвергало. То есть проверка, стоящая первой, пропускала
// токен, на котором путь чтения падал ниже.
//
// # Признак
//
// Разбор тела токена: `time.Parse(time.RFC3339Nano, …)` над результатом
// `base64…DecodeString`. Так выглядит РАЗБОР ФОРМЫ, а не всякая работа с
// base64 (её в дереве много: ключи, подписи, схемы) и не всякий разбор времени.
//
// Единственное законное место — `pkg/pagetoken`. Всё прочее обязано звать его.
func TestPageCursorFormIsDeclaredOnce(t *testing.T) {
	const home = "pkg/pagetoken"

	roots := []string{"../../pkg", "../../services", "../../gateway"}

	var scanned int
	sites := map[string]bool{}
	for _, root := range roots {
		files, err := treecorpus.UnderWithSuffix(root, ".go")
		if err != nil {
			t.Fatalf("перечислить %s: %v", root, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/pkg/api/") {
				continue
			}
			scanned++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue
			}
			if parsesCursorBody(f) {
				sites[relToRepo(path)] = true
			}
		}
	}

	var names []string
	for s := range sites {
		names = append(names, s)
	}
	sort.Strings(names)

	t.Logf("осмотрено прод-файлов %d; мест, разбирающих тело курсора: %d %v",
		scanned, len(sites), names)

	if scanned == 0 {
		t.Fatal("корпус пуст — «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if len(sites) == 0 {
		t.Fatal("ни одного разбора формы не распознано: признак разошёлся с деревом, " +
			"и гейт стал тождественно-зелёным — а объявление обязано существовать")
	}

	for _, name := range names {
		if strings.Contains(name, home) {
			continue
		}
		t.Errorf("%s: вторая запись формы курсора. Объявление одно — %s; "+
			"остальные обязаны ЗВАТЬ его, а не повторять: guard, бегущий первым, "+
			"иначе разойдётся с путём чтения молча", name, home)
	}
}

// parsesCursorBody — разбирает ли файл ТЕЛО токена: время в формате RFC3339Nano
// над результатом декодирования base64.
//
// Оба признака нужны вместе: base64 в дереве встречается всюду (ключи, подписи,
// схемы), а `time.Parse` — в разборе конфигурации и отметок ресурсов. Форму
// курсора образует именно их сочетание в одном файле.
func parsesCursorBody(f *ast.File) bool {
	sawB64, sawNano := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "DecodeString" {
			sawB64 = true
		}
		if sel.Sel.Name == "Parse" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" {
				for _, a := range call.Args {
					if s, ok := a.(*ast.SelectorExpr); ok && s.Sel.Name == "RFC3339Nano" {
						sawNano = true
					}
				}
			}
		}
		return true
	})
	return sawB64 && sawNano
}

// relToRepo — путь от корня репозитория, чтобы координата в отказе читалась.
func relToRepo(abs string) string {
	for _, marker := range []string{"/pkg/", "/services/", "/gateway/"} {
		if i := strings.Index(abs, marker); i >= 0 {
			return abs[i+1:]
		}
	}
	return abs
}
