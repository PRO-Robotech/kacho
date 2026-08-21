// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_revocation_wiring_test.go — читатель отзыва НАШИХ токенов провязывается
// НЕЗАВИСИМО от адреса прежнего провайдера.
//
// # Предмет
//
// Он стоял ВНУТРИ ветки «адрес прежнего провайдера задан», и это делало
// невыразимой посадку, к которой фаза ведёт: «принимаем ТОЛЬКО нашего
// издателя». Такой профиль адреса прежнего провайдера не задаёт — задавать
// нечего, — и наш читатель не провязывался вовсе, а следом старт отвергался.
// Отказ был честный, но отвергал он не ошибку оператора, а состояние, которое
// обязано быть законным: возможность, объявленная и неисполнимая ни при каком
// входе, — тот же класс, что поле, которое требуют и прислать нельзя.
//
// # Почему проверяется ИСХОДНИК, а не поведение
//
// `main()` из пробы не исполнить: он дозванивается до соседей и занимает
// слушатели. Чтение исходника СЛАБЕЕ исполнения, и здесь оно применяется ровно
// к тому свойству, которого «оно собирается» не показывает, — к ВЛОЖЕННОСТИ
// одного решения в другое. Ровно та же форма и то же обоснование, что у
// соседней пробы провязки административного хопа.
//
// Разбор ведётся по дереву синтаксиса, а не по тексту: предмет здесь —
// вложенность узлов, и предикат по подстроке отвечал бы на другой вопрос.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// f1bFindCall возвращает позиции всех вызовов метода с названным именем.
func f1bFindCall(f *ast.File, name string) []token.Pos {
	var out []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		out = append(out, call.Pos())
		return true
	})
	return out
}

// legacyProviderURLBinding — имя, которым композиционный корень связывает адрес
// прежнего провайдера. Ветка ищется по ЭТОЙ привязке: имя названо здесь, чтобы
// его переименование роняло пробу переписью («ветка не найдена»), а не делало
// её тихо беспредметной.
const legacyProviderURLBinding = "introspectionURL"

// legacyProviderBranchMinLines — порог, ниже которого найденная область считается
// вырожденной. Настоящая ветка несёт построение кэша, выбор источников и
// провязку; двухстрочная — это соседний страж, найденный по ошибке.
const legacyProviderBranchMinLines = 10

// f1bLegacyProviderBranch возвращает тело ветки, гейтующей адрес ПРЕЖНЕГО
// провайдера, — то есть ту область, внутри которой наш читатель стоять не
// вправе.
func f1bLegacyProviderBranch(f *ast.File) *ast.BlockStmt {
	var found *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || found != nil {
			return true
		}
		// Различитель — ПРИВЯЗКА имени в Init, а не упоминание метода где-то
		// внутри условия. Упоминание ловит и соседнюю двухстрочную ветку стража
		// настройки, где Init читает тот же адрес: первая редакция этой функции
		// так и делала, находила её и проходила ВПУСТУЮ. Область, найденная не
		// та, — это молчание, неотличимое от исполненного свойства.
		assign, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || ident.Name != legacyProviderURLBinding {
			return true
		}
		found = ifs.Body
		return true
	})
	return found
}

func TestF1b_OurRevocationReaderIsWiredIndependentlyOfTheLegacyProvider(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("композиционный корень не разбирается: %v", err)
	}

	// Положительный контроль: читатель вообще провязывается. Без него проба
	// молчала бы на дереве, где его сняли целиком.
	calls := f1bFindCall(f, "WithPlatformRevocationCheck")
	if len(calls) == 0 {
		t.Fatalf("читатель отзыва НАШИХ токенов не провязывается в композиционном корне " +
			"вовсе — объявленный контроль без читателя не отказал бы ни разу за свою жизнь")
	}

	branch := f1bLegacyProviderBranch(f)
	if branch == nil {
		t.Fatalf("в композиционном корне не найдена ветка адреса прежнего провайдера — " +
			"предмет вложенности искать не в чем, и молчание этой пробы сказано ни о чём")
	}

	for _, pos := range calls {
		if pos > branch.Lbrace && pos < branch.Rbrace {
			t.Fatalf("читатель отзыва НАШИХ токенов провязан ВНУТРИ ветки адреса прежнего "+
				"провайдера (%s внутри %s..%s).\n\n"+
				"Тогда посадка «принимаем только нашего издателя» невыразима: профиль, не "+
				"задающий адреса прежнего провайдера, не провязывает наш читатель и следом "+
				"отвергает старт. Отказ честный, но отвергает он не ошибку оператора, а "+
				"состояние, которое обязано быть законным.",
				fset.Position(pos), fset.Position(branch.Lbrace), fset.Position(branch.Rbrace))
		}
	}

	// Область обязана быть НЕВЫРОЖДЕННОЙ: ветка в две строки означает, что
	// найдена не та, и «вне неё» тогда верно про что угодно.
	span := fset.Position(branch.Rbrace).Line - fset.Position(branch.Lbrace).Line
	if span < legacyProviderBranchMinLines {
		t.Fatalf("ветка прежнего провайдера найдена вырожденной — строк %d при пороге %d "+
			"(%s..%s). Это не «свойство исполнено», а «искали не там»: утверждение «наш "+
			"читатель вне неё» на двухстрочной области верно про что угодно.",
			span, legacyProviderBranchMinLines,
			fset.Position(branch.Lbrace), fset.Position(branch.Rbrace))
	}

	t.Logf("перепись: вызовов провязки нашего читателя %d, все вне ветки прежнего провайдера "+
		"(%s..%s, строк %d)", len(calls),
		fset.Position(branch.Lbrace), fset.Position(branch.Rbrace), span)
}
