// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// apply_state_wiring_test.go — заполнитель провязан на КАЖДОЙ заполняющей
// поверхности, и ни на одной лишней (часть APPLY-11 и DoD стадии 2).
//
// # Почему отдельный гейт, а не «пара сошлась»
//
// Гейт пары (`dataplane_apply_pairing_test.go`) отвечает на вопрос «существуют ли
// обе половины вообще». Он останется зелёным, если поле есть у семи ресурсов, а
// заполнитель позван у одного: любое поле плюс любой вызывающий читаются им как
// сошедшаяся пара. Это осознанная граница ТОГО гейта — его предмет
// существование, а не полнота.
//
// Полнота — предмет этого. Заполняющих поверхностей четырнадцать: `Get` и
// списочный RPC у каждого из семи ресурсов, несущих строку намерения. Пропущенная
// поверхность — это ровно тот дефект, ради которого поле заводилось: список
// отдаёт пустоту, клиент, обновляющий состояние из него (дешёвый и потому частый
// путь), читает её как утрату.
//
// # Почему разбор синтаксиса, а не поиск подстроки
//
// Имя заполнителя стоит в комментариях этих же файлов — в том числе в
// комментарии, объясняющем, зачем он тут. Текстовый предикат объявил бы
// поверхность провязанной по её же документации.
//
// # Граница, названная честно
//
// Гейт видит ВЫЗОВ, а не его исход: он не доказывает, что результат вызова
// положен в поле. Это утверждают пробы уровня поведения (интеграционные и
// сквозные), и они названы в приёмке отдельно. Здесь закрывается ровно тот
// класс, который пробы поведения ловят хуже всего, — молчаливо пропущенная
// поверхность, о которой никто не вспомнит, пока клиент не прочитает пустоту.

// applyStateWiringRoot — корень дерева сервиса относительно этого пакета.
const applyStateWiringRoot = "../../.."

// applyStateFillingSurfaces — методы, обязанные спрашивать состояние применения.
//
// Пары «пакет ресурса → метод». Перечень выписан, потому что он И ЕСТЬ предмет
// утверждения: вывести его из дерева значило бы объявить провязанным ровно то,
// что провязано.
var applyStateFillingSurfaces = []struct{ pkg, method string }{
	{"network", "Get"}, {"network", "List"},
	{"subnet", "Get"}, {"subnet", "List"},
	{"networkinterface", "Get"}, {"networkinterface", "List"},
	{"securitygroup", "Get"}, {"securitygroup", "List"},
	{"routetable", "Get"}, {"routetable", "List"},
	{"gateway", "Get"}, {"gateway", "List"},
	{"address", "Get"}, {"address", "List"},
}

// applyStateFillerCalls — имена, которыми поверхность спрашивает состояние.
//
// Два: единичное чтение и страница. Третьего способа заводить нельзя — он
// разошёлся бы с этими двумя молча.
var applyStateFillerCalls = map[string]bool{"One": true, "FillPage": true}

// TestApplyStateFillerIsWiredAtEveryFillingSurface — все четырнадцать.
func TestApplyStateFillerIsWiredAtEveryFillingSurface(t *testing.T) {
	found := map[string]int{}
	filesRead, methodsRead := 0, 0

	seenPkgs := map[string]bool{}
	for _, s := range applyStateFillingSurfaces {
		seenPkgs[s.pkg] = true
	}

	for pkg := range seenPkgs {
		path := filepath.Join(applyStateWiringRoot, "internal", "apps", "kacho", "api", pkg, "handler.go")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Нечитаемый файл — слепое пятно, а не пропуск: промолчав о нём, гейт
			// отчитался бы об осмотре дерева, части которого он не видел.
			t.Fatalf("файл %s не разбирается, гейт слеп на нём: %v", path, err)
		}
		filesRead++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			methodsRead++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !applyStateFillerCalls[sel.Sel.Name] {
					return true
				}
				// Отсекаем однофамильцев: спрашивать состояние вправе только
				// заполнитель — либо через своё поле (`h.applyState.One`), либо
				// через пакет (`applystate.FillPage`).
				switch x := sel.X.(type) {
				case *ast.SelectorExpr:
					if x.Sel.Name != "applyState" {
						return true
					}
				case *ast.Ident:
					if x.Name != "applystate" {
						return true
					}
				default:
					return true
				}
				found[pkg+"."+fn.Name.Name]++
				return true
			})
		}
	}

	var missing []string
	for _, s := range applyStateFillingSurfaces {
		if found[s.pkg+"."+s.method] == 0 {
			missing = append(missing, s.pkg+"."+s.method)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("состояние применения не спрашивается на %d поверхности(ях) из %d: %s.\n"+
			"Поверхность, где поле не заполняется, отдаёт арендатору пустоту, "+
			"неотличимую от «утверждения нет»; клиент, обновляющий состояние из списка, "+
			"прочтёт её как утрату.",
			len(missing), len(applyStateFillingSurfaces), strings.Join(missing, ", "))
	}

	// ПЕРЕПИСЬ: «ноль пропущенного» обязано быть отличимо от «ноль прочитанного».
	if filesRead != len(seenPkgs) || methodsRead == 0 {
		t.Fatalf("гейт прочитал %d файл(ов) из %d и %d метод(ов) — "+
			"утверждение о полноте сделано на непрочитанном дереве",
			filesRead, len(seenPkgs), methodsRead)
	}
	t.Logf("осмотрено %d файл(ов) обработчиков, %d метод(ов); "+
		"состояние применения спрашивается на %d поверхности(ях) из %d объявленных",
		filesRead, methodsRead, len(found), len(applyStateFillingSurfaces))

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: поверхности, которые заполнителя звать НЕ должны.
	// Без неё «провязано везде» зеленело бы и на сборке, где его зовут отовсюду —
	// в том числе с горячего пути опроса операции и из потока намерения
	// исполнителя, куда он не приезжает по решению приёмки.
	for _, forbidden := range []string{"Create", "Update", "Delete"} {
		for _, s := range applyStateFillingSurfaces {
			if found[s.pkg+"."+forbidden] > 0 {
				t.Errorf("%s.%s спрашивает состояние применения: мутация возвращает Operation, "+
					"а не ресурс, и отчёта по новой ревизии на этот момент заведомо нет",
					s.pkg, forbidden)
			}
		}
	}
}

// TestApplyStateFillerIsWiredInTheCompositionRoot — боевая сборка провязывает
// заполнитель КАЖДОМУ из семи обработчиков.
//
// Отдельное утверждение, потому что предмет другой: предыдущая проба говорит
// «поверхность спрашивает», эта — «есть у кого спросить». Нулевой заполнитель
// законен (пробы собирают use-case без композиционного корня) и молча отдаёт
// «утверждения нет» — то есть забытая провязка выглядит ровно как исправная
// работа на ресурсе, который удаляют.
func TestApplyStateFillerIsWiredInTheCompositionRoot(t *testing.T) {
	path := filepath.Join(applyStateWiringRoot, "cmd", "vpc", "main.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("композиционный корень %s не разбирается, гейт слеп на нём: %v", path, err)
	}

	wired, constructed := 0, 0
	// handlerConstructors — конструкторы обработчиков семи ресурсов, несущих поле.
	handlerConstructors := map[string]bool{
		"networkapp": true, "subnetapp": true, "niapp": true, "sgapp": true,
		"routetableapp": true, "gatewayapp": true, "addressapp": true,
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "WithApplyState" {
			wired++
		}
		if sel.Sel.Name == "NewHandler" {
			if id, ok := sel.X.(*ast.Ident); ok && handlerConstructors[id.Name] {
				constructed++
			}
		}
		return true
	})

	if constructed != len(handlerConstructors) {
		t.Fatalf("в композиционном корне собрано %d обработчик(ов) из %d ожидаемых — "+
			"предпосылка гейта ложна: он считает не то, что думает",
			constructed, len(handlerConstructors))
	}
	if wired != constructed {
		t.Fatalf("заполнитель провязан %d обработчику(ам) из %d собранных: "+
			"непровязанный отдаёт «утверждения нет» о каждом ресурсе, и это неотличимо "+
			"от штатной гонки удаления", wired, constructed)
	}
	t.Logf("осмотрен композиционный корень: собрано %d обработчик(ов) ресурсов, "+
		"заполнитель провязан %d раз(а)", constructed, wired)
}
