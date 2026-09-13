// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/modulemanifest"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// modelcanonpin_injection_test.go — доказательство того, что судья сверки
// СПОСОБЕН УПАСТЬ, и что падает он на своём предмете, а не на соседнем.
//
// Инъекция ведётся в ОБЕ стороны и настоящим входом — собранным корнем из
// настоящих манифестов дерева и настоящего канона из пина, а не синтетикой:
// набор модулей знает исполнитель, и выдуманное имя модуля он отверг бы
// по другому поводу, чем проверяемый.
//
// # Почему дефект — снятая СТРОКА, а не снятый ресурс
//
// Одна строка есть ОДИН факт. Снятый ресурс меняет и состав раздела, и блок
// разом, и тогда неизвестно, какой из двух дал красное. Признак одно-фактности
// здесь ещё и ИЗМЕРЯЕТСЯ: блоков, принадлежащих модулям, остаётся столько же,
// а сверенных становится ровно на один меньше.
//
// # Законный близнец
//
// Комментарий, дописанный в манифест, меняет ФАЙЛ и не меняет ни одного блока.
// На нём судья обязан молчать, и перепись обязана совпасть с непорченой
// ПОБАЙТОВО: иначе сверка шла бы по тексту документа, а не по порождённому из
// него, и всякая правка прозы краснела бы.

// canonJudgeFixture — собранный корень из настоящего дерева и собранный из пина
// исполнитель. Возвращает корень, путь двоичного и перепись сборки.
func canonJudgeFixture(t *testing.T) (root, bin string) {
	t.Helper()
	repo := repoRoot(t)

	manifests := platformDomainManifests(t, repo)
	if len(manifests) == 0 {
		t.Fatal("манифестов домена в индексе нет — инъекции не во что вносить дефект")
	}
	canonSrc, err := contractsource.Path(repo, ModelCanonRelToProto)
	if err != nil {
		t.Fatalf("канон не резолвится: %v", err)
	}
	dst := t.TempDir()
	if _, err := ComposeCanonJudgeRoot(dst, manifests, canonSrc, kanameModuleManifest(t, repo)); err != nil {
		t.Fatalf("корень сверки не собран: %v", err)
	}
	return dst, buildCanonJudge(t, repo)
}

// canonJudgeVictim — манифест, в который вносится дефект. Существование
// проверяется: инъекция, не нашедшая своего предмета, молча стала бы холостой и
// «доказывала» бы способность падать, ничего не тронув.
func canonJudgeVictim(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "services", "vpc", modulemanifest.FileName)
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("манифест, в который вносится дефект, не найден в собранном корне (%s): "+
			"инъекция была бы холостой и зеленела бы, ничего не тронув", p)
	}
	return p
}

// dropOneLine — снять ПЕРВОЕ вхождение строки. Отсутствие строки — отказ: это
// предпосылка инъекции, и её молчаливый пропуск дал бы холостой прогон.
func dropOneLine(t *testing.T, path, line string) {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- путь собранного корня пробы
	if err != nil {
		t.Fatalf("манифест не прочитан (%s): %v", path, err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if l == line {
			out := strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
			if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
				t.Fatalf("манифест не записан (%s): %v", path, err)
			}
			return
		}
	}
	t.Fatalf("строки %q в манифесте нет (%s) — предпосылка инъекции не выполнена, и "+
		"прогон без неё был бы холостым", line, path)
}

// runCanonJudgeCensus — прогон плюс разобранная перепись.
func runCanonJudgeCensus(t *testing.T, bin, root string) (code, blocks, owned, bytesCompared int, out string) {
	t.Helper()
	out, code = runCanonJudge(t, bin, root)
	blocks, owned, bytesCompared, err := ParseCanonJudgeCensus(out)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return code, blocks, owned, bytesCompared, out
}

// TestCanonJudgeFindsAManifestThatLostAVerb — ДЕФЕКТ: манифест перестал
// порождать глагол, который канон объявляет.
//
// Утверждается не только красное, но и КООРДИНАТА: находка без имени модуля и
// типа посылает читателя искать вручную.
func TestCanonJudgeFindsAManifestThatLostAVerb(t *testing.T) {
	t.Parallel()
	root, bin := canonJudgeFixture(t)

	cleanCode, cleanBlocks, cleanOwned, cleanBytes, _ := runCanonJudgeCensus(t, bin, root)
	if cleanCode != CanonJudgeOK {
		t.Fatalf("контроль: непорченый корень обязан быть зелёным, а код %d — инъекция "+
			"доказывала бы способность падать на уже красном", cleanCode)
	}

	dropOneLine(t, canonJudgeVictim(t, root), "      - delete")

	code, blocks, owned, bytesCompared, out := runCanonJudgeCensus(t, bin, root)
	if code != CanonJudgeFinding {
		t.Fatalf("дефект внесён, а судья вышел кодом %d вместо %d (находка)\n%s",
			code, CanonJudgeFinding, strings.TrimSpace(out))
	}
	for _, want := range []string{"vpc", "vpc_network", "v_delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("находка не называет %q — координата дефекта не напечатана\n%s",
				want, strings.TrimSpace(out))
		}
	}

	// ОДНО-ФАКТНОСТЬ, измеренная: принадлежащих модулям блоков столько же,
	// сверенных — ровно на один меньше. Инъекция, уронившая соседние блоки,
	// доказывала бы способность падать на чужом предмете.
	if owned != cleanOwned {
		t.Errorf("инъекция сдвинула состав принадлежащих блоков: было %d, стало %d — "+
			"дефект тронул больше одного факта", cleanOwned, owned)
	}
	if blocks != cleanBlocks-1 {
		t.Errorf("сверенных блоков %d, ожидалось ровно на один меньше контроля (%d) — "+
			"инъекция не одно-фактна", blocks, cleanBlocks-1)
	}
	if bytesCompared >= cleanBytes {
		t.Errorf("байт сверено %d, у контроля %d — дефект не уменьшил объём сверенного",
			bytesCompared, cleanBytes)
	}
}

// TestCanonJudgeIsSilentOnACommentOnlyChange — ЗАКОННЫЙ БЛИЗНЕЦ: правка прозы
// манифеста не меняет ни одного блока, и судья обязан молчать.
//
// Перепись обязана совпасть с непорченой ПОБАЙТОВО: расхождение означало бы,
// что сверяется текст документа, а не порождённое из него.
func TestCanonJudgeIsSilentOnACommentOnlyChange(t *testing.T) {
	t.Parallel()
	root, bin := canonJudgeFixture(t)

	cleanCode, cleanBlocks, cleanOwned, cleanBytes, _ := runCanonJudgeCensus(t, bin, root)
	if cleanCode != CanonJudgeOK {
		t.Fatalf("контроль: непорченый корень обязан быть зелёным, а код %d", cleanCode)
	}

	victim := canonJudgeVictim(t, root)
	b, err := os.ReadFile(victim) // #nosec G304 -- путь собранного корня пробы
	if err != nil {
		t.Fatalf("манифест не прочитан: %v", err)
	}
	const note = "\n# строка прозы, не меняющая ни одного блока модели\n"
	if err := os.WriteFile(victim, append(b, []byte(note)...), 0o600); err != nil {
		t.Fatalf("манифест не записан: %v", err)
	}

	code, blocks, owned, bytesCompared, out := runCanonJudgeCensus(t, bin, root)
	if code != CanonJudgeOK {
		t.Fatalf("законный близнец дал код %d: судья краснеет на правке прозы, то есть "+
			"сверяет текст документа вместо порождённого из него\n%s",
			code, strings.TrimSpace(out))
	}
	if blocks != cleanBlocks || owned != cleanOwned || bytesCompared != cleanBytes {
		t.Errorf("перепись сдвинулась на правке прозы: было %d из %d, %d байт; стало %d из %d, %d байт",
			cleanBlocks, cleanOwned, cleanBytes, blocks, owned, bytesCompared)
	}
}

// TestCanonJudgeDoesNotPassWithoutTheCanon — второй операнд снят: судья обязан
// НЕ БЫТЬ ЗЕЛЁНЫМ.
//
// Это та самая ось, ради которой заведён весь файл: до него исполнитель не
// приезжал в это дерево вовсе, и отсутствие сверки выглядело как её отсутствие
// находок.
func TestCanonJudgeDoesNotPassWithoutTheCanon(t *testing.T) {
	t.Parallel()
	root, bin := canonJudgeFixture(t)

	canon := filepath.Join(root, "proto", filepath.FromSlash(ModelCanonRelToProto))
	if err := os.Remove(canon); err != nil {
		t.Fatalf("канон не снят из собранного корня (%s): %v", canon, err)
	}

	out, code := runCanonJudge(t, bin, root)
	if _, _, green := CanonJudgeOutcome(code); green {
		t.Fatalf("канона нет, а судья вышел ЗЕЛЁНЫМ (код %d) — «сверять нечем» подано "+
			"как «расхождений нет»\n%s", code, strings.TrimSpace(out))
	}
}

// TestComposeCanonJudgeRootRefusesAnEmptyManifestSet — пустой обход есть ОТКАЗ,
// а не пустой корень.
func TestComposeCanonJudgeRootRefusesAnEmptyManifestSet(t *testing.T) {
	t.Parallel()
	repo := repoRoot(t)
	canonSrc, err := contractsource.Path(repo, ModelCanonRelToProto)
	if err != nil {
		t.Fatalf("канон не резолвится: %v", err)
	}
	if _, err := ComposeCanonJudgeRoot(t.TempDir(), nil, canonSrc, ""); err == nil {
		t.Fatal("корень собран из НУЛЯ манифестов: сверка на нём дала бы зелёное по " +
			"непрочитанному, и отличить его от настоящего зелёного было бы нечем")
	}

	// Законный близнец: ровно один манифест — корень собирается.
	one := platformDomainManifests(t, repo)
	if len(one) == 0 {
		t.Fatal("манифестов домена в индексе нет — близнецу не на чем стоять")
	}
	if _, err := ComposeCanonJudgeRoot(t.TempDir(), one[:1], canonSrc, ""); err != nil {
		t.Errorf("корень из одного манифеста собран не был: %v — отказ на непустом наборе "+
			"означал бы, что проверка пустоты ловит форму, а не существо", err)
	}
}

// TestComposeCanonJudgeRootRefusesCollidingDirectories — два манифеста из
// одноимённых каталогов затёрли бы друг друга, и один модуль ушёл бы из-под
// сверки МОЛЧА.
func TestComposeCanonJudgeRootRefusesCollidingDirectories(t *testing.T) {
	t.Parallel()
	repo := repoRoot(t)
	canonSrc, err := contractsource.Path(repo, ModelCanonRelToProto)
	if err != nil {
		t.Fatalf("канон не резолвится: %v", err)
	}

	a := filepath.Join(t.TempDir(), "vpc", modulemanifest.FileName)
	b := filepath.Join(t.TempDir(), "vpc", modulemanifest.FileName)
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог фикстуры: %v", err)
		}
		if err := os.WriteFile(p, []byte("module: vpc\n"), 0o600); err != nil {
			t.Fatalf("манифест фикстуры: %v", err)
		}
	}
	if _, err := ComposeCanonJudgeRoot(t.TempDir(), []string{a, b}, canonSrc, ""); err == nil {
		t.Fatal("столкновение каталогов принято: один манифест затёр бы другой, и его " +
			"модуль ушёл бы из-под сверки без единой находки")
	}
}

// TestParseCanonJudgeCensusRefusesAnUnparsableOutput — неразобранная перепись
// есть ОТКАЗ, а не нули.
//
// Формат вывода принадлежит чужому репозиторию. Молчаливые нули прошли бы за
// «сверять было нечего», то есть смена формата выдала бы себя за свойство
// дерева.
func TestParseCanonJudgeCensusRefusesAnUnparsableOutput(t *testing.T) {
	t.Parallel()

	if _, _, _, err := ParseCanonJudgeCensus("перепись: формат сменился, величин нет"); err == nil {
		t.Error("неразобранная перепись принята: объём осмотренного назвать нечем, а " +
			"вердикт был бы вынесен")
	}

	// Законный близнец: настоящая строка переписи разбирается, и величины те.
	const real = "перепись: модулей набора 6 · манифестов найдено 6 · прощено ведомостью 0 · " +
		"блоков сверено 26 из 27 принадлежащих модулям · байт сверено 79306 · блоков вне модулей 5"
	blocks, owned, bytesCompared, err := ParseCanonJudgeCensus(real)
	if err != nil {
		t.Fatalf("настоящая перепись не разобрана: %v", err)
	}
	if blocks != 26 || owned != 27 || bytesCompared != 79306 {
		t.Errorf("разобрано %d из %d, %d байт; ожидалось 26 из 27, 79306", blocks, owned, bytesCompared)
	}
}

// TestCanonJudgeOutcomeNamesEachExitCodeApart — четыре исхода не схлопываются, и
// зелёный ровно один.
func TestCanonJudgeOutcomeNamesEachExitCodeApart(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for _, code := range []int{CanonJudgeOK, CanonJudgeFinding, CanonJudgeVoid, CanonJudgeNotRun, 7} {
		name, meaning, green := CanonJudgeOutcome(code)
		if name == "" || meaning == "" {
			t.Errorf("код %d не назван словами", code)
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("коды %d и %d названы одинаково (%q) — исходы схлопнуты", prev, code, name)
		}
		seen[name] = code
		if green != (code == CanonJudgeOK) {
			t.Errorf("код %d признан зелёным=%t: зелёный обязан быть ровно один — "+
				"«сверять нечего» и «не исполнялась» в успех не засчитываются", code, green)
		}
	}
}
