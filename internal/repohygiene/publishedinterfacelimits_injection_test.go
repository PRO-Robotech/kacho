// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проба гейта «опубликованная величина одного интерфейса живёт одним объявлением,
// сверяется с документацией и не выдаёт долг за исполнение» — инъекцией в ОБЕ стороны.
//
// Одной стороны мало, и здесь особенно: числа величин (1000, 2000, 8000, 10000) —
// НЕ приметные. Гейт, проверенный только на дефекте, ловил бы форму: рядом в дереве
// живут законные числа тех же порядков (предел размера страницы, ёмкости кэшей, длина
// значения метки), и первый же ложный срабат такой гейт выключает. Поэтому у каждого
// дефекта здесь стоит законный близнец ТОЙ ЖЕ формы, и один из них — «другая зона даёт
// то же число», то есть ровно то утверждение, ради которого величина и объявлена
// постоянной.

// synthLimitsTree — минимальное дерево: домен с объявлениями, страница с тремя
// обещаниями, реестр долга. Вызывающий перекрывает любой файл своим.
//
// Пути НАМЕРЕННО совпадают с настоящими: и область читателя, и путь реестра — часть
// предиката гейта, и синтетика, разложенная иначе, проверяла бы другое утверждение.
func synthLimitsTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"services/vpc/internal/domain/interface_limits.go": "" +
			"package domain\n\n" +
			"const (\n" +
			"\tGuaranteedInterfaceBandwidthFloorMbps   = 1000\n" +
			"\tInterfaceConnectionCeiling              = 10000\n" +
			"\tInterfaceConnectionRateCeilingPerSecond = 2000\n" +
			"\tInterfaceConnectionRateBurstCeiling     = 8000\n" +
			")\n",
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			synthLimitsBandwidthBlock, synthLimitsConnectionBlock, synthLimitsRateBlock),
		limitsDebtRegisterPath: synthLimitsRegister(map[string]string{}),
	}
	for rel, body := range base {
		if _, taken := files[rel]; !taken {
			files[rel] = body
		}
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// Три обещания страницы — порознь, чтобы проба могла подменить ровно одно.
const (
	synthLimitsBandwidthBlock = "" +
		":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n" +
		"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n" +
		":::\n"
	synthLimitsConnectionBlock = "" +
		":::info Одновременных соединений на интерфейс — не более 10 000 соединений\n" +
		"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n" +
		":::\n"
	synthLimitsRateBlock = "" +
		":::info Темп установления соединений — 2 000 соединений в секунду, всплеск до 8 000 соединений в секунду\n" +
		"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n" +
		":::\n"
)

func synthLimitsDocPage(blocks ...string) string {
	return "# Граница\n\n" + strings.Join(blocks, "\n")
}

// synthLimitsRegister — реестр долга. override подменяет тело одной записи; запись,
// которой нет в каталоге, задаётся ключом-именем.
func synthLimitsRegister(override map[string]string) string {
	body := "# Долг соответствия исполнителя\n\n"
	order := []string{
		"GuaranteedInterfaceBandwidthFloorMbps",
		"InterfaceConnectionCeiling",
		"InterfaceConnectionRateCeilingPerSecond",
		"InterfaceConnectionRateBurstCeiling",
	}
	seen := map[string]bool{}
	for _, id := range order {
		seen[id] = true
		body += "### `" + id + "`\n\n"
		if custom, ok := override[id]; ok {
			body += custom
			continue
		}
		body += "- **Наша сторона:** " + limitsNotChecked + "\n" +
			"- **Предикат снятия:** приёмка исполнителя подтверждает величину под нагрузкой\n\n"
	}
	for id, custom := range override {
		if seen[id] {
			continue
		}
		body += "### `" + id + "`\n\n" + custom
	}
	return body
}

// limitsAudit — тот же судья, что судит дерево. Плюс проверка предпосылки самой
// инъекции: синтетика обязана быть ПРОЧИТАНА, иначе «ноль находок» на ней ничего
// не значит.
func limitsAudit(t *testing.T, root string) ([]limitFinding, limitsCensus) {
	t.Helper()
	findings, census, err := auditPublishedInterfaceLimits(root)
	if err != nil {
		t.Fatalf("перепись синтетического дерева: %v", err)
	}
	if census.GoFilesParsed == 0 || census.DocFilesRead == 0 {
		t.Fatalf("синтетическое дерево не осмотрено: %s", census)
	}
	return findings, census
}

func limitsJoin(findings []limitFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}

// synthLimitsReader — прод-читатель величины полосы: ровно то, чем наша сторона
// СПОСОБНА связать обещание (страж старта сверяет объявление посадки с обещанием).
// Заведён отдельной константой, потому что участвует в обеих сторонах инъекции.
const synthLimitsReader = "" +
	"package config\n\n" +
	"import \"github.com/PRO-Robotech/kacho/services/vpc/internal/domain\"\n\n" +
	"func BelowProductFloor(declared int) bool {\n" +
	"\treturn declared < domain.GuaranteedInterfaceBandwidthFloorMbps\n" +
	"}\n"

// (б1) ЗАКОННАЯ ФОРМА — гейт молчит.
//
// Рядом стоит близнец, из-за которого предикат и не стал «всякое число в
// документации»: страница ограничений называет ДРУГУЮ величину другого предмета
// («≤ 63 байт» у значения метки). Гейт обязан её пропускать — иначе он ловит цифры,
// а не обещание продукта.
func TestInterfaceLimitsGateSilentOnLegitimateForm(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/api/security-group.mdx": "" +
			"# Группа\n\nЗначение метки — не длиннее 63 байт, ключ — 1..63 байт.\n",
	})
	findings, census := limitsAudit(t, root)
	t.Log(census)

	if len(findings) != 0 {
		t.Fatalf("законная форма помечена — гейт ловит форму, а не предмет:\n%s",
			limitsJoin(findings))
	}
	if !census.RegisterFound || census.RegisterBlocks != 4 {
		t.Fatalf("реестр долга не прочитан — молчание гейта означало бы «не смотрел»: %s", census)
	}
	for _, v := range census.PerValue {
		if v.Declarations == 0 || v.Windows == 0 || v.Figures == 0 || v.RegisterEntries == 0 {
			t.Fatalf("перепись не подтверждает, что величина %q РАЗОБРАНА: %s", v.Name, census)
		}
	}
}

// (б2) Законный близнец: та же полоса, записанная в другой единице и с типографским
// пробелом («1 000 Мбит/с»). Обещанием быть от этого не перестаёт — без нормализации
// единиц гейт краснел бы на верной странице и был бы снят как шумный.
func TestInterfaceLimitsGateSilentOnOtherUnitAndTypographicNumber(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 000 Мбит/с\n"+
				"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, census := limitsAudit(t, root)
	if len(findings) != 0 {
		t.Fatalf("запись в другой единице помечена как расхождение:\n%s", limitsJoin(findings))
	}
	if census.PerValue[0].Figures == 0 {
		t.Fatalf("число не прочитано — молчание означало бы «не смотрел»: %s", census)
	}
}

// (б3) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ на неизменность по зонам: страница прямо говорит, что в
// ЛЮБОЙ зоне и любой сети число то же. Всеобщая форма привязкой к частному размещению
// не является, и гейт обязан молчать — иначе утверждение «величина не зависит от зоны»
// стало бы непроизносимым.
func TestInterfaceLimitsGateSilentOnSameNumberInAnyZone(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n"+
				"В любой зоне и в любой сети — не менее 1 Гбит/с, и для всех семейств адресов "+
				"число то же.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, census := limitsAudit(t, root)
	if len(findings) != 0 {
		t.Fatalf("всеобщая форма («в любой зоне») помечена как привязка к частному:\n%s",
			limitsJoin(findings))
	}
	if census.PerValue[0].Figures < 2 {
		t.Fatalf("оба вхождения числа обязаны быть прочитаны, иначе молчание ничего "+
			"не значит: %s", census)
	}
}

// (б4) Законный близнец восьмого утверждения: у величины ПОЯВИЛСЯ читатель в прод-коде
// (страж старта), и реестр это говорит. Гейт обязан молчать — иначе долг нельзя было
// бы закрыть, и запись пережила бы свой предмет уже с другой стороны.
func TestInterfaceLimitsGateSilentWhenDebtMatchesANewReader(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/internal/apps/kacho/config/validate.go": synthLimitsReader,
		limitsDebtRegisterPath: synthLimitsRegister(map[string]string{
			"GuaranteedInterfaceBandwidthFloorMbps": "- **Наша сторона:** " +
				limitsSelfChecked + "\n" +
				"- **Предикат снятия:** приёмка исполнителя подтверждает полосу под нагрузкой\n\n",
		}),
	})
	findings, census := limitsAudit(t, root)
	if len(findings) != 0 {
		t.Fatalf("реестр, совпавший с деревом, помечен:\n%s", limitsJoin(findings))
	}
	if census.PerValue[0].Readers == 0 {
		t.Fatalf("читатель не найден — молчание означало бы, что классификация не работала: %s",
			census)
	}
}

// (а1) Дефект: документация называет ДРУГОЕ число. Ровно так расходится пара «код +
// проза», и расходится молча — ни сборка, ни слияние об этом не скажут.
func TestInterfaceLimitsGateRedOnDocNumberDrift(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 2 Гбит/с\n"+
				"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("документация разошлась с объявлением, а гейт молчит — он не способен упасть")
	}
	if !strings.Contains(joined, "services/vpc/docs/content/architecture/data-plane.mdx:3") {
		t.Fatalf("находка не называет координату (файл:строка) — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "2000") || !strings.Contains(joined, "1000") {
		t.Fatalf("находка обязана назвать ОБА числа — написанное и объявленное:\n%s", joined)
	}
}

// (а2) Дефект: второе объявление той же величины. Два места об одном предмете — и
// первая же правка разведёт их.
func TestInterfaceLimitsGateRedOnSecondDeclaration(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/internal/handler/limits.go": "" +
			"package handler\n\nconst InterfaceConnectionCeiling = 10000\n",
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина объявлена дважды, а гейт молчит")
	}
	if !strings.Contains(joined, "services/vpc/internal/handler/limits.go") ||
		!strings.Contains(joined, "services/vpc/internal/domain/interface_limits.go") {
		t.Fatalf("находка обязана назвать ОБА объявления:\n%s", joined)
	}
}

// (а3) Дефект: формулировка обещания стоит, числа рядом нет — сверять нечем. Страница
// при этом выглядит дающей обещание, и потому случай отдельный от «обещания нет вовсе».
func TestInterfaceLimitsGateRedOnAnchorWithoutItsNumber(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — величина постоянная\n"+
				"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("формулировка стоит без числа, а гейт молчит")
	}
	if !strings.Contains(joined, "стоит без числа 1000") {
		t.Fatalf("находка не называет предмет (формулировка без числа):\n%s", joined)
	}
}

// (а4) Дефект: число величины названо там, где формулировки нет. Такое место гейту
// невидимо по существу — оно переживёт правку величины. Сюда же попадает измеренное
// значение, публиковать которое нельзя.
func TestInterfaceLimitsGateRedOnUnanchoredNumber(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/api/subnet.mdx": "" +
			"# Подсеть\n\nЗамер на нашем стенде показал 40 Гбит/с через один интерфейс.\n",
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("число названо вне окна формулировки, а гейт молчит")
	}
	if !strings.Contains(joined, "services/vpc/docs/content/api/subnet.mdx:3") {
		t.Fatalf("находка не называет координату:\n%s", joined)
	}
	if !strings.Contains(joined, "ИЗМЕРЕННОЕ") {
		t.Fatalf("находка не называет второй, более опасный исход (публикация измеренного):\n%s",
			joined)
	}
}

// (а5) Дефект: обещание не названо всеобщим. Число есть, совпадает — и читается как
// свойство СВОЕЙ сети или зоны, после чего первое исключение станет законным.
func TestInterfaceLimitsGateRedOnPromiseNotStatedAsUniversal(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n"+
				"Планируйте нагрузку от этой величины.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("обещание не названо всеобщим, а гейт молчит")
	}
	if !strings.Contains(joined, "не названо ВСЕОБЩИМ") {
		t.Fatalf("находка не называет предмет (всеобщность обещания):\n%s", joined)
	}
}

// (а5б) Дефект тоньше: всеобщность заявлена, но названы не все три предмета — про
// семейство адресов умолчали. Умолчание о любом из них оставляет дверь для «а у нас
// исключение», и находка обязана сказать, КАКОГО предмета не хватает.
func TestInterfaceLimitsGateRedOnPartialInvarianceClause(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n"+
				"Число одно для всех сетей и для всех зон.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("предмет всеобщности пропущен, а гейт молчит")
	}
	if !strings.Contains(joined, "семейство") {
		t.Fatalf("находка не называет пропущенный предмет:\n%s", joined)
	}
}

// (а6) Дефект, ради которого проба про зоны и написана: число привязано к ЧАСТНОЙ
// зоне. Число при этом СОВПАДАЕТ с объявленным — иначе краснота была бы заслугой
// сверки чисел, а не проверки формы, и утверждение «не зависит от зоны» осталось бы
// непроверенным.
func TestInterfaceLimitsGateRedOnZoneQualifiedFigureWithTheSameNumber(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n"+
				"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n"+
				"В зоне ru-central1-b — не менее 1 Гбит/с.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("число привязано к частной зоне, а гейт молчит")
	}
	if !strings.Contains(joined, "ЧАСТНОМУ размещению") {
		t.Fatalf("находка не называет предмет (привязка к частному размещению):\n%s", joined)
	}
	if !strings.Contains(joined, "data-plane.mdx:5") {
		t.Fatalf("находка не называет координату привязки:\n%s", joined)
	}
}

// (а7) Дефект: рядом с обещанием назван его ВЫВОД. По плотности и бюджету узла
// опознаётся конкретная реализация фабрики — публичной странице это не адресовано.
func TestInterfaceLimitsGateRedOnDerivationPublished(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			":::info Гарантированная полоса на интерфейс — не менее 1 Гбит/с\n"+
				"Число одно для всех сетей, для всех зон и для обоих семейств адресов.\n"+
				"Получено делением бюджета узла на число интерфейсов на узел.\n:::\n",
			synthLimitsConnectionBlock, synthLimitsRateBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("вывод величины опубликован, а гейт молчит")
	}
	if !strings.Contains(joined, "ВЫВОД величины") {
		t.Fatalf("находка не называет предмет (вывод на публичной странице):\n%s", joined)
	}
}

// (а8) Предпосылка гейта проверяет СЕБЯ: величина, заданная не литералом, лишает его
// возможности сверить документацию — и он обязан упасть, а не промолчать.
func TestInterfaceLimitsGateRedOnNonLiteralDeclaration(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/internal/domain/interface_limits.go": "" +
			"package domain\n\n" +
			"const base = 500\n\n" +
			"const (\n" +
			"\tGuaranteedInterfaceBandwidthFloorMbps   = base * 2\n" +
			"\tInterfaceConnectionCeiling              = 10000\n" +
			"\tInterfaceConnectionRateCeilingPerSecond = 2000\n" +
			"\tInterfaceConnectionRateBurstCeiling     = 8000\n" +
			")\n",
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина задана выражением, а гейт молчит — его предпосылки больше нет")
	}
	if !strings.Contains(joined, "не целочисленным литералом") {
		t.Fatalf("находка не называет предмет (предпосылка гейта):\n%s", joined)
	}
}

// (а9) Дефект: обещание в документации есть вовсе не про эту величину — темп
// установления соединений не опубликован нигде. Тогда арендатор догадывается, а
// догадка у каждого своя.
func TestInterfaceLimitsGateRedOnPromiseMissingFromDocs(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			synthLimitsBandwidthBlock, synthLimitsConnectionBlock),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("обещание не опубликовано, а гейт молчит")
	}
	if !strings.Contains(joined, "<документация>") ||
		!strings.Contains(joined, "темп установления соединений") {
		t.Fatalf("находка не называет неопубликованную величину:\n%s", joined)
	}
}

// (а10) Дефект восьмого утверждения, первая сторона: реестра долга нет вовсе. Величины
// объявлены и выглядят гарантированными, хотя за них на нашей стороне не отвечает ничто.
func TestInterfaceLimitsGateRedOnMissingDebtRegister(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"services/vpc/internal/domain/interface_limits.go": "" +
			"package domain\n\n" +
			"const (\n" +
			"\tGuaranteedInterfaceBandwidthFloorMbps   = 1000\n" +
			"\tInterfaceConnectionCeiling              = 10000\n" +
			"\tInterfaceConnectionRateCeilingPerSecond = 2000\n" +
			"\tInterfaceConnectionRateBurstCeiling     = 8000\n" +
			")\n",
		"services/vpc/docs/content/architecture/data-plane.mdx": synthLimitsDocPage(
			synthLimitsBandwidthBlock, synthLimitsConnectionBlock, synthLimitsRateBlock),
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)

	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)
	if len(findings) == 0 {
		t.Fatal("реестра долга нет, а гейт молчит")
	}
	if !strings.Contains(joined, "реестра открытого долга нет в дереве") {
		t.Fatalf("находка не называет предмет (отсутствие реестра):\n%s", joined)
	}
}

// (а11) Дефект восьмого утверждения, вторая сторона и самая коварная: реестр
// УТВЕРЖДАЕТ, что наша сторона сверяет обещание при старте, а читателя в прод-коде нет.
// Это и есть «объявление, выданное за исполнение»: запись выглядит закрытым долгом.
func TestInterfaceLimitsGateRedOnDebtClaimingWorkThatIsNotThere(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		limitsDebtRegisterPath: synthLimitsRegister(map[string]string{
			"InterfaceConnectionCeiling": "- **Наша сторона:** " + limitsSelfChecked + "\n" +
				"- **Предикат снятия:** приёмка исполнителя подтверждает предел под нагрузкой\n\n",
		}),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("реестр приписал нашей стороне работу, которой нет, а гейт молчит")
	}
	if !strings.Contains(joined, "InterfaceConnectionCeiling") ||
		!strings.Contains(joined, limitsNotChecked) {
		t.Fatalf("находка не называет расхождение реестра с деревом:\n%s", joined)
	}
}

// (а12) Дефект восьмого утверждения, третья сторона — САМОИСТЕЧЕНИЕ: у величины
// появился страж старта, а реестр по-прежнему говорит «не проверяет ничего». Долг
// пережил свой предмет; такую запись обязан снимать гейт, а не память.
func TestInterfaceLimitsGateRedOnDebtThatOutlivedItsSubject(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/internal/apps/kacho/config/validate.go": synthLimitsReader,
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("долг пережил свой предмет, а гейт молчит")
	}
	if !strings.Contains(joined, "GuaranteedInterfaceBandwidthFloorMbps") ||
		!strings.Contains(joined, limitsSelfChecked) {
		t.Fatalf("находка не называет, чем дерево разошлось с реестром:\n%s", joined)
	}
}

// (а13) Дефект: у долга нет предиката снятия. Долг без условия снятия снять некому — он
// переживёт и того, кто его завёл, и причину, по которой он заведён.
func TestInterfaceLimitsGateRedOnDebtWithoutRemovalPredicate(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		limitsDebtRegisterPath: synthLimitsRegister(map[string]string{
			"InterfaceConnectionRateBurstCeiling": "- **Наша сторона:** " + limitsNotChecked + "\n" +
				"- **Предикат снятия:** позже\n\n",
		}),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("у долга нет предиката снятия, а гейт молчит")
	}
	if !strings.Contains(joined, "нет предиката снятия") {
		t.Fatalf("находка не называет предмет:\n%s", joined)
	}
}

// (а14) Дефект: в реестре записан долг по величине, которой в периметре нет. Запись,
// которой больше нечего исключать, наследуется следующей слепой зоной.
func TestInterfaceLimitsGateRedOnDebtWithoutSubject(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		limitsDebtRegisterPath: synthLimitsRegister(map[string]string{
			"RetiredInterfaceMagicNumber": "- **Наша сторона:** " + limitsNotChecked + "\n" +
				"- **Предикат снятия:** приёмка исполнителя подтверждает величину под нагрузкой\n\n",
		}),
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("в реестре запись без предмета, а гейт молчит")
	}
	if !strings.Contains(joined, "RetiredInterfaceMagicNumber") {
		t.Fatalf("находка не называет запись без предмета:\n%s", joined)
	}
}

// (а15) Дефект: величина не объявлена вовсе. Обещание документации не закреплено ничем,
// и сверять его число не с чем.
func TestInterfaceLimitsGateRedOnMissingDeclaration(t *testing.T) {
	root := synthLimitsTree(t, map[string]string{
		"services/vpc/internal/domain/interface_limits.go": "" +
			"package domain\n\n" +
			"const (\n" +
			"\tInterfaceConnectionCeiling              = 10000\n" +
			"\tInterfaceConnectionRateCeilingPerSecond = 2000\n" +
			"\tInterfaceConnectionRateBurstCeiling     = 8000\n" +
			")\n",
	})
	findings, _ := limitsAudit(t, root)
	joined := limitsJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина не объявлена, а гейт молчит")
	}
	if !strings.Contains(joined, "не объявлена ни в одном файле") {
		t.Fatalf("находка не называет предмет:\n%s", joined)
	}
}
