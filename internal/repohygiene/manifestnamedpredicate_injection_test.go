// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestnamedpredicate_injection_test.go — доказательство, что разбор
// «предикат, названный манифестом, исполним в этом дереве» СПОСОБЕН упасть по
// каждой из пяти осей и СПОСОБЕН смолчать на законном близнеце.
//
// Инъекция зовёт ТЕ ЖЕ функции, что держатель
// (`extractManifestNamedPredicates`, `auditManifestNamedPredicates`,
// `parseManifestMakeTargets`, `manifestPredicatePackageDir`), а не свою копию:
// копия доказывала бы свойство копии.
//
// У КАЖДОЙ оси отрицания стоит ПОЛОЖИТЕЛЬНЫЙ близнец, отличающийся ровно одним
// фактом. Без него красное было бы свойством формы входа, а не дефекта.
package repohygiene

import (
	"strings"
	"testing"
)

// injectionManifestTree — вид дерева, в котором ВСЁ названное существует.
//
// Каждый отрицательный случай снимает из него ровно одну вещь. Так «красное»
// привязано к снятому факту, а не к форме входа.
func injectionManifestTree() manifestTreeView {
	return manifestTreeView{
		Files: map[string]bool{
			"services/iam/Makefile":           true,
			"services/vpc/manifest.yaml":      true,
			"internal/repohygiene/holder.go":  true,
			"proto/kaname/cloud/iam/v1/m.fga": true,
		},
		Dirs: map[string]bool{
			".": true, "services": true, "services/iam": true, "services/vpc": true,
			"internal": true, "internal/repohygiene": true, "internal/empty": true,
			"proto": true, "proto/kaname": true, "proto/kaname/cloud": true,
			"proto/kaname/cloud/iam": true, "proto/kaname/cloud/iam/v1": true,
		},
		MakeTargets: map[string]map[string]bool{
			"services/iam/Makefile": {"model-canon-check": true},
		},
		GoDirs: map[string]bool{"internal/repohygiene": true},
		GoFuncs: map[string]map[string]bool{
			"internal/repohygiene": {"TestHolderLives": true},
		},
	}
}

// TestInjection_ManifestNamedPredicateCatchesEachAxis — по одной оси за раз.
func TestInjection_ManifestNamedPredicateCatchesEachAxis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		axis string
		// header — шапка синтетического манифеста.
		header string
		// mutate — что снимается из дерева. nil = дерево полное.
		mutate func(*manifestTreeView)
		// wantFinding — ожидается ли находка.
		wantFinding bool
		// wantSubstr — чем находка обязана называть свой предмет.
		wantSubstr string
	}{
		{
			axis:        "N1 каталог -C отсутствует",
			header:      "#      make -C services/iam model-canon-check",
			mutate:      func(v *manifestTreeView) { delete(v.Dirs, "services/iam") },
			wantFinding: true,
			wantSubstr:  "services/iam",
		},
		{
			axis:        "N1 законный близнец — каталог есть",
			header:      "#      make -C services/iam model-canon-check",
			wantFinding: false,
		},
		{
			axis:        "N2 Makefile есть, цель НЕ объявлена",
			header:      "#      make -C services/iam module-manifest-check",
			wantFinding: true,
			wantSubstr:  "НЕ ОБЪЯВЛЯЕТ",
		},
		{
			axis:        "N2 законный близнец — цель объявлена",
			header:      "#      make -C services/iam model-canon-check",
			wantFinding: false,
		},
		{
			axis:        "N2 Makefile отсутствует",
			header:      "#      make -C services/iam model-canon-check",
			mutate:      func(v *manifestTreeView) { delete(v.Files, "services/iam/Makefile") },
			wantFinding: true,
			wantSubstr:  "цель объявить некому",
		},
		{
			axis:        "N3 каталог пакета отсутствует",
			header:      "#      go test ./internal/nosuch/",
			wantFinding: true,
			wantSubstr:  "каталога пакета",
		},
		{
			axis:        "N3 каталог есть, файлов .go нет",
			header:      "#      go test ./internal/empty/",
			wantFinding: true,
			wantSubstr:  "no Go files",
		},
		{
			axis:        "N3 законный близнец — пакет с исходниками",
			header:      "#      go test ./internal/repohygiene/",
			wantFinding: false,
		},
		{
			axis:        "N4 -run называет функцию, которой нет",
			header:      "#      go test ./internal/repohygiene/ -run TestNoSuchHolder -count=1",
			wantFinding: true,
			wantSubstr:  "no tests to run",
		},
		{
			axis:        "N4 законный близнец — функция объявлена",
			header:      "#      go test ./internal/repohygiene/ -run TestHolderLives -count=1",
			wantFinding: false,
		},
		{
			axis:        "N5 координата в кавычках отсутствует",
			header:      "#  сверка с каноном `services/iam/internal/manifest/roles.go`",
			wantFinding: true,
			wantSubstr:  "которой в дереве нет",
		},
		{
			axis:        "N5 законный близнец — координата существует",
			header:      "#  сверка с каноном `proto/kaname/cloud/iam/v1/m.fga`",
			wantFinding: false,
		},
		{
			axis: "проза, УПОМИНАЮЩАЯ команду, разбору не подлежит — иначе гейт " +
				"краснел бы на собственном объяснении",
			header:      "#  форму судит make -C services/iam module-manifest-check — в чужом дереве",
			wantFinding: false,
		},
		{
			// Пара со следующей осью различается РОВНО ОДНИМ фактом — назван ли
			// на строке чужой репозиторий. Один и тот же путь, один и тот же вид
			// дерева, разный вердикт.
			axis:        "N5 координата чужого репозитория НЕ проверяется, когда он назван",
			header:      "#  предел — `internal/manifest/roles.go` репозитория PRO-Robotech/kaname",
			wantFinding: false,
		},
		{
			axis:        "N5 тот же путь БЕЗ названного репозитория — находка",
			header:      "#  предел — `internal/manifest/roles.go`",
			wantFinding: true,
			wantSubstr:  "которой в дереве нет",
		},
		{
			axis: "своё имя репозитория исключения НЕ даёт: `PRO-Robotech/kacho` " +
				"есть это же дерево",
			header:      "#  предел — `internal/manifest/roles.go` в PRO-Robotech/kacho",
			wantFinding: true,
			wantSubstr:  "которой в дереве нет",
		},
		{
			axis: "команда чужого репозитория остаётся находкой — упоминание репозитория " +
				"обещания исполнимости не снимает",
			header:      "#      make -C services/iam model-canon-check (PRO-Robotech/kaname)",
			mutate:      func(v *manifestTreeView) { delete(v.Dirs, "services/iam") },
			wantFinding: true,
			wantSubstr:  "services/iam",
		},
		{
			axis:        "N6 глагол вне make/go test — аргумент-путь не резолвится",
			header:      "#      grep -l 'x' services/iam/internal/migrations/*.sql",
			mutate:      func(v *manifestTreeView) { delete(v.Dirs, "services/iam") },
			wantFinding: true,
			wantSubstr:  "искать было негде",
		},
		{
			axis:        "N6 законный близнец — образец совпадает с путём индекса",
			header:      "#      grep -l 'x' services/iam/Makefile",
			wantFinding: false,
		},
		{
			axis:        "N6 замер над ЧУЖИМ деревом законен, когда дерево названо",
			header:      "#      grep -l 'x' internal/migrations/*.sql — в PRO-Robotech/kaname",
			wantFinding: false,
		},
		{
			axis:        "N6 ключи и образцы путями не считаются — иначе находка была бы на каждой строке",
			header:      "#      git grep -n 'model-canon-check' -- services/vpc/manifest.yaml",
			wantFinding: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.axis, func(t *testing.T) {
			t.Parallel()
			preds, lines := extractManifestNamedPredicates("services/vpc/manifest.yaml", tc.header)
			if lines == 0 {
				t.Fatalf("ось %q: прочитано НОЛЬ строк-комментариев на входе %q — "+
					"вход не дошёл до разбора, и вердикт был бы свойством фикстуры",
					tc.axis, tc.header)
			}
			tree := injectionManifestTree()
			if tc.mutate != nil {
				tc.mutate(&tree)
			}
			found := auditManifestNamedPredicates(preds, tree)

			switch {
			case tc.wantFinding && len(found) == 0:
				t.Fatalf("ось %q: дефект внесён, находок НОЛЬ — разбор её не измеряет "+
					"(держателей извлечено %d)", tc.axis, len(preds))
			case !tc.wantFinding && len(found) > 0:
				t.Fatalf("ось %q: законный близнец дал %d находку(и) — гейт ловит форму, "+
					"а не существо:\n  %s", tc.axis, len(found), strings.Join(found, "\n  "))
			}
			if tc.wantFinding && !strings.Contains(strings.Join(found, "\n"), tc.wantSubstr) {
				t.Fatalf("ось %q: находка есть, но не называет %q — она про другое:\n  %s",
					tc.axis, tc.wantSubstr, strings.Join(found, "\n  "))
			}
		})
	}
}

// TestInjection_ManifestMakeTargetParseIsNotASubstringSearch — разбор целей
// `Makefile` читает ОБЪЯВЛЕНИЕ, а не вхождение имени.
//
// Ось отдельной пробой, потому что именно здесь проверка подстрокой зеленела бы
// молча: цель объясняют комментарием рядом с собой, и `grep` по имени нашёл бы
// объяснение снятой цели.
func TestInjection_ManifestMakeTargetParseIsNotASubstringSearch(t *testing.T) {
	t.Parallel()

	const makefile = "# Здесь стояла цель model-canon-check — снята вместе с исполнителем.\n" +
		"# Вызов был: make -C services/iam model-canon-check\n" +
		"CANON := model-canon-check\n" +
		"live-target:\n" +
		"\t@echo model-canon-check\n"

	targets := parseManifestMakeTargets(makefile)
	if targets["model-canon-check"] {
		t.Fatal("разбор признал ОБЪЯВЛЕННОЙ цель, встречающуюся только в комментарии, " +
			"в присваивании и в теле рецепта — это проверка подстрокой, и она зеленела бы " +
			"на объяснении снятой цели")
	}
	if !targets["live-target"] {
		t.Fatalf("разбор НЕ увидел объявленной цели — положительный контроль провален, "+
			"и молчание выше ничего не значит (прочитано целей %d)", len(targets))
	}
	if targets[".PHONY"] {
		t.Fatal("разбор признал целью директиву .PHONY — она перечисляет цели, а не объявляет их")
	}
}

// TestInjection_ManifestPackageDirResolution — каталог пакета выводится из пары
// «ключ -C, аргумент пакета», а не из одного из них.
func TestInjection_ManifestPackageDirResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dir, target, want string
	}{
		{"", "./internal/repohygiene/", "internal/repohygiene"},
		{"services/iam", "./internal/moduleroleparity/", "services/iam/internal/moduleroleparity"},
		{".", "./tools/x", "tools/x"},
		{"services/vpc", "./...", "services/vpc"},
	}
	for _, tc := range cases {
		got := manifestPredicatePackageDir(manifestNamedPredicate{Dir: tc.dir, Target: tc.target})
		if got != tc.want {
			t.Errorf("-C %q пакет %q → %q, ожидалось %q — неверный резолв дал бы находку "+
				"о каталоге, которого никто не называл", tc.dir, tc.target, got, tc.want)
		}
	}
}

// TestInjection_ManifestNamedPredicateEmptyInputIsNotPermission — пустой вход не
// есть всеразрешение: держатель падает на пустом обходе, и это проверяется здесь
// у самой переписи.
func TestInjection_ManifestNamedPredicateEmptyInputIsNotPermission(t *testing.T) {
	t.Parallel()

	preds, lines := extractManifestNamedPredicates("services/vpc/manifest.yaml", "resources: []\n")
	if len(preds) != 0 || lines != 0 {
		t.Fatalf("документ без комментариев дал держателей %d и строк %d — разбор видит "+
			"то, чего нет", len(preds), lines)
	}
	if found := auditManifestNamedPredicates(nil, manifestTreeView{}); len(found) != 0 {
		t.Fatalf("предикат на пустом входе дал %d находку(и) — он обязан молчать, а пустоту "+
			"обязан называть ДЕРЖАТЕЛЬ переписью, иначе «ноль находок» неотличимо от "+
			"«ноль прочитанного»", len(found))
	}
	census := manifestNamedPredicateCensus(0, 0, nil, 0)
	for _, must := range []string{"манифестов 0", "строк-комментариев прочитано 0", "названных держателей 0"} {
		if !strings.Contains(census, must) {
			t.Errorf("перепись не называет %q — объём осмотренного не напечатан: %s", must, census)
		}
	}
}

// TestInjection_ManifestForeignExemptionIsVisibleInTheCensus — исключение N5
// печатается ЧИСЛОМ, а не умалчивается.
//
// Отдельной пробой, потому что молчащее исключение и отсутствие предмета дают
// один и тот же вывод: «находок ноль». Величина в переписи — единственное, чем
// они различимы.
func TestInjection_ManifestForeignExemptionIsVisibleInTheCensus(t *testing.T) {
	t.Parallel()

	const header = "#  предел — `internal/manifest/roles.go` репозитория PRO-Robotech/kaname\n"
	preds, _ := extractManifestNamedPredicates("services/vpc/manifest.yaml", header)
	if len(preds) != 1 {
		t.Fatalf("извлечено держателей %d, ожидался 1 — вход не дошёл до разбора", len(preds))
	}
	if !preds[0].Foreign {
		t.Fatal("координата, названная вместе с чужим репозиторием, не отнесена к нему — " +
			"исключение не применилось, и следующая ось ничего не доказывает")
	}
	if n := countManifestForeignPredicates(preds); n != 1 {
		t.Fatalf("чужих координат посчитано %d, ожидалась 1", n)
	}
	census := manifestNamedPredicateCensus(1, 1, preds, 0)
	if !strings.Contains(census, "координат чужого репозитория 1") {
		t.Fatalf("перепись не называет числа исключённых координат — исключение стало бы "+
			"слепой зоной ровно своего размера: %s", census)
	}
}

// TestInjection_ManifestShellPathArgsSelectsOnlyPaths — отбор аргументов-путей.
//
// Отдельной пробой, потому что ось N6 падает или молчит целиком на этом отборе:
// признай он путём имя пакета Go или форму права, и находка была бы на каждой
// строке — гейт с ложными находками отключают первым.
func TestInjection_ManifestShellPathArgsSelectsOnlyPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cmd  string
		want []string
	}{
		{"grep -l 'x' services/iam/internal/migrations/*.sql", []string{"services/iam/internal/migrations/*.sql"}},
		{"git grep -n 'vpc.networks.create' -- services/vpc/manifest.yaml", []string{"services/vpc/manifest.yaml"}},
		// Имя модуля Go — не путь дерева: первый сегмент несёт точку.
		{"go build github.com/PRO-Robotech/kacho/gateway/cmd/x", nil},
		// Форма права и одиночный токен путями не являются.
		{"wc -l manifest.yaml", nil},
		// Абсолютный путь и переменная оболочки — не координаты индекса.
		{"cat /etc/hosts $HOME/x/y", nil},
	}
	for _, tc := range cases {
		got := manifestShellPathArgs(tc.cmd)
		if len(got) != len(tc.want) {
			t.Errorf("%q → %v, ожидалось %v", tc.cmd, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q → %v, ожидалось %v", tc.cmd, got, tc.want)
				break
			}
		}
	}
}
