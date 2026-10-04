// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
	"github.com/PRO-Robotech/kacho/services/notify/internal/probemigrations"
)

// firstSQL — метка встроенной FS цепочки пробы: первый файл миграции.
func firstSQL(t *testing.T, fsys fs.FS) string {
	t.Helper()
	m, err := fs.Glob(fsys, "*.sql")
	if err != nil || len(m) == 0 {
		t.Fatalf("в FS цепочки нет миграций (%v)", err)
	}
	return m[0]
}

// Проба точки (Д74): DSN с базой kacho_notifyprobe → цепочка пробы, в обеих
// записях DSN, которыми точку зовут (URL пробы и ключевая форма манифеста).
func TestProbeDatabaseSelectsTheProbeChain(t *testing.T) {
	want := firstSQL(t, probemigrations.FS)
	for _, dsn := range []string{
		"postgres://u:p@db:5432/kacho_notifyprobe?sslmode=require",
		"host=db port=5432 user=u password=p dbname=kacho_notifyprobe sslmode=require",
	} {
		c, fsys, err := selectChain(chainsTable, embedded, dsn)
		if err != nil {
			t.Fatalf("%q: отказ %v", dsn, err)
		}
		if c.Database != "kacho_notifyprobe" || c.Dir != "services/notify/internal/probemigrations" {
			t.Fatalf("%q: выбрана строка %+v", dsn, c)
		}
		if got := firstSQL(t, fsys); got != want {
			t.Fatalf("%q: FS выбранной строки не цепочка пробы (%s против %s)", dsn, got, want)
		}
	}
}

// Чужое имя базы — отказ с именем базы и перечнем допустимых, без наката.
// Имя pgtest (`kacho_<cfg>_tNNNN`) — та же форма отказа: запасной ветки на имя
// вне таблицы у точки нет (CX1-115 (б)).
func TestForeignDatabaseIsRefusedWithItsName(t *testing.T) {
	for _, db := range []string{"kacho_vpc", "kacho_notifyprobe_t0001", "postgres"} {
		_, _, err := selectChain(chainsTable, embedded, "postgres://u:p@db:5432/"+db)
		if err == nil {
			t.Fatalf("база %s принята — точка накатывает только базы своей таблицы", db)
		}
		for _, s := range []string{db, "kacho_notifyprobe", pointDir} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("отказ по базе %s не называет %q: %v", db, s, err)
			}
		}
	}
}

// DSN без имени базы — отказ, а не выбор «первой строки».
func TestDSNWithoutDatabaseIsRefused(t *testing.T) {
	if _, _, err := selectChain(chainsTable, embedded, "host=db user=u"); err == nil {
		t.Fatal("DSN без имени базы принят")
	}
}

// Страж равенства: множества каталогов таблицы и встроенных FS не равны —
// отказ старта с обоими множествами. Близнец — таблица и FS точки как есть.
func TestTableAndEmbeddedSetsMustBeEqual(t *testing.T) {
	const probeDSN = "postgres://u:p@db:5432/kacho_notifyprobe"
	if _, _, err := selectChain(chainsTable, embedded, probeDSN); err != nil {
		t.Fatalf("таблица точки и её встроенные FS расходятся: %v", err)
	}
	extra := map[string]fs.FS{
		"services/notify/internal/probemigrations": probemigrations.FS,
		"services/notify/internal/migrations":      fstest.MapFS{},
	}
	_, _, err := selectChain(chainsTable, extra, probeDSN)
	if err == nil {
		t.Fatal("встроенная FS без строки таблицы принята")
	}
	for _, s := range []string{"services/notify/internal/migrations", "services/notify/internal/probemigrations"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("отказ не называет %q: %v", s, err)
		}
	}
	if _, _, err := selectChain(chainsTable, map[string]fs.FS{}, probeDSN); err == nil {
		t.Fatal("строка таблицы без встроенной FS принята")
	}
}

// Инъекция (Д74): строка kacho_notifyprobe снята из таблицы → накат пробы
// отказывает с именем базы.
func TestRemovedRowRefusesTheProbeDatabase(t *testing.T) {
	table := []byte("chains:\n  - database: kacho_other\n    dir: services/notify/internal/probemigrations\n")
	_, _, err := selectChain(table, embedded, "postgres://u:p@db:5432/kacho_notifyprobe")
	if err == nil || !strings.Contains(err.Error(), "kacho_notifyprobe") {
		t.Fatalf("строка снята, а база пробы не отвергнута с именем: %v", err)
	}
}

// helperEnv — переменная, по которой тестовый бинарь исполняет main() точки:
// отказ точки наблюдается тем же каналом, что у init-контейнера, — stderr
// процесса и его код выхода.
const helperEnv = "KACHO_NOTIFY_MIGRATOR_TEST_RUN_MAIN"

// helperRowEnv / helperAnswerEnv — подмены процесса-помощника для пробы точки
// на живой базе: строка таблицы цепочек с именем базы pgtest (имени пробы в
// таблице точки нет, а накатывать предстоит её цепочку) и ответ запроса сверки
// чужим именем (сервер, открывший соединение не к той базе). Прод их не
// читает: подмена живёт только в тестовом бинаре.
const (
	helperRowEnv    = "KACHO_NOTIFY_MIGRATOR_TEST_ROW_DB"
	helperAnswerEnv = "KACHO_NOTIFY_MIGRATOR_TEST_ANSWER"
)

// TestHelperRunsMain — не проба: тело процесса-помощника.
func TestHelperRunsMain(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	if db := os.Getenv(helperRowEnv); db != "" {
		dir := "services/notify/internal/probemigrations"
		if d := os.Getenv(helperDirEnv); d != "" {
			dir = d
		}
		// Таблица подмены — одна строка, поэтому из встроенных FS остаётся только
		// FS её каталога: равенство множеств судится как в бою, и FS, которой у
		// точки нет, подмена не создаёт.
		chainsTable = []byte("chains:\n  - database: " + db + "\n    dir: " + dir + "\n")
		for d := range embedded {
			if d != dir {
				delete(embedded, d)
			}
		}
	}
	if answer := os.Getenv(helperAnswerEnv); answer != "" {
		currentDatabaseQuery = "SELECT '" + answer + "'::text"
	}
	os.Args = []string{binaryName, "up"}
	main()
	os.Exit(0)
}

// runPoint исполняет main() точки с DSN в KACHO_MIGRATOR_DSN; stderr и код.
// extra — подмены процесса-помощника (helperRowEnv, helperAnswerEnv).
func runPoint(t *testing.T, dsn string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperRunsMain$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "KACHO_MIGRATOR_DSN="+dsn, "PGDATABASE=")
	cmd.Env = append(cmd.Env, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// SEC-W1-01, наблюдаемое: отказ точки на DSN с паролем-маркером в обеих
// записях — stderr не несёт маркера ни целиком, ни частью. Близнец — годный
// DSN с тем же маркером и базой вне таблицы: отказ называет базу (канал
// наблюдения жив), маркера нет.
func TestPointRefusalCarriesNoPasswordMarker(t *testing.T) {
	cases := []struct {
		dsn     string
		markers []string
		names   string
	}{
		{"postgres://notifyprobe:Sup3r%Secret@db-host-x:5432/kacho_notifyprobe?sslmode=require",
			[]string{"Sup3r", "Secret", "%Se"}, pointDir},
		{"host=db-host-x port=5432 user=notify password=Tail0f Secret dbname='kacho_notifyprobe sslmode=require",
			[]string{"Tail0f", "Secret"}, pointDir},
		// Близнец: DSN разбирается, база вне таблицы — отказ с её именем.
		{"postgres://notifyprobe:Sup3rSecret@db-host-x:5432/kacho_vpc?sslmode=require",
			[]string{"Sup3r", "Secret"}, "kacho_vpc"},
	}
	for _, c := range cases {
		stderr, err := runPoint(t, c.dsn)
		if err == nil {
			t.Errorf("DSN %q: точка вышла успехом, а обязана отказать до соединения:\n%s", c.dsn, stderr)
			continue
		}
		if !strings.Contains(stderr, c.names) {
			t.Errorf("DSN %q: stderr не называет %q — отказ не тот:\n%s", c.dsn, c.names, stderr)
		}
		for _, m := range c.markers {
			if strings.Contains(stderr, m) {
				t.Errorf("DSN %q: stderr точки несёт кусок пароля %q:\n%s", c.dsn, m, stderr)
			}
		}
	}
}

// Д83 — после соединения точка сверяет current_database() со строкой таблицы
// до первого оператора goose: неравенство — отказ с обоими именами, без
// наката. Близнец — равенство → накат разрешён. Ошибка чтения — отказ, а не
// «сверка пройдена».
func TestConnectedDatabaseMustBeTheRowDatabase(t *testing.T) {
	ctx := context.Background()
	answer := func(name string, err error) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return name, err }
	}
	if err := confirmDatabase(ctx, answer("kacho_notifyprobe", nil), "kacho_notifyprobe", time.Second); err != nil {
		t.Fatalf("близнец: база соединения равна строке, а отказ: %v", err)
	}
	err := confirmDatabase(ctx, answer("kacho_notify", nil), "kacho_notifyprobe", time.Second)
	if err == nil {
		t.Fatal("соединение с kacho_notify при строке kacho_notifyprobe принято — накат ушёл бы в чужую базу")
	}
	for _, s := range []string{"kacho_notify", "kacho_notifyprobe", pointDir, "наката нет"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("отказ сверки не называет %q: %v", s, err)
		}
	}
	if err := confirmDatabase(ctx, answer("", errors.New("conn reset")), "kacho_notifyprobe", time.Second); err == nil {
		t.Fatal("ошибка чтения current_database() принята за пройденную сверку")
	}
}

// GS-C2 — у сверки свой предел: сервер, принявший соединение и замерший
// (failover, зависший пулер), не держит init-контейнер бесконечно — истечение
// предела есть отказ «наката нет», а не ожидание. Ответ, пришедший после
// предела, не принимается. Близнец — ответ в пределе принят.
func TestConfirmDatabaseRefusesWhenTheNameDoesNotArriveInTime(t *testing.T) {
	const limit = 100 * time.Millisecond
	late := func(ctx context.Context) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
			return "kacho_notifyprobe", nil
		}
	}
	start := time.Now()
	err := confirmDatabase(context.Background(), late, "kacho_notifyprobe", limit)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("ответ, пришедший через %s при пределе %s, принят — замерший сервер держал бы накат", took, limit)
	}
	for _, s := range []string{pointDir, "kacho_notifyprobe", "наката нет", limit.String()} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("отказ по пределу не называет %q: %v", s, err)
		}
	}
	if took > time.Second {
		t.Errorf("отказ пришёл через %s при пределе %s — предел не действует", took, limit)
	}
	inTime := func(ctx context.Context) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			return "", errors.New("чтение имени базы идёт без предела")
		}
		return "kacho_notifyprobe", nil
	}
	if err := confirmDatabase(context.Background(), inTime, "kacho_notifyprobe", limit); err != nil {
		t.Errorf("близнец: ответ в пределе, а отказ: %v", err)
	}
}

// gooseState — есть ли в базе dsn учётная таблица goose и сколько версий в ней
// применено.
func gooseState(t *testing.T, dsn string) (bool, int) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: база пробы не открыта: %v", err)
	}
	defer func() { _ = db.Close() }()
	var table sql.NullString
	if err := db.QueryRow(`SELECT to_regclass('goose_db_version')::text`).Scan(&table); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: to_regclass не прочитан: %v", err)
	}
	if !table.Valid {
		return false, 0
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM goose_db_version WHERE version_id > 0`).Scan(&n); err != nil {
		t.Fatalf("учётная таблица goose не прочитана: %v", err)
	}
	return true, n
}

// Д83, вторая половина — проводка сверки в main() на живом сервере. Точка
// исполняется процессом (тот же канал, что у init-контейнера: stderr и код
// выхода) против пустой базы pgtest, строка таблицы называет эту базу.
//
//   - сервер отвечает на запрос сверки чужим именем kacho_x → отказ с обоими
//     именами, учётной таблицы goose в базе НЕТ (to_regclass IS NULL): накат не
//     начинался;
//   - близнец — ответ без подмены → накат прошёл, применено = объявлено
//     цепочкой пробы. Близнец исполняет и настоящее чтение current_database()
//     на живом сервере: его поломка (fail-closed) остановила бы и этот накат.
//
// Инъекция «вызов сверки снят из main()» → в первом случае накат проходит,
// таблица goose появляется → красный (Д95, таблица возврата полосы).
func TestPointConfirmsTheConnectedDatabaseBeforeGoose(t *testing.T) {
	want, err := fs.Glob(probemigrations.FS, "*.sql")
	if err != nil || len(want) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в FS цепочки пробы нет миграций (%v)", err)
	}

	foreign := pgtest.NewEmptyDB(t)
	db, err := migrationchains.DatabaseOf(foreign)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: имя базы pgtest не разобрано: %v", err)
	}
	stderr, err := runPoint(t, foreign, helperRowEnv+"="+db, helperAnswerEnv+"=kacho_x")
	if err == nil {
		t.Errorf("сервер ответил базой kacho_x при строке %s, а точка вышла успехом:\n%s", db, stderr)
	}
	for _, s := range []string{"kacho_x", db, pointDir, "наката нет"} {
		if !strings.Contains(stderr, s) {
			t.Errorf("отказ сверки не называет %q:\n%s", s, stderr)
		}
	}
	if exists, n := gooseState(t, foreign); exists {
		t.Errorf("сверка не прошла, а учётная таблица goose в базе %s есть (применено %d) — накат начался "+
			"до сверки либо без неё", db, n)
	}

	twin := pgtest.NewEmptyDB(t)
	tdb, err := migrationchains.DatabaseOf(twin)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: имя базы pgtest не разобрано: %v", err)
	}
	if stderr, err := runPoint(t, twin, helperRowEnv+"="+tdb); err != nil {
		t.Fatalf("близнец: база соединения равна строке %s, а точка отказала (%v):\n%s", tdb, err, stderr)
	}
	exists, n := gooseState(t, twin)
	if !exists || n != len(want) {
		t.Fatalf("близнец: применено %d (таблица goose есть: %v), объявлено цепочкой пробы %d", n, exists, len(want))
	}
	t.Logf("сверка на живой базе: чужое имя → отказ без наката; близнец %s → применено %d / объявлено %d",
		tdb, n, len(want))
}
