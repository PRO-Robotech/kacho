// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck

// installation_records_test.go — записи SPF и DMARC, которые установки
// объявляют в deploy/stacks-mail-dns.txt (kacho#3017), проходят ЭТИ ЖЕ
// таблицы разбора стража старта.
//
// Записи публикуются во внешнем DNS установки рукой оператора, и страж notify
// увидит их только на старте пода — отказом с причиной. Здесь объявленная
// запись судится той же функцией, что судит опубликованную: второй реализации
// формы нет, и запись, которую страж отверг бы (например, `redirect=` вместо
// `-all` или `p=none`), не доезжает до регистратора.
//
// Запись DKIM в таблице не объявляется (её выводит посев из объекта ключа), её
// здесь не судят.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const installationRecordsTable = "../../../../deploy/stacks-mail-dns.txt"

type installationRecord struct {
	profile, kind, value string
	line                 int
}

func readInstallationRecords(t *testing.T) []installationRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(installationRecordsTable))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица записей установок %s не читается: %v", installationRecordsTable, err)
	}
	var out []installationRecord
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "|", 3)
		if len(f) != 3 || (f[1] != "spf" && f[1] != "dmarc") || f[2] == "" {
			t.Fatalf("КРАСНЫЙ: строка %d таблицы %s не разобрана: %q — форма <профиль>|spf|dmarc|<запись>",
				i+1, installationRecordsTable, line)
		}
		out = append(out, installationRecord{profile: f[0], kind: f[1], value: f[2], line: i + 1})
	}
	return out
}

func TestDeclaredInstallationRecordsPassTheGuardTables(t *testing.T) {
	recs := readInstallationRecords(t)
	if len(recs) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s ни одной записи — судить нечего", installationRecordsTable)
	}
	for _, r := range recs {
		var res Result
		switch r.kind {
		case "spf":
			res = parseSPF([]string{r.value})
		case "dmarc":
			res = parseDMARC([]string{r.value})
		}
		t.Logf("%s %s (строка %d): %s %s", r.profile, r.kind, r.line, res.Outcome, res.Reason)
		if res.Outcome != OutcomeOK {
			t.Errorf("%s: запись %s строки %d отвергнута стражем DNS notify: %s", r.profile, r.kind, r.line, res.Reason)
		}
	}
}

// Способность упасть: записи таблицы с ровно одним изменённым фактом проходят
// тот же путь (разбор строки таблицы → таблица стража).
func TestDeclaredInstallationRecordsInjections(t *testing.T) {
	var spf, dmarc string
	for _, r := range readInstallationRecords(t) {
		switch {
		case r.kind == "spf" && spf == "":
			spf = r.value
		case r.kind == "dmarc" && dmarc == "":
			dmarc = r.value
		}
	}
	if spf == "" || dmarc == "" {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: в таблице нет записи spf или dmarc — вход инъекции исчез")
	}
	for _, c := range []struct {
		name string
		res  Result
		want Reason
	}{
		{"spf: -all заменён на ?all", parseSPF([]string{strings.Replace(spf, "-all", "?all", 1)}), ReasonSPFAllPermissive},
		{"spf: -all заменён на redirect=", parseSPF([]string{strings.Replace(spf, "-all", "redirect=_spf.example.org", 1)}), ReasonSPFRedirect},
		{"dmarc: p=reject заменён на p=none", parseDMARC([]string{strings.Replace(dmarc, "p=reject", "p=none", 1)}), ReasonDMARCPolicyNone},
	} {
		if c.res.Outcome != OutcomeViolated || c.res.Reason != c.want {
			t.Errorf("%s: исход %s %s, ожидалось нарушение %s", c.name, c.res.Outcome, c.res.Reason, c.want)
		}
	}
}
