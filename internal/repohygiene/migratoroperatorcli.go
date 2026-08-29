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
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
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
)

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
}

// Recognised — разбор распознан ровно один.
func (p migratorCLIParser) Recognised() bool { return p.Shared != p.Cobra }

// classifyMigratorCLIParser читает ИМПОРТЫ и объявления команд разбором, а не
// подстрокой: имя cobra встречается и в комментариях, и гейт по тексту засчитал
// бы форму по объяснению, а не по вызову.
func classifyMigratorCLIParser(rel, src string) (migratorCLIParser, error) {
	p := migratorCLIParser{Rel: rel}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return p, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	for _, imp := range file.Imports {
		value := strings.Trim(imp.Path.Value, `"`)
		switch {
		case value == migratorCLISharedParserImport:
			p.Shared = true
		case value == migratorCLICobraImport:
			p.Cobra = true
		}
	}
	if !p.Cobra {
		return p, nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isCobraCommandType(lit.Type) {
			return true
		}
		var hasRun, hasArgs bool
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
			}
		}
		if !hasRun {
			// Корневая команда исполнения не несёт: неизвестную подкоманду cobra
			// и так называет. Судить её было бы требованием без предмета.
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
				"%s — третий разбор аргументов: ни общий пакет %q, ни cobra. Именно так различие и накапливалось",
				p.Rel, migratorCLISharedParserImport))
		}
		for _, u := range p.Undecided {
			out = append(out, fmt.Sprintf(
				"%s — команда cobra с исполнением не решила поле Args: умолчание принимает лишний позиционный аргумент МОЛЧА", u))
		}
	}
	sort.Strings(out)
	return out
}
