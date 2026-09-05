// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
// переменная сборки, константа в самой точке наката И имя, которым инструмент
// представляется САМ (поля помощи cobra). Одного места мало — разойтись могут
// именно они между собой (в nlb расходились Dockerfile, Makefile и манифест
// согласованно, а с шестью соседями — нет).
//
// Последняя форма добавлена по инъекции приёмщика: подмена `Use:` оставляла
// гейт зелёным с переписью «различных имён 1», хотя расхождение было живо
// именно в ней — то есть молчание гейта читалось как «имя одно».
func TestMigratorBinaryIsNamedTheSameEverywhere(t *testing.T) {
	root, paths := migratorCLICorpus(t)

	var (
		filesRead   int
		goParsed    int
		mentions    []migratorCLIMention
		selfNamedIn int
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
		rel = filepath.ToSlash(rel)
		content := string(raw)
		mentions = append(mentions, migratorCLIMentions(rel, content)...)

		// Формы 5-6 живут в полях помощи cobra и берутся РАЗБОРОМ. Предварительный
		// отбор по подстроке здесь законен и не сужает предмет: имя, оканчивающееся
		// на «migrator», не может стоять в файле, где этой подстроки нет вовсе.
		if !strings.HasSuffix(rel, ".go") || !strings.Contains(content, "migrator") {
			continue
		}
		goParsed++
		self, perr := migratorCLIGoMentions(rel, content)
		if perr != nil {
			t.Fatalf("%v", perr)
		}
		if len(self) > 0 {
			selfNamedIn++
		}
		mentions = append(mentions, self...)
	}

	names := map[string]int{}
	for _, m := range mentions {
		names[m.Name]++
	}
	t.Logf("перепись: файлов сборки и развёртывания прочитано %d (из них разобрано как Go %d, "+
		"справку cobra с именем несут %d), мест, называющих бинарь, %d, различных имён %d",
		filesRead, goParsed, selfNamedIn, len(mentions), len(names))

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
		roots, rootsRun  int
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
		roots += parsed.Roots
		rootsRun += parsed.Roots - len(parsed.RootsWithoutRun)
	}

	t.Logf("перепись: точек наката %d · на общем разборе %d · на cobra %d · "+
		"команд с исполнением %d · из них решивших Args %d · корневых команд %d · "+
		"из них несущих исполнение %d",
		len(parsers), shared, viaCobra, withRun, decided, roots, rootsRun)

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

	// Величины берутся ИЗ ПРОДУКТА, а не выписываются литералом: выписанный
	// литерал был бы вторым местом об одном предмете и разошёлся бы с первым
	// молча — ровно тем способом, каким накопилось само различие.
	for _, want := range []string{
		migratorCLIBinaryName,
		migratorcli.EnvDSN,
		"--target",
		"--dsn",
		"--dialect " + migratorcli.DialectPostgres,
		migratorcli.CommandHelp,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("%s не называет %q — на него ссылаются ради этого утверждения",
				migratorCLIDecisionDoc, want)
		}
	}
}

// TestMigratorRefusalTextsHaveOneProducer — тон отказа объявлен в ОДНОМ месте.
//
// До #1461 редакций было две: прямая форма говорила словами стандартной
// библиотеки, делегирующая — словами cobra. На одном и том же входе оператор
// получал разные строки, а скрипт, читающий отказ образцом, срабатывал на одном
// сервисе и молчал на соседнем.
//
// Сведение держится не сверкой копий, а тем, что копия одна. Гейт судит
// ОБЪЯВЛЕНИЕ текста: звать производителя точка наката вправе сколько угодно,
// писать свою редакцию — нет.
func TestMigratorRefusalTextsHaveOneProducer(t *testing.T) {
	root := repoRoot(t)

	var corpus []string
	for _, dir := range []string{"services", "pkg"} {
		found, err := treecorpus.UnderWithSuffix(filepath.Join(root, dir), ".go")
		if err != nil {
			t.Fatalf("корпус дерева (%s) не построен: %v", dir, err)
		}
		corpus = append(corpus, found...)
	}

	var (
		filesRead   int
		ownerDecls  int
		findings    []string
		entryPoints int
	)
	for _, p := range corpus {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		// Судятся точки наката и сам общий пакет: больше тексты отказа мигратора
		// нигде не живут. Предпосылка проверяется переписью ниже — объявлений у
		// владельца обязано быть НЕ НОЛЬ, иначе производитель исчез, а гейт
		// продолжал бы молчать.
		inOwner := strings.HasPrefix(rel, migratorCLIRefusalOwner)
		isEntry := strings.Contains(rel, "/cmd/migrator/") && !strings.HasSuffix(rel, "_test.go")
		if !inOwner && !isEntry {
			continue
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("%s: чтение не удалось: %v", rel, rerr)
		}
		filesRead++
		if isEntry {
			entryPoints++
		}
		decls, derr := migratorCLIRefusalDeclarations(rel, string(raw))
		if derr != nil {
			t.Fatalf("%v", derr)
		}
		if inOwner {
			ownerDecls += len(decls)
			continue
		}
		findings = append(findings, decls...)

		// Вторая половина того же предмета: не только ЧТО сказано, но и КАК
		// подано. Журнал ставит впереди метку времени, и она делала из одного
		// контракта две редакции.
		journalled, jerr := migratorCLIJournalRefusals(rel, string(raw))
		if jerr != nil {
			t.Fatalf("%v", jerr)
		}
		findings = append(findings, journalled...)
	}

	t.Logf("перепись: файлов осмотрено %d (точек наката %d), объявлений текста отказа "+
		"у производителя %d, находок (вторая редакция либо подача через журнал) %d",
		filesRead, entryPoints, ownerDecls, len(findings))

	if filesRead == 0 {
		t.Fatal("не прочитано ни одного файла — гейт ничего не осмотрел, и его молчание " +
			"неотличимо от исправности")
	}
	if entryPoints == 0 {
		t.Fatal("точек наката не найдено ни одной — сменилась раскладка каталогов")
	}
	if ownerDecls == 0 {
		t.Fatalf("у производителя (%s) не объявлено ни одного текста отказа — предпосылка "+
			"гейта отпала: он молчал бы, потому что искать стало нечего", migratorCLIRefusalOwner)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}
