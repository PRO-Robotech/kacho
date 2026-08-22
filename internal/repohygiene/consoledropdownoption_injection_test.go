// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoledropdownoption_injection_test.go — способность гейта упасть, доказанная
// в ОБЕ стороны на синтетике.
//
// Одной положительной стороны мало: она доказала бы, что гейт что-то принял, но
// не то, что он вообще способен отказать. Одной отрицательной — тоже: гейт,
// падающий на всём, отключат первым же ложным срабатыванием.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeWithMirror — запись, которая выглядит совершенно обычной и попадает в
// невидимое зеркало варианта. Форма взята с натуры: так был написан помощник
// `выбрать` до починки.
const probeWithMirror = `
import { test } from "@playwright/test";

// Вариант выбирается по тексту — форма, ради которой гейт и заведён.
test("выбор региона", async ({ page }) => {
  await page.locator(".ant-select").first().click();
  await page.locator(".ant-select-dropdown:visible").getByText(/ru-central1/).first().click();
});
`

// probeByOptionClass — законный близнец: та же цепочка, но вариант назван
// своим классом. Гейт обязан МОЛЧАТЬ.
const probeByOptionClass = `
import { test, expect } from "@playwright/test";

test("выбор региона", async ({ page }) => {
  await page.locator(".ant-select").first().click();
  const пункт = page
    .locator(".ant-select-dropdown:visible")
    .locator(".ant-select-item-option")
    .filter({ hasText: /ru-central1/ })
    .first();
  await expect(пункт).toBeVisible({ timeout: 20_000 });
  await пункт.click({ timeout: 20_000 });
});
`

// probeWithoutList — второй законный близнец: проба, которая списков не трогает
// вовсе. Гейт не вправе требовать от неё знания о варианте.
const probeWithoutList = `
import { test, expect } from "@playwright/test";

test("страница списка открывается", async ({ page }) => {
  await page.goto("/projects/p/nlb/target-groups");
  await expect(page.getByRole("table")).toBeVisible();
});
`

// probeWithClassesInComment — близнец, отделяющий КОД от прозы: оба класса
// названы только в комментарии, кода со списком нет. Гейт, читающий сырой
// текст, объявил бы находку на собственном объяснении.
const probeWithClassesInComment = `
import { test } from "@playwright/test";

// Здесь разбирается, почему ".ant-select-dropdown" нельзя опрашивать по тексту
// и почему вариант зовётся ".ant-select-item-option" — это ПРОЗА, а не селектор.
test("ничего не выбирает", async ({ page }) => {
  await page.goto("/");
});
`

func probeCorpus(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatalf("подготовка синтетики %s: %v", name, err)
		}
		paths = append(paths, p)
	}
	return paths
}

func TestConsoleDropdownGateFailsOnTheMirrorForm(t *testing.T) {
	// (а) ИНЪЕКЦИЯ: настоящая форма промаха обязана быть найдена и НАЗВАНА.
	found, read, touching, err := consoleDropdownFindings(
		probeCorpus(t, map[string]string{"mirror.spec.ts": probeWithMirror}))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("инъекция не найдена: находок %d при прочитанных %d, трогающих список %d",
			len(found), read, touching)
	}
	if !strings.Contains(consoleDropdownFindingNames(found), "mirror.spec.ts") {
		t.Fatalf("находка не названа по имени: %s", consoleDropdownFindingNames(found))
	}

	// (б) ЗАКОННЫЕ БЛИЗНЕЦЫ: гейт обязан молчать на всех трёх.
	for name, src := range map[string]string{
		"byclass.spec.ts": probeByOptionClass,
		"nolist.spec.ts":  probeWithoutList,
		"prose.spec.ts":   probeWithClassesInComment,
	} {
		got, _, _, gerr := consoleDropdownFindings(probeCorpus(t, map[string]string{name: src}))
		if gerr != nil {
			t.Fatalf("разбор близнеца %s: %v", name, gerr)
		}
		if len(got) != 0 {
			t.Errorf("ложное срабатывание на законной записи %s: %s",
				name, consoleDropdownFindingNames(got))
		}
	}

	// (в) ПРЕДПОСЫЛКА РАЗБОРА: комментарий действительно не считается за селектор.
	// Проверяется отдельно от (б), потому что «молчит» там могло бы объясняться
	// и тем, что разбор вообще ничего не читает.
	_, literals := tsScan(probeWithClassesInComment)
	if strings.Contains(strings.Join(literals, "\n"), consoleDropdownClass) {
		t.Fatal("разбор считает комментарий строковым литералом — гейт судил бы прозу, " +
			"и объяснение этого класса стало бы находкой на самом себе")
	}
	_, codeLiterals := tsScan(probeWithMirror)
	if !strings.Contains(strings.Join(codeLiterals, "\n"), consoleDropdownClass) {
		t.Fatal("разбор не видит селектора в КОДЕ — тогда «ноль находок» означает " +
			"«ноль прочитанного», а не чистоту")
	}
}
