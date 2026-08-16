// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// excludingbuildtag_injection_test.go — доказательство, что гейт «исключающий
// признак сборки не оставляет пакет несобираемым» СПОСОБЕН упасть и способен
// смолчать.
//
// # Почему инъекция настоящим входом
//
// Предикат гейта — вызов настоящего компилятора. Проба, подменившая этот вызов
// заглушкой, доказала бы, что заглушка возвращает положенное в неё. Поэтому здесь
// строится НАСТОЯЩИЙ модуль Go: с `go.mod`, с индексом git (судья спрашивает
// индекс, а не диск) и с файлом проб под исключающим признаком. Внешних
// зависимостей у модуля нет — сборка идёт без сети.
//
// # Почему без инъекции у гейта не было бы предмета вовсе
//
// После правки #493 исключающих признаков в дереве НОЛЬ, и это цель, а не
// поломка. На пустой ведомости судья не делает ни одного вызова сборки — то есть
// прогон по дереву не доказывает о нём ничего. Всё доказательство живёт здесь.
//
// # Обе стороны и почему одной мало
//
// Дефект и его ЗАКОННЫЙ БЛИЗНЕЦ отличаются ОДНИМ: у близнеца пользователь символа
// выводится из сборки ТЕМ ЖЕ признаком, что и его определение. Всё остальное —
// форма признака, имена, раскладка — совпадает. Без близнеца гейт ловил бы саму
// форму «в дереве есть исключающий признак» и краснел бы на каждом законном; такой
// гейт снимается первым же ложным срабатом.
//
// Второй законный близнец — платформенный (`!windows`): та же форма, но пара к
// нему выбирается по ИМЕНИ файла, а не тегом. Гейт обязан молчать и на нём, иначе
// первая же платформенная развилка сделает его красным на ровном месте.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthExcludingModule — минимальный модуль: прод-файл, файл с ИСКЛЮЧАЮЩИМ
// признаком, определяющий символ, и файл, этот символ зовущий. Вызывающий задаёт
// шапку зовущего файла — единственное, чем дефект отличается от близнеца.
func synthExcludingModule(t *testing.T, definerHeader, callerHeader string) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod": "module synthexcluding\n\ngo 1.25\n",
		"pkg/thing.go": "" +
			"package pkg\n\n" +
			"type Thing struct{}\n",
		"pkg/definer_test.go": definerHeader +
			"package pkg\n\n" +
			"func helperValue() int { return 1 }\n",
		"pkg/caller_test.go": callerHeader +
			"package pkg\n\n" +
			"import \"testing\"\n\n" +
			"func TestUsesHelper(t *testing.T) {\n" +
			"\tif helperValue() != 1 {\n" +
			"\t\tt.Fatal(\"helperValue\")\n" +
			"\t}\n" +
			"}\n",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// auditSynthExcluding — ТОТ ЖЕ судья, что судит дерево, плюс проверка предпосылки
// самой инъекции: синтетика обязана быть ПРОЧИТАНА, её признак — РАСПОЗНАН, а
// сборка — ПОЗВАНА. Иначе «ноль находок» на ней не значит ничего.
func auditSynthExcluding(t *testing.T, root string, wantVetRuns bool) ([]excludingTagFinding, excludingTagCensus) {
	t.Helper()
	findings, census, err := auditExcludingBuildTags(root)
	if err != nil {
		t.Fatalf("судья сорвался на синтетическом дереве: %v", err)
	}
	if census.GoFilesRead == 0 || census.FilesWithConstraint == 0 {
		t.Fatalf("синтетическое дерево не осмотрено — вердикт на нём недействителен: %s", census)
	}
	if wantVetRuns && census.VetRuns == 0 {
		t.Fatalf("сборка не позвана ни разу — судья не дошёл до своего предиката: %s", census)
	}
	return findings, census
}

// TestExcludingBuildTagGateRedOnInjectedDefect — направление (а): символ
// определён в файле, который тег выводит из сборки, а зовущий его файл остаётся, —
// гейт КРАСНЕЕТ и НАЗЫВАЕТ КООРДИНАТУ. Находка без координаты не есть действие.
func TestExcludingBuildTagGateRedOnInjectedDefect(t *testing.T) {
	root := synthExcludingModule(t, "//go:build !synthexclude\n\n", "")

	findings, census := auditSynthExcluding(t, root, true)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("пакет под исключающим признаком не собирается, а гейт молчит — "+
			"он не способен упасть.\n%s", census)
	}
	joined := joinExcludingTagFindings(findings)
	if !strings.Contains(joined, "pkg") {
		t.Fatalf("находка не называет пакет — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "definer_test.go") {
		t.Fatalf("находка не называет файл, чей признак исключает символ:\n%s", joined)
	}
	if !strings.Contains(joined, "synthexclude") {
		t.Fatalf("находка не называет тег — непонятно, какой вызов её воспроизводит:\n%s", joined)
	}
	// Текст компилятора обязан доехать до читателя: без него находка сообщает
	// «не собирается» и не сообщает ЧЕМ, а диагноз ставится по тексту отказа.
	if !strings.Contains(joined, "helperValue") {
		t.Fatalf("находка не несёт текста компилятора — диагноз по ней не поставить:\n%s", joined)
	}
	t.Logf("направление (а): гейт покраснел и назвал координату:\n%s", joined)
}

// TestExcludingBuildTagGateSilentOnLawfulTwin — направление (б): тот же признак,
// та же форма, но из сборки выводится И пользователь символа — гейт МОЛЧИТ.
//
// Это и есть законное употребление исключающего признака: он уносит связку
// целиком, а не половину.
func TestExcludingBuildTagGateSilentOnLawfulTwin(t *testing.T) {
	root := synthExcludingModule(t,
		"//go:build !synthexclude\n\n",
		"//go:build !synthexclude\n\n")

	findings, census := auditSynthExcluding(t, root, true)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("законная связка под исключающим признаком помечена находкой — гейт "+
			"ловит форму, а не существо:\n%s", joinExcludingTagFindings(findings))
	}
	if census.ExcludableFiles != 2 {
		t.Fatalf("оба файла обязаны быть распознаны исключаемыми, распознано %d: %s",
			census.ExcludableFiles, census)
	}
}

// TestExcludingBuildTagGateSilentOnPlatformSplit — второй законный близнец:
// платформенная развилка.
//
// `!windows` формально исключающий, но пара к нему выбирается по ИМЕНИ файла, а
// не тегом, поэтому сборка с `-tags=windows` нашла бы отсутствующее определение и
// дала находку на совершенно законном коде. Имя платформы гейт обязан отбросить —
// и обязан СКАЗАТЬ, что отбросил, иначе молчание неотличимо от непрочтения.
func TestExcludingBuildTagGateSilentOnPlatformSplit(t *testing.T) {
	root := synthExcludingModule(t, "//go:build !windows\n\n", "")

	findings, census := auditSynthExcluding(t, root, false)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("платформенная развилка помечена находкой:\n%s", joinExcludingTagFindings(findings))
	}
	if census.VetRuns != 0 {
		t.Fatalf("сборка позвана с именем платформы — вызов проверял бы не то: %s", census)
	}
	if len(census.SkippedPlatformTags) == 0 {
		t.Fatalf("гейт промолчал, но не назвал причину: перечень отброшенных пуст (%s). "+
			"Молчание без переписи неотличимо от непрочтения", census)
	}
}

// TestExcludingBuildTagGateReadsOnlyTheHeader — предпосылка судьи: `//go:build`
// НИЖЕ объявления пакета инструментом не читается, и судья обязан читать так же.
//
// Не будь этой пробы, судья мог бы «найти» признак в комментарии посреди файла и
// позвать сборку с тегом, которого в дереве нет: вызов прошёл бы молча, и гейт
// стал бы проверкой с формой, но без содержания.
func TestExcludingBuildTagGateReadsOnlyTheHeader(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module synthexcluding\n\ngo 1.25\n",
		"pkg/thing.go": "package pkg\n\n" +
			"// Ниже — не признак сборки, а разговор о нём:\n" +
			"//go:build !synthexclude\n\n" +
			"type Thing struct{}\n",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)

	findings, census, err := auditExcludingBuildTags(root)
	if err != nil {
		t.Fatalf("судья сорвался: %v", err)
	}
	t.Log(census.String())

	if census.GoFilesRead == 0 {
		t.Fatalf("синтетическое дерево не прочитано: %s", census)
	}
	if census.FilesWithConstraint != 0 {
		t.Fatalf("строка ниже объявления пакета принята за признак сборки — судья читает "+
			"не то, что читает инструмент: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("находка на файле без признака сборки:\n%s", joinExcludingTagFindings(findings))
	}
}

func joinExcludingTagFindings(findings []excludingTagFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}
