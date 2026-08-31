// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// syntheticCarrier — эталонная форма носителя, дословно повторяющая объявление
// общего. Стоит ПОЛОЖИТЕЛЬНЫМ КОНТРОЛЁМ в каждом синтетическом дереве: пока
// распознаватель не узнал носитель здесь, его ноль под services/ не значит
// ничего.
const syntheticCarrier = `package health

import "context"

type Checker struct {
	Name  string
	Check func(ctx context.Context) error
}
`

// legitimateConsumer — сервис, потребляющий ОБЩИЙ носитель: литерал с теми же
// полями, но своего типа он не объявляет.
//
// Он стоит вторым сервисом в каждом дереве и обязан молчать всегда. Различие
// «объявляет тип» против «пользуется чужим» и есть предмет гейта: без этого
// близнеца гейт краснел бы на всех шести исправных сервисах, а такую проверку
// снимают первой.
const legitimateConsumer = `package main

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func checkers(ping func(context.Context) error) []health.Checker {
	return []health.Checker{
		{Name: "database", Check: ping},
		{Name: "lro-worker", Check: func(context.Context) error { return nil }},
	}
}
`

// Способность гейта «готовность отдаётся ОДНИМ носителем» упасть и смолчать —
// доказана инъекцией, а не прочтением.
//
// Инъекция снимает НОВОЕ свойство (носитель общий) у элемента, чьи прочие
// свойства на месте: синтетический сервис регистрирует маршруты и называет
// зависимости так же, как исправный, и отличается ровно тем, что объявляет
// собственный тип. Иначе красное приходило бы от соседней оси, и вакуумность
// проверяемой осталась бы незамеченной (`testing.md` §«Гейт на класс», п.2в).
func TestReadinessSingleCarrierInjectionCutsBothWays(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		find  bool
		says  []string
	}{
		{
			name: "НАХОДКА: сервис объявил СВОЙ носитель готовности",
			files: map[string]string{
				"services/broken/internal/handler/diag.go": `package handler

import "context"

type ReadinessChecker struct {
	Name  string
	Check func(context.Context) error
}
`,
			},
			find: true,
			says: []string{"broken", "ReadinessChecker", "НЕ ДОЕДЕТ"},
		},
		{
			name: "НАХОДКА: свой носитель назван иначе — судится СТРУКТУРА, не имя",
			files: map[string]string{
				"services/broken/internal/probe/probe.go": `package probe

import "context"

// Имя типа другое, поля те же: привязка распознавателя к имени сделала бы его
// слепым к следующему носителю.
type DependencyProbe struct {
	Name  string
	Check func(c context.Context) error
}
`,
			},
			find: true,
			says: []string{"broken", "DependencyProbe"},
		},
		{
			name: "МОЛЧИТ: таблица требований — поле Check ДРУГОЙ подписи (форма из дерева)",
			files: map[string]string{
				"services/quiet/internal/config/lanes.go": `package config

// Живёт в дереве владельца прав: колонка-предикат, а не носитель готовности.
type LaneRequirement struct {
	Element string
	Check   func(Config, LaneWiring) error
}

type Config struct{}

type LaneWiring struct{}
`,
			},
			find: false,
		},
		{
			name: "МОЛЧИТ: поле Name есть, а Check принимает не контекст",
			files: map[string]string{
				"services/quiet/internal/rules/rules.go": `package rules

type Rule struct {
	Name  string
	Check func(input string) error
}
`,
			},
			find: false,
		},
		{
			name: "МОЛЧИТ: проверка контекстом есть, именующего поля нет",
			files: map[string]string{
				"services/quiet/internal/gate/gate.go": `package gate

import "context"

type Guard struct {
	Description string
	Check       func(context.Context) error
}
`,
			},
			find: false,
		},
		{
			name: "МОЛЧИТ: Name не строка — тип поля судится, а не только его имя",
			files: map[string]string{
				"services/quiet/internal/step/step.go": `package step

import "context"

type Step struct {
	Name  int
	Check func(context.Context) error
}
`,
			},
			find: false,
		},
		{
			name: "МОЛЧИТ: литерал общего носителя — потребление, а не объявление",
			files: map[string]string{
				"services/quiet/cmd/quiet/diag.go": legitimateConsumer,
			},
			find: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			services := map[string]string{"services/legit/cmd/legit/diag.go": legitimateConsumer}
			for rel, src := range tc.files {
				services[rel] = src
			}
			carrierRel := "pkg/observability/health/health.go"
			ct3Write(t, root, carrierRel, syntheticCarrier)

			var serviceRels []string
			for rel, src := range services {
				ct3Write(t, root, rel, src)
				serviceRels = append(serviceRels, rel)
			}
			sort.Strings(serviceRels)

			findings, cen, err := auditReadinessCarrierIsSingle(root, serviceRels, []string{carrierRel})
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			if cen.FilesRead != len(serviceRels)+1 {
				t.Fatalf("разобрано %d файлов из %d — обход неполон, вердикт беспредметен",
					cen.FilesRead, len(serviceRels)+1)
			}
			// Положительный контроль в КАЖДОМ прогоне: распознаватель обязан узнать
			// эталонную форму, иначе его ноль ниже не значит ничего.
			if got := cen.CarriersAll - cen.CarriersOwn; got != 1 {
				t.Fatalf("распознаватель нашёл %d носителей в эталонном пакете вместо 1 — "+
					"он неспособен узнать даже ту форму, ради которой заведён", got)
			}

			var got []string
			for _, f := range findings {
				got = append(got, f.Service+"/"+f.Type+": "+f.Why)
			}
			joined := strings.Join(got, "\n")

			// Законный близнец обязан молчать в КАЖДОМ прогоне.
			if strings.Contains(joined, "legit/") {
				t.Fatalf("потребитель общего носителя объявлен нарушителем — гейт ловит форму, "+
					"а не существо, и покраснел бы на всех шести исправных сервисах:\n%s", joined)
			}
			if tc.find && len(findings) == 0 {
				t.Fatalf("гейт смолчал на возвращённом дефекте — падать он не способен\nперепись: %+v", cen)
			}
			if !tc.find && len(findings) != 0 {
				t.Fatalf("гейт покраснел на законной конструкции:\n%s", joined)
			}
			for _, want := range tc.says {
				if !strings.Contains(joined, want) {
					t.Errorf("находка не называет %q — читатель пойдёт искать не там:\n%s", want, joined)
				}
			}
		})
	}
}

// Предпосылка гейта теряется вместе с общим носителем — и он обязан это ЗАМЕТИТЬ.
//
// # Предмет
//
// Утверждение гейта отрицательное («собственных носителей ноль»), а такое теряет
// вход МОЛЧА: снимут общий носитель или переименуют его поля — распознаватель
// перестанет узнавать что-либо вовсе и напечатает тот же ноль
// (`testing.md` §«Гейт на класс», п.9). Отличить это от чистого дерева нечем,
// кроме положительного контроля, и здесь доказывается, что контроль работает.
func TestReadinessSingleCarrierNoticesItsOwnPremiseIsGone(t *testing.T) {
	root := t.TempDir()
	// Носитель СМЕНИЛ форму: поле переименовано, распознаватель его не узнаёт.
	carrierRel := "pkg/observability/health/health.go"
	ct3Write(t, root, carrierRel, `package health

import "context"

type Checker struct {
	Label string
	Check func(context.Context) error
}
`)
	serviceRel := "services/legit/cmd/legit/diag.go"
	ct3Write(t, root, serviceRel, legitimateConsumer)

	_, cen, err := auditReadinessCarrierIsSingle(root, []string{serviceRel}, []string{carrierRel})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if got := cen.CarriersAll - cen.CarriersOwn; got != 0 {
		t.Fatalf("распознаватель насчитал %d общих носителей на форме, которой не знает — "+
			"тогда положительный контроль гейта ничего не проверяет", got)
	}
	// Утверждение о гейте, а не о переписи: ноль общих носителей — это то условие,
	// на котором `TestReadinessIsServedByASingleCarrier` обязан упасть, а не
	// сообщить о чистом дереве.
	if cen.CarriersOwn != 0 {
		t.Fatalf("собственных носителей %d при потерянной предпосылке — вердикт беспредметен",
			cen.CarriersOwn)
	}
}

// ct3Write кладёт файл синтетического дерева.
func ct3Write(t *testing.T, root, rel, src string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("создание каталога %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(src), 0o600); err != nil {
		t.Fatalf("запись %s: %v", rel, err)
	}
}
