// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"strings"
	"testing"
)

// Доказательство способности гейта «шапка описывает свой набор» упасть И
// смолчать — по каждой из трёх осей порознь.
//
// Инъекция подаётся СУДЯЩЕЙ ФУНКЦИИ (`auditGeneratorHeaders`), а не её копии, и
// снимает РОВНО одно свойство за раз: осмотренная шапка остаётся законной по
// двум другим осям, иначе красное приходило бы от соседа и доказательством не
// было бы.

const nmProbeGenRel = "services/probe/tests/newman/scripts/gen.py"

// Законная шапка: называет общий слой, не называет чужого генератора, пример
// вызова ведёт к существующему модулю.
const nmCleanHeader = `
tests/newman/scripts/gen.py — генератор Postman collections набора probe.

Использование:
    python3 scripts/gen.py             # все модули
    python3 scripts/gen.py network     # один модуль

Форму коллекции собирает общий модуль tests/newman/kacholib/gen_shared.py.
`

func nmHeaderAudit(t *testing.T, doc string, mods ...string) ([]string, headerCensus) {
	t.Helper()
	own := map[string]bool{}
	for _, m := range mods {
		own[m] = true
	}
	return auditGeneratorHeaders(
		map[string]string{nmProbeGenRel: doc},
		map[string]map[string]bool{nmProbeGenRel: own})
}

// Законный близнец — гейт МОЛЧИТ, и перепись это подтверждает числами.
func TestGeneratorHeaderCleanIsSilent(t *testing.T) {
	findings, cen := nmHeaderAudit(t, nmCleanHeader, "network")
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл предмет на законной шапке: %v", findings)
	}
	if cen.withShared != 1 || cen.usageLines != 1 {
		t.Fatalf("перепись %+v — ожидалось «называет общий слой» 1, примеров 1", cen)
	}
}

// ОСЬ I: шапка молчит про общий слой — тогда читателю негде узнать, где живёт
// форма коллекции, и он пойдёт сверяться с соседом.
func TestGeneratorHeaderInjectionNoSharedLayerIsFound(t *testing.T) {
	doc := strings.Replace(nmCleanHeader,
		"Форму коллекции собирает общий модуль tests/newman/kacholib/gen_shared.py.",
		"Форму коллекции собирает этот файл.", 1)
	findings, cen := nmHeaderAudit(t, doc, "network")
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел шапку без общего слоя.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "не называет общий слой") {
		t.Fatalf("находка не называет ось: %q", findings[0])
	}
	if cen.withShared != 0 {
		t.Fatalf("перепись «называет общий слой» = %d при её отсутствии", cen.withShared)
	}
	if cen.usageLines != 1 {
		t.Fatalf("вторая ось повреждена инъекцией первой: примеров %d вместо 1", cen.usageLines)
	}
}

// ОСЬ II: шапка называет образцом ЧУЖОЙ генератор — ровно та форма, что жила в
// дереве («intentionally a near-mirror of …/gen.py»).
func TestGeneratorHeaderInjectionForeignGeneratorIsFound(t *testing.T) {
	doc := nmCleanHeader + "\nThe generator is intentionally a near-mirror of services/vpc/tests/newman/scripts/gen.py.\n"
	findings, cen := nmHeaderAudit(t, doc, "network")
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел координату чужого генератора.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], "чужой генератор") ||
		!strings.Contains(findings[0], "services/vpc/tests/newman/scripts/gen.py") {
		t.Fatalf("находка не называет, ЧЕЙ генератор объявлен образцом: %q", findings[0])
	}
	if cen.withShared != 1 {
		t.Fatalf("первая ось повреждена инъекцией второй: «называет общий слой» %d вместо 1", cen.withShared)
	}
}

// Та же ось в форме координаты прежнего полирепо: `../kacho-vpc/...`.
func TestGeneratorHeaderInjectionLegacyRepoPathIsFound(t *testing.T) {
	doc := nmCleanHeader + "\nСтруктурно — копия `../kacho-compute/tests/newman/scripts/gen.py`.\n"
	findings, _ := nmHeaderAudit(t, doc, "network")
	if len(findings) == 0 {
		t.Fatalf("гейт не узнал координату прежнего полирепо — а именно в этой форме\n" +
			"она и стояла у двух наборов")
	}
	if !strings.Contains(findings[0], "kacho-compute") {
		t.Fatalf("находка не называет чужой набор: %q", findings[0])
	}
}

// ОСЬ II, законный близнец: ссылка на СВОЙ путь образцом не является.
func TestGeneratorHeaderOwnPathIsNotForeign(t *testing.T) {
	doc := "\ntests/newman/scripts/gen.py — генератор набора probe.\n" +
		"Полный путь: services/probe/tests/newman/scripts/gen.py.\n" +
		"Использование:\n    python3 scripts/gen.py network\n" +
		"Общий слой — tests/newman/kacholib/gen_shared.py.\n"
	findings, _ := nmHeaderAudit(t, doc, "network")
	if len(findings) != 0 {
		t.Fatalf("гейт объявил образцом СВОЙ собственный путь — он ловит форму, а не\n"+
			"чужеродность: %v", findings)
	}
}

// ОСЬ III: пример вызова называет модуль, которого у набора нет. Ровно эта форма
// жила в дереве дважды: шапка, скопированная у соседа, и модуль, снятый расколом
// домена.
func TestGeneratorHeaderInjectionUsageNamesAMissingModuleIsFound(t *testing.T) {
	findings, cen := nmHeaderAudit(t, nmCleanHeader, "operation")
	if len(findings) == 0 {
		t.Fatalf("гейт не увидел пример вызова, ведущий в никуда.\nперепись: %+v", cen)
	}
	if !strings.Contains(findings[0], `модуль "network"`) {
		t.Fatalf("находка не называет ИМЯ модуля — по такой диагностике не понять,\n"+
			"что чинить: %q", findings[0])
	}
	if cen.withShared != 1 {
		t.Fatalf("иные оси повреждены инъекцией третьей: %+v", cen)
	}
}

// ОСЬ III, законный близнец: пример без имени модуля (только флаг) предметом не
// является — иначе гейт краснел бы на шапке, которая просто короче.
func TestGeneratorHeaderUsageWithOnlyAFlagIsNotAFinding(t *testing.T) {
	doc := strings.Replace(nmCleanHeader,
		"    python3 scripts/gen.py network     # один модуль",
		"    python3 scripts/gen.py --validate  # делегировать проверке кейсов", 1)
	findings, cen := nmHeaderAudit(t, doc, "network")
	if len(findings) != 0 {
		t.Fatalf("гейт принял флаг за имя модуля: %v", findings)
	}
	if cen.usageLines != 0 {
		t.Fatalf("примеров с именем модуля насчитано %d при их отсутствии", cen.usageLines)
	}
}

// Предпосылка распознавателя шапки: тройной литерал читается целиком, а не до
// первой пустой строки. Без этой пробы сужение распознавателя прошло бы молча.
func TestGeneratorHeaderDocstringReaderTakesTheWholeLiteral(t *testing.T) {
	src := "#!/usr/bin/env python3\n\"\"\"первая строка\n\nвторая строка\n\"\"\"\nimport sys\n"
	got := moduleDocstring(src)
	if !strings.Contains(got, "вторая строка") {
		t.Fatalf("распознаватель шапки оборвался на пустой строке — всё, что ниже,\n"+
			"уходит из-под наблюдения: %q", got)
	}
	if strings.Contains(got, "import sys") {
		t.Fatalf("распознаватель шапки захватил КОД — тогда любое упоминание в теле\n"+
			"файла читалось бы как утверждение шапки: %q", got)
	}
	if moduleDocstring("import sys\n") != "" {
		t.Fatalf("файл без шапки обязан дать пустоту — тело гейта отказывает по ней")
	}
}
