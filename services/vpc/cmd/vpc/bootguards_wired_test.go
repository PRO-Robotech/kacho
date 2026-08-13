// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// bootguards_wired_test.go — гейт на КЛАСС: страж старта, которого никто не зовёт.
//
// # Предмет
//
// Страж старта — это ветка, которая ничего не делает, пока её не позвал
// композиционный корень. Написанный, покрытый пробами и НЕ позванный страж
// выглядит работающим с любой стороны, кроме одной: он не отказал ни разу за всю
// свою жизнь. Собственные пробы стража этого не замечают — они зовут его напрямую,
// со своими значениями.
//
// # Как устроен
//
// Перечень стражей ВЫВОДИТСЯ из дерева, а не выписывается: разбор AST пакета
// настроек ищет методы `func (c Config) Validate…`. Выписанный перечень отстал бы
// от следующего стража — то есть ровно от того, ради которого гейт и нужен.
//
// # Агрегатор и его САМОИСТЕКАЮЩЕЕ послабление
//
// `ValidateBoot` композиционный корень намеренно НЕ зовёт: он зовёт составные
// проверки поимённо, чтобы отказ называл свою причину. Поэтому агрегатор из
// требования «будь позван» исключён — но взамен обязан НАЗЫВАТЬ КАЖДОГО стража.
// Послабление истекает само: стоит агрегатору забыть проверку, и гейт покраснеет
// именно на нём, а не промолчит. Это не вкусовщина — доку `ValidateBoot` сам
// называет себя ловушкой: он выглядит как «полная проверка старта», и тот, кто
// переведёт корень на него, тихо останется без забытой проверки.
//
// # Предпосылка проверяется
//
// Ноль найденных стражей — находка, а не «всё чисто»: значит разбор смотрит не
// туда, и молчание гейта ничего не доказывает.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	configPkgDir = "../../internal/apps/kacho/config"
	// aggregator — единственный страж, освобождённый от требования «будь позван»;
	// взамен он обязан назвать всех остальных.
	aggregator = "ValidateBoot"
)

// bootGuards — имена стражей и тело агрегатора, прочитанные из пакета настроек.
func bootGuards(t *testing.T) (names []string, aggregatorBody string) {
	t.Helper()
	entries, err := os.ReadDir(configPkgDir)
	require.NoError(t, err, "пакет настроек обязан быть там, где его ищет гейт")

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(configPkgDir, e.Name())
		f, perr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, perr,
			"%s не разобран — гейт не вправе трактовать неразобранный файл как «стражей нет»", path)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !strings.HasPrefix(fn.Name.Name, "Validate") || !fn.Name.IsExported() {
				continue
			}
			// Получатель обязан быть Config: у соседних типов свои методы, и
			// требовать их вызова из корня было бы подменой предмета.
			id, ok := fn.Recv.List[0].Type.(*ast.Ident)
			if !ok || id.Name != "Config" {
				continue
			}
			names = append(names, fn.Name.Name)
			if fn.Name.Name == aggregator {
				var called []string
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						called = append(called, sel.Sel.Name)
					}
					return true
				})
				aggregatorBody = strings.Join(called, " ")
			}
		}
	}
	sort.Strings(names)
	return names, aggregatorBody
}

// TestEveryBootGuardIsCalledByTheCompositionRoot — ядро гейта.
func TestEveryBootGuardIsCalledByTheCompositionRoot(t *testing.T) {
	names, aggregatorBody := bootGuards(t)
	require.NotEmpty(t, names,
		"в пакете настроек не найдено ни одного стража старта — предпосылка гейта сломана, "+
			"его молчание ничего не доказывает (каталог=%s)", configPkgDir)

	// Что читаем как «композиционный корень»: весь прод-код этого пакета.
	root := compositionRootSource(t)

	var unwired []string
	for _, name := range names {
		if name == aggregator {
			continue
		}
		if !strings.Contains(root, name+"(") {
			unwired = append(unwired, name)
		}
	}
	require.Empty(t, unwired,
		"страж(и) старта %v объявлены в пакете настроек и НЕ позваны композиционным корнем: "+
			"такая проверка не отказывает ни разу за всю свою жизнь, оставаясь на вид работающей. "+
			"Позовите её в main() рядом с остальными — или снимите вместе с её пробами",
		unwired)

	require.NotEmpty(t, aggregatorBody,
		"%s не найден или его тело не прочитано — послаблению нечего проверять", aggregator)
	var missing []string
	for _, name := range names {
		if name == aggregator {
			continue
		}
		if !strings.Contains(aggregatorBody, name) {
			missing = append(missing, name)
		}
	}
	require.Empty(t, missing,
		"%s не называет страж(ей) %v. Послабление «агрегатор не обязан быть позван» держится "+
			"ровно на том, что он полон: агрегатор выглядит как «полная проверка старта», и тот, "+
			"кто переведёт на него корень, тихо останется без забытой проверки",
		aggregator, missing)

	t.Logf("перепись: стражей старта в пакете настроек %d (%s); из них агрегатор %q",
		len(names), strings.Join(names, ", "), aggregator)
}

// compositionRootSource — прод-исходники композиционного корня одной строкой.
func compositionRootSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var b strings.Builder
	read := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(e.Name())
		require.NoError(t, rerr)
		b.Write(body)
		b.WriteString("\n")
		read++
	}
	require.NotZero(t, read, "не прочитано ни одного файла композиционного корня")
	return b.String()
}
