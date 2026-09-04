// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredledger_injection_test.go — доказательство способности гейта надгробия
// упасть И смолчать.
//
// Каждая ось проверяется в ОБЕ стороны: внесённый дефект обязан дать находку и
// назвать координату, а законный близнец той же формы — молчание. Односторонняя
// проба зеленела бы на судье, который отвергает всё, и краснела бы на судье,
// который не отвергает ничего.
//
// Мир каждого отрицательного кейса отличается от близнеца ОДНИМ фактом — тем,
// что кейс называет. Иначе неизвестно, какой из двух дал красное.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func liveSet(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func goodLedger() *RetiredLedger {
	return &RetiredLedger{
		Service:          "iam",
		ConsolidatedInto: "20260904000000_iam_schema_baseline.sql",
		Retired:          []string{"0001_initial.sql", "0008_drop_organizations.sql"},
	}
}

func TestRetiredLedgerInjection(t *testing.T) {
	present := liveSet("20260904000000_iam_schema_baseline.sql")

	// ── Положительный контроль: законное надгробие молчит. Без него всякое
	// отрицание ниже зеленело бы на судье, отвергающем что угодно.
	if v := RetiredLedgerViolations("iam", goodLedger(), present); len(v) != 0 {
		t.Fatalf("законное надгробие обязано молчать, получено: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*RetiredLedger)
		present map[string]bool
		want    string
	}{
		{
			// Несущая ось: запись прикрывает ЖИВУЮ миграцию. Ровно так
			// ведомость послаблений становится маской.
			name:    "запись называет снятой миграцию, которая в каталоге есть",
			mutate:  func(l *RetiredLedger) {},
			present: liveSet("20260904000000_iam_schema_baseline.sql", "0001_initial.sql"),
			want:    "0001_initial.sql",
		},
		{
			name:    "надгробие объявляет чужой сервис",
			mutate:  func(l *RetiredLedger) { l.Service = "vpc" },
			present: present,
			want:    "не в своём каталоге",
		},
		{
			name:    "сводной миграции, которую надгробие называет, в каталоге нет",
			mutate:  func(l *RetiredLedger) { l.ConsolidatedInto = "20260101000000_nope.sql" },
			present: present,
			want:    "которой в каталоге нет",
		},
		{
			name:    "надгробие не называет сводную миграцию",
			mutate:  func(l *RetiredLedger) { l.ConsolidatedInto = "" },
			present: present,
			want:    "не называет сводную миграцию",
		},
		{
			name:    "надгробие не называет ни одной снятой миграции",
			mutate:  func(l *RetiredLedger) { l.Retired = nil },
			present: present,
			want:    "послабление без предмета",
		},
		{
			name:    "одно имя названо дважды",
			mutate:  func(l *RetiredLedger) { l.Retired = append(l.Retired, "0001_initial.sql") },
			present: present,
			want:    "дважды",
		},
		{
			name: "сводная миграция числится среди снятых",
			mutate: func(l *RetiredLedger) {
				l.Retired = append(l.Retired, l.ConsolidatedInto)
			},
			present: present,
			want:    "среди снятых",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := goodLedger()
			c.mutate(l)
			v := RetiredLedgerViolations("iam", l, c.present)
			if len(v) == 0 {
				t.Fatalf("дефект внесён, находки нет — гейт не способен упасть по этой оси")
			}
			joined := strings.Join(v, "\n")
			if !strings.Contains(joined, c.want) {
				t.Fatalf("находка есть, но не называет предмет %q:\n%s", c.want, joined)
			}
		})
	}
}

// Отсутствие надгробия — состояние дерева до первого сведения и ЦЕЛЬ после того,
// как цитаты уйдут. Проба, падающая на достижении своей цели, толкала бы держать
// запись ради зелёного.
func TestRetiredLedgerAbsenceIsNotAFinding(t *testing.T) {
	dir := t.TempDir()
	l, err := ReadRetiredLedger(dir)
	if err != nil {
		t.Fatalf("отсутствие надгробия — не ошибка, получено: %v", err)
	}
	if l != nil {
		t.Fatalf("в пустом каталоге надгробия быть не может, получено: %+v", l)
	}
	if v := RetiredLedgerViolations("iam", nil, liveSet()); len(v) != 0 {
		t.Fatalf("отсутствующее надгробие не судится, получено: %v", v)
	}
}

// Битое надгробие обязано быть ОШИБКОЙ, а не молчанием: неразобранный файл,
// прочитанный как «надгробия нет», снял бы проверку целиком и выглядел бы как
// чистое дерево.
func TestRetiredLedgerUnparseableIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RetiredLedgerName), []byte("{не json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRetiredLedger(dir); err == nil {
		t.Fatal("битое надгробие прочитано молча — отказ разбора неотличим от отсутствия файла")
	}
}
