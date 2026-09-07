// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cmd/migrator/main_test.go —; покрывает только парсинг cobra-флагов
// + резолвинг диалекта/DSN. Реальный apply миграций — integration-тесты в
// internal/repo/...integration_test.go (testcontainers Postgres + goose),
// которые появятся в. Тут — быстрые unit-тесты без БД и без Docker.
package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
)

func emptyFS() fs.FS { return fstest.MapFS{} }

// runCommand — helper: парсит args, ловит ошибки cobra. Stdout/stderr
// захватывается для проверок.
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
	stdout, _, err := runCommand(t, []string{"--help"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Имя одно на семь сервисов (#1461). Прежняя редакция этой пробы ЗАКРЕПЛЯЛА
	// расхождение: она требовала, чтобы помощь называла `kacho-nlb-migrator`, —
	// то есть удерживала ровно то различие, которое задача снимает.
	if !strings.Contains(stdout, "kacho-migrator") {
		t.Fatalf("помощь не называет общее имя kacho-migrator: %q", stdout)
	}
	if strings.Contains(stdout, "kacho-nlb-migrator") {
		t.Fatalf("помощь снова называет своё имя вместо общего: %q", stdout)
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

func TestUpCmd_UnknownDialectFails(t *testing.T) {
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
	opts := &rootOptions{dialect: "postgres", dsn: "postgres://u:p@h:5432/db?sslmode=disable"}
	r, err := buildRunner(opts, fstest.MapFS{"0001_x.sql": &fstest.MapFile{Data: []byte("-- empty")}})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	if r == nil {
		t.Fatal("nil runner")
	}
}

func TestBuildRunner_EnvDSNFallback(t *testing.T) {
	// mode defaults to fail-closed production; the migrator only needs the DSN,
	// so opt into dev explicitly to exercise the env-DSN fallback path.
	t.Setenv("KACHO_NLB_MODE", "dev")
	// Объявление домена величин — часть законной посадки: у ручки ровно два
	// законных значения, и незаданное среди них не значится.
	t.Setenv("KACHO_NLB_QUOTA__AUTHORITY", "not-deployed")
	t.Setenv("KACHO_NLB_AUTHZ__TRUST_ANY_FORWARDER", "true") // локальная фикстура: круг не сужаем ЯВНО
	t.Setenv("KACHO_NLB_REPOSITORY__POSTGRES__URL", "postgres://envuser:envpass@h/db")
	opts := &rootOptions{dialect: "postgres" /* dsn пуст*/}
	r, err := buildRunner(opts, fstest.MapFS{"0001_x.sql": &fstest.MapFile{Data: []byte("-- empty")}})
	if err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	if r == nil {
		t.Fatal("nil runner")
	}
}

func TestBuildRunner_NoDSN_NoConfig_Fails(t *testing.T) {
	opts := &rootOptions{dialect: "postgres"}
	_, err := buildRunner(opts, emptyFS())
	if err == nil {
		t.Fatal("expected error when DSN/ENV/config all empty, got nil")
	}
	// config.Load выкидывает validation-ошибку про repository.postgres.url
	if !strings.Contains(err.Error(), "repository.postgres.url") &&
		!strings.Contains(err.Error(), "dsn unset") {
		t.Fatalf("expected DSN-source error, got: %v", err)
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

// TestBuildRunner_SharedEnvDSNIsRead — переменная `KACHO_MIGRATOR_DSN` одна на
// семь сервисов (#1461, docs/architecture/migrator-cli.md).
//
// nlb её не читал вовсе: он шёл своим путём `KACHO_NLB_REPOSITORY__POSTGRES__URL`
// через config.Load. Оператор, задавший общую переменную и получивший УСПЕХ на
// шести сервисах, на седьмом получал отказ загрузки конфигурации — то есть
// знание об одном сервисе к соседнему не применялось.
//
// Опыт, которым это померено на собранных бинарях:
//
//	env -u KACHO_NLB_REPOSITORY__POSTGRES__URL KACHO_MIGRATOR_DSN='@@@не-dsn@@@' \
//	  <s>-migrator status
//
// Шесть отвечали «cannot parse `@@@не-dsn@@@`» (переменная прочитана), nlb —
// «dsn unset (--dsn) and config load failed». Утверждается ЧТЕНИЕ переменной, а
// не успех соединения: значение заведомо негодное, и отказ обязан называть ЕГО.
func TestBuildRunner_SharedEnvDSNIsRead(t *testing.T) {
	t.Setenv("KACHO_MIGRATOR_DSN", "postgres://shared:env@h:5432/db?sslmode=disable")
	opts := &rootOptions{dialect: "postgres" /* dsn пуст, --config не задан */}
	r, err := buildRunner(opts, fstest.MapFS{"0001_x.sql": &fstest.MapFile{Data: []byte("-- empty")}})
	if err != nil {
		t.Fatalf("общая переменная не прочитана, отказ пришёл от запасного пути: %v", err)
	}
	if r == nil {
		t.Fatal("nil runner")
	}
}

// TestBuildRunner_FlagBeatsSharedEnvDSN — положительный контроль порядка к
// пробе выше. Без него она зеленела бы на реализации, читающей ТОЛЬКО общую
// переменную и игнорирующей явно переданный адрес.
func TestBuildRunner_FlagBeatsSharedEnvDSN(t *testing.T) {
	t.Setenv("KACHO_MIGRATOR_DSN", "@@@негодное@@@")
	opts := &rootOptions{dialect: "postgres", dsn: "postgres://u:p@h:5432/db?sslmode=disable"}
	if _, err := buildRunner(opts, emptyFS()); err != nil {
		t.Fatalf("явный --dsn не перекрыл общую переменную: %v", err)
	}
}

// TestEmptyCommandLineIsRefused — пустая командная строка обязана быть ОТКАЗОМ,
// а не успехом (#1461).
//
// Cobra при корне без исполнения печатает помощь и выходит УСПЕХОМ. Опыт по
// собранным бинарям: `nlb-migrator; echo $?` давал 0 у трёх делегирующих
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

// TestBuildRunner_ConfigLaneAsksOnlyWhatTheApplyPointUses — точка наката
// спрашивает у конфигурации РОВНО то, чем пользуется сама.
//
// Предмет: у накатчика нет ни слушателя, ни соседей, ни переданной личности —
// значит посадка СЛУЖБЫ его не связывает. Он читает адрес базы и режим
// шифрования до неё; всё остальное объявление принадлежит процессу, который
// служит, и отказ по нему назвал бы не тот предмет.
//
// Фикстура отличается от законной посадки службы ОДНИМ фактом — в ней нет
// объявления домена величин, — и второй половиной пробы это утверждается прямо:
// та же самая строка через дверь СЛУЖБЫ обязана быть отвергнута с именем ручки.
// Без этой половины «накатчик прошёл» зеленело бы и на конфигурации, которая
// посадку службы проходит целиком, то есть проба не различала бы двери.
func TestBuildRunner_ConfigLaneAsksOnlyWhatTheApplyPointUses(t *testing.T) {
	// Общая переменная адреса базы гасится: не погасив её, полоса конфигурации
	// зеленела бы на DSN из окружения прогона — то есть доказывала бы не себя.
	t.Setenv(migratorcli.EnvDSN, "")

	path := filepath.Join(t.TempDir(), "config.yaml")
	const body = "mode: dev\n" +
		"repository:\n  postgres:\n    url: postgres://u:p@h:5432/kacho_nlb?sslmode=disable\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура не записана: %v", err)
	}

	opts := &rootOptions{dialect: "postgres", configPath: path}
	if _, err := buildRunner(opts, fstest.MapFS{"0001_x.sql": &fstest.MapFile{Data: []byte("-- empty")}}); err != nil {
		t.Fatalf("точка наката отвергла конфигурацию, в которой есть всё, чем она "+
			"пользуется. Отказ накатчика по посадке СЛУЖБЫ называет не тот предмет: "+
			"у процесса, чья работа — накатить цепочку, нет ни одного из объявленных "+
			"в нём предметов:\n%v", err)
	}

	// Положительный близнец: дверь СЛУЖБЫ на той же строке обязана отказать и
	// назвать ручку. Ослабь кто-нибудь стража посадки — эта половина покраснеет.
	if _, err := config.Load(path); err == nil {
		t.Fatal("дверь службы приняла конфигурацию БЕЗ объявления домена величин — " +
			"страж посадки ослаблен, и первая половина пробы больше ничего не различает")
	} else if !strings.Contains(err.Error(), "quota.authority") {
		t.Fatalf("отказ двери службы не называет ручку — оператор не поймёт, что "+
			"крутить:\n%v", err)
	}
}
