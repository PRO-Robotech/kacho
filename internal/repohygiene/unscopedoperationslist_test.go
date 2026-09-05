// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// unscopedoperationslist_test.go — гейт против несуженного перечисления
// операций на tenant-facing поверхности.
//
// Предмет. Строка операции несёт (а) полный ресурс в Response — тот же message,
// что отдаёт Get, и (б) личность инициатора, включая снимок отображаемого имени.
// Поэтому «показать список операций ресурса» без предиката владения показывает
// вызывающему содержимое и людей, к которым он отношения не имеет. Правильная
// точка входа одна — operations.ListForCaller; несуженный operations.Repo.List
// остаётся законным только на административном/внутреннем ярусе, где
// вызывающий авторизован иначе и аудит чужих действий и есть предмет RPC.
//
// Почему гейт, а не удаление метода. Несуженный путь нужен по существу (см.
// adminTierUnscopedList ниже), поэтому запрет выражается поимённым списком с
// обоснованием — и список обязан истекать сам: запись, которой больше нечего
// исключать, здесь считается находкой.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: упоминание
// operations.ListFilter в комментарии или в строковом литерале под запрет не
// попадает (в services/nlb такой комментарий есть, и текстовый поиск давал бы на
// нём ложное срабатывание).
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
	"testing"
)

// adminTierUnscopedList — места, которым несуженное перечисление РАЗРЕШЕНО,
// потому что их ярус — не тенантский.
//
// Каждая запись обязана нести обоснование «почему этот ярус», а не «так
// исторически»: молчаливое освобождение неотличимо от пропущенного места.
var adminTierUnscopedList = map[string]string{
	"services/iam/internal/apps/kaname/api/account/list_all_operations.go": "" +
		"ярус администратора аккаунта: гейт пропускает только кластерного администратора, " +
		"владельца аккаунта и делегированного администратора аккаунта. Аудит чужих действий " +
		"внутри своей тенантности — предмет этого RPC, а не побочный эффект.",
	"services/iam/internal/apps/kaname/api/internal_operations/list_iam_operations.go": "" +
		"внутренний ярус: system_admin @ cluster, RPC живёт только на internal-листенере. " +
		"Ровно тот случай, который godoc operations.Repo.List называет законным — доверенный " +
		"внутренний вызывающий, авторизованный иначе.",
}

// scanRoots — где ищем. Тесты и фейки исключены намеренно: их предмет — сам
// репозиторий, и несуженный путь они обязаны уметь звать, чтобы отличать его от
// суженного.
var scanRoots = []string{"services", "gateway", "pkg"}

// TestNoUnscopedOperationsListOutsideAdminTier — ни одно место вне списка
// исключений не зовёт несуженное перечисление операций.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. место tenant-facing -> перевести на operations.ListForCaller (ключ
//     владения из ctx, предикат внутрь SQL WHERE, отказ при отсутствии ключа);
//  2. место административного/внутреннего яруса -> запись в
//     adminTierUnscopedList С ОБОСНОВАНИЕМ «почему этот ярус» + такой же
//     комментарий рядом с кодом;
//  3. это не список операций, а совпадение по имени метода -> уточнить
//     распознавание ниже, а не расширять список исключений.
//
// Проверено инъекцией в обе стороны: возврат любого места на u.opsRepo.List
// красит гейт и печатает координату; законные два места яруса он пропускает
// молча.
func TestNoUnscopedOperationsListOutsideAdminTier(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	scanned := 0
	forEachProductionGoFile(t, root, func(rel string, body []byte) {
		scanned++
		if _, exempt := adminTierUnscopedList[rel]; exempt {
			return
		}
		for _, line := range unscopedListCalls(t, rel, body) {
			hits = append(hits, rel+":"+strconv.Itoa(line))
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой обход
	// (переименовали каталог, сломали фильтр) выглядел бы как зелёный гейт.
	if scanned == 0 {
		t.Fatalf("гейт не прочитал ни одного файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", scanRoots)
	}
	t.Logf("осмотрено прод-файлов: %d; освобождений: %d", scanned, len(adminTierUnscopedList))

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Errorf("найдено %d несуженных перечислений операций вне административного яруса:\n  %s\n\n"+
			"Строка операции несёт полный ресурс в Response и личность инициатора, поэтому "+
			"перечисление без предиката владения показывает вызывающему чужое содержимое и чужих "+
			"людей.\n\nИсходы: tenant-facing -> operations.ListForCaller / административный или "+
			"внутренний ярус -> запись в adminTierUnscopedList с обоснованием «почему этот ярус» / "+
			"совпадение по имени метода -> уточнить распознавание, а не список исключений.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestAdminTierExemptionsStillHaveSubject — исключение обязано умереть вместе со
// своим предметом.
//
// Освобождённый файл, из которого несуженный вызов уже убрали, — тихая дыра:
// туда можно вернуть его незамеченным. Поэтому пустое исключение здесь считается
// ошибкой, а не «просто больше не нужно».
func TestAdminTierExemptionsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)

	for rel, why := range adminTierUnscopedList {
		if strings.TrimSpace(why) == "" {
			t.Errorf("исключение %s без обоснования: запись обязана называть ЯРУС и почему он "+
				"не тенантский, иначе она неотличима от пропущенного места", rel)
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("исключение %s: файл не читается (%v) — запись устарела, удали её из "+
				"adminTierUnscopedList", rel, err)
			continue
		}
		if len(unscopedListCalls(t, rel, body)) == 0 {
			t.Errorf("исключение %s больше не нужно: несуженного перечисления в файле нет. "+
				"Удали запись — иначе файл останется вне гейта и туда можно будет вернуть "+
				"несуженный вызов незамеченным.", rel)
		}
	}
}

// TestNarrowedEntrypointPremiseHolds — запрет опирается на факт, который может
// перестать быть верным, и тогда сам запрет станет вредным.
//
// Гейт переводит места на operations.ListForCaller. Если эта точка входа
// исчезнет или сменит имя, требование станет невыполнимым, а гейт продолжит
// требовать своё. Поэтому предпосылка проверяется отдельно — по факту наличия
// объявления в дереве, а не по памяти автора.
func TestNarrowedEntrypointPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "pkg/operations/list_for_caller.go"))
	if err != nil {
		t.Fatalf("суженной точки входа нет на месте (%v): гейту некуда переводить места, "+
			"его требование стало невыполнимым — пересмотри запрет", err)
	}
	if !strings.Contains(string(body), "func ListForCaller(") {
		t.Fatalf("pkg/operations/list_for_caller.go больше не объявляет ListForCaller — " +
			"переводить некуда, пересмотри запрет")
	}
}

// unscopedListCalls возвращает номера строк, где вызывается метод List с
// аргументом-литералом operations.ListFilter, — то есть несуженное перечисление
// операций.
//
// Распознавание намеренно узкое: `<что-то>.List(ctx, operations.ListFilter{…})`.
// Вызов ListForCaller под него не попадает (там ListFilter — третий аргумент
// функции пакета, а не приёмника List). Вынесение литерала в переменную гейт не
// увидит — это floor, а не ceiling: молчание гейта не доказывает отсутствие
// несуженного пути, оно лишь ловит форму, которой этот путь пишется во всём
// дереве.
func unscopedListCalls(t *testing.T, rel string, body []byte) []int {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}

	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "List" {
			return true
		}
		for _, arg := range call.Args {
			if isOperationsListFilterLiteral(arg) {
				lines = append(lines, fset.Position(call.Pos()).Line)
				break
			}
		}
		return true
	})
	return lines
}

// isOperationsListFilterLiteral — является ли выражение композитным литералом
// operations.ListFilter (в т.ч. взятым по адресу).
func isOperationsListFilterLiteral(expr ast.Expr) bool {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "operations" && sel.Sel.Name == "ListFilter"
}

// forEachProductionGoFile обходит прод-дерево сервисов и шлюза.
//
// Тесты, моки и сгенерированные stub'ы исключены: предмет фейка — сам
// репозиторий, и несуженный путь он обязан уметь звать, чтобы тест мог отличить
// его от суженного. Запрет адресован прод-коду.
func forEachProductionGoFile(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range scanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("каталог %s не найден (%v) — область обхода гейта сломана", sub, err)
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				switch info.Name() {
				case ".git", "node_modules", "vendor", "docs", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if strings.HasPrefix(rel, "pkg/api/") || strings.Contains(rel, "mock") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			fn(rel, body)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}
}
