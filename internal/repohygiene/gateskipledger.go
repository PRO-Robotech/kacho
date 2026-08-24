// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
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
				if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf") {
					return true
				}
				if len(call.Args) == 0 {
					return true
				}
				reason, ok := constPrefix(call.Args[0])
				if !ok {
					return true
				}
				c.Sites = append(c.Sites, SkipSite{
					Rel:    rel,
					Line:   fset.Position(call.Pos()).Line,
					Reason: reason,
				})
				return true
			})
			return nil
		})
		if err != nil {
			return c, err
		}
	}
	sort.Slice(c.Sites, func(i, j int) bool {
		if c.Sites[i].Rel != c.Sites[j].Rel {
			return c.Sites[i].Rel < c.Sites[j].Rel
		}
		return c.Sites[i].Line < c.Sites[j].Line
	})
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
// Направление сверки ОДНО и выбрано осознанно: каждая запись ведомости обязана
// иметь предмет в дереве. Обратного требования («каждый пропуск объявлен») здесь
// НЕТ и быть не должно — необъявленный пропуск обязан краснеть в момент ПРОГОНА,
// а не благословляться при написании: гейт по дереву не знает, случился ли
// пропуск, а прогонщик знает.
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
