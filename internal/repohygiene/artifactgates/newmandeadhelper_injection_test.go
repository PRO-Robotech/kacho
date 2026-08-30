// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Доказательство способности гейта «впрыскиваемый помощник имеет вызывающего»
// упасть И смолчать — по двум осям порознь.
//
// Ось I — СУЖДЕНИЕ (`auditDeadInjectedHelpers`): подаётся уже собранная перепись.
// Ось II — РАЗБОР (драйвер на Python): подаётся настоящий генератор с настоящими
// модулями кейсов, и проверяется, что вызов ОТЛИЧИМ от упоминания в прозе.
//
// Две оси нужны обе: судящая функция, получившая неверную перепись, молчит
// «правильно», а разбор, считающий комментарий вызовом, делает её вход всегда
// непустым. Каждая половина зелена при сломанной другой.

func nmDeadReport(scanned int, calls map[string]int) deadHelperReport {
	cross := make(map[string]int, len(calls))
	for k := range calls {
		cross[k] = 0
	}
	return deadHelperReport{Scanned: scanned, Calls: calls, Cross: cross}
}

// Законный близнец: у каждого впрыскиваемого помощника есть вызывающий — МОЛЧИТ.
func TestDeadHelperAllCalledIsSilent(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/probe/tests/newman/scripts/gen.py": nmDeadReport(9, map[string]int{
			"malformed_body_block": 3, "retry_until_present": 1,
		}),
	})
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл предмет там, где каждый помощник вызывается: %v", findings)
	}
	if cen.injected != 2 || cen.dead != 0 || cen.files != 9 {
		t.Fatalf("перепись %+v — ожидалось впрыскиваемых 2, мёртвых 0, модулей 9", cen)
	}
}

// ИНЪЕКЦИЯ: помощник без вызывающего — гейт КРАСНЕЕТ и называет НАБОР и ИМЯ.
// Это ровно та форма, что жила в дереве: таблица впрыска доставляла имя, а
// звать его было некому.
func TestDeadHelperInjectionUncalledHelperIsFound(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/probe/tests/newman/scripts/gen.py": nmDeadReport(9, map[string]int{
			"malformed_body_block": 0, "retry_until_present": 1,
		}),
	})
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел помощника без вызывающего.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "services/probe/tests/newman/scripts/gen.py") {
		t.Fatalf("находка не называет НАБОР — читатель не поймёт, где чинить: %q", findings[0])
	}
	if !strings.Contains(findings[0], "malformed_body_block") {
		t.Fatalf("находка не называет ИМЯ помощника: %q", findings[0])
	}
	if strings.Contains(findings[0], "retry_until_present") {
		t.Fatalf("находка приписала мёртвому близнеца, у которого вызывающий есть: %q", findings[0])
	}
	if cen.dead != 1 {
		t.Fatalf("мёртвых насчитано %d, ожидался 1: без отдельного числа находка\n"+
			"неотличима от переписи", cen.dead)
	}
}

// Мёртвые в РАЗНЫХ наборах называются порознь: один список на всё дерево не
// сказал бы, чей набор чинить.
func TestDeadHelperFindingsAreGroupedBySuite(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/a/tests/newman/scripts/gen.py": nmDeadReport(3, map[string]int{"x": 0}),
		"services/b/tests/newman/scripts/gen.py": nmDeadReport(4, map[string]int{"y": 0}),
	})
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2 — по одной на набор: %v", len(findings), findings)
	}
	if cen.suites != 2 || cen.dead != 2 || cen.files != 7 {
		t.Fatalf("перепись %+v — ожидалось наборов 2, мёртвых 2, модулей 7", cen)
	}
}

// Предпосылка: набор без единого впрыскиваемого имени даёт ноль в переписи, и
// тело гейта на этом отказывает. Без такой ветви гейт, потерявший предмет,
// был бы вечнозелёным.
func TestDeadHelperEmptyInjectionTableIsMeasuredNotSilent(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/probe/tests/newman/scripts/gen.py": nmDeadReport(9, map[string]int{}),
	})
	if len(findings) != 0 {
		t.Fatalf("пустая таблица впрыска — не находка судящей функции: %v", findings)
	}
	if cen.injected != 0 {
		t.Fatalf("перепись впрыскиваемых %d при пустой таблице — величина не различает состояния", cen.injected)
	}
}

// ─── ОСЬ II: РАЗБОР ОТЛИЧАЕТ ВЫЗОВ ОТ УПОМИНАНИЯ ─────────────────────────────
//
// Драйвер читает разобранный Python именно ради этого: имя помощника стоит в
// прозе шапок, в комментариях и в отчётах набора, и предикат по подстроке
// объявил бы объяснение вызовом — то есть молчал бы на мёртвой копии всегда.

const nmDeadGen = `_INJECTED = {
    "alive": alive_placeholder,
}
`

func nmRunDeadDriver(t *testing.T, gen, caseSrc string) deadHelperReport {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 не найден (%v): ось разбора не проверена", err)
	}
	dir := t.TempDir()
	cases := filepath.Join(dir, "cases")
	if err := os.MkdirAll(cases, 0o750); err != nil {
		t.Fatalf("каталог кейсов: %v", err)
	}
	genPath := filepath.Join(dir, "gen.py")
	if err := os.WriteFile(genPath, []byte(gen), 0o600); err != nil {
		t.Fatalf("запись генератора: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cases, "probe.py"), []byte(caseSrc), 0o600); err != nil {
		t.Fatalf("запись модуля кейсов: %v", err)
	}
	out, err := exec.Command(python, filepath.Join(repoRoot(t), deadHelperDriverRel), genPath, cases).Output() // #nosec G204 -- путь из индекса git
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("драйвер переписи не исполнился: %v\n%s", err, stderr)
	}
	var r deadHelperReport
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("разбор вывода драйвера: %v (%s)", err, out)
	}
	return r
}

// Настоящий вызов считается вызовом.
func TestDeadHelperDriverCountsARealCall(t *testing.T) {
	gen := "def helper():\n    return 1\n\n\n" + strings.Replace(nmDeadGen, "alive_placeholder", "helper", 1)
	r := nmRunDeadDriver(t, gen, "CASES = [helper()]\n")
	if r.Calls["helper"] != 1 {
		t.Fatalf("вызовов насчитано %d, ожидался 1: разбор не узнаёт вызов, и тогда\n"+
			"каждый помощник дерева читается как мёртвый", r.Calls["helper"])
	}
}

// Упоминание в прозе и в строке вызовом НЕ считается — иначе мёртвая копия
// объявлялась бы живой своим же комментарием, и гейт молчал бы всегда.
func TestDeadHelperDriverDoesNotCountProse(t *testing.T) {
	gen := "def helper():\n    return 1\n\n\n" + strings.Replace(nmDeadGen, "alive_placeholder", "helper", 1)
	caseSrc := "\"\"\"Модуль кейсов.\n\nЗдесь мог бы стоять helper(), но не стоит.\n\"\"\"\n" +
		"# helper() тоже упоминается комментарием\n" +
		"NOTE = \"helper()\"\n" +
		"CASES = []\n"
	r := nmRunDeadDriver(t, gen, caseSrc)
	if r.Calls["helper"] != 0 {
		t.Fatalf("вызовов насчитано %d при нуле настоящих: разбор судит СЛОВО, а не то,\n"+
			"что код делает — мёртвая копия объявила бы себя живой собственной прозой",
			r.Calls["helper"])
	}
	if r.Scanned != 1 {
		t.Fatalf("прочитано модулей %d, ожидался 1 — «ноль вызовов» обязано быть отличимо\n"+
			"от «ноль прочитанного»", r.Scanned)
	}
}

// Ссылка ЗНАЧЕНИЕМ вызовом не считается: таблица впрыска доставляет имя, но не
// потребляет его. Без этой ветви каждый впрыскиваемый помощник считался бы
// живым by construction — то есть гейт был бы вакуумным.
func TestDeadHelperDriverDoesNotCountAValueReference(t *testing.T) {
	gen := "def helper():\n    return 1\n\n\n" + strings.Replace(nmDeadGen, "alive_placeholder", "helper", 1)
	r := nmRunDeadDriver(t, gen, "HOOK = helper\nCASES = []\n")
	if r.Calls["helper"] != 0 {
		t.Fatalf("ссылка значением засчитана вызовом (%d) — тогда сама таблица впрыска\n"+
			"делает любой помощник живым, и гейт не может покраснеть никогда",
			r.Calls["helper"])
	}
}

// ─── ОСЬ III: МЕЖНАБОРНЫЙ ВЫЗЫВАЮЩИЙ — ТОЖЕ ВЫЗЫВАЮЩИЙ ───────────────────────
//
// Форма, которой распознаватель не знал, увела предмет не в находку и не в
// молчание, а в невидимость: помощник без вызывающего в СВОЁМ наборе был назван
// мёртвым и снят, а его звала проба стойкости сериализатора из соседнего набора —
// та упала. Инъекция снимает РОВНО новое свойство: своя полоса у имени как была
// нулевой, так и осталась, меняется только чужая.

// Помощник, которого зовёт только чужой набор, находкой НЕ является.
func TestDeadHelperCrossSuiteCallerCountsAsACaller(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/probe/tests/newman/scripts/gen.py": {
			Scanned: 9, CrossScanned: 4,
			Calls: map[string]int{"assert_op_error_oneof": 0},
			Cross: map[string]int{"assert_op_error_oneof": 2},
		},
	})
	if len(findings) != 0 {
		t.Fatalf("гейт объявил мёртвым помощник, которого зовут из чужого набора —\n"+
			"снятие такого уронило межнаборную пробу: %v", findings)
	}
	if cen.crossOnly != 1 || cen.dead != 0 {
		t.Fatalf("перепись %+v — ожидалось «только межнаборно» 1, «мертво вовсе» 0:\n"+
			"без отдельного числа слепота полосы растворяется в сумме", cen)
	}
	if cen.crossFiles != 4 {
		t.Fatalf("межнаборных модулей насчитано %d, ожидалось 4", cen.crossFiles)
	}
}

// КОНТРОЛЬ ТРЕТЬИМ ПРОГОНОМ: прежняя половина продолжает краснеть на СВОЁМ
// предмете. Без него молчание прежней проверки неотличимо от её смерти.
func TestDeadHelperNoCallerAnywhereIsStillFound(t *testing.T) {
	findings, cen := auditDeadInjectedHelpers(map[string]deadHelperReport{
		"services/probe/tests/newman/scripts/gen.py": {
			Scanned: 9, CrossScanned: 4,
			Calls: map[string]int{"gone": 0},
			Cross: map[string]int{"gone": 0},
		},
	})
	if len(findings) == 0 {
		t.Fatalf("прежняя половина перестала краснеть: помощник без вызывающего НИГДЕ\n"+
			"объявлен живым.\nперепись: %+v", cen)
	}
	if cen.dead != 1 || cen.crossOnly != 0 {
		t.Fatalf("перепись %+v — ожидалось «мертво вовсе» 1, «только межнаборно» 0:\n"+
			"два числа перестали различать два класса", cen)
	}
}

// Ось разбора: драйвер отличает свою полосу от межнаборной и НЕ складывает их.
func TestDeadHelperDriverSeparatesTheCrossSuiteLane(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 не найден (%v): ось разбора не проверена", err)
	}
	dir := t.TempDir()
	own := filepath.Join(dir, "cases")
	foreign := filepath.Join(dir, "foreign")
	for _, d := range []string{own, foreign} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("каталог %s: %v", d, err)
		}
	}
	gen := "def helper():\n    return 1\n\n\n" + strings.Replace(nmDeadGen, "alive_placeholder", "helper", 1)
	genPath := filepath.Join(dir, "gen.py")
	if err := os.WriteFile(genPath, []byte(gen), 0o600); err != nil {
		t.Fatalf("запись генератора: %v", err)
	}
	if err := os.WriteFile(filepath.Join(own, "probe.py"), []byte("CASES = []\n"), 0o600); err != nil {
		t.Fatalf("запись своего модуля: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "escape_test.py"),
		[]byte("PRODUCERS = [lambda g, t: g.helper()]\n"), 0o600); err != nil {
		t.Fatalf("запись чужого модуля: %v", err)
	}
	out, err := exec.Command(python, filepath.Join(repoRoot(t), deadHelperDriverRel), // #nosec G204 -- путь из индекса git
		genPath, own, "--cross", foreign).Output()
	if err != nil {
		t.Fatalf("драйвер не исполнился: %v", err)
	}
	var r deadHelperReport
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("разбор вывода драйвера: %v (%s)", err, out)
	}
	if r.Calls["helper"] != 0 {
		t.Fatalf("своих вызовов насчитано %d при нуле настоящих — полосы слиты, и\n"+
			"межнаборный вызов маскирует смерть в своём наборе", r.Calls["helper"])
	}
	if r.Cross["helper"] != 1 {
		t.Fatalf("межнаборных вызовов насчитано %d, ожидался 1 — форма `g.helper()`\n"+
			"не узнаётся, и помощник читается как мёртвый", r.Cross["helper"])
	}
	if r.CrossScanned != 1 {
		t.Fatalf("межнаборных модулей прочитано %d, ожидался 1", r.CrossScanned)
	}
}
