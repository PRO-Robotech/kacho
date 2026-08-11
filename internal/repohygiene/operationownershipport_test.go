// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operationownershipport_test.go — гейт на КЛАСС: отмена операции идёт
// ownership-scoped портом, а не «прочитал → сравнил → отменил».
//
// Предмет. `operation_id` опакен, но это прямой объект-референс: узнав чужой id,
// вызывающий отменил бы чужую in-flight мутацию. Право на это решается
// предикатом владения, и место предиката принципиально: внутри того же
// оператора, что выполняет отмену (CancelOwned → `UPDATE … WHERE id=$1 AND
// done=false AND <владение> RETURNING …`). Отдельная программная проверка перед
// несуженной мутацией — форма, запрещённая как within-service инвариант
// «check-then-act», и она к тому же бывает открыта при отказе: неудачное чтение
// это «не знаю», а не «разрешено».
//
// Почему гейт, а не разовый фикс. Ровно этот класс уже закрывали по одному
// сервису за раз, и переживший экземпляр находился раундом позже. Семь
// реализаций OperationService живут в семи деревьях; требование обязано
// проверяться механически, иначе восьмая приедет несуженной.
//
// Разбор идёт по синтаксическому дереву: упоминание CancelOwned в комментарии
// (а такие есть в каждом из семи файлов) не считается за вызов, и наоборот —
// закомментированная защита не спасает от находки.
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

// unscopedCancelExempt — пакеты, которым несуженная отмена РАЗРЕШЕНА, с
// обоснованием «почему здесь предикат владения живёт не в мутации».
//
// Запись обязана называть причину, а не факт: молчаливое освобождение
// неотличимо от пропущенного места.
var unscopedCancelExempt = map[string]string{
	"services/nlb/internal/apps/kacho/api/operation": "" +
		"осознанное расхождение контракта: nlb намеренно держит НЕидемпотентную отмену " +
		"(повторная отмена уже отменённой → FAILED_PRECONDITION), а corelib CancelOwned " +
		"идемпотентен на уже-CANCELLED. Поэтому владение решается GetOwned'ом, и его ошибка " +
		"ВОЗВРАЩАЕТСЯ (fail-closed) до несуженной отмены — открытой при отказе эта проверка " +
		"не является. Колонки принципала неизменяемы, поэтому решение о владении не устаревает " +
		"между двумя операторами. Запись снимается, если nlb примет идемпотентную семантику.",
	"gateway/internal/opsproxy": "" +
		"край, а не хендлер поверх репозитория: собственной таблицы операций у него нет, " +
		"он проксирует владельцу. Авторитет владения — предикат в SQL владельца; проверка на " +
		"краю это второй слой, и она выполняется ДО отправки мутации (read → check → mutate).",
}

// operationServiceRoots — где ищем реализации.
var operationServiceRoots = []string{"services", "gateway", "pkg"}

// TestOperationCancelGoesThroughOwnershipScopedPort — ни одна реализация
// OperationService вне списка исключений не мутирует операцию несуженным
// вызовом.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. это tenant-facing отмена -> перевести на operations.AsOwned + CancelOwned
//     (предикат владения внутрь того же UPDATE, отказ при отсутствии owner-ключа);
//  2. семантика отмены сознательно отличается от corelib -> запись в
//     unscopedCancelExempt С ОБОСНОВАНИЕМ и такой же комментарий рядом с кодом,
//     причём ошибка ownership-чтения обязана ВОЗВРАЩАТЬСЯ, а не пропускаться;
//  3. это не отмена операции, а совпадение по имени метода -> уточнить
//     распознавание ниже, а не расширять список исключений.
//
// Проверено инъекцией в обе стороны: возврат iam-хендлера на h.repo.Cancel
// красит гейт и печатает координату; шесть законных реализаций
// (vpc/compute/geo/registry/storage + сам corelib) он пропускает молча.
func TestOperationCancelGoesThroughOwnershipScopedPort(t *testing.T) {
	root := repoRoot(t)
	impls := operationServiceImplPackages(t, root)

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if len(impls) == 0 {
		t.Fatalf("гейт не нашёл ни одной реализации OperationService в %v — предпосылка "+
			"обхода сломана, молчание ничего не доказывает", operationServiceRoots)
	}
	dirs := make([]string, 0, len(impls))
	for dir := range impls {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	t.Logf("реализаций OperationService найдено: %d\n  %s", len(dirs), strings.Join(dirs, "\n  "))

	var hits []string
	for _, dir := range dirs {
		if _, exempt := unscopedCancelExempt[dir]; exempt {
			continue
		}
		hasScoped := false
		for _, rel := range packageProductionFiles(t, root, dir) {
			body := mustRead(t, filepath.Join(root, rel))
			calls := methodCallsOnFieldSelector(t, rel, body)
			if calls.scopedCancel || calls.scopedGet {
				hasScoped = true
			}
			for _, line := range calls.unscopedCancelLines {
				hits = append(hits, rel+":"+strconv.Itoa(line)+" — несуженная отмена операции")
			}
		}
		if !hasScoped {
			hits = append(hits, dir+" — реализация OperationService без единого вызова "+
				"GetOwned/CancelOwned: предиката владения нет вовсе")
		}
	}

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Errorf("найдено %d мест, где отмена операции не защищена предикатом владения в самой мутации:\n  %s\n\n"+
			"operation_id — прямой объект-референс: несуженная отмена позволяет прекратить чужую "+
			"in-flight мутацию.\n\nИсходы: tenant-facing -> operations.AsOwned + CancelOwned / "+
			"сознательно иная семантика -> запись в unscopedCancelExempt с обоснованием / "+
			"совпадение по имени метода -> уточнить распознавание, а не список исключений.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestUnscopedCancelExemptionsStillHaveSubject — исключение живёт, пока у него
// есть предмет. Запись, которой больше нечего исключать, унаследует следующую
// слепую зону, поэтому здесь она считается находкой.
func TestUnscopedCancelExemptionsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	impls := operationServiceImplPackages(t, root)

	for dir, why := range unscopedCancelExempt {
		if strings.TrimSpace(why) == "" {
			t.Errorf("исключение %s без обоснования: запись обязана называть ПРИЧИНУ, иначе она "+
				"неотличима от пропущенного места", dir)
			continue
		}
		if !impls[dir] {
			t.Errorf("исключение %s больше не реализует OperationService — запись устарела, удали её",
				dir)
			continue
		}
		subject := false
		for _, rel := range packageProductionFiles(t, root, dir) {
			calls := methodCallsOnFieldSelector(t, rel, mustRead(t, filepath.Join(root, rel)))
			if len(calls.unscopedCancelLines) > 0 || !calls.scopedCancel {
				subject = true
			}
		}
		if !subject {
			t.Errorf("исключение %s больше не нужно: пакет уже отменяет операцию суженным портом. "+
				"Удали запись — иначе пакет останется вне гейта и туда можно будет вернуть "+
				"несуженную отмену незамеченной.", dir)
		}
	}

	// Перепись: пустой список законен, но обязан быть отличим от непрочитанного.
	t.Logf("перепись: записей исключений рассмотрено %d", len(unscopedCancelExempt))
}

// TestOwnershipScopedPortPremiseHolds — запрет опирается на факт, который может
// перестать быть верным: наличие суженного порта в corelib. Если он исчезнет
// или сменит имя, требование станет невыполнимым, а гейт продолжит требовать
// своё.
func TestOwnershipScopedPortPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	body := string(mustRead(t, filepath.Join(root, "pkg/operations/owner.go")))
	for _, want := range []string{"func AsOwned(", "CancelOwned(ctx context.Context", "GetOwned(ctx context.Context"} {
		if !strings.Contains(body, want) {
			t.Fatalf("pkg/operations/owner.go больше не объявляет %q — переводить некуда, "+
				"пересмотри запрет", want)
		}
	}
}

// operationServiceImplPackages — каталоги прод-пакетов, объявляющих тип, который
// встраивает UnimplementedOperationServiceServer. Сгенерированные stub'ы
// (pkg/api/…) и тесты исключены: там этот тип объявляется и мокается по существу.
func operationServiceImplPackages(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, sub := range operationServiceRoots {
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
			if declaresOperationServiceImpl(t, rel, mustRead(t, path)) {
				found[filepath.ToSlash(filepath.Dir(rel))] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}
	return found
}

// declaresOperationServiceImpl — объявляет ли файл структуру, встраивающую
// UnimplementedOperationServiceServer.
func declaresOperationServiceImpl(t *testing.T, rel string, body []byte) bool {
	t.Helper()
	file := parseGo(t, rel, body)
	impl := false
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			if len(f.Names) != 0 {
				continue // именованное поле — не встраивание
			}
			sel, ok := f.Type.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "UnimplementedOperationServiceServer" {
				impl = true
			}
		}
		return true
	})
	return impl
}

// repoCalls — что найдено в одном файле.
type repoCalls struct {
	// unscopedCancelLines — строки вида `<recv>.<field>.Cancel(…)`: отмена без
	// предиката владения. Приёмник — именно поле структуры (SelectorExpr), а не
	// локальная переменная: `client.Cancel` у прокси и `cancel()` от
	// context.WithTimeout под запрет не попадают.
	unscopedCancelLines []int
	scopedCancel        bool
	scopedGet           bool
}

func methodCallsOnFieldSelector(t *testing.T, rel string, body []byte) repoCalls {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}

	var out repoCalls
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "CancelOwned":
			out.scopedCancel = true
		case "GetOwned":
			out.scopedGet = true
		case "Cancel":
			// Только вызов на поле структуры: `u.repo.Cancel(ctx, id)`.
			if _, isField := sel.X.(*ast.SelectorExpr); isField {
				out.unscopedCancelLines = append(out.unscopedCancelLines, fset.Position(call.Pos()).Line)
			}
		}
		return true
	})
	return out
}

// packageProductionFiles — прод-файлы пакета (без тестов), rel-пути от корня.
func packageProductionFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatalf("чтение пакета %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(dir, e.Name())))
	}
	sort.Strings(out)
	return out
}

func parseGo(t *testing.T, rel string, body []byte) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	return file
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return b
}
