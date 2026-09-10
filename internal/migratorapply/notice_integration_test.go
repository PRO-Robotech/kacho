// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notice_integration_test.go — то, что сервер СКАЗАЛ во время наката, доезжает до
// оператора.
//
// # Предмет
//
// Миграция вправе объявить наблюдаемым то, чего оператор не увидит ни при каком
// вводе. `RAISE NOTICE` / `RAISE WARNING` внутри миграции библиотека отдаёт
// клиенту ТОЛЬКО при заданном обработчике уведомлений; без него сообщение
// существует, выглядит работающим и не видно НИКОМУ. В поставке накат исполняет
// init-контейнер, и его вывод — единственный путь оператора (#2544).
//
// # Почему бинарь, а не вызов функции
//
// Ровно потому же, почему им доказывается сам накат (см. doc.go): проба, зовущая
// функцию, доказала бы ту половину тракта, до которой дотянулась. Здесь предмет —
// ВЫВОД ПРОЦЕССА, то есть то самое, что читает оператор, поэтому доказательство
// обязано читать вывод процесса.
//
// # Обе половины, и вторая — НЕ «молчаливая цепочка»
//
// Положительная: цепочка, поднимающая уведомление безусловно, обязана показать
// его текст. Вторая половина сперва была задумана как молчаливая цепочка
// (обработчик стоит, сказать нечего) — и ЗАМЕР ЭТО ОПРОВЕРГ: молчаливых цепочек
// в дереве нет вовсе. Уведомления поднимает не только `RAISE`; их поднимает сам
// Postgres на `DROP … IF EXISTS` и `CREATE … IF NOT EXISTS` («constraint … does
// not exist, skipping»), а таких операторов в цепочках от 29 до 332 (предикат —
// в шапке [NoticeLimit] общего пакета). Предикат, считавший только `RAISE`, был
// слеп к самой многочисленной форме.
//
// Поэтому вторая половина сменила предмет и стала СИЛЬНЕЕ: она требует, чтобы
// перепись СЧИТАЛА напечатанное, а не печатала константу. Такое утверждение не
// зависит от того, разговорчива цепочка или нет, и его нельзя удовлетворить
// литералом.
//
// Половина «сказать о самом молчании» осталась там, где ей место и где она
// исполнима без базы, — `pkg/migratorcli.TestCensusIsPrintedEvenWhenNothingWasSaid`.
package migratorapply_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

const (
	// noticeProducerService — служба, чья цепочка поднимает уведомление БЕЗУСЛОВНО.
	noticeProducerService = "vpc"
	// noticeProducerFile — сам производитель. Назван координатой, чтобы проба
	// ИСТЕКАЛА вместе с ним: исчезнет производитель — проба откажет и назовёт
	// причину, а не позеленеет на утверждении, которому нечего утверждать.
	noticeProducerFile = "services/vpc/internal/migrations/0042_quota_usage_seeded_from_rows.sql"
	// noticeProducerRaise — оператор RAISE, стоящий в производителе. Проба сверяет
	// именно его: файл мог остаться, а печать из него уйти.
	noticeProducerRaise = "RAISE NOTICE 'quota usage backfill:"
	// noticeProducerText — то, что обязан увидеть оператор.
	noticeProducerText = "quota usage backfill:"

	// censusService — служба, на выводе которой сверяется перепись. Взята самая
	// короткая цепочка дерева: предмет второй половины — арифметика переписи, а
	// не длина цепочки, и платить за длинную незачем.
	censusService = "geo"

	// noticeCensusMarker — начало переписи. Она печатается ВСЕГДА: без неё «ноль
	// доставленных» неотличимо от «обработчика нет».
	noticeCensusMarker = "migration-notices"
)

// noticePointOf — точка наката службы среди выведенных из дерева. Возвращает
// пустую строку, если такой службы в дереве нет: перечень выводится, а не
// выписывается, и его расхождение с этой пробой обязано быть отказом.
func noticePointOf(points []string, service string) string {
	for _, pkg := range points {
		if svc, _ := migrationsDirOf(pkg); svc == service {
			return pkg
		}
	}
	return ""
}

// noticeCensus — числа переписи, вычитанные из вывода наката.
var noticeCensusRe = regexp.MustCompile(
	`migration-notices (\S+): (\d+) delivered, (\d+) suppressed`)

// readNoticeCensus достаёт из вывода процесса строку переписи и её числа.
//
// Разбором, а не подстрокой: предмет второй половины — ЧИСЛО, и проба, ищущая
// слово «delivered», зеленела бы на переписи, печатающей константу.
func readNoticeCensus(t *testing.T, out string) (service string, delivered, suppressed int) {
	t.Helper()
	m := noticeCensusRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("переписи уведомлений в выводе нет — «ноль доставленных» неотличимо от "+
			"«обработчика нет вовсе».\n--- вывод наката ---\n%s", out)
	}
	d, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("число доставленных неразбираемо (%q): %v", m[2], err)
	}
	sup, err := strconv.Atoi(m[3])
	if err != nil {
		t.Fatalf("число отброшенных неразбираемо (%q): %v", m[3], err)
	}
	return m[1], d, sup
}

// countDeliveredNotices — сколько уведомлений НАПЕЧАТАНО в выводе.
//
// Считаются строки, начинающиеся уровнем, — той самой формой, которую видит
// оператор. Подробности (`  DETAIL:` / `  HINT:`) стоят с отступом и в счёт не
// идут: они принадлежат уже посчитанному уведомлению, а не новому.
func countDeliveredNotices(out string) int {
	levels := []string{"NOTICE: ", "WARNING: ", "INFO: ", "LOG: ", "DEBUG: "}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		for _, lv := range levels {
			if strings.HasPrefix(line, lv) {
				n++
				break
			}
		}
	}
	return n
}

// TestOperatorSeesWhatTheServerSaidDuringTheChain — ПОЛОЖИТЕЛЬНАЯ половина.
//
// Настоящий бинарь точки наката, настоящая пустая база, настоящая цепочка. Текст,
// который миграция объявила наблюдаемым, обязан оказаться в выводе процесса.
func TestOperatorSeesWhatTheServerSaidDuringTheChain(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)

	// ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ, А НЕ ПОДРАЗУМЕВАЕТСЯ. Утверждение ниже стоит ровно
	// на том, что производитель в дереве есть и печатает. Уедет он — здесь будет
	// отказ с именем, а не зелёное на пустом месте.
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(noticeProducerFile)))
	if err != nil {
		t.Fatalf("производитель уведомления %s не прочитан (%v). Проба без производителя "+
			"утверждала бы о печати, которой никто не делает", noticeProducerFile, err)
	}
	if !strings.Contains(string(b), noticeProducerRaise) {
		t.Fatalf("в %s больше нет оператора %q — производителя нет, и утверждение ниже стало бы "+
			"вакуумным. Назовите другого производителя либо снимите пробу вместе с предметом",
			noticeProducerFile, noticeProducerRaise)
	}

	points := applyPoints(t, root)
	pkg := noticePointOf(points, noticeProducerService)
	if pkg == "" {
		t.Fatalf("точки наката %s нет среди %d выведенных из дерева — проба и дерево разошлись",
			noticeProducerService, len(points))
	}

	bin := buildApplyPoint(t, root, t.TempDir(), pkg, noticeProducerService)
	dsn := pgtest.NewEmptyDB(t)

	out, err := runMigrator(t, bin, "up", "--dsn", dsn)
	if err != nil {
		t.Fatalf("накат %s на пустую базу ОТКАЗАЛ (%v):\n%s", noticeProducerService, err, out)
	}

	if !strings.Contains(out, noticeProducerText) {
		t.Errorf("оператор НЕ ВИДИТ того, что сервер сказал во время наката: в выводе нет %q. "+
			"Миграция %s печатает это безусловно, значит сообщение существует и не доезжает "+
			"ни до кого.\n--- вывод наката ---\n%s", noticeProducerText, noticeProducerFile, out)
	}
	if !strings.Contains(out, "NOTICE:") {
		t.Errorf("вывод не называет УРОВЕНЬ сообщения: уведомление сервера и его отказ читаются "+
			"по-разному, и без уровня оператор не отличит одно от другого.\n%s", out)
	}
	// Перепись печатается пробой ВСЕГДА — «ноль находок» обязано быть отличимо
	// от «ноль прочитанного», и на зелёном прогоне это единственное место, где
	// видно, сколько сервер сказал на самом деле.
	svc, delivered, suppressed := readNoticeCensus(t, out)
	t.Logf("перепись наката %s: служба %q, доставлено %d, отброшено %d, предел не тронут",
		noticeProducerService, svc, delivered, suppressed)
	if delivered == 0 {
		t.Errorf("перепись говорит «0 delivered» на цепочке, которая печатает безусловно — "+
			"счёт разошёлся с доставкой.\n%s", out)
	}
}

// TestTheCensusCountsWhatTheOperatorActuallySaw — вторая половина.
//
// Перепись обязана быть ПРОИЗВОДНОЙ от напечатанного, а не литералом. Проверяется
// сравнением двух величин, снятых из одного и того же вывода: числа переписи и
// числа строк, начинающихся уровнем.
//
// Положительный контроль стоит рядом и обязателен: сравнение `0 == 0` истинно на
// переписи, которая не считает ничего, поэтому напечатанных обязано быть БОЛЬШЕ
// НУЛЯ. Перестанет цепочка говорить — проба откажет и скажет, что близнец надо
// выбрать заново, а не позеленеет на пустоте.
func TestTheCensusCountsWhatTheOperatorActuallySaw(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	points := applyPoints(t, root)
	pkg := noticePointOf(points, censusService)
	if pkg == "" {
		t.Fatalf("точки наката %s нет среди %d выведенных из дерева — проба и дерево разошлись",
			censusService, len(points))
	}

	bin := buildApplyPoint(t, root, t.TempDir(), pkg, censusService)
	dsn := pgtest.NewEmptyDB(t)

	out, err := runMigrator(t, bin, "up", "--dsn", dsn)
	if err != nil {
		t.Fatalf("накат %s на пустую базу ОТКАЗАЛ (%v):\n%s", censusService, err, out)
	}

	service, delivered, suppressed := readNoticeCensus(t, out)
	printed := countDeliveredNotices(out)
	t.Logf("перепись наката %s: строка называет службу %q, доставлено %d, отброшено %d; "+
		"строк с уровнем в выводе %d", censusService, service, delivered, suppressed, printed)

	if service != censusService {
		t.Errorf("перепись называет службу %q вместо %q — вывод нескольких init-контейнеров "+
			"читается вместе, и строка, назвавшая чужое имя, адресована никому", service, censusService)
	}
	if printed == 0 {
		t.Fatalf("цепочка %s не сказала НИЧЕГО — сравнение переписи с напечатанным стало бы "+
			"«0 == 0», то есть истинным на переписи, которая не считает ничего. Назовите "+
			"другого производителя.\n%s", censusService, out)
	}
	if delivered != printed {
		t.Errorf("перепись говорит «%d delivered», а строк с уровнем напечатано %d — "+
			"перепись не производна от напечатанного, то есть считает не то, что видит "+
			"оператор.\n%s", delivered, printed, out)
	}
	if suppressed != 0 {
		t.Errorf("отброшено %d при пределе, до которого этой цепочке далеко — счёт неверен", suppressed)
	}
}

// TestEveryApplyPointOfThisModuleDeliversNotices — «все семь точек или одна»:
// ответ ЗАМЕРЕН, а не выведен.
//
// Соединение поднимает общий пакет, и из этого следует, что правка закрывает все
// точки наката разом. Следует — но только для тех, кто собирается ИЗ ЭТОГО
// ДЕРЕВА. `services/iam` объявляет собственный Go-модуль и тянет платформу
// ПИНОМ, поэтому его бинарь собирается из ревизии, которой правки ещё нет: до
// бампа пина уведомления там теряются ровно как раньше.
//
// Проба это не обходит и не прячет: точки чужих модулей называются поимённо в
// переписи и в счёт проверенных не идут. Появится седьмая — перепись изменится
// сама, потому что состав выводится из дерева, а не выписан.
func TestEveryApplyPointOfThisModuleDeliversNotices(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	points := applyPoints(t, root)
	if len(points) == 0 {
		t.Fatal("точек наката НЕ НАЙДЕНО — обход пуст, доказывать нечего")
	}

	binDir := t.TempDir()
	var proven int
	var foreign []string

	for _, pkg := range points {
		service, _ := migrationsDirOf(pkg)
		moduleDir, _ := moduleOfPoint(t, root, pkg)
		if moduleDir != root {
			foreign = append(foreign, service+" (модуль "+filepath.ToSlash(strings.TrimPrefix(moduleDir, root+string(filepath.Separator)))+")")
			continue
		}

		ok := t.Run(service, func(t *testing.T) {
			bin := buildApplyPoint(t, root, binDir, pkg, service)
			dsn := pgtest.NewEmptyDB(t)

			out, err := runMigrator(t, bin, "up", "--dsn", dsn)
			if err != nil {
				t.Fatalf("накат %s на пустую базу ОТКАЗАЛ (%v):\n%s", service, err, out)
			}
			named, delivered, suppressed := readNoticeCensus(t, out)
			t.Logf("%s: доставлено %d, отброшено %d", service, delivered, suppressed)
			if named != service {
				t.Errorf("перепись называет службу %q вместо %q — вывод нескольких "+
					"init-контейнеров читается вместе", named, service)
			}
		})
		if ok {
			// Счёт ведётся ПОСЛЕ подпробы и по её исходу: возврат t.Run равен
			// true и на ОТФИЛЬТРОВАННОЙ подпробе, но тогда сюда не дойдёт ни
			// одна — фильтр отсекает её целиком вместе с этой строкой.
			proven++
		}
	}

	t.Logf("перепись: точек наката %d, своего модуля %d, доставка доказана для %d, "+
		"вне модуля %d: %s", len(points), len(points)-len(foreign), proven, len(foreign),
		strings.Join(foreign, ", "))

	if proven == 0 {
		t.Fatal("доставка не доказана НИ ДЛЯ ОДНОЙ точки наката своего модуля — " +
			"вердикт беспредметен")
	}
	if want := len(points) - len(foreign); proven != want {
		t.Errorf("доказано %d из %d точек своего модуля — вердикт относится к доказанным, "+
			"и молчание об этом сделало бы частичный прогон неотличимым от полного", proven, want)
	}
}
