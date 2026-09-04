// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// deliverymarker_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт семьи СПОСОБЕН
// упасть и способен СМОЛЧАТЬ.
//
// Проверка, которая только зеленеет, неотличима от проверки, которой нечего
// искать. Поэтому каждое утверждение здесь идёт ПАРОЙ: дефект обязан краснеть и
// называть координату, а законный близнец той же формы — молчать. Односторонний
// набор зеленел бы на судье, который отвергает всё, и на судье, который не
// отвергает ничего.
//
// Вход подаётся СИНТЕТИЧЕСКИЙ: на настоящем дереве ни падения, ни молчания не
// показать, не сломав его.
package repohygiene

import (
	"strings"
	"testing"
)

// ─── СУЖДЕНИЕ: обе стороны оси ───────────────────────────────────────────────

func TestDeliveryMarkerVerdictRedsOnQueueWithoutTheMarker(t *testing.T) {
	ref := TableRef{Owner: "services/x", Name: "x_outbox"}
	findings, classified, queues, journals := deliveryMarkerVerdict(
		map[TableRef]bool{ref: false},
		[]TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: familyDrainerQueue}},
	)
	if len(findings) != 1 {
		t.Fatalf("очередь без признака доставки обязана быть находкой; находок %d", len(findings))
	}
	// Находка обязана НАЗЫВАТЬ КООРДИНАТУ: находка, посылающая читателя искать
	// самому, стоит прогона и потом снимается как непонятная.
	if !strings.Contains(findings[0], ref.String()) {
		t.Errorf("находка не называет таблицу: %q", findings[0])
	}
	if !strings.Contains(findings[0], DeliveryMarkerColumn) {
		t.Errorf("находка не называет колонку-признак: %q", findings[0])
	}
	if classified != 1 || queues != 1 || journals != 0 {
		t.Errorf("перепись: классифицировано %d, очередей %d, журналов %d — ожидалось 1/1/0",
			classified, queues, journals)
	}
}

func TestDeliveryMarkerVerdictSilentOnQueueWithTheMarker(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ той же формы: та же семья, тот же вид записи — и признак
	// на месте. Без него отрицание выше зеленело бы на судье, отвергающем всё.
	ref := TableRef{Owner: "services/x", Name: "x_outbox"}
	findings, _, _, _ := deliveryMarkerVerdict(
		map[TableRef]bool{ref: true},
		[]TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: familyDrainerQueue}},
	)
	if len(findings) != 0 {
		t.Fatalf("очередь С признаком доставки — законна; находки: %v", findings)
	}
}

func TestDeliveryMarkerVerdictRedsOnJournalCarryingTheMarker(t *testing.T) {
	// ВТОРАЯ СТОРОНА ОСИ. Без неё гейт зеленел бы на реестре, объявившем
	// журналом всё подряд, — то есть на самом дешёвом способе его замолчать.
	ref := TableRef{Owner: "services/y", Name: "y_journal"}
	findings, classified, queues, journals := deliveryMarkerVerdict(
		map[TableRef]bool{ref: true},
		[]TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: familyJournal}},
	)
	if len(findings) != 1 {
		t.Fatalf("журнал С признаком доставки обязан быть находкой; находок %d", len(findings))
	}
	if !strings.Contains(findings[0], ref.String()) {
		t.Errorf("находка не называет таблицу: %q", findings[0])
	}
	if classified != 1 || queues != 0 || journals != 1 {
		t.Errorf("перепись: классифицировано %d, очередей %d, журналов %d — ожидалось 1/0/1",
			classified, queues, journals)
	}
}

func TestDeliveryMarkerVerdictSilentOnJournalWithoutTheMarker(t *testing.T) {
	ref := TableRef{Owner: "services/y", Name: "y_journal"}
	findings, _, _, _ := deliveryMarkerVerdict(
		map[TableRef]bool{ref: false},
		[]TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: familyJournal}},
	)
	if len(findings) != 0 {
		t.Fatalf("журнал БЕЗ признака доставки — законен; находки: %v", findings)
	}
}

func TestDeliveryMarkerVerdictIgnoresUnclassifiedEntries(t *testing.T) {
	// Необъявленная семья — законное значение: гейт судит объявленное, а не
	// требует объявления от всех. Проверяется на входе, который у ОБЪЯВЛЕННОЙ
	// записи был бы находкой в обе стороны.
	q := TableRef{Owner: "services/z", Name: "z_a"}
	j := TableRef{Owner: "services/z", Name: "z_b"}
	findings, classified, _, _ := deliveryMarkerVerdict(
		map[TableRef]bool{q: false, j: true},
		[]TableGrowthDecl{
			{Owner: q.Owner, Table: q.Name, Family: familyUnclassified},
			{Owner: j.Owner, Table: j.Name, Family: familyUnclassified},
		},
	)
	if len(findings) != 0 {
		t.Fatalf("необъявленная семья не судится; находки: %v", findings)
	}
	if classified != 0 {
		t.Errorf("классифицировано %d — необъявленные не входят в перепись судимого", classified)
	}
}

func TestDeliveryMarkerVerdictRedsOnFamilyOutsideTheClosedVocabulary(t *testing.T) {
	// Словарь ЗАКРЫТ: значение, собранное автором самому себе, обязано быть
	// находкой, а не молча принятым синонимом.
	ref := TableRef{Owner: "services/w", Name: "w_t"}
	findings, _, _, _ := deliveryMarkerVerdict(
		map[TableRef]bool{ref: true},
		[]TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: growthFamily("очередь-ish")}},
	)
	if len(findings) != 1 || !strings.Contains(findings[0], "вне закрытого словаря") {
		t.Fatalf("значение вне словаря обязано быть находкой; находки: %v", findings)
	}
}

// ─── РАЗБОР СХЕМЫ: что считается объявлением признака ────────────────────────

// scanMarkerOne — разбор одной синтетической миграции.
func scanMarkerOne(t *testing.T, sql string) map[TableRef]bool {
	t.Helper()
	ev, census := ScanDeliveryMarker("services/x", "0001_syn.sql", []byte(sql))
	if census.MigrationFiles != 1 {
		t.Fatalf("перепись не засчитала прочитанный файл: %+v", census)
	}
	return FoldDeliveryMarker(ev)
}

func TestScanDeliveryMarkerSeesTheColumnDeclaredByCreate(t *testing.T) {
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE q (
  id BIGSERIAL PRIMARY KEY,
  sent_at TIMESTAMPTZ,
  attempt_count INT NOT NULL DEFAULT 0
);
`)
	if !got[TableRef{Owner: "services/x", Name: "q"}] {
		t.Fatal("колонка объявлена телом CREATE TABLE — разбор обязан её увидеть")
	}
}

func TestScanDeliveryMarkerSilentWhenCreateDeclaresAnotherShape(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: ровно та же форма таблицы, но форма ЖУРНАЛА —
	// `sequence_no`/`processed_at`. Это и есть те четыре таблицы дерева, из-за
	// которых гейт заведён.
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE j (
  sequence_no BIGSERIAL PRIMARY KEY,
  processed_at TIMESTAMPTZ
);
`)
	if got[TableRef{Owner: "services/x", Name: "j"}] {
		t.Fatal("признака доставки в этой форме нет — разбор не вправе его находить")
	}
}

func TestScanDeliveryMarkerSeesAlterAdd(t *testing.T) {
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE q (id BIGSERIAL PRIMARY KEY);
ALTER TABLE q ADD COLUMN sent_at TIMESTAMPTZ;
`)
	if !got[TableRef{Owner: "services/x", Name: "q"}] {
		t.Fatal("колонка добавлена ALTER — разбор обязан её увидеть")
	}
}

func TestScanDeliveryMarkerHonoursTheOrderOfApplication(t *testing.T) {
	// НЕСУЩЕЕ: в этом дереве колонку дважды заводили и снимали, и порядок есть
	// единственное, что различает очередь и журнал, ПЕРЕСТАВШИЙ быть очередью.
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE q (id BIGSERIAL PRIMARY KEY, sent_at TIMESTAMPTZ);
ALTER TABLE q DROP COLUMN sent_at;
`)
	if got[TableRef{Owner: "services/x", Name: "q"}] {
		t.Fatal("колонка снята применённой миграцией — таблица больше не очередь")
	}
}

func TestScanDeliveryMarkerIgnoresTheDownSection(t *testing.T) {
	// Секция Down — ОТКАТ. Засчитав её, разбор объявил бы снятой каждую
	// заведённую колонку, и гейт краснел бы на всём дереве.
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE q (id BIGSERIAL PRIMARY KEY, sent_at TIMESTAMPTZ);
-- +goose Down
ALTER TABLE q DROP COLUMN sent_at;
`)
	if !got[TableRef{Owner: "services/x", Name: "q"}] {
		t.Fatal("`DROP COLUMN` секции Down есть откат и снятием НЕ является")
	}
}

func TestScanDeliveryMarkerDoesNotMatchANeighbourColumnName(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ по ГРАНИЦЕ ИМЕНИ: соседние колонки, содержащие имя
	// признака как подстроку, признаком не являются. Без границы разбор объявил
	// бы очередью журнал, у которого есть `notified_at`.
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE j (
  sequence_no BIGSERIAL PRIMARY KEY,
  notified_at TIMESTAMPTZ,
  sent_at_backup TIMESTAMPTZ
);
`)
	if got[TableRef{Owner: "services/x", Name: "j"}] {
		t.Fatal("`notified_at` и `sent_at_backup` признаком доставки не являются")
	}
}

func TestScanDeliveryMarkerDoesNotMatchTheNameInsideAConstraint(t *testing.T) {
	// Имя признака, встреченное ВНУТРИ элемента (ссылка, частичный индекс,
	// проверка), объявлением колонки не является: сверка идёт с ПЕРВЫМ словом
	// элемента верхнего уровня.
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE j (
  sequence_no BIGSERIAL PRIMARY KEY,
  peer_id BIGINT REFERENCES q (sent_at),
  CONSTRAINT j_chk CHECK (sequence_no > 0)
);
`)
	if got[TableRef{Owner: "services/x", Name: "j"}] {
		t.Fatal("`REFERENCES q (sent_at)` не объявляет колонку этой таблицы")
	}
}

// TestScanKnowsEveryLegalFormOfTheMarkerAction — РАСПОЗНАВАТЕЛЬ ЗНАЕТ ВСЕ ФОРМЫ.
//
// Форма, о которой разбор не знает, — не край и не редкость: всё записанное в
// ней оказывается ВНЕ НАБЛЮДЕНИЯ, то есть не находкой и не молчанием, а
// невидимостью. Поэтому каждая законная форма записи действия над колонкой
// названа здесь поимённо и проверена, включая формы, которые обязаны НЕ
// считаться действием над ней.
func TestScanKnowsEveryLegalFormOfTheMarkerAction(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		want      bool
	}{
		{"DROP без слова COLUMN — в Postgres оно необязательно", `-- +goose Up
CREATE TABLE q (id INT, sent_at TIMESTAMPTZ);
ALTER TABLE q DROP sent_at;`, false},
		{"ADD с IF NOT EXISTS", `-- +goose Up
CREATE TABLE q (id INT);
ALTER TABLE q ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ;`, true},
		{"DROP с IF EXISTS", `-- +goose Up
CREATE TABLE q (id INT, sent_at TIMESTAMPTZ);
ALTER TABLE q DROP COLUMN IF EXISTS sent_at;`, false},
		{"несколько действий одним оператором", `-- +goose Up
CREATE TABLE q (id INT);
ALTER TABLE q ADD COLUMN a INT, ADD COLUMN sent_at TIMESTAMPTZ;`, true},
		{"DROP CONSTRAINT колонку не снимает", `-- +goose Up
CREATE TABLE q (id INT, sent_at TIMESTAMPTZ);
ALTER TABLE q DROP CONSTRAINT q_chk;`, true},
		{"ALTER COLUMN не заводит и не снимает", `-- +goose Up
CREATE TABLE q (id INT, sent_at TIMESTAMPTZ);
ALTER TABLE q ALTER COLUMN sent_at SET NOT NULL;`, true},
		{"схема в имени таблицы", `-- +goose Up
CREATE TABLE kacho_x.q (id INT, sent_at TIMESTAMPTZ);`, true},
		{"колонка с похожим именем не считается признаком", `-- +goose Up
CREATE TABLE q (id INT);
ALTER TABLE q ADD COLUMN address TEXT;`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, _ := ScanDeliveryMarker("services/x", "0001.sql", []byte(tc.sql))
			got := FoldDeliveryMarker(ev)[TableRef{Owner: "services/x", Name: "q"}]
			if got != tc.want {
				t.Fatalf("форма %q: разбор ответил %v, ожидалось %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestDeliveryMarkerVerdictRedsOnJournalTheScanNeverSaw — ТРЕТЬЕ состояние
// журнальной половины: таблица, которой разбор не встречал.
//
// # Зачем отдельная проба
//
// Журнальная половина утверждает ОТРИЦАНИЕ — «признака доставки быть не должно».
// На таблице, которой разбор не видел, отрицание выполняется тождественно: карта
// отдаёт нулевое значение и для «видел, признака нет», и для «не встречал вовсе».
// Половина при этом молчит, ничего не проверив, и молчание неотличимо от
// исправности — то есть ровно тот класс, ради которого гейт заведён, но на самом
// гейте.
//
// # Почему это нашлось только 2026-09-04
//
// До свода миграций iam обе журнальные таблицы попадали в карту через СНЯТИЕ
// признака (`ALTER … DROP COLUMN sent_at`), поэтому «видел» выполнялось само
// собой и разницы не было видно. Свод написан `pg_dump`, то есть конечным
// состоянием: снятий он не производит вовсе, и таблицы объявлены сразу без
// признака. Ключа в карте у них не стало — а половина продолжала зеленеть.
//
// Пара обязательна и проверяется В ОДНОЙ пробе: невиданная таблица — находка,
// виденная без признака — молчание.
func TestDeliveryMarkerVerdictRedsOnJournalTheScanNeverSaw(t *testing.T) {
	ref := TableRef{Owner: "services/y", Name: "y_journal"}
	decl := []TableGrowthDecl{{Owner: ref.Owner, Table: ref.Name, Family: familyJournal}}

	// Карта ПУСТА: таблицы разбор не встречал.
	findings, classified, _, journals := deliveryMarkerVerdict(map[TableRef]bool{}, decl)
	if len(findings) != 1 {
		t.Fatalf("журнал, объявления которого разбор не встречал, обязан быть находкой; "+
			"находок %d.\n\nМолчание здесь означает, что отрицание половины выполняется "+
			"тождественно и половина зеленеет вакуумно", len(findings))
	}
	if !strings.Contains(findings[0], ref.String()) {
		t.Errorf("находка не называет таблицу: %q", findings[0])
	}
	if classified != 1 || journals != 1 {
		t.Errorf("перепись: классифицировано %d, журналов %d — ожидалось 1/1", classified, journals)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же запись реестра, но таблица ВИДЕНА и признака не
	// несёт. Без него «находка» была бы неотличима от «журнал находка всегда».
	silent, _, _, _ := deliveryMarkerVerdict(map[TableRef]bool{ref: false}, decl)
	if len(silent) != 0 {
		t.Fatalf("виденная таблица без признака — законный журнал; находки: %v", silent)
	}
}

// TestScanDeliveryMarkerRecordsTheTableItSawWithoutTheMarker — разбор ОТЛИЧАЕТ
// «видел, признака нет» от «не встречал».
//
// Свойство несущее и проверяется здесь, а не только в суждении: без него ключа в
// карте не появляется, и ветвь `!seen` суждения недостижима на настоящем дереве
// — то есть была бы мёртвой при живом предмете.
func TestScanDeliveryMarkerRecordsTheTableItSawWithoutTheMarker(t *testing.T) {
	got := scanMarkerOne(t, `-- +goose Up
CREATE TABLE j (
  sequence_no BIGSERIAL PRIMARY KEY,
  processed_at TIMESTAMPTZ
);
`)
	has, seen := got[TableRef{Owner: "services/x", Name: "j"}]
	if !seen {
		t.Fatal("таблица объявлена телом CREATE TABLE — разбор обязан записать, что ВИДЕЛ её, " +
			"даже когда признака доставки в ней нет")
	}
	if has {
		t.Fatal("признака доставки в этой форме нет — разбор не вправе его находить")
	}
	// Вторая сторона: таблицы, которой в миграции нет, в карте быть не должно.
	if _, seenOther := got[TableRef{Owner: "services/x", Name: "q"}]; seenOther {
		t.Fatal("разбор записал таблицу, которой в миграции нет — «видел» перестало что-либо означать")
	}
}
