// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"strings"
	"testing"
)

// Доказательство способности гейта «потребитель берёт хребет связыванием
// набора» упасть И смолчать.
//
// Инъекция подаётся СУДЯЩЕЙ ФУНКЦИИ гейта (`auditNewmanSpineBinding`), а не её
// копии: проба, повторяющая логику, доказывала бы свойство копии.
//
// Вход берётся в той форме, в какой он живёт в дереве: проверка кейсов —
// самостоятельный скрипт, импортирующий генератор набора; проба — модуль
// pytest, делающий то же самое.

// nmCleanValidator — законная форма проверки кейсов: связывание набора.
const nmCleanValidator = `#!/usr/bin/env python3
"""Проверка кейсов набора: уникальность идентификаторов и каталогизация."""
import sys
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPTS_DIR))


def _all_cases():
    import gen  # noqa: E402

    out = []
    for f in sorted(CASES_DIR.glob("*.py")):
        mod = gen._RUN.load(f)
        for c in getattr(mod, "CASES", []):
            out.append((c.id, f.name))
    return out
`

// nmCleanProbe — законная форма пробы: та же связка, другой предмет. Проба
// собирает элемент кейса и читает порождённый скрипт.
const nmCleanProbe = `#!/usr/bin/env python3
"""Проба: опрос операции наследует предъявителя шага, отчеканившего её."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

import gen  # noqa: E402


def _items(case):
    col = gen._RUN.case_item(case)
    return {it["name"]: it for it in col["item"]}


def test_poll_inherits_the_minting_principal():
    case = gen.Case(id="T", title="t", classes=["CRUD"], priority="P0", steps=[])
    assert _items(case) is not None
`

func nmSpineAudit(t *testing.T, kind, rel, src string) ([]string, spineBindingCensus) {
	t.Helper()
	return auditNewmanSpineBinding(
		map[string]string{rel: src},
		map[string]string{rel: kind},
	)
}

const (
	nmValidatorRel = "services/probe/tests/newman/scripts/validate-cases.py"
	nmProbeRel     = "services/probe/tests/newman/scripts/gen_test.py"
)

// Законный близнец (проверка кейсов): связывание набора — гейт МОЛЧИТ.
func TestSpineBindingCleanValidatorIsSilent(t *testing.T) {
	findings, cen := nmSpineAudit(t, "проверка кейсов", nmValidatorRel, nmCleanValidator)
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл предмет на законной форме — он ловит обращение к хребту вообще,\n"+
			"а не обход связывания: %v", findings)
	}
	if cen.callSites != 1 || cen.bound != 1 || cen.validators != 1 {
		t.Fatalf("перепись: обращений %d, связанных %d, проверок %d — ожидалось 1, 1, 1;\n"+
			"без этих величин «ноль находок» неотличимо от «ноль прочитанного»",
			cen.callSites, cen.bound, cen.validators)
	}
}

// Законный близнец (проба): тот же вердикт на втором виде потребителя.
// Без него расширение охвата было бы объявлено, а не доказано.
func TestSpineBindingCleanProbeIsSilent(t *testing.T) {
	findings, cen := nmSpineAudit(t, "проба", nmProbeRel, nmCleanProbe)
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на пробе, берущей связывание набора: %v", findings)
	}
	if cen.probes != 1 || cen.bound != 1 {
		t.Fatalf("перепись: проб %d, связанных обращений %d — ожидалось 1 и 1", cen.probes, cen.bound)
	}
}

// ИНЪЕКЦИЯ (проверка кейсов): возвращён прямой вызов общего загрузчика — гейт
// КРАСНЕЕТ и НАЗЫВАЕТ координату. Это дефект, который жил в дереве: шесть
// проверок звали загрузчик своими руками и перестали исполняться на смене его
// подписи.
func TestSpineBindingInjectionDirectLoaderCallIsFound(t *testing.T) {
	injected := strings.Replace(nmCleanValidator, "gen._RUN.load(f)", "gen.load_cases_module(f)", 1)
	findings, cen := nmSpineAudit(t, "проверка кейсов", nmValidatorRel, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел прямой вызов общего загрузчика — тогда решения набора\n"+
			"вправе быть записаны дважды и разойтись молча.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], nmValidatorRel) {
		t.Fatalf("находка не называет координату: %q", findings[0])
	}
	if !strings.Contains(findings[0], "load_cases_module") {
		t.Fatalf("находка не называет ФОРМУ обхода: %q", findings[0])
	}
	if cen.bound != 0 {
		t.Fatalf("связанных обращений насчитано %d при единственном прямом вызове", cen.bound)
	}
}

// ИНЪЕКЦИЯ (проба): ровно тот дефект #1536 — проба зовёт сборщик кейса своими
// руками. По каждой из ЧЕТЫРЁХ функций хребта отдельно: форма, о которой
// распознаватель не знает, не находка и не молчание, а НЕВИДИМОСТЬ.
func TestSpineBindingInjectionProbeCallsEachSpineFunctionDirectly(t *testing.T) {
	for _, fn := range []string{
		"load_cases_module", "case_to_postman", "step_to_postman", "build_collection",
	} {
		injected := strings.Replace(nmCleanProbe, "gen._RUN.case_item(case)", "gen."+fn+"(case)", 1)
		findings, cen := nmSpineAudit(t, "проба", nmProbeRel, injected)
		if len(findings) == 0 {
			t.Fatalf("прямой вызов %s из пробы гейт не увидел — двадцать восемь проб #1536\n"+
				"перестали исполняться именно так.\nперепись: %+v", fn, cen)
		}
		if !strings.Contains(findings[0], nmProbeRel) || !strings.Contains(findings[0], fn) {
			t.Fatalf("находка по %s не называет координату и форму: %q", fn, findings[0])
		}
		if cen.bound != 0 {
			t.Fatalf("по %s связанных обращений %d при единственном прямом вызове", fn, cen.bound)
		}
	}
}

// Каждый метод связывания узнаётся. Метод, о котором распознаватель не знает,
// объявил бы законный вызов находкой — и первый же ложный срабат снял бы гейт.
func TestSpineBindingEveryBindingMethodIsRecognised(t *testing.T) {
	for _, method := range []string{"load", "case_item", "step_item", "collection"} {
		injected := strings.Replace(nmCleanProbe, "gen._RUN.case_item(case)", "gen._RUN."+method+"(case)", 1)
		findings, cen := nmSpineAudit(t, "проба", nmProbeRel, injected)
		if len(findings) != 0 {
			t.Fatalf("связывание через _RUN.%s гейт не узнал — распознаватель знает не все\n"+
				"законные формы записи предмета: %v", method, findings)
		}
		if cen.bound != 1 {
			t.Fatalf("_RUN.%s: связанных обращений %d, ожидалась 1", method, cen.bound)
		}
	}
}

// Приёмник связывания НЕ фиксирован именем `gen`: пробы зовут генератор и
// элементом словаря генераторов. Форма, неизвестная распознавателю, уводит
// написанное в ней из-под наблюдения.
func TestSpineBindingReceiverIsNotPinnedToTheNameGen(t *testing.T) {
	injected := strings.Replace(nmCleanProbe,
		"gen._RUN.case_item(case)", "GENERATORS[svc].case_to_postman(case)", 1)
	findings, _ := nmSpineAudit(t, "проба", nmProbeRel, injected)
	if len(findings) == 0 {
		t.Fatalf("прямой вызов через ДРУГОЙ приёмник гейт не увидел — а именно так пробы\n" +
			"зовут чужой генератор (`GENERATORS[svc]`, `_generator(service)`)")
	}
}

// Проза, называющая хребет, — НЕ вызов. Без этой пробы гейт краснел бы на
// собственном объяснении, и его сняли бы как непонятный.
func TestSpineBindingProseAboutTheSpineIsNotACall(t *testing.T) {
	prose := strings.Replace(nmCleanValidator,
		`"""Проверка кейсов набора: уникальность идентификаторов и каталогизация."""`,
		`"""Проверка кейсов набора.

Модули кейсов грузятся связыванием набора, а не вызовом gen.load_cases_module
своими руками: иначе решения набора записаны дважды.
"""`, 1)
	findings, cen := nmSpineAudit(t, "проверка кейсов", nmValidatorRel, prose)
	if len(findings) != 0 {
		t.Fatalf("гейт принял ПРОЗУ о хребте за его вызов — он судит слово, а не то,\n"+
			"что код делает: %v", findings)
	}
	if cen.callSites != 1 {
		t.Fatalf("обращений насчитано %d, ожидалось 1 (проза вызовом не является)", cen.callSites)
	}
}

// Проза, воспроизводящая вызов ДОСЛОВНО — со скобкой и аргументом. Прежний
// распознаватель читал сырой текст и на этой форме краснел бы: шапки самих
// потребителей цитируют вызов именно так.
func TestSpineBindingProseQuotingTheCallVerbatimIsNotACall(t *testing.T) {
	prose := strings.Replace(nmCleanValidator,
		`"""Проверка кейсов набора: уникальность идентификаторов и каталогизация."""`,
		`"""Проверка кейсов набора.

Раньше здесь стояло gen.load_cases_module(f) — прямой вызов общего загрузчика.
"""`, 1)
	findings, cen := nmSpineAudit(t, "проверка кейсов", nmValidatorRel, prose)
	if len(findings) != 0 {
		t.Fatalf("цитата вызова в шапке засчитана вызовом — гейт судит текст, а не код: %v", findings)
	}
	if cen.callSites != 1 {
		t.Fatalf("обращений насчитано %d, ожидалось 1", cen.callSites)
	}
}

// Комментарий с тем же текстом — тоже не вызов, и это ОТДЕЛЬНАЯ ветвь
// распознавателя: строковый литерал и комментарий снимаются разными правилами.
func TestSpineBindingCommentQuotingTheCallIsNotACall(t *testing.T) {
	commented := strings.Replace(nmCleanValidator,
		"        mod = gen._RUN.load(f)",
		"        # было: mod = gen.case_to_postman(f)\n        mod = gen._RUN.load(f)", 1)
	findings, cen := nmSpineAudit(t, "проверка кейсов", nmValidatorRel, commented)
	if len(findings) != 0 {
		t.Fatalf("комментарий засчитан вызовом: %v", findings)
	}
	if cen.callSites != 1 || cen.bound != 1 {
		t.Fatalf("перепись: обращений %d, связанных %d — ожидалось 1 и 1", cen.callSites, cen.bound)
	}
}

// Предпосылка: потребитель, вовсе не зовущий хребет, — это отказ, а не
// молчание. Без этой ветви гейт, потерявший предмет, был бы вечнозелёным.
func TestSpineBindingNoCallSitesIsARefusal(t *testing.T) {
	_, cen := nmSpineAudit(t, "проба", nmProbeRel, "#!/usr/bin/env python3\nprint('ничего не грузим')\n")
	if cen.callSites != 0 {
		t.Fatalf("обращений насчитано %d там, где их нет", cen.callSites)
	}
	// Сам отказ выносит тело гейта по этой величине; здесь доказано, что
	// величина её различает: ноль обращений отличим от «все связаны».
	if cen.bound != 0 {
		t.Fatalf("связанных %d при нуле обращений — величины не различают состояния", cen.bound)
	}
	if cen.probes != 1 {
		t.Fatalf("осмотренных проб %d — перепись обязана считать прочитанное отдельно от находок", cen.probes)
	}
}
