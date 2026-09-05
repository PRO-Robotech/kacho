// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalmarkerquery_test.go — ГЕЙТ: журнал без признака доставки никто не
// спрашивает по этому признаку.
//
// Разбор и его границы — `journalmarkerquery.go`. Здесь только обход дерева,
// перепись и вердикт.
//
// Предпосылка гейта ЗАЯВЛЯЕТСЯ, а не подразумевается: словарь журналов
// выводится ИЗ МИГРАЦИЙ (кто объявил таблицу и объявил ли ей признак доставки),
// а не выписывается. Ноль журналов означает, что судить нечего, и молчание
// гейта было бы сказано ни о чём, — поэтому это отказ.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// journalCensusFloor — нижняя граница переписи носителей.
//
// Не «сколько должно быть», а «ниже этого обход точно оборвался»: дерево
// содержит тысячи носителей, и три цифры здесь означают, что читать было
// нечего.
const journalCensusFloor = 500

// journalTablesFromMigrations выводит журналы БЕЗ признака доставки из миграций
// дерева.
//
// Миграции читаются В ПОРЯДКЕ ПРИМЕНЕНИЯ по каждому владельцу: колонку заводят
// и снимают, и последнее слово за последней миграцией.
func journalTablesFromMigrations(t *testing.T, root string, rels []string) ([]JournalTable, DeliveryMarkerCensus, int) {
	t.Helper()
	byOwner := map[string][]string{}
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".sql") {
			continue
		}
		idx := strings.Index(rel, "/internal/migrations/")
		if idx < 0 {
			continue
		}
		owner := rel[:idx]
		byOwner[owner] = append(byOwner[owner], rel)
	}
	var (
		census  DeliveryMarkerCensus
		events  []ColumnEvent
		schemas = map[TableRef]string{}
	)
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		files := byOwner[owner]
		sort.Strings(files)
		for _, rel := range files {
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("чтение миграции %s: %v", rel, err)
			}
			ev, c := ScanDeliveryMarker(owner, rel, src)
			events = append(events, ev...)
			census.Add(c)
			for name, schema := range ScanTableSchemas(src) {
				if schema != "" {
					schemas[TableRef{Owner: owner, Name: name}] = schema
				}
			}
		}
	}
	folded := FoldDeliveryMarker(events)
	var out []JournalTable
	withMarker := 0
	for ref, present := range folded {
		if present {
			withMarker++
			continue
		}
		out = append(out, JournalTable{Owner: ref.Owner, Schema: schemas[ref], Name: ref.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified() < out[j].Qualified() })
	return out, census, withMarker
}

// TestJournalWithoutDeliveryMarkerIsNotQueriedByIt — сам гейт.
func TestJournalWithoutDeliveryMarkerIsNotQueriedByIt(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, len(tt.files))
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	journals, migrCensus, withMarker := journalTablesFromMigrations(t, root, rels)

	// Предпосылка первая: миграции вообще прочитаны.
	if migrCensus.MigrationFiles == 0 || migrCensus.CreateBodies == 0 {
		t.Fatalf("миграций прочитано %d, тел CREATE TABLE %d — словарь журналов взять "+
			"неоткуда, и вердикт был бы вакуумным",
			migrCensus.MigrationFiles, migrCensus.CreateBodies)
	}
	// Предпосылка вторая: полосы НЕПУСТЫ ОБЕ. Ноль журналов означает, что
	// суждение выполняется тождественно; ноль таблиц с признаком означает, что
	// разбор признака перестал его находить, — и тогда журналом объявлено всё.
	if len(journals) == 0 || withMarker == 0 {
		t.Fatalf("журналов без признака доставки %d, таблиц с признаком %d — "+
			"одна из полос пуста, и гейт судил бы ни о чём", len(journals), withMarker)
	}

	var (
		carriers  int
		probes    int
		chunks    int
		prose     int
		findings  []MarkerQueryFinding
		qualified int
	)
	for _, j := range journals {
		if j.Schema != "" {
			qualified++
		}
	}
	for _, rel := range rels {
		// ГРАНИЦА КОРПУСА, названная, а не спрятанная: проба Go под суд не идёт.
		//
		// Причина не в удобстве, а в том, ЧТО спрашивает такой файл. Проба,
		// поднимающая временную базу, заводит схему САМА (`CREATE TABLE …` в её
		// же тексте) и судится своим объявлением, а не миграцией службы;
		// синтетический вход чужого гейта базы не видит вовсе — это фикстура,
		// нарочно несущая дефект, и краснеть на ней значило бы ломать чужое
		// доказательство. Ни то, ни другое не способно отказать на стенде и не
		// доходит до оператора. Форму Go-проб держит их собственный прогон
		// (kacho#1042); предмет ЭТОГО гейта — носители ВНЕ проб.
		//
		// Пропущенное печатается своим числом: слепая полоса, о которой не
		// сказано, неотличима от полосы, которой нет.
		if strings.HasSuffix(rel, "_test.go") {
			probes++
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		f, c := ScanMarkerQueries(rel, src, journals)
		if c.Carrier {
			carriers++
		}
		chunks += c.Chunks
		prose += c.ProseLines
		findings = append(findings, f...)
	}

	t.Logf("перепись: отслеживаемых файлов %d, носителей разобрано %d, Go-проб пропущено %d, "+
		"исполняемых фрагментов %d, строк прозы отброшено %d; миграций прочитано %d, тел CREATE TABLE %d; "+
		"журналов без признака доставки %d (из них со схемой %d), таблиц с признаком %d; находок %d",
		len(rels), carriers, probes, chunks, prose,
		migrCensus.MigrationFiles, migrCensus.CreateBodies,
		len(journals), qualified, withMarker, len(findings))

	if carriers < journalCensusFloor {
		t.Fatalf("перепись обвалилась: носителей разобрано %d при пороге %d — обход "+
			"прочитал почти ничего, и «ноль находок» здесь означало бы «ноль прочитанного»",
			carriers, journalCensusFloor)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, f.String())
	}
	sort.Strings(lines)
	t.Fatalf("исполняемых запросов к журналу по несуществующему признаку доставки: %d\n%s\n\n"+
		"Такой запрос отвергается базой (42703) В РАНТАЙМЕ, а под `|| true` — МОЛЧА, и это "+
		"неотличимо от исправной работы. Исходов два, третьего нет: привести запрос к "+
		"действительной схеме либо снять его вместе с предметом (для скрипта — вместе с вызовами).",
		len(findings), strings.Join(lines, "\n"))
}
