// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Разбор класса — в шапке providerwalkverdict.go. Здесь инъекции на синтетике:
// каждая меняет РОВНО ОДИН факт против законного близнеца, и у каждой красной
// стоит её зелёная пара.

// healthyWalk — знаменатель и перепись исправного обхода. Числа взяты порядка
// сегодняшнего дерева, но вердикт от их величины не зависит: он зависит от
// СХОДИМОСТИ пар, поэтому инъекция ниже меняет одно число, а не масштаб.
func healthyWalk() (ProviderCensus, ProviderWalkDenominator) {
	census := ProviderCensus{
		Files:          1249,
		Literals:       49441,
		Carriers:       3,
		Reaches:        3,
		LedgerEntries:  3,
		LedgerSurfaces: 3,
		Exempt:         1,
		ProseMentions:  20,
	}
	denom := ProviderWalkDenominator{
		CommitGoFiles:   1250,
		SkippedGoFiles:  0,
		LexicalLiterals: 49441,
		DictionaryPaths: 5,
		RecogniserReach: 5,
	}
	return census, denom
}

// blindBecause — есть ли среди причин та, что называет ожидаемое словом.
func blindBecause(t *testing.T, v ProviderVerdict, substr string) {
	t.Helper()
	if v.Outcome != ProviderOutcomeBlind {
		t.Fatalf("исход %q, ожидался %q — обход, который не состоялся, назван вердиктом",
			v.Outcome, ProviderOutcomeBlind)
	}
	for _, r := range v.Blind {
		if strings.Contains(r, substr) {
			return
		}
	}
	t.Fatalf("исход слепой, но ни одна причина не называет %q: %v.\n"+
		"Причина — часть свойства: находка, называющая симптом вместо предмета, "+
		"чинится не там", substr, v.Blind)
}

// TestProviderWalkInjection_HealthyTreeIsBounded — законный близнец всех инъекций
// ниже: исправный обход при живой поверхности даёт исход «поверхность есть».
func TestProviderWalkInjection_HealthyTreeIsBounded(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	v := JudgeProviderWalk(census, denom)
	if v.Outcome != ProviderOutcomeBounded {
		t.Fatalf("исход %q, ожидался %q; причины: %v", v.Outcome, ProviderOutcomeBounded, v.Blind)
	}
	if len(v.Blind) != 0 {
		t.Fatalf("исправный обход назвал причины слепоты: %v", v.Blind)
	}
}

// TestProviderWalkInjection_EmptySurfaceIsItsOwnNamedOutcome — ПРЕДМЕТ: поверхность
// снята, ведомость пуста, знаменатель доказан.
//
// Это зелёный, но ОТДЕЛЬНЫЙ и названный. Один факт против близнеца выше: мест
// разговора ноль и записей ведомости ноль; знаменатель тот же самый.
func TestProviderWalkInjection_EmptySurfaceIsItsOwnNamedOutcome(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Carriers, census.Reaches = 0, 0
	census.LedgerEntries, census.LedgerSurfaces = 0, 0

	v := JudgeProviderWalk(census, denom)
	if v.Outcome != ProviderOutcomeNoSurface {
		t.Fatalf("исход %q, ожидался %q — «поверхности нет» слито с «поверхность есть», "+
			"и снятие поверхности нечем отличить от обычного зелёного; причины: %v",
			v.Outcome, ProviderOutcomeNoSurface, v.Blind)
	}
	if len(v.Blind) != 0 {
		t.Fatalf("снятая поверхность названа слепотой: %v — проба падает на достижении "+
			"собственной цели", v.Blind)
	}
}

// TestProviderWalkInjection_EmptyDictionaryIsBlind — ПРЕДМЕТ: словарь путей пуст.
//
// Инструмента нет — значит, искать было нечем, и ноль находок ничего не значит.
// Один факт: длина словаря и его доказанный охват обнулены вместе (охват не
// может быть больше словаря).
func TestProviderWalkInjection_EmptyDictionaryIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Carriers, census.Reaches = 0, 0
	census.LedgerEntries, census.LedgerSurfaces = 0, 0
	denom.DictionaryPaths, denom.RecogniserReach = 0, 0

	blindBecause(t, JudgeProviderWalk(census, denom), "словарь")
}

// TestProviderWalkInjection_RecogniserThatFindsNothingIsBlind — словарь на месте, а
// распознаватель не находит на синтетике ни одного его пути.
func TestProviderWalkInjection_RecogniserThatFindsNothingIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	denom.RecogniserReach = 0

	blindBecause(t, JudgeProviderWalk(census, denom), "распознаватель")
}

// TestProviderWalkInjection_PartialRecogniserReachIsBlind — граница предыдущего:
// распознаватель находит все пути, кроме одного.
func TestProviderWalkInjection_PartialRecogniserReachIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	denom.RecogniserReach = denom.DictionaryPaths - 1

	blindBecause(t, JudgeProviderWalk(census, denom), "распознаватель")
}

// TestProviderWalkInjection_ShortWalkIsBlind — ПРЕДМЕТ: обход не дошёл до дерева.
//
// Прочитан один файл при полутора тысячах в коммите. Сегодня это зелёный: порог
// «больше нуля» такой обход пропускает.
func TestProviderWalkInjection_ShortWalkIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files, census.Exempt = 1, 0
	census.Literals, denom.LexicalLiterals = 29, 29
	census.Carriers, census.Reaches = 0, 0
	census.LedgerEntries, census.LedgerSurfaces = 0, 0

	blindBecause(t, JudgeProviderWalk(census, denom), "коммит")
}

// TestProviderWalkInjection_OneFileShortIsBlind — граница предыдущего: обход
// недобрал РОВНО один файл. Порог не имеет запаса, потому что незамеченным
// пропадает именно один файл, а не сотня.
func TestProviderWalkInjection_OneFileShortIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files--

	blindBecause(t, JudgeProviderWalk(census, denom), "коммит")
}

// TestProviderWalkInjection_WalkAheadOfTheCommitIsBlind — зеркальная сторона:
// обход прочитал БОЛЬШЕ, чем лежит в коммите. Так выглядит работа, оставленная
// в индексе: гейт судит коммит, а файл ещё не закоммичен.
func TestProviderWalkInjection_WalkAheadOfTheCommitIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files++

	blindBecause(t, JudgeProviderWalk(census, denom), "коммит")
}

// TestProviderWalkInjection_SkippedFilesAreAccountedNotVanished — законный
// близнец двух предыдущих: файл ушёл под правило игнорирования, и знаменатель
// это ВИДИТ числом, а не разницей.
func TestProviderWalkInjection_SkippedFilesAreAccountedNotVanished(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files -= 7
	denom.SkippedGoFiles = 7

	v := JudgeProviderWalk(census, denom)
	if v.Outcome != ProviderOutcomeBounded {
		t.Fatalf("исход %q, ожидался %q — законное игнорирование прочитано как слепота: %v",
			v.Outcome, ProviderOutcomeBounded, v.Blind)
	}
}

// TestProviderWalkInjection_IgnoreListSwallowingTheTreeIsBlind — правило
// игнорирования отбросило не меньше, чем обход прочитал.
//
// Порог не подогнан под сегодняшнее дерево: перечень игнорирования называет
// вспомогательные деревья (документация, вендоренное, сборочные каталоги), а
// продукт лежит вне их — поэтому отброшенного обязано быть МЕНЬШЕ прочитанного.
// Сегодня отброшенных файлов Go ноль при 1249 прочитанных.
func TestProviderWalkInjection_IgnoreListSwallowingTheTreeIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files = 600
	denom.SkippedGoFiles = 649

	blindBecause(t, JudgeProviderWalk(census, denom), "игнорирован")
}

// TestProviderWalkInjection_IgnoreListMinorityIsSilent — граница предыдущего на
// волосок с законной стороны: отброшено на единицу меньше прочитанного.
func TestProviderWalkInjection_IgnoreListMinorityIsSilent(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files = 626
	denom.SkippedGoFiles = 625
	census.Exempt = 0
	denom.CommitGoFiles = 1251

	v := JudgeProviderWalk(census, denom)
	if v.Outcome == ProviderOutcomeBlind {
		t.Fatalf("отброшено меньше прочитанного, а обход назван несостоявшимся: %v", v.Blind)
	}
}

// TestProviderWalkInjection_ParserLosingLiteralsIsBlind — состав дерева сошёлся,
// а разбор потерял исполняемую часть: лексика видит литералы, дерево разбора —
// нет.
func TestProviderWalkInjection_ParserLosingLiteralsIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Literals = 12

	blindBecause(t, JudgeProviderWalk(census, denom), "лексическ")
}

// TestProviderWalkInjection_ZeroLiteralsIsBlind — вырожденный случай того же:
// литералов ноль при непустом дереве.
func TestProviderWalkInjection_ZeroLiteralsIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Literals = 0

	blindBecause(t, JudgeProviderWalk(census, denom), "литерал")
}

// TestProviderWalkInjection_EmptyCommitIsBlind — в коммите ноль непроверочных
// файлов Go: спрашивать было не у чего.
func TestProviderWalkInjection_EmptyCommitIsBlind(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files, census.Exempt, census.Literals = 0, 0, 0
	census.Carriers, census.Reaches = 0, 0
	census.LedgerEntries, census.LedgerSurfaces = 0, 0
	denom.CommitGoFiles, denom.LexicalLiterals = 0, 0

	blindBecause(t, JudgeProviderWalk(census, denom), "коммит")
}

// TestProviderWalkInjection_EveryBlindReasonIsNamed — гейт, называющий ОДНУ
// причину, прячет остальные: чинить его пришлось бы по одной за прогон.
func TestProviderWalkInjection_EveryBlindReasonIsNamed(t *testing.T) {
	t.Parallel()
	census, denom := healthyWalk()
	census.Files = 1
	census.Literals = 0
	denom.DictionaryPaths, denom.RecogniserReach = 0, 0

	v := JudgeProviderWalk(census, denom)
	if v.Outcome != ProviderOutcomeBlind {
		t.Fatalf("исход %q, ожидался %q", v.Outcome, ProviderOutcomeBlind)
	}
	if len(v.Blind) < 3 {
		t.Fatalf("названо причин %d при трёх внесённых: %v", len(v.Blind), v.Blind)
	}
}

// TestProviderRecogniserReachFindsEveryDictionaryPath — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ
// распознавателя на синтетическом входе.
//
// Утверждение о ДЕРЕВЕ здесь не делается вовсе: вход синтетический, и потому
// контроль остаётся зелёным на дереве, где поверхности больше нет.
func TestProviderRecogniserReachFindsEveryDictionaryPath(t *testing.T) {
	t.Parallel()
	found, missed, err := MeasureRecogniserReach(ProviderSurfaces)
	if err != nil {
		t.Fatalf("положительный контроль: %v", err)
	}
	if len(ProviderSurfaces) == 0 {
		t.Fatal("словарь путей пуст — положительному контролю нечего доказывать, " +
			"и его молчание было бы вакуумным")
	}
	if found != len(ProviderSurfaces) || len(missed) != 0 {
		t.Fatalf("распознаватель находит %d из %d путей словаря; не найдены: %v",
			found, len(ProviderSurfaces), missed)
	}
	t.Logf("положительный контроль: найдено %d из %d путей словаря", found, len(ProviderSurfaces))
}

// TestProviderRecogniserReachInjection_PathOutsideTheDictionaryIsMissed —
// инъекция в контроль: путь, которого распознаватель знать не может, обязан
// попасть в «не найдены». Без этой пробы «найдено всё» неотличимо от «контроль
// зеленеет на чём угодно».
func TestProviderRecogniserReachInjection_PathOutsideTheDictionaryIsMissed(t *testing.T) {
	t.Parallel()
	probe := append([]ProviderSurface{}, ProviderSurfaces...)
	probe = append(probe, ProviderSurface{
		Path: "/admin/path-the-dictionary-does-not-know", What: "инъекция",
	})

	found, missed, err := MeasureRecogniserReach(probe)
	if err != nil {
		t.Fatalf("инъекция в контроль: %v", err)
	}
	if found != len(ProviderSurfaces) {
		t.Fatalf("найдено %d при %d известных путях", found, len(ProviderSurfaces))
	}
	if len(missed) != 1 || missed[0] != "/admin/path-the-dictionary-does-not-know" {
		t.Fatalf("не найденными названы %v — контроль зеленеет на пути, "+
			"которого распознаватель не знает", missed)
	}
}

// TestProviderLexicalCountInjection_SecondExpressionSeesWhatTheFirstSees —
// лексический проход на синтетике: число литералов известно по построению.
func TestProviderLexicalCountInjection_SecondExpressionSeesWhatTheFirstSees(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"a.go": "package a\n\nimport \"strings\"\n\nvar _ = strings.Contains\n" +
			"const x = \"one\"\nconst y = `two`\n",
		"b.go": "package b\n\nconst z = \"three\"\n",
	}
	files, literals, err := MeasureLiteralsLexically(sources, nil)
	if err != nil {
		t.Fatalf("лексический разбор: %v", err)
	}
	if files != 2 || literals != 4 {
		t.Fatalf("файлов %d, литералов %d; ожидалось 2 и 4 (импорт, две константы, "+
			"одна константа)", files, literals)
	}
}

// TestProviderLexicalCountInjection_ExemptFileIsNotCounted — законный близнец:
// послабление снимает файл с обоих выражений одинаково, иначе пара разойдётся
// на исправном дереве.
func TestProviderLexicalCountInjection_ExemptFileIsNotCounted(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"a.go": "package a\n\nconst x = \"one\"\n",
		"b.go": "package b\n\nconst z = \"three\"\n",
	}
	files, literals, err := MeasureLiteralsLexically(sources, func(p string) bool { return p == "b.go" })
	if err != nil {
		t.Fatalf("лексический разбор: %v", err)
	}
	if files != 1 || literals != 1 {
		t.Fatalf("файлов %d, литералов %d; ожидалось 1 и 1", files, literals)
	}
}

// TestProviderLexicalCountInjection_UnreadableSourceIsAnError — молчаливый
// пропуск нечитаемого файла превратил бы знаменатель в «сколько удалось».
func TestProviderLexicalCountInjection_UnreadableSourceIsAnError(t *testing.T) {
	t.Parallel()
	_, _, err := MeasureLiteralsLexically(map[string]string{
		"broken.go": "package b\n\nconst x = \"незакрытая строка\n",
	}, nil)
	if err == nil {
		t.Fatal("лексический разбор проглотил незакрытый литерал — файл, который " +
			"не прочитался, прошёл бы за прочитанный")
	}
}
