// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// TestAcceptanceLedgerGateInjection — доказательство способности гейта упасть и
// смолчать, полученное на СИНТЕТИЧЕСКОМ дереве (#938, п.2 предиката снятия).
//
// Репозиторий заводится в своём временном каталоге и своим окружением: писать в
// индекс, настройки или дерево, из которого запущена проба, запрещено — чужое
// состояние испортила бы ровно та проба, которая должна его беречь.
//
// Пары утверждений — на каждой оси по обе стороны:
//
//	вердикт    DRAFT → находка С ИМЕНЕМ сценария; APPROVED → молчание;
//	основание  записи нет → находка; запись есть → молчание;
//	область    та же миграция, но УЖЕ В СТВОЛЕ → молчание (момент коммита прошёл).
func TestAcceptanceLedgerGateInjection(t *testing.T) {
	root := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "-b", "main")
	run("config", "user.email", "gate@example.invalid")
	run("config", "user.name", "gate")

	// Ствол: миграция, цитирующая приёмку, УЖЕ стоит. Момент коммита для неё
	// прошёл, и гейт о ней не судит — иначе ствол был бы красным за прошлое,
	// которого правкой не изменить (ban #5).
	write("services/iam/internal/migrations/0001_old.sql",
		"-- сценарий IAM-ID-1-08 приёмки\nSELECT 1;\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "ствол")

	l := ledger{Entries: []ledgerEntry{
		{Acceptance: "sub-phase-IAM-ID-1-x-acceptance.md", Prefix: "IAM-ID-1",
			Verdict: "DRAFT", VerdictDated: "2026-08-18", WorkspaceRevision: "abc1234",
			DebtIssue: 938},
		{Acceptance: "sub-phase-quota-v2-x-acceptance.md", Prefix: "QUOTA-V2",
			Verdict: "APPROVED", VerdictDated: "2026-08-14", WorkspaceRevision: "abc1234"},
	}}

	// ОБЛАСТЬ: пока ничего не добавлено, находок нет — миграция ствола не судится.
	added, findings, err := auditNewMigrations(root, "main", l)
	if err != nil {
		t.Fatalf("аудит ствола: %v", err)
	}
	if added != 0 || len(findings) != 0 {
		t.Errorf("миграция ствола попала под суд: добавленных %d, находок %d (%v)",
			added, len(findings), findings)
	}

	run("checkout", "--quiet", "-b", "работа")

	// ВЕРДИКТ DRAFT: находка обязана НАЗВАТЬ сценарий, иначе читатель не поймёт,
	// какое именно основание отсутствует.
	write("services/iam/internal/migrations/0002_draft.sql",
		"-- реализует сценарий IAM-ID-1-13\nSELECT 2;\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "под неутверждённой")

	added, findings, err = auditNewMigrations(root, "main", l)
	if err != nil {
		t.Fatalf("аудит ветки: %v", err)
	}
	if added != 1 {
		t.Errorf("добавленных миграций ждали 1, получили %d", added)
	}
	if len(findings) != 1 {
		t.Fatalf("на неутверждённой приёмке ждали ровно одну находку, получили %d (%v)",
			len(findings), findings)
	}
	if !strings.Contains(findings[0], "IAM-ID-1") {
		t.Errorf("находка не называет сценарий: %q", findings[0])
	}
	if !strings.Contains(findings[0], "0002_draft.sql") {
		t.Errorf("находка не называет координату: %q", findings[0])
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же форма, но приёмка APPROVED — молчание.
	write("services/iam/internal/migrations/0003_ok.sql",
		"-- реализует сценарий QUOTA-V2-04\nSELECT 3;\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "под утверждённой")

	_, findings, err = auditNewMigrations(root, "main", l)
	if err != nil {
		t.Fatalf("аудит ветки: %v", err)
	}
	for _, f := range findings {
		if strings.Contains(f, "0003_ok.sql") {
			t.Errorf("гейт краснеет на APPROVED-приёмке — он ловит форму, а не "+
				"существо: %q", f)
		}
	}

	// ОСНОВАНИЕ НЕ НАЗВАНО: цитата приёмки, которой в ведомости нет вовсе.
	write("services/iam/internal/migrations/0004_unknown.sql",
		"-- реализует сценарий NEWDOM-9-01\nSELECT 4;\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "без основания")

	_, findings, err = auditNewMigrations(root, "main", l)
	if err != nil {
		t.Fatalf("аудит ветки: %v", err)
	}
	var sawUnknown bool
	for _, f := range findings {
		if strings.Contains(f, "0004_unknown.sql") && strings.Contains(f, "NEWDOM-9") {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Errorf("цитата приёмки вне ведомости не поймана: %v", findings)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: миграция БЕЗ цитаты приёмки — не предмет этого гейта.
	write("services/iam/internal/migrations/0005_plain.sql", "SELECT 5;\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "без цитаты")

	_, findings, err = auditNewMigrations(root, "main", l)
	if err != nil {
		t.Fatalf("аудит ветки: %v", err)
	}
	for _, f := range findings {
		if strings.Contains(f, "0005_plain.sql") {
			t.Errorf("миграция без цитаты объявлена находкой: %q", f)
		}
	}

	t.Logf("инъекция: утверждений 8, дерево синтетическое (%s)", root)
}
