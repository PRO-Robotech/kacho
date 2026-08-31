// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionjournalretention_test.go — гейт «полосы одного механизма сверены
// МЕЖДУ СОБОЙ».
//
// Требование к КАЖДОЙ полосе журнала подписки одно, и оно про ПАРУ:
//
//	объявлено RetainsFromEarliestRow  ⟺  уборщик журнала провязан
//
// Гейт не судит, ЧТО именно владелец обязан был объявить, — это решение домена.
// Он судит, что объявление и провязка суть одно решение, а не два, разъехавшихся
// побочным эффектом чужой правки (`architecture.md` §«Параллельные полосы одного
// механизма обязаны сверяться МЕЖДУ СОБОЙ»).
//
// # Перепись печатает ОБЕ величины
//
// «полос N · чистят M» — одно число скрывает ровно тот случай, ради которого
// гейт заведён: пять полос, из них чистят две, — это законное состояние
// (остальным три предмет заведён отдельной задачей), а «полос 5» без второго
// числа читалось бы как «механизм есть у всех».
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// journalLaneServicesRoot — где живут владельцы журнала.
const journalLaneServicesRoot = "../../services"

// TestSubscriptionJournalLanesAgreeOnRetention — объявление удержания и провязка
// уборщика суть одно решение.
func TestSubscriptionJournalLanesAgreeOnRetention(t *testing.T) {
	lanes, census := collectJournalLanes(t, journalLaneServicesRoot)

	if census.Lanes == 0 {
		t.Fatalf("полос журнала подписки не распознано НИ ОДНОЙ (файлов объявления прочитано %d): "+
			"это слепота разбора, а не благополучие дерева", census.FilesRead)
	}
	if census.RetentionDeclared == 0 {
		t.Fatalf("ни одна из %d полос не объявила удержания: разбор перестал читать поле Retention",
			census.Lanes)
	}
	if census.TablesResolved == 0 {
		t.Fatalf("имя таблицы не разобрано НИ У ОДНОЙ из %d полос: сравнивать полосы стало нечем",
			census.Lanes)
	}
	if census.SweepFilesRead == 0 {
		t.Fatal("прод-файлов владельцев не прочитано ни одного: провязку искать было негде")
	}

	findings := JournalLaneFindings(lanes, func(l JournalLane) bool {
		return ageColumnDefaultsToDatabaseClock(t, l)
	})

	sort.Strings(findings)
	t.Logf("перепись: полос %d · чистят %d · объявили удержание %d · имён таблиц разобрано %d "+
		"(файлов объявления %d, прод-файлов владельцев %d)",
		census.Lanes, census.Sweeping, census.RetentionDeclared, census.TablesResolved,
		census.FilesRead, census.SweepFilesRead)
	for _, l := range lanes {
		t.Logf("  %s/%s: %s · уборщик %s", l.Owner, TableNameOf(l.Table), l.Retention,
			map[bool]string{true: "провязан (" + l.SweeperFile + ")", false: "не провязан"}[l.Sweeper])
	}
	if len(findings) > 0 {
		t.Fatalf("полосы механизма разошлись, и это никем не решалось:\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// collectJournalLanes обходит владельцев и собирает их объявления и провязки.
func collectJournalLanes(t *testing.T, servicesRoot string) ([]JournalLane, JournalLaneCensus) {
	t.Helper()

	entries, err := os.ReadDir(servicesRoot)
	if err != nil {
		t.Fatalf("каталог служб %s: %v", servicesRoot, err)
	}

	var (
		lanes  []JournalLane
		census JournalLaneCensus
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		owner := "services/" + e.Name()
		declDir := filepath.Join(servicesRoot, e.Name(), "internal", "subscriptionjournal")
		if _, err := os.Stat(declDir); err != nil {
			// У владельца журнала нет — законное состояние (`geo`, `iam`).
			continue
		}
		files, err := treecorpus.UnderWithSuffix(declDir, ".go")
		if err != nil {
			t.Fatalf("обход %s: %v", declDir, err)
		}
		var lane JournalLane
		got := false
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("чтение %s: %v", path, rerr)
			}
			census.FilesRead++
			l, found, perr := ScanJournalLane(owner, path, src)
			if perr != nil {
				t.Fatalf("разбор %s: %v", path, perr)
			}
			if found {
				lane, got = l, true
			}
		}
		if !got {
			continue
		}
		census.Lanes++
		if lane.Retention != "" {
			census.RetentionDeclared++
		}
		if lane.Table != "" {
			census.TablesResolved++
		}

		// Провязка ищется по ВСЕМУ прод-дереву владельца: она законно живёт и в
		// композиционном корне, и в отдельном файле проводки рядом с ним.
		ownerFiles, err := treecorpus.UnderWithSuffix(filepath.Join(servicesRoot, e.Name()), ".go")
		if err != nil {
			t.Fatalf("обход %s: %v", owner, err)
		}
		for _, path := range ownerFiles {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("чтение %s: %v", path, rerr)
			}
			census.SweepFilesRead++
			wired, perr := JournalSweepWiredIn(path, src)
			if perr != nil {
				t.Fatalf("разбор %s: %v", path, perr)
			}
			if wired && !lane.Sweeper {
				lane.Sweeper = true
				lane.SweeperFile = filepath.Base(path)
			}
		}
		if lane.Sweeper {
			census.Sweeping++
		}
		lanes = append(lanes, lane)
	}
	sort.Slice(lanes, func(i, j int) bool { return lanes[i].Owner < lanes[j].Owner })
	return lanes, census
}

// ageColumnDefaultsToDatabaseClock — объявлена ли колонка срока умолчанием
// `now()` в миграциях владельца.
//
// Утверждение проверяется по СХЕМЕ, а не по намерению: у порога уборки нет
// слагаемого на разницу часов ровно потому, что отметку ставит база — та же, что
// исполняет предикат. Сменится умолчание на часы процесса — слагаемое появится, а
// порог о нём не узнает.
func ageColumnDefaultsToDatabaseClock(t *testing.T, l JournalLane) bool {
	t.Helper()
	svc := strings.TrimPrefix(l.Owner, "services/")
	dir := filepath.Join(journalLaneServicesRoot, svc, "internal", "migrations")
	files, err := treecorpus.UnderWithSuffix(dir, ".sql")
	if err != nil {
		t.Fatalf("обход миграций %s: %v", l.Owner, err)
	}
	table := TableNameOf(l.Table)
	for _, path := range files {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		text := strings.ToLower(string(src))
		if !strings.Contains(text, "create table") || !strings.Contains(text, table) {
			continue
		}
		// Объявление колонки и её умолчание стоят в одной строке определения.
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, strings.ToLower(l.AgeColumn)) {
				continue
			}
			if strings.Contains(line, "default now()") {
				return true
			}
		}
	}
	return false
}
