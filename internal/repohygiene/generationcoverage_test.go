// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// generationcoverage_test.go — по дереву. Предмет, устройство проверки и
// границы — в шапке generationcoverage.go; здесь только обход и перепись.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestEveryContractRootIsNamedInGenerationInputs — каждый наш контракт назван
// во входах объявления генерации, и каждый объявленный путь имеет предмет.
func TestEveryContractRootIsNamedInGenerationInputs(t *testing.T) {
	root := repoRoot(t)

	modules := generationModulePaths(t, root)
	if len(modules) == 0 {
		t.Fatal("модулей дерева не прочитано ни одного — отделить наши контракты от " +
			"вендорных нечем, и проверка стала бы вакуумной")
	}

	out, err := gitenv.Command(root, "ls-files", "-z", "*buf.gen.yaml").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	var decls []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p != "" {
			decls = append(decls, p)
		}
	}
	if len(decls) == 0 {
		t.Fatal("объявлений генерации в дереве не найдено — обход сломан, а не дерево чисто")
	}

	total := generationCoverageCensus{}
	for _, decl := range decls {
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(decl)))
		if rerr != nil {
			t.Errorf("%s не прочитан: %v — объявление НЕ проверено", decl, rerr)
			continue
		}

		dir := filepath.Dir(decl)
		files := generationContractsUnder(t, root, dir, modules)

		findings, census := checkGenerationCoverage(decl, string(raw), files)
		total.Files += census.Files
		total.Ours += census.Ours
		total.Vendored += census.Vendored
		total.Roots += census.Roots
		total.RootsOurs += census.RootsOurs
		total.RootsCovered += census.RootsCovered
		total.Inputs += census.Inputs
		total.Judged += census.Judged

		if census.Ours == 0 {
			t.Errorf("%s: под каталогом объявления нет НИ ОДНОГО контракта, порождающего "+
				"в модули дерева (%s) — дискриминатор проглотил всё, и требование "+
				"полноты стало тождественно истинным", decl, strings.Join(modules, ", "))
		}
		for _, msg := range findings {
			t.Error(msg)
		}
	}

	if total.Files == 0 {
		t.Fatal("контрактов под объявлениями генерации не прочитано ни одного — " +
			"обход сломан")
	}
	t.Logf("модулей дерева: %d (%s); объявлений генерации: %d; входов в них: %d, "+
		"из них судится разбором: %d; контрактов прочитано: %d (наших %d, вендорных %d); "+
		"корней первого уровня: %d, из них наших: %d, покрыто входами: %d",
		len(modules), strings.Join(modules, ", "), len(decls), total.Inputs, total.Judged,
		total.Files, total.Ours, total.Vendored,
		total.Roots, total.RootsOurs, total.RootsCovered)
}

// generationContractsUnder — отслеживаемые контракты каталога с объявлением
// генерации, в координатах этого каталога.
func generationContractsUnder(t *testing.T, root, dir string, modules []string) []generationContractFile {
	t.Helper()
	abs, err := treecorpus.UnderWithSuffix(filepath.Join(root, filepath.FromSlash(dir)), ".proto")
	if err != nil {
		t.Fatalf("состав контрактов под %s: %v — «ноль находок» здесь означало бы "+
			"«ноль прочитанного»", dir, err)
	}
	base := filepath.Join(root, filepath.FromSlash(dir))
	out := make([]generationContractFile, 0, len(abs))
	for _, p := range abs {
		rel, rerr := filepath.Rel(base, p)
		if rerr != nil {
			t.Fatalf("путь %s не приведён к координатам %s: %v", p, dir, rerr)
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("%s не прочитан: %v — принадлежность контракта неизвестна, и "+
				"пропуск выдал бы её за «чужой»", p, rerr)
		}
		out = append(out, generationContractFile{
			Rel:  filepath.ToSlash(rel),
			Ours: generationBelongsToTree(string(body), modules),
		})
	}
	return out
}

// generationBelongsToTree — порождает ли контракт в ОДИН ИЗ модулей этого
// дерева.
//
// Модулей здесь больше одного, и это не подробность реализации, а причина, по
// которой множество ВЫВОДИТСЯ из дерева, а не выписывается. Прежняя редакция
// брала путь модуля из корневого go.mod — и объявила бы чужим всякий контракт,
// порождающий в модуль вынесенной службы, то есть смолчала бы ровно на том
// переезде домена в собственный корень, ради которого гейт и написан.
//
// Контракт без `go_package` считается нашим: молчание объявления не есть
// заявление о чужом происхождении, и обратное прочтение открыло бы дыру ровно
// в том виде, ради которого гейт написан.
func generationBelongsToTree(body string, modules []string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "option go_package")
		if !ok {
			continue
		}
		i := strings.IndexByte(rest, '"')
		if i < 0 {
			continue
		}
		rest = rest[i+1:]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			continue
		}
		pkg := rest[:j]
		if k := strings.IndexByte(pkg, ';'); k >= 0 {
			pkg = pkg[:k]
		}
		for _, m := range modules {
			if pkg == m || strings.HasPrefix(pkg, m+"/") {
				return true
			}
		}
		return false
	}
	return true
}

// generationModulePaths — пути ВСЕХ модулей дерева, отсортированные.
//
// Читаются ФАЙЛЫ, а не `go list`: множество наших контрактов нужно и тогда,
// когда дерево не собирается, а вердикт гейта не должен зависеть от того,
// собирается ли оно прямо сейчас. Состав выводится из индекса git — рукописный
// перечень разошёлся бы с деревом молча, а разошёлся бы он именно в тот день,
// когда служба получила собственный модуль.
func generationModulePaths(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "go.mod", "*/go.mod").Output()
	if err != nil {
		t.Fatalf("git ls-files go.mod: %v — множество модулей неизвестно, и "+
			"дискриминатор стал бы вакуумным", err)
	}
	var modules []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("%s не прочитан: %v — модуль НЕ учтён, и его контракты уехали "+
				"бы в «чужие»", rel, rerr)
		}
		for _, line := range strings.Split(string(body), "\n") {
			if m, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				modules = append(modules, strings.TrimSpace(m))
				break
			}
		}
	}
	sort.Strings(modules)
	return modules
}
