// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildstampword_test.go — слово «сборка штамп не проставила» у платформы и у
// службы доступа ОДНО, хотя объявлений два.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Ряд `*_build_info` держат шесть процессов, и на непроставленном штампе каждый
// обязан ответить одним и тем же словом. Дежурный сверяет витрины разных
// процессов не пересчитывая; два написания одного состояния означали бы две
// тревоги об одном предмете, и одна из них однажды не будет написана.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ДВА ОБЪЯВЛЕНИЯ, А НЕ ОДНО — И ПОЧЕМУ ЭТО НЕ ЛЕНЬ
//
// Служба доступа — ОТДЕЛЬНЫЙ Go-модуль (`polyrepo.md` §Build-граф: модулей два, и
// второй зависит от первого пином). Общей константы у них быть не может: импорт
// платформы в службу существует, но привязан к ПИНУ, поэтому правка слова в
// платформе доехала бы до службы только со следующим бампом — то есть молча и
// не тогда. Значит объявления два по построению, а держит их согласие ЭТОТ гейт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТСЯ ОБЪЯВЛЕНИЕ, А НЕ ТЕКСТ
//
// Слово `unstamped` стоит в прозе — в шапках, в документации службы и в этом
// файле. Поиск по подстроке нашёл бы собственное объяснение и остался бы зелёным
// при разошедшихся объявлениях. Поэтому значение берётся разбором синтаксического
// дерева: узел объявления константы, а не строка файла.
//
// Способность упасть и смолчать доказана инъекцией — buildstampword_injection_test.go.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/observability"
)

// accessServiceBuildStampWordFile — где служба доступа объявляет своё слово.
const accessServiceBuildStampWordFile = "services/iam/internal/observability/metrics/build_info.go"

// accessServiceBuildStampWordConst — имя константы в том объявлении.
const accessServiceBuildStampWordConst = "BuildInfoUnstamped"

// ConstStringValue — значение строковой константы, объявленной в исходнике.
// Возвращает признак находки отдельно от значения: «объявления нет» и
// «объявлено пустым» — разные состояния, и схлопывать их нельзя.
func ConstStringValue(src []byte, name string) (string, bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name+".go", src, 0)
	if err != nil {
		return "", false, fmt.Errorf("разбор исходника: %w", err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if ident.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", false, fmt.Errorf(
						"%s объявлена не строковым литералом: значение вычисляется, "+
							"и сверить его разбором нельзя", name)
				}
				unquoted, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					return "", false, fmt.Errorf("значение %s: %w", name, uerr)
				}
				return unquoted, true, nil
			}
		}
	}
	return "", false, nil
}

// TestBuildStampWordMatchesTheAccessServiceWord — слово одно на оба модуля.
func TestBuildStampWordMatchesTheAccessServiceWord(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, filepath.FromSlash(accessServiceBuildStampWordFile))

	src, err := os.ReadFile(path) // #nosec G304 -- путь-константа своего дерева
	if err != nil {
		t.Fatalf("%s: %v — «ноль находок» здесь означало бы «ноль прочитанного»",
			accessServiceBuildStampWordFile, err)
	}

	word, found, err := ConstStringValue(src, accessServiceBuildStampWordConst)
	if err != nil {
		t.Fatalf("%s: %v", accessServiceBuildStampWordFile, err)
	}
	if !found {
		t.Fatalf("%s: константа %s не объявлена — сверять нечего, и молчание гейта "+
			"ничего не утверждало бы. Переехала? Поправьте координату ЗДЕСЬ",
			accessServiceBuildStampWordFile, accessServiceBuildStampWordConst)
	}

	t.Logf("перепись: прочитано объявлений 2 — платформа %q · служба доступа %q",
		observability.BuildStampUnstamped, word)

	if word != observability.BuildStampUnstamped {
		t.Errorf("слово «штамп не проставлен» разошлось между модулями: платформа "+
			"говорит %q, служба доступа — %q. Дежурный сверяет витрины разных процессов "+
			"не пересчитывая, а два написания одного состояния дают две тревоги об одном "+
			"предмете — и одна из них однажды не будет написана. Модули разные, общей "+
			"константы у них быть не может (пин), поэтому согласие держит только этот гейт",
			observability.BuildStampUnstamped, word)
	}
}
