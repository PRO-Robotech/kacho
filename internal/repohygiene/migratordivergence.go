// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordivergence.go — ведомость различий между формами мигратора истекает
// сама: строка, которой больше нечего называть, — находка.
//
// # Предмет: РЕШЕНИЕ пережило свой предмет, и это заметил не гейт, а человек
//
// Соседний гейт [TestMigratorFormIsOneOfTwoAndBothAreDeclared] судит, не
// появилось ли ТРЕТЬЕЙ формы и ЧЕТВЁРТОЙ копии обёртки. Он о содержании
// решения не утверждает ничего — и не должен. Здесь судится другое: решение
// [migratorFormDecisionDoc] перечисляет различия между двумя формами и на этом
// перечне строит свой довод, а перечень — обыкновенная ВЕДОМОСТЬ, и стареет
// она молча.
//
// Так и вышло. Решение записано 2026-08-28 задачей #1383 и назвало шесть
// настоящих различий. Работа #1461 (общий разбор аргументов, `pkg/migratorcli`)
// сняла ТРИ из них — `--target` появился у всех семи, `--dsn` и
// `KACHO_MIGRATOR_DSN` тоже, флаг после подкоманды перестал теряться, — и ни
// одна проверка дерева этого не заметила. Документ продолжал предъявлять
// снятое как действующее и **отговаривать** от работы, которая уже стала
// возможной: два из трёх его доводов в пользу целевой формы опирались ровно на
// эти строки.
//
// # Что требуется
//
//  1. У различия, объявленного ЖИВЫМ, есть предмет — хотя бы один сервис, у
//     которого оно наблюдается в дереве. Пустой предмет — находка: строку
//     помечают снятой, а не держат «на всякий случай». Это тот же запрет, что и
//     у списка исключений (`testing.md` §Гейт на класс, п. 5): запись, которой
//     больше нечего исключать, унаследует следующая слепая зона.
//  2. У различия, объявленного СНЯТЫМ, предмета НЕТ. Ведомость, умеющая только
//     вычёркивать, — гейт, чей предмет есть отсутствие, а такой зеленеет легче
//     всех: предикат, вернувший «ни у кого», неотличим от достигнутой цели и от
//     сломанного разбора дерева. Обратный знак и есть положительная половина
//     пары, без которой отрицание вакуумно.
//  3. Каждая строка НАЗВАНА в решении своим ключом. Ведомость, живущая в коде,
//     и таблица, живущая в документе, — два места об одном предмете; ключ
//     связывает их так, что расхождение краснеет, а не молчит.
//
// # Почему предмет читается РАЗБОРОМ, а не подстрокой
//
// Имена `--target`, `--dsn`, `--config` стоят в этих же файлах В КОММЕНТАРИЯХ —
// в форме вызова в шапке, в разборе про порядок счёта перед сносом, в объяснении
// того, ПОЧЕМУ флаг больше не теряется. Гейт по тексту засчитал бы различие по
// его собственному объяснению и остался бы зелёным ровно тогда, когда предмет
// исчез. Разбор берёт литералы аргументов вызова и обращения к полям; шапки в
// AST не попадают вовсе (файл разбирается без комментариев).
//
// # Чего гейт НЕ утверждает, названо честно
//
// Что перечень различий ПОЛОН: строка, дописанная в таблицу решения прозой и не
// заведённая здесь ключом, гейту не видна — он ходит от ведомости к дереву, а не
// от документа к ведомости. Обратное направление потребовало бы разбирать
// markdown-таблицу, то есть судить о форме документа, а не о дереве. Предел
// назван числами в переписи: «живых различий N · снятых K» — сверяй их с
// таблицей решения глазами, когда таблицу правишь.
//
// Что различие ЗАКОННО: живой предмет означает «оно есть», а не «так и надо».
// Законность — вопрос решения, и он решается в документе.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// migratorEntryFacts — то, что видно в точке наката. Поля отвечают на вопросы
// ведомости, а не описывают файл целиком.
type migratorEntryFacts struct {
	// HandlesTarget — точка наката знает `--target` (регистрирует флаг либо
	// читает разобранное поле).
	HandlesTarget bool
	// DeclaresConfigFlag — точка наката объявляет `--config`.
	DeclaresConfigFlag bool
	// ParsesWithStdlibFlag — аргументы разбираются стандартным `flag` прямо
	// здесь. Именно этот разбор терял флаг, написанный после подкоманды.
	ParsesWithStdlibFlag bool
	// ResolvesDSN — точка наката принимает адрес базы явно (`--dsn` либо
	// общий резолвер, который читает и переменную окружения).
	ResolvesDSN bool
}

// migratorWrapperFacts — то, что видно в пакете-обёртке сервиса.
type migratorWrapperFacts struct {
	Present bool
	// DialectIsInterface — `Dialect` объявлен интерфейсом, то есть на его место
	// можно подставить дублёра.
	DialectIsInterface bool
	// DialectAcceptsEmptyName — фабрика диалекта принимает пустое имя.
	DialectAcceptsEmptyName bool
}

// migratorServiceFacts — один сервис целиком.
type migratorServiceFacts struct {
	Service string
	Entry   migratorEntryFacts
	Wrapper migratorWrapperFacts
}

// migratorDivergence — строка ведомости: различие между формами вместе с
// предикатом, отвечающим «у кого оно наблюдается».
//
// Ведомость ДВУСТОРОННЯЯ, и это не симметрия ради красоты. Строка, объявленная
// живой, обязана иметь предмет — иначе перечень предъявляет снятое как
// действующее. Строка, объявленная снятой, обязана предмета НЕ иметь — иначе
// снятие тихо откатывается. Односторонняя ведомость зеленела бы сильнее всего
// на сломанном разборе дерева: предикат, вернувший «ни у кого», выглядел бы
// достигнутой целью.
type migratorDivergence struct {
	// ID — ключ, которым строка названа в решении. Связывает документ с кодом.
	ID string
	// What — что именно расходится; идёт в текст находки.
	What string
	// Closed — чем и когда различие снято. Пусто означает «различие живо».
	// Непустое переворачивает требование к предмету на противоположное.
	Closed string
	// Subject возвращает сервисы, у которых различие наблюдается.
	Subject func(facts []migratorServiceFacts) []string
}

// migratorDeclaredDivergences — ведомость. Порядок и формулировки совпадают с
// таблицей решения; каждая строка обязана иметь живой предмет.
var migratorDeclaredDivergences = []migratorDivergence{
	{
		ID:   "dialect-empty-accepted",
		What: "пустая строка диалекта принимается фабрикой",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return f.Wrapper.Present && f.Wrapper.DialectAcceptsEmptyName
			})
		},
	},
	{
		ID:   "dialect-not-an-interface",
		What: "Dialect — конкретный тип, а не интерфейс: подставить дублёра нельзя",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return f.Wrapper.Present && !f.Wrapper.DialectIsInterface
			})
		},
	},
	{
		ID:   "config-flag-only-here",
		What: "точка наката берёт `--config` — флаг, которого нет у остальных",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return f.Entry.DeclaresConfigFlag
			})
		},
	},
	{
		ID:     "target-missing",
		What:   "`--target` отсутствует: накатить или откатить до версии нельзя",
		Closed: "#1461 (общий разбор аргументов), 2026-08-29",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return !f.Entry.HandlesTarget
			})
		},
	},
	{
		ID:     "dsn-missing",
		What:   "нет ни `--dsn`, ни переменной окружения: адрес базы задать нечем",
		Closed: "#1461 (общий разбор аргументов), 2026-08-29",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return !f.Entry.ResolvesDSN
			})
		},
	},
	{
		ID:     "flag-after-subcommand-lost",
		What:   "флаг после подкоманды теряется молча (разбор стандартным `flag` на месте)",
		Closed: "#1461 (общий разбор аргументов), 2026-08-29",
		Subject: func(facts []migratorServiceFacts) []string {
			return migratorServicesWhere(facts, func(f migratorServiceFacts) bool {
				return f.Entry.ParsesWithStdlibFlag
			})
		},
	},
}

// migratorServicesWhere — сервисы, удовлетворяющие предикату, в стабильном
// порядке: находка обязана быть воспроизводимой дословно.
func migratorServicesWhere(facts []migratorServiceFacts, pred func(migratorServiceFacts) bool) []string {
	var out []string
	for _, f := range facts {
		if pred(f) {
			out = append(out, f.Service)
		}
	}
	sort.Strings(out)
	return out
}

// migratorDivergenceCensus — объём осмотренного и состояние ведомости.
type migratorDivergenceCensus struct {
	Services int
	Wrappers int
	// Live — строк, объявленных живыми; LiveWithSubject — из них имеющих предмет.
	Live, LiveWithSubject int
	// Closed — строк, объявленных снятыми; ClosedStillGone — из них без предмета.
	Closed, ClosedStillGone int
}

func (c migratorDivergenceCensus) String() string {
	return fmt.Sprintf(
		"сервисов с точкой наката %d · пакетов-обёрток %d · живых различий %d (с предметом %d) · снятых %d (не вернулись %d)",
		c.Services, c.Wrappers, c.Live, c.LiveWithSubject, c.Closed, c.ClosedStillGone)
}

// parseMigratorEntryFacts читает точку наката РАЗБОРОМ. Комментарии в AST не
// попадают (режим без ParseComments), поэтому форма вызова в шапке и разбор в
// прозе предметом не считаются — а они там есть у всех семи.
func parseMigratorEntryFacts(rel, src string) (migratorEntryFacts, error) {
	var f migratorEntryFacts
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return f, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}

	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == "flag" {
			f.ParsesWithStdlibFlag = true
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			// Разобранное поле общего разбора: `opts.Target`, `opts.DSN`,
			// `migratorcli.ResolveDSN`.
			switch node.Sel.Name {
			case "Target", "ParseTargetVersion":
				f.HandlesTarget = true
			case "DSN", "ResolveDSN", "EnvDSN":
				f.ResolvesDSN = true
			}
		case *ast.CallExpr:
			// Имя флага — строковый литерал в аргументах регистрации.
			for _, arg := range node.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				switch strings.Trim(lit.Value, `"`) {
				case "target":
					f.HandlesTarget = true
				case "config":
					f.DeclaresConfigFlag = true
				case "dsn":
					f.ResolvesDSN = true
				}
			}
		}
		return true
	})
	return f, nil
}

// parseMigratorWrapperFacts накапливает факты по файлам пакета-обёртки.
// Принимает уже собранное значение, чтобы вызывающий подавал файлы по одному.
func parseMigratorWrapperFacts(acc migratorWrapperFacts, rel, src string) (migratorWrapperFacts, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return acc, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	acc.Present = true

	ast.Inspect(file, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name != nil && ts.Name.Name == "Dialect" {
			if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
				acc.DialectIsInterface = true
			}
		}
		return true
	})

	// Пустое имя ищется ТОЛЬКО внутри фабрики диалекта: пустой литерал сам по
	// себе встречается где угодно (умолчание флага, нулевое значение), и без
	// сужения предикат считал бы предметом любую строку.
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil || !strings.Contains(fn.Name.Name, "Dialect") {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.Trim(lit.Value, `"`) == "" {
				acc.DialectAcceptsEmptyName = true
			}
			return true
		})
	}
	return acc, nil
}

// migratorDivergenceFindings — по строке на каждое нарушение ведомости.
//
// Ведомость принимается ПАРАМЕТРОМ, а не читается из пакета: инъекция обязана
// подавать свою (в том числе пустую — на ней гейт обязан молчать, потому что
// пустая ведомость есть достигнутая цель, а не поломка).
func migratorDivergenceFindings(ledger []migratorDivergence, facts []migratorServiceFacts, doc string) []string {
	var out []string
	for _, d := range ledger {
		subj := d.Subject(facts)
		switch {
		case d.Closed == "" && len(subj) == 0:
			out = append(out, fmt.Sprintf(
				"различие %q (%s) ПОТЕРЯЛО ПРЕДМЕТ: в дереве нет ни одного сервиса, у которого оно есть. "+
					"Пометьте строку снятой в ведомости и в таблице %s — перечень, которому нечего называть, "+
					"следующий читатель примет за действующий и не станет делать работу, которая уже сделана",
				d.ID, d.What, migratorFormDecisionDoc))
		case d.Closed != "" && len(subj) > 0:
			out = append(out, fmt.Sprintf(
				"различие %q (%s) ВЕРНУЛОСЬ: объявлено снятым (%s), а наблюдается у %v. "+
					"Либо это регресс и его чинят, либо снятие было объявлено преждевременно и %s лжёт",
				d.ID, d.What, d.Closed, subj, migratorFormDecisionDoc))
		}
		if !strings.Contains(doc, d.ID) {
			out = append(out, fmt.Sprintf(
				"различие %q объявлено в ведомости, но не названо ключом в %s: "+
					"два места об одном предмете расходятся молча",
				d.ID, migratorFormDecisionDoc))
		}
	}
	sort.Strings(out)
	return out
}
