// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schema_rollback_has_a_producer_test.go — у отката ОБЯЗАНА быть названная
// процедура, и у неё обязан быть производитель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Мигратор исполняется при КАЖДОМ раскате. Значит штатный откат выкатки вернул
// бы ПРЕЖНИЙ ОБРАЗ НА НОВУЮ СХЕМУ, и ничто этого не отвергало: проверки
// совместимости версии схемы при старте нет. Разрыв тихий — раскат зелёный, под
// поднимается, отказ приходит на первом обращении к колонке, которой в образе
// ещё нет либо в схеме уже нет.
//
// Отдельно и важнее самого разрыва: единственным советом по откату была ОДНА
// СТРОКА `helm rollback`, о схеме не говорившая ничего. Оператор узнавал исход
// ПОСЛЕ отката.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАКОЙ ИСХОД ВЫБРАН И ПОЧЕМУ НЕ ВТОРОЙ
//
// Исходов было два: назвать процедуру и дать ей производителя — либо объявить,
// что отката нет, и сказать это оператору прямо.
//
// Выбран ПЕРВЫЙ. Второй отвергнут замером, а не вкусом: секцию отката несут 349
// миграций из 354, то есть откат схемы в подавляющем большинстве шагов выразим.
// Объявить «отката нет» значило бы отнять у оператора работающий путь там, где
// он есть, и заменить его на неверное утверждение — тот самый класс, который
// корпус ловит.
//
// Производитель поставлен В РЕЦЕПТ РАСКАТКИ (второй из двух вариантов, названных
// задачей: «проверка совместимости схемы при старте ЛИБО шаг отката схемы в
// рецепте»). Первый вариант — страж старта сервиса — требует общего кода и семи
// композиционных корней; он НЕ сделан, и это сказано прямо здесь и в самом
// заявлении, чтобы «заявление есть» не читалось как «проверка есть».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
//  1. у КАЖДОЙ миграции без секции отката судьба объявлена машинно — снятиями в
//     dropguard-манифесте сервиса либо строкой ведомости deploy/schema-rollback.txt.
//     «Оставить как есть» исходом не является: сегодня отсутствие секции
//     НЕОТЛИЧИМО от обратимости;
//  2. каждая строка ведомости имеет ПРЕДМЕТ — миграция существует и секции
//     отката по-прежнему не несёт. Строка без предмета — находка: послабление
//     обязано истекать само;
//  3. рецепт боевой раскатки ПРОИЗВОДИТ заявление до применения и не советует
//     голый откат, не сказав о схеме.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА, НАЗВАННАЯ ЧЕСТНО
//
//   - Проверяется НАЛИЧИЕ И СОГЛАСОВАННОСТЬ заявления, а не то, что откат
//     сработает: живой базы у этой проверки нет и быть не должно.
//   - Совместимость ЗАПУЩЕННОГО образа с applied-версией схемы здесь НЕ
//     проверяется, потому что такого стража в продукте нет. Это остаток, а не
//     обещание, и он назван и в заявлении, и в тексте отказа рецепта.
package deploy_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// rollbackLedger — ведомость решений по миграциям без секции отката.
	rollbackLedger = "schema-rollback.txt"
	// rollbackStatement — производитель заявления, читающий дерево.
	rollbackStatement = "scripts/schema-rollback-statement.py"
	// gooseDown — признак секции отката.
	gooseDown = "+goose Down"
)

var ledgerRow = regexp.MustCompile(`(?m)^([a-z0-9-]+)\|([^|]+)\|(необратима|откат-не-нужен)\|(\S.*)$`)
var migrationVersion = regexp.MustCompile(`^(\d+)_`)

// migrationFacts — цепочки миграций и объявления их обратимости.
type migrationFacts struct {
	total    int
	noDown   []string          // "<владелец>/<файл>" без секции отката
	byDrop   map[string]bool   // из них объявленные снятиями dropguard
	ledger   map[string]string // строка ведомости → вердикт
	present  map[string]bool   // ключи ведомости, у которых предмет ещё есть
	dirs     int
	recipe   string // текст рецепта раскатки
	statePro bool   // рецепт зовёт производителя заявления
	saysSch  bool   // совет по откату говорит о схеме
	before   bool   // заявление производится ДО применения
}

func migrationFactsFromTree(t *testing.T) migrationFacts {
	t.Helper()
	f := migrationFacts{byDrop: map[string]bool{}, ledger: map[string]string{}, present: map[string]bool{}}

	// Каталоги миграций выводятся обходом отслеживаемых файлов: новый сервис
	// попадает под проверку сам, снятый уходит вместе со своим каталогом.
	sqls := trackedFiles(t, "*.sql")
	dirs := map[string]bool{}
	for _, p := range sqls {
		d := filepath.Dir(p)
		if filepath.Base(d) != "migrations" && filepath.Base(filepath.Dir(d)) != "migrations" {
			continue
		}
		dirs[d] = true
	}
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	f.dirs = len(names)

	for _, d := range names {
		owner := migrationOwner(d)
		drops := map[int64]bool{}
		if b, err := os.ReadFile(filepath.Join(repoRoot, d, "dropguard.json")); err == nil { // #nosec G304
			var man struct {
				Drops []struct {
					Version int64 `json:"version"`
				} `json:"drops"`
			}
			if err := json.Unmarshal(b, &man); err != nil {
				t.Fatalf("%s/dropguard.json не разобран: %v", d, err)
			}
			for _, r := range man.Drops {
				drops[r.Version] = true
			}
		}
		entries, err := os.ReadDir(filepath.Join(repoRoot, d))
		if err != nil {
			t.Fatalf("чтение каталога %s: %v", d, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			f.total++
			b, rerr := os.ReadFile(filepath.Join(repoRoot, d, e.Name())) // #nosec G304
			if rerr != nil {
				t.Fatalf("чтение %s/%s: %v", d, e.Name(), rerr)
			}
			if strings.Contains(string(b), gooseDown) {
				continue
			}
			key := owner + "/" + e.Name()
			f.noDown = append(f.noDown, key)
			if m := migrationVersion.FindStringSubmatch(e.Name()); m != nil {
				var v int64
				for _, r := range m[1] {
					v = v*10 + int64(r-'0')
				}
				if drops[v] {
					f.byDrop[key] = true
				}
			}
		}
	}
	sort.Strings(f.noDown)

	lb, err := os.ReadFile(rollbackLedger) // #nosec G304 -- координата дерева
	if err != nil {
		t.Fatalf("ведомость %s не читается (%v) — предмет проверки исчез, "+
			"а не дерево стало чистым", rollbackLedger, err)
	}
	for _, m := range ledgerRow.FindAllStringSubmatch(string(lb), -1) {
		f.ledger[m[1]+"/"+m[2]] = m[3]
	}
	for _, k := range f.noDown {
		if _, ok := f.ledger[k]; ok {
			f.present[k] = true
		}
	}

	rb, err := os.ReadFile(rolloutScript) // #nosec G304 -- координата дерева
	if err != nil {
		t.Fatalf("рецепт раскатки %s не читается: %v", rolloutScript, err)
	}
	f.recipe = string(rb)
	code := stripShellComments(f.recipe)
	f.statePro = strings.Contains(code, rollbackStatement)
	iState := strings.Index(code, rollbackStatement)
	iApply := strings.Index(code, "helm upgrade \"$RELEASE\"")
	f.before = iState >= 0 && iApply > iState

	// Совет по откату обязан сказать о СХЕМЕ. Голый `helm rollback` без этого —
	// ровно то, что стояло здесь до задачи: строка, умалчивающая о миграторе.
	for _, ln := range strings.Split(f.recipe, "\n") {
		if !strings.Contains(ln, "helm rollback") || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		if strings.Contains(ln, "схем") || strings.Contains(ln, "ОБРАЗЫ") {
			f.saysSch = true
		}
	}
	return f
}

func migrationOwner(dir string) string {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) > 1 && parts[0] == "services" {
		return parts[1]
	}
	if len(parts) > 0 && parts[0] == "gateway" {
		return "gateway"
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами.

func scanRollbackProducer(f migrationFacts) []string {
	var out []string
	for _, k := range f.noDown {
		if f.byDrop[k] {
			continue
		}
		if _, ok := f.ledger[k]; ok {
			continue
		}
		out = append(out, fmt.Sprintf(
			"миграция %s не несёт секции отката, и её судьба не объявлена ни dropguard-манифестом, "+
				"ни ведомостью %s. Отсутствие секции сегодня НЕОТЛИЧИМО от обратимости, "+
				"и оператор узнаёт исход ПОСЛЕ отката", k, rollbackLedger))
	}
	keys := make([]string, 0, len(f.ledger))
	for k := range f.ledger {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !f.present[k] {
			out = append(out, fmt.Sprintf(
				"ведомость %s называет %s, а такой миграции без секции отката в дереве больше нет — "+
					"строка пережила свой предмет; послабление обязано истекать само", rollbackLedger, k))
		}
	}
	if !f.statePro {
		out = append(out, fmt.Sprintf(
			"рецепт раскатки не зовёт %s — заявление об откате не производится ничем, "+
				"и откат снова становится действием с неназванным исходом", rollbackStatement))
	} else if !f.before {
		out = append(out, "заявление об откате производится ПОСЛЕ применения — "+
			"оно нужно оператору до него, а не в качестве объяснения уже случившегося")
	}
	if !f.saysSch {
		out = append(out, "совет по откату в рецепте не говорит о СХЕМЕ: голый `helm rollback` "+
			"возвращает образы и оставляет применённые миграции применёнными, "+
			"а умолчание об этом и есть исходный дефект")
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestSchemaRollbackHasAProducer(t *testing.T) {
	f := migrationFactsFromTree(t)

	byLedger := 0
	for _, k := range f.noDown {
		if _, ok := f.ledger[k]; ok && !f.byDrop[k] {
			byLedger++
		}
	}
	t.Logf("осмотрено: каталогов миграций %d, миграций %d; без секции отката %d (%s); "+
		"объявлено снятиями dropguard %d, ведомостью %d; строк ведомости %d; "+
		"рецепт производит заявление %v (до применения %v); совет говорит о схеме %v",
		f.dirs, f.total, len(f.noDown), strings.Join(f.noDown, ", "),
		len(f.byDrop), byLedger, len(f.ledger), f.statePro, f.before, f.saysSch)

	if f.dirs == 0 || f.total == 0 {
		t.Fatalf("миграций не прочитано ни одной (каталогов %d) — обход пуст, "+
			"и это отказ, а не успех: «ноль находок» неотличимо от «ноль прочитанного»", f.dirs)
	}
	if len(f.noDown) == 0 && len(f.ledger) > 0 {
		t.Errorf("миграций без секции отката в дереве ноль, а ведомость несёт %d строк(и) — "+
			"вся ведомость пережила свой предмет", len(f.ledger))
	}

	for _, msg := range scanRollbackProducer(f) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanRollbackProducer_SelfTest(t *testing.T) {
	base := migrationFacts{
		total:    10,
		noDown:   []string{"сервис/0001_снятие.sql", "сервис/0002_заведение.sql"},
		byDrop:   map[string]bool{"сервис/0001_снятие.sql": true},
		ledger:   map[string]string{"сервис/0002_заведение.sql": "откат-не-нужен"},
		present:  map[string]bool{"сервис/0002_заведение.sql": true},
		dirs:     1,
		statePro: true,
		saysSch:  true,
		before:   true,
	}

	// (0) КОНТРОЛЬ: у каждой миграции без секции отката судьба объявлена —
	//     молчание. Миграция С секцией отката в перечень не попадает вовсе, и
	//     это законный близнец: без него отрицание зеленело бы на пустом дереве.
	if got := scanRollbackProducer(base); len(got) != 0 {
		t.Errorf("(0) объявленное дерево обязано молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно исходный дефект: миграция без секции отката, чья
	//     судьба не объявлена нигде.
	silent := base
	silent.noDown = append(append([]string{}, base.noDown...), "сервис/0003_новая.sql")
	got := scanRollbackProducer(silent)
	if len(got) == 0 || !strings.Contains(got[0], "0003_новая.sql") {
		t.Errorf("(A) необъявленная миграция ПРОПУЩЕНА: %v", got)
	}

	// (B) КОНТРОЛЬ ТОЙ ЖЕ ФОРМЫ: та же миграция, но объявленная ведомостью, —
	//     молчание. Без него (A) зеленело бы на любом добавлении.
	twin := silent
	twin.ledger = map[string]string{"сервис/0002_заведение.sql": "откат-не-нужен",
		"сервис/0003_новая.sql": "необратима"}
	twin.present = map[string]bool{"сервис/0002_заведение.sql": true, "сервис/0003_новая.sql": true}
	if got := scanRollbackProducer(twin); len(got) != 0 {
		t.Errorf("(B) объявленная ведомостью миграция обязана молчать: %v", got)
	}

	// (C) ИНЪЕКЦИЯ в обратную сторону: строка ведомости пережила свой предмет
	//     (миграции дописали секцию отката либо её сняли).
	stale := base
	stale.present = map[string]bool{}
	got = scanRollbackProducer(stale)
	if len(got) == 0 || !strings.Contains(got[0], "пережила свой предмет") {
		t.Errorf("(C) строка без предмета ПРОПУЩЕНА: %v", got)
	}

	// (D) ИНЪЕКЦИЯ: рецепт перестал производить заявление.
	mute := base
	mute.statePro = false
	mute.before = false
	if got := scanRollbackProducer(mute); len(got) == 0 {
		t.Errorf("(D) рецепт без производителя заявления ПРОПУЩЕН")
	}

	// (E) ИНЪЕКЦИЯ: заявление производится, но после применения.
	late := base
	late.before = false
	if got := scanRollbackProducer(late); len(got) == 0 {
		t.Errorf("(E) заявление после применения ПРОПУЩЕНО")
	}

	// (F) ИНЪЕКЦИЯ — ровно тот совет, что стоял до задачи: голый `helm rollback`
	//     без единого слова о схеме.
	bare := base
	bare.saysSch = false
	got = scanRollbackProducer(bare)
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "СХЕМЕ") {
		t.Errorf("(F) голый совет по откату ПРОПУЩЕН: %v", got)
	}
}

// TestRollbackPredicates_RecogniseTheRealTree — предпосылки разбора верны на
// настоящем дереве: секция отката узнаётся, ведомость читается, снятия
// dropguard разбираются. Без этого самопроверка выше доказывала бы
// работоспособность ядра на входе, которого не бывает.
func TestRollbackPredicates_RecogniseTheRealTree(t *testing.T) {
	f := migrationFactsFromTree(t)
	if f.total < 100 {
		t.Errorf("миграций прочитано %d — обход сузился: цепочки этого дерева "+
			"измеряются сотнями", f.total)
	}
	// ЗДЕСЬ СТОЯЛИ ТРИ ПРОВЕРКИ, ПАДАВШИЕ НА ДОСТИЖЕНИИ СВОЕЙ ЦЕЛИ.
	//
	// Они требовали от НАСТОЯЩЕГО дерева нести хотя бы одну миграцию без секции
	// отката, хотя бы одну объявленную снятиями и хотя бы одну строку ведомости —
	// иначе объявляли разбор ослепшим. Замысел верен: самопроверка ниже гоняет
	// ядро на синтетике, и без доказательства, что предпосылки разбора верны и на
	// живом дереве, она доказывала бы работоспособность на входе, которого не
	// бывает.
	//
	// Неверен был ВЫБОР ДОКАЗАТЕЛЬСТВА. Дерево без единой миграции без отката —
	// это не ослепший разбор, а ЦЕЛЬ ведомости: пустая ведомость и есть то
	// состояние, ради которого она заведена. Проверка, краснеющая на нём,
	// подталкивает держать строку ради зелёного — то есть ровно к тому, что
	// ведомость запрещает.
	//
	// Наступило это состояние 2026-09-04 со сведением цепочки iam: её
	// `0078_interactive_clients.sql` была ПОСЛЕДНЕЙ миграцией дерева без секции
	// отката, и свод унёс её вместе со всей историей. По всем десяти каталогам
	// стало ноль.
	//
	// Способность разбора узнавать предмет доказывается теперь СИНТЕТИКОЙ —
	// TestRollbackRecognisers_OnSyntheticInput ниже, — и это доказательство
	// сильнее прежнего: оно не зависит от того, есть ли в дереве дефект.
	if f.dirs < 5 {
		t.Errorf("каталогов миграций прочитано %d — обход сузился: их в этом дереве "+
			"десяток, и на меньшем числе «ноль без отката» сказано о части", f.dirs)
	}
	t.Logf("предпосылки: миграций %d, без секции отката %d, объявлено dropguard %d, "+
		"строк ведомости %d", f.total, len(f.noDown), len(f.byDrop), len(f.ledger))
}

// TestRollbackRecognisers_OnSyntheticInput — разбор узнаёт предмет и НЕ узнаёт
// того, чего нет.
//
// Заведена взамен трёх предпосылок, требовавших дефекта от настоящего дерева
// (см. разбор выше). Синтетика доказывает то же свойство и не перестаёт его
// доказывать, когда дерево становится чистым.
func TestRollbackRecognisers_OnSyntheticInput(t *testing.T) {
	// (А) секция отката: узнаётся её наличие И её отсутствие.
	withDown := "-- +goose Up\nCREATE TABLE t ();\n-- +goose Down\nDROP TABLE t;\n"
	noDown := "-- +goose Up\nCREATE TABLE t ();\n"
	if !strings.Contains(withDown, gooseDown) {
		t.Errorf("(А) признак %q не узнан в миграции, которая его несёт", gooseDown)
	}
	if strings.Contains(noDown, gooseDown) {
		t.Errorf("(А) признак %q узнан в миграции, которая его НЕ несёт", gooseDown)
	}

	// (Б) строка ведомости: разбирается годная и отвергается негодная.
	good := "iam|0078_interactive_clients.sql|откат-не-нужен|Миграция ЗАВОДИТ таблицу и ничего не снимает."
	rows := ledgerRow.FindAllStringSubmatch(good, -1)
	if len(rows) != 1 || rows[0][1] != "iam" || rows[0][2] != "0078_interactive_clients.sql" {
		t.Errorf("(Б) годная строка ведомости не разобрана: %v", rows)
	}
	for _, bad := range []string{
		"iam|0078.sql|вердикт-которого-нет|повод", // вердикт вне закрытого словаря
		"iam|0078.sql|необратима|",                // повод пуст
		"|0078.sql|необратима|повод",              // владелец пуст
	} {
		if m := ledgerRow.FindAllStringSubmatch(bad, -1); len(m) != 0 {
			t.Errorf("(Б) негодная строка %q принята: %v", bad, m)
		}
	}

	// (В) номер версии из имени файла — им разбор сводит миграцию с записью
	// снятия в её манифесте.
	for name, want := range map[string]string{
		"0078_interactive_clients.sql":     "0078",
		"20260901113757_rule_segments.sql": "20260901113757",
		"484002_account_quota.sql":         "484002",
	} {
		m := migrationVersion.FindStringSubmatch(name)
		if m == nil || m[1] != want {
			t.Errorf("(В) версия из %q разобрана как %v, ожидалось %q", name, m, want)
		}
	}
	if migrationVersion.FindStringSubmatch("readme.sql") != nil {
		t.Error("(В) имя без номера принято за версию — разбор шире предмета")
	}
}
