// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GateSkipLedgerFile — ведомость законных пропусков у гейтов дерева. Её читают
// ДВОЕ: прогонщик юнитов `.github/scripts/go-test-verdict.py` (он краснеет на
// необъявленном пропуске) и гейт самоистечения ниже (он роняет запись, которой в
// дереве больше нечего исключать).
//
// Формат объявлен здесь и дословно повторён в шапке самой ведомости и в
// прогонщике — он намеренно в одну строку логики, чтобы два читателя не могли
// разойтись на разборе: строка до первого `#`, обрезанная по краям; пустая —
// не запись.
const GateSkipLedgerFile = ".github/scripts/gate-skips-allowed.txt"

// ParseGateSkipLedger — записи ведомости в порядке файла.
func ParseGateSkipLedger(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SkipSite — вызов `t.Skip`/`t.Skipf` в пробе: координата и КОНСТАНТНОЕ начало
// сообщения (для `Skipf` — часть строки формата до первой подстановки).
type SkipSite struct {
	Rel    string
	Line   int
	Reason string
}

// SkipCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного», поэтому перепись — отдельное утверждение, а не следствие
// пустого списка находок.
type SkipCensus struct {
	FilesRead int
	Sites     []SkipSite

	// Unresolved — вызовы пропуска, чью причину разбор НЕ разрешает: `t.SkipNow()`
	// (причины нет вовсе) и `t.Skip(v)` с неконстантным первым аргументом.
	//
	// Считать их отдельно обязательно. Такой пропуск невидим ОБЕИМ проверкам: гейт
	// по дереву не знает его причины, а прогонщик берёт за причину последнюю строку
	// перед `--- SKIP` — то есть судит соседний `t.Log`, а не сам пропуск. Молча
	// выпав из переписи, он стал бы ровно тем, против чего заведена ведомость:
	// пропуском, неотличимым от успеха.
	Unresolved []SkipSite
}

// CollectSkipSites обходит пробные файлы под корнями и собирает вызовы пропуска
// РАЗБОРОМ, а не текстом: причина, стоящая в комментарии (в том числе в
// комментарии, объясняющем этот самый запрет), вызовом не является и в перепись
// не попадает.
func CollectSkipSites(roots []string) (SkipCensus, error) {
	var c SkipCensus
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			c.FilesRead++
			rel := path
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Skip", "Skipf", "SkipNow":
				default:
					return true
				}
				site := SkipSite{Rel: rel, Line: fset.Position(call.Pos()).Line}
				if len(call.Args) == 0 {
					// `t.SkipNow()` — причины нет вовсе.
					c.Unresolved = append(c.Unresolved, site)
					return true
				}
				reason, ok := constPrefix(call.Args[0])
				if !ok {
					c.Unresolved = append(c.Unresolved, site)
					return true
				}
				site.Reason = reason
				c.Sites = append(c.Sites, site)
				return true
			})
			return nil
		})
		if err != nil {
			return c, err
		}
	}
	byCoord := func(s []SkipSite) func(i, j int) bool {
		return func(i, j int) bool {
			if s[i].Rel != s[j].Rel {
				return s[i].Rel < s[j].Rel
			}
			return s[i].Line < s[j].Line
		}
	}
	sort.Slice(c.Sites, byCoord(c.Sites))
	sort.Slice(c.Unresolved, byCoord(c.Unresolved))
	return c, nil
}

// constPrefix — константное НАЧАЛО выражения: строковый литерал либо конкатенация
// литералов. Второе возвращаемое значение — «начало вообще есть»; `whole`
// (внутреннее) отвечает на другой вопрос — «начало дотянулось до конца выражения».
//
// Различать их обязательно: у `t.Skip("a" + v + "b")` левая половина обрывается
// на переменной, и приклеить к ней `"b"` значило бы объявить префиксом строку,
// которой в сообщении не будет НИКОГДА. Живого такого вызова в дереве сегодня
// нет — правило записано вперёд, потому что ошибка этого рода тиха: ведомость
// сверялась бы с выдуманным началом и молча прощала бы не то.
//
// Подстановка `%` у `Skipf` обрывает константную часть по той же причине.
func constPrefix(e ast.Expr) (string, bool) {
	prefix, _, ok := constPrefixWhole(e)
	return prefix, ok
}

func constPrefixWhole(e ast.Expr) (prefix string, whole, ok bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false, false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false, false
		}
		if i := strings.Index(s, "%"); i >= 0 {
			return s[:i], false, true
		}
		return s, true, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false, false
		}
		left, leftWhole, lok := constPrefixWhole(v.X)
		if !lok {
			return "", false, false
		}
		if !leftWhole {
			return left, false, true
		}
		right, rightWhole, rok := constPrefixWhole(v.Y)
		if !rok {
			return left, false, true
		}
		return left + right, rightWhole, true
	}
	return "", false, false
}

// AuditGateSkipLedger — ядро гейта, отделённое от корня дерева НАМЕРЕННО: инъекция
// обязана прогнать его на синтетическом дереве, а не на этом.
//
// Направление здесь ОДНО: каждая запись ведомости обязана иметь предмет в дереве.
// Обратное направление живёт в `AuditGateSkipDeclared` и намеренно вынесено в
// ОТДЕЛЬНУЮ функцию: инъекция обязана ронять ровно то свойство, которое проверяет,
// а слитые в одну функцию направления краснели бы вместе и не различались.
func AuditGateSkipLedger(entries []string, c SkipCensus) []string {
	var findings []string
	for _, entry := range entries {
		matched := false
		for _, s := range c.Sites {
			if strings.HasPrefix(s.Reason, entry) {
				matched = true
				break
			}
		}
		if !matched {
			findings = append(findings, GateSkipLedgerFile+": запись «"+entry+
				"» не соответствует ни одному вызову t.Skip у гейтов дерева — "+
				"у послабления нет предмета, и оно переживёт то, ради чего заведено; "+
				"снимите запись либо назовите пробу, которая этой причиной пропускается")
		}
	}
	return findings
}

// AuditGateSkipDeclared — ОБРАТНОЕ направление сверки: каждый пропуск у гейтов
// дерева объявлен ведомостью.
//
// # Почему одного прогонщика мало — предмет задачи #2548
//
// Прогонщик юнитов краснеет на необъявленном пропуске В МОМЕНТ ПРОГОНА, и это
// верно ровно для пропусков, которые СЛУЧИЛИСЬ. Пропуск, чья предпосылка сегодня
// держится, не случается — и потому невидим: он молчит до того дня, когда
// предпосылка откажет, а тогда прогон краснеет НЕ НА ДЕФЕКТЕ.
//
// Замер, из которого это выведено (`internal/repohygiene`, 966 файлов Go): вызовов
// пропуска 13, срабатывает при `-short` ОДИН. То есть двенадцать причин прогонщик
// не судил ни разу, и пять из них не были объявлены.
//
// Худший случай нашёлся среди них: проба ведомости границ фундамента пропускалась
// при ПУСТОЙ ведомости — то есть на достижении цели выноса фундамента. Успех
// работы уронил бы прогон.
//
// # Почему это не «благословение при написании»
//
// Прежняя редакция отвергала обратное направление доводом: объявлять пропуск
// законным при написании значило бы разоружать прогонщик заранее. Довод снят не
// мнением, а устройством ПАРЫ направлений: запись без живого вызова роняет
// `AuditGateSkipLedger`, а вызов без записи роняет эту функцию. Значит завести
// пропуск и объявить его законным можно только ОДНИМ изменением, где стоит и то и
// другое, — ровно то, чего шапка ведомости и добивается. Разница лишь в том, когда
// это видно: при написании, а не при отказе предпосылки через полгода.
func AuditGateSkipDeclared(entries []string, c SkipCensus) []string {
	var findings []string
	for _, s := range c.Sites {
		declared := false
		for _, entry := range entries {
			if strings.HasPrefix(s.Reason, entry) {
				declared = true
				break
			}
		}
		if !declared {
			findings = append(findings, fmt.Sprintf(
				"%s:%d: пропуск с причиной «%s» не объявлен в %s — сегодня он молчит, "+
					"потому что его предпосылка держится, а в день её отказа прогон "+
					"покраснеет НЕ НА ДЕФЕКТЕ. Исходов два: причина законна by "+
					"construction — внесите её в ведомость СО СВОИМ основанием; либо "+
					"пропуска здесь быть не должно — перепишите пробу так, чтобы на "+
					"достигнутой цели ПРОХОДИТЬ с переписью, а на отказе предпосылки "+
					"КРАСНЕТЬ (отказ предпосылки ведомостью не прощается)",
				s.Rel, s.Line, s.Reason, GateSkipLedgerFile))
		}
	}
	for _, s := range c.Unresolved {
		findings = append(findings, fmt.Sprintf(
			"%s:%d: причину пропуска не разрешает разбор (t.SkipNow либо неконстантный "+
				"первый аргумент) — такой пропуск невидим ОБЕИМ проверкам: ведомости "+
				"сверять не с чем, а прогонщик примет за причину последнюю строку перед "+
				"`--- SKIP`, то есть соседний t.Log. Назовите причину строковым литералом",
			s.Rel, s.Line))
	}
	return findings
}
