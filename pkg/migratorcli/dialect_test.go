// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// dialect_test.go — пробы общего шага «открыть базу и настроить goose».
//
// # Что проверяется без БД и где исполняются пробы с БД
//
// Здесь закреплены свойства, которым БД не нужна: метадата диалекта, настройка
// goose, тексты отказа и барьер готовности. Для барьера используется недоступный
// сервер: шаг должен дождаться срока контекста и отказать, не вернув соединение.
//
// Пробы с настоящей БД находятся в noticethreshold_integration_test.go:
// TestTheChainGetsItsNoticeThresholdBack и
// TestTheOperatorsOwnThresholdIsNotOverridden проверяют доставку уведомлений
// при накате с соединением OpenDB и при разных источниках порога.
//
// Пакет ./pkg/migratorcli включён в PG_OUTSIDE_SELECTION_PKGS корневого Makefile.
// Цель test-pg-outside-selection исполняет его без краткого режима; job
// pg-outside-selection в .github/workflows/ci.yaml вызывает эту цель. Это
// отдельный маршрут от узкого отбора test-integration. Наличие маршрута
// описывает способ запуска, а результат конкретного прогона устанавливается
// по его выводу, включая исполнение обеих проб с БД без пропуска.
//
// Отрицания идут В ПАРЕ с положительными: без них они зеленели бы на шаге,
// который отвергает всё.
package migratorcli_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

// TestSpecPostgresNamesTheOneSupportedDialect — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ко всему
// файлу. Без него отрицания ниже зеленели бы на метадате, не называющей ничего.
func TestSpecPostgresNamesTheOneSupportedDialect(t *testing.T) {
	spec := migratorcli.SpecPostgres
	if spec.Name != migratorcli.DialectPostgres {
		t.Errorf("имя диалекта %q разошлось с DialectPostgres %q — две редакции одного "+
			"имени, из которых верна одна", spec.Name, migratorcli.DialectPostgres)
	}
	if spec.GooseDialect != migratorcli.DialectPostgres {
		t.Errorf("goose-имя диалекта %q не совпадает с %q", spec.GooseDialect, migratorcli.DialectPostgres)
	}
	if spec.SQLDriver != "pgx" {
		t.Errorf("драйвер %q: точки наката регистрируют blank-импортом именно pgx", spec.SQLDriver)
	}
}

// TestSetupGooseAcceptsTheSupportedDialect — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отказу
// ниже: на поддерживаемом диалекте шаг обязан проходить.
func TestSetupGooseAcceptsTheSupportedDialect(t *testing.T) {
	if err := migratorcli.SetupGoose(fstest.MapFS{}, migratorcli.SpecPostgres); err != nil {
		t.Fatalf("общая настройка goose отвергла поддерживаемый диалект: %v", err)
	}
}

// TestSetupGooseRefusalNamesTheDialect — отказ обязан назвать ЗНАЧЕНИЕ, а не
// только факт неудачи. Прямая четвёрка печатала "goose dialect: …", и по этой
// строке оператор не мог сказать, чтение это было или установка и какое имя не
// подошло (#1383).
func TestSetupGooseRefusalNamesTheDialect(t *testing.T) {
	err := migratorcli.SetupGoose(fstest.MapFS{}, migratorcli.DialectSpec{
		Name:         "nosuch",
		GooseDialect: "nosuch",
		SQLDriver:    "pgx",
	})
	if err == nil {
		t.Fatal("негодный диалект принят — отказ ниже проверять нечем")
	}
	for _, want := range []string{"goose set dialect", `"nosuch"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
}

// TestOpenDBRefusalNamesTheDriver — то же про открытие: прямая четвёрка
// печатала "open db: …" и не называла драйвера, поэтому опечатка в имени
// драйвера была неотличима от опечатки в DSN.
func TestOpenDBRefusalNamesTheDriver(t *testing.T) {
	_, err := migratorcli.OpenDB(context.Background(), "whatever", migratorcli.DialectSpec{
		Name:         "postgres",
		GooseDialect: "postgres",
		SQLDriver:    "nosuchdriver",
	}, migratorcli.NewNoticeRelay("svc", io.Discard))
	if err == nil {
		t.Fatal("незарегистрированный драйвер принят — отказ проверять нечем")
	}
	if !strings.Contains(err.Error(), "open db (driver=nosuchdriver)") {
		t.Errorf("отказ не называет драйвер: %v", err)
	}
}

// TestOpenDBWaitsForTheServerAndThenRefuses — барьер готовности НАСТОЯЩИЙ: на
// сервере, которого нет, шаг не возвращает годное соединение, а отказывает — и
// отказывает общим текстом.
//
// Живой базы здесь нет намеренно (см. шапку файла), а вот молчаливого пропуска
// нет тем более: sql.Open ЛЕНИВ и на несуществующем сервере ошибки не даёт,
// поэтому шаг без барьера вернул бы *sql.DB, годный на вид. Именно это и
// проверяется — что он его НЕ возвращает.
//
// Срок задаёт контекст, а не бюджет ожидания: бюджет боевой (десятки секунд), и
// проба, ждущая его целиком, была бы платой без предмета.
func TestOpenDBWaitsForTheServerAndThenRefuses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Порт 1 в петле: соединение отвергается сразу и всегда, без ожидания DNS.
	const unreachable = "postgres://kacho:kacho@127.0.0.1:1/kacho?sslmode=disable"

	start := time.Now()
	db, err := migratorcli.OpenDB(ctx, unreachable, migratorcli.SpecPostgres,
		migratorcli.NewNoticeRelay("svc", io.Discard))
	if err == nil {
		_ = db.Close()
		t.Fatal("шаг вернул годное соединение к серверу, которого нет — барьера готовности " +
			"нет вовсе, и гонка init-контейнера с подом Postgres вернулась бы")
	}
	if db != nil {
		t.Error("на отказе возвращено ненулевое соединение — закрывать его будет некому")
	}
	if !strings.Contains(err.Error(), "database connection check failed") {
		t.Errorf("отказ не тем текстом (он один на семь точек наката): %v", err)
	}
	// Контроль в другую сторону: если бы барьера не было, отказ пришёл бы
	// мгновенно и не от ожидания. Здесь он приходит по сроку контекста.
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("отказ пришёл за %s — ожидания не было, барьер не исполнялся", elapsed)
	}
}
