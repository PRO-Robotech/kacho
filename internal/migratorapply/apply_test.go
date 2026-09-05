// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// applyBudget — предел на один запуск точки наката. Самая длинная цепочка дерева
// (iam, 148 миграций) накатывается за считанные секунды; предел стоит
// многократно выше, чтобы отличать «медленно» от «висит», а не резать хвост.
const applyBudget = 3 * time.Minute

// migrationsDirOf — каталог цепочки сервиса. Выводится из пути точки наката, а не
// выписывается: перечень сервисов здесь не заводится ни в каком виде.
func migrationsDirOf(pkgPath string) (service, dir string) {
	service = strings.TrimSuffix(strings.TrimPrefix(pkgPath, "services/"), "/cmd/migrator")
	return service, filepath.Join("services", service, "internal", "migrations")
}

// repoRoot — каталог с go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod не найден выше %s — корень репозитория не определён", dir)
		}
		dir = parent
	}
}

// applyPoints — точки наката, ВЫВЕДЕННЫЕ из дерева. Список не выписывается: он
// разошёлся бы с деревом молча, и разошёлся бы именно на новом сервисе — там, где
// слепая зона дороже всего.
func applyPoints(t *testing.T, root string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	list := exec.CommandContext(ctx, "go", "list", "./services/...")
	// Обход идёт от КОРНЯ репозитория: рабочий каталог пробы — её собственный
	// пакет, и относительный образец резолвился бы там в пустоту.
	list.Dir = root
	var stderr strings.Builder
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list сорвался — состав точек наката НЕ ИЗМЕРЕН, "+
			"а пустой перечень здесь означал бы зелёную пробу с нулём доказанного: %v\n%s",
			err, stderr.String())
	}
	const modulePrefix = "github.com/PRO-Robotech/kacho/"
	var points []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		rel := strings.TrimPrefix(strings.TrimSpace(line), modulePrefix)
		if strings.HasPrefix(rel, "services/") && strings.HasSuffix(rel, "/cmd/migrator") {
			points = append(points, rel)
		}
	}
	sort.Strings(points)
	return points
}

// chainLength — сколько миграций объявляет цепочка сервиса. Это ЭТАЛОН, с которым
// сверяется число применённых: без него «накат прошёл» означало бы лишь «бинарь
// вышел нулём», а накат, применивший НОЛЬ миграций, выходит нулём тоже.
//
// Состав берётся у ИНДЕКСА git, а не с диска: неотслеживаемый `.sql`, оставшийся
// в каталоге от чужой работы, завысил бы эталон — и проба покраснела бы на
// исправном накате, назвав виновником сервис. Обратный случай тише и хуже:
// эталон, совпавший с применённым по случайности, зеленел бы на неполном накате.
func chainLength(t *testing.T, root, dir string) int {
	t.Helper()
	files, err := treecorpus.Glob(filepath.Join(root, dir, "*.sql"))
	if err != nil {
		t.Fatalf("состав цепочки %s НЕ ИЗМЕРЕН (%v) — эталона для сверки нет, "+
			"и «накат прошёл» означало бы только «бинарь вышел нулём»", dir, err)
	}
	return len(files)
}

// appliedCount — сколько миграций числит применёнными сам goose.
func appliedCount(t *testing.T, dsn string) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("открытие базы для сверки: %v", err)
	}
	defer func() { _ = db.Close() }()

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM goose_db_version WHERE version_id > 0`).Scan(&n); err != nil {
		t.Fatalf("учётная таблица goose не прочитана — накат не доказан: %v", err)
	}
	return n
}

// replaceDBName подменяет имя базы в DSN, оставляя всё прочее нетронутым: контроль
// обязан отличаться от годного входа ровно тем, что проверяется.
func replaceDBName(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN пробы неразбираем (%q): %v", dsn, err)
	}
	u.Path = "/" + name
	return u.String()
}

// runMigrator запускает собранный бинарь и возвращает исход вместе с выводом.
// Вывод возвращается ВСЕГДА: диагностика — часть свойства, и находка, называющая
// «код 1» без текста, посылает читателя искать не там.
func runMigrator(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	// Переменная DSN окружения гасится. Флаг её ПЕРЕБИВАЕТ (приоритет у всех семи
	// один: --dsn > KACHO_MIGRATOR_DSN > конфигурация), поэтому на сегодняшних
	// вызовах это ничего не меняет — гасится она затем, чтобы вызов БЕЗ флага,
	// если такой здесь когда-нибудь заведут, не увёл накат на базу из окружения
	// прогона молча.
	return runMigratorEnv(t, bin, []string{"KACHO_MIGRATOR_DSN="}, args...)
}

// runMigratorEnv — тот же запуск с добавленным окружением.
//
// Окружение — параметр, а не константа, потому что DSN приходит накату ТРЕМЯ
// источниками (`--dsn` > `KACHO_MIGRATOR_DSN` > конфигурация), и доказательство
// формы вызова (invocation_test.go) гоняет последний из них — тот, которым
// пользуются все семь развёртываний.
func runMigratorEnv(t *testing.T, bin string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), applyBudget)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// buildApplyPoint собирает бинарь точки наката. Отказ сборки — «сервис не
// развернётся», а не «проба не смогла»: он называется предметом, а не средством.
func buildApplyPoint(t *testing.T, root, binDir, pkg, service string) string {
	t.Helper()
	bin := filepath.Join(binDir, service)
	ctx, cancel := context.WithTimeout(context.Background(), applyBudget)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./"+pkg)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("точка наката %s НЕ СОБИРАЕТСЯ — сервис не развернётся: %v\n%s", pkg, err, out)
	}
	return bin
}

// TestEveryMigratorAppliesItsChainToALiveDatabase — доказательство наката.
//
// На каждую точку наката: собрать бинарь, выдать ПУСТУЮ базу, запустить `up`,
// сверить число применённых миграций с длиной цепочки, повторить `up` (накат
// идемпотентен) и прочитать `status`.
//
// Сверка с длиной цепочки — несущая. Без неё зелёным был бы и накат, не
// применивший НИ ОДНОЙ миграции: бинарь, которому нечего делать, выходит нулём.
func TestEveryMigratorAppliesItsChainToALiveDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	points := applyPoints(t, root)

	if len(points) == 0 {
		t.Fatal("точек наката НЕ НАЙДЕНО — обход пуст, доказывать нечего. " +
			"Это отказ, а не «нечего запускать»: зелёная проба с нулём доказанного и есть " +
			"тот класс, ради которого пакет заведён")
	}

	binDir := t.TempDir()
	proven, failed := 0, 0

	for _, pkg := range points {
		service, migDir := migrationsDirOf(pkg)

		ok := t.Run(service, func(t *testing.T) {
			want := chainLength(t, root, migDir)
			if want == 0 {
				t.Fatalf("в %s НЕТ ни одного файла миграции — эталона для сверки не существует, "+
					"и «накат прошёл» здесь означало бы только «бинарь вышел нулём»", migDir)
			}

			bin := buildApplyPoint(t, root, binDir, pkg, service)

			dsn := pgtest.NewEmptyDB(t)

			out, err := runMigrator(t, bin, "up", "--dsn", dsn)
			if err != nil {
				t.Fatalf("накат %s на пустую базу ОТКАЗАЛ (%v) — это и означает «сервис не "+
					"разворачивается»:\n%s", service, err, out)
			}
			if got := appliedCount(t, dsn); got != want {
				t.Fatalf("накат %s вышел успехом, но применил %d миграций из %d объявленных цепочкой. "+
					"Успех на неполном накате хуже отказа: схема не та, а вердикт зелёный.\n%s",
					service, got, want, out)
			}

			// Повторный накат. Init-контейнер запускается на КАЖДОМ развёртывании, а
			// не однажды, поэтому «применяется дважды» — штатный режим, а не край.
			if out, err := runMigrator(t, bin, "up", "--dsn", dsn); err != nil {
				t.Fatalf("повторный накат %s отказал (%v) — развёртывание на уже накатанной базе "+
					"не пройдёт:\n%s", service, err, out)
			}
			if got := appliedCount(t, dsn); got != want {
				t.Fatalf("повторный накат %s изменил число применённых: %d вместо %d", service, got, want)
			}

			if out, err := runMigrator(t, bin, "status", "--dsn", dsn); err != nil {
				t.Fatalf("status %s отказал на накатанной базе (%v):\n%s", service, err, out)
			}

			// Счёт ведётся ИЗНУТРИ тела, последним оператором, и это не стиль.
			// Возврат t.Run равен true и тогда, когда подпроба ОТФИЛЬТРОВАНА и не
			// исполнялась вовсе, — считать по нему значило бы записывать в
			// доказанное то, чего не запускали. Ровно так первая редакция этой
			// пробы отчиталась «доказано 6» на прогоне, где исполнялась одна точка.
			proven++
		})
		if !ok {
			failed++
		}
	}

	t.Logf("перепись: миграторов %d, накат доказан для %d", len(points), proven)

	// Область зелёного называется явно. Доказано меньше найденного — вердикт
	// относится к доказанному, и молчание об этом сделало бы частичный прогон
	// неотличимым от полного.
	switch {
	case failed > 0:
		t.Errorf("накат доказан для %d точек из %d, отказали %d — находки выше", proven, len(points), failed)
	case proven != len(points):
		t.Errorf("доказано %d из %d при нуле отказов — прогон отфильтрован (-run), и его зелёное "+
			"относится к %d точкам, а не к %d. Цель test-pg-outside-selection фильтра не ставит",
			proven, len(points), proven, len(points))
	}
}

// TestApplyProofDistinguishesFailureFromSuccess — положительный контроль наоборот.
//
// Все утверждения пробы выше держатся на том, что она ЧИТАЕТ исход запуска. Если
// бы не читала, они были бы зелены при любом состоянии дерева. Здесь тот же
// запуск подаётся на базу, которой НЕТ, и обязан ОТКАЗАТЬ.
//
// Без этой половины «накат прошёл» неотличимо от «мы его не проверяли».
//
// # Почему несуществующая база, а не недостижимый порт
//
// Недостижимый порт тоже даёт отказ — но через барьер готовности базы, а у него
// СВОЙ бюджет (init-контейнер обязан пережить гонку с подъёмом Postgres, и это
// верное поведение). Замерено: такой контроль стоил 120 с против 25 с у всего
// доказательства, то есть впятеро дороже предмета. Несуществующая база на ЖИВОМ
// сервере отвергается сразу — барьер ждёт только «сервер не принимает
// соединения», а не «такой базы нет».
func TestApplyProofDistinguishesFailureFromSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("собирает точку наката; гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	points := applyPoints(t, root)
	if len(points) == 0 {
		t.Fatal("точек наката не найдено — контроль беспредметен")
	}

	pkg := points[0]
	service, _ := migrationsDirOf(pkg)
	bin := buildApplyPoint(t, root, t.TempDir(), pkg, service)

	// Живой сервер, несуществующая база: DSN отличается от годного ровно тем, что
	// проверяется, — и потому отказ приходит от НАКАТА, а не от разбора аргументов.
	missing := replaceDBName(t, pgtest.NewEmptyDB(t), "kacho_migratorapply_no_such_db")

	out, err := runMigrator(t, bin, "up", "--dsn", missing)
	if err == nil {
		t.Fatalf("накат на НЕСУЩЕСТВУЮЩУЮ базу вышел УСПЕХОМ — проба не читает исход запуска, "+
			"и все её утверждения о накате вакуумны:\n%s", out)
	}
	t.Logf("контроль: несуществующая база даёт отказ, как и должна (%v)", err)
}
