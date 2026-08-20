// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// itoaLocal — число в текст падения. Отдельно от `strconv` по месту, чтобы
// строка находки читалась одной конкатенацией.
func itoaLocal(n int) string { return strconv.Itoa(n) }

// accumulatorreader_test.go — накопитель, который никто не читает, считает в
// никуда, и его ноль ничего не утверждает.
//
// # Предмет
//
// Счётчик, чья величина не выходит наружу, отвечает на два разных вопроса одним
// молчанием: «событие не случилось ни разу» и «код, который его считает, вообще
// не исполнялся». Пока эти состояния неразличимы, ноль в счётчике отказов
// означает тишину, а не благополучие. Это ровно то, чего требуют
// `security.md` §Hardening-инвариант 8 («ноль отказов за всю жизнь контроля»
// обязано быть заметно) и `data-integrity.md` («ноль доставленных строк за всю
// жизнь очереди» обязано быть заметно).
//
// # Почему гейт, а не разовая правка
//
// Провязка наблюдаемости всюду объявлена необязательной и nil-безопасной — так и
// задумано, наблюдение не должно ронять сборку. Поэтому её пропажу не поймает ни
// компилятор, ни один тест самого накопителя: он останется зелёным, продолжая
// считать в пустоту. Свойство «величина доходит до читателя» — свойство ДЕРЕВА,
// и держать его может только обход дерева.
//
// # Разбор синтаксического дерева, а не текста
//
// Имя метода-слепка встречается и в комментариях, объясняющих эту же провязку.
// Текстовый поиск принял бы объяснение за исполнение — ровно тот класс, который
// гейт и ловит.
//
// # ОДИН ХОП ЧЕРЕЗ ИМЕНОВАННОЕ ПОЛЕ — не послабление, а форма, которой пишут
//
// Носитель редко строится и читается в одной функции: композиционный корень
// собирает его там, где есть его зависимости, отдаёт наружу ИМЕНОВАННЫМ полем и
// читает у вызывающего, где есть реестр наблюдаемости. Гейт, засчитывающий чтение
// только на голом идентификаторе, краснеет на этой — законной и здесь
// единственной — форме, а гейт, красный на законном близнеце, снимают следующим
// как ложный. Поэтому чтение засчитывается и на `<что-то>.<поле>.<слепок>()`, но
// ТОЛЬКО если построенное значение действительно кладут в поле с этим именем
// (составной литерал `<поле>: <имя>`). Совпадения имён недостаточно: без записи в
// поле имя не связано с носителем, и гейт остаётся красным.
//
// Дальше одного хопа гейт не идёт намеренно: перенос значения через произвольное
// число присваиваний — анализ потока данных, который на неполной реализации
// зеленел бы молча. Красный на честной провязке дороже разбирать один раз, чем
// зелёный на несуществующей — никогда.
//
// # ОБЪЁМ ГЕЙТА НАЗВАН ЧЕСТНО
//
// Таблица ниже — это ОБЪЁМ проверки, а не список прощённых: накопитель, которого
// в ней нет, гейтом не покрыт. На 2026-08-12 перепись дерева по предикату
// «атомарный счётчик в не-тестовом коде, чей метод-слепок не имеет ни одного
// не-тестового вызывающего» дала ПЯТЬ носителей; два из них — оба в services/iam
// — внесены сюда первыми, три оставшихся (счётчики решений края, клиент проверки
// прав края и счётчики сужения списков общего фундамента) требовали поверхностей,
// которых в дереве не было вовсе, и закрыты отдельным изменением. Сегодня в
// таблице все пять. Каждая запись обязана иметь предмет: исчезнет конструктор —
// гейт упадёт на своей же предпосылке, а не промолчит.
//
// # ДВА СПОСОБА НАЙТИ ЧИТАТЕЛЯ, и второй нужен для общего фундамента
//
// Носитель, который сборщик оставляет у себя, ищется по ИМЕНИ (см. выше про один
// хоп через поле). Но конструктор общего фундамента зовут не корни, а обёртки
// сервисов, и наружу носитель уходит ВОЗВРАЩАЕМЫМ значением: имя, под которым
// его читает корень, с именем в обёртке не связано ничем, и требовать их
// совпадения значило бы требовать ритуала, а не свойства.
//
// Для таких носителей действует режим [accumulatorSubject.perConsumerDir]: гейт
// требует, чтобы В КАЖДОМ каталоге сервиса, где конструктор зовут, нашёлся
// не-тестовый вызов метода-слепка. Это СТРОЖЕ совпадения имён — читатель обязан
// быть у того же потребителя, а не где-нибудь в дереве, — и не требует анализа
// потока данных, который на неполной реализации зеленел бы молча.

// accumulatorSubject — один носитель счётчиков, у которого обязан быть читатель.
type accumulatorSubject struct {
	// pkg / ctor — как конструктор носителя пишется в дереве
	// (`<pkg>.<ctor>(…)`). Разъедется с кодом — перепись найдёт ноль вхождений и
	// гейт упадёт.
	pkg  string
	ctor string
	// accessor — метод, которым величины читаются.
	accessor string
	// why — зачем этому накопителю читатель. Идёт в текст падения: гейт без
	// объяснения снимают следующим как непонятную проверку.
	why string
	// perConsumerDir — искать читателя ПО КАТАЛОГУ ПОТРЕБИТЕЛЯ, а не по имени
	// переменной. См. разбор в шапке файла: носитель общего фундамента уходит из
	// обёртки возвращаемым значением, и связь по имени тут не выражается ничем.
	perConsumerDir bool
}

var observedAccumulators = []accumulatorSubject{
	{
		pkg: "jwksproxyhttp", ctor: "NewHandler", accessor: "Stats",
		why: "зеркало ключей отказывает закрыто, поэтому «отказов не было» обязано быть " +
			"отличимо от «сюда никто не приходил»: второе означает мёртвую плоскость данных",
	},
	{
		pkg: "middleware", ctor: "NewAuthzMetrics", accessor: "Counts",
		why: "это самый нагруженный пункт принятия решения о доступе: без читателя " +
			"«отказов не было» и «край не спрашивали» — одно и то же молчание, а полоса " +
			"объявленного мягкого прохода невидима вовсе",
	},
	{
		pkg: "clients", ctor: "NewIAMAuthorizeClient", accessor: "CallsTotal",
		why: "величина считает обращения ПО ПРОВОДУ, включая повторы, и из числа решений " +
			"не выводится: их отношение и есть ответ на вопрос, спрашивает край по разу " +
			"или долбит владельца прав",
	},
	{
		pkg: "listnarrow", ctor: "New", accessor: "Counts", perConsumerDir: true,
		why: "три из четырёх полос сужателя означают страницу, ушедшую БЕЗ пообъектной " +
			"проверки; без читателя их ноль неотличим от сужателя, которого не звали ни разу",
	},
}

// accumulatorSite — одно место сборки носителя.
type accumulatorSite struct {
	where string
	// bound — имя переменной, которой присвоен результат конструктора. Пусто
	// означает, что носитель построен прямо в аргументе: прочитать его счётчики
	// некому по построению.
	bound string
}

// collectAccumulatorWiring переписывает не-тестовое дерево и возвращает по
// каждому предмету: где носитель собран, какие имена читаются, в какие поля
// значение кладут, сколько файлов осмотрено.
func collectAccumulatorWiring(t *testing.T) (sites map[int][]accumulatorSite, reads map[int]map[string]int,
	fieldsOf map[string]map[string]bool, scanned int) {
	t.Helper()
	sites, reads, fieldsOf, _, _, scanned = collectAccumulatorWiringByDir(t)
	return sites, reads, fieldsOf, scanned
}

// consumerDir — каталог ПОТРЕБИТЕЛЯ, которому принадлежит файл: `services/<svc>`
// для сервиса, каталог самого файла для всего остального.
//
// Единица, по которой судятся носители общего фундамента: читатель обязан быть у
// того же потребителя, который носитель построил, — иначе «читатель есть»
// означало бы «где-нибудь в дереве кто-то его читает», а это не то свойство.
//
// У сервиса единицей взят весь его каталог, а не каталог файла: обёртка и
// композиционный корень лежат в разных пакетах ОДНОГО сервиса, и требовать
// читателя в том же пакете значило бы требовать не того.
func consumerDir(rel string) string {
	slash := filepath.ToSlash(rel)
	parts := strings.Split(slash, "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[0] + "/" + parts[1]
	}
	return path.Dir(slash)
}

// narrowTestHarness — каталог дублёров сужателя для проб сервисов.
//
// Он не-тестовый по расширению файла и потому попадает в обход, но потребителем
// не является: его сужатели живут ровно столько, сколько живёт проба. Исключение
// названо ЗДЕСЬ и проверяется на существование — запись, которой больше нечего
// исключать, роняет гейт вместе с остальными.
const narrowTestHarness = "pkg/listnarrow/narrowtest"

// collectAccumulatorWiringByDir — то же, что [collectAccumulatorWiring], плюс
// разбивка мест сборки и чтений ПО КАТАЛОГУ ПОТРЕБИТЕЛЯ.
func collectAccumulatorWiringByDir(t *testing.T) (sites map[int][]accumulatorSite, reads map[int]map[string]int,
	fieldsOf map[string]map[string]bool, dirSites map[int]map[string]int, dirReads map[int]map[string]int, scanned int) {
	t.Helper()
	root := repoRoot(t)
	sites = map[int][]accumulatorSite{}
	reads = map[int]map[string]int{}
	dirSites = map[int]map[string]int{}
	dirReads = map[int]map[string]int{}
	// fieldsOf: имя переменной → множество имён полей, в которые её кладут
	// составным литералом. Один хоп переноса, ради которого он и собирается.
	fieldsOf = map[string]map[string]bool{}
	for i := range observedAccumulators {
		reads[i] = map[string]int{}
		dirSites[i] = map[string]int{}
		dirReads[i] = map[string]int{}
	}

	walkOwnerRegisterGoFiles(t, root, []string{"services", "gateway", "pkg"}, func(rel string, body []byte) {
		scanned++
		src := string(body)
		interesting := false
		for _, s := range observedAccumulators {
			if strings.Contains(src, s.pkg+"."+s.ctor) || strings.Contains(src, "."+s.accessor+"()") {
				interesting = true
				break
			}
		}
		if !interesting {
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}

		// qualifiedCall распознаёт `<pkg>.<name>(…)`.
		qualifiedCall := func(call *ast.CallExpr, pkg, name string) bool {
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != name {
				return false
			}
			id, ok := sel.X.(*ast.Ident)
			return ok && id.Name == pkg
		}

		// Сначала — присваивания: они дают ИМЯ построенного носителя.
		boundAt := map[token.Pos]string{}
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range as.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || i >= len(as.Lhs) {
					continue
				}
				if id, ok := as.Lhs[i].(*ast.Ident); ok {
					boundAt[call.Lparen] = id.Name
				}
			}
			return true
		})

		// Затем — составные литералы: `<поле>: <имя>` переносит значение под
		// именем поля, и дальше его читают у вызывающего как `<что-то>.<поле>`.
		ast.Inspect(file, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			field, ok := kv.Key.(*ast.Ident)
			if !ok {
				return true
			}
			value, ok := kv.Value.(*ast.Ident)
			if !ok {
				return true
			}
			if fieldsOf[value.Name] == nil {
				fieldsOf[value.Name] = map[string]bool{}
			}
			fieldsOf[value.Name][field.Name] = true
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for i, s := range observedAccumulators {
				if qualifiedCall(call, s.pkg, s.ctor) {
					sites[i] = append(sites[i], accumulatorSite{
						where: rel + ":" + fmtLine(fset, call.Lparen),
						bound: boundAt[call.Lparen],
					})
					dirSites[i][consumerDir(rel)]++
				}
				// Чтение засчитывается только как ВЫЗОВ метода-слепка: `X.Stats`
				// без скобок — это ссылка, а не чтение величины. Получателем
				// годится имя (`shadow.Counters()`) либо поле, в котором значение
				// лежит (`bundle.shadow.Counters()`) — связь имени с полем
				// проверяется ниже по fieldsOf, одного совпадения имён мало.
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == s.accessor {
					dirReads[i][consumerDir(rel)]++
					switch recv := sel.X.(type) {
					case *ast.Ident:
						reads[i][recv.Name]++
					case *ast.SelectorExpr:
						reads[i][recv.Sel.Name]++
					}
				}
			}
			return true
		})
	})
	return sites, reads, fieldsOf, dirSites, dirReads, scanned
}

// readThrough называет имя, под которым величины носителя действительно читают:
// собственное имя переменной либо поле, в которое её кладут. Пусто — читателя нет.
func readThrough(bound string, reads map[string]int, fieldsOf map[string]map[string]bool) string {
	if bound == "" {
		return ""
	}
	if reads[bound] > 0 {
		return bound
	}
	// Один хоп: значение положили в поле — читают через него.
	for field := range fieldsOf[bound] {
		if reads[field] > 0 {
			return field
		}
	}
	return ""
}

// TestDeclaredAccumulatorsHaveANonTestReader — у каждого объявленного накопителя
// есть не-тестовый читатель величин.
func TestDeclaredAccumulatorsHaveANonTestReader(t *testing.T) {
	sites, reads, fieldsOf, dirSites, dirReads, scanned := collectAccumulatorWiringByDir(t)

	// Объём осмотренного — отдельное утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	total := 0
	for i := range observedAccumulators {
		total += len(sites[i])
	}
	t.Logf("осмотрено не-тестовых файлов Go: %d; предметов в таблице: %d; мест сборки носителей: %d",
		scanned, len(observedAccumulators), total)
	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	// Исключение живёт, пока у него есть предмет: каталог дублёров исчез —
	// запись о нём стала находкой, а не тихой строкой.
	harnessSeen := false
	for i := range observedAccumulators {
		if dirSites[i][narrowTestHarness] > 0 {
			harnessSeen = true
		}
	}
	if !harnessSeen {
		t.Fatalf("исключению %q больше нечего исключать: дублёров сужателя в дереве нет — "+
			"снимите запись вместе с каталогом", narrowTestHarness)
	}

	var findings []string
	for i, s := range observedAccumulators {
		name := s.pkg + "." + s.ctor
		// Предпосылка записи: предмет существует. Исчезнет носитель — запись
		// обязана упасть, а не остаться вечно-зелёной строкой таблицы.
		if len(sites[i]) == 0 {
			findings = append(findings, name+
				" — в не-тестовом дереве нет ни одного места сборки: предмет записи отпал, "+
				"снимите её вместе с механизмом либо почините имена, которыми гейт его ищет")
			continue
		}
		if s.perConsumerDir {
			for dir, n := range dirSites[i] {
				if dir == narrowTestHarness {
					continue // дублёры проб — не потребители, см. narrowTestHarness
				}
				if dirReads[i][dir] > 0 {
					continue
				}
				findings = append(findings, dir+" — строит "+name+" ("+itoaLocal(n)+
					" мест), а величины не читает ни один его не-тестовый файл через ."+
					s.accessor+"() ("+s.why+")")
			}
			continue
		}
		for _, site := range sites[i] {
			switch {
			case site.bound == "":
				findings = append(findings, site.where+" — "+name+
					" построен прямо в аргументе: его счётчики некому прочитать по построению ("+
					s.why+")")
			case readThrough(site.bound, reads[i], fieldsOf) == "":
				findings = append(findings, site.where+" — счётчики "+site.bound+
					" не читает ни один не-тестовый вызывающий через ."+s.accessor+"() — "+
					"ни по имени, ни через поле, в которое их кладут ("+
					s.why+")")
			}
		}
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("накопители считают в никуда — их ноль ничего не утверждает:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
