// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проба гейта «обещание продукта живёт одним объявлением и сверяется с
// документацией» — инъекцией в ОБЕ стороны.
//
// Одной стороны мало, и здесь это особенно наглядно. Гейт, проверенный только на
// дефекте, ловил бы ФОРМУ: в дереве есть законные байтовые величины другого
// предмета — длина имени ресурса, длина значения метки, — и предикат «всякое N
// байт равно обещанию» краснел бы на них. Первый же ложный срабат такой гейт
// выключает. Поэтому рядом с каждым дефектом стоит законный близнец ТОЙ ЖЕ
// формы, и один из них — та самая посторонняя байтовая величина.

// synthPayloadFloorTree — минимальное дерево: домен с объявлением, читатель в
// прод-коде, страница документации. Вызывающий перекрывает любой файл своим.
//
// Пути НАМЕРЕННО совпадают с настоящими (`services/vpc/...`): запрет литерала
// действует в прод-дереве владельца, и синтетика, разложенная иначе, проверяла бы
// другой предикат.
func synthPayloadFloorTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"services/vpc/internal/domain/frame_guarantee.go": "" +
			"package domain\n\n" +
			"const GuaranteedPayloadFloorBytes = 1400\n",
		"services/vpc/internal/apps/kacho/config/validate.go": "" +
			"package config\n\n" +
			"import \"github.com/PRO-Robotech/kacho/services/vpc/internal/domain\"\n\n" +
			"func Below(declared int) bool { return declared < domain.GuaranteedPayloadFloorBytes }\n",
		"services/vpc/docs/content/architecture/data-plane.mdx": "" +
			"# Граница\n\n" +
			"Платформа обещает гарантированный минимум полезной нагрузки —\n" +
			"1400 байт на любом стенде.\n",
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

// payloadFloorAudit — тот же судья, что судит дерево. Плюс проверка предпосылки
// самой инъекции: синтетика обязана быть ПРОЧИТАНА, иначе «ноль находок» на ней
// ничего не значит.
func payloadFloorAudit(t *testing.T, root string) ([]payloadFloorFinding, payloadFloorCensus) {
	t.Helper()
	findings, census, err := auditGuaranteedPayloadFloor(root)
	if err != nil {
		t.Fatalf("перепись синтетического дерева: %v", err)
	}
	if census.GoFilesParsed == 0 || census.DocFilesRead == 0 {
		t.Fatalf("синтетическое дерево не осмотрено: %s", census)
	}
	return findings, census
}

func payloadFloorJoin(findings []payloadFloorFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}

// (б) ЗАКОННАЯ ФОРМА — гейт молчит.
//
// Здесь же стоит близнец, из-за которого предикат и был сужен до канонической
// формулировки: страница ограничений называет ДРУГУЮ байтовую величину («≤ 63
// байт» для значения метки). Гейт обязан её пропускать — иначе он ловит слово
// «байт», а не обещание продукта.
func TestPayloadFloorGateSilentOnLegitimateForm(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/api/security-group.mdx": "" +
			"# Группа\n\n" +
			"Значение метки — не длиннее 63 байт, ключ — 1..63 байт.\n",
	})
	findings, census := payloadFloorAudit(t, root)
	t.Log(census)

	if len(findings) != 0 {
		t.Fatalf("законная форма помечена — гейт ловит форму, а не предмет:\n%s",
			payloadFloorJoin(findings))
	}
	if census.Declarations != 1 || census.Value != 1400 || census.Readers == 0 {
		t.Fatalf("перепись не подтверждает, что законная форма РАЗОБРАНА: %s", census)
	}
	if census.AnchoredWindows == 0 || census.NumbersChecked == 0 {
		t.Fatalf("окно формулировки не найдено — молчание гейта означало бы «не читал»: %s", census)
	}
}

// (а1) Дефект: документация называет ДРУГОЕ число. Ровно так расходится пара
// «код + проза», и расходится молча — ни сборка, ни слияние об этом не скажут.
func TestPayloadFloorGateRedOnDocNumberDrift(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": "" +
			"# Граница\n\n" +
			"Платформа обещает гарантированный минимум полезной нагрузки — 1300 байт.\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("документация разошлась с объявлением, а гейт молчит — он не способен упасть")
	}
	if !strings.Contains(joined, "services/vpc/docs/content/architecture/data-plane.mdx:3") {
		t.Fatalf("находка не называет координату (файл:строка) — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "1300") || !strings.Contains(joined, "1400") {
		t.Fatalf("находка обязана назвать ОБА числа — написанное и объявленное:\n%s", joined)
	}
}

// (а2) Дефект: второе объявление той же величины. Два места об одном предмете —
// и первая же правка разведёт их.
func TestPayloadFloorGateRedOnSecondDeclaration(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/internal/handler/frame.go": "" +
			"package handler\n\n" +
			"const GuaranteedPayloadFloorBytes = 1400\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина объявлена дважды, а гейт молчит")
	}
	if !strings.Contains(joined, "services/vpc/internal/handler/frame.go") ||
		!strings.Contains(joined, "services/vpc/internal/domain/frame_guarantee.go") {
		t.Fatalf("находка обязана назвать ОБА объявления:\n%s", joined)
	}
}

// (а3) Дефект: величина записана литералом в прод-коде. Это и есть «число в трёх
// местах», от которого обещание рассыпается по копиям.
//
// Парный положительный контроль — в том же дереве: соседняя строка несёт ДРУГОЕ
// число (1450 — объявление возможностей исполнителя стенда, законная величина
// другого предмета), и гейт обязан промолчать о ней.
func TestPayloadFloorGateRedOnLiteralInProductionCode(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/internal/handler/frame.go": "" +
			"package handler\n\n" +
			"func Floor() int { return 1400 }\n\n" +
			"func DeclaredByStand() int { return 1450 }\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина записана литералом, а гейт молчит")
	}
	if !strings.Contains(joined, "services/vpc/internal/handler/frame.go:3") {
		t.Fatalf("находка не называет строку литерала:\n%s", joined)
	}
	if strings.Contains(joined, "1450") {
		t.Fatalf("гейт помечает ЧУЖОЕ число — он ловит любые литералы, а не величину обещания:\n%s",
			joined)
	}
}

// (а4) Дефект: величину не читает никто. Объявление без читателя выглядит
// закреплённым и не держит ничего — обещание при нём обеспечено ничем.
func TestPayloadFloorGateRedOnDeclarationWithoutReader(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/internal/apps/kacho/config/validate.go": "" +
			"package config\n\n" +
			"func Below(declared int) bool { return declared < 0 }\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величину не читает никто, а гейт молчит")
	}
	if !strings.Contains(joined, "не читает ни один файл прод-кода") {
		t.Fatalf("находка не называет предмет (отсутствие читателя):\n%s", joined)
	}
}

// (а5) Дефект: величина есть в коде, обещания в документации нет вовсе. Тогда
// арендатор догадывается, а догадка у каждого своя.
func TestPayloadFloorGateRedOnPromiseMissingFromDocs(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": "" +
			"# Граница\n\nПро полезную нагрузку кадра здесь не сказано ничего.\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("обещания в документации нет, а гейт молчит")
	}
	if !strings.Contains(joined, "<документация>") {
		t.Fatalf("находка не называет предмет (обещание не дано):\n%s", joined)
	}
}

// (а6) Дефект: формулировка обещания стоит, числа рядом нет — сверять нечем.
// Отдельный случай от (а5): страница выглядит дающей обещание.
func TestPayloadFloorGateRedOnAnchorWithoutItsNumber(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": "" +
			"# Граница\n\n" +
			"Платформа обещает гарантированный минимум полезной нагрузки на любом стенде.\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("формулировка стоит без числа, а гейт молчит")
	}
	if !strings.Contains(joined, "без его числа") {
		t.Fatalf("находка не называет предмет (формулировка без числа):\n%s", joined)
	}
}

// (а7) Дефект: число обещания названо в документации ТАМ, где формулировки нет.
// Такое место гейту невидимо по существу — оно переживёт правку величины.
func TestPayloadFloorGateRedOnUnanchoredNumber(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/api/subnet.mdx": "" +
			"# Подсеть\n\nРазмер полезной части кадра — 1400 байт.\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("число названо вне окна формулировки, а гейт молчит")
	}
	if !strings.Contains(joined, "services/vpc/docs/content/api/subnet.mdx:3") {
		t.Fatalf("находка не называет координату:\n%s", joined)
	}
}

// (а8) Предпосылка гейта проверяет СЕБЯ: величина, заданная не литералом, лишает
// его возможности сверить документацию — и он обязан упасть, а не промолчать.
func TestPayloadFloorGateRedOnNonLiteralDeclaration(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/internal/domain/frame_guarantee.go": "" +
			"package domain\n\n" +
			"const base = 700\n\n" +
			"const GuaranteedPayloadFloorBytes = base * 2\n",
	})
	findings, _ := payloadFloorAudit(t, root)
	joined := payloadFloorJoin(findings)

	if len(findings) == 0 {
		t.Fatal("величина задана выражением, а гейт молчит — его предпосылки больше нет")
	}
	if !strings.Contains(joined, "не целочисленным литералом") {
		t.Fatalf("находка не называет предмет (предпосылка гейта):\n%s", joined)
	}
}

// (б2) Второй законный близнец: типографская запись числа («1 400 байт»)
// обещанием быть не перестаёт. Без этого случая гейт краснел бы на верной
// странице, и его сняли бы как шумный.
func TestPayloadFloorGateSilentOnTypographicNumber(t *testing.T) {
	root := synthPayloadFloorTree(t, map[string]string{
		"services/vpc/docs/content/architecture/data-plane.mdx": "" +
			"# Граница\n\n" +
			"Гарантированный минимум полезной нагрузки — 1 400 байт.\n",
	})
	findings, census := payloadFloorAudit(t, root)
	if len(findings) != 0 {
		t.Fatalf("типографская запись помечена как расхождение:\n%s", payloadFloorJoin(findings))
	}
	if census.NumbersChecked == 0 {
		t.Fatalf("число не прочитано — молчание означало бы «не смотрел»: %s", census)
	}
}
