// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// browserbeforestand_injection_test.go — доказательство, что гейт порядка
// СПОСОБЕН упасть и способен смолчать.
//
// Гейт, не доказавший обоих умений, доказательством не является: молчащий на
// дефекте бесполезен, а кричащий на законной конструкции будет снят первым же,
// кому он помешает. Поэтому обе стороны проверяются на синтетическом
// содержимом, а не на дереве, — иначе проба умрёт вместе с починкой дерева и
// унесёт доказательство с собой.
package repohygiene

import (
	"strings"
	"testing"
)

// Дефект: браузер добывается ПОСЛЕ подъёма стенда — ровно то, что наблюдалось
// в console-e2e.yml шесть прогонов подряд.
const injBrowserAfterStand = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        run: make dev-up CLUSTER_NAME="$CLUSTER_NAME"
      - name: браузер
        run: npx playwright install chromium
`

// Законный близнец ТОЙ ЖЕ формы: те же два шага, порядок верный.
const injBrowserBeforeStand = `
name: проба
jobs:
  probes:
    steps:
      - name: браузер
        run: npx playwright install chromium
      - name: стенд
        run: make dev-up CLUSTER_NAME="$CLUSTER_NAME"
`

// Законный близнец: браузер есть, стенда нет — сторожить нечего.
const injBrowserNoStand = `
name: проба
jobs:
  probes:
    steps:
      - name: браузер
        run: npx playwright install chromium
`

// Законный близнец: стенд есть, браузера нет — сторожить нечего.
const injStandNoBrowser = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        run: make dev-up
`

// Граница предиката: `install-deps` — это apt, а не добыча браузера, и она
// законно стоит после стенда. Если бы гейт считал её добычей, он бы краснел на
// конструкции, которая предметом правила не является.
const injInstallDepsAfterStand = `
name: проба
jobs:
  probes:
    steps:
      - name: стенд
        run: make dev-up
      - name: системные зависимости
        run: npx playwright install-deps chromium
`

// Порядок считается ПО ЗАДАНИЯМ: шаги разных заданий друг друга не занимают,
// потому что исполняются на разных ранерах.
const injSeparateJobs = `
name: проба
jobs:
  stand:
    steps:
      - name: стенд
        run: make dev-up
  probes:
    steps:
      - name: браузер
        run: npx playwright install chromium
`

func TestBrowserBeforeStandGateRedsOnTheDefect(t *testing.T) {
	findings, census := checkBrowserBeforeStand("проба.yml", injBrowserAfterStand)
	if len(findings) == 0 {
		t.Fatal("гейт СМОЛЧАЛ на внесённом дефекте — он ничего не сторожит")
	}
	joined := strings.Join(findings, "\n")
	// Находка обязана НАЗЫВАТЬ координату: перечень без имён нечитаем, и его
	// перестают читать.
	for _, want := range []string{"проба.yml", "job probes", "браузер", "стенд"} {
		if !strings.Contains(joined, want) {
			t.Errorf("находка не называет %q — по ней нельзя найти место:\n%s", want, joined)
		}
	}
	if census.Browser != 1 || census.StandUps != 1 {
		t.Errorf("перепись разошлась с содержимым: браузер=%d стенд=%d, ожидалось 1 и 1",
			census.Browser, census.StandUps)
	}
}

func TestBrowserBeforeStandGateStaysSilentOnLegitimateForms(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantBrowser int
		wantStand   int
	}{
		{"верный порядок", injBrowserBeforeStand, 1, 1},
		{"браузер без стенда", injBrowserNoStand, 1, 0},
		{"стенд без браузера", injStandNoBrowser, 0, 1},
		{"install-deps после стенда — не добыча браузера", injInstallDepsAfterStand, 0, 1},
		{"разные задания — разные ранеры", injSeparateJobs, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, census := checkBrowserBeforeStand("проба.yml", c.raw)
			if len(findings) != 0 {
				t.Errorf("гейт ЗАКРАСНЕЛ на законной конструкции — первый же, кому он "+
					"помешает, его снимет:\n%s", strings.Join(findings, "\n"))
			}
			if census.Browser != c.wantBrowser || census.StandUps != c.wantStand {
				t.Errorf("перепись разошлась с содержимым: браузер=%d (ждали %d), стенд=%d (ждали %d)",
					census.Browser, c.wantBrowser, census.StandUps, c.wantStand)
			}
		})
	}
}

// Неразобранный файл — находка, а не тихий зелёный: иначе гейт «проходит» ровно
// там, где он ничего не прочитал.
func TestBrowserBeforeStandGateReportsUnparsableFile(t *testing.T) {
	findings, census := checkBrowserBeforeStand("битый.yml", "jobs: [ это не отображение")
	if len(findings) == 0 {
		t.Fatal("неразобранный YAML принят молча — «ноль находок» стало неотличимо от «ноль прочитанного»")
	}
	if !strings.Contains(findings[0], "НЕ проверен") {
		t.Errorf("находка не говорит, что файл не проверен: %s", findings[0])
	}
	if census.Files != 1 {
		t.Errorf("перепись не засчитала прочитанный файл: %d", census.Files)
	}
}

// TestSelfTestStepIsNotCountedAsAcquisition — доказательство в обе стороны для
// различения «добыл браузер» и «прогнал самопроверку скрипта добычи».
//
// Без него гейт можно обойти, не тронув ни одной его строки: оставить
// самопроверку впереди подъёма стенда, а настоящую добычу увести за него.
// Порядок при этом «соблюдён» — по шагу, который браузера не добывает.
func TestSelfTestStepIsNotCountedAsAcquisition(t *testing.T) {
	// (а) ДЕФЕКТ: настоящая добыча ПОСЛЕ стенда, самопроверка — до.
	broken := `
jobs:
  probes:
    steps:
      - name: самопроверка
        run: bash .github/scripts/install-pinned-browser.sh --self-test
      - name: стенд
        run: make dev-up
      - name: браузер
        run: bash .github/scripts/install-pinned-browser.sh
`
	findings, census := checkBrowserBeforeStand("синтетика.yml", broken)
	if census.Browser != 1 {
		t.Fatalf("самопроверка засчитана добычей: шагов добычи %d, ждали 1", census.Browser)
	}
	if len(findings) != 1 {
		t.Fatalf("порядок нарушен, а гейт молчит: находок %d — самопроверка замаскировала "+
			"настоящую добычу", len(findings))
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ: то же дерево, но добыча на своём месте.
	fine := `
jobs:
  probes:
    steps:
      - name: самопроверка
        run: bash .github/scripts/install-pinned-browser.sh --self-test
      - name: браузер
        run: bash .github/scripts/install-pinned-browser.sh
      - name: стенд
        run: make dev-up
`
	findings, census = checkBrowserBeforeStand("синтетика.yml", fine)
	if len(findings) != 0 {
		t.Fatalf("ложное срабатывание на исправном порядке: %v", findings)
	}
	if census.Browser != 1 {
		t.Fatalf("перепись добычи %d, ждали 1 (самопроверка не считается)", census.Browser)
	}
}
