// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Доказательство, что разбор локаторов строки формы умеет И краснеть, И
// молчать. Гейт рядом читает дерево — на исправленном дереве он зелен всегда, и
// одного его недостаточно: зелёный гейт неотличим от гейта, который ничего не
// распознаёт.
//
// Каждый законный близнец здесь — не выдумка, а форма, реально встречающаяся в
// спеках: отсечённая цепочка, выбор без текста, адресация не строки формы, и
// разбор класса ПРОЗОЙ. Последнее особенно важно: объяснение этого же класса
// написано в шапках помощников, и гейт по сырому тексту краснел бы на нём.

import (
	"strings"
	"testing"
)

func TestFormRowLocatorFindsTheUnguardedChain(t *testing.T) {
	const src = `
function поле(page: Page, подпись: string) {
  return page.locator(".ant-form-item", { has: page.getByText(подпись, { exact: true }) }).first();
}
`
	got, examined := FindUnguardedFormRowLocators(src)
	if examined != 1 {
		t.Fatalf("рассмотрено выражений %d, ожидалось 1 — разбор не видит своего предмета", examined)
	}
	if len(got) != 1 {
		t.Fatalf("находок %d, ожидалась 1: незащищённая цепочка не распознана", len(got))
	}
	if got[0].Line != 3 {
		t.Errorf("находка названа строкой %d, ожидалась 3 — координата уводит не туда", got[0].Line)
	}
}

// TestFormRowLocatorFindsTheAncestorAxisWithoutIndex — ось `ancestor` БЕЗ
// индекса возвращает всех предков-рядов, и `.first()` снова берёт самого
// внешнего: тот же дефект, записанный второй формой.
//
// Проба заведена вместе с распознаванием этой формы: до него разбор её не видел
// вовсе — ни как находку, ни как законную, — и объявил себя вакуумным (гейт
// упал на собственной предпосылке, а не на находке). Форма записи менялась,
// класс — нет.
func TestFormRowLocatorFindsTheAncestorAxisWithoutIndex(t *testing.T) {
	const src = `
function ряд(page: Page, подпись: string) {
  return page
    .getByText(подпись, { exact: true })
    .locator('xpath=ancestor::*[contains(@class, " ant-form-item ")]')
    .first();
}
`
	got, examined := FindUnguardedFormRowLocators(src)
	if examined != 1 {
		t.Fatalf("рассмотрено выражений %d, ожидалось 1 — разбор не видит ось `ancestor` как адресацию ряда", examined)
	}
	if len(got) != 1 {
		t.Fatalf("находок %d, ожидалась 1: ось без индекса берёт самого внешнего предка", len(got))
	}
}

func TestFormRowLocatorIsSilentOnGuardedAndOnUnrelatedForms(t *testing.T) {
	for _, c := range []struct{ имя, src string }{
		{
			"отсечение вложенных строк есть",
			`return page
  .locator(".ant-form-item")
  .filter({ has: page.getByText(подпись, { exact: true }) })
  .filter({ hasNot: page.locator(".ant-form-item") })
  .first();`,
		},
		{
			// Выбор строки формы БЕЗ текста неоднозначности предок/потомок не
			// создаёт: селектор совпадает с обеими, но вызывающий и не называет
			// текст, по которому их можно спутать.
			"строка формы выбирается не по тексту",
			`const все = page.locator(".ant-form-item");`,
		},
		{
			// Ближайший предок по оси `ancestor` — второй законный способ
			// назвать ряд, и он СИЛЬНЕЕ отсечения: не выбрасывает лишние
			// совпадения, а с самого начала называет одно. Разбор, знавший
			// только `hasNot`, объявил бы эту форму находкой — то есть
			// потребовал бы вернуть худшую ради зелёного гейта.
			"ряд взят ближайший по оси ancestor",
			`return page
  .getByText(подпись, { exact: true })
  .locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " ant-form-item ")][1]')
  .first();`,
		},
		{
			// Подполя строки списка — обычные блоки, не строки формы.
			"адресуется не строка формы",
			`return полеСписка.locator("div").filter({ has: page.getByText(подпись, { exact: true }) })
   .filter({ hasNot: page.locator(".ant-select") }).filter({ has: page.locator("input") });`,
		},
	} {
		got, _ := FindUnguardedFormRowLocators(c.src)
		if len(got) != 0 {
			t.Errorf("%s: найдено %d находок на законной форме — гейт ловит форму, а не существо: %v",
				c.имя, len(got), got)
		}
	}
}

// TestFormRowLocatorReadsCodeNotProse — разбор класса написан прозой в шапках
// помощников и в этом файле. Гейт по сырому тексту краснел бы на собственном
// объяснении: записанный класс (`testing.md` §«Гейт на класс», п.4).
func TestFormRowLocatorReadsCodeNotProse(t *testing.T) {
	const src = `
// Прежняя редакция звала page.locator(".ant-form-item", { has: page.getByText(x) }).first()
// и попадала в объемлющий блок.
/* То же в блочном комментарии:
   page.locator(".ant-form-item", { has: page.getByText(x) }).first();
*/
const ничего = 1;
`
	got, examined := FindUnguardedFormRowLocators(src)
	if examined != 0 || len(got) != 0 {
		t.Fatalf("разбор прочитал КОММЕНТАРИЙ как код: рассмотрено %d, находок %d — "+
			"гейт покраснел бы на объяснении того класса, который ловит", examined, len(got))
	}
}

// TestFormRowLocatorKeepsLineNumbersAcrossComments — координата находки обязана
// указывать на строку файла, а не на строку очищенного текста: комментарии
// вырезаются, и без сохранения переводов строк номера уехали бы вверх.
func TestFormRowLocatorKeepsLineNumbersAcrossComments(t *testing.T) {
	const src = `const a = 1;
/* пояснение
   в три
   строки */
return page.locator(".ant-form-item", { has: page.getByText(x) }).first();
`
	got, _ := FindUnguardedFormRowLocators(src)
	if len(got) != 1 {
		t.Fatalf("находок %d, ожидалась 1", len(got))
	}
	if got[0].Line != 5 {
		t.Errorf("координата %d, ожидалась 5 — вырезание комментариев сдвинуло нумерацию", got[0].Line)
	}
}

// TestStripTSCommentsKeepsStringLiterals — селектор живёт в строковом литерале,
// и вырезать его вместе с комментариями значило бы ослепить гейт полностью.
func TestStripTSCommentsKeepsStringLiterals(t *testing.T) {
	const src = `const s = "// не комментарий, а текст"; // а это комментарий`
	out := StripTSComments(src)
	if !strings.Contains(out, `"// не комментарий, а текст"`) {
		t.Errorf("строковый литерал повреждён очисткой: %q", out)
	}
	if strings.Contains(out, "а это комментарий") {
		t.Errorf("построчный комментарий не вырезан: %q", out)
	}
}
