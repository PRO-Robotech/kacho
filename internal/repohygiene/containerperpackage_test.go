// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// containerperpackage_test.go — контейнер поднимается ОДИН НА ПАКЕТ, а не на каждый
// тест.
//
// # Предмет
//
// Тест, которому нужна настоящая база, поднимал настоящий контейнер и заново
// проигрывал всю цепочку миграций — на КАЖДЫЙ вызов. Дорого не то, что проверяют
// тесты, а оснастка: пакет, зовущий такой хелпер 91 раз, платит 91 старт контейнера
// и 91 проигрыш цепочки до первого утверждения.
//
// Цена не линейна, и это главное. `go test` применяет СВОЙ предел в 600 с на пакет,
// и предел этот вшит в инструмент: пакет, переваливший за него, буквальная команда
// `go test ./...` не заканчивает ВООБЩЕ — не медленно, а никак, на сколь угодно
// свободной машине. Причём wall-clock пакета зависит не от него одного: `go test
// ./...` гоняет пакеты параллельно, и на этой машине тот же `pkg/migrations/common`
// занимал 71.7 с в одиночку и 338.9 с при шестикратной параллели (замер 2026-08-02,
// b49f5794, Docker 29.3.1, 12 CPU). Под параллелью старт контейнера начинает и
// просто ОТКАЗЫВАТЬ — `context deadline exceeded` на 60 с, то есть пакет отдаёт
// красное там, где ничего не сломано.
//
// # Что именно запрещено — и почему порог равен двум
//
// Запрещено не «поднимать контейнер». Запрещено поднимать его ПО РАЗУ НА ТЕСТ.
// Гейт считает, сколько РАЗНЫХ тестовых функций пакета транзитивно доходят до старта
// контейнера:
//
//   - ноль — предмета нет;
//   - один — контейнер поднимется один раз за прогон пакета, что и требуется;
//     запрещать это значило бы требовать TestMain там, где он ничего не меняет;
//   - два и больше — старт лежит на пути, по которому проходит каждый из них, то
//     есть контейнер поднимается на тест. Находка.
//
// Порог обоснован предметом, а не сегодняшним деревом, и проверен с обеих сторон
// границы — ровно один тест молчит, ровно два краснеют (см. инъекции ниже).
//
// # Санкционированные поставщики
//
// Их два, и оба устроены одинаково: контейнер стартует под `sync.Once`, а тест
// получает собственную область внутри него — `pkg/pgtest` выдаёт СВОЮ БАЗУ,
// склонированную из мигрированного шаблона, `…/testsupport/fgatest` — СВОЙ STORE на
// общем сервере OpenFGA. Поэтому вызовы к ним за старт не считаются.
//
// Это допущение — предпосылка гейта, и проверяется оно НЕ чтением кода, а поведением,
// в самих пакетах: обе половины сразу — «сервер один» И «области разные, данные между
// ними не ходят». Разойдётся — покраснеет там, а не промолчит здесь. Где именно лежит
// каждое доказательство, записано значением в sanctionedProviders, и запись без
// предмета — находка (TestSanctionedProvidersStillExist).
//
// # Чего гейт НЕ видит, названо, а не спрятано
//
//   - вызовы через значение (`h.boot()`, поле структуры, переменная-функция) не
//     резолвятся без типов: гейт разбирает вызовы, квалифицированные ИМЕНЕМ ПАКЕТА,
//     и вызовы по имени внутри своего пакета. Обёртка, спрятанная за интерфейсом,
//     ему не видна;
//   - методы (функции с получателем) в граф не входят по той же причине.
//
// Это ограничение радиуса, а не умолчание: закрывать его пришлось бы разрешением
// типов, чего этот обход не делает.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// файлов разобрал, сколько пакетов с тестами увидел, сколько функций классифицировал
// стартующими и сколько тестов до них доходит. Пустой обход — провал, а не чистота.
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

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// modulePath переехал в operationtimestamptruncation.go — НЕтестовый файл того
// же пакета: константу понадобилось читать второму гейту, а объявленная в
// _test.go она видна только тестам, поэтому второй объявил бы её заново. Одно
// место на один предмет.

// sanctionedProviders — пакеты, чей старт контейнера происходит ОДИН раз на процесс,
// а тесту выдаётся собственная область внутри него.
//
// Запись здесь — не поблажка, а ссылка на ДОКАЗАТЕЛЬСТВО: у каждого поставщика в его
// собственном пакете стоит поведенческий тест, утверждающий обе половины сразу —
// «сервер один» И «области разные, данные не пересекаются». Разойдётся — покраснеет
// там, а не промолчит здесь. Значение map — где искать это доказательство.
//
// Множество, а не одна строка: поставщиков два (Postgres и OpenFGA), и вторым его
// сделала не симметрия, а замер — старт OpenFGA на тест жил в пяти пакетах iam, в
// одном из них 25 раз за прогон.
//
// «ПОКРАСНЕЕТ ТАМ» ВЕРНО ПОКА НЕ ДЛЯ ОБОИХ, и разница названа, а не сглажена.
// Доказательство OpenFGA-половины конвейер ИСПОЛНЯЕТ с 2026-08-06: пакет входит в
// перечень цели test-authz-fga (см. authzfgaproofs_test.go). Доказательство
// Postgres-половины не исполняется ни одной джобой — под кратким режимом оно
// пропускается, а отбор интеграционной джобы до `pkg/pgtest` не достаёт, — и
// пакет остаётся названным долгом в shortgatedselection_test.go. То есть санкция
// здесь стоит на проверяемом утверждении для одного поставщика и на непроверяемом
// для другого; закрывается это тем же способом, что и для первого.
var sanctionedProviders = map[string]string{
	"pkg/pgtest": "pkg/pgtest/pgtest_test.go: TestOneContainerManyDatabases + " +
		"TestRowsDoNotCrossBetweenDatabases",
	// Инструмент сравнительного замера форм модели прав: тот же приём, что у двух
	// соседей выше — стек под `sync.Once`, СВОЙ store на кейс, — и та же половинчатая
	// проверка: «сервер один» И «области разные», обе в одном тесте. Пакет не выдаёт
	// область чужим, он единственный свой потребитель; в списке он потому, что предмет
	// санкции — устройство старта, а не число потребителей.
	"services/iam/tools/authzformbench": "services/iam/tools/authzformbench/isolation_test.go: TestOneStackManyStores",
}

// containerStartAPIs — вызовы, стартующие контейнер, по ПУТИ ИМПОРТА и имени.
//
// Ключуемся на путь, а не на локальное имя: в дереве один и тот же пакет
// импортируется и как `postgres`, и как `tcpostgres`, а хелпер iam зовут и `pg.`, и
// `kanamepg.` — предикат по имени пропустил бы восемь мест в двух пакетах (замерено
// до перехода на разбор импортов).
//
// pkgName обязателен и хранится ОТДЕЛЬНО от пути, потому что имя пакета не выводится
// из пути: `github.com/testcontainers/testcontainers-go` объявляет `package
// testcontainers`, и последний сегмент пути (`testcontainers-go`) идентификатором в
// коде не является НИКОГДА. Пока имя бралось из пути, каждый вызов
// `testcontainers.GenericContainer` без явного алиаса был гейту НЕВИДИМ — то есть
// весь старт OpenFGA в fgatest, а с ним три пакета iam. Гейт при этом оставался
// зелёным: молчание по слепоте неотличимо от молчания по чистоте, поэтому ниже стоит
// положительная проба на живом дереве (TestScannerClassifiesKnownStartersInTheTree).
var containerStartAPIs = map[string]containerAPI{
	"github.com/testcontainers/testcontainers-go/modules/postgres": {
		pkgName: "postgres", funcs: []string{"Run"},
	},
	"github.com/testcontainers/testcontainers-go": {
		pkgName: "testcontainers", funcs: []string{"GenericContainer", "Run"},
	},
}

type containerAPI struct {
	// pkgName — идентификатор, под которым пакет виден в коде при импорте без алиаса.
	pkgName string
	// funcs — функции этого пакета, поднимающие контейнер.
	funcs []string
}

// importIdent — под каким именем импорт виден в коде файла.
func importIdent(explicitAlias, importPath string) string {
	if explicitAlias != "" {
		return explicitAlias
	}
	if api, ok := containerStartAPIs[importPath]; ok {
		return api.pkgName
	}
	// Для остальных импортов совпадение имени пакета с последним сегментом пути —
	// соглашение, а не правило; здесь оно достаточно, потому что от этих имён зависит
	// только резолв ВНУТРИМОДУЛЬНЫХ вызовов, где оно соблюдено.
	return path.Base(importPath)
}

// perTestContainerExceptions — пакеты, которым СЕЙЧАС разрешено поднимать контейнер
// на тест, с предметом у каждой записи.
//
// Это не whitelist «так и надо», а перепись долга: запись, которой нечего исключать,
// — находка (гейт ниже это утверждает), иначе список переживает свой повод и
// достаётся следующему как слепая зона.
var perTestContainerExceptions = map[string]string{}

// funcKey — функция в графе: каталог пакета + имя пакета + имя функции. Имя пакета
// входит в ключ, потому что в одном каталоге живут `foo` и `foo_test`, и вызов по
// голому имени резолвится внутри своего.
type funcKey struct {
	dir, pkg, name string
}

// pkgKey — пакет: каталог + имя.
type pkgKey struct{ dir, pkg string }

// scan — то, что обход извлёк из дерева. Отделено от вердикта: судит judge, ниже.
type scan struct {
	filesParsed int
	testPkgs    int
	// starters — функции, транзитивно стартующие контейнер.
	starters map[funcKey]bool
	// testsReaching[pkgdir] — имена тестовых функций, доходящих до старта.
	testsReaching map[string]map[string]bool
	// pkgsWithTests — каталоги пакетов, где есть хоть одна тестовая функция.
	pkgsWithTests map[string]bool
}

// TestNoPackageStartsAContainerPerTest — сам гейт против дерева.
func TestNoPackageStartsAContainerPerTest(t *testing.T) {
	root := repoRoot(t)
	sc := scanTree(t, root)

	t.Logf("перепись: разобрано файлов %d; пакетов с тестами %d; функций, стартующих контейнер, %d; "+
		"пакетов, где до старта доходит хоть один тест, %d",
		sc.filesParsed, sc.testPkgs, len(sc.starters), len(sc.testsReaching))

	if sc.filesParsed == 0 || sc.testPkgs == 0 {
		t.Fatalf("обход пуст (файлов %d, пакетов с тестами %d) — гейт ничего не прочитал, "+
			"а значит ничего и не доказал", sc.filesParsed, sc.testPkgs)
	}
	// Предмет обязан существовать: если в дереве не осталось НИ ОДНОГО старта
	// контейнера, то либо testcontainers больше не используется (и гейту нечего
	// держать), либо containerStartAPIs описывает API, которого нет. Оба случая
	// делают молчание гейта неотличимым от поломки — §«гейт, чей предмет отсутствие».
	if len(sc.starters) == 0 {
		t.Fatalf("в дереве не найдено ни одной функции, стартующей контейнер — "+
			"либо containerStartAPIs (%v) описывает API, которого больше нет, либо "+
			"тесты перестали поднимать контейнеры вовсе; молчание гейта в обоих случаях "+
			"неотличимо от его поломки", containerStartAPIs)
	}

	counts := map[string]int{}
	for dir, tests := range sc.testsReaching {
		// Санкция действует и на СОБСТВЕННЫЙ старт пакета, а не только на вызовы к
		// нему извне.
		//
		// Предмет санкции — устройство старта («один на процесс, области разные»), а
		// не то, из какого пакета он виден. Прежняя редакция снимала её лишь при
		// разборе вызовов, квалифицированных именем провайдера, поэтому пакет,
		// поднимающий свой стек тем же приёмом и доказавший обе половины, всё равно
		// объявлялся нарушителем — и единственным исходом для него был перечень
		// «нужен контейнер на тест», то есть запись с ЗАВЕДОМО НЕВЕРНОЙ причиной.
		// Исключение, лгущее о своей причине, хуже отсутствующего: следующий читатель
		// чинит по нему не то.
		if _, sanctioned := sanctionedProviders[dir]; sanctioned {
			continue
		}
		counts[dir] = len(tests)
	}
	for _, f := range judgePerTestContainers(counts, perTestContainerExceptions) {
		t.Errorf("%s", f)
	}
}

// judgePerTestContainers — вердикт, отделённый от измерения.
//
// reaching — сколько РАЗНЫХ тестовых функций пакета доходят до старта контейнера;
// declared — перепись разрешённых, с причиной.
func judgePerTestContainers(reaching map[string]int, declared map[string]string) []string {
	left := map[string]bool{}
	for p := range declared {
		left[p] = true
	}
	var findings []string

	dirs := make([]string, 0, len(reaching))
	for d := range reaching {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs) // порядок находок не зависит от обхода карты

	for _, dir := range dirs {
		n := reaching[dir]
		if n < 2 {
			// Ноль или один тест — контейнер поднимется максимум раз за прогон
			// пакета. Требовать здесь TestMain значило бы требовать обряд без предмета.
			if declared[dir] != "" {
				findings = append(findings, "perTestContainerExceptions называет "+dir+
					", но до старта контейнера доходит "+strconv.Itoa(n)+" тест(ов) — "+
					"поднять его на тест этот пакет не может, и запись описывает долг, которого нет")
			}
			delete(left, dir)
			continue
		}
		if !left[dir] {
			findings = append(findings, "пакет "+dir+": до старта контейнера доходит "+
				strconv.Itoa(n)+" разных тестов, то есть контейнер поднимается НА ТЕСТ. "+
				"`go test` даёт пакету 600 с по умолчанию, и такой пакет упирается в них тем "+
				"вернее, чем параллельнее прогон. Переведи пакет на один контейнер: TestMain → "+
				"pgtest.Run(m, pgtest.Config{…}), а хелпер — на pgtest.NewDB(t) / NewEmptyDB(t) "+
				"(тела тестов при этом не трогаются; см. pkg/pgtest). Если пакету нужен "+
				"именно свой контейнер на тест — назови его в perTestContainerExceptions "+
				"с причиной, долгом с именем, а не умолчанием")
		}
		delete(left, dir)
	}

	rest := make([]string, 0, len(left))
	for p := range left {
		rest = append(rest, p)
	}
	sort.Strings(rest)
	for _, p := range rest {
		findings = append(findings, "perTestContainerExceptions называет "+p+", но такого пакета "+
			"с тестами, доходящими до старта контейнера, в дереве нет — записи, которой нечего "+
			"исключать, в списке не место: она достанется следующему как слепая зона")
	}
	return findings
}

// scanTree разбирает дерево ПО ИНДЕКСУ git (не по диску: посторонний каталог рядом
// с репозиторием не должен влиять на вердикт) и строит граф вызовов.
func scanTree(t *testing.T, root string) scan {
	t.Helper()

	out, err := gitenv.Command(root, "ls-files", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Fields(string(out)) {
		if skipPath(f) {
			continue
		}
		files = append(files, f)
	}

	sc := scan{
		starters:      map[funcKey]bool{},
		testsReaching: map[string]map[string]bool{},
		pkgsWithTests: map[string]bool{},
	}

	// calls[fn] — куда fn зовёт: по (dir,pkg,name) внутри своего пакета или по пути
	// импорта для чужого. Чужие резолвим вторым проходом, когда знаем все пакеты.
	type target struct {
		importPath string // пусто → свой пакет
		name       string
	}
	calls := map[funcKey][]target{}
	// dirPkgs[dir] — непустые имена пакетов в каталоге (для резолва импорта).
	dirPkgs := map[string][]string{}
	testFns := map[pkgKey][]string{}

	fset := token.NewFileSet()
	for _, rel := range files {
		f, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			// Файл, который не разбирается, — не «чисто»: это непрочитанное.
			t.Fatalf("parse %s: %v", rel, err)
		}
		sc.filesParsed++
		dir := path.Dir(rel)
		pkg := f.Name.Name
		if !contains(dirPkgs[dir], pkg) {
			dirPkgs[dir] = append(dirPkgs[dir], pkg)
		}

		// alias → import path
		alias := map[string]string{}
		for _, im := range f.Imports {
			p, perr := strconv.Unquote(im.Path.Value)
			if perr != nil {
				continue
			}
			explicit := ""
			if im.Name != nil {
				explicit = im.Name.Name
			}
			alias[importIdent(explicit, p)] = p
		}

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv != nil { // методы — вне графа (см. шапку)
				continue
			}
			self := funcKey{dir: dir, pkg: pkg, name: fd.Name.Name}
			if strings.HasSuffix(rel, "_test.go") && isTestFunc(fd.Name.Name) {
				testFns[pkgKey{dir, pkg}] = append(testFns[pkgKey{dir, pkg}], fd.Name.Name)
				sc.pkgsWithTests[dir] = true
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := ce.Fun.(type) {
				case *ast.Ident: // вызов по имени внутри своего пакета
					calls[self] = append(calls[self], target{name: fun.Name})
				case *ast.SelectorExpr:
					id, ok := fun.X.(*ast.Ident)
					if !ok {
						return true
					}
					p, ok := alias[id.Name]
					if !ok {
						return true
					}
					// Санкционированный поставщик за старт не считается.
					if _, sanctioned := sanctionedProviders[strings.TrimPrefix(p, modulePath)]; sanctioned {
						return true
					}
					if api, ok := containerStartAPIs[p]; ok && contains(api.funcs, fun.Sel.Name) {
						sc.starters[self] = true
					}
					calls[self] = append(calls[self], target{importPath: p, name: fun.Sel.Name})
				}
				return true
			})
		}
	}
	sc.testPkgs = len(sc.pkgsWithTests)

	// resolve — превратить цель вызова в ключ функции, если она наша.
	resolve := func(from funcKey, tg target) (funcKey, bool) {
		if tg.importPath == "" {
			return funcKey{from.dir, from.pkg, tg.name}, true
		}
		if !strings.HasPrefix(tg.importPath, modulePath) {
			return funcKey{}, false
		}
		dir := strings.TrimPrefix(tg.importPath, modulePath)
		for _, p := range dirPkgs[dir] {
			if strings.HasSuffix(p, "_test") { // внешний тестовый пакет не импортируется
				continue
			}
			return funcKey{dir, p, tg.name}, true
		}
		return funcKey{}, false
	}

	// Неподвижная точка: зовущий стартующего сам стартует.
	for changed := true; changed; {
		changed = false
		for fn, tgs := range calls {
			if sc.starters[fn] {
				continue
			}
			for _, tg := range tgs {
				if k, ok := resolve(fn, tg); ok && sc.starters[k] {
					sc.starters[fn] = true
					changed = true
					break
				}
			}
		}
	}

	// Сколько РАЗНЫХ тестов доходит до старта.
	for pk, names := range testFns {
		for _, name := range names {
			fn := funcKey{pk.dir, pk.pkg, name}
			if !sc.starters[fn] {
				continue
			}
			if sc.testsReaching[pk.dir] == nil {
				sc.testsReaching[pk.dir] = map[string]bool{}
			}
			sc.testsReaching[pk.dir][name] = true
		}
	}
	return sc
}

// isTestFunc — тестовая функция, кроме TestMain: TestMain стартует контейнер РАЗ на
// пакет, ради чего гейт и существует, поэтому он не предмет запрета.
func isTestFunc(name string) bool {
	if name == "TestMain" {
		return false
	}
	for _, p := range []string{"Test", "Fuzz", "Benchmark"} {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			r := name[len(p)]
			if r >= 'A' && r <= 'Z' || r == '_' {
				return true
			}
		}
	}
	return false
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// TestPerTestContainerJudgeFiresAndStaysSilent — инъекция в обе стороны, включая обе
// стороны ПОРОГА.
//
// Гейт выше на дереве зелёный, и зелёный сам по себе ничего не доказывает: ровно так
// же он выглядел бы, если бы вердикт не умел краснеть.
func TestPerTestContainerJudgeFiresAndStaysSilent(t *testing.T) {
	const pkg = "services/example/internal/repo"

	t.Run("краснеет: два теста доходят до старта", func(t *testing.T) {
		f := judgePerTestContainers(map[string]int{pkg: 2}, nil)
		if len(f) != 1 || !strings.Contains(f[0], pkg) {
			t.Fatalf("гейт не назвал пакет, поднимающий контейнер на тест: %v", f)
		}
	})

	t.Run("молчит: ровно один тест — контейнер поднимется один раз", func(t *testing.T) {
		if f := judgePerTestContainers(map[string]int{pkg: 1}, nil); len(f) != 0 {
			t.Fatalf("гейт краснеет там, где контейнер и так один: %v", f)
		}
	})

	t.Run("молчит: ноль тестов доходит до старта", func(t *testing.T) {
		if f := judgePerTestContainers(map[string]int{pkg: 0}, nil); len(f) != 0 {
			t.Fatalf("гейт краснеет на пакете без стартов: %v", f)
		}
	})

	t.Run("молчит: пакет назван в переписи с причиной", func(t *testing.T) {
		f := judgePerTestContainers(map[string]int{pkg: 9}, map[string]string{pkg: "причина"})
		if len(f) != 0 {
			t.Fatalf("гейт краснеет на объявленном долге: %v", f)
		}
	})

	t.Run("краснеет: запись переписи без предмета", func(t *testing.T) {
		f := judgePerTestContainers(nil, map[string]string{"pkg/gone": "причина"})
		if len(f) != 1 || !strings.Contains(f[0], "pkg/gone") {
			t.Fatalf("исключение без предмета не найдено: %v", f)
		}
	})

	t.Run("краснеет: перепись называет пакет, который и так не может нарушать", func(t *testing.T) {
		f := judgePerTestContainers(map[string]int{pkg: 1}, map[string]string{pkg: "причина"})
		if len(f) != 1 || !strings.Contains(f[0], pkg) {
			t.Fatalf("перепись приняла пакет с одним тестом: %v", f)
		}
	})
}

// TestPerTestContainerScannerFindsAPlantedViolatorAndSpacesTheLegitimateTwin —
// инъекция НАСТОЯЩИМ входом: два синтетических пакета разбираются тем же обходом,
// что и дерево.
//
// Без второй половины гейт ловил бы форму (упоминание testcontainers), а не существо
// (сколько тестов до неё доходит), и первый же законный TestMain его бы отключил.
func TestPerTestContainerScannerFindsAPlantedViolatorAndSpacesTheLegitimateTwin(t *testing.T) {
	// Нарушитель: два теста зовут хелпер, который стартует контейнер.
	violator := `package repo_test
import (
	"testing"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)
func boot(t *testing.T) string { _, _ = tcpostgres.Run(nil, "postgres:16-alpine"); return "" }
func TestOne(t *testing.T) { _ = boot(t) }
func TestTwo(t *testing.T) { _ = boot(t) }
`
	// Законный близнец: та же форма, тот же импорт, но старт достижим ТОЛЬКО из
	// TestMain, а тесты берут базу у санкционированного поставщика.
	legit := `package repo_test
import (
	"os"
	"testing"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)
func bootOnce() { _, _ = tcpostgres.Run(nil, "postgres:16-alpine") }
func TestMain(m *testing.M) { bootOnce(); os.Exit(m.Run()) }
func TestOne(t *testing.T) { _ = pgtest.NewDB(t) }
func TestTwo(t *testing.T) { _ = pgtest.NewDB(t) }
`
	if got := reachingTestsIn(t, violator); got != 2 {
		t.Fatalf("обход не увидел нарушителя: до старта доходит %d тестов, ожидалось 2", got)
	}
	if got := reachingTestsIn(t, legit); got != 0 {
		t.Fatalf("обход посчитал законный общий контейнер нарушением: %d теста(ов) доходят до старта", got)
	}
}

// reachingTestsIn прогоняет ту же классификацию, что и scanTree, по одному исходнику.
// Это НЕ копия логики: обе стороны считают одинаково, поэтому расхождение здесь
// означало бы расхождение и на дереве.
func reachingTestsIn(t *testing.T, src string) int {
	t.Helper()
	dir := t.TempDir()
	// Каталог обхода — пакет `x` внутри временного дерева; git тут не нужен, входом
	// служит переданный исходник.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, "x_test.go"), src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	alias := map[string]string{}
	for _, im := range f.Imports {
		p, perr := strconv.Unquote(im.Path.Value)
		if perr != nil {
			continue
		}
		explicit := ""
		if im.Name != nil {
			explicit = im.Name.Name
		}
		alias[importIdent(explicit, p)] = p
	}

	starters := map[string]bool{}
	calls := map[string][]string{}
	var tests []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Recv != nil {
			continue
		}
		name := fd.Name.Name
		if isTestFunc(name) {
			tests = append(tests, name)
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := ce.Fun.(type) {
			case *ast.Ident:
				calls[name] = append(calls[name], fun.Name)
			case *ast.SelectorExpr:
				id, ok := fun.X.(*ast.Ident)
				if !ok {
					return true
				}
				p, ok := alias[id.Name]
				if !ok {
					return true
				}
				if _, sanctioned := sanctionedProviders[strings.TrimPrefix(p, modulePath)]; sanctioned {
					return true
				}
				if api, ok := containerStartAPIs[p]; ok && contains(api.funcs, fun.Sel.Name) {
					starters[name] = true
				}
			}
			return true
		})
	}
	for changed := true; changed; {
		changed = false
		for fn, tgs := range calls {
			if starters[fn] {
				continue
			}
			for _, tg := range tgs {
				if starters[tg] {
					starters[fn] = true
					changed = true
					break
				}
			}
		}
	}
	n := 0
	for _, name := range tests {
		if starters[name] {
			n++
		}
	}
	return n
}

// TestSanctionedProvidersStillExist — послабление живёт, пока у него есть предмет.
//
// Санкция снимает с пакета весь запрет, поэтому она обязана истекать сама: исчезнет
// поставщик или уедет файл с его поведенческим доказательством — и санкция начнёт
// покрывать пустоту, оставаясь при этом невидимой. Предикат снятия внешний по
// отношению к самой записи: существование каталога и файла-держателя в индексе git.
func TestSanctionedProvidersStillExist(t *testing.T) {
	root := repoRoot(t)
	// --others --exclude-standard добавляет к индексу файлы, ЕЩЁ не добавленные, но
	// которые git добавит (не игнорируемые). Без них тест падал бы на каждом честном
	// новом поставщике до первого `git add` — то есть требовал бы коммитить, чтобы
	// пройти проверку, которая решает, можно ли коммитить.
	out, err := gitenv.Command(root, "ls-files", "--cached", "--others",
		"--exclude-standard").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	tracked := map[string]bool{}
	dirs := map[string]bool{}
	for _, f := range strings.Fields(string(out)) {
		tracked[f] = true
		dirs[path.Dir(f)] = true
	}
	if len(tracked) == 0 {
		t.Fatal("индекс git пуст — проверить существование поставщиков не по чему")
	}

	for provider, proof := range sanctionedProviders {
		if !dirs[provider] {
			t.Errorf("sanctionedProviders называет %s, но такого пакета в дереве нет — "+
				"санкция покрывает пустоту", provider)
		}
		file, _, ok := strings.Cut(proof, ":")
		if !ok {
			t.Errorf("у санкции %s не записано, ГДЕ лежит её поведенческое доказательство "+
				"(ожидается \"путь/файл_test.go: ИмяТеста\")", provider)
			continue
		}
		if !tracked[strings.TrimSpace(file)] {
			t.Errorf("санкция %s ссылается на доказательство в %s, а такого файла в индексе "+
				"git нет — санкция осталась без того, на чём стоит", provider, file)
		}
	}

	// Перепись: «все дозволенные поставщики на месте» держится и на пустом обходе,
	// поэтому объём осмотренного назван отдельным утверждением.
	t.Logf("перепись: отслеживаемых путей %d, каталогов-поставщиков %d",
		len(tracked), len(dirs))
}

// TestScannerClassifiesKnownStartersInTheTree — положительная проба на ЖИВОМ дереве.
//
// Инъекция подставным исходником выше доказывает, что обход умеет краснеть; она не
// доказывает, что он видит то, что реально написано в этом репозитории. Разница не
// умозрительная: пока имя импортированного пакета выводилось из последнего сегмента
// пути, `testcontainers.GenericContainer` в fgatest был обходу невидим — а подставная
// фикстура импортировала пакет С АЛИАСОМ и потому проходила. Сопутствующий элемент,
// который в живых данных лежит всегда (импорт БЕЗ алиаса, чьё имя пакета не равно
// сегменту пути), в пробе отсутствовал, и гейт молчал на настоящем предмете.
//
// Поэтому здесь названы функции самого дерева, которые обход ОБЯЗАН классифицировать
// стартующими. Исчезнут — перепись покраснеет и потребует обновления, а не тихо
// потеряет предмет.
func TestScannerClassifiesKnownStartersInTheTree(t *testing.T) {
	sc := scanTree(t, repoRoot(t))

	must := []funcKey{
		// OpenFGA: импорт `github.com/testcontainers/testcontainers-go` БЕЗ алиаса,
		// package testcontainers — тот самый случай, что был невидим.
	}
	// Про вторую половину каталога (modules/postgres) положительной пробы на дереве
	// НЕТ, и это названо, а не умолчано: после перехода на общий контейнер прямой
	// `postgres.Run` остаётся только внутри pkg/pgtest, где он лежит в МЕТОДЕ, а
	// методы в граф не входят (см. шапку). Эту половину держат подставная фикстура
	// выше и TestContainerStartAPIsStillExistInTheTree ниже.
	for _, k := range must {
		if !sc.starters[k] {
			t.Errorf("обход НЕ считает %s.%s стартующим контейнер, а он стартует — "+
				"значит гейт слеп к этой форме вызова, и его молчание на дереве "+
				"ничего не означает", k.dir, k.name)
		}
	}

	// Перепись: сколько мест обходчик классифицировал. Ноль найденных дал бы тот же
	// зелёный вердикт, что и верная классификация всех.
	t.Logf("перепись: обязательных к распознанию %d; обходчик разобрал файлов %d, тестовых пакетов %d, стартующих функций %d",
		len(must), sc.filesParsed, sc.testPkgs, len(sc.starters))
}

// TestContainerStartAPIsStillExistInTheTree — предпосылка гейта.
//
// Гейт ключуется на путях импорта testcontainers. Переедет пакет или переименуется
// конструктор — гейт ослепнет и останется ЗЕЛЁНЫМ, что неотличимо от чистого дерева.
// Поэтому каждый объявленный путь обязан встречаться в дереве хотя бы раз.
func TestContainerStartAPIsStillExistInTheTree(t *testing.T) {
	root := repoRoot(t)
	out, err := gitenv.Command(root, "grep", "-l", "testcontainers", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git grep: %v", err)
	}
	if len(strings.Fields(string(out))) == 0 {
		t.Fatal("в дереве нет ни одного файла с testcontainers — containerStartAPIs описывает " +
			"мир, которого нет, и гейт зелен по построению")
	}
	for p := range containerStartAPIs {
		o, gerr := gitenv.Command(root, "grep", "-l", p, "--", "*.go").Output()
		if gerr != nil || len(strings.Fields(string(o))) == 0 {
			t.Errorf("путь импорта %q не встречается в дереве: гейт ищет API, которого здесь "+
				"больше нет, и его молчание ничего не значит", p)
		}
	}
}
