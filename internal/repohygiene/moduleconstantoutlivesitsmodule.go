// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleconstantoutlivesitsmodule.go — КОНСТАНТА, НАЗЫВАЮЩАЯ ПУТЬ МОДУЛЯ,
// ОБЪЯВЛЯЕТ, ЧТО ЕЁ ПРЕДМЕТ ИСЧЕЗ, вместо того чтобы замолчать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Гейты дерева судят чужой код по ПУТИ ИМПОРТА, а не по каталогу: пакет, чей
// импорт совпал с константой, признаётся производителем, потребителем,
// владельцем. Удаление КАТАЛОГА такое условие не нарушает — оно просто
// перестаёт совпадать:
//
//	const applierImportPath = "github.com/PRO-Robotech/kaname/internal/…"
//	   каталог уехал → совпадений 0 → находок 0 → перепись 0 → вердикт ЗЕЛЁНЫЙ
//
// Отличить это от исправной работы нельзя ничем: у негативного утверждения нет
// ведомости, которая могла бы истечь (`testing.md` §«Гейт на класс», п. 9). И
// есть отягощение, которого у обычного замолкания нет: предмет снимается НЕ
// правкой самой проверки — автор снятия каталога о ней не узнаёт вовсе, потому
// что его изменение этих файлов не касается.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — МОДУЛЬ, А НЕ ПАКЕТ
//
// Спрашивается ровно одно: объявлен ли КОРНЕВОЙ МОДУЛЬ, названный константой,
// хоть одним `go.mod` дерева. Существование конкретного пакета под ним НЕ
// спрашивается намеренно:
//
//   - пакет переезжает внутри модуля рутинно, и находка на каждом переезде
//     сделала бы гейт признаком, который отключают первым;
//   - предмет задачи — модуль, уехавший ЦЕЛИКОМ вместе со своим каталогом; для
//     него признак «модуль не объявлен» точен и не даёт ложных.
//
// Названо, чтобы наличие гейта не читалось шире: константа, называющая
// несуществующий пакет ЖИВОГО модуля, здесь молчит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВЛАДЕЛЕЦ ИМЁН ВЫВОДИТСЯ ИЗ ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ
//
// Приставка владельца (`github.com/PRO-Robotech`) берётся у объявленных
// модулей, а не стоит здесь константой. Выписанная — она была бы ровно тем, что
// этот гейт ловит: собственным именем, пережившим свой предмет. Смена владельца
// имён перенастраивает гейт сама.
//
// Следствие, названное вслух: чужие пути импорта (`google.golang.org/grpc`) под
// ось не подпадают — их модули объявляет не это дерево, и судить их нечем.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ ЗНАЕТ ОДНУ ФОРМУ, И ЭТО РЕШЕНИЕ
//
// Судится СТРОКОВАЯ КОНСТАНТА, чьё значение ЦЕЛИКОМ есть путь модуля либо путь
// пакета под ним. Литерал, лишь СОДЕРЖАЩИЙ такой путь, — не она:
//
//	const applierImportPath = "github.com/PRO-Robotech/kaname/internal/x"  ← судится
//	const injSrc = "package p\nimport \"github.com/PRO-Robotech/kaname/x\"" ← нет
//
// Второе — фикстура инъекции: синтетический исходник, который гейт подаёт
// другому гейту на вход. Она не утверждает о дереве ничего и после снятия
// каталога продолжает работать. Первое — координата, которой судят дерево.
//
// Разбор идёт по УЗЛУ синтаксического дерева (объявление `const` со строковым
// литералом), а не по образцу над текстом: поиск по подстроке нашёл бы имя
// модуля в комментарии — в том числе в этом файле, — и гейт краснел бы на
// собственном объяснении.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ ДЕЛАЕТ С НАХОДКОЙ — И ЧЕГО ОН НЕ РЕШАЕТ ЗА АВТОРА
//
// Он превращает МОЛЧАНИЕ в ОТКАЗ, и только это. Какой из двух исходов выбрать,
// решает автор снятия, и «оставить как есть» в них не входит:
//
//  1. перевести условие на признак, который дерево ПРОИЗВОДИТ, — координату
//     каталога либо объявление модуля, — тогда снятие краснеет само;
//  2. снять константу ВМЕСТЕ с предметом, тем же изменением.
//
// Разница с ведомостью исключений существенная и намеренная: ведомость требует
// записи на каждое снятие и краснеет на обычной работе, поэтому её отключают
// первой. Здесь записи не заводится вовсе — гейт молчит, пока модуль объявлен, и
// говорит ровно в тот день, когда предмет уехал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПЕРЕПИСЬ НЕ МОЖЕТ НАЗВАТЬ — сказано, а не умолчано
//
// Она печатает по каждому носителю: константу, её модуль и объявлен ли он.
// Третьего числа — «сколько совпадений даёт условие ЭТОГО носителя в дереве» —
// у неё нет и быть не может: у каждого носителя своё условие (импорт, селектор,
// приставка пути), и посчитать их одним обходом значило бы переписать сюда пять
// разных гейтов. Совпадения по модулю она называет, и этого довольно, чтобы
// «ноль находок» было отличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// ModulePathConstant — одна координата: константа, чьё значение есть путь
// модуля дерева или пакета под ним.
type ModulePathConstant struct {
	// File — путь носителя от корня дерева.
	File string
	// Line — строка объявления.
	Line int
	// Name — имя константы.
	Name string
	// Value — её значение целиком.
	Value string
	// Module — корневой модуль, выведенный из значения.
	Module string
}

// ModuleConstantCensus — объём осмотренного.
//
// Печатается ТРИ числа, а не одно: «констант 0» означает сломанный
// распознаватель, «модулей 0» — непрочитанный состав, а «совпадений 0» при
// живых первых двух есть законная чистота. Одно число слило бы все три.
type ModuleConstantCensus struct {
	// Files — файлов Go прочитано.
	Files int
	// Consts — строковых констант прочитано.
	Consts int
	// Modules — модулей объявлено `go.mod` дерева.
	Modules int
	// Owners — приставок владельца имён выведено из объявленных модулей.
	Owners int
	// Matched — констант, названных путём модуля дерева.
	Matched int
	// PerModule — совпадений по каждому модулю отдельно.
	PerModule map[string]int
}

// String — перепись одной строкой плюс строка на модуль.
func (c ModuleConstantCensus) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "файлов Go %d, строковых констант %d, объявленных модулей %d "+
		"(владельцев имён %d), из констант названы путём модуля %d",
		c.Files, c.Consts, c.Modules, c.Owners, c.Matched)
	names := make([]string, 0, len(c.PerModule))
	for m := range c.PerModule {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		fmt.Fprintf(&b, "\n  модуль %-40s констант %3d", m, c.PerModule[m])
	}
	return b.String()
}

// declaredModulePath — путь модуля, объявленный содержимым `go.mod`.
//
// Читается ПЕРВАЯ директива `module`, а не первая строка: файл вправе начинаться
// с комментария, и разбор по номеру строки объявил бы такой модуль
// необъявленным — то есть дал бы находку на верном дереве.
func declaredModulePath(src string) (string, bool) {
	for _, raw := range strings.Split(src, "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "module")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			continue
		}
		return strings.Trim(rest, `"`), true
	}
	return "", false
}

// moduleNameOwners — приставки владельца имён по объявленным модулям.
// `github.com/PRO-Robotech/kacho` → `github.com/PRO-Robotech`.
func moduleNameOwners(modules []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range modules {
		owner := path.Dir(m)
		if owner == "." || owner == "/" || seen[owner] {
			continue
		}
		seen[owner] = true
		out = append(out, owner)
	}
	sort.Strings(out)
	return out
}

// modulePathOfConstValue — корневой модуль, названный значением константы.
//
// Второй результат false означает «значение путём модуля ЭТОГО дерева не
// является»: чужая зависимость, произвольная строка, кусок исходника. Такие
// значения из оси выпадают by construction, а не по списку исключений.
func modulePathOfConstValue(value string, owners []string) (string, bool) {
	v := strings.TrimSuffix(value, "/")
	for _, owner := range owners {
		rest, ok := strings.CutPrefix(v, owner+"/")
		if !ok || rest == "" {
			continue
		}
		name := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name = rest[:i]
		}
		if name == "" {
			continue
		}
		return owner + "/" + name, true
	}
	return "", false
}

// collectModulePathConstants — строковые константы файла, названные путём
// модуля дерева. Разбирает УЗЛЫ, а не текст (см. шапку).
//
// Второй результат — сколько строковых констант прочитано ВСЕГО: без него «ноль
// совпадений» неотличимо от «файл не разобран».
func collectModulePathConstants(rel, src string, owners []string) ([]ModulePathConstant, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", rel, err)
	}
	var out []ModulePathConstant
	read := 0
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, id := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				read++
				module, ok := modulePathOfConstValue(value, owners)
				if !ok {
					continue
				}
				out = append(out, ModulePathConstant{
					File: rel, Line: fset.Position(id.Pos()).Line,
					Name: id.Name, Value: value, Module: module,
				})
			}
		}
	}
	return out, read, nil
}

// judgeModulePathConstants — ЧИСТОЕ суждение: у каждой константы, названной
// путём модуля, этот модуль объявлен деревом.
//
// Разведено с добычей входа намеренно: инъекция гоняет эту функцию, а не её
// копию.
func judgeModulePathConstants(
	consts []ModulePathConstant, declared []string, files, constsRead, owners int,
) ([]string, ModuleConstantCensus) {
	census := ModuleConstantCensus{
		Files: files, Consts: constsRead, Modules: len(declared),
		Owners: owners, Matched: len(consts), PerModule: map[string]int{},
	}
	have := map[string]bool{}
	for _, m := range declared {
		have[m] = true
		census.PerModule[m] = 0
	}
	for _, c := range consts {
		census.PerModule[c.Module]++
	}

	// ПРЕДПОСЫЛКИ. Каждая — отказ, а не пустой успех: «ноль находок» обязано
	// быть отличимо от «ноль прочитанного», и три разных нуля означают три
	// разные поломки.
	switch {
	case len(declared) == 0:
		return []string{"ни одного `go.mod` не прочитано: объявленных модулей ноль, и " +
			"всякая константа была бы объявлена пережившей свой модуль — это отказ, " +
			"а не находка"}, census
	case owners == 0:
		return []string{"владельца имён не выведено ни одного: приставка берётся у " +
			"объявленных модулей, и без неё судить нечем"}, census
	case files == 0:
		return []string{"обход пуст: ни одного файла Go не прочитано — вердикт " +
			"относился бы к непрочитанному"}, census
	case constsRead == 0:
		return []string{"строковых констант не прочитано ни одной: распознаватель " +
			"разошёлся с деревом (разбор узлов сломан либо корпус не тот). " +
			"Это отказ, а не чистота"}, census
	}

	var faults []string
	for _, c := range consts {
		if have[c.Module] {
			continue
		}
		faults = append(faults, fmt.Sprintf(
			"%s:%d константа %s = %q называет модуль %s, которого НЕ ОБЪЯВЛЯЕТ ни один "+
				"`go.mod` дерева: условие, построенное на этом пути, перестало совпадать — "+
				"совпадений ноль, находок ноль, вердикт зелёный, и отличить это от "+
				"исправной работы нечем",
			c.File, c.Line, c.Name, c.Value, c.Module))
	}
	sort.Strings(faults)
	return faults, census
}
