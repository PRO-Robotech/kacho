// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// critical_backends_validation_test.go — ЛОЖНАЯ ВЕТВЬ ЛИБО ГОВОРИТ, ЛИБО ЕЁ НЕТ
// (C3).
//
// # Что наблюдалось
//
// Корень объявлял прозой: «ветки „а вдруг его нет“ здесь не заводится — она
// была бы веткой, в которой край всё равно не работает», — и следующей строкой
// заводил ровно эту ветку: `if iamConn := backends["iamInternal"]; iamConn !=
// nil {`, без `else`. Пока рядом стоял читатель чужого поставщика, ветка только
// УЛУЧШАЛА композицию, и утверждение было близко к истине. Он снят, и ложная
// ветвь стала оставлять путь запроса БЕЗ читателя отзыва вовсе — молча: прежний
// `logger.Warn` ушёл тем же изменением.
//
// # Исходов два, и «молча» среди них нет
//
// Ветка есть и она говорит — либо ветки нет. Взят второй: соединение к службе
// прав КРИТИЧЕСКОЕ (она фронтит и личность, и права), его отсутствие край не
// переживает ни при какой посадке, и правильная форма такого условия — отказ
// СТАРТА, а не тихий пропуск провязки на каждом запросе.
//
// # Почему страж, а не просто снятие проверки
//
// Снять `!= nil` и провязать безусловно значило бы построить читатель на
// пустом соединении: отказ пришёл бы паникой на первом запросе арендатора.
// Отказ старта виден оператору; паника на пути запроса видна арендатору.
//
// # Предмет ШИРЕ одной строки
//
// Тот же ключ читался в корне ВОСЕМЬЮ местами: три под проверкой на `nil`
// (включая два с этой самой прозой) и пять — без неё. То есть дерево уже
// считало ключ непустым в большинстве мест и сторожило его в меньшинстве, и
// решал это не автор, а порядок появления строк. После стража проверок ноль,
// чтений восемь.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// TestCriticalBackendsGuard_RefusesOnAnAbsentConnection — ДЕФЕКТ КРАСНИТ.
func TestCriticalBackendsGuard_RefusesOnAnAbsentConnection(t *testing.T) {
	t.Parallel()
	keys := criticalBackendKeys()
	if len(keys) == 0 {
		t.Fatal("критических соединений объявлено ноль — сторожить нечего, " +
			"и молчание стража сказано ни о чём")
	}
	for absent := range keys {
		t.Run(absent, func(t *testing.T) {
			b := proxy.Backends{}
			for k := range keys {
				if k == absent {
					continue
				}
				b[k] = &grpc.ClientConn{}
			}
			err := validateCriticalBackends(b)
			if err == nil {
				t.Fatalf("соединение %q отсутствует, а старт разрешён: провязка, "+
					"зависящая от него, будет молча пропущена, и край поднимется "+
					"готовым без неё", absent)
			}
			if !strings.Contains(err.Error(), absent) {
				t.Errorf("отказ не называет отсутствующее соединение (%q): оператор "+
					"не поднимет стенд по отказу, который не говорит что чинить", err)
			}
			if !strings.Contains(err.Error(), "refuse to start") {
				t.Errorf("отказ не объявляет себя отказом СТАРТА: %q", err)
			}
		})
	}
}

// TestCriticalBackendsGuard_SilentWhenEveryCriticalConnectionIsThere —
// ЗАКОННЫЙ БЛИЗНЕЦ МОЛЧИТ.
//
// Против дефекта меняется РОВНО ОДИН факт: соединение на месте. Без этой
// половины «страж отказывает» было бы неотличимо от «страж отказывает всегда».
func TestCriticalBackendsGuard_SilentWhenEveryCriticalConnectionIsThere(t *testing.T) {
	t.Parallel()
	b := proxy.Backends{}
	for k := range criticalBackendKeys() {
		b[k] = &grpc.ClientConn{}
	}
	// Некритический сосед отсутствует намеренно: его недоступность — деградация
	// одного домена, а не отказ края.
	if err := validateCriticalBackends(b); err != nil {
		t.Fatalf("полный набор критических соединений отвергнут: %v", err)
	}
}

// TestCompositionRoot_WiresTheRevocationReaderWithoutASkippableBranch —
// ПРОВЯЗКА НЕ СТОИТ ПОД УСЛОВИЕМ, КОТОРОЕ МОЖЕТ ЕЁ ПРОПУСТИТЬ.
//
// Судится ДОСТИЖИМОЕ от `main()` синтаксическим разбором, а не текстом:
// условие судится узлом `if`, а не словом «if» рядом.
func TestCompositionRoot_WiresTheRevocationReaderWithoutASkippableBranch(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, compositionRootLabel, compositionRoot(t), parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("достижимый код корня не разбирается: %v", err)
	}

	guarded := map[string][]int{}
	found := map[string]int{}
	for _, wiring := range []string{"WithRevocationCheck", "WithSessionCutoffCheck"} {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != wiring {
				return true
			}
			found[wiring]++
			for _, anc := range stack[:len(stack)-1] {
				if ifs, isIf := anc.(*ast.IfStmt); isIf {
					guarded[wiring] = append(guarded[wiring], fset.Position(ifs.Pos()).Line)
				}
			}
			return true
		})
	}

	t.Logf("перепись: провязок читателей отзыва найдено %v · под условием %v", found, guarded)
	for wiring, n := range found {
		if n == 0 {
			t.Fatalf("провязка %s в достижимом коде корня не найдена — предмет пробы "+
				"исчез, а это НЕ то же самое, что «условия нет»", wiring)
		}
	}
	if len(found) != 2 {
		t.Fatalf("найдено провязок видов %d, ожидалось 2 — распознавание разошлось "+
			"с корнем, и молчание сказано ни о чём", len(found))
	}
	for wiring, lines := range guarded {
		t.Errorf("провязка %s стоит под условием (строки %v достижимого кода). "+
			"Ложная ветвь оставляет путь запроса БЕЗ читателя отзыва молча: край "+
			"поднимается готовым, отзыв не исполняется, и не краснеет ничто. "+
			"Соединение критическое — его отсутствие обязано отказывать в СТАРТЕ "+
			"(validateCriticalBackends), а провязка обязана быть безусловной.",
			wiring, lines)
	}
}

// TestCompositionRoot_AsksTheCriticalBackendsGuard — страж ПОЗВАН корнем.
//
// Страж, которого никто не зовёт, зеленит свои пробы и ничего не меняет в
// старте: ровно тот класс, ради которого провязка судится позванностью.
func TestCompositionRoot_AsksTheCriticalBackendsGuard(t *testing.T) {
	t.Parallel()
	src := compositionRoot(t)
	if !strings.Contains(src, "validateCriticalBackends(backends)") {
		t.Fatal("композиционный корень не зовёт стража критических соединений — " +
			"без него безусловная провязка строится на пустом соединении, и отказ " +
			"приходит паникой на первом запросе арендатора, а не отказом старта")
	}
	if !strings.Contains(src, `log.Fatalf("critical backends: %v", cbErr)`) {
		t.Error("корень не падает на отказе стража критических соединений: страж, " +
			"чей отказ поглощён, не меняет в старте ничего")
	}
}
