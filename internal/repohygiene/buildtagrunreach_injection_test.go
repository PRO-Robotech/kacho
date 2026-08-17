// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtagrunreach_injection_test.go — доказательство, что гейт «пакет под
// признаком сборки достижим объявленным прогоном» СПОСОБЕН упасть и способен
// смолчать.
//
// # Почему инъекция настоящим входом
//
// Предикат гейта — чтение НАСТОЯЩИХ объявлений. Проба, подсунувшая судье готовый
// список прогонов, доказала бы, что список содержит то, что в него положили.
// Здесь поэтому строится настоящее дерево: `go.mod`, индекс git (судья
// спрашивает индекс, а не диск), Makefile с вызовом и пакеты с признаком.
//
// # Обе стороны и почему одной мало
//
// Дефект и его ЗАКОННЫЙ БЛИЗНЕЦ отличаются одним — местом пакета относительно
// области прогона. Всё остальное совпадает: тот же признак, та же форма файла,
// то же объявление. Без близнеца гейт ловил бы форму «в дереве есть признак
// сборки» и краснел бы на каждом законном теге; первый же ложный срабат такой
// гейт выключает.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const synthReachModule = "synthreach"

// synthReachTree — дерево с объявлением прогона и произвольным набором пакетов
// под признаком.
//
// Признак назван `synthtag`, а не `integration`: настоящий тег дерева здесь
// означал бы, что проба зависит от того, жив ли этот тег в дереве. Класс же —
// про ЛЮБОЙ признак сборки.
func synthReachTree(t *testing.T, makefile string, taggedPkgs []string) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod":   "module " + synthReachModule + "\n\ngo 1.24\n",
		"Makefile": makefile,
	}
	for _, pkg := range taggedPkgs {
		files[pkg+"/probe_test.go"] = "//go:build synthtag\n\n" +
			"package " + filepath.Base(pkg) + "\n\n" +
			"import \"testing\"\n\n" +
			"func TestSynth(t *testing.T) { _ = t }\n"
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

// makefileWithScope — объявление в той же форме, что стоит в корневом Makefile
// дерева: `go list` задаёт область, `grep -E` сужает её, `xargs` кормит `go test`.
func makefileWithScope() string {
	return "test-integration:\n" +
		"\t@all=$$($(GO) list ./services/$(SVC)/...); \\\n" +
		"\tpkgs=$$(printf '%s\\n' \"$$all\" | grep -E '/internal/(repo|clients)(/|$$$$)'); \\\n" +
		"\techo \"$$pkgs\" | xargs $(GO) test -tags=synthtag -race -count=1\n"
}

// auditSynthReach — ТОТ ЖЕ судья, что судит дерево, плюс проверка предпосылки
// самой инъекции: синтетика обязана быть ПРОЧИТАНА, её признак — РАСПОЗНАН, и
// объявление — найдено. Иначе «ноль находок» на ней не значит ничего.
func auditSynthReach(t *testing.T, root string) ([]tagRunFinding, tagRunCensus) {
	t.Helper()
	findings, census, err := auditTaggedPackagesAreExecuted(root, synthReachModule)
	if err != nil {
		t.Fatalf("судья сорвался на синтетическом дереве: %v", err)
	}
	if census.FilesScanned == 0 || census.FilesWithTag == 0 || census.DeclarationFiles == 0 {
		t.Fatalf("синтетическое дерево не осмотрено — вердикт на нём недействителен: %s", census)
	}
	return findings, census
}

// TestTagRunGateRedOnPackageOutsideEverySelection — направление (а): пакет с
// признаком лежит ВНЕ области прогона — гейт КРАСНЕЕТ и НАЗЫВАЕТ координату.
//
// Это ровно тот случай, ради которого гейт написан: первый файл `//go:build
// integration` в `internal/apps/…` невидим всем прогонам, потому что отбор
// интеграционной джобы идёт по пути.
func TestTagRunGateRedOnPackageOutsideEverySelection(t *testing.T) {
	root := synthReachTree(t, makefileWithScope(), []string{"internal/apps/thing"})

	findings, census := auditSynthReach(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("пакет под признаком вне всякой области, а гейт молчит — "+
			"он не способен упасть.\n%s", census)
	}
	joined := joinTagRunFindings(findings)
	if !strings.Contains(joined, "internal/apps/thing") {
		t.Fatalf("находка не называет пакет — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "synthtag") {
		t.Fatalf("находка не называет признак — непонятно, какого прогона не хватает:\n%s", joined)
	}
	// Рассмотренные прогоны обязаны доехать до читателя: без них находку нечем
	// опровергнуть, а «покрытия нет» неотличимо от «судья не нашёл объявлений».
	if !strings.Contains(joined, "Makefile:") {
		t.Fatalf("находка не называет рассмотренные прогоны — её нечем опровергнуть:\n%s", joined)
	}
}

// TestTagRunGateSilentOnPackageInsideSelection — направление (б): тот же признак,
// та же форма файла, то же объявление — но пакет попадает в область. Гейт МОЛЧИТ.
//
// Без этой стороны гейт ловил бы форму «в дереве есть признак сборки».
func TestTagRunGateSilentOnPackageInsideSelection(t *testing.T) {
	root := synthReachTree(t, makefileWithScope(), []string{"services/x/internal/repo"})

	findings, census := auditSynthReach(t, root)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("покрытый пакет помечен находкой — гейт ловит форму, а не существо:\n%s",
			joinTagRunFindings(findings))
	}
}

// TestTagRunGateAppliesTheDeclaredFilter — отбор `grep -E` ВЫВОДИТСЯ из
// объявления и реально сужает.
//
// Пакет лежит внутри области `go list` (`services/x/...`), но НЕ проходит фильтр
// `/internal/(repo|clients)`. Прогон его не запустит — значит гейт обязан
// краснеть. Без этой пробы гейт мог бы читать одну лишь область и молча считать
// покрытым всё, что под неё подпадает: копия отбора, разошедшаяся с объявлением,
// отвечает «покрыт» ровно там, где расхождение не видно.
func TestTagRunGateAppliesTheDeclaredFilter(t *testing.T) {
	root := synthReachTree(t, makefileWithScope(), []string{"services/x/internal/usecase"})

	findings, census := auditSynthReach(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("пакет не проходит объявленный отбор, а гейт считает его покрытым — "+
			"фильтр из объявления не применяется.\n%s", census)
	}
	if !strings.Contains(joinTagRunFindings(findings), "services/x/internal/usecase") {
		t.Fatalf("находка не называет пакет:\n%s", joinTagRunFindings(findings))
	}
}

// TestTagRunGateDoesNotCountACommentAsARun — предпосылка судьи: прогоном
// является ВЫЗОВ, а не проза о нём.
//
// Проба стоит здесь не умозрительно. В `.github/workflows/ci.yaml` рядом с
// настоящим вызовом стоит абзац, объясняющий его и содержащий `-tags helmcharts`
// дословно. Гейт по сырому тексту засчитал бы покрытие по комментарию,
// объясняющему покрытие, — то есть был бы проверкой с формой, но без содержания
// ровно на том дереве, ради которого написан.
func TestTagRunGateDoesNotCountACommentAsARun(t *testing.T) {
	commented := "# Прогон когда-то звался так:\n" +
		"#\t$(GO) test -tags=synthtag ./internal/...\n" +
		"test-nothing:\n\t@echo нечего\n"
	root := synthReachTree(t, commented, []string{"internal/apps/thing"})

	findings, census := auditSynthReach(t, root)
	t.Log(census.String())

	if census.RunsFound != 0 {
		t.Fatalf("комментарий принят за прогон — судья читает текст, а не вызов: %s", census)
	}
	if len(findings) == 0 {
		t.Fatalf("единственное упоминание признака — в комментарии, а гейт считает пакет "+
			"покрытым.\n%s", census)
	}
}

func joinTagRunFindings(findings []tagRunFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}
