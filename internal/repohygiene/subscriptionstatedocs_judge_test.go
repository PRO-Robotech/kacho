// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

// subscriptionstatedocs_judge_test.go — суждение гейта «страница подписки говорит про
// КАЖДОГО владельца то же, что делает его журнал».
//
// Судящие функции вынесены из пробы, чтобы доказательство способности гейта
// упасть прогоняло ИХ, а не свою копию. Файл тестовый по той же причине, что и
// у соседнего гейта того же предмета: состав дерева он берёт у одного и того же
// типа-поставщика (`subscriptionDocsLister`), объявленного там, и второе имя для
// той же формы разошлось бы с первым молча.

// stateCarrierType — форма имени типа состояния в клиентской странице.
//
// Предикат ПОЛОЖИТЕЛЬНЫЙ намеренно, и это не стиль. Соблазн искать отрицание
// («в клетке стоит „состояния нет“») велик и неисправим: ровно эта фраза стоит
// и у владельца, который состояние ПРОИЗВОДИТ, — у него нет состояния у снятия,
// и он обязан это сказать. Предикат по отрицанию считал бы такого владельца
// непроизводящим, то есть краснел бы на верном тексте и был бы снят первым.
//
// Названный тип сомнений не допускает: он либо назван, либо нет.
var stateCarrierType = regexp.MustCompile(`kacho\.cloud\.[a-z0-9]+\.v1\.[A-Za-z][A-Za-z0-9]*`)

// ownerRowRe — строка таблицы владельцев клиентской страницы.
var ownerRowRe = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)

// ownerCodeRe — первая ячейка строки: написание владельца.
var ownerCodeRe = regexp.MustCompile(`<code>([a-z][a-z0-9]*)</code>`)

// ownerCellRe — ячейки строки.
var ownerCellRe = regexp.MustCompile(`(?s)<td>(.*?)</td>`)

// journalStateReport — что гейт установил про журнал одного сервиса.
type journalStateReport struct {
	// service — каталог сервиса (`nlb`), а НЕ написание владельца
	// (`loadbalancer`): это две разные величины, и совпадают они не всегда.
	service string
	// stateFunc — имя функции состояния, найденной в объявлении журнала.
	stateFunc string
	// produces — доходит ли хоть один путь функции до упаковки состояния.
	produces bool
	// funcsWalked — сколько объявлений функций обойдено, пока это устанавливали.
	//
	// Печатается переписью, потому что расширение распознавателя обязано МЕНЯТЬ
	// это число: расширение, его не изменившее, ничего нового не осмотрело и
	// подлежит снятию, а не «оставлению на всякий случай».
	funcsWalked int
	// pkgFiles — не-тестовых файлов в пакете журнала.
	//
	// Предпосылка распознавателя: он обходит вызовы, объявленные в ОДНОМ файле
	// (`journal.go`). Она верна, пока пакет журнала этим файлом и исчерпывается.
	// Заведётся второй — помощник, до которого распознаватель не дойдёт, окажется
	// НЕ ОСМОТРЕН, а ответ «не собирает» будет ложным. Число печатается и судится,
	// чтобы предпосылка истекала сама, а не держалась памятью.
	pkgFiles int
	// parseErr — исходник не разобрался; молчание гейта по нему не утверждение.
	parseErr error
}

// ownerRowReport — что гейт прочитал в строке таблицы клиентской страницы.
type ownerRowReport struct {
	owner string
	// claim — текст последней ячейки: что страница обещает про состояние.
	claim string
	// claimsState — называет ли клетка тип состояния.
	claimsState bool
}

// subscriptionJournalStates — перепись журналов: производит ли `state` состояние.
//
// Судится УЗЕЛ разобранного дерева, а не подстрока: имя `anypb.New` стоит и в
// комментариях объявлений журналов (в том числе в объяснении, почему состояние
// НЕ производится), и сверка по тексту краснела бы на собственном объяснении.
func subscriptionJournalStates(root string, list subscriptionDocsLister) (
	reports []journalStateReport, filesRead int, err error,
) {
	files, err := list(filepath.Join(root, "services"), ".go")
	if err != nil {
		return nil, 0, err
	}
	fset := token.NewFileSet()
	for _, path := range files {
		slash := filepath.ToSlash(path)
		if !strings.Contains(slash, "/internal/subscriptionjournal/") {
			continue
		}
		if !strings.HasSuffix(slash, "/journal.go") {
			continue
		}
		filesRead++
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 || parts[0] != "services" {
			continue
		}
		rep := journalStateReport{service: parts[1], pkgFiles: countPkgFiles(files, path)}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			rep.parseErr = parseErr
			reports = append(reports, rep)
			continue
		}
		local := localFuncs(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			// Имя функции состояния объявляется владельцем и бывает как прямым
			// (`state`), так и построителем замыкания (`stateWithEndpoint`).
			// Обе формы законны и обе встречаются в дереве.
			if fn.Name.Name != "state" && !strings.HasPrefix(fn.Name.Name, "stateWith") {
				continue
			}
			rep.stateFunc = fn.Name.Name
			rep.produces, rep.funcsWalked = reachesStatePacking(fn, local)
			break
		}
		reports = append(reports, rep)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].service < reports[j].service })
	return reports, filesRead, nil
}

// localFuncs — объявления функций файла по имени (без методов).
func localFuncs(file *ast.File) map[string]*ast.FuncDecl {
	out := make(map[string]*ast.FuncDecl, len(file.Decls))
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil {
			continue
		}
		out[fn.Name.Name] = fn
	}
	return out
}

// journalCalleeName — имя вызываемой функции, если вызов адресован функции ПАКЕТА.
//
// Форм записи одного и того же вызова две, и обе законны: обычная (`f(x)`) и
// явно инстанцированная у обобщённой функции (`f[T](x)`, `f[K, V](x)` — узлы
// `IndexExpr`/`IndexListExpr`). Распознаватель, знающий лишь первую, на второй
// не даёт ни красного, ни зелёного — он МОЛЧИТ, и записанное второй формой
// оказывается не разрешённым, а не осмотренным.
//
// Пустая строка означает «вызов не к функции пакета» (метод, функция чужого
// пакета, значение в переменной).
func journalCalleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.IndexExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// packsDirectly — стоит ли упаковка состояния в теле самой функции.
//
// Упаковка — единственный способ отдать состояние общей форме: носитель события
// объявлен как `*anypb.Any`, и заполнить его иначе, чем `anypb.New`, нельзя.
//
// Судится УЗЕЛ вызова, а не подстрока: имя `anypb.New` стоит и в комментариях
// объявлений журналов — в том числе в объяснении, почему состояние НЕ
// производится, — и сверка по тексту краснела бы на собственном объяснении.
func packsDirectly(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "New" {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "anypb" {
			found = true
			return false
		}
		return true
	})
	return found
}

// reachesStatePacking — доходит ли функция состояния до упаковки, СВОИМ телом
// или через вызов функции своего пакета. Возвращает вердикт и число обойдённых
// объявлений.
//
// # Почему обход, а не «упаковка стоит прямо в state»
//
// Форм у одного предмета в этом дереве ДВЕ, и обе законны:
//
//   - упаковка в теле самой функции состояния — так у compute, storage, vpc, а
//     также у registry, где она стоит в замыкании, возвращаемом построителем
//     (лексически это то же объявление, и обход в него заходит);
//   - упаковка в ОБЩЕЙ полосе, которую функция состояния зовёт по видам — так у
//     nlb: разбор конверта, перенос записи в контракт и упаковка вынесены в одну
//     функцию, чтобы три её отказа классифицировались у всех видов одинаково.
//
// Распознаватель, знавший только первую, на второй молчал — и записанное ею
// оказалось не «разрешено», а НЕ ОСМОТРЕНО: гейт объявил журнал nlb
// непроизводящим при живом производителе и потребовал править ПРАВДИВУЮ
// страницу. Это тот самый класс, ради которого правило требует называть все
// законные формы записи предмета и доказывать инъекцией каждую.
//
// # Граница названа
//
// Обходятся вызовы, объявленные в ТОМ ЖЕ файле. Она держится предпосылкой, что
// пакет журнала этим файлом и исчерпывается, — предпосылка проверяется отдельно
// (`journalStateReport.pkgFiles`), а не подразумевается.
func reachesStatePacking(fn *ast.FuncDecl, local map[string]*ast.FuncDecl) (bool, int) {
	seen := map[string]bool{}
	walked := 0
	var walk func(*ast.FuncDecl) bool
	walk = func(cur *ast.FuncDecl) bool {
		walked++
		if packsDirectly(cur) {
			return true
		}
		reached := false
		ast.Inspect(cur, func(n ast.Node) bool {
			if reached {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := journalCalleeName(call)
			if name == "" || seen[name] {
				return true
			}
			next, ok := local[name]
			if !ok {
				return true
			}
			seen[name] = true
			if walk(next) {
				reached = true
				return false
			}
			return true
		})
		return reached
	}
	if fn.Name != nil {
		seen[fn.Name.Name] = true
	}
	return walk(fn), walked
}

// countPkgFiles — не-тестовых файлов в каталоге объявления журнала.
func countPkgFiles(files []string, journalPath string) int {
	dir := filepath.Dir(journalPath)
	n := 0
	for _, f := range files {
		if filepath.Dir(f) != dir {
			continue
		}
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		n++
	}
	return n
}

// ownerTableHeading — заголовок раздела, в котором живёт таблица владельцев.
const ownerTableHeading = "## \u0421\u043b\u043e\u0432\u0430\u0440\u044c \u0432\u043b\u0430\u0434\u0435\u043b\u044c\u0446\u0435\u0432"

// subscriptionOwnerRows читает строки таблицы владельцев клиентской страницы.
//
// Читается ОДИН РАЗДЕЛ, а не страница целиком, и это не осторожность: таблиц на
// странице несколько, и у соседней («кадры потока») первая ячейка тоже несёт
// `<code>`. Гейт, читающий все строки подряд, принял бы имя кадра за владельца и
// краснел бы на верном тексте.
func subscriptionOwnerRows(page string) []ownerRowReport {
	out := make([]ownerRowReport, 0, 8)
	start := strings.Index(page, ownerTableHeading)
	if start < 0 {
		return out
	}
	section := page[start+len(ownerTableHeading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	for _, row := range ownerRowRe.FindAllStringSubmatch(section, -1) {
		cells := ownerCellRe.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 2 {
			continue
		}
		name := ownerCodeRe.FindStringSubmatch(cells[0][1])
		if name == nil {
			continue
		}
		claim := cells[len(cells)-1][1]
		out = append(out, ownerRowReport{
			owner:       name[1],
			claim:       strings.Join(strings.Fields(claim), " "),
			claimsState: stateCarrierType.MatchString(claim),
		})
	}
	return out
}

// subscriptionOwnerOfService — написание владельца по каталогу сервиса.
//
// # ПОЧЕМУ ЭТО ОБЪЯВЛЕНО ЗДЕСЬ, А НЕ ВЫВЕДЕНО
//
// Величины две и они РАЗНЫЕ: каталог сервиса (`nlb`) и написание владельца
// (`loadbalancer`, имя домена в контракте). У большинства доменов они совпадают,
// у балансировщика — нет, и вывести одно из другого нельзя ничем: соответствие
// установлено решением, а не правилом.
//
// Повторённая величина расходится молча — поэтому она не просто объявлена, а
// проверена БИЕКЦИЕЙ (`subscriptionStateFindings`): каждый журнал дерева обязан
// иметь здесь написание, и каждое написание — журнал. Шестой владелец и
// переименование пятого краснеют сразу, а не переживают правку.
var subscriptionOwnerOfService = func() map[string]string {
	out := make(map[string]string, len(subscriptionJournalServices))
	for _, service := range subscriptionJournalServices {
		owner, ok := platformmodules.CatalogModuleOfService(service)
		if !ok {
			// Отсутствие записи не «подставляется по умолчанию»: умолчание
			// вернуло бы соглашение об именовании, ради снятия которого словарь
			// и заведён. Пустое написание краснит биекцию ниже с координатой.
			out[service] = ""
			continue
		}
		out[service] = owner
	}
	return out
}()

// subscriptionJournalServices — службы, несущие журнал изменений. Перечень
// объявлен ЗДЕСЬ, потому что «есть ли у службы журнал» словарь имён не знает и
// знать не должен: его предмет — написания, а не возможности модуля. Биекция
// ниже требует, чтобы перечень сходился с деревом в обе стороны.
var subscriptionJournalServices = []string{"compute", "nlb", "registry", "storage", "vpc"}

// subscriptionStateFindings — суждение: страница и журнал говорят одно.
func subscriptionStateFindings(reports []journalStateReport, rows []ownerRowReport) []string {
	out := make([]string, 0, 4)

	byOwner := make(map[string]ownerRowReport, len(rows))
	for _, r := range rows {
		byOwner[r.owner] = r
	}
	claimed := make(map[string]bool, len(reports))

	for _, rep := range reports {
		if rep.parseErr != nil {
			out = append(out, fmt.Sprintf(
				"объявление журнала services/%s не разбирается (%v) — гейт судит по узлам "+
					"дерева, и неосмотренный файл его молчания не оправдывает",
				rep.service, rep.parseErr))
			continue
		}
		if rep.stateFunc == "" {
			out = append(out, fmt.Sprintf(
				"объявление журнала services/%s не называет функции состояния — общая форма "+
					"требует её у каждого владельца, и её отсутствие означает, что осмотрено "+
					"не то место", rep.service))
			continue
		}
		// ПРЕДПОСЫЛКА РАСПОЗНАВАТЕЛЯ, проверяемая, а не подразумеваемая.
		//
		// Он обходит вызовы, объявленные в `journal.go`. Пока пакет журнала этим
		// файлом и исчерпывается, обход полон. Заведётся второй файл — помощник,
		// в котором стоит упаковка, окажется НЕ ОСМОТРЕН, и вердикт «не собирает»
		// станет ложным, оставаясь на вид исправным.
		//
		// Это ровно тот класс, что привёл сюда: распознаватель знал одну форму,
		// вторая молчала. Поэтому предпосылка истекает сама.
		if rep.pkgFiles > 1 {
			out = append(out, fmt.Sprintf(
				"пакет журнала services/%s несёт %d не-тестовых файла, а распознаватель "+
					"обходит вызовы только в journal.go: упаковка, вынесенная в соседний файл, "+
					"осталась бы НЕ ОСМОТРЕННОЙ, и вердикт «не собирает» был бы ложным. "+
					"Расширьте обход на файлы пакета и докажите инъекцией",
				rep.service, rep.pkgFiles))
			continue
		}
		owner := subscriptionOwnerOfService[rep.service]
		if owner == "" {
			out = append(out, fmt.Sprintf(
				"у журнала services/%s нет написания владельца в перечне гейта: страницу "+
					"о нём сверять не с чем, и его строка осталась бы вне наблюдения — "+
					"допишите соответствие каталог→владелец", rep.service))
			continue
		}
		claimed[owner] = true

		row, ok := byOwner[owner]
		if !ok {
			out = append(out, fmt.Sprintf(
				"владелец %q (services/%s) служит журнал, а таблица владельцев клиентской "+
					"страницы его не называет: клиент, читающий страницу, не узнает о нём вовсе",
				owner, rep.service))
			continue
		}
		if row.claimsState == rep.produces {
			continue
		}
		if rep.produces {
			out = append(out, fmt.Sprintf(
				"владелец %q: журнал services/%s/internal/subscriptionjournal/journal.go "+
					"СОБИРАЕТ состояние (%s доходит до `anypb.New`), а клиентская страница "+
					"обещает обратное — %q. Клиент, поверивший странице, напишет чтение "+
					"ресурса на каждое событие у владельца, который состояние отдаёт, и "+
					"откажется от клиентского отбора по меткам, который здесь исполним",
				owner, rep.service, rep.stateFunc, row.claim))
			continue
		}
		out = append(out, fmt.Sprintf(
			"владелец %q: журнал services/%s/internal/subscriptionjournal/journal.go "+
				"состояния НЕ собирает (%s не доходит до `anypb.New` ни одним путём), а "+
				"клиентская страница называет тип состояния — %q. Клиент, поверивший "+
				"странице, отберёт по меткам, которых не получал",
			owner, rep.service, rep.stateFunc, row.claim))
	}

	for _, row := range rows {
		if !claimed[row.owner] {
			out = append(out, fmt.Sprintf(
				"таблица владельцев называет %q, а журнала с таким написанием в дереве нет: "+
					"строка пережила свой предмет либо соответствие каталог→владелец отстало",
				row.owner))
		}
	}
	return out
}
