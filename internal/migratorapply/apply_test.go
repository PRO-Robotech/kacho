// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
	"github.com/PRO-Robotech/kacho/internal/pgdsn"
)

// applyBudget — предел на один запуск точки наката. Самая длинная цепочка дерева
// (iam, 148 миграций) накатывается за считанные секунды; предел стоит
// многократно выше, чтобы отличать «медленно» от «висит», а не резать хвост.
const applyBudget = 3 * time.Minute

// pointService — служба точки наката `services/<svc>/cmd/migrator`.
func pointService(pkgPath string) string {
	return strings.TrimSuffix(strings.TrimPrefix(pkgPath, "services/"), "/cmd/migrator")
}

// treeChains — цепочки дерева у ЕДИНСТВЕННОГО их вывода,
// [migrationchains.List] (kacho#2915, CX1-114). Каталог цепочки здесь из
// имени службы больше не выводится: у каталога services/notify две базы и
// цепочка пробы в services/notify/internal/probemigrations, и вывод из имени
// её не видел бы. Отказ перечня — отказ пробы с его текстом (точка с таблицей
// без строк называется по имени, а не превращается в «0 = 0»).
func treeChains(t *testing.T, root string) []migrationchains.Chain {
	t.Helper()
	chains, err := migrationchains.List(root)
	if err != nil {
		t.Fatalf("перечень цепочек дерева: %v", err)
	}
	return chains
}

// chainLabel — имя подпробы и строки печати: служба и база строки.
func chainLabel(c migrationchains.Chain) string {
	if c.Database == "" {
		return c.Service
	}
	return c.Service + "/" + c.Database
}

// databaseColumn — колонка «Database» печати на строку.
func databaseColumn(c migrationchains.Chain) string {
	if c.Database == "" {
		return "не выбирает"
	}
	return c.Database
}

// chainDSN — база, на которую накатывается строка цепочки (CX1-115 (а)).
//
// Пустое Database — точка по имени базы не выбирает, и база — pgtest.NewEmptyDB
// (имя `kacho_<cfg>_tNNNN`), как было. Непустое — база РОВНО С ИМЕНЕМ СТРОКИ:
// её заводит `CREATE DATABASE` по DSN NewEmptyDB, имя в DSN подменяет
// replaceDBName, снимается база в t.Cleanup. Имени базы из имени службы не
// выводит никто — его объявляет таблица точки.
func chainDSN(t *testing.T, c migrationchains.Chain) string {
	t.Helper()
	base := pgtest.NewEmptyDB(t)
	if c.Database == "" {
		return base
	}
	db, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("открытие базы пробы для CREATE DATABASE %s: %v", c.Database, err)
	}
	defer func() { _ = db.Close() }()
	ident := `"` + strings.ReplaceAll(c.Database, `"`, `""`) + `"`
	if _, err := db.Exec("CREATE DATABASE " + ident); err != nil {
		t.Fatalf("база строки %s (точка %s) не заведена: %v", c.Database, c.Point, err)
	}
	t.Cleanup(func() {
		drop, err := sql.Open("pgx", base)
		if err != nil {
			t.Errorf("снятие базы %s: %v", c.Database, err)
			return
		}
		defer func() { _ = drop.Close() }()
		if _, err := drop.Exec("DROP DATABASE IF EXISTS " + ident + " WITH (FORCE)"); err != nil {
			t.Errorf("снятие базы %s: %v", c.Database, err)
		}
	})
	return replaceDBName(t, base, c.Database)
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

// applyPoints — точки наката, ВЫВЕДЕННЫЕ ИЗ ДЕРЕВА.
//
// Из дерева, а не из `go list ./services/...`, и различие несущее. `go list`
// границу Go-модуля НЕ ПЕРЕСЕКАЕТ: `services/iam` объявляет собственный модуль
// (`github.com/PRO-Robotech/kaname`), поэтому его точка наката не попадала в
// перечень BY CONSTRUCTION. Перепись при этом сходилась сама с собой — «точек
// наката 6, форма выведена для 6», — и «шесть из шести» читалось как полнота
// (kacho#2183, kacho#2255).
//
// Тот же класс, что «ноль находок неотличимо от ноль прочитанного», только на
// уровне ЕДИНИЦЫ СЧЁТА: осмотренное сходится с найденным, а осматривать надо
// было больше. Индекс git модульной границы не знает и потому отвечает о ДЕРЕВЕ.
func applyPoints(t *testing.T, root string) []string {
	t.Helper()
	files, err := treecorpus.Glob(filepath.Join(root, "services", "*", "cmd", "migrator", "main.go"))
	if err != nil {
		t.Fatalf("состав точек наката НЕ ИЗМЕРЕН (%v) — а пустой перечень здесь "+
			"означал бы зелёную пробу с нулём доказанного", err)
	}
	var points []string
	for _, abs := range files {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, rerr)
		}
		points = append(points, filepath.ToSlash(filepath.Dir(rel)))
	}
	sort.Strings(points)
	return points
}

// moduleOfPoint — модуль, которому принадлежит точка наката, и путь пакета
// ОТНОСИТЕЛЬНО этого модуля.
//
// Существует ровно потому же, почему перечень берётся у дерева: сборка идёт из
// модуля-владельца. `go build ./services/iam/cmd/migrator` из корня отвечает
// «main module does not contain package» — тот модуль этого пакета не содержит.
func moduleOfPoint(t *testing.T, root, pkg string) (moduleDir, pkgInModule string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && st.Mode().IsRegular() {
			rel, rerr := filepath.Rel(dir, filepath.Join(root, filepath.FromSlash(pkg)))
			if rerr != nil {
				t.Fatalf("пакет %s не сводится под модуль %s: %v", pkg, dir, rerr)
			}
			return dir, "./" + filepath.ToSlash(rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir || len(dir) <= len(root) {
			t.Fatalf("модуль точки наката %s не найден подъёмом за маркером go.mod — "+
				"собрать её нечем, и это отказ, а не пропуск", pkg)
		}
		dir = parent
	}
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
// обязан отличаться от годного входа ровно тем, что проверяется. DSN читает разбор
// драйвера (pgdsn.WithDatabase → pgconn.ParseConfig, Д93).
func replaceDBName(t *testing.T, dsn, name string) string {
	t.Helper()
	out, err := pgdsn.WithDatabase(dsn, name)
	if err != nil {
		t.Fatalf("DSN пробы: %v", err)
	}
	return out
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
// пользуются развёртывания ВСЕХ точек наката дерева.
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
	moduleDir, pkgInModule := moduleOfPoint(t, root, pkg)
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, pkgInModule)
	build.Dir = moduleDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("точка наката %s НЕ СОБИРАЕТСЯ — сервис не развернётся: %v\n%s", pkg, err, out)
	}
	return bin
}

// TestEveryMigratorAppliesItsChainToALiveDatabase — доказательство наката.
//
// На каждую цепочку дерева (migrationchains.List, строка таблицы точки): собрать
// бинарь её точки, выдать ПУСТУЮ базу строки (chainDSN: NewEmptyDB либо база
// ровно с именем строки), запустить `up`, сверить число применённых миграций с
// длиной цепочки, повторить `up` (накат идемпотентен) и прочитать `status`.
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

	chains := treeChains(t, root)
	binDir := t.TempDir()
	bins := map[string]string{}
	proven, failed, notify := 0, 0, 0

	for _, c := range chains {

		if c.Service == "notify" {
			notify++
		}
		ok := t.Run(chainLabel(c), func(t *testing.T) {
			want := chainLength(t, root, c.Dir)
			if want == 0 {
				t.Fatalf("в %s (база %s) НЕТ ни одного файла миграции — эталона для сверки не "+
					"существует, и «накат прошёл» здесь означало бы только «бинарь вышел нулём»",
					c.Dir, databaseColumn(c))
			}

			bin, built := bins[c.Point]
			if !built {
				bin = buildApplyPoint(t, root, binDir, c.Point, c.Service)
				bins[c.Point] = bin
			}

			dsn := chainDSN(t, c)
			dbName, _ := migrationchains.DatabaseOf(dsn)

			out, err := runMigrator(t, bin, "up", "--dsn", dsn)
			if err != nil {
				t.Fatalf("накат %s (точка %s, база %s) на пустую базу ОТКАЗАЛ (%v) — это и "+
					"означает «сервис не разворачивается»:\n%s", c.Dir, c.Point, dbName, err, out)
			}
			got := appliedCount(t, dsn)
			t.Logf("  точка %s · Database %s · база в DSN %s · применено %d / объявлено %d",
				c.Point, databaseColumn(c), dbName, got, want)
			if got != want {
				t.Fatalf("накат %s вышел успехом, но применил %d миграций из %d объявленных цепочкой. "+
					"Успех на неполном накате хуже отказа: схема не та, а вердикт зелёный.\n%s",
					chainLabel(c), got, want, out)
			}

			// Повторный накат. Init-контейнер запускается на КАЖДОМ развёртывании, а
			// не однажды, поэтому «применяется дважды» — штатный режим, а не край.
			if out, err := runMigrator(t, bin, "up", "--dsn", dsn); err != nil {
				t.Fatalf("повторный накат %s отказал (%v) — развёртывание на уже накатанной базе "+
					"не пройдёт:\n%s", chainLabel(c), err, out)
			}
			if got := appliedCount(t, dsn); got != want {
				t.Fatalf("повторный накат %s изменил число применённых: %d вместо %d", chainLabel(c), got, want)
			}

			if out, err := runMigrator(t, bin, "status", "--dsn", dsn); err != nil {
				t.Fatalf("status %s отказал на накатанной базе (%v):\n%s", chainLabel(c), err, out)
			}

			// Счёт ведётся ИЗНУТРИ тела, последним оператором: возврат t.Run
			// равен true и на отфильтрованной подпробе.
			proven++
		})
		if !ok {
			failed++
		}
	}

	t.Logf("перепись: точек наката %d, цепочек %d, из них по services/notify %d, накат доказан для %d",
		len(points), len(chains), notify, proven)
	if notify == 0 {
		t.Errorf("по services/notify цепочек 0 — точка наката каталога не видна доказательству")
	}

	switch {
	case failed > 0:
		t.Errorf("накат доказан для %d цепочек из %d, отказали %d — находки выше", proven, len(chains), failed)
	case proven != len(chains):
		t.Errorf("доказано %d из %d при нуле отказов — прогон отфильтрован (-run), и его зелёное "+
			"относится к %d цепочкам, а не к %d. Цель test-pg-outside-selection фильтра не ставит",
			proven, len(chains), proven, len(chains))
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
	service := pointService(pkg)
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
