// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Тесты CLI миграций: покрывают только парсинг cobra-флагов и резолвинг
// диалекта/DSN. Реальный apply миграций — в integration-suite
// `internal/repo/...integration_test.go` (testcontainers Postgres + goose.Up).
// Здесь БД не открывается: тесты быстрые и не зависят от docker.
package main

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func emptyFS() fs.FS { return fstest.MapFS{} }

// runCommand — helper: парсит args, ловит ошибки cobra. Stdout/stderr
// захватывается для последующих assert'ов в тестах конкретных subcommand'ов.
func runCommand(t *testing.T, args []string, env map[string]string) (stdout, stderr string, err error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	cmd := newRootCmd(emptyFS())
	var sout, serr bytes.Buffer
	cmd.SetOut(&sout)
	cmd.SetErr(&serr)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return sout.String(), serr.String(), err
}

func TestRootCmd_HelpDoesNotError(t *testing.T) {
	// `--help` отрабатывает чисто и печатает Use-строку.
	stdout, _, err := runCommand(t, []string{"--help"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "kacho-migrator") {
		t.Fatalf("expected help output to mention kacho-migrator, got: %q", stdout)
	}
	// Перечень утверждается ПО БЛОКУ «Available Commands», а не подстрокой во
	// всём выводе: пока проба искала слово «create» где угодно, её удовлетворяла
	// ПРОЗА в Long («и создание новой миграции (create)») — то есть после снятия
	// глагола она осталась бы зелёной, а помощь продолжала бы обещать команду,
	// которой нет.
	block := stdout
	if i := strings.Index(block, "Available Commands:"); i >= 0 {
		block = block[i:]
		if j := strings.Index(block, "\nFlags:"); j >= 0 {
			block = block[:j]
		}
	} else {
		t.Fatalf("в помощи нет блока перечня команд: %q", stdout)
	}
	for _, sub := range []string{"up", "down", "status"} {
		if !strings.Contains(block, sub) {
			t.Errorf("в перечне команд нет %q: %q", sub, block)
		}
	}
	// Глагол create снят (#566): имя миграции пишется рукой. Обещать его в
	// помощи нельзя — обещание не исполнимо.
	if strings.Contains(block, "create") {
		t.Errorf("в перечне команд снова есть create: %q", block)
	}
}

func TestUpCmd_ParsesTargetFlag(t *testing.T) {
	// Парсинг `up --target 10` не должен падать на flag-уровне; ошибка
	// допустима только из-за пустого DSN (наш namespace при unset env).
	// Проверка проще: запустить с явно невалидным dialect — cobra-парсер
	// корректно дойдет до RunE, а там NewDialect отдаст ошибку.
	_, _, err := runCommand(t, []string{
		"--dialect", "bogus-dialect",
		"--dsn", "postgres://x:y@z:1/d?sslmode=disable",
		"up", "--target", "10",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown dialect, got nil")
	}
	if !strings.Contains(err.Error(), "unknown dialect") {
		t.Fatalf("expected 'unknown dialect' error, got: %v", err)
	}
}

func TestDownCmd_ParsesTargetFlag(t *testing.T) {
	_, _, err := runCommand(t, []string{
		"--dialect", "bogus",
		"--dsn", "postgres://x:y@z:1/d?sslmode=disable",
		"down", "--target", "5",
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown dialect") {
		t.Fatalf("got unexpected error: %v", err)
	}
}

func TestCreateVerbIsGone(t *testing.T) {
	// Замок на снятие (#566). Глагол выдавал имя с меткой времени — форму,
	// которой дерево не принимает: гейт пространства номеров отвергает номер
	// больше номера всякой возможной задачи. Вернуть его молча нельзя.
	_, _, err := runCommand(t, []string{
		"--dsn", "postgres://x:y@z:1/d?sslmode=disable",
		"create", "probe_name",
	}, nil)
	if err == nil {
		t.Fatal("глагол create снова принимается — имя миграции пишется рукой, см. " +
			"docs/architecture/migration-version-namespace.md")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("ожидался отказ cobra «unknown command», получено: %v", err)
	}
}

func TestBuildRunner_DSNFromFlag(t *testing.T) {
	// Прямой unit-тест на buildRunner: --dsn явно задан → используется без
	// обращения к ENV / envconfig.
	opts := &rootOptions{dialect: "postgres", dsn: "postgres://u:p@h:5432/db?sslmode=disable"}
	r, err := buildRunner(opts, emptyFS())
	if err != nil {
		t.Fatalf("buildRunner failed: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestBuildRunner_DSNFromEnv(t *testing.T) {
	// --dsn пуст, ENV KACHO_MIGRATOR_DSN — выставлен → берется из ENV.
	t.Setenv(envDSN, "postgres://envuser:envpw@envhost:5432/envdb?sslmode=disable")
	opts := &rootOptions{dialect: "postgres"}
	r, err := buildRunner(opts, emptyFS())
	if err != nil {
		t.Fatalf("buildRunner failed: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestBuildRunner_UnknownDialect(t *testing.T) {
	opts := &rootOptions{dialect: "nosuch", dsn: "postgres://x"}
	_, err := buildRunner(opts, emptyFS())
	if err == nil || !strings.Contains(err.Error(), "unknown dialect") {
		t.Fatalf("expected unknown dialect error, got: %v", err)
	}
}

func TestBuildRunner_DSNFallbackToConfig(t *testing.T) {
	// --dsn пуст, ENV KACHO_MIGRATOR_DSN пуст, но KACHO_VPC_DB_PASSWORD
	// задан → buildRunner должен дойти до config.Load и собрать DSN из
	// envconfig (cfg.MigrateDSN()). Никаких ошибок не ожидается.
	t.Setenv("KACHO_VPC_DB_PASSWORD", "fallback-password")
	t.Setenv("KACHO_VPC_DB_HOST", "fallback-host")
	// envDSN явно НЕ выставляем — пусть будет тот, что в shell (обычно пуст).
	// Если шелл выставит KACHO_MIGRATOR_DSN — этот тест становится no-op
	// (берется ENV-DSN), что не ломает контракт fallback.
	opts := &rootOptions{dialect: "postgres"}
	r, err := buildRunner(opts, emptyFS())
	if err != nil {
		t.Fatalf("expected fallback to config to succeed, got: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

// TestExtraPositionalArgumentIsRefused — задача #1461: лишний позиционный
// аргумент обязан получить ответ, а не молчаливый накат.
//
// Cobra по умолчанию (`Args == nil`) принимает произвольные позиционные
// аргументы. Прогон настоящего дерева команд показал это дословно: `up 800001`
// — догадка оператора о том, как задать цель — проходил разбор молча и уезжал
// накатывать ДО ГОЛОВЫ на базе из конфигурации. Прямая четвёрка теперь такой
// аргумент называет; делегирующая тройка обязана отвечать так же, иначе
// «один результат у всех семи» не выполняется.
//
// `--dialect bogus` в аргументах — не украшение: он делает пробу быстрой и
// разом утверждает ПОРЯДОК. До правки отказ приходил от диалекта (или, без
// него, от двухминутного барьера готовности БД); после — от разбора
// аргументов, который стоит РАНЬШЕ любого исполнения.
func TestExtraPositionalArgumentIsRefused(t *testing.T) {
	for _, sub := range []string{"up", "down", "status"} {
		_, _, err := runCommand(t, []string{"--dialect", "bogus", sub, "800001"}, nil)
		if err == nil {
			t.Fatalf("%s 800001: лишний аргумент принят молча", sub)
		}
		if !strings.Contains(err.Error(), "800001") {
			t.Errorf("%s 800001: отказ не называет лишний аргумент: %v", sub, err)
		}
	}
}

// TestLegitimateFlagsSurviveTheArgumentCheck — положительный контроль к пробе
// выше. Без него она зеленела бы на дереве команд, отвергающем вообще всё:
// законный вызов обязан дойти до исполнения, и отказ прийти от диалекта.
func TestLegitimateFlagsSurviveTheArgumentCheck(t *testing.T) {
	for _, args := range [][]string{
		{"--dialect", "bogus", "up"},
		{"up", "--dialect", "bogus"},
		{"up", "--dialect", "bogus", "--target", "800001"},
	} {
		_, _, err := runCommand(t, args, nil)
		if err == nil {
			t.Fatalf("%v: ожидался отказ по диалекту, получено nil", args)
		}
		if strings.Contains(err.Error(), "800001") && !strings.Contains(err.Error(), "dialect") {
			t.Errorf("%v: законный вызов отвергнут разбором аргументов: %v", args, err)
		}
		if !strings.Contains(err.Error(), "dialect") {
			t.Errorf("%v: отказ пришёл не от диалекта, значит проба утверждает не то: %v", args, err)
		}
	}
}

// TestEmptyCommandLineIsRefused — пустая командная строка обязана быть ОТКАЗОМ,
// а не успехом (#1461).
//
// Cobra при корне без исполнения печатает помощь и выходит УСПЕХОМ. Опыт по
// собранным бинарям: `vpc-migrator; echo $?` давал 0 у трёх делегирующих
// сервисов и 1 у четырёх прямых. Различие не решал никто, и цена у него не
// косметическая: init-контейнер или скрипт, потерявший аргумент, на трёх
// сервисах из семи объявляется УСПЕШНЫМ — то есть «миграции накатаны» там, где
// не выполнено ничего.
//
// Выбран отказ, а не помощь-успех: успех на невыполненной работе есть тот же
// класс, ради которого задача заведена.
func TestEmptyCommandLineIsRefused(t *testing.T) {
	_, _, err := runCommand(t, nil, nil)
	if err == nil {
		t.Fatal("пустая командная строка принята УСПЕХОМ: скрипт, потерявший аргумент, " +
			"объявляется выполнившим накат")
	}
	if !strings.Contains(err.Error(), "no command given") {
		t.Fatalf("отказ не называет свой предмет: %v", err)
	}
}

// TestHelpIsStillASuccess — положительный контроль к пробе выше. Без него она
// зеленела бы на дереве команд, объявляющем отказом и явный запрос помощи.
func TestHelpIsStillASuccess(t *testing.T) {
	if _, _, err := runCommand(t, []string{"--help"}, nil); err != nil {
		t.Fatalf("явный запрос помощи объявлен отказом: %v", err)
	}
}

// TestUnknownCommandIsStillNamed — второй положительный контроль: решение о
// пустой строке не должно проглотить отказ по неизвестной подкоманде.
func TestUnknownCommandIsStillNamed(t *testing.T) {
	_, _, err := runCommand(t, []string{"upp"}, nil)
	if err == nil {
		t.Fatal("неизвестная подкоманда принята")
	}
	if !strings.Contains(err.Error(), "upp") {
		t.Fatalf("отказ не называет подкоманду: %v", err)
	}
}

// TestRefusalTextsComeFromTheSharedProducer — форма отказа одна на семь (#1461).
//
// Опыт по собранным бинарям на одинаковом входе показал две редакции одного
// отказа: делегирующая форма говорила «unknown command …» там, где прямая
// говорила «unexpected argument … ; a version is given as --target …», и не
// называла перечень известных подкоманд. Оператор читает эти строки глазами, а
// скрипт — образцом: образец, написанный по одному сервису, на соседнем не
// срабатывал.
//
// Утверждается РАВЕНСТВО наблюдаемого текста тому, что производит общий пакет,
// а не совпадение с литералом: литерал был бы второй редакцией того же текста и
// разошёлся бы с первой молча.
func TestRefusalTextsComeFromTheSharedProducer(t *testing.T) {
	const binary = "kacho-migrator"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"upp"}, migratorcli.UnknownCommandError(binary, "upp").Error()},
		{[]string{"up", "800001"}, migratorcli.UnexpectedArgumentError(binary+" up", "800001").Error()},
		{[]string{"down", "800001"}, migratorcli.UnexpectedArgumentError(binary+" down", "800001").Error()},
		{[]string{"status", "800001"}, migratorcli.UnexpectedArgumentError(binary+" status", "800001").Error()},
		// Эту редакцию производит библиотека разбора; общий пакет её ЗАИМСТВУЕТ.
		// Проба ловит расхождение с обеих сторон: и смену формулировки в
		// библиотеке, и отход общего пакета от неё.
		{[]string{"--nosuchflag", "up"}, migratorcli.UnknownFlagError("nosuchflag").Error()},
		{[]string{"up", "--nosuchflag"}, migratorcli.UnknownFlagError("nosuchflag").Error()},
		{[]string{"status", "--target", "1"}, migratorcli.UnknownFlagError("target").Error()},
		{nil, migratorcli.ErrNoCommand.Error()},
	} {
		_, _, err := runCommand(t, tc.args, nil)
		if err == nil {
			t.Fatalf("%v: принято молча", tc.args)
		}
		if err.Error() != tc.want {
			t.Errorf("%v: своя редакция отказа:\n  получено: %q\n  общая:    %q",
				tc.args, err.Error(), tc.want)
		}
	}
}

// TestAvailableCommandsAreTheOnesSevenShare — перечень команд один на семь.
//
// Cobra доводила к дереву `completion` и `help`; прямая форма их не имела,
// поэтому помощь у трёх сервисов из семи предлагала команды, которых на
// остальных четырёх нет. Перечень читает оператор — значит он тоже поверхность.
//
// Равенство достигнуто с разных сторон, и это решение: `completion` снят (у
// прямой формы дерева команд нет и дополнений оболочки не будет), а `help`
// оставлен и понимается прямой формой — снять его из перечня cobra можно лишь
// зарегистрировав скрытую команду под ЧУЖИМ именем, то есть разменяв одну
// асимметрию на худшую.
func TestAvailableCommandsAreTheOnesSevenShare(t *testing.T) {
	stdout, _, err := runCommand(t, []string{"--help"}, nil)
	if err != nil {
		t.Fatalf("помощь объявлена отказом: %v", err)
	}
	block := stdout
	i := strings.Index(block, "Available Commands:")
	if i < 0 {
		t.Fatalf("в помощи нет блока перечня команд: %q", stdout)
	}
	block = block[i:]
	if j := strings.Index(block, "\nFlags:"); j >= 0 {
		block = block[:j]
	}
	for _, want := range []string{"up", "down", "status"} {
		if !strings.Contains(block, want) {
			t.Errorf("в перечне нет %q: %q", want, block)
		}
	}
	if strings.Contains(block, "completion") {
		t.Errorf("в перечне есть completion, которого нет у прямой формы: %q", block)
	}
	if !strings.Contains(block, "help") {
		t.Errorf("help пропал из перечня, хотя равенство по нему достигнуто иначе: %q", block)
	}
}

// TestHelpSubcommandWorks — `help` работает подкомандой у всех семи: у cobra он
// свой, у прямой формы разбирается общим пакетом. Положительный контроль к
// решению выше — без него «help остался в перечне» ничего бы не значило.
func TestHelpSubcommandWorks(t *testing.T) {
	stdout, _, err := runCommand(t, []string{"help"}, nil)
	if err != nil {
		t.Fatalf("help объявлен отказом: %v", err)
	}
	if !strings.Contains(stdout, "kacho-migrator") {
		t.Fatalf("help не напечатал форму вызова: %q", stdout)
	}
}
