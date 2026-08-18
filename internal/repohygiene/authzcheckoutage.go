// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authzcheckoutage.go — разбор стража, который схлопывает «спросить не удалось»
// в «не положено».
//
// # Предмет
//
// У вопроса к хранилищу прав ТРИ исхода, а не два: разрешено · отказано ·
// спросить не удалось. Третий — не оттенок второго:
//
//   - отказ говорит «вам нельзя». Решение зависит от тройки
//     (субъект, отношение, объект), и повтор тождественного вопроса не меняет ни
//     одного из трёх — повторять бессмысленно, и воспитанный клиент не повторяет;
//   - недоступность хранилища не говорит о правах НИЧЕГО: тот же вопрос
//     мгновением позже получает ответ.
//
// Схлопнув третье во второе, страж выдаёт ТЕРМИНАЛЬНЫЙ вердикт на мигание.
// Fail-closed при этом не меняется ни в одну сторону — запрос отвергнут, ничего
// не исполнено, — меняется только код, а код и есть весь сигнал. Канон объявлен
// в `services/iam/internal/authzguard/authzguard.go`
// (`AuthzBackendUnavailable`).
//
// # Что именно ищется
//
// Вызов формы `<что-то>.Check(ctx, subject, relation, object)` — четыре
// аргумента, первый контекстный — это подпись `RelationChecker`. Находка, если
// ошибка такого вызова НИКУДА не девается: связана с `_`, либо связана с
// именем, которое во всей функции встречается только в сравнении `== nil`.
//
// Обе формы означают одно: исход «не ответили» неотличим от «отказано» уже в
// самом коде. Законные формы — `if err != nil { … }`, `return …, err`,
// `fmt.Errorf(…, err)` — ошибку ИСПОЛЬЗУЮТ, и гейт их не трогает: он смотрит на
// употребление имени, а не на текст решения. Требовать именно
// `AuthzBackendUnavailable()` он не вправе — вызывающий бывает обязан отдать
// ошибку выше и облечь её в контракт своего домена (так делают nlb и registry).
//
// # Почему по употреблению имени, а не по «есть ли рядом Unavailable»
//
// Предикат «рядом стоит `AuthzBackendUnavailable`» мерил бы соглашение об
// именовании, а не свойство. Он молчал бы на страже, который вернул нужный код и
// при этом потерял ошибку в другой ветке, и краснел бы на страже, честно
// пробросившем ошибку наверх. Употребление имени — свойство самого потока
// данных, и от словаря оно не зависит.
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
//   - он не знает ТИПОВ и опирается на подпись: четыре аргумента, первый
//     контекстный. Метод `Check` другой арности (проверка здоровья, вход-структура
//     края) под разбор не попадает — и не должен;
//   - он не судит, ПРАВИЛЬНЫЙ ли код возврата выбран у использованной ошибки:
//     это решение домена. Он держит одно — ошибка не может исчезнуть.
//
// # Перепись
//
// Печатается всегда: файлов прочитано · вызовов распознано · находок. «Ноль
// находок» обязано быть отличимо от «ноль прочитанного», а ноль распознанных
// вызовов при непустом дереве — поломка разбора, а не чистота.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// AuthzCheckSite — один разобранный вызов проверки прав.
type AuthzCheckSite struct {
	// File — путь относительно корня дерева.
	File string
	// Line — строка вызова.
	Line int
	// ErrName — имя, которым связана ошибка (`_` для явно выброшенной).
	ErrName string
	// Collapsed — ошибка никуда не девается.
	Collapsed bool
	// Why — почему это находка; пусто у законного вызова.
	Why string
}

// AuthzCheckCensus — объём осмотренного плюс находки.
type AuthzCheckCensus struct {
	FilesRead int
	Sites     []AuthzCheckSite
	Findings  []AuthzCheckSite
}

// ScanAuthzCheckOutage обходит непробное дерево и разбирает вызовы проверки прав.
//
// Пробные файлы исключены НАМЕРЕННО: дублёр, отвечающий отказом на любой вход, —
// законная фикстура, и требовать от неё различения исходов значило бы требовать
// свойство от того, что свойством не обладает. Сгенерированные стабы исключены
// по той же причине, по какой они не правятся руками.
func ScanAuthzCheckOutage(root string) (AuthzCheckCensus, error) {
	var c AuthzCheckCensus
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		// `pkg/api` — сгенерённые стабы; `internal/repohygiene` — сам разбор и его
		// синтетика, иначе гейт судил бы собственные фикстуры.
		if strings.HasPrefix(rel, "pkg/api/") || strings.HasPrefix(rel, "internal/repohygiene/") {
			return nil
		}

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			// Неразбираемый файл НЕ пропускается молча: молчание здесь
			// неотличимо от чистоты.
			return perr
		}
		c.FilesRead++

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, site := range scanFuncForAuthzCheck(fset, fn, rel) {
				c.Sites = append(c.Sites, site)
				if site.Collapsed {
					c.Findings = append(c.Findings, site)
				}
			}
		}
		return nil
	})
	return c, err
}

// scanFuncForAuthzCheck разбирает ОДНУ функцию: находит вызовы проверки прав и
// решает по каждому, доживает ли ошибка до употребления.
//
// Имя ошибки ищется в ЕЁ ОБЛАСТИ ВИДИМОСТИ, а не по всему телу функции. Иначе
// однобуквенное `err`, использованное соседним вызовом двадцатью строками ниже,
// зачлось бы за употребление — и гейт молчал бы ровно там, где схлопывание
// прячется удобнее всего: в функции, где `err` встречается часто.
func scanFuncForAuthzCheck(fset *token.FileSet, fn *ast.FuncDecl, rel string) []AuthzCheckSite {
	var out []AuthzCheckSite

	// parent — узел, внутри которого искать имя. Для присваивания в заголовке
	// `if` это сам `if` (там имя и живёт), иначе — ближайший блок.
	scopeOf := map[*ast.AssignStmt]ast.Node{}
	var blocks []ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.IfStmt:
			if a, ok := v.Init.(*ast.AssignStmt); ok {
				scopeOf[a] = v
			}
		case *ast.ForStmt:
			if a, ok := v.Init.(*ast.AssignStmt); ok {
				scopeOf[a] = v
			}
		case *ast.SwitchStmt:
			if a, ok := v.Init.(*ast.AssignStmt); ok {
				scopeOf[a] = v
			}
		case *ast.BlockStmt:
			_ = blocks
			for _, st := range v.List {
				if a, ok := st.(*ast.AssignStmt); ok {
					if _, taken := scopeOf[a]; !taken {
						scopeOf[a] = v
					}
				}
			}
		}
		return true
	})

	record := func(assign *ast.AssignStmt, call *ast.CallExpr) {
		if !isRelationCheckCall(call) {
			return
		}
		// Получателей РОВНО ДВА — это и есть подпись `RelationChecker`:
		// `(bool, error)`. Иное их число означает ДРУГОЙ метод с тем же именем, и
		// судить его этим гейтом нельзя.
		//
		// Это не послабление и не перечень прощённых, а граница предмета, и она
		// проверена: первая редакция брала ошибку вторым получателем безусловно и
		// объявила находками шесть мест измерительного инструмента, где `Check`
		// отдаёт три значения — там во втором стоит `_`, а ошибка в третьем и
		// употребляется честно. Гейт, у которого две трети находок ложные,
		// перестают читать.
		if len(assign.Lhs) != 2 {
			return
		}
		site := AuthzCheckSite{File: rel, Line: fset.Position(call.Lparen).Line}
		id, ok := assign.Lhs[1].(*ast.Ident)
		if !ok {
			return
		}
		site.ErrName = id.Name
		if id.Name == "_" {
			site.Collapsed = true
			site.Why = "ошибка проверки прав выброшена в `_`: «спросить не удалось» неотличимо от «не положено»"
			out = append(out, site)
			return
		}
		scope, ok := scopeOf[assign]
		if !ok {
			scope = fn.Body
		}
		if !errNameIsUsed(scope, assign, id.Name) {
			site.Collapsed = true
			site.Why = "ошибка `" + id.Name + "` в своей области видимости не употребляется нигде, " +
				"кроме сравнения `== nil`: недоступность хранилища прав схлопнута в отказ"
		}
		out = append(out, site)
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && len(a.Rhs) == 1 {
			if call, ok := a.Rhs[0].(*ast.CallExpr); ok {
				record(a, call)
			}
		}
		return true
	})
	return out
}

// isRelationCheckCall — подпись `RelationChecker.Check`: метод `Check`, четыре
// аргумента, первый контекстный.
func isRelationCheckCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Check" {
		return false
	}
	if len(call.Args) != 4 {
		return false
	}
	id, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	n := strings.ToLower(id.Name)
	return n == "ctx" || strings.HasSuffix(n, "ctx")
}

// errNameIsUsed отвечает, употребляется ли имя ошибки в НАЗВАННОЙ области
// где-либо, кроме сравнения `== nil` и собственного связывания.
//
// Сравнение `!= nil` считается УПОТРЕБЛЕНИЕМ: за ним стоит ветка, отвечающая на
// исход отдельно, — а это ровно то, чего гейт и требует.
func errNameIsUsed(scope ast.Node, binding *ast.AssignStmt, name string) bool {
	used := false
	ast.Inspect(scope, func(n ast.Node) bool {
		if used {
			return false
		}
		if n == binding {
			// Само связывание употреблением не является; правая часть при этом
			// осматривается — там мог стоять другой вызов с тем же именем.
			for _, r := range binding.Rhs {
				ast.Inspect(r, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok && id.Name == name {
						used = true
					}
					return !used
				})
			}
			return false
		}
		if v, ok := n.(*ast.BinaryExpr); ok && v.Op == token.EQL {
			if (authzIsIdent(v.X, name) && authzIsNilIdent(v.Y)) || (authzIsIdent(v.Y, name) && authzIsNilIdent(v.X)) {
				// `name == nil` — не употребление. Внутрь не спускаемся, иначе
				// сам операнд зачёлся бы.
				return false
			}
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			used = true
			return false
		}
		return true
	})
	return used
}

func authzIsIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func authzIsNilIdent(e ast.Expr) bool { return authzIsIdent(e, "nil") }
