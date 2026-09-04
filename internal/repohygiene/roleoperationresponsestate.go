// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// roleoperationresponsestate.go — анализатор «ответ операции над ролью не несёт
// вычисленного состояния, и это ОБЕСПЕЧЕНО, а не совпало».
//
// # Предмет
//
// Контракт роли обещает арендатору буквально следующее: нулевое значение
// `health` и `lifecycle` означает «ЭТИМ ОТВЕТОМ НЕ ВЫЧИСЛЕНО» и никогда «роль
// здорова» либо «роль объявлена»; его несут ответы операций `Create`/`Update`,
// а `Get` и `List` заполняют состояние всегда. То же говорят счётчики
// сегментов, обе ведомости и состояния правил — семь полей контракта, которые
// приходят из ПЯТИ производных полей доменной роли.
//
// Обещание это до сих пор держалось ТОЛЬКО ТЕМ, что производителя вычисленного
// состояния никто не звал на пути мутации. Свойство «by construction» тем и
// плохо, что его снятие ТИХОЕ: достаточно одной строки в пути создания, и ответ
// операции начнёт объявлять роль здоровой и объявленной там, где её никто не
// читал, — при том что ни один прогон не покраснеет.
//
// # Почему обнуление, а не запрет вызова
//
// Состояние, вычисленное НА ПУТИ МУТАЦИИ, было бы не «лишним», а НЕВЕРНЫМ: оно
// считается по проекции, которую мутация только что изменила либо ещё не
// изменила, то есть относится к другому снимку. Опубликовать его значило бы
// ответить арендатору про состояние, которого у роли не было ни до, ни после.
//
// Поэтому проекция ответа операции ОБНУЛЯЕТ производные поля явно
// ([domain.Role.WithoutComputedState]), и обещание перестаёт зависеть от того,
// позвал ли кто-нибудь производителя. Запрет вызова закрывал бы ОДИН путь к
// нарушению; обнуление закрывает ВСЕ, включая те, которых сегодня нет.
//
// # Что судится
//
// ПЕРЕВОДЧИК ответа операции над ролью — не-тестовая функция, у которой (а)
// результаты ровно `(*anypb.Any, error)`, (б) есть параметр типа `domain.Role`
// и (в) в теле стоит вызов перевода `dto.Transfer`. Все три признака вместе:
// (а) отделяет ответ операции от синхронного чтения, (б) — от переводчика
// чужого ресурса, (в) — от вызывающего, который перевод делегирует
// (`doCreate`/`doUpdate` роль принимают и возвращают, но не переводят).
//
// Каждый такой переводчик обязан звать проекцию. Не зовёт — находка с
// координатой.
//
// Вызов опознаётся УЗЛОМ, а не текстом, и сверх того исходник разбирается БЕЗ
// комментариев вовсе (`parser.ParseFile` без `ParseComments`): имя проекции
// стоит в комментариях обоих переводчиков, объясняя, зачем она нужна, — гейт по
// подстроке зеленел бы на собственном объяснении. Ось инъекции ставит имя
// комментарием ВНУТРИ тела, то есть там, где его увидел бы всякий текстовый
// разбор.
//
// # ЧЕГО ОН НЕ СУДИТ
//
//  1. ПОЛНОТУ набора производных полей. Что полей ровно пять и что перевод
//     обнуляет каждое — предмет пробы самой проекции (`role_computed_state_test.go`
//     в пакете домена): она сверяет МНОЖЕСТВО изменившихся полей контракта, а не
//     их число, поэтому шестое производное поле, заведённое без обнуления, ей
//     видно. Гейт судит ПРОВЯЗКУ, проба — СОДЕРЖАНИЕ; ни один из двух не
//     заменяет другого.
//  2. ЛИШНЮЮ РАБОТУ. Вычислить состояние на пути мутации и выбросить — расход
//     без последствий для арендатора, и после обнуления он перестаёт быть
//     дефектом контракта. Гейт его не ловит, и это названо, а не умолчано.
//  3. ЧТЕНИЯ. `Get` и `List` состояние заполняют, проекция к ним не относится:
//     у них результат не `(*anypb.Any, error)`, и под признак они не подпадают
//     by construction.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов, ноль разобранных функций либо ноль найденных
// переводчиков роли — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// RoleOperationResponseStateOptions — вход анализатора.
type RoleOperationResponseStateOptions struct {
	// Root — корень дерева.
	Root string
	// ServiceRoot — каталог сервиса относительно Root.
	ServiceRoot string
	// DomainPkg — имя пакета доменных типов, каким его видит вызывающий.
	DomainPkg string
	// RoleType — имя доменного типа роли.
	RoleType string
	// TransferFunc — имя функции перевода в контрактную форму.
	TransferFunc string
	// ProjectionMethod — имя проекции, обнуляющей вычисленное состояние.
	ProjectionMethod string
}

// RoleOperationResponseStateCensus — объём осмотренного.
type RoleOperationResponseStateCensus struct {
	Files            int
	Funcs            int
	AnypbFuncs       int
	RoleTranslators  int
	ProjectionCalled int
}

func (c RoleOperationResponseStateCensus) String() string {
	return fmt.Sprintf(
		"файлов Go прочитано %d · функций разобрано %d · возвращающих ответ операции %d · "+
			"из них ПЕРЕВОДЧИКОВ роли %d · зовущих проекцию %d",
		c.Files, c.Funcs, c.AnypbFuncs, c.RoleTranslators, c.ProjectionCalled)
}

// RoleOperationResponseStateFinding — переводчик, не зовущий проекцию.
type RoleOperationResponseStateFinding struct {
	File string
	Line int
	Func string
}

func (f RoleOperationResponseStateFinding) String() string {
	return fmt.Sprintf("%s:%d: %s переводит роль в ответ операции и не зовёт проекцию",
		f.File, f.Line, f.Func)
}

// AuditRoleOperationResponseState выносит вердикт о дереве.
func AuditRoleOperationResponseState(
	opts RoleOperationResponseStateOptions,
	log io.Writer,
) ([]RoleOperationResponseStateFinding, RoleOperationResponseStateCensus, error) {
	var census RoleOperationResponseStateCensus
	var findings []RoleOperationResponseStateFinding

	root := filepath.Join(opts.Root, filepath.FromSlash(opts.ServiceRoot))
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return fmt.Errorf("%s: %w", p, perr)
		}
		census.Files++
		rel, _ := filepath.Rel(opts.Root, p)
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			census.Funcs++
			if !roleOpStateReturnsOperationPayload(fn.Type) {
				continue
			}
			census.AnypbFuncs++
			if !roleOpStateTakesRole(fn.Type, opts) {
				continue
			}
			if !roleOpStateCalls(fn.Body, opts.TransferFunc) {
				continue // перевод делегирован — судится тот, кто переводит
			}
			census.RoleTranslators++
			if roleOpStateCalls(fn.Body, opts.ProjectionMethod) {
				census.ProjectionCalled++
				continue
			}
			findings = append(findings, RoleOperationResponseStateFinding{
				File: rel, Line: fset.Position(fn.Pos()).Line, Func: roleOpStateName(fn),
			})
		}
		return nil
	})
	if err != nil {
		return nil, census, err
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: %s\n", census)
	}

	switch {
	case census.Files == 0:
		return findings, census, fmt.Errorf(
			"прочитано ноль файлов Go в %s: обход пуст, вердикт беспредметен", opts.ServiceRoot)
	case census.Funcs == 0:
		return findings, census, fmt.Errorf(
			"разобрано ноль функций в %s: разбор перестал видеть объявления", opts.ServiceRoot)
	case census.RoleTranslators == 0:
		return findings, census, fmt.Errorf(
			"переводчиков роли в ответ операции найдено ноль: признак разошёлся с деревом, "+
				"и «находок ноль» здесь означает «прочитано ноль» (искали функцию с "+
				"результатами (*anypb.Any, error), параметром %s.%s и вызовом %s)",
			opts.DomainPkg, opts.RoleType, opts.TransferFunc)
	}
	return findings, census, nil
}

// roleOpStateReturnsOperationPayload — результаты ровно `(*anypb.Any, error)`.
//
// Это и есть машинный признак ОТВЕТА ОПЕРАЦИИ: синхронное чтение возвращает
// ресурс, а не полезную нагрузку операции, поэтому под признак не подпадает.
func roleOpStateReturnsOperationPayload(ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) != 2 {
		return false
	}
	var names []string
	for _, r := range ft.Results.List {
		if len(r.Names) > 1 {
			return false
		}
		names = append(names, roleOpStateTypeString(r.Type))
	}
	return names[0] == "*anypb.Any" && names[1] == "error"
}

// roleOpStateTakesRole — среди параметров есть доменная роль (значением либо
// указателем).
func roleOpStateTakesRole(ft *ast.FuncType, opts RoleOperationResponseStateOptions) bool {
	if ft.Params == nil {
		return false
	}
	want := opts.DomainPkg + "." + opts.RoleType
	for _, p := range ft.Params.List {
		t := roleOpStateTypeString(p.Type)
		if t == want || t == "*"+want {
			return true
		}
	}
	return false
}

// roleOpStateCalls — зовёт ли тело функцию либо метод с таким именем.
//
// Читается УЗЕЛ ВЫЗОВА, а не текст: имя проекции встречается и в комментарии,
// объясняющем, зачем она нужна, — гейт по подстроке зеленел бы на собственном
// объяснении.
func roleOpStateCalls(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			if f.Name == name {
				found = true
			}
		case *ast.SelectorExpr:
			if f.Sel.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// roleOpStateName — имя функции с приёмником, чтобы координата называла её
// однозначно: `marshalRole` в дереве не одна.
func roleOpStateName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return "(" + roleOpStateTypeString(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

// roleOpStateTypeString — запись типа в исходнике; типовой информации у гейта
// нет и не нужно, различают признаки выше.
func roleOpStateTypeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + roleOpStateTypeString(t.X)
	case *ast.SelectorExpr:
		return roleOpStateTypeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + roleOpStateTypeString(t.Elt)
	default:
		return ""
	}
}
