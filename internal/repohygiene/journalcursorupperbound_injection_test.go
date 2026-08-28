// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalcursorupperbound_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, и того, что он молчит на законных близнецах.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`journalcursorupperbound_test.go`) о способности проверки падать не
// говорит ничего — зелёный получает и та, что не смотрит никуда.
//
// Каждое утверждение стоит ПАРОЙ: внесённый дефект обязан краснеть И НАЗЫВАТЬ
// координату, а законный близнец той же формы — молчать. Близнецы здесь не
// выдуманы, все три взяты из настоящего дерева:
//
//   - ЧТЕНИЕ С ВЕРХНЕЙ ГРАНИЦЕЙ — `pkg/subscription/drain.go`, окно `(курсор,
//     устоявшееся]`;
//   - ОЧЕРЕДЬ С КЛЕЙМОМ — `FOR UPDATE SKIP LOCKED`: пропущенная незакоммиченная
//     строка будет забрана следующим проходом, курсор мимо неё не идёт;
//   - ПОСТРАНИЧНЫЙ ОБХОД ПО TEXT-ИДЕНТИФИКАТОРУ — `user_oauth_clients`,
//     `service_account_oauth_clients`, `access_bindings`: у крокфордова
//     идентификатора счётчика на вставке нет вовсе, поэтому «номер выдан раньше
//     видимости» к нему неприменимо by construction.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type journalCursorStand struct {
	root string
}

// newJournalCursorStand — ЗАКОННОЕ состояние дерева: три близнеца и ни одного
// голого номера.
func newJournalCursorStand(t *testing.T) *journalCursorStand {
	t.Helper()
	s := &journalCursorStand{root: t.TempDir()}

	s.write(t, "services/probe/internal/migrations/0001_initial.sql", `
CREATE TABLE kacho_probe.journal (
    sequence_no bigint NOT NULL,
    payload jsonb NOT NULL
);
CREATE TABLE kacho_probe.pages (
    id text NOT NULL,
    owner_id text NOT NULL
);
`)

	// Близнец 1: окно ограничено сверху границей устоявшегося.
	s.write(t, "pkg/subscription/drain.go", "package subscription\n\n"+
		"const readSQL = `SELECT sequence_no FROM journal"+
		" WHERE sequence_no > $1 AND sequence_no <= $2 ORDER BY sequence_no ASC LIMIT 512`\n")

	// Близнец 2: очередь с клеймом — курсор мимо строки не идёт.
	s.write(t, "pkg/outbox/drainer/claim.go", "package drainer\n\n"+
		"const claimSQL = `SELECT sequence_no FROM journal"+
		" WHERE sequence_no > $1 ORDER BY sequence_no ASC FOR UPDATE SKIP LOCKED LIMIT 64`\n")

	// Близнец 3: постраничный обход по TEXT-идентификатору.
	s.write(t, "services/probe/internal/repo/pages.go", "package repo\n\n"+
		"const listSQL = `SELECT id FROM pages"+
		" WHERE owner_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3`\n")

	return s
}

func (s *journalCursorStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *journalCursorStand) audit(
	t *testing.T, allow ...JournalCursorAllowance,
) ([]JournalCursorFinding, JournalCursorCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditJournalCursorUpperBound(JournalCursorOptions{
		Root:     s.root,
		GoRoots:  []string{"pkg", "services", "gateway", "internal"},
		SQLRoots: []string{"pkg", "services"},
		Allow:    allow,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

func journalCursorKinds(findings []JournalCursorFinding, kind string) []JournalCursorFinding {
	var out []JournalCursorFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// TestJournalCursorGateStaysSilentOnLegitimateTwins — КОНТРОЛЬ. Без него
// отрицание ниже зеленело бы на анализаторе, который не смотрит никуда.
func TestJournalCursorGateStaysSilentOnLegitimateTwins(t *testing.T) {
	s := newJournalCursorStand(t)
	findings, census := s.audit(t)

	if len(findings) != 0 {
		t.Fatalf("законное дерево объявлено негодным: %v", findings)
	}
	// Премиса контроля: близнецы ПРОЧИТАНЫ, а не пропущены обходом. Молчание на
	// непрочитанном ничего не доказывает.
	// Близнецы обязаны быть ПРОЧИТАНЫ, а не пропущены обходом: два опознаны
	// возобновимыми (ограниченное сверху и постраничное по TEXT), третий —
	// очередь с клеймом — прочитан и осознанно выведен из предмета. Считаем оба
	// числа: молчание на непрочитанном не доказывает ничего.
	if census.ResumableReads != 2 || census.Claimed != 1 {
		t.Fatalf("возобновимых чтений %d (ожидалось 2), очередей с клеймом %d (ожидалась 1): близнецы не прочитаны",
			census.ResumableReads, census.Claimed)
	}
	if census.Columns == 0 || census.GoFiles == 0 {
		t.Fatalf("обход пуст: колонок %d, файлов Go %d", census.Columns, census.GoFiles)
	}
}

// TestJournalCursorGateCatchesABareNumberCursor — ИНЪЕКЦИЯ: снимаем у чтения
// ВЕРХНЮЮ ГРАНИЦУ, оставив всё остальное на месте.
//
// Форма инъекции выбрана так, чтобы ронять ТОЛЬКО проверяемое: близнецы стенда
// не трогаются, счётчик остаётся счётчиком, клейма не появляется. Инъекция вида
// «завести ещё один файл» нарушала бы всё, что требуется от файлов вообще.
func TestJournalCursorGateCatchesABareNumberCursor(t *testing.T) {
	s := newJournalCursorStand(t)
	s.write(t, "services/probe/internal/repo/feed.go", "package repo\n\n"+
		"const pollSQL = `SELECT sequence_no FROM journal"+
		" WHERE sequence_no > $1 ORDER BY sequence_no ASC LIMIT $2`\n")

	findings, census := s.audit(t)

	bare := journalCursorKinds(findings, JournalCursorBareNumber)
	if len(bare) != 1 {
		t.Fatalf("находок «курсор по голому номеру» %d, ожидалась 1: %v", len(bare), findings)
	}
	if !strings.Contains(bare[0].Where, "services/probe/internal/repo/feed.go") {
		t.Fatalf("находка не называет координату: %q", bare[0].Where)
	}
	if !strings.Contains(bare[0].What, "sequence_no") {
		t.Fatalf("находка не называет колонку позиции: %q", bare[0].What)
	}
	if census.CounterReads != 2 {
		t.Fatalf("чтений по счётчику %d, ожидалось 2 (ограниченное и голое)", census.CounterReads)
	}
}

// TestJournalCursorGateAcceptsAnAllowanceAndExpiresIt — послабление закрывает
// СВОЙ предмет и становится находкой, когда предмета не стало.
func TestJournalCursorGateAcceptsAnAllowanceAndExpiresIt(t *testing.T) {
	s := newJournalCursorStand(t)
	s.write(t, "services/probe/internal/repo/feed.go", "package repo\n\n"+
		"const pollSQL = `SELECT sequence_no FROM journal"+
		" WHERE sequence_no > $1 ORDER BY sequence_no ASC LIMIT $2`\n")

	allow := JournalCursorAllowance{
		File:    "services/probe/internal/repo/feed.go",
		Column:  "sequence_no",
		Because: "предмет заведён задачей #0000, предикат снятия — верхняя граница по устоявшемуся",
	}

	t.Run("послабление с предметом закрывает находку", func(t *testing.T) {
		findings, _ := s.audit(t, allow)
		if len(findings) != 0 {
			t.Fatalf("послабление не закрыло свой предмет: %v", findings)
		}
	})

	t.Run("послабление без причины — само находка", func(t *testing.T) {
		mute := allow
		mute.Because = ""
		findings, _ := s.audit(t, mute)
		if len(journalCursorKinds(findings, JournalCursorAllowanceNoReason)) != 1 {
			t.Fatalf("послабление без причины принято молча: %v", findings)
		}
	})

	t.Run("послабление без предмета — само находка", func(t *testing.T) {
		clean := newJournalCursorStand(t) // голого номера в дереве нет
		findings, _ := clean.audit(t, allow)
		stale := journalCursorKinds(findings, JournalCursorAllowanceStale)
		if len(stale) != 1 {
			t.Fatalf("истёкшее послабление пережило свой предмет молча: %v", findings)
		}
		if !strings.Contains(stale[0].Where, "feed.go") {
			t.Fatalf("находка не называет истёкшую запись: %q", stale[0].Where)
		}
	})
}

// TestJournalCursorGateRefusesAnUnresolvedColumn — неизвестный вход даёт ЯВНЫЙ
// ОТКАЗ, а не молчание: колонка, которой нет ни в одной схеме, не может быть
// объявлена безопасной по умолчанию.
func TestJournalCursorGateRefusesAnUnresolvedColumn(t *testing.T) {
	s := newJournalCursorStand(t)
	s.write(t, "services/probe/internal/repo/ghost.go", "package repo\n\n"+
		"const ghostSQL = `SELECT tick FROM nowhere"+
		" WHERE tick > $1 ORDER BY tick ASC LIMIT $2`\n")

	findings, _ := s.audit(t)
	unknown := journalCursorKinds(findings, JournalCursorUnresolvedColumn)
	if len(unknown) != 1 {
		t.Fatalf("неопознанная колонка принята молча: %v", findings)
	}
	if !strings.Contains(unknown[0].What, "nowhere") {
		t.Fatalf("находка не называет таблицу: %q", unknown[0].What)
	}
}

// TestJournalCursorGateFailsOnAnEmptyWalk — «ноль находок» обязано быть отличимо
// от «ноль прочитанного».
func TestJournalCursorGateFailsOnAnEmptyWalk(t *testing.T) {
	var log strings.Builder
	_, _, err := AuditJournalCursorUpperBound(JournalCursorOptions{
		Root:     t.TempDir(),
		GoRoots:  []string{"pkg"},
		SQLRoots: []string{"pkg"},
	}, &log)
	if err == nil {
		t.Fatal("пустой обход выдан за успех: «ноль находок» стало неотличимо от «ноль прочитанного»")
	}
}

// TestJournalCursorGateKnowsEveryLegalFormOfTheSameRead — ИНЪЕКЦИЯ ПО КАЖДОЙ
// ФОРМЕ, а не одна на все.
//
// Все формы ниже — законная запись ОДНОГО И ТОГО ЖЕ запрещённого чтения, а не
// края. Форма, о которой распознаватель не знает, даёт не красное и не зелёное:
// она даёт МОЛЧАНИЕ, и перепись при этом не шелохнётся — то есть слепота не
// наблюдаема ничем (`testing.md` §«Гейт на класс», п. 7).
//
// Поэтому каждый случай утверждает ДВЕ величины сразу: сдвинулась ли перепись и
// появилась ли находка. Одной первой мало — она молчит ровно там, где гейт слеп.
func TestJournalCursorGateKnowsEveryLegalFormOfTheSameRead(t *testing.T) {
	const (
		head = "package repo\n\nconst q = `SELECT sequence_no FROM journal WHERE "
		tail = " LIMIT 200`\n"
	)
	cases := []struct {
		name    string
		where   string
		caught  bool // ожидается ли находка
		counted bool // опознано ли чтение возобновимым (сдвиг переписи)
	}{
		{"явное ASC — контроль", "sequence_no > $1 ORDER BY sequence_no ASC", true, true},
		{"умолчание стандарта: направления нет", "sequence_no > $1 ORDER BY sequence_no", true, true},
		{"включающий курсор >=", "sequence_no >= $1 ORDER BY sequence_no", true, true},
		{"обратные операнды $1 < col", "$1 < sequence_no ORDER BY sequence_no", true, true},
		{"обратные операнды, умолчание и включение", "$1 <= sequence_no ORDER BY sequence_no ASC", true, true},
		// Отрицательные близнецы: форма похожа, предмета нет.
		{"нисходящая выборка — курсора «дальше» не выражает", "sequence_no > $1 ORDER BY sequence_no DESC", false, false},
		{"верхняя граница обратными операндами", "sequence_no > $1 AND $2 >= sequence_no ORDER BY sequence_no", false, true},
		{"верхняя граница прямой записью", "sequence_no > $1 AND sequence_no <= $2 ORDER BY sequence_no ASC", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newJournalCursorStand(t)
			base := 2 // близнецы стенда, опознанные возобновимыми
			s.write(t, "services/probe/internal/repo/feed.go", head+tc.where+tail)

			findings, census := s.audit(t)
			bare := journalCursorKinds(findings, JournalCursorBareNumber)

			want := base
			if tc.counted {
				want = base + 1
			}
			if census.ResumableReads != want {
				t.Fatalf("возобновимых чтений %d, ожидалось %d — перепись не сдвинулась, значит форма не опознана вовсе",
					census.ResumableReads, want)
			}
			switch {
			case tc.caught && len(bare) != 1:
				t.Fatalf("форма не поймана: находок %d, ожидалась 1 (%v)", len(bare), findings)
			case tc.caught && !strings.Contains(bare[0].Where, "feed.go"):
				t.Fatalf("находка не называет координату: %q", bare[0].Where)
			case !tc.caught && len(bare) != 0:
				t.Fatalf("законный близнец объявлен негодным: %v", bare)
			}
		})
	}
}

// TestJournalCursorGateDeclaresItsBlindSpots — то, что распознаватель НЕ ловит,
// названо здесь и в шапке пакета.
//
// Это не оправдание слепоты, а её ОБНАРУЖИВАЕМОСТЬ: пока форма не ловится, о ней
// сказано вслух; как только кто-нибудь научит распознаватель — эта проба
// покраснеет и заставит поправить шапку. Молчаливая слепота тем и опасна, что
// неотличима от отсутствия предмета.
func TestJournalCursorGateDeclaresItsBlindSpots(t *testing.T) {
	blind := []struct{ name, body string }{
		{
			"склейка запроса из нескольких литералов",
			"package repo\n\nconst (\n\ta = `SELECT sequence_no FROM journal WHERE sequence_no > $1 `\n" +
				"\tb = `ORDER BY sequence_no LIMIT 200`\n)\n",
		},
		{
			"порядок по номеру выражения в списке выборки",
			"package repo\n\nconst q = `SELECT sequence_no FROM journal" +
				" WHERE sequence_no > $1 ORDER BY 1 LIMIT 200`\n",
		},
	}
	for _, tc := range blind {
		t.Run(tc.name, func(t *testing.T) {
			s := newJournalCursorStand(t)
			s.write(t, "services/probe/internal/repo/feed.go", tc.body)
			findings, census := s.audit(t)
			if census.ResumableReads != 2 || len(journalCursorKinds(findings, JournalCursorBareNumber)) != 0 {
				t.Fatalf("форма СТАЛА опознаваться (чтений %d, находок %d) — распознаватель расширился, "+
					"поправь шапку пакета и сними эту запись",
					census.ResumableReads, len(journalCursorKinds(findings, JournalCursorBareNumber)))
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ТРЕТЬЯ ЗАКРЫТОСТЬ — позиция, выдаваемая в ПОРЯДКЕ ФИКСАЦИЙ (kacho#1387).
//
// Она лежит на стороне ПИСАТЕЛЯ и в запросе читателя невидима by construction,
// поэтому доказывать её приходится не одним «покраснел/смолчал», а инъекцией ПО
// КАЖДОМУ признаку: судить по слову `pg_advisory_xact_lock` значило бы принять за
// закрытость блокировку, взятую не там, не так и не тем.
//
// Стенд ниже — законное состояние: таблица со счётчиком, триггер `BEFORE INSERT
// OR UPDATE`, штампующий её под транзакционной блокировкой с ключом-константой, и
// читатель БЕЗ верхней границы. Читатель тот же во всех случаях; меняется только
// объявление писателя — инъекция роняет ТОЛЬКО проверяемое.

// jcOrderedWriter — тело миграции писателя. Части вынесены в параметры, чтобы
// каждый случай отличался от законного РОВНО одним признаком.
func jcOrderedWriter(lock, stampBefore, stampAfter, timing, events, down string) string {
	return `
CREATE TABLE kacho_probe.limits (
    revision bigint NOT NULL,
    payload jsonb NOT NULL
);
-- +goose Up
CREATE OR REPLACE FUNCTION kacho_probe.limits_stamp_revision() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
` + stampBefore + `
    ` + lock + `
` + stampAfter + `
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS limits_stamp_revision_trg ON kacho_probe.limits;
CREATE TRIGGER limits_stamp_revision_trg
    ` + timing + ` ` + events + ` ON kacho_probe.limits
    FOR EACH ROW
    EXECUTE FUNCTION kacho_probe.limits_stamp_revision();
` + down
}

const (
	jcXactLockCall  = `PERFORM pg_advisory_xact_lock(hashtext('kacho_probe.limits_revision'));`
	jcStampCall     = `    NEW.revision := nextval('kacho_probe.limits_revision_seq');`
	jcOrderedReader = "package repo\n\n" +
		"const deltaSQL = `SELECT revision FROM limits" +
		" WHERE revision > $1 ORDER BY revision ASC LIMIT 512`\n"
)

// jcOrderedStand — стенд с писателем и читателем. Возвращает находки и перепись.
func jcOrderedStand(t *testing.T, writer string) ([]JournalCursorFinding, JournalCursorCensus) {
	t.Helper()
	s := newJournalCursorStand(t)
	s.write(t, "services/probe/internal/migrations/0092_limits.sql", writer)
	s.write(t, "services/probe/internal/repo/limit_repo.go", jcOrderedReader)
	return s.audit(t)
}

// TestJournalCursorGateReadsClosednessOnTheWriterSide — КОНТРОЛЬ третьей
// закрытости: законный писатель молчит, и перепись это ПОКАЗЫВАЕТ.
//
// Утверждаются обе величины сразу. Одного «находок нет» мало: ровно так же
// молчит распознаватель, переставший читать триггеры вовсе, — и тогда молчание
// означало бы не закрытость, а слепоту.
func TestJournalCursorGateReadsClosednessOnTheWriterSide(t *testing.T) {
	findings, census := jcOrderedStand(t,
		jcOrderedWriter(jcXactLockCall, "", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""))

	if len(findings) != 0 {
		t.Fatalf("законное чтение по колонке, выдаваемой в порядке фиксаций, объявлено находкой: %v", findings)
	}
	if census.CommitOrderedColumns != 1 {
		t.Fatalf("колонок в порядке фиксаций %d, ожидалась 1: закрытость не выведена из тела триггера",
			census.CommitOrderedColumns)
	}
	if census.CommitOrdered != 1 {
		t.Fatalf("чтений, закрытых порядком фиксаций, %d, ожидалось 1: перепись не показывает, "+
			"ЧЕМ закрыто — прибавка к молчанию неотличима от расширения слепой зоны", census.CommitOrdered)
	}
	// И читатель ПРОЧИТАН: молчание на непрочитанном не доказывает ничего.
	if census.CounterReads != 2 {
		t.Fatalf("чтений по счётчику %d, ожидалось 2 (ограниченное близнеца и закрытое порядком фиксаций)",
			census.CounterReads)
	}
}

// TestJournalCursorGateJudgesTheShapeNotTheWord — ИНЪЕКЦИЯ ПО КАЖДОМУ ПРИЗНАКУ.
//
// Каждый случай — законная запись писателя, у которого ОДИН признак закрытости
// снят; текст `pg_advisory_xact_lock` при этом в большинстве случаев остаётся на
// месте. Гейт, судящий по слову, прошёл бы их все.
func TestJournalCursorGateJudgesTheShapeNotTheWord(t *testing.T) {
	cases := []struct {
		name   string
		writer string
	}{
		{
			"блокировки нет вовсе — номер выдан вне всякого порядка",
			jcOrderedWriter("", "", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""),
		},
		{
			"блокировка ПОСЛЕ выдачи номера — не упорядочивает ничего",
			jcOrderedWriter("", jcStampCall, "    "+jcXactLockCall, "BEFORE", "INSERT OR UPDATE", ""),
		},
		{
			"блокировка СЕАНСОВАЯ — до фиксации не держится",
			jcOrderedWriter(`PERFORM pg_advisory_lock(hashtext('kacho_probe.limits_revision'));`,
				"", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""),
		},
		{
			"блокировка НЕОБЯЗАТЕЛЬНАЯ — вправе не взять и пойти дальше",
			jcOrderedWriter(`PERFORM pg_try_advisory_xact_lock(hashtext('kacho_probe.limits_revision'));`,
				"", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""),
		},
		{
			"ключ блокировки ЗАВИСИТ ОТ СТРОКИ — порядок лишь внутри своей группы",
			jcOrderedWriter(`PERFORM pg_advisory_xact_lock(hashtext(NEW.scope_id));`,
				"", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""),
		},
		{
			"триггер AFTER — NEW уже не меняет",
			jcOrderedWriter(jcXactLockCall, "", jcStampCall, "AFTER", "INSERT OR UPDATE", ""),
		},
		{
			"триггер без INSERT — вставленной строке достаётся умолчание колонки",
			jcOrderedWriter(jcXactLockCall, "", jcStampCall, "BEFORE", "UPDATE", ""),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := jcOrderedStand(t, tc.writer)
			bare := journalCursorKinds(findings, JournalCursorBareNumber)
			if census.CommitOrderedColumns != 0 {
				t.Fatalf("колонок в порядке фиксаций %d, ожидалось 0: признак снят, а закрытость объявлена",
					census.CommitOrderedColumns)
			}
			if len(bare) != 1 {
				t.Fatalf("находок «курсор по голому номеру» %d, ожидалась 1: %v", len(bare), findings)
			}
			if !strings.Contains(bare[0].Where, "limit_repo.go") {
				t.Fatalf("находка не называет координату чтения: %q", bare[0].Where)
			}
			if !strings.Contains(bare[0].What, "revision") {
				t.Fatalf("находка не называет колонку позиции: %q", bare[0].What)
			}
		})
	}
}

// TestJournalCursorGateHonoursTheOrderOfMigrations — более поздняя миграция
// СНИМАЕТ закрытость, а нисходящая часть — НЕ снимает.
//
// Оба случая читаются одним линейным проходом и различаются только тем, к живой
// ли базе применяется текст. Первая редакция распознавателя этого не различала и
// нашла НОЛЬ закрытых колонок при живом триггере в дереве: миграция, заведшая
// триггер, в своей нисходящей части его же и снимает.
func TestJournalCursorGateHonoursTheOrderOfMigrations(t *testing.T) {
	legit := jcOrderedWriter(jcXactLockCall, "", jcStampCall, "BEFORE", "INSERT OR UPDATE", "")

	t.Run("нисходящая часть закрытость НЕ снимает", func(t *testing.T) {
		_, census := jcOrderedStand(t, legit+
			"\n-- +goose Down\nDROP TRIGGER IF EXISTS limits_stamp_revision_trg ON kacho_probe.limits;\n"+
			"DROP FUNCTION IF EXISTS kacho_probe.limits_stamp_revision();\n")
		if census.CommitOrderedColumns != 1 {
			t.Fatalf("колонок в порядке фиксаций %d, ожидалась 1: нисходящая часть прочитана как "+
				"применённая, хотя к живой базе она не применяется", census.CommitOrderedColumns)
		}
	})

	t.Run("более поздняя миграция снимает триггер", func(t *testing.T) {
		s := newJournalCursorStand(t)
		s.write(t, "services/probe/internal/migrations/0092_limits.sql", legit)
		s.write(t, "services/probe/internal/migrations/0093_retire.sql",
			"-- +goose Up\nDROP TRIGGER IF EXISTS limits_stamp_revision_trg ON kacho_probe.limits;\n")
		s.write(t, "services/probe/internal/repo/limit_repo.go", jcOrderedReader)
		findings, census := s.audit(t)
		if census.CommitOrderedColumns != 0 {
			t.Fatalf("колонок в порядке фиксаций %d, ожидалось 0: закрытость пережила снявшую её миграцию",
				census.CommitOrderedColumns)
		}
		if len(journalCursorKinds(findings, JournalCursorBareNumber)) != 1 {
			t.Fatalf("снятие триггера не вернуло находку: %v", findings)
		}
	})

	t.Run("более поздняя миграция заменяет функцию без блокировки", func(t *testing.T) {
		s := newJournalCursorStand(t)
		s.write(t, "services/probe/internal/migrations/0092_limits.sql", legit)
		s.write(t, "services/probe/internal/migrations/0093_relax.sql",
			"-- +goose Up\nCREATE OR REPLACE FUNCTION kacho_probe.limits_stamp_revision() RETURNS trigger\n"+
				"    LANGUAGE plpgsql\n    AS $$\nBEGIN\n"+jcStampCall+"\n    RETURN NEW;\nEND;\n$$;\n")
		s.write(t, "services/probe/internal/repo/limit_repo.go", jcOrderedReader)
		findings, census := s.audit(t)
		if census.CommitOrderedColumns != 0 {
			t.Fatalf("колонок в порядке фиксаций %d, ожидалось 0: замена функции без блокировки "+
				"закрытости не сняла", census.CommitOrderedColumns)
		}
		if len(journalCursorKinds(findings, JournalCursorBareNumber)) != 1 {
			t.Fatalf("замена функции без блокировки не вернула находку: %v", findings)
		}
	})
}

// TestJournalCursorAllowanceExpiresOnClosednessBecomingVisible — послабление,
// заведённое на ЗАКОННОЕ чтение, обязано истечь, как только гейт научился видеть
// его закрытость.
//
// Ради этого свойства задача и заводилась: пока закрытость невидима, запись в
// ведомости выглядит долгом, самоистечение по ней не сработает НИКОГДА (предмет
// у неё есть и будет всегда), и следующий читатель ведомости не отличит «ещё не
// починили» от «чинить нечего».
func TestJournalCursorAllowanceExpiresOnClosednessBecomingVisible(t *testing.T) {
	s := newJournalCursorStand(t)
	s.write(t, "services/probe/internal/migrations/0092_limits.sql",
		jcOrderedWriter(jcXactLockCall, "", jcStampCall, "BEFORE", "INSERT OR UPDATE", ""))
	s.write(t, "services/probe/internal/repo/limit_repo.go", jcOrderedReader)

	findings, _ := s.audit(t, JournalCursorAllowance{
		File:    "services/probe/internal/repo/limit_repo.go",
		Column:  "revision",
		Because: "заведено, пока закрытость на стороне писателя была невидима",
	})
	stale := journalCursorKinds(findings, JournalCursorAllowanceStale)
	if len(stale) != 1 {
		t.Fatalf("послабление на законное чтение пережило свой предмет молча: %v", findings)
	}
	if !strings.Contains(stale[0].Where, "limit_repo.go") {
		t.Fatalf("находка не называет истёкшую запись: %q", stale[0].Where)
	}
}
