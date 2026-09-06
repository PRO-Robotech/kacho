// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// clusterAnchorBeyondGoCorpus — отслеживаемые файлы ВНЕ Go, спрошенные У ИНДЕКСА.
//
// Каталоги документации НЕ исключаются (в отличие от skipPath, которым живёт
// разбор Go): написание якоря стоит в 18 файлах документации, и именно там
// заведётся ведомость решённого остаться. Исключить их значило бы завести
// слепую зону ровно на той оси, ради которой гейт написан.
func clusterAnchorBeyondGoCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	root := repoRoot(t)
	files, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v — «ноль находок» здесь означало бы «ноль прочитанного»", err)
	}
	corpus := map[string][]byte{}
	for _, abs := range files {
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("путь %s: %v", abs, relErr)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".go") {
			continue // предмет соседнего разбора, см. шапку clusteranchorbeyondgo.go
		}
		b, readErr := os.ReadFile(abs) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		corpus[rel] = b
	}
	return corpus
}

// clusterAnchorDeclared — объявленное написание, добытое ИЗ ОБЪЯВЛЕНИЙ Go.
//
// Владелец истины один и тот же у обоих разборов: второе место об одном
// предмете разошлось бы молча, и разошлось бы ровно в день перехода.
func clusterAnchorDeclared(t *testing.T) string {
	t.Helper()
	decls, _, _, err := FindClusterAnchorLiterals(clusterAnchorSources(t))
	if err != nil {
		t.Fatalf("объявления написания: %v", err)
	}
	values := map[string]bool{}
	for _, d := range decls {
		values[d.Value] = true
	}
	if len(values) != 1 {
		keys := make([]string, 0, len(values))
		for v := range values {
			keys = append(keys, v)
		}
		sort.Strings(keys)
		t.Fatalf("объявленных написаний %d (%v) — вердикт о дереве вне Go беспредметен, "+
			"пока не решено, какое из них верно; согласие объявлений судит "+
			"TestClusterAnchorDeclarationsAgree", len(values), keys)
	}
	for v := range values {
		return v
	}
	return ""
}

// TestClusterAnchorBeyondGoMatchesItsDeclaration — написание якоря во ВСЕХ
// отслеживаемых файлах вне Go равно объявленному в Go.
//
// Разбор класса, границы и устройство ведомости — в шапке
// clusteranchorbeyondgo.go. Здесь только обход дерева, перепись и вердикт.
func TestClusterAnchorBeyondGoMatchesItsDeclaration(t *testing.T) {
	declared := clusterAnchorDeclared(t)
	findings, ledgerFindings, census, err := FindClusterAnchorBeyondGo(
		clusterAnchorBeyondGoCorpus(t), declared, ClusterAnchorBeyondGoLedger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("объявленное написание: %q", declared)
	t.Logf("прочитано файлов вне Go: %d; пропущено двоичных: %d; пустых: %d",
		census.FilesRead, census.FilesBinary, census.FilesEmpty)
	t.Logf("файлов с якорем: %d; вхождений: %d; расхождений: %d",
		census.FilesWithAnchor, census.Occurrences, len(findings))
	t.Logf("записей ведомости: %d; прощено вхождений: %d",
		census.LedgerEntries, census.Forgiven)
	t.Logf("НЕ СУДЯТСЯ, и это границы, а не находки: якорь, собранный из частей — %d; "+
		"прозаическое сокращение («cluster …root») — %d",
		census.Assembled, census.Elided)
	t.Logf("по видам файлов: %s", anchorKindCensusLine(census))

	// Предпосылки: гейт ОТКАЗЫВАЕТ на беспредметности, а не молчит.
	if census.FilesRead == 0 {
		t.Fatal("прочитано ноль файлов вне Go — обход не состоялся, " +
			"и молчание гейта ничего не значит")
	}
	if census.FilesWithAnchor == 0 {
		t.Fatal("файлов с якорем ноль — предмет не найден, «расхождений ноль» тривиально верно")
	}

	for _, f := range findings {
		t.Errorf("%s:%d [%s]: написание якоря %q РАЗОШЛОСЬ с объявленным %q.\n"+
			"Служба спросит про один объект, а этот файл называет другой — молча: "+
			"proto соберётся, YAML разберётся, консоль нарисуется. Переведите вхождение "+
			"на объявленное написание либо, если прежнее здесь обязано остаться "+
			"(запись замера, разбор прошлого решения, ПРИМЕНЁННАЯ миграция), заведите "+
			"запись в ClusterAnchorBeyondGoLedger с причиной и ТОЧНЫМ числом",
			f.Path, f.Line, f.Kind, f.Text, f.Declared)
	}
	for _, lf := range ledgerFindings {
		t.Errorf("ведомость, запись %q (причина: %s): записано %d, в дереве %d — %s",
			lf.Path, lf.Reason, lf.Want, lf.Got, lf.Why)
	}
}

// anchorKindCensusLine — перепись по осям одной строкой: вид → файлов/вхождений.
//
// Печатается ВСЕГДА, а не только на находке: «ноль по виду» обязано быть
// отличимо от «вид не читали».
func anchorKindCensusLine(c BeyondGoCensus) string {
	kinds := make([]string, 0, len(c.FilesByKind))
	for k := range c.FilesByKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if c.OccurrencesByKind[kinds[i]] != c.OccurrencesByKind[kinds[j]] {
			return c.OccurrencesByKind[kinds[i]] > c.OccurrencesByKind[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s: файлов %d, вхождений %d",
			k, c.FilesByKind[k], c.OccurrencesByKind[k]))
	}
	if len(parts) == 0 {
		return "ни одного вида — предмет не найден"
	}
	return strings.Join(parts, "; ")
}

// TestClusterAnchorBeyondGoWouldCatchTheTransition — доказательство НА НАСТОЯЩЕМ
// дереве: если написание переедет в Go, а файлы вне Go останутся прежними, гейт
// назовёт КАЖДЫЙ из них.
//
// Синтетический мир доказывает, что разбор способен упасть; он не доказывает,
// что он читает ЭТО дерево. Здесь тот же корпус прогоняется против написания,
// которого в дереве нет ни одного (образцу оно подходит), и вердикт обязан
// накрыть все вхождения, кроме прощённых ведомостью.
//
// Проба переживает сам переход by construction: она не называет ни сегодняшнего
// написания, ни целевого — только заведомо отсутствующее.
func TestClusterAnchorBeyondGoWouldCatchTheTransition(t *testing.T) {
	const absent = "cluster_probe_root" // образцу подходит, в дереве отсутствует

	// Ведомость здесь НЕ подаётся намеренно: её записи выданы против
	// ОБЪЯВЛЕННОГО написания, а этот прогон спрашивает про другое — и число
	// прощаемых у них законно разное. Подать её значило бы измерить ведомость
	// вопросом, на который она не отвечает. Ведомость судит проба выше.
	corpus := clusterAnchorBeyondGoCorpus(t)
	findings, _, census, err := FindClusterAnchorBeyondGo(corpus, absent, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("против отсутствующего написания %q: вхождений %d, названо %d, файлов с якорем %d",
		absent, census.Occurrences, len(findings), census.FilesWithAnchor)
	t.Logf("по видам файлов: %s", anchorKindCensusLine(census))

	if census.Occurrences == 0 {
		t.Fatal("вхождений ноль — корпус пуст, и доказательство беспредметно")
	}
	if len(findings) != census.Occurrences {
		t.Fatalf("названо %d вхождений из %d — часть дерева осталась невидимой, "+
			"и по ней «ноль находок» означало бы «ноль прочитанного»",
			len(findings), census.Occurrences)
	}

	// Каждый вид обязан быть представлен среди названного: вид, ни разу не
	// попавший в находки, разбором не читается.
	named := map[string]int{}
	for _, f := range findings {
		named[f.Kind]++
	}
	for kind, occ := range census.OccurrencesByKind {
		if named[kind] != occ {
			t.Errorf("вид %q несёт %d вхождений, названо %d — часть вида вне наблюдения",
				kind, occ, named[kind])
		}
	}
}

// TestClusterAnchorBeyondGoInjectionCoversEveryKindInTheTree — перечень видов,
// по которым идёт инъекция, ВЫВОДИТСЯ из дерева, а не выписывается по памяти.
//
// Синтетический мир доказывает способность упасть по каждому виду, который в
// нём назван. Появись в дереве тринадцатый вид — доказательства по нему не
// было бы, и его молчание не отличалось бы от молчания мёртвой проверки.
// Поэтому вид дерева без своего предмета в инъекции — НАХОДКА, а не пробел.
func TestClusterAnchorBeyondGoInjectionCoversEveryKindInTheTree(t *testing.T) {
	declared := clusterAnchorDeclared(t)
	_, _, census, err := FindClusterAnchorBeyondGo(
		clusterAnchorBeyondGoCorpus(t), declared, ClusterAnchorBeyondGoLedger)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	injected := map[string]bool{}
	for path := range beyondGoSubjects() {
		injected[anchorFileKind(path)] = true
	}

	t.Logf("видов в дереве: %d; видов под инъекцией: %d",
		len(census.FilesByKind), len(injected))

	if len(census.FilesByKind) == 0 {
		t.Fatal("видов в дереве ноль — обход не состоялся, и сверка беспредметна")
	}
	for kind, files := range census.FilesByKind {
		if !injected[kind] {
			t.Errorf("вид %q несут %d файлов дерева, а инъекции по нему нет — "+
				"способность разбора упасть на этом виде НЕ доказана, и его молчание "+
				"неотличимо от молчания мёртвой проверки; добавьте предмет этого вида "+
				"в beyondGoSubjects", kind, files)
		}
	}
}
