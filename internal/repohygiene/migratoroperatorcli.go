// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratoroperatorcli.go — командная строка мигратора одна на семь сервисов.
//
// # Предмет: различие, которое видит ОПЕРАТОР (#1461)
//
// Соседний гейт [TestMigratorFormIsOneOfTwoAndBothAreDeclared] судит ВНУТРЕННЮЮ
// раскладку точки наката (делегирует она своей обёртке или зовёт goose сама) и
// прямо оговаривает, что о равенстве CLI не утверждает ничего. Здесь судится
// ровно то, что оговорено: поверхность, к которой оператор прикасается руками.
//
// Различий было четыре, и ни одно никем не решалось — они накопились от того,
// что сервисы заводились в разное время. Два из них тихие:
//
//   - флаг ПОСЛЕ подкоманды прямая четвёрка теряла молча (`flag.Parse`
//     останавливается на первом не-флаге), поэтому `kacho-migrator up --dsn X`
//     накатывал на базу из запасного пути и выглядел УСПЕХОМ;
//   - лишний позиционный аргумент cobra принимала молча (`Args == nil`),
//     поэтому `up 800001` — догадка о том, как задать цель — накатывал до
//     головы.
//
// Решение о поверхности записано в [migratorCLIDecisionDoc]; форма самой точки
// наката — в соседнем документе, и здесь не пересказывается.
//
// # Что требуется
//
//  1. Имя бинаря одно — [migratorCLIBinaryName]. Судятся все места, где имя
//     называется: путь установки, выход сборки, переменная Makefile, константа
//     в самой точке наката. Разные имена означают, что знание об одном сервисе
//     к соседнему не применяется.
//  2. Разбор аргументов — один из ДВУХ признанных: общий пакет
//     [migratorCLISharedParserImport] либо cobra. Третий разбор — находка: он и
//     есть тот способ, каким различие накапливалось.
//  3. У каждой команды cobra, несущей исполнение (Run/RunE), решено, что делать
//     с лишним позиционным аргументом (поле Args). Умолчание принимает
//     произвольные аргументы молча.
//  4. Корневая команда cobra (та, чьё `Use` называет бинарь) несёт исполнение.
//     Без него ПУСТАЯ командная строка печатает помощь и выходит УСПЕХОМ, тогда
//     как прямая форма отвечает отказом: скрипт или init-контейнер, потерявший
//     аргумент, объявлялся бы выполнившим накат на трёх сервисах из семи.
//     Гейт держит ФОРМУ (исполнение есть); ИСХОД держит проба самого сервиса
//     (`TestEmptyCommandLineIsRefused`) — статически «отказ» от «успеха» не
//     отличить, и обещать это здесь значило бы обещать несуществующее.
//
// # Чего гейт НЕ утверждает, названо честно
//
// Что семь миграторов ведут себя одинаково на живой базе: это предмет проб
// самих миграторов, и они прогоняются отдельно. Что общий разбор верен:
// его поведение утверждают пробы `pkg/migratorcli` (оба порядка флага, отказы
// по имени). Здесь судится ФОРМА поверхности, а не её исполнение.
//
// # Слепые зоны
//
// Корпус строится из индекса git, поэтому файл, ни разу не добавленный в
// индекс, невидим; окно закрывается на первом `git add`, то есть до слияния.
// Предел общий для всех гейтов этого дерева. Отдельно: строки-комментарии
// отбрасываются до разбора — имя, названное в прозе, поверхностью не является,
// иначе гейт краснел бы на собственном объяснении.
//
// Корпус ограничен каталогами `services` и `deploy`, и это ПРЕДПОСЫЛКА, а не
// умолчание: сегодня бинарь мигратора нигде больше не называется. Предикат,
// которым это проверено и которым проверяется впредь:
//
//	git grep -ln migrator -- . | grep -vE '^(services/|deploy/|internal/repohygiene/|docs/architecture/|pkg/migratorcli/)'
//
// На 2026-08-29 он даёт один файл — `.github/scripts/check-volume-mounts.py`, и
// тот называет КЛЮЧ значений чарта (`migrator.enable`), а не имя бинаря. Формы
// `ENTRYPOINT`/`CMD` с мигратором в дереве нет ни одной. Появится место вне
// этих двух каталогов — расширять надо корпус, а не толковать молчание гейта
// как отсутствие находок.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// migratorCLIBinaryName — единственное законное имя бинаря мигратора.
	migratorCLIBinaryName = "kacho-migrator"

	// migratorCLIDecisionDoc — единственное место, где объявлена поверхность CLI.
	migratorCLIDecisionDoc = "docs/architecture/migrator-cli.md"

	// migratorCLISharedParserImport — общий разбор аргументов прямой формы.
	migratorCLISharedParserImport = "github.com/PRO-Robotech/kacho/pkg/migratorcli"

	// migratorCLICobraImport — разбор аргументов делегирующей формы.
	migratorCLICobraImport = "github.com/spf13/cobra"

	// migratorCLIParseFunc — вызов, которым общий разбор ИСПОЛНЯЕТСЯ. Импорт
	// пакета сам по себе разбором не является: из него берут ещё имя бинаря и
	// тексты отказа.
	migratorCLIParseFunc = "Parse"
)

// migratorCLINameRe — имя, оканчивающееся на «migrator». Именно оканчивающееся:
// оно ловит и `migrator`, и `kacho-migrator`, и любое третье, которое кто-нибудь
// заведёт, — а посторонние бинари (`kacho-vpc`, `zot`) не трогает.
var migratorCLINameRe = `[A-Za-z0-9_.-]*migrator`

var (
	// форма 1 — путь установки либо выход сборки в каталог bin.
	migratorCLIBinPathRe = regexp.MustCompile(`(?:^|[\s"'=:\[(])(?:[\w./-]*/)?bin/(` + migratorCLINameRe + `)\b`)
	// форма 2 — выход `go build -o <путь>`.
	migratorCLIBuildOutRe = regexp.MustCompile(`-o\s+\S*?(` + migratorCLINameRe + `)(?:\s|$)`)
	// форма 3 — переменная Makefile, называющая ИМЯ бинаря (BIN и MIG сразу:
	// `CMD_MIG := ./cmd/migrator` — это каталог исходников, а не имя).
	migratorCLIMakeBinRe = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]*(?:BIN[A-Za-z0-9_]*MIG|MIG[A-Za-z0-9_]*BIN)[A-Za-z0-9_]*)\s*:?\??=\s*(\S+)`)
	// форма 4 — константа имени в самой точке наката.
	migratorCLIGoConstRe = regexp.MustCompile(`binaryName\s*=\s*"(` + migratorCLINameRe + `)"`)
	// формы 5-6 — имя, которым инструмент представляется САМ: поля помощи cobra.
	// `\b` на конце обязателен: без него `migratorcli` (имя ПАКЕТА разбора)
	// читалось бы как имя бинаря.
	migratorCLIBareNameRe = regexp.MustCompile(migratorCLINameRe + `\b`)
)

// migratorCLIHelpFields — поля cobra, которые ЧИТАЕТ ОПЕРАТОР. Имя, названное
// здесь, инструмент печатает о себе: в форме вызова (`Usage: ИМЯ [command]`) и
// в тексте отказа (`unknown command "X" for "ИМЯ"`). Оно и есть имя бинаря с
// точки зрения того, кто им пользуется, — поэтому судится наравне с путём
// установки. Форма живая: расхождение сидело именно в ней.
var migratorCLIHelpFields = map[string]string{
	"Use":     "имя команды cobra",
	"Short":   "краткая справка cobra",
	"Long":    "справка cobra",
	"Example": "пример вызова cobra",
}

// migratorCLIMention — одно место, называющее бинарь мигратора.
type migratorCLIMention struct {
	Rel  string
	Line int
	Name string
	Form string
}

// migratorCLIMentions находит все такие места в одном файле.
//
// Строки-комментарии отбрасываются: гейт судит исполняемую часть, а не прозу о
// ней. Без этого он краснел бы на объяснении, которое сам же и требует писать.
func migratorCLIMentions(rel, content string) []migratorCLIMention {
	var out []migratorCLIMention
	seen := map[string]bool{}

	add := func(line int, name, form string) {
		key := fmt.Sprintf("%d/%s", line, name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, migratorCLIMention{Rel: rel, Line: line, Name: name, Form: form})
	}

	for i, raw := range strings.Split(content, "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, m := range migratorCLIBinPathRe.FindAllStringSubmatch(raw, -1) {
			add(line, m[1], "путь установки")
		}
		for _, m := range migratorCLIBuildOutRe.FindAllStringSubmatch(raw, -1) {
			add(line, path.Base(m[1]), "выход сборки")
		}
		for _, m := range migratorCLIMakeBinRe.FindAllStringSubmatch(raw, -1) {
			if base := path.Base(m[2]); strings.HasSuffix(base, "migrator") {
				add(line, base, "переменная сборки "+m[1])
			}
		}
		for _, m := range migratorCLIGoConstRe.FindAllStringSubmatch(raw, -1) {
			add(line, m[1], "константа имени")
		}
	}
	return out
}

// migratorCLIGoMentions находит имя бинаря в полях помощи cobra — РАЗБОРОМ.
//
// Разбором, а не подстрокой, по той же причине, что и классификация ниже: те же
// слова стоят в комментариях, и гейт по тексту краснел бы на собственном
// объяснении. Справка, собранная склейкой строк (`"a" + "b"`), обходится
// целиком: имя бывает в любом слагаемом.
func migratorCLIGoMentions(rel, src string) ([]migratorCLIMention, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	var out []migratorCLIMention
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isCobraCommandType(lit.Type) {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			form, judged := migratorCLIHelpFields[key.Name]
			if !judged {
				continue
			}
			ast.Inspect(kv.Value, func(v ast.Node) bool {
				bl, ok := v.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return true
				}
				text, uerr := strconv.Unquote(bl.Value)
				if uerr != nil {
					return true
				}
				for _, name := range migratorCLINamesOutsideAPath(text) {
					out = append(out, migratorCLIMention{
						Rel:  rel,
						Line: fset.Position(bl.Pos()).Line,
						Name: name,
						Form: form,
					})
				}
				return true
			})
		}
		return true
	})
	return out, nil
}

// migratorCLINamesOutsideAPath — имена бинаря в строке справки, БЕЗ составляющих
// пути. Токен, которому непосредственно предшествует «/», — это каталог
// исходников (`services/vpc/cmd/migrator`) либо адрес документа, а не имя
// бинаря; путь УСТАНОВКИ судит форма 1, и он живёт в манифесте, а не в справке.
// Без этой границы гейт краснел бы на ссылке справки на собственное решение.
func migratorCLINamesOutsideAPath(text string) []string {
	var out []string
	for _, loc := range migratorCLIBareNameRe.FindAllStringIndex(text, -1) {
		if loc[0] > 0 && text[loc[0]-1] == '/' {
			continue
		}
		out = append(out, text[loc[0]:loc[1]])
	}
	return out
}

// migratorCLINameFindings — места, называющие бинарь не тем именем.
func migratorCLINameFindings(mentions []migratorCLIMention) []string {
	var out []string
	for _, m := range mentions {
		if m.Name == migratorCLIBinaryName {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d — %s называет бинарь %q, а имя одно: %q",
			m.Rel, m.Line, m.Form, m.Name, migratorCLIBinaryName))
	}
	sort.Strings(out)
	return out
}

// migratorCLIParser — разбор аргументов, распознанный у одной точки наката.
type migratorCLIParser struct {
	Rel    string
	Shared bool
	Cobra  bool
	// CommandsWithRun — команд cobra, несущих исполнение.
	CommandsWithRun int
	// CommandsWithArgs — из них таких, где решено, что делать с лишним
	// позиционным аргументом.
	CommandsWithArgs int
	// Undecided — координаты команд, оставивших этот вопрос умолчанию.
	Undecided []string
	// Roots — команд, чьё `Use` называет бинарь, то есть корневых.
	Roots int
	// RootsWithoutRun — координаты корневых команд, не несущих исполнения. Такая
	// команда на ПУСТОЙ командной строке печатает помощь и выходит УСПЕХОМ, и
	// скрипт, потерявший аргумент, объявляется выполнившим накат.
	RootsWithoutRun []string
}

// Recognised — разбор распознан ровно один.
func (p migratorCLIParser) Recognised() bool { return p.Shared != p.Cobra }

// classifyMigratorCLIParser читает объявления и вызовы РАЗБОРОМ, а не подстрокой:
// имя cobra встречается и в комментариях, и гейт по тексту засчитал бы форму по
// объяснению, а не по вызову.
//
// Общий разбор засчитывается по ВЫЗОВУ `migratorcli.Parse`, а не по импорту
// пакета: делегирующая форма импортирует тот же пакет ради ИМЕНИ бинаря и ради
// ТЕКСТОВ отказа — то есть ради того самого сведения, которого гейт и требует.
// Классификация по импорту объявляла бы это «двумя разборами сразу», запрещая
// единственный источник величины. Cobra же засчитывается по импорту: она
// разбирает не одним вызовом, а всем деревом команд.
func classifyMigratorCLIParser(rel, src string) (migratorCLIParser, error) {
	p := migratorCLIParser{Rel: rel}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return p, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	sharedAlias := ""
	for _, imp := range file.Imports {
		value := strings.Trim(imp.Path.Value, `"`)
		switch {
		case value == migratorCLISharedParserImport:
			sharedAlias = path.Base(value)
			if imp.Name != nil {
				sharedAlias = imp.Name.Name
			}
		case value == migratorCLICobraImport:
			p.Cobra = true
		}
	}
	if sharedAlias != "" {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != migratorCLIParseFunc {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == sharedAlias {
				p.Shared = true
			}
			return true
		})
	}
	if !p.Cobra {
		return p, nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isCobraCommandType(lit.Type) {
			return true
		}
		var hasRun, hasArgs, isRoot bool
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Run", "RunE":
				hasRun = true
			case "Args":
				hasArgs = true
			case "Use":
				if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if text, uerr := strconv.Unquote(bl.Value); uerr == nil {
						isRoot = len(migratorCLINamesOutsideAPath(text)) > 0
					}
				}
			}
		}
		if isRoot {
			p.Roots++
			if !hasRun {
				p.RootsWithoutRun = append(p.RootsWithoutRun,
					fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line))
			}
		}
		if !hasRun {
			// Команда без исполнения решать нечего: лишний позиционный аргумент до
			// неё не доходит. Судить её было бы требованием без предмета.
			return true
		}
		p.CommandsWithRun++
		if hasArgs {
			p.CommandsWithArgs++
		} else {
			p.Undecided = append(p.Undecided,
				fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line))
		}
		return true
	})
	return p, nil
}

// isCobraCommandType — литерал вида `cobra.Command{…}` (в т.ч. под амперсандом).
func isCobraCommandType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra" && sel.Sel.Name == "Command"
}

// migratorCLIParserFindings — находки по разбору аргументов.
func migratorCLIParserFindings(parsers []migratorCLIParser) []string {
	var out []string
	for _, p := range parsers {
		switch {
		case p.Shared && p.Cobra:
			out = append(out, fmt.Sprintf(
				"%s — разбирает аргументы И общим пакетом, И cobra: по коду не сказать, какой исполняется", p.Rel))
		case !p.Shared && !p.Cobra:
			out = append(out, fmt.Sprintf(
				"%s — третий разбор аргументов: не зовёт %s.%s и не строит дерево cobra. "+
					"Именно так различие и накапливалось",
				p.Rel, path.Base(migratorCLISharedParserImport), migratorCLIParseFunc))
		}
		for _, r := range p.RootsWithoutRun {
			out = append(out, fmt.Sprintf(
				"%s — корневая команда cobra не несёт исполнения: на ПУСТОЙ командной строке "+
					"она печатает помощь и выходит УСПЕХОМ, поэтому скрипт, потерявший аргумент, "+
					"объявляется выполнившим накат", r))
		}
		for _, u := range p.Undecided {
			out = append(out, fmt.Sprintf(
				"%s — команда cobra с исполнением не решила поле Args: умолчание принимает лишний позиционный аргумент МОЛЧА", u))
		}
	}
	sort.Strings(out)
	return out
}

// ── Производитель текстов отказа ОДИН ──────────────────────────────────────
//
// Тон отказа — часть контракта, и до #1461 у него было две редакции: прямая
// форма говорила словами стандартной библиотеки, делегирующая — словами cobra.
// Сведение держится не сверкой двух копий, а тем, что копия одна: текст
// объявлен в общем пакете, обе формы берут его оттуда.
//
// Гейт судит ОБЪЯВЛЕНИЕ текста, а не его употребление: точка наката вправе
// звать производителя сколько угодно раз, но не вправе написать свою редакцию.

// migratorCLIRefusalPhrases — образцы, по которым узнаётся ОБЪЯВЛЕНИЕ текста
// отказа. Взяты по форматной строке, а не по готовому предложению: своя
// редакция отличается от общей именно словами вокруг подстановок.
var migratorCLIRefusalPhrases = []string{
	"unknown command %q for",
	"unexpected argument %q for",
	"unknown flag: --",
	"no command given",
}

// migratorCLIRefusalOwner — единственный пакет, которому объявлять их можно.
const migratorCLIRefusalOwner = "pkg/migratorcli/"

// migratorCLIRefusalDeclarations находит объявления текстов отказа в одном
// файле — РАЗБОРОМ, по строковым литералам.
//
// Именно по литералам, а не по тексту файла: те же слова стоят в комментариях,
// объясняющих сведение, и гейт по подстроке краснел бы на собственном
// объяснении. Комментарий, повторяющий образец, объявлением не является — он
// ничего не производит.
func migratorCLIRefusalDeclarations(rel, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		text, uerr := strconv.Unquote(bl.Value)
		if uerr != nil {
			return true
		}
		for _, phrase := range migratorCLIRefusalPhrases {
			if strings.Contains(text, phrase) {
				out = append(out, fmt.Sprintf("%s:%d — своя редакция текста отказа (%q). "+
					"Производитель один и живёт в %s: две редакции одного отказа расходятся "+
					"молча, и образец, написанный по одному сервису, на соседнем не срабатывает",
					rel, fset.Position(bl.Pos()).Line, phrase, migratorCLIRefusalOwner))
				break
			}
		}
		return true
	})
	sort.Strings(out)
	return out, nil
}

// migratorCLIJournalRefusals — подача отказа через журнал в точке наката.
//
// Журнал ставит впереди метку времени. Для однократного инструмента командной
// строки это шум, а главное — из одного контракта получаются две редакции: у
// делегирующей формы отказ приходит строкой `Error: <предмет>`, у прямой
// приходил с меткой. Скрипт читает отказ образцом, и это разные строки.
//
// Судятся ВЫЗОВЫ, а не импорт: журнал законен для того, что отказом не
// является, — и такого в точке наката сегодня нет, но запрещать импорт значило
// бы запрещать не то. Форма подачи объявлена в общем пакете.
func migratorCLIJournalRefusals(rel, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "log" {
			return true
		}
		if !strings.HasPrefix(sel.Sel.Name, "Fatal") && !strings.HasPrefix(sel.Sel.Name, "Panic") {
			return true
		}
		out = append(out, fmt.Sprintf(
			"%s:%d — отказ подаётся через журнал (log.%s): метка времени впереди делает из "+
				"одного контракта две редакции. Форма подачи одна и объявлена в %s",
			rel, fset.Position(call.Pos()).Line, sel.Sel.Name, migratorCLIRefusalOwner))
		return true
	})
	sort.Strings(out)
	return out, nil
}
