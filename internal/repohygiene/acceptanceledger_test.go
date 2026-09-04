// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ledgerPath — ведомость приёмок, под которыми ведётся кодирование дерева.
const ledgerPath = "docs/acceptance-ledger.yaml"

type ledgerEntry struct {
	Acceptance string `yaml:"acceptance"`
	Prefix     string `yaml:"prefix"`
	Verdict    string `yaml:"verdict"`
	// VerdictWithdrawn — санкция БЫЛА вынесена и снята позже, а не «ещё не
	// выносилась». Различие не косметическое: отставшая запись чинится
	// перечитыванием шапки на свежей ревизии воркспейса, а отозванная — не
	// чинится им вовсе (на названной ревизии там по-прежнему APPROVED), и
	// требует нового круга ревью.
	VerdictWithdrawn  bool   `yaml:"verdict_withdrawn"`
	VerdictDated      string `yaml:"verdict_dated"`
	WorkspaceRevision string `yaml:"workspace_revision"`
	DebtIssue         int    `yaml:"debt_issue"`
	Note              string `yaml:"note"`
}

type ledger struct {
	Entries []ledgerEntry `yaml:"entries"`
}

// citedAcceptance — цитата приёмки в тексте миграции: либо имя её документа,
// либо код сценария (`IAM-ID-1-08`), чей префикс называет документ.
var (
	reAcceptanceDoc = regexp.MustCompile(`sub-phase-[A-Za-z0-9._-]+-acceptance\.md`)
	reScenarioCode  = regexp.MustCompile(`\b([A-Z]{2,8}(?:-[A-Z]{2,4})?-[0-9]+(?:\.[0-9]+)?)-[0-9]{2}\b`)
)

func loadLedger(t *testing.T, root string) ledger {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ledgerPath))
	if err != nil {
		t.Fatalf("ведомость приёмок не читается (%s): %v — гейт без неё судил бы "+
			"по неизвестному основанию", ledgerPath, err)
	}
	var l ledger
	if err := yaml.Unmarshal(raw, &l); err != nil {
		t.Fatalf("ведомость не разбирается: %v", err)
	}
	if len(l.Entries) == 0 {
		t.Fatal("ведомость пуста: гейт стал бы тождественно-зелёным — всякая " +
			"цитата разрешалась бы в «записи нет», а находки печатались бы " +
			"только про отсутствие, никогда про вердикт")
	}
	return l
}

// citationsIn возвращает приёмки, которые цитирует текст миграции: по имени
// документа и по префиксу сценария.
func citationsIn(text string) (docs, prefixes []string) {
	for _, m := range reAcceptanceDoc.FindAllString(text, -1) {
		docs = append(docs, m)
	}
	for _, m := range reScenarioCode.FindAllStringSubmatch(text, -1) {
		prefixes = append(prefixes, m[1])
	}
	return uniq(docs), uniq(prefixes)
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// TestNewMigrationCitesAnApprovedAcceptance — миграция, ДОБАВЛЕННАЯ этим
// изменением и цитирующая приёмку, обязана цитировать APPROVED-приёмку (#938).
//
// # Предмет
//
// Кодировать без утверждённой приёмки запрещено (ban #1), и запрет держался
// вниманием. Внимания не хватило: две стадии линии сели в ствол ДО вердикта —
// тем самым слиянием, которое вердикт и должно было предварять.
//
// # Почему ведомость, а не чтение самой приёмки
//
// Приёмки живут в другом репозитории, и гейт продукта прочитать их вердикт не
// может by construction. Ведомость — перенос вердикта в дерево: утверждение с
// координатой вместо молчания. Соврать в ней можно, но ложь становится видимой
// строкой с автором и ревизией, а не отсутствием проверки.
//
// # Почему только ДОБАВЛЕННЫЕ
//
// Момент, который проверяет ban #1, — момент коммита. Для миграций, уже стоящих
// в стволе, он прошёл; их состояние — перепись, а не вердикт. Требовать APPROVED
// от них значило бы держать ствол красным за прошлое, которого правкой не
// изменить (ban #5 запрещает править применённую миграцию).
func TestNewMigrationCitesAnApprovedAcceptance(t *testing.T) {
	root := repoRoot(t)
	l := loadLedger(t, root)

	base := requireTrunkRef(t, root)

	added, findings, err := auditNewMigrations(root, base, l)
	if err != nil {
		t.Fatalf("перечислить добавленные файлы: %v", err)
	}

	t.Logf("осмотрено: миграций, добавленных относительно %s — %d; записей ведомости — %d",
		base, added, len(l.Entries))

	for _, f := range findings {
		t.Error(f)
	}
}

// auditNewMigrations — ядро гейта, отделённое от корня дерева НАМЕРЕННО: инъекция
// обязана прогнать его на синтетическом репозитории, а не на этом. Возвращает
// число осмотренных миграций и находки.
func auditNewMigrations(root, base string, l ledger) (int, []string, error) {
	byDoc := map[string]ledgerEntry{}
	byPrefix := map[string]ledgerEntry{}
	for _, e := range l.Entries {
		byDoc[e.Acceptance] = e
		if e.Prefix != "" {
			byPrefix[e.Prefix] = e
		}
	}

	out, err := gitenv.Command(root, "diff", "--name-only", "--diff-filter=A",
		base+"...HEAD").Output()
	if err != nil {
		return 0, nil, err
	}

	var added int
	var findings []string
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel == "" || !strings.Contains(rel, "/internal/migrations/") ||
			!strings.HasSuffix(rel, ".sql") {
			continue
		}
		added++
		body, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			continue // файл мог быть снят следующим коммитом ветки
		}
		docs, prefixes := citationsIn(string(body))
		for _, d := range docs {
			check(&findings, rel, "приёмка "+d, byDoc[d], byDoc, d)
		}
		for _, pfx := range prefixes {
			if _, known := byPrefix[pfx]; !known {
				findings = append(findings, rel+": сценарий «"+pfx+
					"-NN» цитирует приёмку, которой нет в "+ledgerPath+
					" — основание кодирования не названо")
				continue
			}
			check(&findings, rel, "сценарий "+pfx+"-NN", byPrefix[pfx], byDoc, pfx)
		}
	}
	return added, findings, nil
}

func check(findings *[]string, rel, what string, e ledgerEntry, byDoc map[string]ledgerEntry, key string) {
	if e.Acceptance == "" {
		if _, ok := byDoc[key]; !ok {
			*findings = append(*findings, rel+": "+what+
				" — записи в "+ledgerPath+" нет, основание кодирования не названо")
		}
		return
	}
	if e.Verdict != "APPROVED" {
		// Два состояния выглядят в записи одинаково («не APPROVED») и требуют
		// РАЗНОГО: отставшую запись чинит перечитывание шапки на свежем стволе
		// воркспейса, отозванную — не чинит вовсе, потому что на названной
		// ревизии там по-прежнему стоит прежний вердикт.
		why := "санкции не было; если запись отстала — перечитай шапку документа " +
			"на свежей ревизии ствола воркспейса"
		if e.VerdictWithdrawn {
			why = "санкция БЫЛА и ОТОЗВАНА: запись не устарела, и обновление копии " +
				"её не починит — нужен новый круг ревью"
		}
		*findings = append(*findings, rel+": "+what+" — вердикт «"+e.Verdict+
			"» на "+e.VerdictDated+" (ревизия воркспейса "+e.WorkspaceRevision+
			"); "+why+". Кодировать без APPROVED запрещено (ban #1): либо получить "+
			"вердикт, либо не вносить миграцию")
	}
}

// TestAcceptanceLedgerEntriesHaveASubject — запись ведомости живёт, пока её
// цитирует дерево, а неутверждённая — пока названа задачей.
//
// Послабление обязано истекать САМО: запись, которой больше нечего обосновывать,
// унаследует следующая слепая зона, а долг без номера задачи не имеет
// ответственного.
func TestAcceptanceLedgerEntriesHaveASubject(t *testing.T) {
	root := repoRoot(t)
	l := loadLedger(t, root)

	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	if err != nil {
		t.Fatalf("перечислить миграции: %v", err)
	}

	citedDocs := map[string]bool{}
	citedPrefixes := map[string]bool{}
	var scanned int
	for _, path := range files {
		if !strings.Contains(path, "/internal/migrations/") {
			continue
		}
		scanned++
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		docs, prefixes := citationsIn(string(body))
		for _, d := range docs {
			citedDocs[d] = true
		}
		for _, p := range prefixes {
			citedPrefixes[p] = true
		}
	}

	t.Logf("осмотрено миграций дерева %d; цитируемых документов приёмок %d, "+
		"префиксов сценариев %d; записей ведомости %d",
		scanned, len(citedDocs), len(citedPrefixes), len(l.Entries))

	if scanned == 0 {
		t.Fatal("миграций не прочитано ни одной — «ноль находок» здесь означало бы " +
			"«ноль прочитанного»")
	}

	for _, f := range auditLedgerEntries(l, citedDocs, citedPrefixes) {
		t.Error(f)
	}
}

// auditLedgerEntries — ядро пообъектного разбора записей, отделённое от корня
// дерева НАМЕРЕННО: инъекция обязана прогнать его на синтетических записях, а не
// на тех, что лежат в этом дереве.
func auditLedgerEntries(l ledger, citedDocs, citedPrefixes map[string]bool) []string {
	var findings []string
	for _, e := range l.Entries {
		if !citedDocs[e.Acceptance] && !citedPrefixes[e.Prefix] {
			findings = append(findings, ledgerPath+": запись «"+e.Acceptance+
				"» не цитируется ни одной миграцией дерева — у послабления нет "+
				"предмета, и оно переживёт то, ради чего заведено")
		}
		if e.Verdict != "APPROVED" && e.DebtIssue == 0 {
			findings = append(findings, ledgerPath+": запись «"+e.Acceptance+
				"» несёт вердикт «"+e.Verdict+"» и не называет задачи — за долгом "+
				"никто не отвечает")
		}
		if e.WorkspaceRevision == "" || e.VerdictDated == "" {
			findings = append(findings, ledgerPath+": запись «"+e.Acceptance+
				"» не называет ревизию воркспейса или дату вердикта — утверждение "+
				"о неизвестном моменте")
		}
		// Отзыв истекает САМ: вернулся APPROVED — поле обязано уйти вместе с ним,
		// иначе запись объявляет действующую санкцию и её отсутствие разом.
		if e.Verdict == "APPROVED" && e.VerdictWithdrawn {
			findings = append(findings, ledgerPath+": запись «"+e.Acceptance+
				"» объявляет вердикт APPROVED и отзыв санкции разом — два "+
				"утверждения об одном предмете, из которых верно одно")
		}
	}
	return findings
}
