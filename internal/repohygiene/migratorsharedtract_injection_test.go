// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorsharedtract_injection_test.go — доказательство, что гейт общей
// половины тракта СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Инъекция настоящая: каждый случай взят с живого исходника до сведения (#1383),
// а не выдуман. Законные близнецы — тоже живые: сведённая обёртка ЗОВЁТ общий
// разбор и объясняет запрет прозой, и обе формы обязаны молчать.
package repohygiene

import (
	"strings"
	"testing"
)

const (
	// srcTractConverged — сведённая обёртка, как она выглядит после #1383:
	// делегирует общему предикату и зовёт общий разбор цели.
	srcTractConverged = `package migrator

import "github.com/PRO-Robotech/kacho/pkg/migratorcli"

func (c Config) Validate() error {
	return migratorcli.RunnerPreconditions{Service: c.Service, DSN: c.DSN}.Validate()
}

func (r *Runner) Up(target string) error {
	_, err := migratorcli.ParseTargetVersion(target)
	return err
}`

	// srcTractRedeclaresRefusal — то, что было: свой текст отказа в копии.
	srcTractRedeclaresRefusal = `package migrator

import "errors"

func (c Config) Validate() error {
	if c.DSN == "" {
		return errors.New("dsn is empty (set --dsn or KACHO_MIGRATOR_DSN)")
	}
	return nil
}`

	// srcTractOwnTargetParser — то, что было: свой разбор цели форматным чтением.
	srcTractOwnTargetParser = `package migrator

import "fmt"

func parseTargetVersion(s string) (int64, error) {
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0, err
	}
	return v, nil
}`

	// srcTractRefusalOnlyInProse — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: текст
	// отказа стоит в комментарии, объясняющем сам запрет. Ровно так он стоит в
	// шапке migratorsharedtract.go, поэтому гейт по подстроке краснел бы на
	// собственном объяснении.
	srcTractRefusalOnlyInProse = `package migrator

// Свой текст отказа здесь НЕ объявляется: "dsn is empty" и остальные пять живут
// в общем пакете. Разбор цели тоже общий — fmt.Sscanf отсюда снят, он на
// "12abc" отдавал 12 без ошибки.
import "github.com/PRO-Robotech/kacho/pkg/migratorcli"

func (c Config) Validate() error { return migratorcli.RunnerPreconditions{}.Validate() }`

	// srcTractThirdForm — дефект СОСЕДНЕГО гейта (третья форма наката). Нужен,
	// чтобы показать: моя проверка на нём молчит, а его — краснеет.
	srcTractThirdForm = `package main

import "github.com/PRO-Robotech/kacho/pkg/db"

func main() { _ = db.Open }`
)

// auditSource — одна инъекция через ТЕ ЖЕ функции, которые зовёт гейт. Своя
// копия разбора доказывала бы что-то о копии, а не о гейте.
func auditSource(t *testing.T, rel, src string) []string {
	t.Helper()
	var findings []migratorTractFinding

	lits, err := stringLiteralsOfGoSource(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	for _, marker := range migratorRefusalMarkers {
		for _, lit := range lits {
			if strings.Contains(lit, marker) {
				findings = append(findings, migratorTractFinding{
					Rel: rel, What: "заново объявляет текст отказа предусловий " + quotedMarker(marker)})
				break
			}
		}
	}
	own, err := declaresOwnTargetParser(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	for _, what := range own {
		findings = append(findings, migratorTractFinding{Rel: rel, What: what})
	}
	return sortedFindingTexts(findings)
}

const relProbe = "services/svc/internal/apps/migrator/runner.go"

// TestSharedTractInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат
// ОБА гейта. Без него молчание существующего контроля в прогоне 2 неотличимо от
// молчания мёртвого (testing.md §«Гейт на класс», п. 2в).
func TestSharedTractInjectionRunOne_Control(t *testing.T) {
	if got := auditSource(t, relProbe, srcTractConverged); len(got) != 0 {
		t.Errorf("новый гейт краснеет на сведённой обёртке: %v", got)
	}
	form := classifyForProbe(t, srcDelegating)
	if !form.Recognised() {
		t.Errorf("существующий гейт краснеет на законной форме — контроль недействителен")
	}
}

// TestSharedTractInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ
// свойство, старое цело. Краснеет только новый гейт.
func TestSharedTractInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	t.Run("свой текст отказа", func(t *testing.T) {
		got := auditSource(t, relProbe, srcTractRedeclaresRefusal)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
		}
		for _, want := range []string{relProbe, "dsn is empty", "pkg/migratorcli/"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("находка не называет %q: %s", want, got[0])
			}
		}
	})

	t.Run("свой разбор цели", func(t *testing.T) {
		got := auditSource(t, relProbe, srcTractOwnTargetParser)
		if len(got) != 2 {
			t.Fatalf("находок %d, ожидалось две (объявление и форматный разбор): %v", len(got), got)
		}
		joined := strings.Join(got, "\n")
		for _, want := range []string{"parseTargetVersion", "fmt.Sscanf", "12abc"} {
			if !strings.Contains(joined, want) {
				t.Errorf("находки не называют %q: %s", want, joined)
			}
		}
	})

	// Существующий гейт на ОБЕИХ инъекциях выше молчать не обязан — он их не
	// видит вовсе (судит импорты точки наката). Утверждается именно это: моя
	// инъекция не трогает его предмет.
	if form := classifyForProbe(t, srcDelegating); !form.Recognised() {
		t.Errorf("существующий гейт покраснел от чужой инъекции — доказательство недействительно")
	}
}

// TestSharedTractInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (третья форма наката). Краснеет только соседний гейт,
// новый молчит.
func TestSharedTractInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	form := classifyForProbe(t, srcTractThirdForm)
	if form.Recognised() {
		t.Fatalf("существующий гейт не увидел третьей формы — он мёртв, и прогон 2 ничего не доказывал")
	}
	if got := auditSource(t, "services/svc/cmd/migrator/main.go", srcTractThirdForm); len(got) != 0 {
		t.Errorf("новый гейт краснеет на чужом предмете: %v", got)
	}
}

// TestSharedTractGateIsSilentOnLegalTwins — гейт СПОСОБЕН смолчать. Без этого он
// ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
func TestSharedTractGateIsSilentOnLegalTwins(t *testing.T) {
	t.Run("текст отказа только в прозе", func(t *testing.T) {
		if got := auditSource(t, relProbe, srcTractRefusalOnlyInProse); len(got) != 0 {
			t.Errorf("гейт краснеет на комментарии, объясняющем его же запрет: %v", got)
		}
	})

	t.Run("вызов общего разбора не есть своё объявление", func(t *testing.T) {
		// Самый важный близнец: ровно эта строка стоит теперь в КАЖДОЙ сведённой
		// обёртке. Гейт, считающий её нарушением, краснел бы на верном коде.
		src := `package migrator

import "github.com/PRO-Robotech/kacho/pkg/migratorcli"

func f(s string) { _, _ = migratorcli.ParseTargetVersion(s) }`
		if got := auditSource(t, relProbe, src); len(got) != 0 {
			t.Errorf("вызов общего разбора засчитан как своё объявление: %v", got)
		}
	})

	t.Run("общий пакет объявлять вправе", func(t *testing.T) {
		if !migratorTractIsShared("pkg/migratorcli/preconditions.go") {
			t.Error("дом общей половины не опознан — гейт судил бы её как нарушителя")
		}
		if migratorTractIsShared("services/vpc/internal/apps/migrator/runner.go") {
			t.Error("файл сервиса принят за общий пакет — отрицание стало бы вакуумным")
		}
	})

	t.Run("корпус — только тракт наката", func(t *testing.T) {
		for _, rel := range []string{
			"services/vpc/cmd/migrator/main.go",
			"services/vpc/internal/apps/migrator/runner.go",
		} {
			if !migratorTractIsEntryPoint(rel) {
				t.Errorf("%s не попал в корпус — гейт его не читает", rel)
			}
		}
		if migratorTractIsEntryPoint("services/vpc/internal/repo/kacho/pg/network_repo.go") {
			t.Error("посторонний файл попал в корпус тракта")
		}
	})
}
