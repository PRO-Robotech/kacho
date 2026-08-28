// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт провязки полосы видимости СПОСОБЕН упасть —
// и что после переустройства он падает на том же существе, что и до него.
//
// ПОЧЕМУ ИНЪЕКЦИЯ ЗАВЕДЕНА ИМЕННО СЕЙЧАС. Предикат обёртки переехал в общий
// слой, и гейт переехал за ним. `testing.md` §«Гейт на класс», п.8: переустройство
// требует ПОВТОРНОЙ инъекции — совпадение переписи её не заменяет, потому что
// гейт, потерявший способность краснеть, на чистом дереве выглядит точно так же.
// Прежде инъекции у этого гейта не было вовсе: способность краснеть держалась
// тем, что её однажды наблюдали.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditFreshReadWrapWiring`).
//
// У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и перепись печатает
// ОБЕ величины: «генераторов с полосой» и «берут предикат из общего слоя».
// Одного числа не хватает — оно скрыло бы ровно тот случай, ради которого гейт
// заведён.
package artifactgates

import (
	"strings"
	"testing"
)

// frwShared — общий слой в форме, в какой он лежит в дереве после сведения.
const frwShared = `
def _wrap_own_fresh_reads(steps, retry_until_authorized, rename=True):
    return steps
`

// frwSharedWithout — общий слой ДО сведения: предиката там нет.
const frwSharedWithout = `
def js_str(value):
    return value
`

// frwWiredGenerator — генератор ПОСЛЕ сведения: полоса своя, предикат взят
// импортом и применён к шагам кейса. Законный близнец — гейт обязан молчать.
const frwWiredGenerator = `
from gen_shared import (
    _wrap_own_fresh_reads,
    js_str,
)


def retry_until_authorized(step, budget=25, interval_ms=500):
    return step


def case_to_postman(case):
    return [step_to_postman(s) for s in _wrap_own_fresh_reads(case.steps, retry_until_authorized)]
`

func frwAudit(t *testing.T, shared, generator string) ([]string, freshReadWrapCensus) {
	t.Helper()
	return auditFreshReadWrapWiring(shared, map[string]string{"services/x/tests/newman/scripts/gen.py": generator})
}

// ─── молчание на ЗАКОННОМ близнеце ───────────────────────────────────────────

func TestFreshReadWrapWiredGeneratorIsSilent(t *testing.T) {
	findings, cen := frwAudit(t, frwShared, frwWiredGenerator)
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на сведённом генераторе: предикат взят из общего слоя\n"+
			"и провязан — это ровно та форма, к которой сведение и вело: %v", findings)
	}
	// Положительный контроль переписи: молчание обязано означать «посмотрел и не
	// нашёл», а не «не посмотрел».
	if cen.withLane != 1 || !cen.sharedDeclare || cen.takenFromShared != 1 {
		t.Fatalf("перепись не подтверждает, что гейт читал предмет: %+v\n"+
			"ожидалось: с полосой 1, объявлен в общем слое, берут оттуда 1", cen)
	}
}

// ─── красное на настоящем дефекте: предикат НЕ ПРОВЯЗАН ──────────────────────

func TestFreshReadWrapInjectionUnwiredGeneratorIsFound(t *testing.T) {
	// Возвращаем ровно тот дефект, ради которого гейт заведён: полоса видимости
	// у набора есть, предикат доступен, а сериализация кейса его не применяет —
	// значит обёртка ставится вручную, и пропуск неотличим от решения.
	unwired := strings.Replace(frwWiredGenerator,
		"_wrap_own_fresh_reads(case.steps, retry_until_authorized)", "case.steps", 1)
	findings, cen := frwAudit(t, frwShared, unwired)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел непровязанный предикат — он не способен упасть на своём\n"+
			"предмете.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "провязан в case_to_postman: нет") {
		t.Fatalf("находка не называет, ЧЕГО не хватает: %q", findings[0])
	}
	if !strings.Contains(findings[0], "services/x/tests/newman/scripts/gen.py") {
		t.Fatalf("находка не называет координату генератора: %q", findings[0])
	}
}

// ─── красное на настоящем дефекте: предикат НЕДОСТУПЕН ───────────────────────

func TestFreshReadWrapInjectionMissingImportIsFound(t *testing.T) {
	// Объявления в общем слое НЕ достаточно: оно одинаково истинно для
	// генератора, который предикат импортирует, и для того, который о нём не
	// знает. Снимаем импорт — предикат в дереве есть, а этому набору недоступен.
	noImport := strings.Replace(frwWiredGenerator, "    _wrap_own_fresh_reads,\n", "", 1)
	findings, _ := frwAudit(t, frwShared, noImport)
	if len(findings) == 0 {
		t.Fatalf("гейт зачёл предикат ДОСТУПНЫМ по одному лишь объявлению в общем слое.\n" +
			"Тогда проверка вырождается: она истинна для всякого генератора, включая тот,\n" +
			"который об общем слое не знает вовсе.")
	}
	if !strings.Contains(findings[0], "предикат доступен: нет") {
		t.Fatalf("находка не называет, что предикат недоступен: %q", findings[0])
	}
}

// ─── ПРЕДПОСЫЛКА: обход импорта обязан быть живым ────────────────────────────

func TestFreshReadWrapImportRecognizerSeesTheList(t *testing.T) {
	// Отдельная ось: если форма импорта изменится и обход её не прочитает,
	// «доступен» снова начнёт вычисляться только по локальному `def` — то есть
	// вернётся ровно тот дефект, который переустройство и чинило.
	if !importsSharedName(frwWiredGenerator, "_wrap_own_fresh_reads") {
		t.Fatalf("обход не нашёл имя в списке импорта общего слоя")
	}
	if importsSharedName(frwWiredGenerator, "poll_operation_until_done") {
		t.Fatalf("обход нашёл имя, которого в списке импорта нет — он судит не список")
	}
	// Упоминание в вызове импортом не является: распознаватель по подстроке
	// краснел бы на собственном объяснении.
	mention := "\n# _wrap_own_fresh_reads — так это называется в общем слое\n" +
		"x = _wrap_own_fresh_reads(steps, retry)\n"
	if importsSharedName(mention, "_wrap_own_fresh_reads") {
		t.Fatalf("обход принял упоминание за импорт")
	}
}

// ─── ПРЕДПОСЫЛКА: старая полоса не приписана новой ───────────────────────────

func TestFreshReadWrapLocalDeclarationStillCounts(t *testing.T) {
	// Контроль в обратную сторону: генератор, объявивший предикат У СЕБЯ (форма
	// до сведения), обязан по-прежнему считаться защищённым. Иначе гейт требовал
	// бы переезда как условия — то есть судил бы раскладку, а не свойство.
	local := `
def retry_until_authorized(step):
    return step


def _wrap_own_fresh_reads(steps, rename=True):
    return steps


def case_to_postman(case):
    return _wrap_own_fresh_reads(case.steps)
`
	findings, cen := frwAudit(t, frwSharedWithout, local)
	if len(findings) != 0 {
		t.Fatalf("гейт объявил находкой собственное объявление предиката у набора —\n"+
			"он судит раскладку вместо свойства: %v", findings)
	}
	if cen.sharedDeclare || cen.takenFromShared != 0 {
		t.Fatalf("перепись приписала общему слою то, чего в нём нет: %+v", cen)
	}
}
