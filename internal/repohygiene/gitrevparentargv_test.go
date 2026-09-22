// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gitrevparentargv_test.go — держатель одного свойства: ВОПРОС О РОДИТЕЛЕ HEAD
// ЗАДАЁТСЯ В ПАКЕТЕ ОДНИМ НАБОРОМ АРГУМЕНТОВ.
//
// # Почему это свойство нельзя проверить прогоном
//
// Фикстура [gateBaseRealExitError] обязана воспроизводить ТО, что приходит на
// боевой путь: `*exec.ExitError` с кодом 1. Но код 1 — свойство очень широкого
// класса вопросов к `git`, а не этого вопроса: `cat-file -e` о несуществующем
// объекте, в другом репозитории, другой подкомандой даёт ровно тот же класс и
// ровно тот же код. Измерено инъекцией 2026-09-22: подмена
// `rev-parse --verify --quiet HEAD^` на `cat-file -e <несуществующий объект>`
// оставила ОБА стража фикстуры (класс отказа и код) молчащими — `ok`, EXIT=0.
//
// Значит тождество команд ПРОГОНОМ не проверяется в принципе: оба конца дают
// неотличимый исход. Проверяется оно только СТРОЕНИЕМ — тем, что обе стороны
// берут аргументы из одного объявления и обойти его нечем.
//
// # Что именно судится
//
//  1. ДОМ ОДИН. Объявление [gitRevParentArgv] в пакете ровно одно.
//  2. МИМО ДОМА НИКТО НЕ СПРАШИВАЕТ. Ни один вызов `gitenv.Command` не несёт
//     литералами СРАЗУ `rev-parse` и `HEAD^`: это и есть вторая запись того же
//     вопроса, а две записи расходятся молча — ровно это и было измерено.
//     Одного `HEAD^` мало: `log HEAD^` спрашивает другое, и краснеть на нём
//     значило бы объявлять находкой законного близнеца.
//  3. ВОПРОС ЗАДАН ИЗ ДОМА ОБЕИМИ СТОРОНАМИ. Вызов вида
//     `gitenv.Command(<дерево>, gitRevParentArgv()...)` есть и в боевом файле, и
//     в пробном. Судится именно ВЫЗОВ, а не упоминание имени: фикстура называет
//     `gitRevParentArgv()` ещё и в текстах своих отказов, и считать упоминания
//     значило бы оставить дыру — подмену спрашивающей команды на `cat-file`
//     такая перепись пережила бы молча, а это ровно снятый дефект.
//
// Согласованная смена — правка тела [gitRevParentArgv] — не трогает ни одного
// из трёх пунктов и молчит: именно этого от общего источника и ждут.

// gitRevParentHome — имя общего дома аргументов вопроса о родителе.
const gitRevParentHome = "gitRevParentArgv"

// Признак ВТОРОЙ ЗАПИСИ вопроса: подкоманда и объект, стоящие литералами в
// одном вызове `gitenv.Command`.
//
// Оба слова, а не одно: `HEAD^` встречается и в чужих вопросах об этом объекте,
// и объявлять их находкой значило бы краснеть на законном близнеце; `rev-parse`
// же спрашивают и о других объектах — в этом же пакете о ссылке линии и о
// ревизии ведомости.
const (
	gitRevParentVerb   = "rev-parse"
	gitRevParentObject = "HEAD^"
)

// packageCallSites — КТО ЗОВЁТ функцию пакета, с координатами, раздельно по
// боевым и пробным файлам.
//
// Раздельно намеренно: свойство «фикстура берёт команду там же, где боевой
// путь» есть утверждение именно о ДВУХ сторонах, и одно суммарное число его не
// выражает — два боевых зовущих при нуле пробных дали бы то же «2».
func packageCallSites(t *testing.T, name string) (prod, test []string) {
	t.Helper()
	_ = repohygienePackageWalk(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != name {
				return true
			}
			coord := fmt.Sprintf("%s:%d", filepath.Base(path), fset.Position(id.Pos()).Line)
			if strings.HasSuffix(path, "_test.go") {
				test = append(test, coord)
			} else {
				prod = append(prod, coord)
			}
			return true
		})
	})
	return prod, test
}

// TestGitRevParentQuestionHasASingleArgvHome — свойство из шапки файла.
func TestGitRevParentQuestionHasASingleArgvHome(t *testing.T) {
	t.Parallel()

	var homes, literalAskers, prodAskers, probeAskers []string
	commandCalls := 0

	census := repohygienePackageWalk(t, func(path string, fset *token.FileSet, file *ast.File) {
		base := filepath.Base(path)
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil && x.Name.Name == gitRevParentHome {
					homes = append(homes, fmt.Sprintf("%s:%d", base,
						fset.Position(x.Name.Pos()).Line))
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Command" {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "gitenv" {
					return true
				}
				commandCalls++
				coord := fmt.Sprintf("%s:%d", base, fset.Position(x.Lparen).Line)
				words := map[string]bool{}
				fromHome := false
				for _, a := range x.Args {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							words[v] = true
						}
						continue
					}
					// Спрашивающий ИЗ ДОМА: аргументы команды суть развёрнутый
					// результат [gitRevParentArgv]. Судится вызов, а не
					// упоминание имени, — упоминание живёт и в текстах отказов
					// фикстуры и подмену команды пережило бы молча.
					inner, ok := a.(*ast.CallExpr)
					if !ok || x.Ellipsis == token.NoPos {
						continue
					}
					if id, ok := inner.Fun.(*ast.Ident); ok && id.Name == gitRevParentHome {
						fromHome = true
					}
				}
				if words[gitRevParentVerb] && words[gitRevParentObject] {
					literalAskers = append(literalAskers, coord)
				}
				if fromHome {
					if strings.HasSuffix(base, "_test.go") {
						probeAskers = append(probeAskers, coord)
					} else {
						prodAskers = append(prodAskers, coord)
					}
				}
			}
			return true
		})
	})

	// Предпосылка распознавателя: он обязан ВИДЕТЬ вызовы `gitenv.Command`.
	// Ноль вызовов означает, что разбор ослеп либо пакет переехал, — и тогда
	// «литеральных спрашивающих ноль» есть «мы не смотрели», а не находка.
	if commandCalls == 0 {
		t.Fatalf("обход пакета (%s) не нашёл НИ ОДНОГО вызова `gitenv.Command` — "+
			"распознаватель слеп; молчание здесь означало бы «не смотрели»", census)
	}

	t.Logf("перепись: %s · вызовов `gitenv.Command` %d · "+
		"домов `%s` %d %v · спрашивающих литералами %q+%q %d %v · спрашивающих ИЗ "+
		"ДОМА: боевых %d %v, пробных %d %v",
		census, commandCalls, gitRevParentHome, len(homes), homes,
		gitRevParentVerb, gitRevParentObject, len(literalAskers), literalAskers,
		len(prodAskers), prodAskers, len(probeAskers), probeAskers)

	switch len(homes) {
	case 1: // единственный дом — то, ради чего всё
	case 0:
		t.Errorf("объявления `%s` в пакете НЕТ: вопрос о родителе HEAD задаётся каждой "+
			"стороной своими руками, и разойтись они могут молча — ровно это измерено "+
			"инъекцией (см. шапку файла)", gitRevParentHome)
	default:
		t.Errorf("объявлений `%s` в пакете %d (%v) — у одного вопроса два дома, и "+
			"сойтись им нечем", gitRevParentHome, len(homes), homes)
	}

	if len(literalAskers) > 0 {
		t.Errorf("вопрос о родителе задан ЛИТЕРАЛАМИ %q+%q мимо общего дома `%s` — "+
			"координаты %v; это вторая запись того же вопроса, и разойтись с первой "+
			"она может молча", gitRevParentVerb, gitRevParentObject, gitRevParentHome,
			literalAskers)
	}

	if len(prodAskers) == 0 {
		t.Errorf("НИ ОДИН боевой вызов `gitenv.Command` не спрашивает из дома `%s` — "+
			"проба сверялась бы сама с собой, а боевой путь спрашивал бы своё",
			gitRevParentHome)
	}
	if len(probeAskers) == 0 {
		t.Errorf("НИ ОДНА проба не спрашивает `gitenv.Command` из дома `%s` — фикстура "+
			"задаёт СВОЙ вопрос, и её исход ничего не говорит о боевом пути: код 1 "+
			"приходит и от другой подкоманды, и от другого репозитория",
			gitRevParentHome)
	}
}
