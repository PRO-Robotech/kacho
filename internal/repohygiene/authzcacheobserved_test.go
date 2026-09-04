// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzcacheobserved_test.go — процесс, отдавший входящий путь носителю контура,
// ОБЯЗАН выставлять долю попаданий своего кеша вердиктов.
//
// # Предмет
//
// Кеш положительных вердиктов строит один носитель на все сервисы
// (`pkg/servicehost.decisionLink`), а диагностическую поверхность держит
// композиционный корень каждого сервиса. Между ними — граница, которую величины
// переходят ТОЛЬКО если корень их провязал. Провязка наблюдаемости всюду
// nil-безопасна и необязательна по построению, поэтому её пропажу не поймает ни
// компилятор, ни проба самого кеша: он останется зелёным, считая в пустоту.
//
// Свойство «величина доходит до собирателя» — свойство ДЕРЕВА, и держать его
// может только обход дерева. Отсюда гейт, а не разовая правка шести корней:
// седьмой сервис, отдавший себя носителю и забывший коллектор, обязан краснеть
// в тот же день, а не через полгода при разборе «сколько даёт кеш».
//
// # Почему предикат — «отдал себя носителю», а не перечень сервисов
//
// Перечень — это место, которое забывают дополнить. Условие ВЫВОДИТСЯ из дерева:
// вызов `servicehost.Serve` означает, что процесс получил звено решения со своим
// кешем вердиктов, и другого способа получить его у сервиса нет — `Serve` не
// возвращает сервер, приделать своё звено не к чему.
//
// # Разбор синтаксического дерева, а не текста
//
// Оба имени встречаются в комментариях, объясняющих ровно эту провязку.
// Текстовый поиск принял бы объяснение за исполнение — тот самый класс, который
// гейт и ловит. Локальное имя пакета берётся из ОБЪЯВЛЕНИЯ импорта: псевдоним не
// должен становиться слепым пятном.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// observerPkg / observerFunc — как строится коллектор доли попаданий. Разъедется
// с кодом — перепись найдёт ноль наблюдателей, и гейт скажет об этом отдельной
// строкой, а не промолчит.
const (
	observerPkg  = "github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"
	observerFunc = "New"
)

// TestEveryCarrierServiceExportsItsVerdictCacheHitRate — у каждого сервиса,
// отдавшего входящий путь носителю, доля попаданий кеша вердиктов выставлена.
func TestEveryCarrierServiceExportsItsVerdictCacheHitRate(t *testing.T) {
	root := repoRoot(t)
	carriers, observers, scanned, err := scanVerdictCacheObservers(root)
	if err != nil {
		t.Fatalf("%v", err)
	}

	names := make([]string, 0, len(carriers))
	for svc := range carriers {
		names = append(names, svc)
	}
	sort.Strings(names)
	observed := 0
	for _, svc := range names {
		if observers[svc] != "" {
			observed++
		}
	}
	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено: файлов прочитано=%d, сервисов у носителя=%d (%s), выставляют долю попаданий=%d",
		scanned, len(carriers), strings.Join(names, ", "), observed)

	// Предпосылка: носителя кто-то зовёт. Ноль означает, что вызов переименован
	// либо сервисы съехали с носителя, — и тогда гейт судит пустоту.
	if len(carriers) == 0 {
		t.Fatalf("предпосылка гейта нарушена: ни один сервис не зовёт носитель контура — "+
			"вызов переименован либо сервисы больше не отдают ему входящий путь; "+
			"пока это не выяснено, гейт не судит ничего (файлов прочитано %d)", scanned)
	}

	for _, svc := range names {
		if observers[svc] != "" {
			continue
		}
		t.Errorf("services/%s отдаёт входящий путь носителю (%s), то есть получает звено решения "+
			"со СВОИМ кешем положительных вердиктов, но нигде не строит коллектор доли попаданий "+
			"(%s.%s) — величины остаются в процессе. Пока их нет, «кеш не попадает ни разу» снаружи "+
			"неотличимо от «кеш поглощает весь поток», и всякое утверждение о том, сколько даёт кеш, "+
			"непроверяемо в обе стороны",
			svc, carriers[svc], filepath.Base(observerPkg), observerFunc)
	}
}

// scanVerdictCacheObservers — обход дерева сервисов: кто зовёт носитель и кто
// строит коллектор доли попаданий.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
func scanVerdictCacheObservers(root string) (carriers, observers map[string]string, files int, err error) {
	dir := filepath.Join(root, "services")
	tracked, terr := treecorpus.Under(dir)
	if terr != nil {
		return nil, nil, 0, fmt.Errorf("состав дерева под %s не читается: %w", dir, terr)
	}
	carriers, observers = map[string]string{}, map[string]string{}
	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			return nil, nil, 0, fmt.Errorf("относительный путь для %s: %w", abs, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 || parts[0] != "services" {
			continue
		}
		svc := parts[1]
		files++
		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, nil, 0, fmt.Errorf("разбор %s: %w", rel, perr)
		}
		if callsCarrierServe(f) && carriers[svc] == "" {
			carriers[svc] = filepath.ToSlash(rel)
		}
		if callsPkgFunc(f, observerPkg, observerFunc) && observers[svc] == "" {
			observers[svc] = filepath.ToSlash(rel)
		}
	}
	return carriers, observers, files, nil
}

// callsPkgFunc — зовёт ли файл `<pkg>.<fn>`, где локальное имя пакета взято из
// объявления импорта этого файла.
func callsPkgFunc(f *ast.File, importPath, fn string) bool {
	local := ""
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || path != importPath {
			continue
		}
		local = filepath.Base(path)
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != fn {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
			found = true
		}
		return true
	})
	return found
}
