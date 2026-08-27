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
