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


def retry_until_authorized(step, budget=25, interval_ms=500, lane_head=False):
    return step


_rya = functools.partial(retry_until_authorized, budget=25, interval_ms=500, lane_head=True)


def case_to_postman(case):
    return [step_to_postman(s) for s in _wrap_own_fresh_reads(case.steps, _rya)]
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
		"_wrap_own_fresh_reads(case.steps, _rya)", "case.steps", 1)
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

// ─── третья ЗАКОННАЯ форма полосы: тело общее, окно связано набором ──────────
//
// После #1379 тело полосы живёт в общем слое в одном экземпляре, а набор
// связывает своё ОКНО — величину, которую общий слой решать за него не вправе:
// пути материализации у доменов разные. Записывается это связыванием
// (`_rya = functools.partial(retry_until_authorized, …)`), и `def
// retry_until_authorized(` у набора не остаётся вовсе.
//
// Для распознавателя это третья законная форма записи предмета. Не зная её, он
// не находит полосы НИ У ОДНОГО набора — и гейт падает СВОЕЙ предпосылкой на
// работе, которая защиту укрепила.
const frwBoundLaneGenerator = `
import functools

from gen_shared import (
    retry_until_authorized,
    _wrap_own_fresh_reads,
    js_str,
)

_rya = functools.partial(retry_until_authorized, budget=25, interval_ms=500, lane_head=True)


def case_to_postman(case):
    return [step_to_postman(s) for s in _wrap_own_fresh_reads(case.steps, _rya)]
`

func TestFreshReadWrapBoundLaneIsRecognised(t *testing.T) {
	findings, cen := frwAudit(t, frwShared, frwBoundLaneGenerator)
	if cen.withLane != 1 {
		t.Fatalf("распознаватель не видит полосу, взятую импортом и связанную окном набора:\n"+
			"с полосой %d, ожидалось 1.\nЭто не «ноль находок», а ноль ПРОЧИТАННОГО: набор с\n"+
			"полосой уходит из-под наблюдения целиком, и непровязанный предикат в нём\n"+
			"остался бы ненайденным.\nперепись: %+v", cen.withLane, cen)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законном близнеце: полоса взята из общего слоя, окно\n"+
			"связано набором, предикат провязан: %v", findings)
	}
}

func TestFreshReadWrapBoundLaneUnwiredIsFound(t *testing.T) {
	// Тот же генератор с ВОЗВРАЩЁННЫМ дефектом: полоса есть и доступна, а
	// сериализация кейса предикат не применяет. Без распознавания третьей формы
	// эта находка не появилась бы вовсе — набор был бы пропущен раньше проверки.
	unwired := strings.Replace(frwBoundLaneGenerator,
		"_wrap_own_fresh_reads(case.steps, _rya)", "case.steps", 1)
	findings, cen := frwAudit(t, frwShared, unwired)
	if len(findings) == 0 {
		t.Fatalf("непровязанный предикат у набора со связанной полосой не найден:\n"+
			"перепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "провязан в case_to_postman: нет") ||
		!strings.Contains(findings[0], "services/x/tests/newman/scripts/gen.py") {
		t.Fatalf("находка не называет ни предмета, ни координаты: %q", findings[0])
	}
}

// ─── ОСЬ ГОЛОВЫ ПОЛОСЫ: ждать можно ПРАВА, а не появление имени ──────────────
//
// Инъекция снимает РОВНО новое свойство: полоса остаётся взятой из общего слоя и
// провязанной в сериализацию, меняется только наличие головы. Красное от соседа
// доказательством бы не было.

func TestFreshReadWrapLaneWithoutHeadIsFound(t *testing.T) {
	headless := strings.Replace(frwBoundLaneGenerator,
		"budget=25, interval_ms=500, lane_head=True", "budget=25, interval_ms=500", 1)
	findings, cen := frwAudit(t, frwShared, headless)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел полосу без головы — тогда шаг, адресующийся к\n"+
			"незахваченной переменной, выжигает весь бюджет на вопрос ни о чём.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "полоса несёт голову: нет") {
		t.Fatalf("находка не называет, ЧЕГО не хватает: %q", findings[0])
	}
	if cen.withLane != 1 {
		t.Fatalf("инъекция повредила соседнюю ось: с полосой %d вместо 1", cen.withLane)
	}
	if cen.withHead != 0 {
		t.Fatalf("перепись «несёт голову» = %d при её отсутствии", cen.withHead)
	}
}

// Проза о голове головой НЕ является: величина обязана стоять ВНУТРИ связывания.
// Без этой ветви гейт зеленел бы на объяснении, которым набор рассказывает, чего
// у него нет.
func TestFreshReadWrapProseAboutHeadIsNotAHead(t *testing.T) {
	prose := strings.Replace(frwBoundLaneGenerator,
		"_rya = functools.partial(retry_until_authorized, budget=25, interval_ms=500, lane_head=True)",
		"# голову полосы (lane_head=True) этот набор пока не несёт\n"+
			"_rya = functools.partial(retry_until_authorized, budget=25, interval_ms=500)", 1)
	findings, cen := frwAudit(t, frwShared, prose)
	if len(findings) == 0 {
		t.Fatalf("гейт принял ПРОЗУ о голове за саму голову — он судит слово, а не то,\n"+
			"что код делает.\nперепись: %+v", cen)
	}
	if cen.withHead != 0 {
		t.Fatalf("перепись засчитала комментарий: «несёт голову» = %d", cen.withHead)
	}
}

// Набор, объявивший полосу СВОЕЙ копией, головы не имеет by construction: ручка
// живёт в реализации общего слоя. Требовать её от копии значило бы судить
// раскладку, а её судит соседний гейт.
func TestFreshReadWrapOwnLaneCopyIsNotAskedForAHead(t *testing.T) {
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
		t.Fatalf("гейт требует ручку общего слоя от набора, который берёт полосу СВОЕЙ\n"+
			"копией — такой ручки у него нет вовсе: %v", findings)
	}
	if cen.withLane != 1 {
		t.Fatalf("полоса своей копией не распознана: с полосой %d вместо 1", cen.withLane)
	}
}

// Предпосылка распознавателя: он обязан дочитывать связывание до его собственной
// закрывающей скобки. Обход, начатый не с той позиции, отвечает «головы нет» на
// ЛЮБОМ входе — то есть гейт краснеет всегда и его отключают первым.
func TestFreshReadWrapHeadRecognizerReadsTheWholeBinding(t *testing.T) {
	multiline := "_rya = functools.partial(retry_until_authorized,\n" +
		"                        budget=25, interval_ms=500, lane_head=True)\n"
	bound, head := laneBindingHasHead(multiline)
	if !bound || !head {
		t.Fatalf("связывание в НЕСКОЛЬКО строк не прочитано целиком: связано=%v, голова=%v.\n"+
			"Ровно в этой форме оно и записано во всех шести наборах", bound, head)
	}
	after := "_rya = functools.partial(retry_until_authorized, budget=25)\n" +
		"# дальше по файлу слово lane_head=True встречается в объяснении\n"
	_, headAfter := laneBindingHasHead(after)
	if headAfter {
		t.Fatalf("распознаватель прочитал ЗА пределами связывания — тогда любое упоминание\n" +
			"ниже по файлу засчитывается головой")
	}
	if bound, _ := laneBindingHasHead("def case_to_postman(case):\n    return case.steps\n"); bound {
		t.Fatalf("распознаватель нашёл связывание там, где его нет")
	}
}
