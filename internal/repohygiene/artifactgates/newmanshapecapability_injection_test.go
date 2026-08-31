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

// Доказательство способности гейта «у каждой принимаемой формы есть вызывающий»
// упасть И смолчать — по двум осям порознь.
//
// Ось I — РАЗБОР (драйвер на Python): подаётся настоящий исходник, и проверяется,
// что распознаватель отличает ВОЗМОЖНОСТЬ от СТРАЖА, раскрывает цепочку имён
// (`partial` → таблица впрыска) и не считает вызовом упоминание в прозе.
// Ось II — СУЖДЕНИЕ (`auditShapeCapabilities`): подаётся уже собранная перепись.
//
// Обе нужны: распознаватель, объявивший стража возможностью, делает вход
// судящей функции всегда непустым; судящая функция, молчащая на нуле вызовов,
// зелена при исправном разборе. Каждая половина зелена при сломанной другой.
//
// ОСИ ВНУТРИ КАЖДОЙ ПОЛОВИНЫ РАЗВЕДЕНЫ. Инъекция, роняющая сразу «мёртвая
// форма», «неизвестная форма» и «сдвинутый индекс», оставила бы незамеченным
// распознаватель, знающий только одну из трёх.

// ─── ось I: РАЗБОР ──────────────────────────────────────────────────────────

// nmShapeDriver — запуск настоящего драйвера на подготовленных исходниках.
func nmShapeDriver(t *testing.T, declFiles []string, callDirs []string) shapeCapabilityReport {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 не найден (%v): разбор не исполним, и это «не выполнилось», а не согласие", err)
	}
	args := append([]string{filepath.Join(repoRoot(t), shapeCapabilityDriverRel), "--decl"}, declFiles...)
	args = append(args, "--calls")
	args = append(args, callDirs...)
	cmd := exec.Command(python, args...) // #nosec G204 -- путь из индекса git этого модуля
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("драйвер не исполнился (%v)\n%s", err, stderr)
	}
	var r shapeCapabilityReport
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("разбор вывода драйвера: %v\n%s", err, out)
	}
	return r
}

// nmShapeTree раскладывает синтетический набор: общий слой, генератор набора и
// модули кейсов. Возвращает (файлы объявлений, каталоги вызывающих).
func nmShapeTree(t *testing.T, shared, gen string, cases map[string]string) ([]string, []string) {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "kacholib")
	scripts := filepath.Join(root, "suite", "scripts")
	casesDir := filepath.Join(root, "suite", "cases")
	for _, d := range []string{lib, scripts, casesDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("синтетическое дерево: %v", err)
		}
	}
	write := func(p, body string) {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", p, err)
		}
	}
	sharedPath := filepath.Join(lib, "gen_shared.py")
	genPath := filepath.Join(scripts, "gen.py")
	write(sharedPath, shared)
	write(genPath, gen)
	for name, body := range cases {
		write(filepath.Join(casesDir, name+".py"), body)
	}
	return []string{sharedPath, genPath}, []string{casesDir, scripts}
}

// nmCapabilitySrc — общий слой в той форме, в какой он лежит в дереве:
// многоимённое ожидание принимает и строку, и перечень.
const nmCapabilitySrc = `
def retry_until_present(step, id_env_var, budget, interval_ms, ledger=None):
    """Ждать появления СВОИХ свежих строк в списке."""
    want = [id_env_var] if isinstance(id_env_var, str) else list(id_env_var)
    return want
`

// nmGuardSrc — ЗАКОННЫЙ БЛИЗНЕЦ: та же проверка типа, но вторая ветвь ОТВЕРГАЕТ.
// Второй принимаемой формы у стража нет, и требовать для неё вызывающего значило
// бы требовать вызова, который обязан упасть.
const nmGuardSrc = `
def js_name(value, where):
    """Имя, годное в позицию ключа."""
    if not isinstance(value, str) or value == "":
        raise ValueError(f"{where}: имя пусто")
    return value
`

// nmGenSrc — генератор набора: имя доезжает до модулей кейсов ЦЕПОЧКОЙ —
// импорт, привязка окна `functools.partial`, запись в таблицу впрыска.
const nmGenSrc = `
import functools
from kacholib.gen_shared import retry_until_present

_rup = functools.partial(retry_until_present, budget=25, interval_ms=500)

_INJECTED = {
    "retry_until_present": _rup,
}
`

// ─── РАЗБОР: возможность отличима от стража ─────────────────────────────────

func TestShapeCapabilityParserTellsCapabilityFromGuard(t *testing.T) {
	// Дискриминатор — что делает ВТОРАЯ ВЕТВЬ, а не что стоит в условии: в обоих
	// исходниках стоит один и тот же `isinstance(x, str)`. Распознаватель,
	// судящий по условию, объявил бы стража возможностью и потребовал бы для
	// отказа вызывающего.
	decls, dirs := nmShapeTree(t, nmCapabilitySrc+nmGuardSrc, nmGenSrc, map[string]string{
		"probe": "retry_until_present(None, 'oneId')\n",
	})
	r := nmShapeDriver(t, decls, dirs)

	kinds := map[string]string{}
	for _, s := range r.Subjects {
		kinds[s.Func] = s.Kind
	}
	if kinds["retry_until_present"] != "capability" {
		t.Fatalf("возможность распознана как %q — тогда мёртвая форма была бы невидима", kinds["retry_until_present"])
	}
	if kinds["js_name"] != "guard" {
		t.Fatalf("страж распознан как %q — гейт требовал бы вызывающего для ОТКАЗА", kinds["js_name"])
	}
	if len(r.Subjects) != 2 {
		t.Fatalf("перепись: предметов %d, ожидалось 2 — распознаватель видит не то, о чём судит", len(r.Subjects))
	}
}

// ─── РАЗБОР: цепочка имён РАСКРЫВАЕТСЯ, а прозу за вызов не считает ─────────

func TestShapeCapabilityParserResolvesTheAliasChainAndIgnoresProse(t *testing.T) {
	// Вызов в модуле кейсов идёт по имени ИЗ ТАБЛИЦЫ ВПРЫСКА, а не по имени
	// объявления, и приезжает туда через `functools.partial`. Распознаватель,
	// знающий только имя объявления, дал бы ноль вызовов у ЖИВОЙ формы — то есть
	// ложную находку, и гейт сняли бы первым.
	//
	// Рядом — проза: то же имя в комментарии и в строке документации. Она
	// вызовом не является, и предикат по подстроке считал бы объяснение вызовом.
	decls, dirs := nmShapeTree(t, nmCapabilitySrc, nmGenSrc, map[string]string{
		"lists": `"""Ожидание берётся retry_until_present (общий слой)."""
# retry_until_present ждёт появления ВСЕХ названных имён
retry_until_present(None, ['a', 'b'])
`,
		"single": "retry_until_present(None, 'oneId')\n",
	})
	r := nmShapeDriver(t, decls, dirs)

	if len(r.Subjects) != 1 {
		t.Fatalf("предметов %d, ожидался 1", len(r.Subjects))
	}
	s := r.Subjects[0]
	if !contains(s.Aliases, "_rup") {
		t.Fatalf("цепочка имён не раскрыта: %v — вызовы через таблицу впрыска остались бы невидимы", s.Aliases)
	}
	if s.Shapes["seq"] != 1 || s.Shapes["str"] != 1 {
		t.Fatalf("перепись форм не сошлась: %v — ожидалось по одному вызову каждой формы "+
			"(проза вызовом не считается)", s.Shapes)
	}
}

// ─── РАЗБОР: привязка ПОЗИЦИОННОГО аргумента названа, а не проглочена ───────

func TestShapeCapabilityParserNamesTheShiftedIndex(t *testing.T) {
	// `functools.partial(f, X)` съедает первый позиционный, и индекс проверяемого
	// параметра сдвигается. Перепись, не знающая об этом, считала бы форму НЕ ТОГО
	// аргумента — то есть отвечала бы уверенно и неверно.
	gen := `
import functools
from kacholib.gen_shared import retry_until_present

_rup = functools.partial(retry_until_present, None)

_INJECTED = {"retry_until_present": _rup}
`
	decls, dirs := nmShapeTree(t, nmCapabilitySrc, gen, map[string]string{
		"probe": "retry_until_present('oneId', 25, 500)\n",
	})
	r := nmShapeDriver(t, decls, dirs)
	if len(r.Subjects) != 1 {
		t.Fatalf("предметов %d, ожидался 1", len(r.Subjects))
	}
	if len(r.Subjects[0].Shifted) == 0 {
		t.Fatal("сдвиг позиционного аргумента не назван: перепись считала бы форму не того аргумента")
	}
}

// ─── РАЗБОР: форма проверки, которой он НЕ ЗНАЕТ, названа отдельно ──────────

func TestShapeCapabilityParserNamesTheFormItDoesNotKnow(t *testing.T) {
	// Проверка типа вне `if`/`if-else` не попадает ни в возможности, ни в стражи.
	// Молчание здесь означало бы невидимость — а она хуже находки: ни красного,
	// ни зелёного.
	src := `
def odd(step, id_env_var):
    flag = isinstance(id_env_var, str)
    return flag
`
	decls, dirs := nmShapeTree(t, src, "", map[string]string{"probe": "odd(None, 'x')\n"})
	r := nmShapeDriver(t, decls, dirs)
	if len(r.Subjects) != 1 || r.Subjects[0].Kind != "unknown-form" {
		t.Fatalf("форма вне известных разбору не названа: %+v", r.Subjects)
	}
}

// ─── ось II: СУЖДЕНИЕ ───────────────────────────────────────────────────────

func nmShapeReport(subs ...shapeSubject) shapeCapabilityReport {
	return shapeCapabilityReport{Generators: 9, Files: 136, Subjects: subs}
}

func nmCapability(shapes map[string]int) shapeSubject {
	return shapeSubject{
		File: "tests/newman/kacholib/gen_shared.py", Func: "retry_until_present",
		Param: "id_env_var", Kind: "capability", Line: 1703, Index: 1,
		Aliases: []string{"_rup", "retry_until_present"}, Shapes: shapes,
	}
}

// ИНЪЕКЦИЯ: воспроизведено СОСТОЯНИЕ ДЕРЕВА до сведения форка — форма со
// списком имён не звалась ни разу. Гейт обязан покраснеть и назвать ФОРМУ.
//
// Числа взяты с ревизии `56772d353^` замером тем же драйвером (генераторов 8,
// модулей 132, формы: строка 28, перечень 0) — то есть инъекция повторяет не
// выдуманный, а бывший вход. Предикат повторения назван в шапке гейта.
func TestShapeCapabilityInjectionDeadFormIsFound(t *testing.T) {
	findings, cen := auditShapeCapabilities(nmShapeReport(
		nmCapability(map[string]int{"str": 28, "seq": 0, "unknown": 0})))

	if len(findings) != 1 {
		t.Fatalf("мёртвая форма не найдена: %v\nперепись: %+v", findings, cen)
	}
	if !strings.Contains(findings[0], shapeNames["seq"]) {
		t.Fatalf("находка есть, но не называет ФОРМУ: %q", findings[0])
	}
	if !strings.Contains(findings[0], "retry_until_present(id_env_var)") {
		t.Fatalf("находка не называет координату: %q", findings[0])
	}
	if cen.capabilities != 1 || cen.shapesTotal != 2 || cen.shapesSeen != 1 {
		t.Fatalf("перепись не сошлась: %+v — ожидалось возможностей 1, форм 2, со своим вызывающим 1", cen)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: обе формы зовут — МОЛЧИТ, и перепись подтверждает, что
// предмет был осмотрен. Без этой пробы молчание было бы неотличимо от слепоты.
func TestShapeCapabilityBothFormsCalledIsSilent(t *testing.T) {
	findings, cen := auditShapeCapabilities(nmShapeReport(
		nmCapability(map[string]int{"str": 33, "seq": 1, "unknown": 0})))

	if len(findings) != 0 {
		t.Fatalf("живая возможность объявлена находкой: %v", findings)
	}
	if cen.capabilities != 1 || cen.shapesSeen != 2 {
		t.Fatalf("перепись: возможность не осмотрена (%+v) — тогда молчание означало бы слепоту", cen)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ: СТРАЖ вызывающего второй формы не требует — МОЛЧИТ.
// Ось разведена: у стража нули по обеим формам, и гейт, судящий по числам без
// рода, дал бы здесь ДВЕ ложные находки.
func TestShapeCapabilityGuardNeedsNoCallerOfTheRefusedForm(t *testing.T) {
	guard := shapeSubject{
		File: "services/iam/tests/newman/scripts/gen.py", Func: "js_name", Param: "value",
		Kind: "guard", Line: 167, Index: 0, Aliases: []string{"js_name"},
		Shapes: map[string]int{"str": 0, "seq": 0, "unknown": 11},
	}
	findings, cen := auditShapeCapabilities(nmShapeReport(guard))
	if len(findings) != 0 {
		t.Fatalf("страж объявлен возможностью без вызывающего: %v", findings)
	}
	if cen.guards != 1 || cen.capabilities != 0 || cen.shapesTotal != 0 {
		t.Fatalf("перепись родов не сошлась: %+v", cen)
	}
}

// ИНЪЕКЦИЯ: неизвестная разбору форма — находка, а не молчание.
func TestShapeCapabilityInjectionUnknownFormIsFound(t *testing.T) {
	s := nmCapability(map[string]int{"str": 1, "seq": 1, "unknown": 0})
	s.Kind = "unknown-form"
	findings, cen := auditShapeCapabilities(nmShapeReport(s))
	if len(findings) != 1 || !strings.Contains(findings[0], "ВНЕ") {
		t.Fatalf("невидимость не названа: %v", findings)
	}
	if cen.unknownForm != 1 || cen.capabilities != 0 {
		t.Fatalf("перепись: %+v — ожидалось вне известных форм 1", cen)
	}
}

// ИНЪЕКЦИЯ: сдвинутый индекс — находка, и она НЕ подменяется находкой про
// мёртвую форму. Ось разведена намеренно: перепись форм при сдвиге недостоверна,
// и объявлять по ней форму мёртвой значило бы отвечать уверенно и неверно.
func TestShapeCapabilityInjectionShiftedIndexIsFound(t *testing.T) {
	s := nmCapability(map[string]int{"str": 0, "seq": 0, "unknown": 0})
	s.Shifted = []string{"_rup"}
	findings, cen := auditShapeCapabilities(nmShapeReport(s))
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка про сдвиг, получено: %v", findings)
	}
	if !strings.Contains(findings[0], "ПОЗИЦИОННЫЙ") {
		t.Fatalf("находка называет не тот предмет: %q", findings[0])
	}
	if cen.shifted != 1 {
		t.Fatalf("перепись сдвигов не сошлась: %+v", cen)
	}
}
