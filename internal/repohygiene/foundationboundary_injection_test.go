// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// foundationboundary_injection_test.go — доказательство способности трёх гейтов
// границы фундамента падать И молчать.
//
// # Прогонов по оси больше двух, и это не педантизм
//
// У каждой оси НЕСКОЛЬКО независимых утверждений, и сверка у всех трёх
// двусторонняя. Инъекция обязана ронять ТОЛЬКО проверяемое: красное, пришедшее
// от соседнего утверждения, доказывало бы работу соседа, а новое могло бы
// оказаться вакуумным, не показав этого ничем. Поэтому каждая инъекция снимает
// ОДНО свойство у входа, чьи остальные целы, и проверяется, что находка ровно
// одна и пришла от нужной ветви.
//
// Порядок по каждой оси: контроль (всё цело — молчит) · инъекция нового
// свойства · инъекция соседнего, уже существующего рядом. Без третьего
// молчание соседнего утверждения неотличимо от молчания мёртвого.
//
// # Законный близнец подан по каждой оси
//
//	ось 1  каталог, чей класс объявлен, — находкой быть не должен
//	ось 2  ребро РАЗРЕШЁННОГО направления в отбор не попадает вовсе
//	ось 3  рантайм-пакет в замыкании двоичного лежать ОБЯЗАН

// ------------------------------- ось 1 -------------------------------

func catalogFixture() ([]string, map[string]foundationClass) {
	inTree := []string{"authz", "ids", "pgtest", "tokenpolicy"}
	declared := map[string]foundationClass{
		"authz":       classCorelib,
		"ids":         classCorelib,
		"pgtest":      classToolchain,
		"tokenpolicy": classKaname,
	}
	return inTree, declared
}

func TestCatalogJudgeIsSilentWhenEveryCatalogDeclaresItsClass(t *testing.T) {
	inTree, declared := catalogFixture()

	faults, census := judgeFoundationCatalogs(inTree, declared)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на согласованном объявлении (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Законные близнецы названы ЧИСЛОМ: все четыре каталога дошли до разбора и
	// находкой не стали, включая оснастку и каталог службы.
	if census.Catalogs != 4 || census.Declared != 4 {
		t.Fatalf("перепись контроля не сошлась: каталогов %d, объявлено %d, ожидалось 4 и 4",
			census.Catalogs, census.Declared)
	}
}

func TestCatalogJudgeCatchesACatalogWithNoDeclaredClass(t *testing.T) {
	inTree, declared := catalogFixture()
	inTree = append(inTree, "principalwire") // 52-й каталог заведён, класса нет

	faults, census := judgeFoundationCatalogs(inTree, declared)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Утверждается ПРИЧИНА, а не только координата: соседняя ветвь (класс вне
	// закрытого набора) на том же входе называет тот же каталог, поэтому проверка
	// по одной координате не различала бы две ветви и осталась бы зелёной при
	// снятой первой. Измерено сломом: без этой строки инъекция вакуумна.
	if !strings.Contains(faults[0], "класса не объявлено") {
		t.Fatalf("находка не называет ПРИЧИНУ (класса не объявлено): %s", faults[0])
	}
	if !strings.Contains(faults[0], "pkg/principalwire") {
		t.Fatalf("находка не называет координату нового каталога: %s", faults[0])
	}
	if census.Catalogs != 5 {
		t.Fatalf("перепись не назвала объём осмотренного: каталогов %d, ожидалось 5",
			census.Catalogs)
	}
}

// TestCatalogJudgeCatchesADeclarationWithNoCatalog — соседняя, уже существующая
// сторона той же сверки. Без этого прогона её молчание в контроле неотличимо от
// молчания мёртвой ветви.
func TestCatalogJudgeCatchesADeclarationWithNoCatalog(t *testing.T) {
	inTree, declared := catalogFixture()
	declared["retiredpackage"] = classCorelib // запись описывает несуществующее

	faults, _ := judgeFoundationCatalogs(inTree, declared)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка обратной стороны, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "pkg/retiredpackage") {
		t.Fatalf("находка не называет запись без каталога: %s", faults[0])
	}
}

func TestCatalogJudgeCatchesAClassOutsideTheClosedSet(t *testing.T) {
	inTree, declared := catalogFixture()
	declared["ids"] = foundationClass("фундамент")

	faults, _ := judgeFoundationCatalogs(inTree, declared)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "вне закрытого набора") {
		t.Fatalf("находка не называет закрытость набора: %s", faults[0])
	}
}

// TestCatalogJudgeRefusesAnEmptyWalkInsteadOfReportingNoFindings — K3-15.
func TestCatalogJudgeRefusesAnEmptyWalkInsteadOfReportingNoFindings(t *testing.T) {
	_, declared := catalogFixture()

	faults, census := judgeFoundationCatalogs(nil, declared)

	if len(faults) != 1 || !strings.Contains(faults[0], "обход пуст") {
		t.Fatalf("пустой обход обязан быть ОТКАЗОМ, а не «находок ноль»: %v", faults)
	}
	if census.Catalogs != 0 {
		t.Fatalf("перепись пустого обхода обязана называть ноль осмотренных, а не %d",
			census.Catalogs)
	}
}

// ------------------------------- ось 2 -------------------------------

func edgeFixture() ([]boundaryEdge, []knownBoundaryEdge) {
	observed := []boundaryEdge{
		{From: "pkg/listnarrow", To: "pkg/api/kacho/cloud/iam/v1",
			FromClass: classCorelib, ToClass: classKaname, Prod: 3, Test: 3},
		{From: "services/iam/internal/manifest", To: "pkg/modulemanifest",
			FromClass: classKaname, ToClass: classKacho, Prod: 1, Test: 1},
	}
	ledger := []knownBoundaryEdge{
		{"pkg/listnarrow", "pkg/api/kacho/cloud/iam/v1", 3, 3, "З2"},
		{"services/iam/internal/manifest", "pkg/modulemanifest", 1, 1, "З8"},
	}
	return observed, ledger
}

func TestEdgeJudgeIsSilentWhenTheTreeMatchesTheLedger(t *testing.T) {
	observed, ledger := edgeFixture()

	faults, census := judgeBoundaryEdges(observed, ledger, 5914, 11647)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на дереве, сошедшемся с ведомостью (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if census.Edges != 2 || census.LedgerRows != 2 || census.FilesRead != 5914 {
		t.Fatalf("перепись контроля не сошлась: рёбер %d, строк ведомости %d, файлов %d",
			census.Edges, census.LedgerRows, census.FilesRead)
	}
}

// TestEdgeJudgeCatchesAnEdgeThatTheLedgerDoesNotName — новое нарушение.
func TestEdgeJudgeCatchesAnEdgeThatTheLedgerDoesNotName(t *testing.T) {
	observed, ledger := edgeFixture()
	observed = append(observed, boundaryEdge{
		From: "pkg/outbox", To: "pkg/api/kacho/cloud/iam/v1",
		FromClass: classCorelib, ToClass: classKaname, Prod: 1, Test: 0,
	})

	faults, _ := judgeBoundaryEdges(observed, ledger, 5914, 11647)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Находка обязана назвать ОБЕ координаты и направление: без них читатель
	// пойдёт искать не там.
	for _, want := range []string{"pkg/outbox", "pkg/api/kacho/cloud/iam/v1", "corelib", "kaname"} {
		if !strings.Contains(faults[0], want) {
			t.Fatalf("находка не называет %q: %s", want, faults[0])
		}
	}
}

// TestEdgeJudgeCatchesALedgerRowWithNothingToForgive — самоистечение
// послабления. Это соседняя сторона той же сверки, и она обязана быть доказана
// отдельно: в контроле её молчание неотличимо от молчания мёртвой ветви.
func TestEdgeJudgeCatchesALedgerRowWithNothingToForgive(t *testing.T) {
	observed, ledger := edgeFixture()
	observed = observed[:1] // ребро З8 в дереве закрыли, запись осталась

	faults, _ := judgeBoundaryEdges(observed, ledger, 5914, 11647)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "нечего исключать") ||
		!strings.Contains(faults[0], "services/iam/internal/manifest") {
		t.Fatalf("находка не объявляет истечение записи с координатой: %s", faults[0])
	}
}

// TestEdgeJudgeCatchesGrowthUnderAnAlreadyNamedEdge — счёт точный, а не потолок:
// ребро между уже названными пакетами, набравшее лишний файл, — нарушение.
func TestEdgeJudgeCatchesGrowthUnderAnAlreadyNamedEdge(t *testing.T) {
	observed, ledger := edgeFixture()
	observed[0].Prod = 4 // прибавился четвёртый прод-файл

	faults, _ := judgeBoundaryEdges(observed, ledger, 5914, 11647)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "прод 3") || !strings.Contains(faults[0], "прод 4") {
		t.Fatalf("находка не называет ОБА числа — записанное и наблюдённое: %s", faults[0])
	}
}

func TestEdgeJudgeRefusesAnEmptyWalkInsteadOfReportingNoFindings(t *testing.T) {
	_, ledger := edgeFixture()

	faults, census := judgeBoundaryEdges(nil, ledger, 0, 0)

	if len(faults) != 1 || !strings.Contains(faults[0], "обход пуст") {
		t.Fatalf("пустой обход обязан быть ОТКАЗОМ, а не «находок ноль»: %v", faults)
	}
	if census.FilesRead != 0 {
		t.Fatalf("перепись пустого обхода обязана называть ноль прочитанных, а не %d",
			census.FilesRead)
	}
}

// ------------------------------- ось 3 -------------------------------

func closureFixture() (map[string]foundationClass, map[string]string) {
	reached := map[string]foundationClass{
		"operations":  classCorelib,   // законный близнец: рантайм-пакет
		"grpcsrv":     classCorelib,   // законный близнец
		"tokenpolicy": classKaname,    // законный близнец: kacho -> kaname разрешено
		"gitenv":      classToolchain, // прощено ведомостью
		"treecorpus":  classToolchain, // прощено ведомостью
	}
	ledger := map[string]string{"gitenv": "З8", "treecorpus": "З8"}
	return reached, ledger
}

func TestClosureJudgeIsSilentWhenOnlyForgivenToolchainIsShipped(t *testing.T) {
	reached, ledger := closureFixture()

	faults, census := judgeShippedToolchain(reached, ledger, 17)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на замыкании, сошедшемся с ведомостью (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Законные близнецы названы ЧИСЛОМ: рантайм и пакет службы дошли до разбора
	// и находкой не стали.
	if census.Binaries != 17 || census.ReachedPkgs != 5 {
		t.Fatalf("перепись контроля не сошлась: двоичных %d, пакетов %d, ожидалось 17 и 5",
			census.Binaries, census.ReachedPkgs)
	}
}

func TestClosureJudgeCatchesToolchainThatNoLedgerRowForgives(t *testing.T) {
	reached, ledger := closureFixture()
	reached["listfiltergate"] = classToolchain // новая оснастка уехала в поставку

	faults, _ := judgeShippedToolchain(reached, ledger, 17)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "pkg/listfiltergate") {
		t.Fatalf("находка не называет координату: %s", faults[0])
	}
}

// TestClosureJudgeCatchesALedgerRowWithNothingToForgive — соседняя сторона.
func TestClosureJudgeCatchesALedgerRowWithNothingToForgive(t *testing.T) {
	reached, ledger := closureFixture()
	delete(reached, "gitenv") // долг закрыт, запись осталась

	faults, _ := judgeShippedToolchain(reached, ledger, 17)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "нечего исключать") || !strings.Contains(faults[0], "gitenv") {
		t.Fatalf("находка не объявляет истечение записи с координатой: %s", faults[0])
	}
}

// TestClosureJudgeCatchesAClosureWithoutAnyRuntimePackage — положительный
// близнец, сформулированный как отказ: замыкание без рантайм-пакетов означает,
// что «оснастки не нашлось» относится к непрочитанному.
func TestClosureJudgeCatchesAClosureWithoutAnyRuntimePackage(t *testing.T) {
	ledger := map[string]string{}
	reached := map[string]foundationClass{"tokenpolicy": classKaname}

	faults, _ := judgeShippedToolchain(reached, ledger, 17)

	if len(faults) != 1 || !strings.Contains(faults[0], "НИ ОДНОГО") {
		t.Fatalf("замыкание без рантайм-пакетов обязано быть находкой: %v", faults)
	}
}

func TestClosureJudgeRefusesAnEmptyWalkInsteadOfReportingNoFindings(t *testing.T) {
	reached, ledger := closureFixture()

	noBinaries, census := judgeShippedToolchain(reached, ledger, 0)
	if len(noBinaries) != 1 || !strings.Contains(noBinaries[0], "обход пуст") {
		t.Fatalf("ноль двоичных обязан быть ОТКАЗОМ: %v", noBinaries)
	}
	if census.Binaries != 0 {
		t.Fatalf("перепись обязана называть ноль двоичных, а не %d", census.Binaries)
	}

	empty, _ := judgeShippedToolchain(map[string]foundationClass{}, ledger, 17)
	if len(empty) != 1 || !strings.Contains(empty[0], "замыкание пусто") {
		t.Fatalf("пустое замыкание обязано быть ОТКАЗОМ: %v", empty)
	}
}

// --------------------- разрешение пути и анти-маска ---------------------

// TestClassOfPackageResolvesSplitSubtreesAndRefusesUnknownCatalogs — каталоги
// со знаком † расщеплены, и подкаталог обязан получать СВОЙ класс, а не класс
// остатка. Победа самой длинной приставки проверяется парой, у которой обе
// приставки совпадают началом.
func TestClassOfPackageResolvesSplitSubtreesAndRefusesUnknownCatalogs(t *testing.T) {
	cases := []struct {
		path string
		want foundationClass
	}{
		// расщепление pkg/api (приёмка §5.1)
		{"pkg/api/kacho/cloud/iam/v1", classKaname},
		{"pkg/api/kacho/cloud/operation/v1", classCorelib},
		{"pkg/api/kacho/cloud/subscription", classCorelib},
		{"pkg/api/kacho/cloud/quota/v1", classCorelib},
		{"pkg/api/kacho/iam/authz/v1", classCorelib},
		{"pkg/api/kacho/cloud/vpc/v1", classKacho},
		// расщепление pkg/quota (приёмка §5.3): остаток corelib, два подпакета kaname
		{"pkg/quota", classCorelib},
		{"pkg/quota/quotaread", classCorelib},
		{"pkg/quota/quotaiam", classKaname},
		{"pkg/quota/quotapb", classKaname},
		// оснастка и обычный каталог
		{"pkg/pgtest", classToolchain},
		{"pkg/listnarrow/narrowtest", classCorelib},
		// дерево вне pkg/: победа самой длинной приставки
		{"services/iam/internal/manifest", classKaname},
		{"services/vpc/internal/repo", classKacho},
		{"gateway/internal/restmux", classKacho},
		{"internal/repohygiene", classToolchain},
		{"tools/quota-refusal-migration", classToolchain},
	}
	for _, c := range cases {
		got, ok := classOfPackage(c.path)
		if !ok {
			t.Fatalf("%s: класс не разрешён, а обязан", c.path)
		}
		if got != c.want {
			t.Fatalf("%s: класс %s, ожидался %s", c.path, got, c.want)
		}
	}

	// Каталог `pkg/*` без записи обязан вернуть false, а НЕ умолчание: умолчание
	// здесь и было бы дырой, ради которой карта заведена.
	if _, ok := classOfPackage("pkg/nosuchcatalog"); ok {
		t.Fatal("необъявленный каталог pkg/ получил класс умолчанием — карта обезврежена")
	}
}

// TestEveryLedgerRowNamesAGenuinelyForbiddenDirection — анти-маска.
//
// Запись, прощающая направление, которое и без неё разрешено, ведомостью не
// является: она ничего не прощает, никогда не совпадает с наблюдённым и потому
// выглядит работающей, ничего не удерживая.
func TestEveryLedgerRowNamesAGenuinelyForbiddenDirection(t *testing.T) {
	if len(knownBoundaryEdges) == 0 {
		t.Skip("ведомость пуста — прощать нечего, и это цель, а не поломка")
	}
	for _, k := range knownBoundaryEdges {
		from, okFrom := classOfPackage(k.From)
		to, okTo := classOfPackage(k.To)
		if !okFrom || !okTo {
			t.Fatalf("строка ведомости %s -> %s называет пакет, чей класс не разрешается",
				k.From, k.To)
		}
		if !forbiddenDirections[[2]foundationClass{from, to}] {
			t.Fatalf("строка ведомости %s -> %s (%s -> %s) прощает РАЗРЕШЁННОЕ "+
				"направление: она ничего не удерживает и никогда не истечёт по совпадению",
				k.From, k.To, from, to)
		}
		if k.Subject == "" {
			t.Fatalf("строка ведомости %s -> %s не называет предмета: послабление без "+
				"предмета не истечёт никогда", k.From, k.To)
		}
	}
	t.Logf("перепись: строк ведомости рёбер %d, у каждой названы предмет и запрещённое "+
		"направление", len(knownBoundaryEdges))
}

// TestTargetModuleLayoutIsAcyclic — ацикличность ДОКАЗЫВАЕТСЯ, а не объявляется.
//
// Разрешённые направления выводятся из forbiddenDirections (дополнение по всем
// упорядоченным парам трёх модулей), после чего ацикличность устанавливается
// топологической сортировкой: если после снятия всех истоков остаётся хоть одна
// вершина, в графе есть цикл.
func TestTargetModuleLayoutIsAcyclic(t *testing.T) {
	modules := []foundationClass{classCorelib, classKaname, classKacho}

	allowed := map[foundationClass][]foundationClass{}
	edges := 0
	for _, a := range modules {
		for _, b := range modules {
			if a == b || forbiddenDirections[[2]foundationClass{a, b}] {
				continue
			}
			allowed[a] = append(allowed[a], b)
			edges++
		}
	}
	if edges == 0 {
		t.Fatal("разрешённых направлений ноль — граф пуст, и ацикличность относилась бы " +
			"к непрочитанному")
	}

	indeg := map[foundationClass]int{}
	for _, m := range modules {
		indeg[m] += 0
	}
	for _, outs := range allowed {
		for _, b := range outs {
			indeg[b]++
		}
	}
	var queue []foundationClass
	for m, d := range indeg {
		if d == 0 {
			queue = append(queue, m)
		}
	}
	removed := 0
	for len(queue) > 0 {
		n := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		removed++
		for _, b := range allowed[n] {
			indeg[b]--
			if indeg[b] == 0 {
				queue = append(queue, b)
			}
		}
	}
	if removed != len(modules) {
		t.Fatalf("граф разрешённых направлений содержит цикл: снято %d вершин из %d",
			removed, len(modules))
	}
	t.Logf("перепись: модулей %d · разрешённых направлений %d · запрещённых %d · "+
		"топологический порядок построен целиком",
		len(modules), edges, len(forbiddenDirections))
}

// TestClosureJudgeCatchesAForgivenToolchainWithNoSubject — послабление без
// предмета: оно выглядит записью ведомости и не снимается ничем.
func TestClosureJudgeCatchesAForgivenToolchainWithNoSubject(t *testing.T) {
	reached, ledger := closureFixture()
	ledger["gitenv"] = "" // предмет стёрт, запись осталась

	faults, _ := judgeShippedToolchain(reached, ledger, 17)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "не называя предмета") ||
		!strings.Contains(faults[0], "gitenv") {
		t.Fatalf("находка не называет предмет и координату: %s", faults[0])
	}
}

// ------------------------------- ось 4 -------------------------------

// prefixFixture — две живые приставки из каждой карты: расщепление † и корень
// вне `pkg/`. Обе с непустым предметом, поэтому контроль обязан молчать.
func prefixFixture() ([]string, map[string]int) {
	declared := []string{"pkg/api/kacho/cloud/iam", "services/iam"}
	pathsUnder := map[string]int{"pkg/api/kacho/cloud/iam": 83, "services/iam": 2332}
	return declared, pathsUnder
}

func TestPrefixJudgeIsSilentWhenEveryDeclaredPrefixHasASubject(t *testing.T) {
	declared, pathsUnder := prefixFixture()

	faults, census := judgeFoundationPrefixes(declared, pathsUnder)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на живых приставках (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if census.Declared != 2 || census.Catalogs != 2 {
		t.Fatalf("перепись контроля не сошлась: объявлено %d, с предметом %d, ожидалось 2 и 2",
			census.Declared, census.Catalogs)
	}
}

// TestPrefixJudgeCatchesADeadSubtreeEntry — мёртвая запись карты расщеплений.
func TestPrefixJudgeCatchesADeadSubtreeEntry(t *testing.T) {
	declared, pathsUnder := prefixFixture()
	declared = append(declared, "pkg/api/kacho/cloud/nosuchdomain") // путей 0

	faults, census := judgeFoundationPrefixes(declared, pathsUnder)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Находка обязана назвать ИМЯ: без него читатель не знает, какую из
	// одиннадцати записей снимать.
	if !strings.Contains(faults[0], "pkg/api/kacho/cloud/nosuchdomain") {
		t.Fatalf("находка не называет мёртвую приставку: %s", faults[0])
	}
	// Перепись обязана показать, что запись ПРОЧИТАНА и осуждена, а не пропущена.
	if census.Declared != 3 || census.Catalogs != 2 {
		t.Fatalf("перепись не разделила прочитанное и живое: объявлено %d, с предметом %d",
			census.Declared, census.Catalogs)
	}
}

// TestPrefixJudgeCatchesADeadRootEntry — та же беззубость у второй карты.
// Прогон отдельный: молчание соседней карты в контроле иначе неотличимо от
// молчания мёртвой ветви.
func TestPrefixJudgeCatchesADeadRootEntry(t *testing.T) {
	declared, pathsUnder := prefixFixture()
	declared = append(declared, "services/nosuchservice")

	faults, _ := judgeFoundationPrefixes(declared, pathsUnder)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "services/nosuchservice") {
		t.Fatalf("находка не называет мёртвый корень: %s", faults[0])
	}
}

// TestPrefixJudgeStaysSilentOnALiveAdditionAtAGrownCensus — законный близнец.
//
// Требование к нему жёстче обычного молчания: перепись обязана ВЫРАСТИ. Молчание
// при неизменной переписи означало бы, что запись не прочитана, — то есть тот же
// класс «ноль находок против ноль прочитанного», только внутри пробы.
func TestPrefixJudgeStaysSilentOnALiveAdditionAtAGrownCensus(t *testing.T) {
	declared, pathsUnder := prefixFixture()
	_, before := judgeFoundationPrefixes(declared, pathsUnder)

	declared = append(declared, "pkg/api/kacho/cloud/vpc")
	pathsUnder["pkg/api/kacho/cloud/vpc"] = 41

	faults, after := judgeFoundationPrefixes(declared, pathsUnder)

	if len(faults) != 0 {
		t.Fatalf("законный близнец покраснел (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if after.Declared != before.Declared+1 || after.Catalogs != before.Catalogs+1 {
		t.Fatalf("перепись не выросла: было объявлено %d с предметом %d, стало %d и %d — "+
			"молчание относится к непрочитанному",
			before.Declared, before.Catalogs, after.Declared, after.Catalogs)
	}
}

func TestPrefixJudgeRefusesAnEmptyWalkInsteadOfReportingNoFindings(t *testing.T) {
	declared, pathsUnder := prefixFixture()

	noDecl, _ := judgeFoundationPrefixes(nil, pathsUnder)
	if len(noDecl) != 1 || !strings.Contains(noDecl[0], "карты путей пусты") {
		t.Fatalf("пустые карты обязаны быть ОТКАЗОМ: %v", noDecl)
	}

	noTree, census := judgeFoundationPrefixes(declared, map[string]int{})
	if len(noTree) != 1 || !strings.Contains(noTree[0], "обход пуст") {
		t.Fatalf("пустой обход обязан быть ОТКАЗОМ: %v", noTree)
	}
	if census.Catalogs != 0 {
		t.Fatalf("перепись пустого обхода обязана называть ноль живых, а не %d",
			census.Catalogs)
	}
}

// TestDeclaredPrefixesCoversBothPathMaps — вывод перечня сам обязан быть сверен.
//
// Найдено сломом собственной оси 4: если declaredPrefixes() перестаёт читать
// одну из двух карт, проба над деревом ПРОХОДИТ, молча упав с одиннадцати
// приставок до семи. Перепись при этом честно печатает 7 — и выглядит нормой,
// потому что сравнить её не с чем.
//
// Это тот же класс, что ловит сама ось 4, только уровнем выше: там мёртвой была
// запись карты, здесь — целая карта, выпавшая из обхода. Поэтому сверяется не
// только состав, но и ЧИСЛО: пропажа карты обязана быть арифметически видна.
func TestDeclaredPrefixesCoversBothPathMaps(t *testing.T) {
	got := map[string]bool{}
	for _, p := range declaredPrefixes() {
		got[p] = true
	}

	want := 0
	for _, s := range foundationSubtrees {
		want++
		if !got[s.Prefix] {
			t.Fatalf("приставка расщепления %s не попала в перечень: её карта выпала "+
				"из обхода, и ось 4 о ней не судит", s.Prefix)
		}
	}
	for _, r := range foundationRoots {
		want++
		if !got[r.Prefix] {
			t.Fatalf("приставка корня %s не попала в перечень: её карта выпала из "+
				"обхода, и ось 4 о ней не судит", r.Prefix)
		}
	}

	if len(declaredPrefixes()) != want {
		t.Fatalf("перечень приставок несёт %d записей при %d объявленных в двух картах: "+
			"расхождение означает выпавшую или задвоенную карту",
			len(declaredPrefixes()), want)
	}
	if want == 0 {
		t.Fatal("обе карты путей пусты — вывод перечня относился бы к непрочитанному")
	}
	t.Logf("перепись: приставок расщепления %d · корней %d · в перечне %d",
		len(foundationSubtrees), len(foundationRoots), len(declaredPrefixes()))
}
