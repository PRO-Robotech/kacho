// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"strings"
	"testing"
)

// Доказательство способности гейта «проверка кейсов грузит модули связыванием
// набора» упасть И смолчать.
//
// Инъекция подаётся СУДЯЩЕЙ ФУНКЦИИ гейта (`auditValidatorCasesBinding`), а не
// её копии: проба, повторяющая логику, доказывала бы свойство копии.
//
// Вход берётся в той форме, в какой он живёт в дереве: проверка кейсов —
// самостоятельный скрипт, который импортирует генератор набора и зовёт загрузку
// модулей кейсов.

// nmCleanValidator — законная форма: проверка берёт связывание набора.
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
        mod = gen.load_cases(f)
        for c in getattr(mod, "CASES", []):
            out.append((c.id, f.name))
    return out
`

func nmValidatorAudit(t *testing.T, src string) ([]string, validatorBindingCensus) {
	t.Helper()
	return auditValidatorCasesBinding(map[string]string{
		"services/probe/tests/newman/scripts/validate-cases.py": src,
	})
}

// Законный близнец: связывание набора — гейт МОЛЧИТ.
func TestValidatorBindingCleanValidatorIsSilent(t *testing.T) {
	findings, cen := nmValidatorAudit(t, nmCleanValidator)
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл предмет на законной форме — он ловит вызов загрузчика вообще,\n"+
			"а не обход связывания: %v", findings)
	}
	if cen.callSites != 1 || cen.bound != 1 {
		t.Fatalf("перепись: обращений %d, связанных %d — ожидалось 1 и 1; без обеих величин\n"+
			"«ноль находок» неотличимо от «ноль прочитанного»", cen.callSites, cen.bound)
	}
}

// ИНЪЕКЦИЯ: возвращён прямой вызов общего загрузчика — гейт КРАСНЕЕТ и НАЗЫВАЕТ
// координату. Это ровно тот дефект, который жил в дереве: шесть проверок звали
// загрузчик своими руками и перестали исполняться на смене его подписи.
func TestValidatorBindingInjectionDirectLoaderCallIsFound(t *testing.T) {
	injected := strings.Replace(nmCleanValidator,
		"gen.load_cases(f)", "gen.load_cases_module(f)", 1)
	findings, cen := nmValidatorAudit(t, injected)
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел прямой вызов общего загрузчика — тогда решения набора\n"+
			"вправе быть записаны дважды и разойтись молча.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "services/probe/tests/newman/scripts/validate-cases.py") {
		t.Fatalf("находка не называет координату — по такой диагностике читатель не поймёт,\n"+
			"какую проверку чинить: %q", findings[0])
	}
	if !strings.Contains(findings[0], "load_cases_module") {
		t.Fatalf("находка не называет ФОРМУ обхода: %q", findings[0])
	}
	if cen.bound != 0 {
		t.Fatalf("связанных обращений насчитано %d при единственном прямом вызове", cen.bound)
	}
}

// Дескриптор оркестрации (#1474) — тоже законное связывание: решения набора
// живут в нём, и проверка берёт их оттуда.
func TestValidatorBindingRunDescriptorIsRecognised(t *testing.T) {
	injected := strings.Replace(nmCleanValidator,
		"gen.load_cases(f)", "gen._RUN.load(f)", 1)
	findings, cen := nmValidatorAudit(t, injected)
	if len(findings) != 0 {
		t.Fatalf("гейт не узнал связывание через дескриптор оркестрации — распознаватель\n"+
			"знает не все законные формы записи предмета, и всё записанное в неизвестной\n"+
			"ему форме уходит из-под наблюдения: %v", findings)
	}
	if cen.bound != 1 {
		t.Fatalf("перепись связанных обращений %d, ожидалась 1", cen.bound)
	}
}

// Проза шапки, называющая загрузчик, — НЕ вызов. Без этой пробы гейт краснел бы
// на собственном объяснении, и его сняли бы как непонятный.
func TestValidatorBindingProseAboutTheLoaderIsNotACall(t *testing.T) {
	prose := strings.Replace(nmCleanValidator,
		`"""Проверка кейсов набора: уникальность идентификаторов и каталогизация."""`,
		`"""Проверка кейсов набора.

Модули кейсов грузятся связыванием набора, а не вызовом gen.load_cases_module
своими руками: иначе решения набора записаны дважды.
"""`, 1)
	findings, cen := nmValidatorAudit(t, prose)
	if len(findings) != 0 {
		t.Fatalf("гейт принял ПРОЗУ о загрузчике за его вызов — он судит слово, а не то,\n"+
			"что код делает: %v", findings)
	}
	if cen.callSites != 1 {
		t.Fatalf("обращений насчитано %d, ожидалось 1 (проза вызовом не является)", cen.callSites)
	}
}

// Предпосылка: проверка, вовсе не грузящая модулей кейсов, — это отказ, а не
// молчание. Без этой ветви гейт, потерявший предмет, был бы вечнозелёным.
func TestValidatorBindingNoCallSitesIsARefusal(t *testing.T) {
	_, cen := nmValidatorAudit(t, "#!/usr/bin/env python3\nprint('ничего не грузим')\n")
	if cen.callSites != 0 {
		t.Fatalf("обращений насчитано %d там, где их нет", cen.callSites)
	}
	// Сам отказ выносит тело гейта по этой величине; здесь доказано, что величина
	// её различает: ноль обращений отличим от «все связаны».
	if cen.bound != 0 {
		t.Fatalf("связанных %d при нуле обращений — величины не различают состояния", cen.bound)
	}
}
