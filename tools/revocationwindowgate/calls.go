// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package revocationwindowgate

// calls.go — третья полоса того же предмета, взятая со стороны ВЫЗОВА.
//
// # Что осталось непокрытым и почему это нашлось только инъекцией
//
// `implicit.go` берёт предметом ЛИТЕРАЛ `InterceptorOptions`, и берёт его
// осознанно: дерево пользуется двумя формами — литерал прямо в вызове
// (vpc/compute/geo/storage/registry) и литерал, вынесенный в переменную (nlb),
// — а предикат по аргументу вызова вторую форму пропускал бы. Довод верен, и
// эта полоса его не отменяет.
//
// Неверным было следствие, которое из него вывели: «форма без литерала в
// дереве не встречается, значит предикат по литералу накрывает всё, что есть».
// Предпосылка была записана как проверка (`literalsSeen == 0` — нарушенная
// предпосылка), но проверка эта СУММАРНАЯ по дереву: пока хоть одна площадка
// собирает опции литералом — а их восемь, — она не срабатывает никогда.
// Предпосылка объявлена про КАЖДУЮ площадку, а проверяется про дерево целиком,
// поэтому площадка, собравшая опции присвоением полей, невидима, и невидима
// именно тем механизмом, который был написан её ловить.
//
// Проверено инъекцией на настоящем дереве: седьмой сервис, строящий интерсептор
// из `var o authz.InterceptorOptions` с присвоением полей, прошёл ВСЕ четыре
// проверки гейта зелёным, а число прочитанных файлов при этом выросло на
// единицу — то есть файл был прочитан и объявлен чистым.
//
// # Предикат
//
// Предмет здесь — ВЫЗОВ `authz.NewInterceptor`, а не литерал. Для каждого
// вызова спрашивается: доказуемо ли в этом файле, что кеш назван?
//
//   - аргумент — литерал `InterceptorOptions` ⇒ предмет переписи литералов,
//     здесь НЕ дублируется (иначе одна площадка стоила бы двух находок и
//     вердикт перестал бы быть числом);
//   - аргумент — переменная, которой в объемлющей функции присвоен литерал
//     `InterceptorOptions` ⇒ то же самое, предмет переписи литералов;
//   - аргумент — переменная с присвоением `x.Cache = …` ⇒ кеш назван, молчим;
//   - всё остальное (переменная без литерала и без присвоения кеша, значение из
//     чужой функции, поле структуры) ⇒ НАХОДКА.
//
// Последняя ветка fail-closed намеренно: «доказать не смог» — это не «чисто».
// Гейт про окно отзыва доступа, а не про стиль; корзины «всё остальное, и
// ладно» у него быть не может.
//
// # Отношение к отказу в конструкторе
//
// `authz.NewInterceptor` отказывает в старте на неназванный кеш, какой бы
// формой опции ни собрали, и остаётся исчерпывающей защитой. Эта полоса нужна
// раньше: она называет файл и строку тогда, когда процесс ещё никто не
// поднимал, — ровно то, что `implicit.go` обещал и на этой форме не держал.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// CallReport — исход разбора одного файла со стороны вызова, вместе с объёмом
// осмотренного.
//
// CallsSeen отделяет «вызовы прочитаны и все доказуемо назвали кеш» от «вызовов
// не встретилось». Без этого числа молчание значит сразу и то и другое.
type CallReport struct {
	CallsSeen int
	Sites     []ImplicitSite
}

// ScanInterceptorCalls разбирает один Go-файл и возвращает каждый вызов
// `authz.NewInterceptor`, про который в этом файле НЕЛЬЗЯ доказать, что кеш
// вердиктов назван.
//
// Разбор, а не текстовый поиск: godoc конструктора содержит ровно эту форму
// вызова, и перепись по тексту объявила бы комментарий площадкой.
func ScanInterceptorCalls(service, path, src string) (CallReport, error) {
	var rep CallReport

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return rep, fmt.Errorf("parse %s: %w", path, err)
	}

	// Объемлющая функция нужна, чтобы разрешить переменную-аргумент: именно в её
	// теле лежит либо литерал, либо присвоение поля кеша. Вызов вне какой-либо
	// функции (инициализация пакетного уровня) объемлющего тела не имеет —
	// доказывать нечем, значит находка.
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		scanBody(&rep, fset, service, path, fn.Body)
		return true
	})

	// Вызовы вне тела функции: их не увидел обход выше, но они существуют
	// (`var intr = authz.NewInterceptor(...)`). Тела у них нет, поэтому доказать
	// имя кеша можно только литералом прямо в аргументе.
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		scanBody(&rep, fset, service, path, gen)
	}

	return rep, nil
}

// scanBody осматривает вызовы конструктора внутри одного узла-владельца (тело
// функции либо объявление пакетного уровня) и разрешает аргумент в пределах
// этого же узла.
func scanBody(rep *CallReport, fset *token.FileSet, service, path string, owner ast.Node) {
	ast.Inspect(owner, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isNewInterceptor(call.Fun) || len(call.Args) == 0 {
			return true
		}
		rep.CallsSeen++
		if callNamesCache(owner, call.Args[0]) {
			return true
		}
		rep.Sites = append(rep.Sites, ImplicitSite{
			Service: service,
			File:    path,
			Line:    fset.Position(call.Pos()).Line,
		})
		return true
	})
}

// isNewInterceptor распознаёт конструктор в обеих формах, которыми пользуется
// дерево: `authz.NewInterceptor` из сервиса и голый `NewInterceptor` внутри
// самого пакета.
func isNewInterceptor(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		return ok && pkg.Name == "authz" && t.Sel.Name == "NewInterceptor"
	case *ast.Ident:
		return t.Name == "NewInterceptor"
	}
	return false
}

// callNamesCache сообщает, доказуемо ли в пределах owner, что кеш назван.
//
// Литерал (прямо в вызове или присвоенный переменной) считается ДОКАЗАННЫМ
// здесь намеренно: он — предмет переписи литералов, и она уже спрашивает, назван
// ли в нём кеш. Удваивать находку значило бы удваивать цену одной площадки в
// вердикте.
func callNamesCache(owner ast.Node, arg ast.Expr) bool {
	if lit, ok := arg.(*ast.CompositeLit); ok && isInterceptorOptions(lit.Type) {
		return true
	}
	ident, ok := arg.(*ast.Ident)
	if !ok {
		// Значение пришло из чужой функции, поля структуры, индекса. Доказать
		// нечем — fail-closed: «не смог посмотреть» это не «чисто».
		return false
	}

	proven := false
	ast.Inspect(owner, func(n ast.Node) bool {
		if proven {
			return false
		}
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				// x := authz.InterceptorOptions{…} / x = authz.InterceptorOptions{…}
				if id, ok := lhs.(*ast.Ident); ok && id.Name == ident.Name && i < len(s.Rhs) {
					if lit, ok := s.Rhs[i].(*ast.CompositeLit); ok && isInterceptorOptions(lit.Type) {
						proven = true
						return false
					}
				}
				// x.Cache = …
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cache" {
					if base, ok := sel.X.(*ast.Ident); ok && base.Name == ident.Name {
						proven = true
						return false
					}
				}
			}
		case *ast.ValueSpec:
			// var x = authz.InterceptorOptions{…}
			for i, name := range s.Names {
				if name.Name != ident.Name || i >= len(s.Values) {
					continue
				}
				if lit, ok := s.Values[i].(*ast.CompositeLit); ok && isInterceptorOptions(lit.Type) {
					proven = true
					return false
				}
			}
		}
		return true
	})
	return proven
}
