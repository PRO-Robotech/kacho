// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

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

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// ContractRootLiteralFinding — одно место, где популяция отбирается ЛИТЕРАЛОМ
// приставки корня контрактов вместо объявленного словаря.
type ContractRootLiteralFinding struct {
	File    string
	Line    int
	Literal string
	Call    string
}

func (f ContractRootLiteralFinding) String() string {
	return fmt.Sprintf(
		"%s:%d: %s(…, %q) отбирает популяцию ЛИТЕРАЛОМ приставки корня контрактов. "+
			"Корней в дереве больше одного, и дерево второго корня такой отбор не "+
			"находит: он не краснеет и не зеленеет, а МОЛЧИТ — проверка честно "+
			"печатает ноль по опустевшей популяции. Отбор берётся у объявленного "+
			"словаря: contractroot.HasAnyPrefix(x, contractroot.NamePrefixes()) для "+
			"полных имён, PathPrefixes() для путей, CloudDirs/ResolveDomain для "+
			"каталогов",
		f.File, f.Line, f.Call, f.Literal)
}

// ContractRootLiteralCensus — объём осмотренного.
type ContractRootLiteralCensus struct {
	FilesRead    int
	PrefixCalls  int
	LiteralArgs  int
	RootLiterals int
	Unparsed     []string
}

func (c ContractRootLiteralCensus) String() string {
	return fmt.Sprintf(
		"файлов Go прочитано %d · вызовов проверки приставки %d · из них с литералом %d · "+
			"из них приставкой КОРНЯ %d · не разобрано %d · объявленных корней %v",
		c.FilesRead, c.PrefixCalls, c.LiteralArgs, c.RootLiterals, len(c.Unparsed),
		contractroot.Roots)
}

// contractRootPrefixFuncs — функции проверки приставки, чей второй аргумент и
// есть отбор популяции.
//
// Перечень закрыт намеренно: он называет ФОРМЫ, в которых отбор записывается в
// этом дереве. Форма, о которой распознаватель не знает, даёт не красное и не
// зелёное, а молчание, — поэтому перепись печатает, сколько таких вызовов
// вообще встречено, и падает на нуле.
var contractRootPrefixFuncs = map[string]bool{
	"HasPrefix":    true,
	"TrimPrefix":   true,
	"CutPrefix":    true,
	"HasSuffix":    false, // суффикс приставкой корня не бывает: корень стоит В НАЧАЛЕ
	"Contains":     false, // подстрока популяцию не отбирает — она её лишь ищет
	"EqualFold":    false,
	"Split":        false,
	"SplitN":       false,
	"TrimSuffix":   false,
	"CutSuffix":    false,
	"ReplaceAll":   false,
	"Replace":      false,
	"Index":        false,
	"LastIndex":    false,
	"TrimLeft":     false,
	"TrimFunc":     false,
	"ContainsRune": false,
}

// rootLiteral — является ли строка приставкой ОБЪЯВЛЕННОГО корня.
//
// Судятся ровно две формы приставки — имени (`kacho.`) и пути (`kacho/`).
// Литерал с продолжением (`kacho.cloud.compute`) отбирает ДОМЕН, а не корень:
// это законный отбор, и он молчит. Различие несущее — гейт, краснеющий на
// верном коде, отключают первым.
func rootLiteral(s string) bool {
	for _, r := range contractroot.Roots {
		if s == r+"." || s == r+"/" {
			return true
		}
	}
	return false
}

// AuditContractRootLiterals — вердикт о дереве.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Проверка, отбирающая свою популяцию литералом приставки бренда, была верна,
// пока корень дерева контрактов был один. Корней стало два — платформа зовётся
// `kacho`, служба доступа `kaname`, — и всё, что лежит под вторым, ПЕРЕСТАЛО
// РАССМАТРИВАТЬСЯ. Это молчание, а не находка: проверка честно печатает ноль по
// своей популяции, потому что популяция сузилась (kacho#2138).
//
// Отличить «ноль находок» от «ноль прочитанного» можно только переписью, и
// печатает её не каждое такое место.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО НЕ СУДИТСЯ, И ЭТО НАЗВАНО
//
//   - литерал с продолжением (`kacho.cloud.compute`) — отбор ДОМЕНА, законный;
//   - имя корня внутри строки сообщения, комментария, координаты — не отбор;
//   - объявление самого словаря (`pkg/contractroot`) — он и есть владелец.
func AuditContractRootLiterals(root string, dirs []string) ([]ContractRootLiteralFinding, ContractRootLiteralCensus, error) {
	var (
		census   ContractRootLiteralCensus
		findings []ContractRootLiteralFinding
	)
	fset := token.NewFileSet()

	for _, dir := range dirs {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return nil, census, fmt.Errorf(
				"каталог обхода %s не разрешается: обход был бы пуст, а «ноль находок» "+
					"неотличимо от «ноль прочитанного»", dir)
		}
		err := filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			census.FilesRead++

			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				census.Unparsed = append(census.Unparsed, rel)
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, isIdent := sel.X.(*ast.Ident)
				if !isIdent || pkg.Name != "strings" {
					return true
				}
				judged, known := contractRootPrefixFuncs[sel.Sel.Name]
				if !known || !judged {
					return true
				}
				census.PrefixCalls++
				lit, isLit := call.Args[1].(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					return true
				}
				census.LiteralArgs++
				val, uerr := strconv.Unquote(lit.Value)
				if uerr != nil || !rootLiteral(val) {
					return true
				}
				census.RootLiterals++
				findings = append(findings, ContractRootLiteralFinding{
					File: rel, Line: fset.Position(lit.Pos()).Line,
					Literal: val, Call: "strings." + sel.Sel.Name,
				})
				return true
			})
			return nil
		})
		if err != nil {
			return nil, census, err
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}
