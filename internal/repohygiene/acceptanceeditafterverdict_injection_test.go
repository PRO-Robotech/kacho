// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// acceptanceeditafterverdict_injection_test.go — способность гейта упасть и
// смолчать доказывается ИНЪЕКЦИЕЙ на синтетическом дереве.
//
// Случаев ТРИ, и третий обязателен:
//
//	правлен после вердикта, записи НЕТ  → находка с координатой;
//	правлен после вердикта, запись ЕСТЬ → молчание;
//	НЕ правлен после вердикта           → молчание.
//
// Без третьего гейт краснел бы на всяком доме, где всё в порядке: документ, чью
// шапку никто не трогал после последней правки тела, есть обычное состояние.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// acceptanceFixtureRepo — синтетический репозиторий с домом приёмок.
//
// Репозиторий СВОЙ и изолированный: проба, заводящая его без изоляции, пишет в
// индекс и настройки того дерева, из которого запущена, — и дальше проверки,
// читающие дерево, выдумывают красные вердикты на целом коде.
func acceptanceFixtureRepo(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = "docs/acceptance"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o750); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", ".")
	git("config", "user.email", "fixture@example.org")
	git("config", "user.name", "fixture")
	return root, dir
}

func writeDoc(t *testing.T, root, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dir), name),
		[]byte(body), 0o600); err != nil {
		t.Fatalf("документ %s: %v", name, err)
	}
}

func commitFixture(t *testing.T, root, msg string, when string) {
	t.Helper()
	cmd := gitenv.Command(root, "add", "-A")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	c := gitenv.Command(root, "commit", "-q", "-m", msg)
	c.Env = append(c.Env, "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

const verdictHeader = "# Приёмка\n\n- **Статус:** **✅ APPROVED** — вердикт о ревизии abc123\n\n"

// TestInjection_EditedAfterVerdictWithoutANoteIsAFinding — ИНЪЕКЦИЯ.
func TestInjection_EditedAfterVerdictWithoutANoteIsAFinding(t *testing.T) {
	t.Parallel()
	root, dir := acceptanceFixtureRepo(t)
	writeDoc(t, root, dir, "a.md", verdictHeader+"тело\n")
	commitFixture(t, root, "вердикт", "2026-01-01T00:00:00+00:00")
	// Один изменённый факт: правится ТЕЛО, шапка не тронута, записи нет.
	writeDoc(t, root, dir, "a.md", verdictHeader+"тело правленное\n")
	commitFixture(t, root, "правка тела", "2026-02-01T00:00:00+00:00")

	findings, census, err := AuditAcceptanceEditsAfterVerdict(root, dir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.EditedAfter != 1 {
		t.Fatalf("правка после вердикта не распознана: %s", census)
	}
	if len(findings) != 1 {
		t.Fatalf("правка без записи не объявлена находкой: находок %d (%s) — гейт "+
			"потерял способность падать", len(findings), census)
	}
	if !strings.Contains(findings[0].String(), "a.md") {
		t.Errorf("находка не называет координаты: %s", findings[0])
	}
}

// TestInjection_EditedAfterVerdictWithANoteIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Отличие от инъекции РОВНО ОДНО: в теле стоит запись о правке.
func TestInjection_EditedAfterVerdictWithANoteIsSilent(t *testing.T) {
	t.Parallel()
	root, dir := acceptanceFixtureRepo(t)
	writeDoc(t, root, dir, "a.md", verdictHeader+"тело\n")
	commitFixture(t, root, "вердикт", "2026-01-01T00:00:00+00:00")
	writeDoc(t, root, dir, "a.md", verdictHeader+
		"- **⚠️ ПОСЛЕ вердикта документ правлен.** Правок 1.\n\nтело правленное\n")
	commitFixture(t, root, "правка тела с записью", "2026-02-01T00:00:00+00:00")

	findings, census, err := AuditAcceptanceEditsAfterVerdict(root, dir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.EditedAfter != 1 || census.CarryingNote != 1 {
		t.Fatalf("перепись не различает правку и запись о ней: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("документ, назвавший свою правку, объявлен находкой: %v", findings)
	}
}

// TestInjection_DocumentNotEditedAfterItsVerdictIsSilent — ТРЕТИЙ случай, и без
// него гейт краснел бы на всяком доме, где всё в порядке.
func TestInjection_DocumentNotEditedAfterItsVerdictIsSilent(t *testing.T) {
	t.Parallel()
	root, dir := acceptanceFixtureRepo(t)
	writeDoc(t, root, dir, "a.md", verdictHeader+"тело\n")
	commitFixture(t, root, "вердикт вместе с телом", "2026-01-01T00:00:00+00:00")

	findings, census, err := AuditAcceptanceEditsAfterVerdict(root, dir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.DocsRead != 1 {
		t.Fatalf("документ не прочитан: %s", census)
	}
	if census.EditedAfter != 0 || len(findings) != 0 {
		t.Fatalf("непра́вленный после вердикта документ объявлен правленным: %s, %v",
			census, findings)
	}
}

// TestInjection_SecondFormOfTheNoteIsAlsoRecognised — ВТОРАЯ ФОРМА записи.
//
// Документ без вынесенного вердикта объявляет то же событие иначе — переносить
// нечего. Распознаватель, знающий одну форму, объявил бы находкой документ,
// который своё движение как раз назвал.
func TestInjection_SecondFormOfTheNoteIsAlsoRecognised(t *testing.T) {
	t.Parallel()
	root, dir := acceptanceFixtureRepo(t)
	const draft = "# Приёмка\n\n- **Статус:** DRAFT — вердикт не вынесен\n\n"
	writeDoc(t, root, dir, "a.md", draft+"тело\n")
	commitFixture(t, root, "черновик", "2026-01-01T00:00:00+00:00")
	writeDoc(t, root, dir, "a.md", draft+
		"- **⚠️ ПОСЛЕ объявления состояния документ правлен МАССОВО.**\n\nтело правленное\n")
	commitFixture(t, root, "массовая правка", "2026-02-01T00:00:00+00:00")

	findings, census, err := AuditAcceptanceEditsAfterVerdict(root, dir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.CarryingNote != 1 || len(findings) != 0 {
		t.Fatalf("вторая форма записи не распознана: %s, %v — форма, о которой "+
			"распознаватель не знает, даёт КРАСНОЕ на верном тексте", census, findings)
	}
}

// TestInjection_MissingHomeIsRefusedNotSilent — дома приёмок нет: отказ.
func TestInjection_MissingHomeIsRefusedNotSilent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, _, err := AuditAcceptanceEditsAfterVerdict(root, "нет/такого"); err == nil {
		t.Fatal("несуществующий дом приёмок принят молча — «ноль находок» стало бы " +
			"неотличимо от «ноль прочитанного»")
	}
}
