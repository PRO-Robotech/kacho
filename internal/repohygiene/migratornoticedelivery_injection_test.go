// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratornoticedelivery_injection_test.go — доказательство, что гейт доставки
// уведомлений СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Инъекция настоящая: случай «открыл и не провязал» списан с живого исходника ДО
// правки (#2544) — общий шаг открывал соединение простым `sql.Open`, и ни одно
// уведомление сервера не доезжало до оператора. Ничего не выдумано.
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
	// srcNoticeWired — общий шаг после правки: открывает соединение и задаёт
	// обработчик до открытия.
	srcNoticeWired = `package migratorcli

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type NoticeRelay struct{}

func NewNoticeRelay(service string, out interface{}) *NoticeRelay { return &NoticeRelay{} }

func OpenDB(ctx context.Context, dsn string, spec DialectSpec, notices *NoticeRelay) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.OnNotice = notices.Handler()
	return stdlib.OpenDB(*cfg), nil
}`

	// srcNoticeUnwired — то, что было ДО правки: соединение открывается, а
	// обработчик не задаётся ни здесь, ни где-либо ещё в каталоге.
	srcNoticeUnwired = `package migratorcli

import (
	"context"
	"database/sql"
)

func OpenDB(ctx context.Context, dsn string, spec DialectSpec) (*sql.DB, error) {
	return sql.Open(spec.SQLDriver, dsn)
}`

	// srcNoticeDiscarded — тихий возврат того же дефекта ОДНОЙ строкой: приёмник
	// есть, компилируется, выглядит настроенным и смотрит в никуда.
	srcNoticeDiscarded = `package migratorrun

import (
	"io"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func (r *Runner) relay() *migratorcli.NoticeRelay {
	return migratorcli.NewNoticeRelay(r.cfg.Service, io.Discard)
}`

	// srcNoticeProseOnly — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: и OnNotice, и
	// NoticeRelay, и io.Discard стоят в КОММЕНТАРИИ, объясняющем сам запрет.
	// Ровно так они стоят в шапке migratornoticedelivery.go, поэтому гейт по
	// подстроке краснел бы на собственном объяснении.
	srcNoticeProseOnly = `package migratorrun

// Своего открытия соединения здесь НЕТ, и приёмника уведомлений тоже: обработчик
// OnNotice задаёт общий шаг, а NoticeRelay собирается там же. Направить его в
// io.Discard значило бы вернуть дефект целиком — sql.Open без обработчика.
type Runner struct{ cfg Config }`

	// srcNoticeSplitDir — ЗАКОННЫЙ БЛИЗНЕЦ: открывает один файл пакета, а
	// обработчик задаёт СОСЕД по каталогу. Гейт, судящий пофайлово, краснел бы
	// на законном расщеплении.
	srcNoticeSplitDir = `package migratorcli

import (
	"database/sql"

	"github.com/jackc/pgx/v5/stdlib"
)

func open(cfg interface{}) *sql.DB { return stdlib.OpenDB(cfg) }`

	// srcNoticeStdout — ЗАКОННЫЙ БЛИЗНЕЦ: второй допустимый поток процесса.
	srcNoticeStdout = `package migratorrun

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func relay(service string) *migratorcli.NoticeRelay {
	return migratorcli.NewNoticeRelay(service, os.Stdout)
}`

	// srcNoticeConvergedPoint — сведённая точка наката после правки: зовёт общий
	// шаг, приёмник строит на потоке процесса, своего открытия не заводит. На ней
	// обязаны молчать ОБА гейта, и она — единственный контроль, где это можно
	// спросить у обоих: дом общего шага сосед не судит вовсе.
	srcNoticeConvergedPoint = `package main

import (
	"context"
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func run(ctx context.Context, dsn string, spec migratorcli.DialectSpec) error {
	_, err := migratorcli.OpenDB(ctx, dsn, spec, migratorcli.NewNoticeRelay("svc", os.Stderr))
	return err
}`

	relNoticeHome  = "pkg/migratorcli/dialect.go"
	relNoticeRun   = "pkg/migratorrun/runner.go"
	relNoticeTract = "services/svc/cmd/migrator/main.go"
)

// auditNoticeSource — одна инъекция через ТЕ ЖЕ функции, которые зовёт гейт.
// Своя копия разбора доказывала бы что-то о копии, а не о гейте.
func auditNoticeSource(t *testing.T, rel, src string, wiredInDir bool) []string {
	t.Helper()
	facts, err := readMigratorNoticeSource(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	return sortedNoticeFindingTexts(migratorNoticeJudge(rel, facts, wiredInDir))
}

// TestNoticeInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат ОБА
// гейта. Без него молчание соседа в прогоне 2 неотличимо от молчания мёртвого.
func TestNoticeInjectionRunOne_Control(t *testing.T) {
	t.Parallel()

	// Общий шаг после правки: открывает и провязывает. Спрашивается только новый
	// гейт — соседа об этом файле спрашивать нельзя, он его обходом исключает
	// (предикат исключения проверяется в прогоне 2).
	if got := auditNoticeSource(t, relNoticeHome, srcNoticeWired, true); len(got) != 0 {
		t.Errorf("новый гейт краснеет на провязанном общем шаге: %v", got)
	}

	// Сведённая точка наката — единственное место, где контроль можно спросить у
	// ОБОИХ: она лежит в корпусе соседа и в корпусе нового гейта сразу.
	if got := auditNoticeSource(t, relNoticeTract, srcNoticeConvergedPoint, false); len(got) != 0 {
		t.Errorf("новый гейт краснеет на сведённой точке наката: %v", got)
	}
	if got := auditDBOpenSource(t, relNoticeTract, srcNoticeConvergedPoint); len(got) != 0 {
		t.Errorf("соседний гейт краснеет на сведённой точке наката — контроль недействителен: %v", got)
	}
}

// TestNoticeInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ свойство
// (соединение открывается без обработчика), СТАРОЕ цело — шаг по-прежнему
// объявлен в общем пакете и своих текстов отказа не заводит. Краснеет только
// новый гейт.
func TestNoticeInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	t.Parallel()

	t.Run("открыл и не провязал", func(t *testing.T) {
		got := auditNoticeSource(t, relNoticeHome, srcNoticeUnwired, false)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
		}
		for _, want := range []string{relNoticeHome, "sql.Open", "OnNotice", "NewNoticeRelay"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("находка не называет %q: %s", want, got[0])
			}
		}
	})

	t.Run("приёмник смотрит в никуда", func(t *testing.T) {
		got := auditNoticeSource(t, relNoticeRun, srcNoticeDiscarded, true)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
		}
		for _, want := range []string{relNoticeRun, "io.Discard", "os.Stderr"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("находка не называет %q: %s", want, got[0])
			}
		}
	})

	// СОСЕДНИЙ ГЕЙТ ОБЕ ИНЪЕКЦИИ ВЫШЕ НЕ СУДИТ ВОВСЕ, и утверждается именно
	// ПРЕДИКАТ этого, а не его следствие.
	//
	// Прогнать разбор соседа мимо его обхода — значит спросить его о файле,
	// которого он не смотрит: он ответит находкой, и «покраснел» будет означать
	// лишь «я позвал не то». Так и вышло на первой редакции этой строки.
	// Проверяется поэтому сам механизм исключения: дом общего шага сосед
	// пропускает (объявлять там — законно), а pkg/migratorrun вне его корпуса.
	if !migratorTractIsShared(relNoticeHome) {
		t.Errorf("%s не опознан как дом общего шага — сосед судил бы его наравне с "+
			"точками наката, и обе инъекции выше роняли бы оба гейта", relNoticeHome)
	}
	if migratorTractIsShared(relNoticeRun) || migratorTractIsEntryPoint(relNoticeRun) {
		t.Errorf("%s попал в корпус соседа — инъекция про назначение приёмника роняла бы "+
			"и его, и доказательство стало бы недействительным", relNoticeRun)
	}
}

// TestNoticeInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (точка наката открывает базу сама и заводит свой текст
// отказа), НОВОЕ цело — доставка уведомлений её не касается. Краснеет только
// сосед, новый гейт молчит.
func TestNoticeInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
)

func main() {
	db, err := sql.Open("pgx", "")
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	if err := dbready.Wait(context.Background(), db, dbready.Options{}); err != nil {
		panic(err)
	}
}`

	if got := auditDBOpenSource(t, relNoticeTract, src); len(got) == 0 {
		t.Fatal("соседний гейт не увидел своего дефекта — он мёртв, и прогон 2 ничего не доказывал")
	}
	if mine := auditNoticeSource(t, relNoticeTract, src, false); len(mine) != 0 {
		t.Errorf("новый гейт краснеет на чужом предмете: %v", mine)
	}
}

// TestNoticeGateIsSilentOnLegalTwins — гейт СПОСОБЕН смолчать. Без этого он
// ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
func TestNoticeGateIsSilentOnLegalTwins(t *testing.T) {
	t.Parallel()

	t.Run("предмет назван только в прозе", func(t *testing.T) {
		if got := auditNoticeSource(t, relNoticeRun, srcNoticeProseOnly, true); len(got) != 0 {
			t.Errorf("гейт краснеет на комментарии, объясняющем его же запрет: %v", got)
		}
	})

	t.Run("обработчик задаёт сосед по каталогу", func(t *testing.T) {
		if got := auditNoticeSource(t, relNoticeHome, srcNoticeSplitDir, true); len(got) != 0 {
			t.Errorf("гейт судит пофайлово и краснеет на законном расщеплении пакета: %v", got)
		}
		// Пара к близнецу: тот же файл БЕЗ провязки в каталоге обязан краснеть.
		// Без неё «молчит» означало бы «не смотрит».
		if got := auditNoticeSource(t, relNoticeHome, srcNoticeSplitDir, false); len(got) != 1 {
			t.Errorf("без провязки в каталоге находок %d, ожидалась одна: %v", len(got), got)
		}
	})

	t.Run("второй допустимый поток процесса", func(t *testing.T) {
		if got := auditNoticeSource(t, relNoticeRun, srcNoticeStdout, true); len(got) != 0 {
			t.Errorf("гейт признаёт законным только один из двух потоков процесса: %v", got)
		}
	})

	t.Run("общий тракт опознан, дом сервиса — нет", func(t *testing.T) {
		if !migratorNoticeIsHome(relNoticeHome) || !migratorNoticeIsHome(relNoticeRun) {
			t.Error("дом общего шага не опознан — гейт судил бы его как чужой")
		}
		if migratorNoticeIsHome(relNoticeTract) {
			t.Error("точка наката принята за общий тракт — прогон 3 стал бы недействительным")
		}
	})
}

// TestNoticeOpenerVocabularyKnowsEveryLegalForm — распознаватель знает ВСЕ формы
// открытия соединения, а не ту, что попалась.
//
// Форма, о которой он не знает, даёт не красное и не зелёное, а МОЛЧАНИЕ: всё,
// что ею написано, уходит из-под наблюдения (testing.md §«Гейт на класс», п. 7).
func TestNoticeOpenerVocabularyKnowsEveryLegalForm(t *testing.T) {
	t.Parallel()
	for _, opener := range migratorNoticeOpeners {
		src := "package migratorcli\n\nfunc f(a interface{}) { " + opener + "(a) }\n"
		got := auditNoticeSource(t, relNoticeHome, src, false)
		if len(got) != 1 {
			t.Errorf("форма %q не опознана как открытие соединения (находок %d): %v",
				opener, len(got), got)
			continue
		}
		if !strings.Contains(got[0], opener) {
			t.Errorf("находка по форме %q её не называет: %s", opener, got[0])
		}
	}
	// Обратная сторона: похожий, но ЧУЖОЙ вызов открытием не считается — иначе
	// словарь ловил бы форму, а не предмет.
	if got := auditNoticeSource(t, relNoticeHome,
		"package migratorcli\n\nfunc f(a interface{}) { os.Open(a) }\n", false); len(got) != 0 {
		t.Errorf("открытие ФАЙЛА принято за открытие соединения: %v", got)
	}
}
