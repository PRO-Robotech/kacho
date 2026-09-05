// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalmarkerquery_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт запроса к
// журналу СПОСОБЕН упасть и способен СМОЛЧАТЬ.
//
// Каждое утверждение идёт ПАРОЙ, и близнец отличается от дефекта РОВНО ОДНИМ
// фактом: иначе красное могло прийти от соседнего различия, и доказательство
// было бы совпадением. Односторонний набор зеленел бы на разборе, который
// отвергает всё, и на разборе, который не отвергает ничего.
//
// Вход СИНТЕТИЧЕСКИЙ: на настоящем дереве ни падения, ни молчания не показать,
// не сломав его.
package repohygiene

import (
	"strings"
	"testing"
)

// journalFixture — журнал БЕЗ признака доставки; queueFixture — очередь С ним.
var (
	journalFixture = JournalTable{Owner: "services/x", Schema: "kacho_x", Name: "x_journal"}
	queueFixture   = JournalTable{Owner: "services/x", Schema: "kacho_x", Name: "x_queue"}
)

// ─── ОСЬ 1: запрос против прозы ──────────────────────────────────────────────

func TestMarkerQueryRedsOnExecutableShellQuery(t *testing.T) {
	src := []byte("#!/usr/bin/env bash\n" +
		`Q="SELECT count(*) FROM kacho_x.x_journal WHERE sent_at IS NULL;"` + "\n")
	f, census := ScanMarkerQueries("fix/drain.sh", src, []JournalTable{journalFixture})
	if len(f) != 1 {
		t.Fatalf("исполняемый запрос по несуществующему признаку обязан быть находкой; находок %d (перепись %+v)", len(f), census)
	}
	// Находка обязана НАЗЫВАТЬ координату, таблицу и колонку: находка, посылающая
	// читателя искать самому, стоит прогона и потом снимается как непонятная.
	got := f[0].String()
	for _, want := range []string{"fix/drain.sh", "kacho_x.x_journal", DeliveryMarkerColumn} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	if f[0].Line != 2 {
		t.Errorf("находка обязана называть строку запроса, названа %d", f[0].Line)
	}
}

func TestMarkerQuerySilentOnTheSameTextInAComment(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: изменён РОВНО ОДИН факт — тот же текст стоит
	// комментарием. Правильная правка этого класса оставляет в дереве именно
	// такое объяснение в прошедшем времени, и разбор, судящий по слову, покраснел
	// бы на нём.
	src := []byte("#!/usr/bin/env bash\n" +
		`# Прежде здесь стоял SELECT count(*) FROM kacho_x.x_journal WHERE sent_at IS NULL;` + "\n")
	f, census := ScanMarkerQueries("fix/drain.sh", src, []JournalTable{journalFixture})
	if len(f) != 0 {
		t.Fatalf("объяснение в комментарии запросом не является; находки: %v", f)
	}
	if census.ProseLines == 0 {
		t.Fatalf("проза обязана быть СОСЧИТАНА, иначе «ноль находок» неотличимо от «ноль прочитанного»: %+v", census)
	}
}

// ─── ОСЬ 2: журнал против очереди ────────────────────────────────────────────

func TestMarkerQuerySilentOnAQueueThatCarriesTheMarker(t *testing.T) {
	// Изменён РОВНО ОДИН факт: та же строка спрашивает таблицу, у которой признак
	// ЕСТЬ. Без этой пары гейт краснел бы на каждом законном дренаже дерева.
	src := []byte(`Q="SELECT count(*) FROM kacho_x.x_queue WHERE sent_at IS NULL;"` + "\n")
	f, _ := ScanMarkerQueries("fix/drain.sh", src, []JournalTable{journalFixture})
	if len(f) != 0 {
		t.Fatalf("очередь с признаком доставки спрашивать по нему законно; находки: %v", f)
	}
	// Контроль: словарь журналов вообще способен совпасть с этим написанием.
	f2, _ := ScanMarkerQueries("fix/drain.sh", src, []JournalTable{queueFixture})
	if len(f2) != 1 {
		t.Fatalf("контроль: то же написание обязано совпадать, когда таблица объявлена журналом; находок %d", len(f2))
	}
}

// ─── ОСЬ 3: разметка — блок команд против прозы ──────────────────────────────

func TestMarkerQueryRedsInsideAFencedBlockAndIsSilentOutsideIt(t *testing.T) {
	inside := []byte("Руководство.\n\n```bash\npsql -c \"SELECT 1 FROM kacho_x.x_journal WHERE sent_at IS NULL;\"\n```\n")
	f, _ := ScanMarkerQueries("docs/runbook.md", inside, []JournalTable{journalFixture})
	if len(f) != 1 {
		t.Fatalf("команда руководства оператору исполняется — обязана быть находкой; находок %d", len(f))
	}
	// Близнец: РОВНО ОДИН изменённый факт — тот же текст вне блока команд.
	outside := []byte("Руководство.\n\nПрежняя редакция предлагала SELECT 1 FROM kacho_x.x_journal WHERE sent_at IS NULL;\n")
	f2, census := ScanMarkerQueries("docs/runbook.md", outside, []JournalTable{journalFixture})
	if len(f2) != 0 {
		t.Fatalf("проза документа командой не является; находки: %v", f2)
	}
	if census.ProseLines == 0 {
		t.Fatalf("строки прозы обязаны быть сосчитаны: %+v", census)
	}
}

// ─── ОСЬ 4: единица суждения — ОПЕРАТОР, а не весь фрагмент ──────────────────

func TestMarkerQueryBlamesOnlyTheBranchThatAsksForTheMarker(t *testing.T) {
	src := []byte("```bash\npsql -c \"\n" +
		"SELECT count(*) FILTER (WHERE sent_at IS NULL) FROM kacho_x.x_journal\n" +
		"UNION ALL SELECT count(*) FROM kacho_x.x_other\n\"\n```\n")
	other := JournalTable{Owner: "services/x", Schema: "kacho_x", Name: "x_other"}
	f, _ := ScanMarkerQueries("docs/runbook.md", src, []JournalTable{journalFixture, other})
	if len(f) != 1 {
		t.Fatalf("обвиняться обязана ТОЛЬКО ветвь, спрашивающая признак; находок %d: %v", len(f), f)
	}
	if f[0].Table != journalFixture.Qualified() {
		t.Fatalf("обвинена не та таблица: %s", f[0].Table)
	}
}

// ─── ОСЬ 5: «не носитель» ОТЛИЧИМО от «носитель без находок» ─────────────────

func TestMarkerQueryDistinguishesANonCarrierFromACleanCarrier(t *testing.T) {
	q := []byte(`SELECT 1 FROM kacho_x.x_journal WHERE sent_at IS NULL;`)
	_, unknown := ScanMarkerQueries("fix/thing.bin", q, []JournalTable{journalFixture})
	if unknown.Carrier || unknown.Chunks != 0 {
		t.Fatalf("расширение, разбору неизвестное, носителем не является: %+v", unknown)
	}
	clean := []byte(`Q="SELECT count(*) FROM kacho_x.x_queue;"` + "\n")
	f, known := ScanMarkerQueries("fix/ok.sh", clean, []JournalTable{journalFixture})
	if !known.Carrier || known.Chunks == 0 {
		t.Fatalf("носитель обязан быть прочитан и сосчитан: %+v", known)
	}
	if len(f) != 0 {
		t.Fatalf("чистый носитель находок не даёт; находки: %v", f)
	}
}

// ─── ОСЬ 6: словарь журналов ВЫВОДИТСЯ из миграции, а не выписан ─────────────

func TestJournalDictionaryComesFromTheMigrationAndCarriesTheSchema(t *testing.T) {
	sql := []byte(`-- +goose Up
CREATE TABLE kacho_x.x_journal (id bigserial PRIMARY KEY, created_at timestamptz);
CREATE TABLE kacho_x.x_queue (id bigserial PRIMARY KEY, sent_at timestamptz);
`)
	ev, _ := ScanDeliveryMarker("services/x", "0001.sql", sql)
	folded := FoldDeliveryMarker(ev)
	if folded[TableRef{Owner: "services/x", Name: "x_journal"}] {
		t.Errorf("таблица без признака объявлена несущей его")
	}
	if !folded[TableRef{Owner: "services/x", Name: "x_queue"}] {
		t.Errorf("таблица с признаком объявлена журналом")
	}
	schemas := ScanTableSchemas(sql)
	if schemas["x_journal"] != "kacho_x" {
		t.Errorf("схема объявления потеряна: %q — поиск по голому имени совпал бы "+
			"с одноимённой таблицей чужой службы", schemas["x_journal"])
	}
}
