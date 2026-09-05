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
// # ЗДЕСЬ БЫЛ ПЕРЕЧЕНЬ, И ЭТО БЫЛА ТА ЖЕ ОШИБКА В ДРУГОМ МЕСТЕ (#1221)
//
// Прежняя редакция судила ЧЕТЫРЕ выписанных руками носителя. Тогда накопитель
// без читателя находился только среди перечисленных, а следующий заводился без
// читателя молча — то есть проверка требовала свойства от кода, который уже
// есть, вместо кода, которого ещё нет. Перечень при этом честно называл себя
// объёмом, а не списком прощённых; беда не в нечестности, а в том, что объём
// пополняется рукой, а дерево растёт само.
//
// Замер на ревизии ec133b94e (предикат — этот же обход): накопителей в дереве
// **16**, в перечне значились **4**, читателя не имели **2**. То есть перечень
// покрывал четверть предмета, и оба настоящих нарушения лежали ВНЕ него.
//
// # ЧТО СЧИТАЕТСЯ НАКОПИТЕЛЕМ — предикат, а не список
//
// Носитель — структурный тип, у которого есть И ТО И ДРУГОЕ:
//
//   - хотя бы одно поле счётного атомарного типа (`atomic.Uint64`, `Int64`,
//     `Uint32`, `Int32`, `Bool`) — то есть величина, накапливаемая на горячем
//     пути без замка;
//   - ЭКСПОРТИРУЕМЫЙ метод-слепок: без параметров, с результатом, чьё тело
//     читает `<получатель>.<это самое поле>.Load()`.
//
// Связь «метод читает ИМЕННО ЭТО поле» проверяется по получателю, а не по имени
// метода: имя не гарантирует ничего.
//
// # ЗАЩЁЛКА — НЕ ВЕЛИЧИНА, и это выяснила ИНЪЕКЦИЯ, а не чтение
//
// Первая редакция предиката считала накопителем всякий тип со счётным атомарным
// полем. Законный близнец, поставленный рядом с настоящей находкой
// (`Latch{closed atomic.Bool}` с методом `Closed() bool`), тут же покраснел — и
// показал, что предикат ловит ФОРМУ, а не существо: защёлки `closed`/`stopped`/
// `ready` есть в каждом втором пакете, внешнего читателя у них не бывает by
// construction, и гейт, красный на них, сняли бы следующим как ложный.
//
// Отсюда вычет ровно одной вырожденной формы: метод, ВСЁ чтение которого — одна
// или несколько `atomic.Bool`, и результат которого — голый `bool`, сообщает
// СОСТОЯНИЕ, а не величину. Состояние читают «каково оно сейчас», и двусмыслицы
// «ноль событий против ноль исполнений», ради которой гейт существует, у него
// нет. Вычет узок намеренно: снимок, отдающий СТРУКТУРУ (даже собранную вокруг
// одной защёлки — так устроен `BasicCredentialCacheStats`), и всякое чтение
// счётчика остаются накопителями.
//
// # ЧТО СЧИТАЕТСЯ ЧИТАТЕЛЕМ
//
// Не-тестовая ссылка на метод-слепок — вызов `x.Stats()` ЛИБО значение метода
// `x.Stats`, переданное как читатель, — из пакета, который (а) не объявляет сам
// носитель и (б) ИМПОРТИРУЕТ пакет-объявитель.
//
// Оба условия нужны. Без (а) засчитывался бы внутренний вызов: полоса базового
// секрета читает свой же слепок, чтобы напечатать предупреждение, и это не
// «величина вышла наружу». Без (б) засчитывалось бы любое совпадение имени в
// дереве — а `Stats` и `Counts` носят десятки типов.
//
// Разбор синтаксического дерева, а не текста: имя метода-слепка встречается и в
// комментариях, объясняющих эту же провязку, и текстовый поиск принял бы
// объяснение за исполнение — ровно тот класс, который гейт и ловит.
//
// # ОТКРЫТЫЕ НАХОДКИ НАЗВАНЫ, А НЕ ПРОЩЕНЫ
//
// Таблица [openAccumulatorFindings] — это НЕ список прощённых: каждая запись
// называет предмет, чинит который другая полоса, и обязана истечь сама. Запись,
// чей носитель исчез либо обзавёлся читателем, роняет гейт: исключение живёт,
// пока у него есть предмет (`testing.md` §«Гейт на класс», п.5).
//
// # ЧТО ЭТОТ ГЕЙТ НЕ ПРОВЕРЯЕТ, и где это проверяется вместо него
//
// Он спрашивает «есть ли читатель ХОТЬ У КОГО-ТО», а не «есть ли читатель у
// КАЖДОГО потребителя». Второе — свойство иной формы, и оно нужно ровно там, где
// носитель общего фундамента строит каждый сервис сам: сужатель списков.
// Прежняя редакция несла его отдельным режимом записи перечня; сегодня его
// держит СВОЙ гейт, `TestEveryListNarrowConsumerRegistersItsCollector`, — он
// строже (требует коллектор у того же потребителя) и падает на пустом обходе.
// Там же живёт и самоистечение исключения для каталога дублёров: две проверки
// одного предиката разошлись бы молча.

// atomicObservableTypes — типы `sync/atomic`, чьё поле может нести НАБЛЮДАЕМУЮ
// величину. Именованный набор, а не проверка по префиксу.
//
// `atomic.Pointer`/`atomic.Value` сюда не входят: они держат объект, а не
// величину, и «прочитанного нуля» у них не бывает.
//
// `atomic.Bool` входит, хотя сам по себе он защёлка. Причина в том, что снимок
// законно собирается ВОКРУГ защёлки: `BasicCredentialCacheStats` отдаёт
// занятость, потолок и признак «потолок достигался» одной структурой, и
// выбросив Bool отсюда, обход потерял бы её целиком. Вырожденная форма —
// защёлка, отданная голым `bool`, — вычитается ниже, у самого метода-слепка
// ([returnsBareBool]), а не здесь: вычитать её на уровне ПОЛЯ значило бы судить
// не то место.
var atomicObservableTypes = map[string]bool{
	"Uint64": true, "Int64": true, "Uint32": true, "Int32": true, "Bool": true,
}

// accumulatorRoots — корни обхода. Те же, что у прочих гейтов провязки: код,
// который поднимается в процессе платформы.
var accumulatorRoots = []string{"services", "gateway", "pkg"}

// modulePathPrefix объявлен ОДИН раз на пакет — в `subscriptionkindvocabulary.go`,
// у второго его потребителя. Здесь стояло второе объявление того же значения; оно
// снято, потому что два написания одного предмета расходятся молча — ровно тот
// класс, который гейт-сосед и ловит.

// openAccumulatorFinding — накопитель без читателя, найденный обходом и
// оставленный ЧУЖОЙ полосе.
//
// Не прощение: запись обязана нести предмет и предикат снятия, и гейт роняет
// прогон, как только предмета не станет.
type openAccumulatorFinding struct {
	// dir / typ / accessor — координата носителя ровно в той форме, в какой её
	// печатает обход. Разъедется с деревом — запись потеряет предмет и упадёт.
	dir, typ, accessor string
	// owner — кто чинит и по какому предмету.
	owner string
}

// openAccumulatorFindings — открытые находки обхода, каждая со своим владельцем.
//
// СЕЙЧАС ПУСТА, и это её нормальное состояние, а не поломка: пустая ведомость
// есть цель, ради которой ведомость заведена. Гейт на ней проходит — падать он
// обязан на находке БЕЗ записи и на записи БЕЗ предмета, а не на их отсутствии
// (`testing.md` §«Чтение вердикта», п.4: проба не имеет права падать на
// достижении своей цели).
//
// Здесь стояли две записи о накопителях вердиктного разбора iam — они найдены
// этим же гейтом при переводе его с перечня на обход (#1221) и сняты вместе со
// своим предметом в #1224: у обеих величин теперь есть не-тестовый читатель
// (коллектор наблюдаемости iam, провязанный в композиционном корне).
//
// Заводя запись, назови находку, её владельца и предикат снятия: гейт роняет
// прогон, как только прощать станет нечего.
var openAccumulatorFindings []openAccumulatorFinding

// accumulator — один найденный обходом носитель величин.
type accumulator struct {
	dir      string // каталог пакета-объявителя
	pkg      string // имя пакета (для читаемости находки)
	typ      string
	accessor string
	where    string // файл:строка объявления метода-слепка
}

// id — координата носителя одной строкой; ключ сверки с таблицей находок.
func (a accumulator) id() string { return a.dir + "." + a.typ + "." + a.accessor }

// goFileFacts — то, что обход вынимает из одного файла.
type goFileFacts struct {
	rel     string
	dir     string
	pkgName string
	file    *ast.File
	// fset — набор, в котором разобран ИМЕННО ЭТОТ файл. Позиция осмысленна
	// только в своём наборе: чужой отдаёт координату другого файла, и находка
	// уводила бы читателя не туда — молча и правдоподобно.
	fset *token.FileSet
	// imports — каталоги ЭТОГО модуля, которые файл импортирует.
	imports map[string]bool
}

// receiverTypeName — имя типа получателя.
//
// Формы четыре, и все четыре — один и тот же тип: `T`, `*T`, `T[K, V]`,
// `*T[K, V]`. Обобщённые вписаны не для полноты: первая редакция их не читала,
// и слепой зоной оказался кэш общего примитива вытеснения — тот самый носитель,
// ради величины которого гейт в этот раз и трогали. Перепись назвала на один
// накопитель меньше, чем есть, и это было неотличимо от исправной работы.
func receiverTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.IndexExpr: // T[K]
		return receiverTypeName(t.X)
	case *ast.IndexListExpr: // T[K, V]
		return receiverTypeName(t.X)
	}
	return ""
}

// isAtomicObservableField — поле атомарного типа, способного нести наблюдаемую
// величину (`atomic.Uint64` и т. п.).
func isAtomicObservableField(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "atomic" && atomicObservableTypes[sel.Sel.Name]
}

// isAtomicBoolField — поле-защёлка (`atomic.Bool`).
func isAtomicBoolField(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "atomic" && sel.Sel.Name == "Bool"
}

// returnsBareBool — результат метода есть ровно один голый `bool`.
//
// Именно голый: снимок, отдающий структуру, остаётся снимком, даже если внутри
// у него одна защёлка (так устроен `BasicCredentialCacheStats`).
func returnsBareBool(results *ast.FieldList) bool {
	if countFields(results) != 1 {
		return false
	}
	id, ok := results.List[0].Type.(*ast.Ident)
	return ok && id.Name == "bool"
}

// countFields — сколько имён объявляет список параметров/результатов.
// Безымянный элемент — тоже один.
func countFields(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
			continue
		}
		n += len(f.Names)
	}
	return n
}

// readAccumulatorTree разбирает не-тестовое дерево один раз и отдаёт факты по
// каждому файлу плюс объём осмотренного.
func readAccumulatorTree(t *testing.T) (facts []goFileFacts, scanned int) {
	t.Helper()
	fset := token.NewFileSet()
	walkOwnerRegisterGoFiles(t, repoRoot(t), accumulatorRoots, func(rel string, body []byte) {
		scanned++
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		imports := map[string]bool{}
		for _, im := range file.Imports {
			p, uerr := strconv.Unquote(im.Path.Value)
			rel, own := treeRelOfImport(p)
			if uerr != nil || !own {
				continue
			}
			imports[rel] = true
		}
		facts = append(facts, goFileFacts{
			rel: rel, dir: path.Dir(filepath.ToSlash(rel)),
			pkgName: file.Name.Name, file: file, fset: fset, imports: imports,
		})
	})
	return facts, scanned
}

// collectAccumulators — носители, объявленные деревом.
func collectAccumulators(t *testing.T, facts []goFileFacts) []accumulator {
	t.Helper()
	// atomicFieldsOf: «каталог.тип» → имена его счётных атомарных полей.
	// boolFields — те из них, что суть защёлки (`atomic.Bool`).
	atomicFieldsOf := map[string]map[string]bool{}
	boolFields := map[string]map[string]bool{}
	for _, f := range facts {
		ast.Inspect(f.file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, fld := range st.Fields.List {
				if !isAtomicObservableField(fld.Type) {
					continue
				}
				key := f.dir + "." + ts.Name.Name
				if atomicFieldsOf[key] == nil {
					atomicFieldsOf[key] = map[string]bool{}
					boolFields[key] = map[string]bool{}
				}
				latch := isAtomicBoolField(fld.Type)
				for _, nm := range fld.Names {
					atomicFieldsOf[key][nm.Name] = true
					if latch {
						boolFields[key][nm.Name] = true
					}
				}
			}
			return true
		})
	}

	var out []accumulator
	for _, f := range facts {
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || !fd.Name.IsExported() {
				continue
			}
			recv := fd.Recv.List[0]
			typeName := receiverTypeName(recv.Type)
			if typeName == "" || len(recv.Names) == 0 {
				continue // получатель без имени поля читать не может
			}
			recvName := recv.Names[0].Name
			fields := atomicFieldsOf[f.dir+"."+typeName]
			if len(fields) == 0 {
				continue
			}
			// Слепок: без параметров, с результатом.
			if countFields(fd.Type.Params) != 0 || countFields(fd.Type.Results) == 0 {
				continue
			}
			// И тело читает ИМЕННО ЭТИ поля через получателя. Проверка по
			// получателю, а не по имени метода: имя не гарантирует ничего.
			reads, onlyLatches := false, true
			ast.Inspect(fd, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Load" {
					return true
				}
				field, ok := sel.X.(*ast.SelectorExpr)
				if !ok || !fields[field.Sel.Name] {
					return true
				}
				id, ok := field.X.(*ast.Ident)
				if !ok || id.Name != recvName {
					return true
				}
				reads = true
				if !boolFields[f.dir+"."+typeName][field.Sel.Name] {
					onlyLatches = false
				}
				return true
			})
			if !reads {
				continue
			}
			// Вычет вырожденной формы: одна защёлка → голый `bool`. См. разбор в
			// шапке — его нашла инъекция законного близнеца, а не чтение.
			if onlyLatches && returnsBareBool(fd.Type.Results) {
				continue
			}
			out = append(out, accumulator{
				dir: f.dir, pkg: f.pkgName, typ: typeName, accessor: fd.Name.Name,
				where: f.rel + ":" + fmtLine(f.fset, fd.Pos()),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id() < out[j].id() })
	return out
}

// collectAccumulatorReaders — где в дереве ссылаются на метод с таким именем:
// «имя слепка» → каталоги пакетов, в которых он вызван либо передан значением.
func collectAccumulatorReaders(facts []goFileFacts) map[string]map[string]string {
	out := map[string]map[string]string{}
	note := func(name, dir, rel string) {
		if out[name] == nil {
			out[name] = map[string]string{}
		}
		if _, seen := out[name][dir]; !seen {
			out[name][dir] = rel
		}
	}
	for _, f := range facts {
		ast.Inspect(f.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// И вызов `x.Stats()`, и значение метода `x.Stats` — одинаково
			// законные формы читателя: предмет в том, ОТКУДА берутся числа, а не
			// в том, сколько скобок по дороге. Требовать одну форму значило бы
			// требовать ритуала — а гейт, красный на законной форме, снимают
			// следующим как ложный.
			note(sel.Sel.Name, f.dir, f.rel)
			return true
		})
	}
	return out
}

// importersOfDirs — каталог-объявитель → каталоги, которые его импортируют.
func importersOfDirs(facts []goFileFacts) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range facts {
		for dir := range f.imports {
			if out[dir] == nil {
				out[dir] = map[string]bool{}
			}
			out[dir][f.dir] = true
		}
	}
	return out
}

// accumulatorVerdict — ЧИСТОЕ суждение: находки, просроченные записи и перепись.
//
// Отделено от обхода намеренно: инъекция подаёт сюда синтетический корпус и
// проверяет, что суждение способно упасть и способно смолчать, — на настоящем
// дереве ни того ни другого не показать, не сломав его.
func accumulatorVerdict(accs []accumulator, readers map[string]map[string]string,
	importersOf map[string]map[string]bool, ledger []openAccumulatorFinding,
) (findings, stale []string, withReader, open int) {
	forgiven := map[string]openAccumulatorFinding{}
	for _, f := range ledger {
		forgiven[f.dir+"."+f.typ+"."+f.accessor] = f
	}
	seen := map[string]bool{}
	for _, a := range accs {
		seen[a.id()] = true
		reader := readerOf(a, readers, importersOf)
		if reader != "" {
			withReader++
			// Запись, которой больше нечего прощать, — находка: иначе она
			// переживёт свой предмет и следующий унаследует слепую зону.
			if f, ok := forgiven[a.id()]; ok {
				stale = append(stale, a.id()+" — величины ЧИТАЕТ "+reader+
					", а таблица открытых находок всё ещё числит их непрочитанными ("+
					f.owner+"): снимите запись")
			}
			continue
		}
		if _, ok := forgiven[a.id()]; ok {
			open++
			continue
		}
		findings = append(findings, a.where+" — "+a.pkg+"."+a.typ+"."+a.accessor+
			"() не читает ни один не-тестовый пакет, импортирующий "+a.dir+
			": величина считается в никуда, и её ноль не отличим от «этот код не исполнялся»")
	}
	// Запись о носителе, которого в дереве больше нет, — тоже находка.
	for id, f := range forgiven {
		if !seen[id] {
			stale = append(stale, id+" — носителя в дереве нет ("+f.owner+
				"): предмет записи отпал, снимите её")
		}
	}
	sort.Strings(findings)
	sort.Strings(stale)
	return findings, stale, withReader, open
}

// readerOf называет файл, из которого величины носителя действительно читают, —
// либо пусто, если такого нет.
func readerOf(a accumulator, readers map[string]map[string]string, importersOf map[string]map[string]bool) string {
	for dir, rel := range readers[a.accessor] {
		// Требование ровно одно: читающий пакет ИМПОРТИРУЕТ объявителя.
		//
		// Оно закрывает сразу две вещи. Совпадение имени без импорта — не
		// читатель (`Stats` и `Counts` носят десятки типов). И чтение в СВОЁМ
		// пакете тоже не читатель: пакет не импортирует сам себя, поэтому
		// собственный вызов сюда не попадает by construction — а он бывает и
		// законен (полоса базового секрета читает свой же слепок, чтобы
		// напечатать предупреждение), и величиной наружу от этого не становится.
		//
		// Отдельной проверки «не свой каталог» здесь СТОЯЛО, и она снята: она
		// была недостижима, и проба, якобы её державшая, оставалась зелёной при
		// её снятии. Ослабнет условие импорта — покраснеет
		// TestAccumulatorGateDoesNotCountAReadInTheDeclaringPackage.
		if !importersOf[a.dir][dir] {
			continue
		}
		return rel
	}
	return ""
}

// consumerDir — каталог ПОТРЕБИТЕЛЯ, которому принадлежит файл: `services/<svc>`
// для сервиса, каталог самого файла для всего остального.
//
// Используется соседним гейтом (`listnarrowcollector_test.go`): читатель обязан
// быть у того же потребителя, который носитель построил, — иначе «читатель
// есть» означало бы «где-нибудь в дереве кто-то его читает», а это не то
// свойство.
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
// не является: его сужатели живут ровно столько, сколько живёт проба.
//
// Существование каталога проверяет тот гейт, который им и пользуется
// (`listnarrowcollector_test.go`: `harnessSites == 0` роняет прогон). Здесь
// проверки нет намеренно — второе место об одном предикате разошлось бы молча.
const narrowTestHarness = "pkg/listnarrow/narrowtest"

// TestDeclaredAccumulatorsHaveANonTestReader — у КАЖДОГО накопителя, объявленного
// где угодно в дереве, есть не-тестовый читатель величин.
func TestDeclaredAccumulatorsHaveANonTestReader(t *testing.T) {
	facts, scanned := readAccumulatorTree(t)
	accs := collectAccumulators(t, facts)
	readers := collectAccumulatorReaders(facts)
	findings, stale, withReader, open := accumulatorVerdict(
		accs, readers, importersOfDirs(facts), openAccumulatorFindings)

	// Объём осмотренного — отдельное утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("осмотрено не-тестовых файлов Go: %d; накопителей объявлено: %d; "+
		"из них с читателем: %d; открытых находок чужих полос: %d",
		scanned, len(accs), withReader, open)
	if scanned == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if len(accs) == 0 {
		t.Fatal("накопителей в дереве не найдено ни одного — предикат обхода разъехался с " +
			"кодом, и молчание гейта означает не благополучие, а слепоту")
	}

	if len(stale) > 0 {
		t.Errorf("таблица открытых находок пережила свой предмет:\n  %s", strings.Join(stale, "\n  "))
	}
	if len(findings) > 0 {
		t.Fatalf("накопители считают в никуда — их ноль ничего не утверждает:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
