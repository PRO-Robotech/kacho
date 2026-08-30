// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordbopen_injection_test.go — доказательство, что гейт открытия базы
// СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Инъекция настоящая: каждый случай списан с живого исходника ДО сведения
// (#1383) — прямая четвёрка открывала базу в main.go, делегирующая тройка
// объявляла openPgxDB/setupGoose у себя. Ничего не выдумано.
//
// Прогонов ТРИ, а не два (testing.md §«Гейт на класс», п. 2в). Инъекция вида
// «завести ещё один файл» нарушала бы сразу всё, что требуется от файлов тракта,
// и красное приходило бы от соседа. Поэтому здесь снимается НОВОЕ свойство у
// элемента, чьё СТАРОЕ на месте, и наоборот; третий прогон нужен затем, что без
// него молчание существующего контроля неотличимо от молчания мёртвого.
package repohygiene

import (
	"strings"
	"testing"
)

const (
	// srcDBOpenConverged — сведённая точка наката, как она выглядит после
	// #1383: зовёт общий шаг и НЕ объявляет ничего своего.
	srcDBOpenConverged = `package main

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func run(ctx context.Context, dsn string, fsys interface{}) error {
	if err := migratorcli.SetupGoose(nil, migratorcli.SpecPostgres); err != nil {
		return err
	}
	_, err := migratorcli.OpenDB(ctx, dsn, migratorcli.SpecPostgres)
	return err
}`

	// srcDBOpenInline — то, что было у прямой четвёрки: открытие базы, барьер
	// готовности и настройка goose прямо в main.go, со своими текстами отказа.
	srcDBOpenInline = `package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	opts, _ := migratorcli.Parse("kacho-migrator", nil)
	goose.SetBaseFS(nil)
	if err := goose.SetDialect(opts.Dialect); err != nil {
		panic(fmt.Errorf("goose dialect: %w", err))
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	if err := dbready.Wait(context.Background(), db, dbready.Options{}); err != nil {
		panic(fmt.Errorf("database connection check failed: %w", err))
	}
}`

	// srcDBOpenOwnSpec — то, что было у делегирующей тройки: свои метаданные
	// диалекта рядом со своим открытием базы.
	srcDBOpenOwnSpec = `package migrator

type DialectSpec struct {
	Name         string
	GooseDialect string
	SQLDriver    string
}

var SpecPostgres = DialectSpec{Name: "postgres", GooseDialect: "postgres", SQLDriver: "pgx"}`

	// srcDBOpenProseOnly — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: и sql.Open, и
	// dbready.Wait, и текст "open db" стоят в КОММЕНТАРИИ, объясняющем сам
	// запрет. Ровно так они стоят в шапке migratordbopen.go, поэтому гейт по
	// подстроке краснел бы на собственном объяснении.
	srcDBOpenProseOnly = `package migrator

// Своего открытия базы здесь НЕТ: sql.Open ленив, поэтому барьер готовности
// (dbready.Wait) живёт в общем шаге вместе с текстом "open db (driver=%s)" и с
// "database connection check failed". Настройка goose (goose.SetDialect) — там же.
import "github.com/PRO-Robotech/kacho/pkg/migratorcli"

func f() { _, _ = migratorcli.OpenDB(nil, "", migratorcli.SpecPostgres) }`

	// srcDBOpenAppliesChain — ЗАКОННЫЙ БЛИЗНЕЦ, отделяющий предмет от соседнего:
	// накат цепочки ОСТАЁТСЯ в services/ и сведения ждёт (предусловие названо в
	// docs/architecture/migrator-form.md). Гейт, считающий его нарушением,
	// требовал бы работы, которую сам же и не даёт сделать.
	srcDBOpenAppliesChain = `package migrator

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func up(ctx context.Context, db *sql.DB, dir string) error {
	return goose.UpContext(ctx, db, dir, goose.WithAllowMissing())
}`

	relDBOpenProbe = "services/svc/cmd/migrator/main.go"
)

// auditDBOpenSource — одна инъекция через ТЕ ЖЕ функции, которые зовёт гейт.
// Своя копия разбора доказывала бы что-то о копии, а не о гейте.
func auditDBOpenSource(t *testing.T, rel, src string) []string {
	t.Helper()
	facts, err := readMigratorDBOpenSource(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	return sortedDBOpenFindingTexts(migratorDBOpenFindings(rel, facts))
}

// TestDBOpenInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат ОБА
// гейта. Без него молчание соседа в прогоне 2 неотличимо от молчания мёртвого.
func TestDBOpenInjectionRunOne_Control(t *testing.T) {
	if got := auditDBOpenSource(t, relDBOpenProbe, srcDBOpenConverged); len(got) != 0 {
		t.Errorf("новый гейт краснеет на сведённой точке наката: %v", got)
	}
	if got := auditSource(t, relDBOpenProbe, srcDBOpenConverged); len(got) != 0 {
		t.Errorf("соседний гейт краснеет на сведённой точке наката — контроль недействителен: %v", got)
	}
}

// TestDBOpenInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ свойство
// (шаг открытия базы объявлен на месте), СТАРОЕ цело — предусловия и разбор
// цели по-прежнему общие. Краснеет только новый гейт.
func TestDBOpenInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	t.Run("своё открытие базы в точке наката", func(t *testing.T) {
		got := auditDBOpenSource(t, relDBOpenProbe, srcDBOpenInline)
		if len(got) != 5 {
			t.Fatalf("находок %d, ожидалось пять (открытие, барьер, goose и два текста): %v",
				len(got), got)
		}
		joined := strings.Join(got, "\n")
		for _, want := range []string{
			relDBOpenProbe, "sql.Open", "dbready.Wait", "goose.SetBaseFS/SetDialect",
			`"open db"`, "pkg/migratorcli/", "migrator-form.md",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("находки не называют %q:\n%s", want, joined)
			}
		}
	})

	t.Run("свои метаданные диалекта", func(t *testing.T) {
		got := auditDBOpenSource(t, "services/svc/internal/apps/migrator/dialect.go", srcDBOpenOwnSpec)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
		}
		if !strings.Contains(got[0], "DialectSpec/SpecPostgres") {
			t.Errorf("находка не называет предмет: %s", got[0])
		}
	})

	// Соседний гейт обе инъекции выше НЕ видит вовсе: его предмет — тексты
	// предусловий и разбор цели, а их эти исходники не трогают. Утверждается
	// именно это: моя инъекция не роняет чужого.
	if got := auditSource(t, relDBOpenProbe, srcDBOpenInline); len(got) != 0 {
		t.Errorf("соседний гейт покраснел от чужой инъекции — доказательство недействительно: %v", got)
	}
}

// TestDBOpenInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (свой текст отказа предусловий), НОВОЕ цело — открытие
// базы делегировано. Краснеет только сосед, новый гейт молчит.
func TestDBOpenInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	const src = `package migrator

import (
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func (c Config) Validate() error {
	if c.DSN == "" {
		return errors.New("dsn is empty (set --dsn or KACHO_MIGRATOR_DSN)")
	}
	return nil
}

func open(dsn string) error {
	_, err := migratorcli.OpenDB(nil, dsn, migratorcli.SpecPostgres)
	return err
}`

	got := auditSource(t, "services/svc/internal/apps/migrator/runner.go", src)
	if len(got) == 0 {
		t.Fatal("соседний гейт не увидел своего дефекта — он мёртв, и прогон 2 ничего не доказывал")
	}
	if mine := auditDBOpenSource(t, "services/svc/internal/apps/migrator/runner.go", src); len(mine) != 0 {
		t.Errorf("новый гейт краснеет на чужом предмете: %v", mine)
	}
}

// TestDBOpenGateIsSilentOnLegalTwins — гейт СПОСОБЕН смолчать. Без этого он
// ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
func TestDBOpenGateIsSilentOnLegalTwins(t *testing.T) {
	t.Run("шаг назван только в прозе", func(t *testing.T) {
		if got := auditDBOpenSource(t, relDBOpenProbe, srcDBOpenProseOnly); len(got) != 0 {
			t.Errorf("гейт краснеет на комментарии, объясняющем его же запрет: %v", got)
		}
	})

	t.Run("накат цепочки — НЕ предмет этого гейта", func(t *testing.T) {
		// Самый важный близнец: goose.Up* ОСТАЁТСЯ в services/ и ждёт своих проб.
		// Гейт, считающий его нарушением, требовал бы работы, которую сам же
		// объявляет отложенной, и краснел бы на верном коде.
		if got := auditDBOpenSource(t, relDBOpenProbe, srcDBOpenAppliesChain); len(got) != 0 {
			t.Errorf("гейт принял накат цепочки за настройку goose: %v", got)
		}
	})

	t.Run("общий пакет объявлять вправе", func(t *testing.T) {
		if !migratorTractIsShared("pkg/migratorcli/dialect.go") {
			t.Error("дом общего шага не опознан — гейт судил бы его как нарушителя")
		}
		if migratorTractIsShared("services/vpc/internal/apps/migrator/postgres.go") {
			t.Error("файл сервиса принят за общий пакет — отрицание стало бы вакуумным")
		}
	})
}
