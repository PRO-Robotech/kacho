// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// foundationboundary_test.go — гейты над ДЕРЕВОМ по трём осям границы
// фундамента (приёмка K3-1). Предмет и обе стороны каждой оси разобраны на
// foundationboundary.go; здесь — только добыча входа.
//
// Способность падать доказывает не этот прогон, а инъекция
// (foundationboundary_injection_test.go).
//
// # Импорты читаются РАЗБОРОМ, а не поиском по образцу
//
// Путь импорта встречается в комментариях этого дерева десятками (и в шапке
// самого этого файла тоже), поэтому гейт по подстроке краснел бы на
// собственном объяснении. `go/build` читает объявление импорта как узел и,
// сверх того, учитывает ограничения сборки — то есть отвечает тем же составом
// файлов, что и `go list`.

const (
	kachoModule  = "github.com/PRO-Robotech/kacho"
	kanameModule = "github.com/PRO-Robotech/kaname"
)

// packageImports — импорты одного каталога, разделённые на прод и пробы.
//
// Счёт ведётся в ФАЙЛАХ, а не в записях списка импортов: `go/build` отдаёт
// список пакета уже дедуплицированным, поэтому восемнадцать файлов с одним и
// тем же импортом дали бы там единицу — единица счёта, на которой рост долга
// невидим.
type packageImports struct {
	Dir   string
	Prod  map[string]int
	Test  map[string]int
	Files int
}

// readTreePackages — все каталоги дерева с файлами Go и их импорты.
//
// Пустой обход — отказ: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», и `treecorpus` возвращает ошибку вместо пустого среза
// by construction.
func readTreePackages(t *testing.T, root string) ([]packageImports, int) {
	t.Helper()

	tracked, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	if len(tracked) == 0 {
		t.Fatal("под корнем нет ни одного отслеживаемого файла Go — обход пуст")
	}

	// Отслеживаемые файлы собираются МНОЖЕСТВОМ, а не только каталогами: состав
	// файлов берётся из индекса git, а не с диска. Иначе вердикт стал бы
	// свойством рабочего каталога — неотслеживаемый черновик рядом с пакетом
	// краснил бы гейт, а вердикт перестал бы относиться к коммиту.
	dirs := map[string]struct{}{}
	trackedSet := map[string]struct{}{}
	for _, f := range tracked {
		dirs[filepath.Dir(f)] = struct{}{}
		trackedSet[f] = struct{}{}
	}

	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	sort.Strings(ordered)

	out := make([]packageImports, 0, len(ordered))
	files := 0
	for _, abs := range ordered {
		pkg, err := build.ImportDir(abs, 0)
		if err != nil {
			// Каталог без пригодных файлов Go под текущими ограничениями сборки —
			// не находка: разбирать в нём нечего.
			if _, ok := err.(*build.NoGoError); ok {
				continue
			}
			if _, ok := err.(*build.MultiplePackageError); !ok {
				t.Fatalf("разбираю импорты %s: %v", abs, err)
			}
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, rerr)
		}
		prodNames := keepTracked(abs, pkg.GoFiles, trackedSet)
		testNames := keepTracked(abs,
			append(append([]string{}, pkg.TestGoFiles...), pkg.XTestGoFiles...), trackedSet)
		if len(prodNames)+len(testNames) == 0 {
			continue
		}
		prod := importsByFile(t, abs, prodNames)
		test := importsByFile(t, abs, testNames)
		n := len(prodNames) + len(testNames)
		files += n
		out = append(out, packageImports{Dir: filepath.ToSlash(rel), Prod: prod, Test: test, Files: n})
	}
	if len(out) == 0 {
		t.Fatal("ни одного каталога с файлами Go не разобрано — обход пуст")
	}
	return out, files
}

// keepTracked оставляет из имён файлов каталога только отслеживаемые git.
//
// `go/build` отдаёт состав по ограничениям сборки, но берёт его С ДИСКА;
// пересечение с индексом делает вход гейта свойством коммита.
func keepTracked(dir string, names []string, tracked map[string]struct{}) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := tracked[filepath.Join(dir, name)]; ok {
			out = append(out, name)
		}
	}
	return out
}

// importsByFile — сколько ФАЙЛОВ каталога несут каждый путь импорта.
//
// Импорт читается узлом синтаксического дерева (`parser.ImportsOnly`): путь
// импорта встречается в этом дереве и в комментариях, и в строковых литералах
// проб, поэтому поиск по образцу считал бы собственные объяснения.
func importsByFile(t *testing.T, dir string, names []string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, name := range names {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("разбираю импорты %s: %v", filepath.Join(dir, name), err)
		}
		seen := map[string]bool{}
		for _, spec := range f.Imports {
			path, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil || seen[path] {
				continue
			}
			seen[path] = true
			out[path]++
		}
	}
	return out
}

// treePathOfImport переводит путь импорта в путь от корня дерева. Второе
// значение — false для всего, что лежит вне обоих модулей продукта.
func treePathOfImport(imp string) (string, bool) {
	switch {
	case imp == kanameModule:
		return "services/iam", true
	case strings.HasPrefix(imp, kanameModule+"/"):
		return "services/iam/" + strings.TrimPrefix(imp, kanameModule+"/"), true
	case imp == kachoModule:
		return "", false
	case strings.HasPrefix(imp, kachoModule+"/"):
		return strings.TrimPrefix(imp, kachoModule+"/"), true
	}
	return "", false
}

// TestEveryFoundationCatalogDeclaresItsClass — ось ПЕРВАЯ (K3-01, K3-02, K3-03).
//
// Сверка двусторонняя: каталог без записи и запись без каталога — обе находки.
func TestEveryFoundationCatalogDeclaresItsClass(t *testing.T) {
	root := repoRoot(t)

	tracked, err := treecorpus.Under(filepath.Join(root, "pkg"))
	if err != nil {
		t.Fatalf("состав pkg/: %v", err)
	}
	seen := map[string]struct{}{}
	for _, f := range tracked {
		rel, rerr := filepath.Rel(filepath.Join(root, "pkg"), f)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", f, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > 1 {
			seen[parts[0]] = struct{}{}
		}
	}
	inTree := make([]string, 0, len(seen))
	for d := range seen {
		inTree = append(inTree, d)
	}
	sort.Strings(inTree)

	faults, census := judgeFoundationCatalogs(inTree, foundationClasses)
	t.Logf("перепись: %s", census.CatalogSummary())

	if len(faults) > 0 {
		t.Fatalf("класс каталога фундамента разошёлся с деревом (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// TestNoModuleEdgeRunsAgainstTheTargetLayout — ось ВТОРАЯ (K3-05, K3-08, K3-17, K3-19).
//
// Обход идёт по дереву ЦЕЛИКОМ, а не по `pkg/`: предикат, сужённый до `pkg/`,
// отвечает одинаково на «ребро снято» и «ребро переехало в службу» — то есть
// не различает сделанное и перенесённое (приёмка §7.4).
func TestNoModuleEdgeRunsAgainstTheTargetLayout(t *testing.T) {
	root := repoRoot(t)
	pkgs, files := readTreePackages(t, root)

	type counts struct{ prod, test int }
	tally := map[[2]string]*counts{}
	imports := 0

	for _, p := range pkgs {
		fromClass, ok := classOfPackage(p.Dir)
		if !ok {
			// Каталог без объявленного класса ловит ось первая; здесь он не
			// становится молчаливым пропуском, но и не удваивает находку.
			continue
		}
		add := func(byImport map[string]int, isTest bool) {
			for imp, files := range byImport {
				rel, in := treePathOfImport(imp)
				if !in {
					continue
				}
				imports += files
				toClass, known := classOfPackage(rel)
				if !known || !forbiddenDirections[[2]foundationClass{fromClass, toClass}] {
					continue
				}
				key := [2]string{p.Dir, rel}
				if tally[key] == nil {
					tally[key] = &counts{}
				}
				if isTest {
					tally[key].test += files
				} else {
					tally[key].prod += files
				}
			}
		}
		add(p.Prod, false)
		add(p.Test, true)
	}

	observed := make([]boundaryEdge, 0, len(tally))
	for k, c := range tally {
		fromClass, _ := classOfPackage(k[0])
		toClass, _ := classOfPackage(k[1])
		observed = append(observed, boundaryEdge{
			From: k[0], To: k[1], FromClass: fromClass, ToClass: toClass,
			Prod: c.prod, Test: c.test,
		})
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].key() < observed[j].key() })

	faults, census := judgeBoundaryEdges(observed, knownBoundaryEdges, files, imports)
	t.Logf("перепись: %s", census.EdgeSummary())

	if len(faults) > 0 {
		t.Fatalf("направление рёбер разошлось с целевой раскладкой (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// TestShippedBinariesDoNotExecuteBuildToolchain — ось ТРЕТЬЯ (K3-13, K3-14, F5).
//
// Замыкание берётся ОТ ГЛАВНЫХ ПАКЕТОВ поставляемых двоичных, а не от каталога:
// пакет, который никто не импортирует, в поставку не попадает, и судить его по
// месту в дереве значило бы судить не то.
//
// Генераторы `gateway/cmd/protoc-gen-*` исключены намеренно: они сами оснастка
// сборки, и включив их, мы получили бы замыкание, доказывающее свою же посылку.
func TestShippedBinariesDoNotExecuteBuildToolchain(t *testing.T) {
	root := repoRoot(t)
	pkgs, _ := readTreePackages(t, root)

	prodImports := map[string][]string{}
	for _, p := range pkgs {
		for imp := range p.Prod {
			if rel, in := treePathOfImport(imp); in {
				prodImports[p.Dir] = append(prodImports[p.Dir], rel)
			}
		}
	}

	var mains []string
	for _, p := range pkgs {
		if !isShippedBinaryDir(p.Dir) {
			continue
		}
		mains = append(mains, p.Dir)
	}
	sort.Strings(mains)

	reachedDirs := map[string]struct{}{}
	queue := append([]string{}, mains...)
	for len(queue) > 0 {
		n := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, m := range prodImports[n] {
			if _, ok := reachedDirs[m]; ok {
				continue
			}
			reachedDirs[m] = struct{}{}
			queue = append(queue, m)
		}
	}

	reached := map[string]foundationClass{}
	for d := range reachedDirs {
		if !strings.HasPrefix(d, "pkg/") {
			continue
		}
		parts := strings.Split(d, "/")
		if len(parts) < 2 {
			continue
		}
		if cls, ok := foundationClasses[parts[1]]; ok {
			reached[parts[1]] = cls
		}
	}

	faults, census := judgeShippedToolchain(reached, knownShippedToolchain, len(mains))
	t.Logf("перепись: %s", census.ClosureSummary())

	if len(faults) > 0 {
		t.Fatalf("оснастка сборки исполняется в поставляемом процессе (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
}

// isShippedBinaryDir — каталог главного пакета ПОСТАВЛЯЕМОГО двоичного:
// `gateway/cmd/<имя>` и `services/<служба>/cmd/<имя>`, кроме генераторов.
func isShippedBinaryDir(dir string) bool {
	if strings.Contains(dir, "protoc-gen") {
		return false
	}
	parts := strings.Split(dir, "/")
	switch {
	case len(parts) == 3 && parts[0] == "gateway" && parts[1] == "cmd":
		return true
	case len(parts) == 4 && parts[0] == "services" && parts[2] == "cmd":
		return true
	}
	return false
}
