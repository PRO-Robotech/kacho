// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestformpin_injection_test.go — доказательство падучести гейта
// TestModuleManifestFormIsJudgedByTheOneExecutor.
//
// Гейт, чью способность падать никто не проверял, неотличим от мёртвого: он
// молчит одинаково и на исправном дереве, и на сломанном. Поэтому по каждой оси
// здесь стоит ПАРА — внесённый дефект и ЗАКОННЫЙ БЛИЗНЕЦ той же формы, на
// котором судья обязан смолчать.
//
// # Почему инъекция идёт по СИНТЕТИЧЕСКОМУ дереву, а не по живому
//
// Судья читает состав КОММИТА (`git ls-files`), а не диска. Правка живого дерева
// ради доказательства сделала бы вердикт функцией рабочего каталога и трогала
// бы копию, в которой работают соседние полосы. Синтетический репозиторий живёт
// в каталоге прогона и снимается вместе с ним.
//
// # Третья категория доказывается ОТДЕЛЬНЫМ прогоном, а не рассуждением
//
// «Судить было нечего» обязано приходить своим кодом и НЕ засчитываться в
// успех. Ось D подаёт судье корень без единого манифеста и требует ровно этого:
// код, отличный и от годного, и от находки, и вердикт «не зелёное».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestFormFixture — синтетическое дерево с одним манифестом домена и
// собранный судья. Тело манифеста берётся ИЗ ЖИВОГО дерева: выдуманное не
// доказало бы ничего о документах, которые гейт судит на самом деле.
func manifestFormFixture(t *testing.T, mutate func(string) string) (root, bin string) {
	t.Helper()
	live := repoRoot(t)
	bin = buildManifestFormJudge(t, live)

	src := filepath.Join(live, "services", "vpc", "manifest.yaml")
	body, err := os.ReadFile(src) // #nosec G304 -- путь собран из корня дерева и постоянных сегментов
	if err != nil {
		t.Fatalf("живой манифест не прочитан (%s): %v", src, err)
	}

	root = t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	dst := filepath.Join(root, "services", "vpc", "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatalf("каталог фикстуры не заведён: %v", err)
	}
	text := string(body)
	if mutate != nil {
		text = mutate(text)
	}
	if err := os.WriteFile(dst, []byte(text), 0o600); err != nil {
		t.Fatalf("манифест фикстуры не записан: %v", err)
	}
	git(t, root, "add", "-A")
	return root, bin
}

// TestManifestFormJudgeFindsAnUnknownKey — ОСЬ A: ключ, которого схема не знает,
// есть находка, и судья называет КООРДИНАТУ.
//
// Ключ, а не опечатка в значении: схема закрыта (`KnownFields`), и именно это
// свойство второй судья повторить не смог бы, не разойдясь с оригиналом.
func TestManifestFormJudgeFindsAnUnknownKey(t *testing.T) {
	t.Parallel()
	root, bin := manifestFormFixture(t, func(s string) string {
		return s + "\nunknownKeyTheSchemaDoesNotDeclare: 1\n"
	})

	out, code := runManifestFormJudge(t, bin, root)
	if code != ManifestFormJudgeFinding {
		t.Fatalf("судья НЕ нашёл неизвестного ключа: код %d, ожидался %d (находка)\n%s",
			code, ManifestFormJudgeFinding, strings.TrimSpace(out))
	}
	if !strings.Contains(out, "services/vpc/manifest.yaml") {
		t.Fatalf("находка не назвала координату документа — читателю негде искать\n%s",
			strings.TrimSpace(out))
	}
	if _, _, green := ManifestFormJudgeOutcome(code); green {
		t.Fatalf("находка прочитана как ЗЕЛЁНОЕ — классификатор исходов неверен (код %d)", code)
	}

	census, err := ParseManifestFormCensus(out)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if census.ManifestsRead != 1 || census.Findings == 0 {
		t.Fatalf("перепись находки неполна: %s — прочитанных обязано быть 1, находок не ноль",
			census)
	}
}

// TestManifestFormJudgeIsSilentOnTheLegitimateTwin — ОСЬ B: тот же документ БЕЗ
// правки молчит.
//
// Без этой оси ось A доказывала бы только то, что судья умеет краснеть, — а
// краснеть на всём умеет и сломанный.
func TestManifestFormJudgeIsSilentOnTheLegitimateTwin(t *testing.T) {
	t.Parallel()
	root, bin := manifestFormFixture(t, nil)

	out, code := runManifestFormJudge(t, bin, root)
	if code != ManifestFormJudgeOK {
		t.Fatalf("судья нашёл находку на НЕТРОНУТОМ документе: код %d\n%s",
			code, strings.TrimSpace(out))
	}
	census, err := ParseManifestFormCensus(out)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if census.ManifestsRead != 1 {
		t.Fatalf("законный близнец прочитан не один раз: %s — молчание над нулём "+
			"прочитанного доказывает не молчание судьи, а его слепоту", census)
	}
	if census.Findings != 0 {
		t.Fatalf("перепись называет находки на нетронутом документе: %s", census)
	}
}

// TestManifestFormJudgeIsSilentOnACommentOnlyChange — ОСЬ C: правка ПРОЗЫ
// документа молчит.
//
// Ось отделяет «судья судит форму» от «судья сверяет байты»: комментарий формы
// не меняет, и находка на нём означала бы, что судья измеряет не то.
func TestManifestFormJudgeIsSilentOnACommentOnlyChange(t *testing.T) {
	t.Parallel()
	root, bin := manifestFormFixture(t, func(s string) string {
		return "# строка прозы, заведённая инъекцией: формы документа она не меняет\n" + s
	})

	out, code := runManifestFormJudge(t, bin, root)
	if code != ManifestFormJudgeOK {
		t.Fatalf("судья краснеет на правке КОММЕНТАРИЯ (код %d) — значит он сверяет байты, "+
			"а не судит форму\n%s", code, strings.TrimSpace(out))
	}
}

// TestManifestFormJudgeSaysVoidWhenThereIsNothingToJudge — ОСЬ D: ТРЕТЬЯ
// КАТЕГОРИЯ приходит своим кодом и в успех не засчитывается.
//
// Корень без единого манифеста — это «условие не создано», а не «форма годна».
// Схлопни мы его в зелёное, пустое дерево отчитывалось бы так же уверенно, как
// проверенное, и первый документ, положенный мимо ожидаемого имени, остался бы
// невидимым навсегда.
func TestManifestFormJudgeSaysVoidWhenThereIsNothingToJudge(t *testing.T) {
	t.Parallel()
	bin := buildManifestFormJudge(t, repoRoot(t))

	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	// Один отслеживаемый файл, НЕ являющийся манифестом: иначе доказывалось бы
	// «индекс пуст», а доказать надо «манифеста нет».
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("тут манифеста нет\n"), 0o600); err != nil {
		t.Fatalf("файл фикстуры не записан: %v", err)
	}
	git(t, root, "add", "-A")

	out, code := runManifestFormJudge(t, bin, root)
	if code != ManifestFormJudgeVoid {
		t.Fatalf("корень БЕЗ манифестов дал код %d, а обязан был дать %d («судить нечего»): "+
			"третья категория схлопнута в вердикт\n%s",
			code, ManifestFormJudgeVoid, strings.TrimSpace(out))
	}
	name, _, green := ManifestFormJudgeOutcome(code)
	if green {
		t.Fatalf("«судить нечего» прочитано как ЗЕЛЁНОЕ (%s) — «ноль находок» стало "+
			"неотличимо от «ноль прочитанного»", name)
	}
	census, err := ParseManifestFormCensus(out)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if census.ManifestsRead != 0 {
		t.Fatalf("судья объявил «нечего судить», прочитав %d документов: %s", census.ManifestsRead, census)
	}
}

// TestManifestFormCensusFollowsTheTree — ОСЬ E: перепись СЛЕДУЕТ за деревом.
//
// Второе утверждение гейта («прочитано ровно столько, сколько в индексе»)
// осмысленно лишь тогда, когда читаемая величина от дерева ЗАВИСИТ. Если бы она
// была константой, сравнение выполнялось бы тождественно и не сужало ничего.
func TestManifestFormCensusFollowsTheTree(t *testing.T) {
	t.Parallel()
	one, bin := manifestFormFixture(t, nil)

	outOne, codeOne := runManifestFormJudge(t, bin, one)
	if codeOne != ManifestFormJudgeOK {
		t.Fatalf("фикстура с одним документом не зелена: код %d\n%s", codeOne, strings.TrimSpace(outOne))
	}
	censusOne, err := ParseManifestFormCensus(outOne)
	if err != nil {
		t.Fatalf("%v", err)
	}

	live := repoRoot(t)
	outLive, codeLive := runManifestFormJudge(t, bin, live)
	if codeLive != ManifestFormJudgeOK {
		t.Fatalf("живое дерево не зелено: код %d\n%s", codeLive, strings.TrimSpace(outLive))
	}
	censusLive, err := ParseManifestFormCensus(outLive)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if censusOne.ManifestsRead == censusLive.ManifestsRead {
		t.Fatalf("перепись НЕ следует за деревом: на фикстуре с одним документом и на живом "+
			"дереве она дала одно число (%d). Сравнение «прочитано = лежит в индексе» тогда "+
			"выполняется тождественно и не сужает ничего\n  фикстура: %s\n  дерево: %s",
			censusOne.ManifestsRead, censusOne, censusLive)
	}
}

// TestManifestFormJudgeOutcomeNamesEachExitCodeApart — ОСЬ F: у каждого кода
// возврата своё имя, и зелёный ровно один.
//
// Ось стережёт классификатор, а не судью: схлопнув два исхода в одно имя, он
// вернул бы «ненулевой код» — ровно то, ради чего гейт заведён.
func TestManifestFormJudgeOutcomeNamesEachExitCodeApart(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	greens := 0
	for _, code := range []int{ManifestFormJudgeOK, ManifestFormJudgeFinding, ManifestFormJudgeVoid, 7} {
		name, meaning, green := ManifestFormJudgeOutcome(code)
		if name == "" || meaning == "" {
			t.Fatalf("код %d не назван словами: имя %q, смысл %q", code, name, meaning)
		}
		if prev, dup := seen[name]; dup {
			t.Fatalf("коды %d и %d названы ОДИНАКОВО (%q) — исходы схлопнуты", prev, code, name)
		}
		seen[name] = code
		if green {
			greens++
		}
	}
	if greens != 1 {
		t.Fatalf("зелёных исходов %d, а обязан быть ровно один: «судить нечего» и "+
			"неизвестный код в успех не засчитываются", greens)
	}
}

// TestParseManifestFormCensusRefusesAnUnparsableOutput — ОСЬ G: неразобранная
// перепись есть ОТКАЗ, а не нули.
//
// Формат вывода принадлежит чужому репозиторию. Молчаливые нули при смене
// формата прошли бы за «судить было нечего», то есть расхождение формата выдало
// бы себя за свойство дерева.
func TestParseManifestFormCensusRefusesAnUnparsableOutput(t *testing.T) {
	t.Parallel()
	if _, err := ParseManifestFormCensus("всё хорошо, честное слово\n"); err == nil {
		t.Fatal("перепись разобрана из вывода, которого в ней нет: смена формата у соседа " +
			"прошла бы за «судить было нечего»")
	}
	// Законный близнец: настоящая строка переписи обязана разобраться.
	good := "перепись: осмотрено файлов 6515 · каталогов пропущено 1 · манифестов прочитано 5 · " +
		"находок 0 · таблица типов: образ"
	c, err := ParseManifestFormCensus(good)
	if err != nil {
		t.Fatalf("настоящая строка переписи не разобрана: %v", err)
	}
	if c.FilesWalked != 6515 || c.ManifestsRead != 5 || c.Findings != 0 {
		t.Fatalf("величины переписи разобраны неверно: %s", c)
	}
}
