// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «ссылка `verifies #<N>` называет задачу и стоит
// внутри пробы» СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало (молчание
// бывает от того, что читать не стали):
//
//	ссылка без номера          → краснеет, называя координату;
//	ссылка вне пробы           → краснеет, называя координату И вид находки;
//	ссылка внутри пробы        → молчит, и перепись её ЗАСЧИТЫВАЕТ;
//	ссылка в блочном коммента. → молчит (оба вида комментария читаются);
//	слово в коде и в строке    → молчит (маркер живёт в комментарии, не в тексте);
//	спека без ссылок вовсе     → молчит: пустой перечень есть цель, а не поломка.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditConsoleProbeIssueLinks`), что и прогон
// по дереву: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические спеки. Каждая — настоящая форма из этого набора, а не выдумка:
// каркас взят у `ui-future/e2e/specs/mutate.spec.ts`.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ 1. Связь объявлена, номера в ней нет: перейти некуда, снять пробу
// вместе с предметом нельзя — предмет не назван.
const synthSpecLinkWithoutNumber = `import { test, expect } from "@playwright/test";

test("метка остаётся после сохранения", async ({ page }) => {
  // verifies issue про метки
  await page.goto("/projects/p1/vpc/route-tables");
  await expect(page.getByText("env")).toBeVisible();
});
`

// ДЕФЕКТ 2. Форма верна, место — нет: ссылка стоит на верхнем уровне файла.
// Пробу снимут, ссылка останется и будет утверждать про набор то, чего в нём
// уже нет.
const synthSpecLinkOutsideProbe = `import { test, expect } from "@playwright/test";

// verifies #416
test("раздел compute не двоится", async ({ page }) => {
  await page.goto("/projects/p1/compute/instances");
  await expect(page.getByRole("table")).toHaveCount(1);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — канон: ссылка внутри пробы, номер назван.
const synthSpecLinkInsideProbe = `import { test, expect } from "@playwright/test";

test("раздел compute не двоится", async ({ page }) => {
  // verifies #416 — двоение раздела нашёл владелец на живом стенде
  await page.goto("/projects/p1/compute/instances");
  await expect(page.getByRole("table")).toHaveCount(1);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — блочный комментарий. Оба вида читаются: иначе автор,
// оформивший связь блоком, получал бы находку за верную ссылку.
const synthSpecLinkBlockComment = `import { test, expect } from "@playwright/test";

test.describe("метки", () => {
  test("правка маршрутов не стирает меток", async ({ page }) => {
    /* verifies #422 */
    await page.goto("/projects/p1/vpc/route-tables");
    await expect(page.getByText("env")).toBeVisible();
  });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — слово в ИСПОЛНЯЕМОЙ части и в тексте на экране. Маркер
// живёт в комментарии; читай гейт сырой текст, он покраснел бы на имени
// переменной и на строковом литерале, то есть на коде, к связи отношения не
// имеющем. Регулярный литерал со слэшами стоит здесь намеренно: принятый за
// деление, он переключил бы разбор и съел бы остаток файла.
const synthSpecWordInCode = `import { test, expect } from "@playwright/test";

const verifies = /\/vpc\/v1\/networks/;

test("сеть создаётся", async ({ page }) => {
  const c = await page.request.get("/vpc/v1/networks?projectId=p1");
  expect(c.ok(), "verifies #0 — это ТЕКСТ на экране, а не связь").toBeTruthy();
  expect(verifies.test("/vpc/v1/networks")).toBeTruthy();
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — спека без единой ссылки. Пустой перечень есть цель, а не
// поломка: находкой это быть не может.
const synthSpecNoLinks = `import { test, expect } from "@playwright/test";

test("вход выполняется", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Войти" })).toBeVisible();
});
`

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в предикат.
// ─────────────────────────────────────────────────────────────────────────────

func TestConsoleProbeIssueLinkPredicateSeparatesDefectFromLegalForm(t *testing.T) {
	const (
		noNumber  = "ui-future/e2e/specs/injected-no-number.spec.ts"
		outside   = "ui-future/e2e/specs/injected-outside.spec.ts"
		inside    = "ui-future/e2e/specs/injected-inside.spec.ts"
		blockCmt  = "ui-future/e2e/specs/injected-block.spec.ts"
		wordCode  = "ui-future/e2e/specs/injected-word-in-code.spec.ts"
		noLinkAll = "ui-future/e2e/specs/injected-no-links.spec.ts"
	)

	census, findings := auditConsoleProbeIssueLinks(map[string]string{
		noNumber:  synthSpecLinkWithoutNumber,
		outside:   synthSpecLinkOutsideProbe,
		inside:    synthSpecLinkInsideProbe,
		blockCmt:  synthSpecLinkBlockComment,
		wordCode:  synthSpecWordInCode,
		noLinkAll: synthSpecNoLinks,
	})

	got := map[string]consoleProbeLinkFinding{}
	for _, f := range findings {
		got[f.File] = f
	}

	t.Run("ссылка без номера краснеет и называет координату", func(t *testing.T) {
		f, ok := got[noNumber]
		if !ok {
			t.Fatal("связь без номера гейтом НЕ поймана — перейти по такой ссылке некуда, " +
				"и снять пробу вместе с её предметом нельзя: предмет не назван")
		}
		if f.Line != 4 {
			t.Errorf("координата строки %d, ожидалась 4 — вердикт без верной координаты не приводит к правке", f.Line)
		}
		if !strings.Contains(f.Why, "не называет номера") {
			t.Errorf("вид находки назван неверно: %q", f.Why)
		}
	})

	t.Run("ссылка вне пробы краснеет и отличается видом", func(t *testing.T) {
		f, ok := got[outside]
		if !ok {
			t.Fatal("ссылка на верхнем уровне файла гейтом НЕ поймана — она переживёт пробу, " +
				"к которой относилась, и будет утверждать про набор то, чего в нём уже нет")
		}
		if f.Line != 3 {
			t.Errorf("координата строки %d, ожидалась 3", f.Line)
		}
		if !strings.Contains(f.Why, "вне пробы") {
			t.Errorf("вид находки назван неверно: %q — два вида находки обязаны быть различимы "+
				"в вердикте, иначе правка пойдёт не туда", f.Why)
		}
	})

	t.Run("канонная ссылка молчит", func(t *testing.T) {
		if f, ok := got[inside]; ok {
			t.Errorf("ссылка внутри пробы объявлена находкой (%s) — гейт наказывал бы за верный исход", f.Why)
		}
	})

	t.Run("ссылка в блочном комментарии молчит", func(t *testing.T) {
		if f, ok := got[blockCmt]; ok {
			t.Errorf("связь, оформленная блочным комментарием, объявлена находкой (%s) — "+
				"гейт ловил бы оформление, а не существо", f.Why)
		}
	})

	t.Run("слово в коде и в строке молчит", func(t *testing.T) {
		if f, ok := got[wordCode]; ok {
			t.Errorf("имя переменной либо текст на экране объявлены связью (%s: %q) — "+
				"гейт читает сырой текст вместо комментариев и покраснеет на первой же "+
				"строке, к связи отношения не имеющей", f.Why, f.Text)
		}
	})

	t.Run("спека без ссылок молчит", func(t *testing.T) {
		if f, ok := got[noLinkAll]; ok {
			t.Errorf("отсутствие ссылок объявлено находкой (%s) — пустой перечень есть цель, "+
				"а не поломка: падение здесь толкало бы заводить ссылку ради зелёного", f.Why)
		}
	})

	t.Run("перепись различает виды и растёт на законных близнецах", func(t *testing.T) {
		// Без этих чисел «молчит» выше означало бы «не прочитал»: молчание на
		// законной форме обязано сопровождаться ростом объёма осмотренного.
		if census.Files != 6 {
			t.Errorf("спек прочитано %d, ожидалось 6", census.Files)
		}
		if census.Markers != 4 {
			t.Errorf("ссылок встречено %d, ожидалось 4 (две негодные + две канонные) — "+
				"распознавание маркера сломано", census.Markers)
		}
		if census.Good != 2 {
			t.Errorf("годных ссылок %d, ожидалось 2 — засчитывается не то", census.Good)
		}
		if len(census.Issues) != 2 || census.Issues[0] != 416 || census.Issues[1] != 422 {
			t.Errorf("названные задачи %v, ожидались [416 422]", census.Issues)
		}
		// Проб: по одной в пяти спеках, плюс вложенная в describe — describe
		// пробой не считается намеренно.
		if census.Probes != 6 {
			t.Errorf("проб распознано %d, ожидалось 6 — разбор вызовов test(…) сломан, "+
				"и деление «внутри/вне пробы» стало бы свойством поломки", census.Probes)
		}
	})
}

// TestConsoleProbeIssueLinkRejectsNumbersThatCannotBeATask — номер, которым
// задача быть не может, отвергается на форме, БЕЗ обращения к трекеру: сетевое
// измерение по умолчанию выключено, и без этой проверки `#0` доехал бы до
// перечня как настоящая связь.
func TestConsoleProbeIssueLinkRejectsNumbersThatCannotBeATask(t *testing.T) {
	for _, c := range []struct{ name, link string }{
		{"ноль", "// verifies #0"},
		{"ведущий ноль", "// verifies #0416"},
		{"решётки нет", "// verifies 416"},
		{"номера нет вовсе", "// verifies #"},
		{"чужой трекер", "// verifies KAC-416"},
		{"восемь разрядов", "// verifies #12345678"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "import { test } from \"@playwright/test\";\n\n" +
				"test(\"проба\", async ({ page }) => {\n  " + c.link + "\n  await page.goto(\"/\");\n});\n"
			census, findings := auditConsoleProbeIssueLinks(map[string]string{
				"ui-future/e2e/specs/injected-number.spec.ts": src,
			})
			if len(findings) != 1 {
				t.Fatalf("находок %d, ожидалась 1: %q принят за связь с задачей, "+
					"хотя номера, по которому переходят, в нём нет", len(findings), c.link)
			}
			if len(census.Issues) != 0 {
				t.Errorf("в перечень задач попало %v — негодная ссылка засчитана", census.Issues)
			}
		})
	}

	// Положительный контроль в паре: отрицания выше зеленели бы и на разборе,
	// который не признаёт связью НИЧЕГО.
	t.Run("канонная форма рядом принимается", func(t *testing.T) {
		src := "import { test } from \"@playwright/test\";\n\n" +
			"test(\"проба\", async ({ page }) => {\n  // verifies #416\n  await page.goto(\"/\");\n});\n"
		census, findings := auditConsoleProbeIssueLinks(map[string]string{
			"ui-future/e2e/specs/injected-number.spec.ts": src,
		})
		if len(findings) != 0 {
			t.Fatalf("канонная ссылка объявлена находкой: %v — все отрицания выше ничего не стоят", findings)
		}
		if len(census.Issues) != 1 || census.Issues[0] != 416 {
			t.Errorf("задачи %v, ожидалась [416]", census.Issues)
		}
	})
}

// TestConsoleProbeIssueLinkCorpusIsEmptyByRefusal — пустой КОРПУС и пустой
// ПЕРЕЧЕНЬ ссылок — разные вещи, и гейт обязан различать их: ноль спек означает
// «не прочитал», ноль ссылок — «нечего было находить».
func TestConsoleProbeIssueLinkCorpusIsEmptyByRefusal(t *testing.T) {
	census, findings := auditConsoleProbeIssueLinks(map[string]string{})
	if len(findings) != 0 {
		t.Errorf("на пустом корпусе получены находки %v — их неоткуда взять", findings)
	}
	if census.Files != 0 || census.Probes != 0 || census.Markers != 0 {
		t.Errorf("перепись пустого корпуса непуста: %+v", census)
	}
	// Отказ на пустом корпусе принимает ГЕЙТ (t.Fatal при len(sources)==0), а не
	// разборщик: разборщику пустая карта — законный вход, и это записано здесь,
	// чтобы следующий читатель не «починил» его отказом.
}
