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
	// verdictLane — объявлена ли ПОЛОСА ОКНА ВЕРДИКТОВ сужателя
	// (`authzmetrics.LaneNarrow`) в композиционном корне.
	//
	// Отдельно от `registered`, потому что это ДРУГОЙ предмет: коллектор
	// `narrowmetrics` считает ИСХОДЫ СТРАНИЦЫ (сужено / аварийный режим / мягкий
	// проход) и про окно вердиктов не говорит ничего. Через это окно проходит
	// БОЛЬШЕ вопросов, чем через окно звена решения: звено спрашивает раз на
	// вызов, сужатель — на каждый элемент страницы (#768).
	verdictLane bool
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
		// Дешёвый отсев. Подстрочного совпадения ДОСТАТОЧНО, чтобы файл
		// пропустить, и НЕ достаточно, чтобы что-либо засчитать: решает разбор по
		// узлам вызова ниже. Поэтому хвост чужого идентификатора здесь стоит
		// лишнего разбора, а не ложного вердикта (замер по 4983 файлам Go:
		// хвостов у всех трёх имён — ноль).
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

		// Полоса окна вердиктов объявляется КЛЮЧОМ карты читателей, а не вызовом,
		// поэтому ищется отдельным проходом по выражениям выбора: `authzmetrics.LaneNarrow`.
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "LaneNarrow" {
				return true
			}
			pkgIdent, isQualified := sel.X.(*ast.Ident)
			if !isQualified || pkgIdent.Name != "authzmetrics" || !inServices {
				return true
			}
			get(service).verdictLane = true
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

// TestEveryListNarrowConsumerExportsItsVerdictWindow — у каждого потребителя
// сужателя доля попаданий ЕГО ОКНА ВЕРДИКТОВ выставлена (#768).
//
// # Почему это ОТДЕЛЬНОЕ требование, а не часть предыдущего
//
// Коллектор `narrowmetrics` считает ИСХОДЫ СТРАНИЦЫ — сужено, аварийный режим,
// мягкий проход, — и про окно вердиктов не говорит ничего. Между тем именно оно
// решает, сколько вопросов доезжает до владельца модели под списочной нагрузкой:
// звено решения задаёт ОДИН вопрос на вызов, а сужатель — по вопросу на КАЖДЫЙ
// элемент страницы, а страница контрактно бывает до тысячи. До #768 у окна был
// размер и не было ни одного счётчика попаданий, то есть утверждение «кеш
// сужателя даёт столько-то» было непроверяемо в обе стороны.
//
// # Предпосылка
//
// Гейт судит ТЕХ ЖЕ потребителей, что и проба выше, и падает на своей же
// предпосылке, если их не нашлось: молчание на пустом перечне ничего не
// доказывает.
func TestEveryListNarrowConsumerExportsItsVerdictWindow(t *testing.T) {
	consumers, _, scanned := collectNarrowConsumers(t)

	services := make([]string, 0, len(consumers))
	for name, c := range consumers {
		if len(c.ctorSites) > 0 {
			services = append(services, name)
		}
	}
	sort.Strings(services)
	exporting := 0
	for _, name := range services {
		if consumers[name].verdictLane {
			exporting++
		}
	}
	t.Logf("осмотрено не-тестовых файлов Go: %d; потребителей сужателя: %d (%s); "+
		"объявляют полосу окна вердиктов: %d",
		scanned, len(services), strings.Join(services, ", "), exporting)

	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if len(services) == 0 {
		t.Fatal("потребителей сужателя не нашлось — предмет гейта отпал: снимите его вместе " +
			"с сужателем либо почините имена, которыми он его ищет")
	}

	var findings []string
	for _, name := range services {
		if consumers[name].verdictLane {
			continue
		}
		findings = append(findings, "services/"+name+
			" — строит сужатель ("+consumers[name].ctorSites[0]+"), но полосу окна его "+
			"вердиктов не объявляет: передайте authzmetrics.LaneNarrow с читателем "+
			"<сужатель>.CacheStats в RegisterAuthzCache композиционного корня. Через это "+
			"окно идёт по вопросу на КАЖДЫЙ элемент страницы, а страница контрактно бывает "+
			"до тысячи — то есть больше вопросов, чем через окно звена решения")
	}
	if len(findings) > 0 {
		t.Fatalf("потребитель(и) сужателя не выставляют долю попаданий своего окна вердиктов:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
