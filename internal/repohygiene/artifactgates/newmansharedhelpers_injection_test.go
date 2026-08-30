// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт единственного объявления общего помощника
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditSharedHelperForks`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии, а не гейта.
//
// У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль» само по
// себе неотличимо от «распознаватель ослеп», поэтому каждая молчащая проба
// дополнительно утверждает перепись: гейт объявления УВИДЕЛ и промолчал по
// существу, а не потому, что смотрел мимо.
//
// ОСИ РАЗВЕДЕНЫ. Функция и константа проверяются порознь: одна инъекция, роняющая
// обе, оставила бы незамеченным распознаватель, знающий только одну форму — тот
// самый дефект, ради которого распознаватель и расширяли.
package artifactgates

import (
	"strings"
	"testing"
)

// nmSharedModule — общий слой в форме, в какой он лежит в дереве: обе формы
// объявления сразу, потому что предмет гейта — обе.
const nmSharedModule = `
_MUTATION_METHODS = {"POST", "PATCH"}
_FIELD_DONE = "done"


def js_str(value):
    return value


def _asserts_done(step):
    return True
`

// nmCleanGenerator — генератор ПОСЛЕ сведения: общие имена приходят импортом,
// свои — объявлены у себя. Это законный близнец: форма объявления та же, имена
// другие, и гейт обязан молчать.
const nmCleanGenerator = `
from gen_shared import (
    js_str,
    _asserts_done,
    _MUTATION_METHODS,
    _FIELD_DONE,
)

_SUITE_ONLY = {"vpc"}


def build_collection(case):
    return js_str(case) if _asserts_done(case) else _MUTATION_METHODS
`

func nmSharedHelperAudit(t *testing.T, generator string) ([]string, sharedHelperCensus) {
	t.Helper()
	return auditSharedHelperForks(nmSharedModule, map[string]string{"services/x/tests/newman/scripts/gen.py": generator})
}

// ─── красное на настоящем дефекте: ФУНКЦИЯ ───────────────────────────────────

func TestSharedHelperInjectionFunctionForkIsFound(t *testing.T) {
	// Возвращаем ровно тот дефект, ради которого гейт заведён: набор снова
	// объявляет общий помощник у себя.
	injected := nmCleanGenerator + `

def js_str(value):
    return value + "фork"
`
	findings, cen := nmSharedHelperAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел форк ФУНКЦИИ js_str — он не способен упасть на своём предмете.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "js_str") {
		t.Fatalf("находка не называет имя форкнутой функции — по такой диагностике\n"+
			"читатель пойдёт искать не там: %q", findings[0])
	}
	if !strings.Contains(findings[0], "services/x/tests/newman/scripts/gen.py") {
		t.Fatalf("находка не называет координату генератора: %q", findings[0])
	}
	if cen.forks != 1 {
		t.Fatalf("перепись форков = %d, ожидалась 1 (иначе неясно, что именно сосчитано)", cen.forks)
	}
}

// ─── красное на настоящем дефекте: КОНСТАНТА ─────────────────────────────────

func TestSharedHelperInjectionConstantForkIsFound(t *testing.T) {
	// Отдельная ось. До расширения распознавателя эта проба была бы зелёной при
	// живом форке — константы не наблюдались вовсе.
	injected := nmCleanGenerator + `

_FIELD_DONE = "готово"
`
	findings, cen := nmSharedHelperAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел форк КОНСТАНТЫ _FIELD_DONE — распознаватель знает только `def`,\n"+
			"и одиннадцать переехавших констант остаются вне наблюдения.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "_FIELD_DONE (константа)") {
		t.Fatalf("находка не называет константу как константу — читатель не поймёт,\n"+
			"какую форму объявления чинить: %q", findings[0])
	}
	if cen.forks != 1 {
		t.Fatalf("перепись форков = %d, ожидалась 1", cen.forks)
	}
}

// ─── молчание на ЗАКОННОМ близнеце ───────────────────────────────────────────

func TestSharedHelperCleanGeneratorIsSilent(t *testing.T) {
	findings, cen := nmSharedHelperAudit(t, nmCleanGenerator)
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на сведённом генераторе — он ловит ФОРМУ, а не существо,\n"+
			"и первый же ложный срабат его отключит: %v", findings)
	}
	// Положительный контроль переписи: молчание обязано означать «посмотрел и не
	// нашёл», а не «не посмотрел».
	if cen.generators != 1 || cen.sharedFuncs != 2 || cen.sharedConsts != 2 {
		t.Fatalf("перепись не подтверждает, что гейт читал предмет: %+v\n"+
			"ожидалось: генераторов 1, функций 2, констант 2", cen)
	}
	if cen.forks != 0 {
		t.Fatalf("форков насчитано %d при законном входе", cen.forks)
	}
}

// Собственный помощник набора, чьего имени в общем слое НЕТ, — не форк.
// Без этой пробы гейт мог бы запрещать наборам иметь свои функции вообще.
func TestSharedHelperSuiteOwnNamesAreNotForks(t *testing.T) {
	own := nmCleanGenerator + `

def _suite_only_helper(step):
    return step


_SUITE_ONLY_TABLE = {"a": 1}
`
	findings, cen := nmSharedHelperAudit(t, own)
	if len(findings) != 0 {
		t.Fatalf("гейт объявил форком собственный помощник набора — он судит наличие\n"+
			"объявления, а не совпадение с общим слоем: %v", findings)
	}
	if cen.forks != 0 {
		t.Fatalf("форков насчитано %d при собственных именах набора", cen.forks)
	}
}

// Упоминание общего имени в вызове, строке и комментарии форком не является:
// распознаватель по подстроке краснел бы на собственном объяснении гейта.
func TestSharedHelperMentionIsNotDeclaration(t *testing.T) {
	mentions := nmCleanGenerator + `

MESSAGE = "чинится в gen_shared: def js_str( — общий слой"
# def _asserts_done( — так это объявлено в общем слое; здесь это ПОЯСНЕНИЕ
`
	findings, _ := nmSharedHelperAudit(t, mentions)
	if len(findings) != 0 {
		t.Fatalf("гейт принял упоминание за объявление — он ловит текст, а не узел: %v", findings)
	}
}

// Вложенное определение принадлежит своей функции и форком не является.
func TestSharedHelperNestedDefinitionIsNotAFork(t *testing.T) {
	nested := nmCleanGenerator + `

def outer(case):
    def js_str(value):
        return value
    return js_str(case)
`
	findings, _ := nmSharedHelperAudit(t, nested)
	if len(findings) != 0 {
		t.Fatalf("гейт объявил форком ВЛОЖЕННОЕ определение — оно локально своей функции\n"+
			"и общий слой не подменяет: %v", findings)
	}
}

// ─── НОВАЯ ОСЬ: форма объявления у набора и в общем слое РАЗНАЯ ───────────────
//
// Пока распознаватель сверял функцию с функцией и константу с константой, имя,
// перекрытое ДРУГОЙ формой, оставалось вне наблюдения: не находкой и не
// молчанием, а невидимостью. Python разрешает имя по последнему связыванию в
// модуле, поэтому импорт из общего слоя при этом остаётся на месте и выглядит
// действующим — прочитать перекрытие можно только сравнив весь модуль.
//
// ИНЪЕКЦИЯ СНИМАЕТ РОВНО НОВОЕ СВОЙСТВО. Она не заводит лишнего объявления
// (такое нарушало бы и прежнюю проверку, и красное приходило бы от соседа —
// доказательством это не было бы), а МЕНЯЕТ ФОРМУ у имени, чьё прежнее
// свойство на месте.

// Присваивание набора перекрывает функцию общего слоя. Каноническая форма, ради
// которой ось и заведена: связывание общего помощника с умолчаниями набора.
func TestSharedHelperInjectionAssignmentShadowingFunctionIsFound(t *testing.T) {
	injected := nmCleanGenerator + `

js_str = _suite_bind(js_str, quote="'")
`
	findings, cen := nmSharedHelperAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел ПРИСВАИВАНИЕ, перекрывающее функцию общего слоя.\n"+
			"Распознаватель знает только сверку формы с формой, и связывание\n"+
			"`имя = …` уводит помощник из-под наблюдения молча.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "js_str (присваивание набора затеняет функцию общего слоя)") {
		t.Fatalf("находка не называет ФОРМУ перекрытия — по такой диагностике читатель\n"+
			"пойдёт искать второе `def`, которого нет: %q", findings[0])
	}
	if cen.crossKind != 1 {
		t.Fatalf("перепись перекрёстных затенений = %d, ожидалась 1: без отдельного числа\n"+
			"расширение распознавателя неотличимо от холостого", cen.crossKind)
	}
}

// Та же ось с другой стороны: функция набора перекрывает константу общего слоя.
func TestSharedHelperInjectionFunctionShadowingConstantIsFound(t *testing.T) {
	injected := nmCleanGenerator + `

def _FIELD_DONE(step):
    return step
`
	findings, cen := nmSharedHelperAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел ФУНКЦИЮ, перекрывающую константу общего слоя.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "_FIELD_DONE (функция набора затеняет константу общего слоя)") {
		t.Fatalf("находка не называет форму перекрытия: %q", findings[0])
	}
	if cen.crossKind != 1 {
		t.Fatalf("перепись перекрёстных затенений = %d, ожидалась 1", cen.crossKind)
	}
}

// КОНТРОЛЬ ТРЕТЬИМ ПРОГОНОМ (`testing.md` §«Гейт на класс», п.2в): прежняя
// проверка обязана продолжать краснеть НА СВОЁМ предмете и не приписывать его
// себе новая. Без этой пробы молчание прежней половины было бы неотличимо от её
// смерти.
func TestSharedHelperSameKindForkIsNotCountedAsCrossKind(t *testing.T) {
	injected := nmCleanGenerator + `

def js_str(value):
    return value + "фork"
`
	findings, cen := nmSharedHelperAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("прежняя половина распознавателя перестала краснеть на своём предмете")
	}
	if cen.forks != 1 || cen.crossKind != 0 {
		t.Fatalf("форков %d, перекрёстных %d — ожидалось 1 и 0: форк ОДНОЙ формы не должен\n"+
			"попадать в перепись новой оси, иначе два числа перестают различать два класса",
			cen.forks, cen.crossKind)
	}
}

// Законный близнец новой оси: имя, которого в общем слое НЕТ, набор вправе
// связывать присваиванием — это его собственное имя, а не перекрытие.
func TestSharedHelperOwnAssignmentIsNotCrossKind(t *testing.T) {
	own := nmCleanGenerator + `

_suite_retry = _suite_bind(None, quote="'")
`
	findings, cen := nmSharedHelperAudit(t, own)
	if len(findings) != 0 {
		t.Fatalf("гейт объявил перекрытием собственное имя набора — он ловит форму\n"+
			"объявления, а не совпадение с общим слоем: %v", findings)
	}
	if cen.crossKind != 0 {
		t.Fatalf("перекрёстных затенений насчитано %d при собственном имени набора", cen.crossKind)
	}
}
