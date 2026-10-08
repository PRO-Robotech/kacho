// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// anonMailMigration — миграция хранилища ограничителя анонимной почты (NTF-2).
const anonMailMigration = "migrations/20261001180000_anon_mail_limiter.sql"

var reCreateTable = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?kacho_gateway\.(\w+)`)

// subjectsFinding — таблицы миграции без предмета уборки и без довода, и
// предметы без таблицы.
func subjectsFinding(sql string, subjects []anonMailSubject) (tables []string, findings []string) {
	known := map[string]bool{}
	for _, s := range subjects {
		known[s.table] = true
		if (s.reap == "") == (s.reason == "") {
			findings = append(findings, s.table+": у предмета ровно одно из двух — уборка или довод")
		}
	}
	seen := map[string]bool{}
	for _, m := range reCreateTable.FindAllStringSubmatch(sql, -1) {
		tables = append(tables, m[1])
		seen[m[1]] = true
		if !known[m[1]] {
			findings = append(findings, "таблица "+m[1]+" без предмета уборки и без довода")
		}
	}
	for _, s := range subjects {
		if !seen[s.table] {
			findings = append(findings, "предмет "+s.table+" без таблицы в миграции")
		}
	}
	sort.Strings(tables)
	return tables, findings
}

// TestAnonMailStorageSubjectsAreClosed — перечень предметов хранения края
// закрыт (З26, CX2-35): у каждой таблицы миграции ограничителя есть строка
// «чем снимается» у уборщика хранилища однократности, либо довод. Печать
// «таблиц N · предметов уборки N · доводов M». Инъекция таблицы без предмета в
// копию миграции — красный с её именем.
func TestAnonMailStorageSubjectsAreClosed(t *testing.T) {
	b, err := os.ReadFile(anonMailMigration)
	if err != nil {
		t.Fatal(err)
	}
	tables, findings := subjectsFinding(string(b), anonMailSubjects)
	reaped, reasons := 0, 0
	for _, s := range anonMailSubjects {
		if s.reap != "" {
			reaped++
		} else {
			reasons++
		}
	}
	t.Logf("таблиц %d %v · предметов уборки %d · доводов %d", len(tables), tables, reaped, reasons)
	if len(tables) == 0 {
		t.Fatal("в миграции не найдено ни одной таблицы — обход пуст")
	}
	for _, f := range findings {
		t.Error(f)
	}
	injected := string(b) + "\nCREATE TABLE IF NOT EXISTS kacho_gateway.anon_mail_orphan (id int);\n"
	if _, f := subjectsFinding(injected, anonMailSubjects); len(f) != 1 || !strings.Contains(f[0], "anon_mail_orphan") {
		t.Fatalf("инъекция таблицы без предмета не найдена: %v", f)
	}
}

// TestAnonMailPassRetentionExceedsEveryReadingWindow — срок хранения моментов
// края — функция над таблицей границ (З26, CX2-29): строго больше верхней
// границы каждого окна, которое их читает; на границах Р8 — 25 ч.
func TestAnonMailPassRetentionExceedsEveryReadingWindow(t *testing.T) {
	r := config.AnonMailPassRetention()
	if r <= config.AnonMailWindowUpperBound() {
		t.Fatalf("срок хранения %s не больше верхней границы окон %s", r, config.AnonMailWindowUpperBound())
	}
	if r != 25*time.Hour {
		t.Errorf("срок хранения на границах Р8 %s, по таблице замысла — 25 ч", r)
	}
	t.Logf("верхняя граница окон %s · срок хранения %s", config.AnonMailWindowUpperBound(), r)
}
