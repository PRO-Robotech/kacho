// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// journalCursorAllowances — ведомость послаблений: чтения по голому номеру,
// заведённые ДО этого гейта и снимаемые своими задачами.
//
// # Записи здесь — ДОЛГ С ПРЕДМЕТОМ, а не прощение
//
// Каждая называет задачу и предикат снятия. Ведомость самоистекающая: запись,
// которой в дереве больше нечего исключать, роняет прогон отдельной находкой —
// иначе она пережила бы свой фикс и разрешила следующему завести такое же
// чтение под тем же оправданием.
//
// Обе записи найдены ЗАМЕРОМ по дереву при закрытии kacho#1053, тело которой
// утверждало обратное: «все прочие журналы читаются как очередь с клеймом».
// Клейма нет ни у одного из двух.
var journalCursorAllowances = []JournalCursorAllowance{
	{
		File:   "services/iam/internal/repo/kacho/pg/limit_repo.go",
		Column: "revision",
		Because: "kacho#1373 — дельта величин; курсор сохраняют пять доменных тянущих, " +
			"предикат снятия: верхняя граница по устоявшемуся плюс проба окна",
	},
	{
		File:   "services/iam/internal/repo/kacho/pg/subject_change_repo.go",
		Column: "id",
		Because: "kacho#1374 — журнал изменений субъекта; курсор хранит край, " +
			"предикат снятия: верхняя граница по устоявшемуся плюс проба окна",
	},
}

func journalCursorOptions(t *testing.T) JournalCursorOptions {
	t.Helper()
	return JournalCursorOptions{
		Root:     repoRoot(t),
		GoRoots:  []string{"pkg", "services", "gateway", "terraform", "internal", "cmd"},
		SQLRoots: []string{"pkg", "services"},
		Allow:    journalCursorAllowances,
	}
}

// TestJournalReadersNeverAdvanceOnABareSequenceNumber — вердикт о НАСТОЯЩЕМ
// дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`journalcursorupperbound_injection_test.go`): здесь только вердикт.
func TestJournalReadersNeverAdvanceOnABareSequenceNumber(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditJournalCursorUpperBound(journalCursorOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок» было бы
	// достижимо пустым обходом.
	if census.GoFiles < 500 || census.Literals < 5000 || census.Columns < 500 {
		t.Fatalf("файлов прод-кода %d, литералов %d, колонок схемы %d — обход пуст, вердикт беспредметен",
			census.GoFiles, census.Literals, census.Columns)
	}
	// И премиса предмета: возобновимые чтения в дереве ЕСТЬ. Ноль означал бы, что
	// распознаватель перестал их видеть, а не что дерево исправилось.
	if census.ResumableReads == 0 {
		t.Fatal("возобновимых чтений опознано 0: распознаватель ослеп, и молчание гейта беспредметно")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("чтений по счётчику %d, из них ограничено сверху %d; послаблений %d:\n%s",
		census.CounterReads, census.Bounded, census.Allowances, strings.Join(lines, "\n"))
}
