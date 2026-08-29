// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// migratorCLICorpusSuffixes — что читается: сборка, развёртывание и сами точки
// наката. Имя бинаря живёт только здесь.
var migratorCLICorpusSuffixes = []string{".go", ".yaml", ".yml", ".tpl", "Dockerfile", "Makefile"}

// migratorCLICorpus собирает корпус из индекса git по обоим корням.
func migratorCLICorpus(t *testing.T) (root string, paths []string) {
	t.Helper()
	root = repoRoot(t)
	for _, dir := range []string{"services", "deploy"} {
		found, err := treecorpus.UnderWithSuffix(filepath.Join(root, dir), migratorCLICorpusSuffixes...)
		if err != nil {
			t.Fatalf("корпус дерева (%s) не построен: %v", dir, err)
		}
		paths = append(paths, found...)
	}
	return root, paths
}

// TestMigratorBinaryIsNamedTheSameEverywhere — имя бинаря мигратора одно.
//
// До задачи #1461 nlb собирал и запускал его под именем `migrator`, остальные
// шесть — под `kacho-migrator`. Различие не решал никто; оператор, знающий
// шесть сервисов, на седьмом получал «executable file not found».
//
// Судятся ВСЕ места, где имя называется: путь установки, выход сборки,
// переменная сборки, константа в самой точке наката. Одного места мало —
// разойтись могут именно они между собой (в nlb расходились Dockerfile,
// Makefile и манифест согласованно, а с шестью соседями — нет).
func TestMigratorBinaryIsNamedTheSameEverywhere(t *testing.T) {
	root, paths := migratorCLICorpus(t)

	var (
		filesRead int
		mentions  []migratorCLIMention
	)
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: чтение не удалось: %v", p, err)
		}
		filesRead++
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		mentions = append(mentions, migratorCLIMentions(filepath.ToSlash(rel), string(raw))...)
	}

	names := map[string]int{}
	for _, m := range mentions {
		names[m.Name]++
	}
	t.Logf("перепись: файлов сборки и развёртывания прочитано %d, мест, называющих бинарь, %d, "+
		"различных имён %d", filesRead, len(mentions), len(names))

	if filesRead == 0 {
		t.Fatal("не прочитано ни одного файла — гейт ничего не осмотрел, и его молчание " +
			"неотличимо от исправности")
	}
	if len(mentions) == 0 {
		t.Fatalf("ни одно место не называет бинарь мигратора — предикат перестал их узнавать. "+
			"Прочитано файлов: %d", filesRead)
	}

	for _, f := range migratorCLINameFindings(mentions) {
		t.Errorf("%s", f)
	}
}

// TestMigratorArgumentParsingIsOneOfTwoAndDecidesExtraArguments — разбор
// аргументов признанный, и лишний позиционный аргумент решён, а не оставлен
// умолчанию.
//
// Признанных разборов два: общий пакет (прямая форма) и cobra (делегирующая).
// Третий — находка: именно так различие и накапливалось, каждый следующий
// сервис копировал ближайшего соседа наугад.
//
// Второе требование — про молчание. Cobra при `Args == nil` принимает
// произвольные позиционные аргументы, поэтому `up 800001` уезжал накатывать до
// головы. Гейт требует, чтобы вопрос был решён; КАК решён — дело команды
// (`NoArgs`, `ExactArgs`, своя проверка), и гейт об этом не судит.
func TestMigratorArgumentParsingIsOneOfTwoAndDecidesExtraArguments(t *testing.T) {
	root := repoRoot(t)
	paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	if err != nil {
		t.Fatalf("корпус дерева не построен: %v", err)
	}

	var (
		parsers          []migratorCLIParser
		shared, viaCobra int
		withRun, decided int
	)
	for _, p := range paths {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/cmd/migrator/") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("%s: чтение не удалось: %v", rel, rerr)
		}
		parsed, cerr := classifyMigratorCLIParser(rel, string(raw))
		if cerr != nil {
			t.Fatalf("%v", cerr)
		}
		parsers = append(parsers, parsed)
		if parsed.Shared {
			shared++
		}
		if parsed.Cobra {
			viaCobra++
		}
		withRun += parsed.CommandsWithRun
		decided += parsed.CommandsWithArgs
	}

	t.Logf("перепись: точек наката %d · на общем разборе %d · на cobra %d · "+
		"команд с исполнением %d · из них решивших Args %d",
		len(parsers), shared, viaCobra, withRun, decided)

	if len(parsers) == 0 {
		t.Fatal("точек наката не найдено ни одной — гейт ничего не осмотрел. Сменилась " +
			"раскладка каталогов либо предикат перестал их узнавать")
	}

	for _, f := range migratorCLIParserFindings(parsers) {
		t.Errorf("%s", f)
	}
}

// TestMigratorCLISurfaceIsDeclared — решение о поверхности существует и
// называет то, ради чего на него ссылаются.
//
// Документ, потерявший своё утверждение, — тот же класс, что и отсутствие
// решения: гейт продолжал бы требовать равенства, а прочитать, каким оно
// объявлено, было бы негде.
func TestMigratorCLISurfaceIsDeclared(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, migratorCLIDecisionDoc))
	if err != nil {
		t.Fatalf("решение о поверхности CLI мигратора не читается (%s): %v", migratorCLIDecisionDoc, err)
	}
	doc := string(raw)
	t.Logf("перепись: решение прочитано, %d байт", len(raw))

	for _, want := range []string{migratorCLIBinaryName, "--target", "--dsn"} {
		if !strings.Contains(doc, want) {
			t.Errorf("%s не называет %q — на него ссылаются ради этого утверждения",
				migratorCLIDecisionDoc, want)
		}
	}
}
