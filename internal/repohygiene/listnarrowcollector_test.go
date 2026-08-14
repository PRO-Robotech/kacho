// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// listnarrowcollector_test.go — шестой потребитель сужателя списков не может
// появиться незамеченным.
//
// # Предмет
//
// Сужатель списков в этом дереве один, потому что четыре копии одного решения о
// видимости уже разошлись — и разошлись там, где расхождение не видно. Его
// величины устроены так же: одна положительная полоса и три, каждая из которых
// означает СТРАНИЦУ, УШЕДШУЮ БЕЗ ПООБЪЕКТНОЙ ПРОВЕРКИ. Потребитель, который
// построил сужатель и не зарегистрировал коллектор, отдаёт такие страницы молча:
// снаружи он неотличим от потребителя, у которого их не было.
//
// # Почему гейт, а не пункт регламента
//
// Регистрация коллектора необязательна по построению (наблюдение не должно
// ронять сборку), поэтому её отсутствие не поймает ни компилятор, ни проба
// самого сужателя. Свойство «у каждого потребителя есть коллектор» — свойство
// ДЕРЕВА, и держать его может только обход дерева. Шестой сервис заведут
// копированием пятого; копироваться должна и эта строка.
//
// # Что именно требуется от потребителя
//
// Оба конца провязки, потому что каждый по отдельности ничего не значит:
//
//   - `narrowmetrics.New(<имя сервиса>, …)` — коллектор собран, и имя сервиса в
//     имени серии совпадает с каталогом потребителя (иначе серия vpc уехала бы
//     под именем storage, и это было бы видно только на панели);
//   - `RegisterListNarrow(…)` у композиционного корня — коллектор
//     ЗАРЕГИСТРИРОВАН. Собранный и не зарегистрированный коллектор не отдаёт
//     ничего, и его отсутствие на поверхности выглядит ровно как «сужений не
//     было».
//
// # Дублёры проб потребителями НЕ являются
//
// `pkg/listnarrow/narrowtest` строит сужатели для проб сервисов; они живут
// столько же, сколько проба, и наблюдать в них нечего. Исключение названо и
// проверяется на существование: исчезнет каталог — гейт упадёт на своей же
// предпосылке.

// narrowConsumer — один каталог сервиса, строящий сужатель.
type narrowConsumer struct {
	service    string // имя каталога: `vpc`, `nlb`, …
	ctorSites  []string
	collector  string // имя сервиса, переданное narrowmetrics.New; пусто — коллектора нет
	registered bool   // есть ли вызов RegisterListNarrow
}

// collectNarrowConsumers переписывает не-тестовое дерево сервисов и общего
// фундамента: кто строит сужатель, кто собирает коллектор и кто его
// регистрирует.
func collectNarrowConsumers(t *testing.T) (consumers map[string]*narrowConsumer, harnessSites, scanned int) {
	t.Helper()
	root := repoRoot(t)
	consumers = map[string]*narrowConsumer{}

	get := func(service string) *narrowConsumer {
		if c, ok := consumers[service]; ok {
			return c
		}
		c := &narrowConsumer{service: service}
		consumers[service] = c
		return c
	}

	walkOwnerRegisterGoFiles(t, root, []string{"services", "pkg"}, func(rel string, body []byte) {
		scanned++
		src := string(body)
		if !strings.Contains(src, "listnarrow.New(") &&
			!strings.Contains(src, "narrowmetrics.New(") &&
			!strings.Contains(src, "RegisterListNarrow(") {
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		dir := consumerDir(rel)
		service := strings.TrimPrefix(dir, "services/")
		inServices := strings.HasPrefix(dir, "services/")

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, isQualified := sel.X.(*ast.Ident)
			switch {
			case isQualified && pkgIdent.Name == "listnarrow" && sel.Sel.Name == "New":
				if dir == narrowTestHarness {
					harnessSites++
					return true
				}
				if !inServices {
					t.Errorf("%s:%s — сужатель строится вне каталога сервиса и вне дублёров проб. "+
						"Гейт не знает, чем судить такого потребителя: либо это новый вид "+
						"потребителя (тогда его надо описать здесь), либо носитель заведён там, "+
						"где его величины некому наблюдать",
						rel, fmtLine(fset, call.Lparen))
					return true
				}
				c := get(service)
				c.ctorSites = append(c.ctorSites, rel+":"+fmtLine(fset, call.Lparen))
			case isQualified && pkgIdent.Name == "narrowmetrics" && sel.Sel.Name == "New":
				if !inServices {
					return true
				}
				c := get(service)
				if len(call.Args) > 0 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if unquoted, uerr := strconv.Unquote(lit.Value); uerr == nil {
							c.collector = unquoted
						}
					}
				}
			case sel.Sel.Name == "RegisterListNarrow":
				if !inServices {
					return true
				}
				get(service).registered = true
			}
			return true
		})
	})
	return consumers, harnessSites, scanned
}

// TestEveryListNarrowConsumerRegistersItsCollector — у каждого потребителя
// сужателя есть свой зарегистрированный коллектор, и его имя совпадает с
// каталогом потребителя.
func TestEveryListNarrowConsumerRegistersItsCollector(t *testing.T) {
	consumers, harnessSites, scanned := collectNarrowConsumers(t)

	// Объём осмотренного — отдельное утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	services := make([]string, 0, len(consumers))
	ctors := 0
	for name, c := range consumers {
		if len(c.ctorSites) > 0 {
			services = append(services, name)
			ctors += len(c.ctorSites)
		}
	}
	sort.Strings(services)
	t.Logf("осмотрено не-тестовых файлов Go: %d; потребителей сужателя: %d (%s); "+
		"мест сборки в сервисах: %d; в оснастке проб общего фундамента: %d",
		scanned, len(services), strings.Join(services, ", "), ctors, harnessSites)

	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if ctors == 0 {
		t.Fatal("в каталогах сервисов нет ни одного вызова listnarrow.New — предмет гейта отпал: " +
			"снимите его вместе с сужателем либо почините имена, которыми он его ищет")
	}
	if harnessSites == 0 {
		t.Fatalf("исключению %q больше нечего исключать: дублёров сужателя в дереве нет — "+
			"снимите запись вместе с каталогом", narrowTestHarness)
	}

	var findings []string
	for _, name := range services {
		c := consumers[name]
		switch {
		case c.collector == "":
			findings = append(findings, "services/"+name+
				" — строит сужатель ("+c.ctorSites[0]+"), но коллектора его величин нет: "+
				"зовите narrowmetrics.New(\""+name+"\", …) в своём адаптере наблюдаемости. "+
				"Три из четырёх полос означают страницу, ушедшую БЕЗ пообъектной проверки — "+
				"без них она уходит молча")
		case c.collector != name:
			findings = append(findings, "services/"+name+
				" — коллектор собран под именем \""+c.collector+"\": серия уехала бы под чужим "+
				"именем сервиса, и на панели это выглядело бы как чужая нагрузка")
		case !c.registered:
			findings = append(findings, "services/"+name+
				" — коллектор собран, но НЕ зарегистрирован: вызова RegisterListNarrow нет ни в "+
				"одном не-тестовом файле сервиса. Незарегистрированный коллектор не отдаёт "+
				"ничего, и его отсутствие на поверхности выглядит ровно как «сужений не было»")
		}
	}
	if len(findings) > 0 {
		t.Fatalf("потребитель сужателя без наблюдаемых величин:\n  %s", strings.Join(findings, "\n  "))
	}
}
